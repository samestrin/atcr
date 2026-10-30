package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/integrations/linear"
	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const linearTestSecret = "lin-s3cret"

// linearBody is an Issue update for issue id in team teamKey, now in state
// stateID/stateName. changed lists the updatedFrom keys.
func linearBody(id, teamKey, stateID, stateName string, now time.Time, changed ...string) string {
	from := make([]string, 0, len(changed))
	for _, k := range changed {
		from = append(from, fmt.Sprintf("%q:%q", k, "old"))
	}
	return fmt.Sprintf(`{"action":"update","type":"Issue","webhookTimestamp":%d,`+
		`"data":{"id":%q,"teamId":"team-uuid","team":{"id":"team-uuid","key":%q},"state":{"id":%q,"name":%q,"type":"started"}},`+
		`"updatedFrom":{%s}}`, now.UnixMilli(), id, teamKey, stateID, stateName, strings.Join(from, ","))
}

// linearRequest builds a POST to the Linear route signed with linearTestSecret.
func linearRequest(t *testing.T, url, body, delivery string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+LinearWebhookPath, strings.NewReader(body))
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(linearTestSecret))
	mac.Write([]byte(body))
	req.Header.Set(linear.HeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set(linearDeliveryHeader, delivery)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// fakeLookup maps issue ids to PRs and counts calls. gate, when set, holds
// every lookup until it is closed.
type fakeLookup struct {
	mu    sync.Mutex
	prs   map[string]linear.PR
	gate  chan struct{}
	calls atomic.Int32
}

func (f *fakeLookup) PullRequest(ctx context.Context, issueID string) (linear.PR, error) {
	f.calls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return linear.PR{}, ctx.Err()
		}
	}
	f.mu.Lock()
	pr, ok := f.prs[issueID]
	f.mu.Unlock()
	if !ok {
		return linear.PR{}, linear.ErrNoPR
	}
	return pr, nil
}

// fakeCommenter records each PR comment.
type fakeCommenter struct {
	mu     sync.Mutex
	bodies []string
	prs    []string
}

func (c *fakeCommenter) PostIssueComment(_ context.Context, owner, repo string, number int, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prs = append(c.prs, fmt.Sprintf("%s/%s#%d", owner, repo, number))
	c.bodies = append(c.bodies, body)
	return nil
}

// logSink collects log lines safely across goroutines.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logSink) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

type linearHarness struct {
	srv    *Server
	ts     *httptest.Server
	lookup *fakeLookup
	logs   *logSink
	runner *LinearRunner
	review atomic.Int32
}

// newLinearHarness serves a LinearRunner over the PR fixture. review, when
// non-nil, runs inside the counted reviewer.
func newLinearHarness(t *testing.T, lookup *fakeLookup, review Reviewer) *linearHarness {
	t.Helper()
	f := newPRFixture(t)
	h := &linearHarness{lookup: lookup, logs: &logSink{}}
	h.runner = &LinearRunner{
		Repos:    f.repos(),
		Teams:    []string{"EST"},
		Statuses: []string{"In Review"},
		Lookup:   lookup,
		Scratch:  f.scratch,
		Review: func(ctx context.Context, dir string) ([]reconcile.Finding, error) {
			h.review.Add(1)
			if review != nil {
				return review(ctx, dir)
			}
			return nil, nil
		},
		Logf: h.logs.logf,
	}
	h.srv, h.ts = newTestServer(t, Options{
		LinearSigningSecret: linearTestSecret,
		LinearDispatch:      h.runner.Dispatch,
		Logf:                h.logs.logf,
	})
	return h
}

func (h *linearHarness) send(t *testing.T, body, delivery string) {
	t.Helper()
	code, _ := do(t, linearRequest(t, h.ts.URL, body, delivery))
	require.Equal(t, http.StatusOK, code)
}

