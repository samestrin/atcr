package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const highFinding = `[{"severity":"HIGH","file":"a.go","line":7,"problem":"boom","fix":"do x","category":"security","est_minutes":10,"evidence":"e; fix by opus","reviewers":["greta"],"confidence":"HIGH"}]`
const mediumFinding = `[{"severity":"MEDIUM","file":"b.go","line":3,"problem":"meh","fix":"f","category":"perf","est_minutes":5,"evidence":"e","reviewers":["greta"],"confidence":"MEDIUM"}]`
const twoFindings = `[{"severity":"HIGH","file":"a.go","line":7,"problem":"boom","fix":"do x","category":"security","est_minutes":10,"evidence":"e; fix by opus","reviewers":["greta"],"confidence":"HIGH"},{"severity":"MEDIUM","file":"b.go","line":3,"problem":"meh","fix":"f","category":"perf","est_minutes":5,"evidence":"e","reviewers":["greta"],"confidence":"MEDIUM"}]`

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
		paths[r.Method+" "+r.URL.Path]++
		mu.Unlock()
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(srv.Close)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", srv.URL, "--inline-comments", "--pr", "5", "2026-06-10_i")

	// HIGH finding with no --fail-on: neutral conclusion → exit 0, review posts.
	require.Equal(t, 0, code)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, paths["POST /repos/samestrin/atcr/check-runs"], "check run still posts")
	assert.Equal(t, 1, paths["POST /repos/samestrin/atcr/pulls/5/reviews"], "batched review posts")
	assert.Equal(t, 0, paths["POST /repos/samestrin/atcr/pulls/5/comments"], "no per-comment POSTs")
}

func TestGithubCmd_InlineCommentsBeforeCheckRunAndReflectedInOutput(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_order", twoFindings)

	var mu sync.Mutex
	var order []string
	var checkBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "check-runs") {
			_ = json.Unmarshal(raw, &checkBody)
		}
		mu.Unlock()

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(srv.Close)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", srv.URL, "--inline-comments", "--pr", "5", "2026-06-10_order")

	require.Equal(t, 0, code)
	mu.Lock()
	defer mu.Unlock()

	// Review (list + batch) must arrive before the check run.
	checkIdx := -1
	reviewIdx := -1
	for i, entry := range order {
		if strings.Contains(entry, "check-runs") {
			checkIdx = i
		}
		if strings.Contains(entry, "/pulls/5/reviews") && reviewIdx < 0 {
			reviewIdx = i
		}
	}
	require.Greater(t, reviewIdx, -1, "batch review must be requested")
	require.Less(t, reviewIdx, checkIdx, "inline review must be posted before the check run")

	text, _ := checkBody["output"].(map[string]any)["text"].(string)
	assert.Contains(t, text, "2 posted")
	assert.Contains(t, text, "0 already present")
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

func TestGithubCmd_WritesGithubOutput(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_go", highFinding)
	url, _ := captureGitHub(t)

	githubOutput := filepath.Join(t.TempDir(), "github-output")
	t.Setenv("GITHUB_OUTPUT", githubOutput)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr", "--sha", "abc123", "--token", "tok",
		"--api-url", url, "--fail-on", "high", "2026-06-10_go")

	require.Equal(t, 1, code)
	content, err := os.ReadFile(githubOutput)
	require.NoError(t, err)
	assert.Contains(t, string(content), "conclusion=failure")
	assert.Contains(t, string(content), "findings=1")
}

func TestPostInlineComments_UsesBatchedAPI(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.Method+" "+r.URL.Path]++
		mu.Unlock()
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	findings := []reconcile.JSONFinding{
		{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"},
		{File: "b.go", Line: 2, Problem: "p2", Fix: "f2"},
	}

	posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
	require.NoError(t, err)
	assert.Equal(t, 2, posted)
	assert.Equal(t, 0, deduped)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, paths["GET /repos/owner/repo/pulls/1/comments"], "expected one list call")
	assert.Equal(t, 1, paths["POST /repos/owner/repo/pulls/1/reviews"], "expected one batch review")
	assert.Equal(t, 0, paths["POST /repos/owner/repo/pulls/1/comments"], "expected no per-comment POSTs")
}

func TestPostInlineComments_DedupsExistingATCRComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"path":"a.go","line":1,"body":"ATCR found: p1. Fix: f1."}]`))
			return
		}
		// POST /reviews — verify only b.go:2 is in the batch
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		comments, _ := body["comments"].([]any)
		if len(comments) != 1 {
			http.Error(w, "expected exactly 1 comment after dedup", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":2}`))
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	findings := []reconcile.JSONFinding{
		{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"},
		{File: "b.go", Line: 2, Problem: "p2", Fix: "f2"},
	}

	posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
	require.NoError(t, err)
	assert.Equal(t, 1, posted, "only b.go:2 should post — a.go:1 already exists")
	assert.Equal(t, 1, deduped, "a.go:1 should be counted as deduped")
}

