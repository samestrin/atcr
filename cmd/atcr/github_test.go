package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const highFinding = `[{"severity":"HIGH","file":"a.go","line":7,"problem":"boom","fix":"do x","category":"security","est_minutes":10,"evidence":"e; fix by opus","reviewers":["greta"],"confidence":"HIGH"}]`
const mediumFinding = `[{"severity":"MEDIUM","file":"b.go","line":3,"problem":"meh","fix":"f","category":"perf","est_minutes":5,"evidence":"e","reviewers":["greta"],"confidence":"MEDIUM"}]`

// captureGitHub starts a fake GitHub REST server that records the body of the
// check-run POST it receives.
func captureGitHub(t *testing.T) (url string, body func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(raw, &got)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() map[string]any { mu.Lock(); defer mu.Unlock(); return got }
}

func TestGithubCmd_GateFailurePostsFailedCheck(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_g", highFinding)
	url, body := captureGitHub(t)

	code, out := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", url, "--fail-on", "high", "2026-06-10_g")

	require.Equal(t, 1, code, "a surviving HIGH finding must fail the gate (exit 1)")
	assert.Equal(t, "failure", body()["conclusion"])
	assert.Equal(t, "abc123", body()["head_sha"])
	_ = out
}

func TestGithubCmd_BelowThresholdPasses(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_m", mediumFinding)
	url, body := captureGitHub(t)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", url, "--fail-on", "high", "2026-06-10_m")

	require.Equal(t, 0, code, "only a MEDIUM finding under --fail-on high passes")
	assert.Equal(t, "success", body()["conclusion"])
}

func TestGithubCmd_InlineCommentsPostReviewComments(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_i", highFinding)

	var mu sync.Mutex
	paths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(srv.Close)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", srv.URL, "--inline-comments", "--pr", "5", "2026-06-10_i")

	// HIGH finding with no --fail-on: neutral conclusion → exit 0, but a comment posts.
	require.Equal(t, 0, code)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, paths["/repos/samestrin/atcr/check-runs"], "check run still posts")
	assert.Equal(t, 1, paths["/repos/samestrin/atcr/pulls/5/comments"], "inline comment posts")
}

func TestGithubCmd_InlineCommentsRequirePR(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_np", highFinding)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc", "--token", "t",
		"--inline-comments", "2026-06-10_np")
	require.Equal(t, 2, code, "--inline-comments without --pr is a usage error")
}

func TestGithubCmd_InvalidFailOnIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_x", highFinding)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc", "--token", "t",
		"--fail-on", "bogus", "2026-06-10_x")
	require.Equal(t, 2, code)
}

func TestGithubCmd_MissingTokenIsUsageError(t *testing.T) {
	isolate(t)
	t.Setenv("GITHUB_TOKEN", "")
	fixtureReconciled(t, "2026-06-10_t", highFinding)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc", "2026-06-10_t")
	require.Equal(t, 2, code)
}