// drain waits for every in-flight request to be dispatched and every job to
// return, without cancelling any.
func (h *linearHarness) drain(t *testing.T) {
	t.Helper()
	h.ts.Close()
	h.idle(t)
}

// idle waits until no job is running.
func (h *linearHarness) idle(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		h.srv.jobs.mu.Lock()
		defer h.srv.jobs.mu.Unlock()
		return len(h.srv.jobs.running) == 0
	}, 10*time.Second, 5*time.Millisecond)
}

func widgetPR() linear.PR { return linear.PR{Owner: "acme", Repo: "widget", Number: 7} }

// A redelivery, and a second delivery of the same status change, run one job.
func TestLinear_RepeatedDeliveryRunsOneJob(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	h := newLinearHarness(t, lookup, nil)

	now := time.Now()
	body := linearBody("iss-1", "EST", "st-review", "In Review", now, "stateId")
	h.send(t, body, "d-1")
	h.send(t, body, "d-1") // Linear retries the same delivery.
	// A second delivery for the same issue and state: new id and timestamp.
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", now.Add(time.Second), "stateId", "updatedAt"), "d-2")
	h.drain(t)

	assert.Equal(t, int32(1), lookup.calls.Load(), "one PR lookup")
	assert.Equal(t, int32(1), h.review.Load(), "one review")
	assert.Equal(t, 2, h.logs.count("already handled"))
}

// An update whose updatedFrom does not show a state change runs nothing, nor
// does a state change to a status that is not a trigger.
func TestLinear_NonTriggeringUpdatesRunNone(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"state unchanged":       linearBody("iss-1", "EST", "st-review", "In Review", now, "title", "updatedAt"),
		"empty updatedFrom":     linearBody("iss-1", "EST", "st-review", "In Review", now),
		"not a trigger status":  linearBody("iss-1", "EST", "st-todo", "Todo", now, "stateId"),
		"unlisted team":         linearBody("iss-1", "OPS", "st-review", "In Review", now, "stateId"),
		"not an issue (create)": strings.Replace(linearBody("iss-1", "EST", "st-review", "In Review", now, "stateId"), `"update"`, `"create"`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
			h := newLinearHarness(t, lookup, nil)
			h.send(t, body, "d-1")
			h.drain(t)
			assert.Zero(t, lookup.calls.Load(), "no PR lookup")
			assert.Zero(t, h.review.Load(), "no review")
		})
	}
}

// The same issue may trigger again once it reaches another trigger state.
func TestLinear_NewStateIsNotDeduplicated(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	h := newLinearHarness(t, lookup, nil)
	h.runner.Statuses = []string{"In Review", "Ready"}

	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	require.Eventually(t, func() bool { return h.review.Load() == 1 }, 10*time.Second, 5*time.Millisecond)
	h.idle(t) // the first job must finish, or the PR is still busy
	h.send(t, linearBody("iss-1", "EST", "st-ready", "ready", time.Now(), "stateId"), "d-2")
	h.drain(t)
	assert.Equal(t, int32(2), h.review.Load())
}