// TestPostInlineComments_422IsNonFatal pins that a 422 from CreatePRReview (all
// comments off-diff) is treated as a non-fatal warning rather than propagating as
// exitFailure — a single off-diff finding must not fail the atcr github step.
func TestPostInlineComments_422IsNonFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())

	client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	findings := []reconcile.JSONFinding{{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"}}

	posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
	require.NoError(t, err, "422 from CreatePRReview must not propagate as exitFailure")
	assert.Equal(t, 0, posted)
	assert.Equal(t, 0, deduped)
	assert.Contains(t, stderr.String(), "422")
}

// TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch pins the
// GitHub-Enterprise mitigation: when the batched /reviews endpoint is
// unavailable (404 Not Found or 405 Method Not Allowed — older GHE versions),
// postInlineComments falls back to posting each comment individually via
// CreateReviewComment rather than failing the step. Other batch errors keep
// propagating as exitFailure, and 422 keeps its non-fatal off-diff behavior
// (TestPostInlineComments_422IsNonFatal), both unchanged.
func TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var mu sync.Mutex
			paths := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths[r.Method+" "+r.URL.Path]++
				mu.Unlock()
				switch {
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments"):
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews"):
					w.WriteHeader(status) // batch endpoint unsupported on this host
					_, _ = w.Write([]byte(`{"message":"unsupported"}`))
				default: // per-comment POST /comments
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":1}`))
				}
			}))
			defer srv.Close()

			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())

			client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
			findings := []reconcile.JSONFinding{
				{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"},
				{File: "b.go", Line: 2, Problem: "p2", Fix: "f2"},
			}

			posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
			require.NoError(t, err, "404/405 from the batch endpoint must trigger fallback, not fail the step")
			assert.Equal(t, 2, posted, "both comments posted via the per-comment fallback")
			assert.Equal(t, 0, deduped)

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, 1, paths["POST /repos/owner/repo/pulls/1/reviews"], "batch attempted exactly once")
			assert.Equal(t, 2, paths["POST /repos/owner/repo/pulls/1/comments"], "fell back to one POST per comment")
		})
	}
}

// TestPostInlineComments_FallbackPerComment422IsSkipped pins that, once the
// fallback path is taken, a per-comment 422 (off-diff for that one line) is a
// non-fatal skip — mirroring the batch path's 422 handling — so a single
// off-diff finding does not fail the whole step.
func TestPostInlineComments_FallbackPerComment422IsSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews"):
			w.WriteHeader(http.StatusNotFound) // force fallback
		default: // per-comment POST — every comment off-diff
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"off-diff"}`))
		}
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())

	client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	findings := []reconcile.JSONFinding{
		{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"},
		{File: "b.go", Line: 2, Problem: "p2", Fix: "f2"},
	}

	posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
	require.NoError(t, err, "per-comment 422 during fallback is a non-fatal off-diff skip")
	assert.Equal(t, 0, posted, "both off-diff comments skipped")
	assert.Equal(t, 0, deduped)
	assert.Contains(t, stderr.String(), "422")
}

// TestPostInlineComments_FallbackPerCommentHardErrorPropagates pins that a
// non-422 failure during the per-comment fallback (e.g. 403) aborts with
// exitFailure rather than being silently swallowed.
func TestPostInlineComments_FallbackPerCommentHardErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/comments"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews"):
			w.WriteHeader(http.StatusNotFound) // force fallback
		default: // per-comment POST — hard failure (non-retriable, non-422)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
		}
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	findings := []reconcile.JSONFinding{{File: "a.go", Line: 1, Problem: "p1", Fix: "f1"}}

	_, _, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
	require.Error(t, err, "a non-422 per-comment failure during fallback must propagate as exitFailure")
}

// TestReadReconciledFindings_MissingPreservesErrNotExist verifies that a missing
// findings.json preserves os.ErrNotExist through the error chain so callers can
// distinguish absent data (usage error, exit 2) from present-but-malformed data
// (operational error, exit 1).
func TestReadReconciledFindings_MissingPreservesErrNotExist(t *testing.T) {
	dir := t.TempDir() // no reconciled/findings.json inside
	_, err := readReconciledFindings(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist),
		"missing findings.json must preserve os.ErrNotExist so callers can exit 2 for absent data")
}
