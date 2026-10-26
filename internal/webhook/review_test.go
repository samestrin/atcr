package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	plantedToken = "xoxb-999-888-PLANTEDsecretVALUE"
	rawMarker    = "RAW-MODEL-OUTPUT-MARKER"
)

// slackRecorder is a fake Slack Web API that records each chat.postMessage.
type slackRecorder struct {
	srv    *httptest.Server
	mu     sync.Mutex
	posts  []map[string]any
	status int
	reply  string
}

func newSlackRecorder(t *testing.T) *slackRecorder {
	t.Helper()
	r := &slackRecorder{status: http.StatusOK, reply: `{"ok":true}`}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		r.mu.Lock()
		r.posts = append(r.posts, body)
		status, reply := r.status, r.reply
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *slackRecorder) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.posts {
		s, _ := p["text"].(string)
		out = append(out, s)
	}
	return out
}

func (r *slackRecorder) poster() *slack.Poster {
	return &slack.Poster{Token: plantedToken, BaseURL: r.srv.URL, Timeout: 2 * time.Second}
}

func testMention(text string) *slack.AppMention {
	return &slack.AppMention{EventID: "Ev1", TeamID: "T1", Channel: "C1", User: "U1", Text: text, TS: "1700000000.000100"}
}

func testAllow() slack.Allowlist {
	return slack.Allowlist{Workspaces: []string{"T1"}, Channels: []string{"C1"}, Users: []string{"U1"}, Repos: []string{"acme/widget"}}
}

// stubFindings returns findings whose every free-text field carries something
// that must never reach Slack: the worktree path, the planted token and raw
// model output.
func stubFindings(dir string) []reconcile.Finding {
	return []reconcile.Finding{
		{Severity: "CRITICAL", File: filepath.Join(dir, "db/query.go"), Line: 10,
			Problem: "SQL injection in query builder\n" + rawMarker + " full model reasoning",
			Fix:     rawMarker + " fix text", Evidence: rawMarker + " evidence " + dir},
		{Severity: "high", File: "auth.go", Problem: "Token " + plantedToken + " logged at " + dir + "/auth.go:12",
			Evidence: rawMarker},
		{Severity: "HIGH", File: "b.go", Problem: "Missing nil check", Evidence: rawMarker},
		{Severity: "low", File: "c.go", Problem: "Typo in comment", Evidence: rawMarker},
	}
}

func newRunner(t *testing.T, f prFixture, rec *slackRecorder, review Reviewer) (*ReviewRunner, *[]string) {
	t.Helper()
	var mu sync.Mutex
	logs := &[]string{}
	return &ReviewRunner{
		Repos:   f.repos(),
		Allow:   testAllow(),
		Scratch: f.scratch,
		Review:  review,
		Poster:  rec.poster(),
		Secrets: []string{plantedToken},
		Logf: func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			*logs = append(*logs, fmt.Sprintf(format, args...))
		},
	}, logs
}

func TestReviewRunner_PostsCountsAndTitlesOnly(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	var reviewedDir string
	r, _ := newRunner(t, f, rec, func(ctx context.Context, dir string) ([]reconcile.Finding, error) {
		reviewedDir = dir
		b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
		require.NoError(t, err)
		require.Equal(t, "pr head\n", string(b), "the reviewer must run in the PR head worktree")
		return stubFindings(dir), nil
	})

	job, ok := r.Dispatch(testMention("<@B1> review https://github.com/acme/widget/pull/7"))
	require.True(t, ok)
	assert.Equal(t, "acme/widget#7", job.Key)
	require.NoError(t, job.Run(context.Background()))

	texts := rec.texts()
	require.Len(t, texts, 1)
	text := texts[0]
	assert.Contains(t, text, "acme/widget#7")
	assert.Contains(t, text, "4 findings")
	assert.Contains(t, text, "1 CRITICAL")
	assert.Contains(t, text, "2 HIGH")
	assert.Contains(t, text, "1 LOW")
	assert.NotContains(t, text, "MEDIUM", "a zero count is left out")
	assert.Contains(t, text, "SQL injection in query builder")
	assert.Contains(t, text, "Missing nil check")
	assert.Contains(t, text, "Typo in comment")
	assert.Contains(t, text, "Token [redacted] logged at")

	require.NotEmpty(t, reviewedDir)
	assert.NotContains(t, text, reviewedDir, "worktree path leaked")
	assert.NotContains(t, text, f.checkout, "checkout path leaked")
	assert.NotContains(t, text, f.scratch, "scratch path leaked")
	assert.NotContains(t, text, "auth.go", "file path leaked")
	assert.NotContains(t, text, plantedToken, "token leaked")
	assert.NotContains(t, text, rawMarker, "raw model output leaked")
	assert.NotContains(t, text, "full model reasoning", "raw model output leaked")

	// CRITICAL is listed before HIGH, HIGH before LOW.
	assert.Less(t, strings.Index(text, "SQL injection"), strings.Index(text, "Missing nil check"))
	assert.Less(t, strings.Index(text, "Missing nil check"), strings.Index(text, "Typo in comment"))
	assertNothingLeft(t, f)
}