// A second event for the same PR while its review runs is refused.
func TestLinear_SecondEventForRunningPRIsRefused(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR(), "iss-2": widgetPR()}}
	started := make(chan struct{})
	release := make(chan struct{})
	h := newLinearHarness(t, lookup, func(ctx context.Context, _ string) ([]reconcile.Finding, error) {
		close(started)
		<-release
		return nil, nil
	})

	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	<-started
	// Another issue linked to the same PR.
	h.send(t, linearBody("iss-2", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-2")
	require.Eventually(t, func() bool { return h.logs.count(ErrJobBusy.Error()) == 1 }, 10*time.Second, 5*time.Millisecond)
	close(release)
	h.drain(t)

	assert.Equal(t, int32(2), lookup.calls.Load(), "both issues are looked up")
	assert.Equal(t, int32(1), h.review.Load(), "only the first runs a review")
	assert.Equal(t, 1, h.logs.count("refused"))
}

// A PR in a repository that is not in the repos map is ignored.
func TestLinear_UnlistedRepoIsIgnored(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": {Owner: "evil", Repo: "fork", Number: 7}}}
	h := newLinearHarness(t, lookup, nil)
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	h.drain(t)

	assert.Equal(t, int32(1), lookup.calls.Load())
	assert.Zero(t, h.review.Load(), "no review of an unlisted repo")
	assert.Equal(t, 1, h.logs.count(ErrRepoNotAllowed.Error()))
}

// The request is acknowledged before the PR lookup, which runs inside the job.
func TestLinear_AcknowledgesBeforePRLookup(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}, gate: make(chan struct{})}
	h := newLinearHarness(t, lookup, nil)

	req := linearRequest(t, h.ts.URL, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	done := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()
	select {
	case code := <-done:
		assert.Equal(t, http.StatusOK, code)
	case <-time.After(AckDeadline):
		t.Fatal("request was not acknowledged while the PR lookup was pending")
	}
	require.Eventually(t, func() bool { return lookup.calls.Load() == 1 }, 10*time.Second, 5*time.Millisecond)
	close(lookup.gate)
	h.drain(t)
	assert.Equal(t, int32(1), h.review.Load())
}

// A failed PR lookup ends the job without a review.
func TestLinear_LookupFailureRunsNoReview(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{}}
	h := newLinearHarness(t, lookup, nil)
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	h.drain(t)
	assert.Zero(t, h.review.Load())
	assert.Equal(t, 1, h.logs.count(linear.ErrNoPR.Error()))
}

// A job that never reviewed (here: no PR linked yet) does not burn the dedupe
// key, so the same issue and state delivered again runs one review.
func TestLinear_LookupFailureForgetsTheDedupeKey(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{}}
	h := newLinearHarness(t, lookup, nil)
	now := time.Now()
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", now, "stateId"), "d-1")
	h.idle(t)
	assert.Zero(t, h.review.Load())

	lookup.mu.Lock()
	lookup.prs["iss-1"] = widgetPR() // the developer linked the PR
	lookup.mu.Unlock()
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", now.Add(time.Second), "stateId"), "d-2")
	h.drain(t)
	assert.Equal(t, int32(1), h.review.Load(), "one review after the retry")
	assert.Zero(t, h.logs.count("already handled"))
}

// A delivery that ran a review keeps its key: delivered again, none runs.
func TestLinear_RanReviewKeepsTheDedupeKey(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	h := newLinearHarness(t, lookup, func(context.Context, string) ([]reconcile.Finding, error) {
		return nil, errors.New("review failed")
	})
	now := time.Now()
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", now, "stateId"), "d-1")
	h.idle(t)
	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", now.Add(time.Second), "stateId"), "d-2")
	h.drain(t)
	assert.Equal(t, int32(1), h.review.Load())
	assert.Equal(t, 1, h.logs.count("already handled"))
}

func TestLinear_RouteRefusesBadRequests(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	h := newLinearHarness(t, lookup, nil)
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")

	bad := linearRequest(t, h.ts.URL, body, "d-1")
	bad.Header.Set(linear.HeaderSignature, strings.Repeat("0", 64))
	code, _ := do(t, bad)
	assert.Equal(t, http.StatusUnauthorized, code)

	stale := linearBody("iss-1", "EST", "st-review", "In Review", time.Now().Add(-time.Hour), "stateId")
	code, _ = do(t, linearRequest(t, h.ts.URL, stale, "d-2"))
	assert.Equal(t, http.StatusUnauthorized, code)

	get, err := http.NewRequest(http.MethodGet, h.ts.URL+LinearWebhookPath, nil)
	require.NoError(t, err)
	code, _ = do(t, get)
	assert.Equal(t, http.StatusMethodNotAllowed, code)

	h.drain(t)
	assert.Zero(t, lookup.calls.Load())
}

