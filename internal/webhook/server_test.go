package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// signedRequest builds a POST to the Slack route signed with testSecret at now.
func signedRequest(t *testing.T, url, body string, now time.Time) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+SlackEventsPath, strings.NewReader(body))
	require.NoError(t, err)
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	req.Header.Set(slack.HeaderTimestamp, ts)
	req.Header.Set(slack.HeaderSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func mentionBody(eventID string) string {
	return fmt.Sprintf(`{"type":"event_callback","team_id":"T1","event_id":%q,"event":{"type":"app_mention","user":"U1","channel":"C1","text":"<@B1> review https://github.com/o/r/pull/1","ts":"1.2"}}`, eventID)
}

// newTestServer starts the handler under httptest and shuts the job runner
// down when the test ends.
func newTestServer(t *testing.T, o Options) (*Server, *httptest.Server) {
	t.Helper()
	if o.SlackSigningSecret == "" {
		o.SlackSigningSecret = testSecret
	}
	if o.Logf == nil {
		o.Logf = t.Logf
	}
	s := NewServer(o)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, s.Shutdown(ctx))
	})
	return s, ts
}

func do(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

func TestServer_OversizedBodyIs413(t *testing.T) {
	var ran atomic.Int32
	_, ts := newTestServer(t, Options{
		MaxBodyBytes: 64,
		Dispatch: func(*slack.AppMention) (Job, bool) {
			ran.Add(1)
			return Job{}, false
		},
	})
	body := mentionBody("Ev1") // well over 64 bytes
	require.Greater(t, len(body), 64)
	code, _ := do(t, signedRequest(t, ts.URL, body, time.Now()))
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Zero(t, ran.Load(), "an oversized body must not reach dispatch")
}

func TestServer_BodyAtCapIsAccepted(t *testing.T) {
	body := mentionBody("Ev1")
	_, ts := newTestServer(t, Options{MaxBodyBytes: int64(len(body))})
	code, _ := do(t, signedRequest(t, ts.URL, body, time.Now()))
	assert.Equal(t, http.StatusOK, code)
}

func TestServer_RepeatedEventIDRunsOneJob(t *testing.T) {
	var runs atomic.Int32
	done := make(chan struct{}, 4)
	_, ts := newTestServer(t, Options{
		Dispatch: func(m *slack.AppMention) (Job, bool) {
			return Job{Key: "o/r#1", Run: func(context.Context) error {
				runs.Add(1)
				done <- struct{}{}
				return nil
			}}, true
		},
	})
	for i := 0; i < 3; i++ {
		req := signedRequest(t, ts.URL, mentionBody("EvSame"), time.Now())
		if i > 0 {
			req.Header.Set("X-Slack-Retry-Num", strconv.Itoa(i))
		}
		code, _ := do(t, req)
		assert.Equal(t, http.StatusOK, code, "a retry is still acknowledged")
		if i == 0 {
			<-done // let the first job finish so a rerun would not be refused as busy
		}
	}
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), runs.Load())
}

func TestServer_DistinctEventIDsEachRun(t *testing.T) {
	var runs atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	_, ts := newTestServer(t, Options{
		Dispatch: func(m *slack.AppMention) (Job, bool) {
			return Job{Key: m.EventID, Run: func(context.Context) error {
				runs.Add(1)
				wg.Done()
				return nil
			}}, true
		},
	})
	for _, id := range []string{"EvA", "EvB"} {
		code, _ := do(t, signedRequest(t, ts.URL, mentionBody(id), time.Now()))
		require.Equal(t, http.StatusOK, code)
	}
	wg.Wait()
	assert.Equal(t, int32(2), runs.Load())
}

func TestServer_AcknowledgesWhileJobStillRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	_, ts := newTestServer(t, Options{
		Dispatch: func(*slack.AppMention) (Job, bool) {
			return Job{Key: "o/r#1", Run: func(context.Context) error {
				close(started)
				<-release
				close(finished)
				return nil
			}}, true
		},
	})
	t0 := time.Now()
	code, _ := do(t, signedRequest(t, ts.URL, mentionBody("Ev1"), time.Now()))
	elapsed := time.Since(t0)
	assert.Equal(t, http.StatusOK, code)
	assert.Less(t, elapsed, AckDeadline, "Slack needs the acknowledgement within 3 seconds")
	<-started
	select {
	case <-finished:
		t.Fatal("job finished before it was released: the acknowledgement waited for it")
	default:
	}
	close(release)
	<-finished
}

