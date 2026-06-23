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

// TestGithubCmd_EndToEndFlow is the AC5 demonstration: a reconciled review with
// a fix-bearing HIGH finding is rendered onto a pull request as both a failing
// check run and an inline comment, in a single `atcr github` invocation, against
// a fake GitHub REST API (net/http/httptest) — the same shape the composite
// action.yml drives in CI, with no live network calls.
func TestGithubCmd_EndToEndFlow(t *testing.T) {
	isolate(t)

	// A realistic reconciled stream: one blocking HIGH finding carrying an
	// executor-generated fix and the "fix by <name>" attribution token, plus a
	// sub-threshold MEDIUM that must not fail the gate.
	const reconciled = `[
		{"severity":"HIGH","file":"internal/auth/token.go","line":42,"problem":"JWT signature not verified before claims are read","fix":"Call jwt.Verify before decoding claims","category":"security","est_minutes":20,"evidence":"bruce: token, _ := jwt.Parse(raw); fix by opus","reviewers":["bruce","greta"],"confidence":"HIGH"},
		{"severity":"MEDIUM","file":"internal/store/cache.go","line":88,"problem":"Unbounded map grows without eviction","fix":"Add an LRU bound","category":"performance","est_minutes":45,"evidence":"otto: c.entries[k] = v","reviewers":["otto"],"confidence":"MEDIUM"}
	]`
	fixtureReconciled(t, "2026-06-22_e2e", reconciled)

	var mu sync.Mutex
	var checkBody map[string]any
	var reviewBody map[string]any
	var reviewComments []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		switch {
		case r.URL.Path == "/repos/samestrin/atcr/check-runs":
			_ = json.Unmarshal(raw, &checkBody)
		case r.URL.Path == "/repos/samestrin/atcr/pulls/12/comments" && r.Method == http.MethodGet:
			// List existing comments for dedup — return empty.
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		case r.URL.Path == "/repos/samestrin/atcr/pulls/12/reviews":
			// Batch review — capture the full body and extract the comments array.
			_ = json.Unmarshal(raw, &reviewBody)
			if cs, ok := reviewBody["comments"].([]any); ok {
				for _, c := range cs {
					if cm, ok := c.(map[string]any); ok {
						reviewComments = append(reviewComments, cm)
					}
				}
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(srv.Close)

	code, _ := execCmdCapture(t, "github",
		"--repo", "samestrin/atcr",
		"--sha", "headsha42",
		"--token", "ghtoken",
		"--api-url", srv.URL,
		"--fail-on", "high",
		"--inline-comments", "--pr", "12",
		"2026-06-22_e2e")

	// The HIGH finding survives the gate → exit 1, merge blocked.
	require.Equal(t, 1, code, "a surviving HIGH must fail the merge gate")

	mu.Lock()
	defer mu.Unlock()

	// Check run: failure conclusion, reported against the PR head SHA, summary
	// reflects the blocking count.
	require.NotNil(t, checkBody, "a check run must be posted")
	assert.Equal(t, "completed", checkBody["status"])
	assert.Equal(t, "failure", checkBody["conclusion"])
	assert.Equal(t, "headsha42", checkBody["head_sha"])
	output, _ := checkBody["output"].(map[string]any)
	require.NotNil(t, output)
	assert.Contains(t, output["text"], "internal/auth/token.go:42")

	// Both findings anchor to changed lines, so two inline comments post as a
	// single batched review anchored to the PR head SHA.
	require.NotNil(t, reviewBody, "a batch review must be posted")
	assert.Equal(t, "headsha42", reviewBody["commit_id"], "review must be anchored to the head SHA")
	require.Len(t, reviewComments, 2, "one inline comment per anchorable finding")

	// Locate the HIGH finding's comment: anchored at FILE:LINE, AC3-formatted
	// body with PROBLEM, FIX, and executor attribution parsed from EVIDENCE.
	var high map[string]any
	for _, c := range reviewComments {
		if c["path"] == "internal/auth/token.go" {
			high = c
		}
	}
	require.NotNil(t, high, "the HIGH finding must produce an inline comment")
	assert.Equal(t, float64(42), high["line"])
	body, _ := high["body"].(string)
	assert.Contains(t, body, "ATCR found: JWT signature not verified before claims are read")
	assert.Contains(t, body, "Fix: Call jwt.Verify before decoding claims")
	assert.Contains(t, body, "Suggested by: opus")
}