func TestLinear_RouteIs404WithoutSecret(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	code, _ := do(t, linearRequest(t, ts.URL, "{}", "d-1"))
	assert.Equal(t, http.StatusNotFound, code)
}

// With a commenter, the summary is posted on the linked PR.
func TestLinearRunner_CommentsOnThePR(t *testing.T) {
	f := newPRFixture(t)
	c := &fakeCommenter{}
	r := &LinearRunner{
		Repos: f.repos(), Teams: []string{"est"}, Statuses: []string{"in review"},
		Lookup:  &fakeLookup{prs: map[string]linear.PR{"iss-1": {Owner: "ACME", Repo: "Widget", Number: 7}}},
		Scratch: f.scratch, Commenter: c, Secrets: []string{plantedToken},
		Review: func(_ context.Context, dir string) ([]reconcile.Finding, error) { return stubFindings(dir), nil },
	}
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	job, ok := r.Dispatch(ev)
	require.True(t, ok)
	assert.Equal(t, "linear:iss-1", job.Key)
	key, err := job.Resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "acme/widget#7", key, "the configured repo spelling keys the job")
	require.NoError(t, job.Run(context.Background()))

	require.Len(t, c.bodies, 1)
	assert.Equal(t, "ACME/Widget#7", c.prs[0])
	assert.Contains(t, c.bodies[0], "4 findings")
	assert.NotContains(t, c.bodies[0], plantedToken)
	assert.NotContains(t, c.bodies[0], f.checkout)
	assertNothingLeft(t, f)
}

// The PR comment is GitHub markdown: the heading is **bold**, and a title's
// markdown characters are backslash-escaped (GitHub renders them as
// themselves) rather than turned into Slack's &amp; and &lt; entities.
func TestLinearRunner_CommentIsGitHubMarkdown(t *testing.T) {
	f := newPRFixture(t)
	c := &fakeCommenter{}
	r := &LinearRunner{
		Repos: f.repos(), Teams: []string{"est"}, Statuses: []string{"in review"},
		Lookup:  &fakeLookup{prs: map[string]linear.PR{"iss-1": {Owner: "acme", Repo: "widget", Number: 7}}},
		Scratch: f.scratch, Commenter: c,
		Review: func(context.Context, string) ([]reconcile.Finding, error) {
			return []reconcile.Finding{{Severity: "HIGH", Problem: "Compare a & b < c with *x*"}}, nil
		},
	}
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	job, ok := r.Dispatch(ev)
	require.True(t, ok)
	_, err = job.Resolve(context.Background())
	require.NoError(t, err)
	require.NoError(t, job.Run(context.Background()))

	require.Len(t, c.bodies, 1)
	got := c.bodies[0]
	assert.True(t, strings.HasPrefix(got, "**ATCR review of acme/widget#7**"), "heading is GitHub bold: %q", got)
	assert.NotContains(t, got, "&amp;")
	assert.NotContains(t, got, "&lt;")
	assert.Contains(t, got, `Compare a \& b \< c with \*x\*`)
}

// A failed review's PR comment uses GitHub bold too.
func TestFailureText_GitHubBold(t *testing.T) {
	text := failureText("acme/widget#7", context.Canceled, githubFormat)
	assert.Equal(t, "**ATCR review of acme/widget#7** was cancelled.", text)
}