func TestServer_StuckJobIsCancelledAtTimeout(t *testing.T) {
	got := make(chan error, 1)
	_, ts := newTestServer(t, Options{
		JobTimeout: 50 * time.Millisecond,
		Dispatch: func(*slack.AppMention) (Job, bool) {
			return Job{Key: "o/r#1", Run: func(ctx context.Context) error {
				<-ctx.Done() // stuck until cancelled
				got <- ctx.Err()
				return ctx.Err()
			}}, true
		},
	})
	t0 := time.Now()
	code, _ := do(t, signedRequest(t, ts.URL, mentionBody("Ev1"), time.Now()))
	require.Equal(t, http.StatusOK, code)
	select {
	case err := <-got:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(t0), 2*time.Second)
	case <-time.After(5 * time.Second):
		t.Fatal("stuck job was not cancelled at the timeout")
	}
}

func TestServer_RejectsUnsignedBadAndStale(t *testing.T) {
	var ran atomic.Int32
	_, ts := newTestServer(t, Options{
		Dispatch: func(*slack.AppMention) (Job, bool) { ran.Add(1); return Job{}, false },
	})
	body := mentionBody("Ev1")

	unsigned, err := http.NewRequest(http.MethodPost, ts.URL+SlackEventsPath, strings.NewReader(body))
	require.NoError(t, err)
	code, _ := do(t, unsigned)
	assert.Equal(t, http.StatusUnauthorized, code)

	bad := signedRequest(t, ts.URL, body, time.Now())
	bad.Header.Set(slack.HeaderSignature, "v0=00")
	code, _ = do(t, bad)
	assert.Equal(t, http.StatusUnauthorized, code)

	code, _ = do(t, signedRequest(t, ts.URL, body, time.Now().Add(-6*time.Minute)))
	assert.Equal(t, http.StatusUnauthorized, code)

	assert.Zero(t, ran.Load())
}

func TestServer_RejectedRequestDoesNotBurnEventID(t *testing.T) {
	ran := make(chan struct{}, 2)
	_, ts := newTestServer(t, Options{
		Dispatch: func(*slack.AppMention) (Job, bool) {
			return Job{Key: "k", Run: func(context.Context) error { ran <- struct{}{}; return nil }}, true
		},
	})
	body := mentionBody("Ev1")
	bad := signedRequest(t, ts.URL, body, time.Now())
	bad.Header.Set(slack.HeaderSignature, "v0=00")
	code, _ := do(t, bad)
	require.Equal(t, http.StatusUnauthorized, code)

	code, _ = do(t, signedRequest(t, ts.URL, body, time.Now()))
	require.Equal(t, http.StatusOK, code)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("a forged request with a real event id blocked the genuine delivery")
	}
}

func TestServer_URLVerificationEchoesChallenge(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	code, body := do(t, signedRequest(t, ts.URL, `{"type":"url_verification","challenge":"abc123"}`, time.Now()))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "abc123", body)
}

func TestServer_UnsupportedEventIsAcknowledged(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	code, _ := do(t, signedRequest(t, ts.URL, `{"type":"event_callback","event_id":"Ev9","event":{"type":"message"}}`, time.Now()))
	assert.Equal(t, http.StatusOK, code, "Slack retries anything that is not 2xx")
}