func TestReviewRunner_RepliesInTheMentionsThread(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	r, _ := newRunner(t, f, rec, func(context.Context, string) ([]reconcile.Finding, error) { return nil, nil })

	m := testMention("<@B1> review https://github.com/acme/widget/pull/7")
	m.ThreadTS = "1699999999.000001"
	job, ok := r.Dispatch(m)
	require.True(t, ok)
	require.NoError(t, job.Run(context.Background()))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.posts, 1)
	assert.Equal(t, "C1", rec.posts[0]["channel"])
	assert.Equal(t, "1699999999.000001", rec.posts[0]["thread_ts"])
	assert.Contains(t, rec.posts[0]["text"], "no findings")
}

func TestReviewRunner_ThreadDefaultsToMentionTS(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	r, _ := newRunner(t, f, rec, func(context.Context, string) ([]reconcile.Finding, error) { return nil, nil })

	job, ok := r.Dispatch(testMention("<@B1> review https://github.com/acme/widget/pull/7"))
	require.True(t, ok)
	require.NoError(t, job.Run(context.Background()))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	assert.Equal(t, "1700000000.000100", rec.posts[0]["thread_ts"])
}

func TestReviewRunner_ReviewErrorPostsGenericFailure(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	r, logs := newRunner(t, f, rec, func(_ context.Context, dir string) ([]reconcile.Finding, error) {
		return nil, errors.New(rawMarker + " model said " + plantedToken + " in " + dir)
	})

	job, ok := r.Dispatch(testMention("<@B1> review https://github.com/acme/widget/pull/7"))
	require.True(t, ok)
	err := job.Run(context.Background())
	require.Error(t, err, "the failure is returned so the job runner logs it")

	texts := rec.texts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], "acme/widget#7")
	assert.Contains(t, texts[0], "failed")
	assert.NotContains(t, texts[0], rawMarker)
	assert.NotContains(t, texts[0], plantedToken)
	assert.NotContains(t, texts[0], f.scratch)
	assert.Empty(t, *logs)
	assertNothingLeft(t, f)
}