// Through the real GitHub client and a fake GitHub server, the posted comment
// carries severity counts and titles and no checkout path or secret.
func TestLinearRunner_PostsThroughGitHubClient(t *testing.T) {
	f := newPRFixture(t)
	var mu sync.Mutex
	var gotPath, gotAuth, gotBody string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p struct {
			Body string `json:"body"`
		}
		_ = json.Unmarshal(raw, &p)
		mu.Lock()
		gotPath, gotAuth, gotBody = r.URL.Path, r.Header.Get("Authorization"), p.Body
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer gh.Close()
	r := &LinearRunner{
		Repos: f.repos(), Teams: []string{"EST"}, Statuses: []string{"In Review"},
		Lookup:  &fakeLookup{prs: map[string]linear.PR{"iss-1": {Owner: "ACME", Repo: "Widget", Number: 7}}},
		Scratch: f.scratch, Secrets: []string{plantedToken},
		Commenter: &ghaction.Client{APIURL: gh.URL, Token: "gh-tok", HTTPClient: gh.Client()},
		Review:    func(_ context.Context, dir string) ([]reconcile.Finding, error) { return stubFindings(dir), nil },
	}
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	job, ok := r.Dispatch(ev)
	require.True(t, ok)
	_, err = job.Resolve(context.Background())
	require.NoError(t, err)
	require.NoError(t, job.Run(context.Background()))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/repos/ACME/Widget/issues/7/comments", gotPath)
	assert.Equal(t, "Bearer gh-tok", gotAuth)
	assert.Contains(t, gotBody, "4 findings")
	assert.NotContains(t, gotBody, plantedToken)
	assert.NotContains(t, gotBody, f.checkout)
}

// A failed review still comments, without the cause's detail.
func TestLinearRunner_FailureCommentsWithoutDetail(t *testing.T) {
	f := newPRFixture(t)
	c := &fakeCommenter{}
	r := &LinearRunner{
		Repos: f.repos(), Teams: []string{"EST"}, Statuses: []string{"In Review"},
		Lookup:  &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}},
		Scratch: f.scratch, Commenter: c,
		Review: func(context.Context, string) ([]reconcile.Finding, error) {
			return nil, errors.New("model said " + plantedToken)
		},
	}
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	job, ok := r.Dispatch(ev)
	require.True(t, ok)
	_, err = job.Resolve(context.Background())
	require.NoError(t, err)
	require.Error(t, job.Run(context.Background()))
	require.Len(t, c.bodies, 1)
	assert.Contains(t, c.bodies[0], "failed")
	assert.NotContains(t, c.bodies[0], plantedToken)
}

func TestLinearRunner_IgnoresWithoutReviewerOrLookup(t *testing.T) {
	body := linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	_, ok := (&LinearRunner{Teams: []string{"EST"}, Statuses: []string{"In Review"}}).Dispatch(ev)
	assert.False(t, ok)
}

func signedHeader(t *testing.T, body string) http.Header {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(linearTestSecret))
	mac.Write([]byte(body))
	h := http.Header{}
	h.Set(linear.HeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	return h
}

// fakeFixer counts auto-fix runs. gate, when set, holds each run until closed.
type fakeFixer struct {
	mu      sync.Mutex
	targets []AutoFixTarget
	started chan struct{}
	gate    chan struct{}
}

func (f *fakeFixer) Run(ctx context.Context, t AutoFixTarget) error {
	f.mu.Lock()
	f.targets = append(f.targets, t)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeFixer) runs() []AutoFixTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AutoFixTarget(nil), f.targets...)
}

// fakeGitHub serves the pull request head (branch "feature", same repo) and
// records each PR comment.
type fakeGitHub struct {
	ts       *httptest.Server
	headRepo string
	mu       sync.Mutex
	comments []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{headRepo: "acme/widget"}
	g.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/pulls/7":
			_, _ = fmt.Fprintf(w, `{"number":7,"head":{"ref":"feature","repo":{"full_name":%q}}}`, g.headRepo)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widget/issues/7/comments":
			var body struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.mu.Lock()
			g.comments = append(g.comments, body.Body)
			g.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.ts.Close)
	return g
}

func (g *fakeGitHub) posted() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.comments...)
}