func TestServer_MalformedJSONIs400(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	code, _ := do(t, signedRequest(t, ts.URL, `{not json`, time.Now()))
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestServer_WrongMethodAndPath(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	resp, err := http.Get(ts.URL + SlackEventsPath)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	resp, err = http.Post(ts.URL+"/other", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_SlackUnconfiguredIs404(t *testing.T) {
	s := NewServer(Options{Logf: t.Logf})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	code, _ := do(t, signedRequest(t, ts.URL, mentionBody("Ev1"), time.Now()))
	assert.Equal(t, http.StatusNotFound, code)
}

func TestServer_SamePRBusyRefusesSecondJob(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var runs atomic.Int32
	_, ts := newTestServer(t, Options{
		Dispatch: func(*slack.AppMention) (Job, bool) {
			return Job{Key: "o/r#1", Run: func(context.Context) error {
				runs.Add(1)
				started <- struct{}{}
				<-release
				return nil
			}}, true
		},
	})
	code, _ := do(t, signedRequest(t, ts.URL, mentionBody("EvA"), time.Now()))
	require.Equal(t, http.StatusOK, code)
	<-started
	code, _ = do(t, signedRequest(t, ts.URL, mentionBody("EvB"), time.Now()))
	require.Equal(t, http.StatusOK, code)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), runs.Load(), "a second job on the same PR must not run while the first does")
	close(release)
}

func TestServer_DispatchDeclinedRunsNothing(t *testing.T) {
	_, ts := newTestServer(t, Options{
		Dispatch: func(*slack.AppMention) (Job, bool) { return Job{}, false },
	})
	code, _ := do(t, signedRequest(t, ts.URL, mentionBody("Ev1"), time.Now()))
	assert.Equal(t, http.StatusOK, code)
}

func TestListen_DefaultsToLoopbackAndRefusesOthers(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.0.2.1:0", "example.com:0", "nonsense"} {
		_, err := Listen(addr)
		assert.ErrorIs(t, err, ErrNotLoopback, addr)
	}
	ln, err := Listen("127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	host, _, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	assert.True(t, net.ParseIP(host).IsLoopback())
}

func TestListen_EmptyUsesDefault(t *testing.T) {
	addr := listenAddr("")
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	assert.True(t, net.ParseIP(host).IsLoopback(), addr)
}

func TestServer_ServeStopsOnContextAndCancelsJobs(t *testing.T) {
	jobCtxErr := make(chan error, 1)
	started := make(chan struct{})
	s := NewServer(Options{
		SlackSigningSecret: testSecret,
		Logf:               t.Logf,
		Dispatch: func(*slack.AppMention) (Job, bool) {
			return Job{Key: "k", Run: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				jobCtxErr <- ctx.Err()
				return ctx.Err()
			}}, true
		},
	})
	ln, err := Listen("127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()

	code, _ := do(t, signedRequest(t, "http://"+ln.Addr().String(), mentionBody("Ev1"), time.Now()))
	require.Equal(t, http.StatusOK, code)
	<-started
	cancel()
	select {
	case err := <-served:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after its context was cancelled")
	}
	assert.ErrorIs(t, <-jobCtxErr, context.Canceled)
}

func TestJobs_PanicDoesNotCrashAndFreesKey(t *testing.T) {
	j := newJobs(time.Minute, t.Logf)
	done := make(chan struct{})
	require.NoError(t, j.Start(Job{Key: "k", Run: func(context.Context) error {
		defer close(done)
		panic("boom")
	}}))
	<-done
	require.Eventually(t, func() bool {
		err := j.Start(Job{Key: "k", Run: func(context.Context) error { return nil }})
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, j.Shutdown(context.Background()))
}

func TestJobs_StartAfterShutdownIsRefused(t *testing.T) {
	j := newJobs(time.Minute, t.Logf)
	require.NoError(t, j.Shutdown(context.Background()))
	err := j.Start(Job{Key: "k", Run: func(context.Context) error { return nil }})
	assert.ErrorIs(t, err, ErrShuttingDown)
}

func TestJobs_ShutdownHonoursItsDeadline(t *testing.T) {
	j := newJobs(time.Minute, t.Logf)
	release := make(chan struct{})
	defer close(release)
	require.NoError(t, j.Start(Job{Key: "k", Run: func(context.Context) error {
		<-release // ignores cancellation
		return nil
	}}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assert.True(t, errors.Is(j.Shutdown(ctx), context.DeadlineExceeded))
}

func TestDedupe_ExpiresOldIDs(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	d := newDedupe(time.Hour)
	assert.True(t, d.firstSeen("a", now))
	assert.False(t, d.firstSeen("a", now.Add(time.Minute)))
	assert.True(t, d.firstSeen("a", now.Add(2*time.Hour)), "an id older than the window is forgotten")
	assert.True(t, d.firstSeen("", now), "an empty id is never deduplicated")
	assert.True(t, d.firstSeen("", now))
}
