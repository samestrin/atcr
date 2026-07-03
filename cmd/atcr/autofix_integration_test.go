//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/require"
)

// Phase 6 (Sprint 17.0) end-to-end integration tests. Unlike the unit tests in
// autofix_test.go — which drive runAutoFix through the call-recording fakeGitHub
// — these exercise the FULL auto-fix flow against a real *ghaction.Client pointed
// at an httptest GitHub stub, over real HTTP. They prove the sprint's central
// sequencing guarantee end-to-end: no GitHub-mutating call is reachable before
// local validation has passed. Gated behind the `integration` build tag so CI
// runs them intentionally (`go test -tags integration ./...`).

// ghStub is an httptest handler that routes the GitHub Git Data + Pulls API
// sequence on METHOD+path (order-independent, exactly as the real API is keyed)
// and records every request it receives. Its request log is the load-bearing
// assertion surface: the zero-HTTP-on-failure guard reads count(); the happy
// path reads saw() to confirm the full branch→commit→PR sequence fired.
type ghStub struct {
	mu       sync.Mutex
	requests []string // "METHOD /path" per received request, in arrival order
}

func (s *ghStub) record(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, method+" "+path)
}

// count returns how many HTTP requests the stub has received.
func (s *ghStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// saw reports whether any recorded request line contains sub.
func (s *ghStub) saw(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.requests {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// newGHStub stands up an httptest server that satisfies the full happy-path
// Git Data API sequence CreateBranch → CreateCommit (get-parent → blob → tree →
// commit → patch-ref) → FindOpenPullRequest → CreatePullRequest. It records
// every request and fails the test on any route it does not recognize, so a
// mis-routed call surfaces loudly instead of false-greening. httptest binds
// 127.0.0.1, a loopback host the client accepts over plain http.
func newGHStub(t *testing.T) (*httptest.Server, *ghStub) {
	t.Helper()
	stub := &ghStub{}
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.record(r.Method, r.URL.Path)
		switch {
		// FindOpenPullRequest: no existing PR -> empty list -> create path.
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls":
			writeJSON(w, http.StatusOK, []map[string]any{})
		// CreatePullRequest.
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/pulls":
			writeJSON(w, http.StatusCreated, map[string]any{"number": 42})
		// CreateBranch (create ref).
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/refs":
			writeJSON(w, http.StatusCreated, map[string]any{"ref": "refs/heads/x"})
		// CreateCommit ref-advance (PATCH refs/heads/<branch>).
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/o/r/git/refs/heads/"):
			writeJSON(w, http.StatusOK, map[string]any{"ref": "refs/heads/x"})
		// CreateCommit step 1: resolve parent commit's base tree.
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/git/commits/"):
			writeJSON(w, http.StatusOK, map[string]any{"tree": map[string]any{"sha": "basetree"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/blobs":
			writeJSON(w, http.StatusCreated, map[string]any{"sha": "blobsha"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/trees":
			writeJSON(w, http.StatusCreated, map[string]any{"sha": "treesha"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/commits":
			writeJSON(w, http.StatusCreated, map[string]any{"sha": "commitsha"})
		default:
			t.Errorf("unexpected GitHub call routed to stub: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, stub
}

// stubHeadSHA substitutes the git-shelling resolveHeadSHAFn with a hermetic
// constant for the duration of the test, so orchestrateAutoFix resolves a base
// commit without a real git repo. Restored on cleanup.
func stubHeadSHA(t *testing.T, sha string) {
	t.Helper()
	orig := resolveHeadSHAFn
	resolveHeadSHAFn = func(context.Context, string) (string, error) { return sha, nil }
	t.Cleanup(func() { resolveHeadSHAFn = orig })
}

// TestAutoFixIntegration_HappyPathOpensPR (Task 6.1, validation-PASS branch):
// drives the real `--auto-fix` entry point orchestrateAutoFix end-to-end against
// the GitHub stub. A reconciled finding carrying a fix diff is applied, local
// validation passes, and the full branch→commit→PR sequence fires over real HTTP,
// producing a PR against the stub. The applied content stays on disk.
func TestAutoFixIntegration_HappyPathOpensPR(t *testing.T) {
	isolate(t) // fresh cwd + isolated HOME; verifyFixture writes under ./.atcr

	applyDir := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(applyDir, rel), []byte("old\n"), 0o644))

	srv, stub := newGHStub(t)
	stubHeadSHA(t, "basecommitsha")

	id := verifyFixture(t, "af-happy", []reconcile.JSONFinding{
		{Severity: "HIGH", File: rel, Line: 1, Problem: "x", Confidence: "HIGH", Fix: diffFor(rel)},
	})
	reviewDir := filepath.Join(".atcr", "reviews", id)

	be := autoFixBackend{
		applyTarget:     applyDir,
		validateArgv:    []string{"true"}, // validation passes
		validateTimeout: 5 * time.Second,
		owner:           "o", repo: "r", token: "tok",
		apiURL: srv.URL,
	}

	var out strings.Builder
	err := orchestrateAutoFix(context.Background(), &out, be, reviewDir, "", "main")
	require.NoError(t, err)

	// A PR was opened against the stub.
	require.Contains(t, out.String(), "opened pull request #42")
	require.True(t, stub.saw("POST /repos/o/r/git/refs"), "branch must be created")
	require.True(t, stub.saw("POST /repos/o/r/git/commits"), "commit must be created")
	require.True(t, stub.saw("PATCH /repos/o/r/git/refs/heads/"), "branch ref must be advanced")
	require.True(t, stub.saw("GET /repos/o/r/pulls"), "existence check (create-vs-update) must run before opening a PR")
	require.True(t, stub.saw("POST /repos/o/r/pulls"), "PR must be opened")

	// The validated fix stays applied to the working tree.
	got, rerr := os.ReadFile(filepath.Join(applyDir, rel))
	require.NoError(t, rerr)
	require.Equal(t, "new\n", string(got))
}

// TestAutoFixIntegration_ValidationFailRevertsThroughEntryPoint (Task 6.1,
// validation-FAIL branch): the same real entry point, but validation fails. The
// working tree is reverted to its pre-patch content and the GitHub stub receives
// ZERO requests — proving no remote mutation is reachable before validation
// passes, exercised end-to-end through orchestrateAutoFix.
func TestAutoFixIntegration_ValidationFailRevertsThroughEntryPoint(t *testing.T) {
	isolate(t)

	applyDir := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(applyDir, rel), []byte("old\n"), 0o644))

	srv, stub := newGHStub(t)
	stubHeadSHA(t, "basecommitsha")

	id := verifyFixture(t, "af-fail", []reconcile.JSONFinding{
		{Severity: "HIGH", File: rel, Line: 1, Problem: "x", Confidence: "HIGH", Fix: diffFor(rel)},
	})
	reviewDir := filepath.Join(".atcr", "reviews", id)

	be := autoFixBackend{
		applyTarget:     applyDir,
		validateArgv:    []string{"false"}, // validation fails
		validateTimeout: 5 * time.Second,
		owner:           "o", repo: "r", token: "tok",
		apiURL: srv.URL,
	}

	err := orchestrateAutoFix(context.Background(), io.Discard, be, reviewDir, "", "main")
	require.Error(t, err)
	// Pin the intended non-zero-exit validation-failure branch specifically (not
	// the cannot-start branch, which also says "reverted"), so the test can't
	// green while exercising the wrong path.
	require.Contains(t, err.Error(), "local validation failed (exit")
	require.Equal(t, 0, stub.count(), "no GitHub call may fire when validation fails")

	got, rerr := os.ReadFile(filepath.Join(applyDir, rel))
	require.NoError(t, rerr)
	require.Equal(t, "old\n", string(got), "working tree must be reverted to pre-patch content")
}

// TestAutoFixIntegration_ZeroHTTPOnValidationFailure (Task 6.2): an INDEPENDENT
// cross-check of the zero-HTTP guarantee via a different route — runAutoFix
// driven directly with a real *ghaction.Client (injected entries), rather than
// through orchestrateAutoFix. Two independent paths reaching the same assertion
// guard against a false-green from httptest mis-routing in either one. When
// validation fails, the stub's request counter must be exactly 0 and the tree
// must be restored.
func TestAutoFixIntegration_ZeroHTTPOnValidationFailure(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	srv, stub := newGHStub(t)
	gh := &ghaction.Client{APIURL: srv.URL, Token: "tok"}

	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"false"}, // validation fails
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok", apiURL: srv.URL,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "basecommitsha", Base: "main", Branch: "atcr/auto-fix",
		Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err)
	require.Equal(t, 0, stub.count(), "the GitHub stub must receive zero requests on the validation-failure path")

	got, rerr := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, rerr)
	require.Equal(t, "old\n", string(got), "the working tree must be reverted")
}

// TestAutoFixIntegration_ValidationPassCreatesPRViaRealClient (Task 6.1 support):
// the validation-PASS branch through runAutoFix with a real *ghaction.Client,
// confirming the real HTTP client (not the fake) drives the full sequence and
// decodes the stub's PR number. Complements the orchestrateAutoFix happy path.
func TestAutoFixIntegration_ValidationPassCreatesPRViaRealClient(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	srv, stub := newGHStub(t)
	gh := &ghaction.Client{APIURL: srv.URL, Token: "tok"}

	var out strings.Builder
	err := runAutoFix(context.Background(), &out, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // validation passes
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok", apiURL: srv.URL,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "basecommitsha", Base: "main", Branch: "atcr/auto-fix",
		Title: "fix", Body: "b", Message: "m",
	})
	require.NoError(t, err)
	require.Contains(t, out.String(), "opened pull request #42")
	require.True(t, stub.saw("POST /repos/o/r/pulls"), "PR must be opened over real HTTP")
	require.Greater(t, stub.count(), 4, "the full Git Data API sequence must fire")

	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "new\n", string(got), "validated content stays applied")
}