// newAutoFixHarness is newLinearHarness with auto-fix on "Fix It" for
// acme/widget, a fresh state file with dailyCap, and the fake GitHub server.
func newAutoFixHarness(t *testing.T, lookup *fakeLookup, fixer *fakeFixer, dailyCap int) (*linearHarness, *fakeGitHub, string) {
	t.Helper()
	h := newLinearHarness(t, lookup, nil)
	gh := newFakeGitHub(t)
	path := filepath.Join(t.TempDir(), "auto-fix-state.json")
	state, err := OpenAutoFixState(path, dailyCap)
	require.NoError(t, err)
	client := &ghaction.Client{APIURL: gh.ts.URL, Token: "push-tok", HTTPClient: gh.ts.Client()}
	h.runner.AutoFix = &LinearAutoFix{
		Statuses:  []string{"Fix It"},
		Repos:     []string{"acme/widget"},
		State:     state,
		Heads:     client,
		Fixer:     fixer,
		Commenter: client,
	}
	return h, gh, path
}

// A redelivery, a second delivery of the same change, and the same change after
// a restart (a new server over the same state file) run one auto-fix job.
func TestLinearAutoFix_RepeatedDeliveryRunsOneJob(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	fixer := &fakeFixer{}
	h, gh, path := newAutoFixHarness(t, lookup, fixer, 5)

	now := time.Now()
	body := linearBody("iss-1", "EST", "st-fix", "Fix It", now, "stateId")
	h.send(t, body, "d-1")
	h.send(t, body, "d-1")
	h.send(t, linearBody("iss-1", "EST", "st-fix", "Fix It", now.Add(time.Second), "stateId", "updatedAt"), "d-2")
	h.drain(t)

	runs := fixer.runs()
	require.Len(t, runs, 1)
	assert.Equal(t, AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature"}, runs[0])
	assert.Zero(t, h.review.Load(), "an auto-fix status runs no review")
	assert.Empty(t, gh.posted())

	// After a restart the in-memory dedupe is empty; the state file refuses.
	h2, _, _ := newAutoFixHarness(t, lookup, fixer, 5)
	state, err := OpenAutoFixState(path, 5)
	require.NoError(t, err)
	h2.runner.AutoFix.State = state
	h2.send(t, linearBody("iss-1", "EST", "st-fix", "Fix It", now.Add(2*time.Second), "stateId"), "d-3")
	h2.drain(t)
	assert.Len(t, fixer.runs(), 1, "the replay after a restart starts no second fix")
	assert.Equal(t, 1, h2.logs.count(ErrAutoFixClaimed.Error()))
}

// A second event for the same issue while its auto-fix runs is refused, even
// for another state.
func TestLinearAutoFix_SecondEventForRunningIssueIsRefused(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	fixer := &fakeFixer{started: make(chan struct{}, 1), gate: make(chan struct{})}
	h, _, _ := newAutoFixHarness(t, lookup, fixer, 5)
	h.runner.AutoFix.Statuses = []string{"Fix It", "Fix Again"}

	h.send(t, linearBody("iss-1", "EST", "st-fix", "Fix It", time.Now(), "stateId"), "d-1")
	<-fixer.started
	h.send(t, linearBody("iss-1", "EST", "st-again", "Fix Again", time.Now(), "stateId"), "d-2")
	require.Eventually(t, func() bool { return h.logs.count(ErrJobBusy.Error()) == 1 }, 10*time.Second, 5*time.Millisecond)
	close(fixer.gate)
	h.drain(t)

	assert.Len(t, fixer.runs(), 1)
	assert.Equal(t, 1, h.logs.count("linear:iss-1"), "the refusal names the issue")
}

// An unlisted team, a repository outside the auto-fix allowlist (though in
// webhook.repos), a repository outside webhook.repos and a fork pull request
// run no auto-fix and use no claim.
func TestLinearAutoFix_UnlistedTeamOrRepoRunsNone(t *testing.T) {
	cases := map[string]struct {
		team     string
		pr       linear.PR
		allow    []string
		headRepo string
		log      string
	}{
		"unlisted team":            {team: "OPS", pr: widgetPR(), allow: []string{"acme/widget"}, log: "team not allowlisted"},
		"repo not in auto-fix":     {team: "EST", pr: widgetPR(), allow: []string{"acme/other"}, log: ErrRepoNotAllowed.Error()},
		"repo not in webhook.repo": {team: "EST", pr: linear.PR{Owner: "evil", Repo: "fork", Number: 7}, allow: []string{"evil/fork"}, log: ErrRepoNotAllowed.Error()},
		"fork pull request":        {team: "EST", pr: widgetPR(), allow: []string{"acme/widget"}, headRepo: "evil/widget", log: ErrForkPR.Error()},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": c.pr}}
			fixer := &fakeFixer{}
			h, gh, _ := newAutoFixHarness(t, lookup, fixer, 5)
			h.runner.AutoFix.Repos = c.allow
			if c.headRepo != "" {
				gh.headRepo = c.headRepo
			}
			h.send(t, linearBody("iss-1", c.team, "st-fix", "Fix It", time.Now(), "stateId"), "d-1")
			h.drain(t)

			assert.Empty(t, fixer.runs())
			assert.Zero(t, h.review.Load())
			assert.Zero(t, h.runner.AutoFix.State.Count(time.Now()), "no claim is used")
			assert.Equal(t, 1, h.logs.count(c.log))
		})
	}
}