func TestReviewRunner_TimeoutPostsTimedOutReply(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	r, _ := newRunner(t, f, rec, func(ctx context.Context, _ string) ([]reconcile.Finding, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	job, ok := r.Dispatch(testMention("<@B1> review https://github.com/acme/widget/pull/7"))
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := job.Run(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	texts := rec.texts()
	require.Len(t, texts, 1, "the reply is posted even though the job's context is done")
	assert.Contains(t, texts[0], "timed out")
	assertNothingLeft(t, f)
}

func TestReviewRunner_SlackErrorIsLoggedAndDaemonKeepsRunning(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	rec.reply = `{"ok":false,"error":"invalid_auth"}`
	r, _ := newRunner(t, f, rec, func(context.Context, string) ([]reconcile.Finding, error) {
		return []reconcile.Finding{{Severity: "LOW", Problem: "x"}}, nil
	})

	var mu sync.Mutex
	var logs []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	srv := NewServer(Options{SlackSigningSecret: testSecret, Dispatch: r.Dispatch, Logf: logf})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body := `{"type":"event_callback","team_id":"T1","event_id":"EvA","event":{"type":"app_mention","user":"U1","channel":"C1","text":"<@B1> review https://github.com/acme/widget/pull/7","ts":"1.2"}}`
	code, _ := do(t, signedRequest(t, ts.URL, body, time.Now()))
	require.Equal(t, http.StatusOK, code)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range logs {
			if strings.Contains(l, "invalid_auth") {
				return true
			}
		}
		return false
	}, 10*time.Second, 10*time.Millisecond, "the Slack error is logged")
	mu.Lock()
	for _, l := range logs {
		assert.NotContains(t, l, plantedToken, "a log line carries the bot token")
	}
	mu.Unlock()

	// The daemon still serves and runs the next job.
	rec.mu.Lock()
	rec.reply = `{"ok":true}`
	rec.mu.Unlock()
	body2 := strings.Replace(body, "EvA", "EvB", 1)
	require.Eventually(t, func() bool {
		code, _ := do(t, signedRequest(t, ts.URL, body2, time.Now()))
		return code == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return len(rec.texts()) == 2 }, 10*time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
	assertNothingLeft(t, f)
}

func TestReviewRunner_RefusedMentionRunsNothingAndLogs(t *testing.T) {
	f := newPRFixture(t)
	rec := newSlackRecorder(t)
	called := false
	r, logs := newRunner(t, f, rec, func(context.Context, string) ([]reconcile.Finding, error) {
		called = true
		return nil, nil
	})

	for _, m := range []*slack.AppMention{
		testMention("<@B1> review https://github.com/other/repo/pull/7"),
		testMention("<@B1> hello"),
		{EventID: "Ev2", TeamID: "T1", Channel: "C1", User: "U-unlisted", Text: "<@B1> review https://github.com/acme/widget/pull/7", TS: "1.2"},
	} {
		_, ok := r.Dispatch(m)
		assert.False(t, ok)
	}
	assert.False(t, called)
	assert.Empty(t, rec.texts(), "a refused mention gets no reply")
	assert.Len(t, *logs, 3)
}

func TestSummarize_CapsTitlesAndScrubs(t *testing.T) {
	var fs []reconcile.Finding
	for i := range maxReplyTitles + 3 {
		fs = append(fs, reconcile.Finding{Severity: "MEDIUM", Problem: fmt.Sprintf("Problem %02d", i)})
	}
	fs = append(fs,
		reconcile.Finding{Severity: "weird", Problem: "   "},
		reconcile.Finding{Severity: "LOW", Problem: strings.Repeat("long ", 100)},
		reconcile.Finding{Severity: "LOW", Problem: "uses <!channel> & `C:\\Users\\me\\x.go` and https://evil.example/a?t=1 and ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
	)
	text := summarize("acme/widget#7", fs, nil)

	assert.Contains(t, text, fmt.Sprintf("%d findings", len(fs)))
	assert.Contains(t, text, fmt.Sprintf("%d MEDIUM", maxReplyTitles+3))
	assert.Contains(t, text, "1 OTHER")
	assert.Contains(t, text, "Problem 00")
	assert.NotContains(t, text, fmt.Sprintf("Problem %02d", maxReplyTitles+2))
	assert.Contains(t, text, "more not shown")
	assert.NotContains(t, text, "Problem 99")

	lines := strings.Split(text, "\n")
	assert.LessOrEqual(t, len(lines), maxReplyTitles+3)

	// Every listed title fits the cap.
	for _, l := range lines {
		assert.LessOrEqual(t, len([]rune(l)), maxTitleRunes+20, "line too long: %q", l)
	}
}

func TestFindingTitle(t *testing.T) {
	r := newScrubber("/srv/wt/abc", []string{plantedToken})
	cases := map[string]string{
		"Simple problem":                            "Simple problem",
		"First line\nsecond line":                   "First line",
		"   ":                                       "(untitled finding)",
		"Leak " + plantedToken + " here":            "Leak [redacted] here",
		"Bad /srv/wt/abc/x.go:3 path":               "Bad [path] path",
		"Bad internal/db/q.go path":                 "Bad [path] path",
		`Bad C:\x\y.go path`:                        "Bad [path] path",
		"Ping <!channel> & <@U1>":                   "Ping &lt;!channel&gt; &amp; &lt;@U1&gt;",
		"See https://evil.example/x":                "See [path]",
		"Key ghp_abcdefghijklmnopqrstuvwxyz0123456": "Key [redacted]",
		"Bearer abc.def":                            "Bearer [redacted]",
		"Slack xoxp-1-2-3abc":                       "Slack [redacted]",
		"Key sk-proj-abcdef":                        "Key [redacted]",
	}
	for in, want := range cases {
		assert.Equal(t, want, r.title(in), "input %q", in)
	}
	long := r.title(strings.Repeat("x", 500))
	assert.Equal(t, maxTitleRunes, len([]rune(long)))
	assert.True(t, strings.HasSuffix(long, "…"))
}

func TestFailureText_SaysCancelledOnShutdown(t *testing.T) {
	text := failureText("acme/widget#7", fmt.Errorf("wrapped: %w", context.Canceled))
	assert.Contains(t, text, "was cancelled")
}

func TestReviewRunner_UnconfiguredRunsNothing(t *testing.T) {
	var logs []string
	r := &ReviewRunner{Allow: testAllow(), Logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }}
	_, ok := r.Dispatch(testMention("<@B1> review https://github.com/acme/widget/pull/7"))
	assert.False(t, ok)
	assert.Len(t, logs, 1)
	_, ok = (&ReviewRunner{}).Dispatch(nil)
	assert.False(t, ok, "a nil mention with no Logf neither runs nor panics")
}