// The cap-plus-one event of a day posts "daily cap reached" on the pull
// request through GitHub and starts no job.
func TestLinearAutoFix_DailyCapPostsCommentAndRunsNone(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR(), "iss-2": widgetPR()}}
	fixer := &fakeFixer{}
	h, gh, _ := newAutoFixHarness(t, lookup, fixer, 1)

	h.send(t, linearBody("iss-1", "EST", "st-fix", "Fix It", time.Now(), "stateId"), "d-1")
	require.Eventually(t, func() bool { return len(fixer.runs()) == 1 }, 10*time.Second, 5*time.Millisecond)
	h.idle(t)
	h.send(t, linearBody("iss-2", "EST", "st-fix", "Fix It", time.Now(), "stateId"), "d-2")
	h.drain(t)

	assert.Len(t, fixer.runs(), 1, "the cap-plus-one event starts no job")
	comments := gh.posted()
	require.Len(t, comments, 1)
	assert.Contains(t, comments[0], "daily cap reached")
	assert.Contains(t, comments[0], "acme/widget#7")
	assert.NotContains(t, comments[0], "iss-2", "the comment carries the cap only")
	assert.Equal(t, 1, h.logs.count(ErrAutoFixDailyCap.Error()))
}

// A status in both lists starts the auto-fix; a review status still reviews.
func TestLinearAutoFix_ReviewStatusStillReviews(t *testing.T) {
	lookup := &fakeLookup{prs: map[string]linear.PR{"iss-1": widgetPR()}}
	fixer := &fakeFixer{}
	h, _, _ := newAutoFixHarness(t, lookup, fixer, 5)

	h.send(t, linearBody("iss-1", "EST", "st-review", "In Review", time.Now(), "stateId"), "d-1")
	h.drain(t)
	assert.Equal(t, int32(1), h.review.Load())
	assert.Empty(t, fixer.runs())
}

func TestLinearRunner_IgnoresIncompleteAutoFix(t *testing.T) {
	body := linearBody("iss-1", "EST", "st-fix", "Fix It", time.Now(), "stateId")
	ev, err := linear.Verify(linearTestSecret, signedHeader(t, body), []byte(body), time.Now())
	require.NoError(t, err)
	r := &LinearRunner{Teams: []string{"EST"}, Lookup: &fakeLookup{}, AutoFix: &LinearAutoFix{Statuses: []string{"Fix It"}}}
	_, ok := r.Dispatch(ev)
	assert.False(t, ok)
}
