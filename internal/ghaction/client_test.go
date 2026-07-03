package ghaction

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCheckRunRejectsPlaintextAPIURL(t *testing.T) {
	c := &Client{APIURL: "http://example.com/api/v3", Token: "tok"}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insecure API URL")
}

func TestCreateCheckRunAllowsLoopbackHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
}

func TestCreateCheckRun(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "samestrin", "atcr", CheckRunRequest{
		Name:       "atcr",
		HeadSHA:    "deadbeef",
		Conclusion: "failure",
		Output:     CheckOutput{Title: "atcr — 1 finding", Summary: "s", Text: "t"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/repos/samestrin/atcr/check-runs", gotPath)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Equal(t, "completed", gotBody["status"])
	assert.Equal(t, "failure", gotBody["conclusion"])
	assert.Equal(t, "deadbeef", gotBody["head_sha"])
}

func TestPostSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(gotUA, "atcr"), "User-Agent must identify the app, got: %q", gotUA)
}

func TestClientTimeoutConfigurable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", Timeout: 50 * time.Millisecond}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestPostRetriesTransientFailures(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3)
}

func TestPostRetriesExhausted(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestCreateCheckRunWithID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	id, err := c.CreateCheckRunWithID(context.Background(), "o", "r", CheckRunRequest{
		Name: "atcr", HeadSHA: "sha", Conclusion: "success",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
}

func TestCreateCheckRunAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "not accessible")
}

// TestPostDo_422ReturnsAPIError pins that postDo wraps non-retriable HTTP errors as
// *APIError so callers can inspect StatusCode (e.g. to treat 422 off-diff as expected).
func TestPostDo_422ReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.post(context.Background(), "/repos/o/r/test", map[string]string{"k": "v"})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "non-retriable error must be *APIError so callers can inspect the status code")
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Validation Failed")
}

// TestClient_RedactsTokenFromErrorMessages pins that a GitHub error body echoing
// the Authorization header (Bearer <token>) cannot leak the token through either
// the postDo *APIError message or the get wrapped error. Mirrors
// llmclient.redactErrorSnippet's defense.
func TestClient_RedactsTokenFromErrorMessages(t *testing.T) {
	const token = "ghp_SECRETTOKEN1234567890abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// Simulate GitHub/a proxy echoing the Authorization header into the error body.
		_, _ = w.Write([]byte(`{"message":"bad credentials for ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: token, HTTPClient: srv.Client()}

	postErr := c.post(context.Background(), "/repos/o/r/test", map[string]string{"k": "v"})
	require.Error(t, postErr)
	assert.NotContains(t, postErr.Error(), token, "token must not leak via postDo APIError message")
	assert.Contains(t, postErr.Error(), "[redacted]", "Bearer token must be redacted in postDo message")

	getErr := c.get(context.Background(), "/repos/o/r/x", nil)
	require.Error(t, getErr)
	assert.NotContains(t, getErr.Error(), token, "token must not leak via get error message")
	assert.Contains(t, getErr.Error(), "[redacted]", "Bearer token must be redacted in get message")
}

func TestSleepCtxInterruptedByCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepCtx(ctx, time.Hour)
	elapsed := time.Since(start)

	require.Error(t, err, "sleepCtx must return the context error when cancelled mid-backoff")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, time.Second, "sleepCtx must abandon the backoff promptly on cancellation, not block for the full duration")
}

func TestSleepCtxRunsToCompletion(t *testing.T) {
	err := sleepCtx(context.Background(), 5*time.Millisecond)
	assert.NoError(t, err, "sleepCtx must return nil when the backoff elapses without cancellation")
}

// --- Story 4: Git Data API (CreateBranch / CreateCommit) ---

// gitDataRecorder records the ordered sequence of Git Data API endpoints a
// stub server receives plus the last decoded/raw JSON body per endpoint, so a
// test can assert both the get-commit→blob→tree→commit→ref ordering and the
// individual payloads (including an explicit `"sha":null` for deletions).
type gitDataRecorder struct {
	mu     sync.Mutex
	order  []string
	bodies map[string]map[string]any
	raws   map[string]string
}

func (rec *gitDataRecorder) record(name string, raw []byte) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.order = append(rec.order, name)
	if raw != nil {
		if rec.bodies == nil {
			rec.bodies = map[string]map[string]any{}
			rec.raws = map[string]string{}
		}
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		rec.bodies[name] = b
		rec.raws[name] = string(raw)
	}
}

func (rec *gitDataRecorder) seq() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]string, len(rec.order))
	copy(out, rec.order)
	return out
}

// newGitDataServer returns a stub GitHub Git Data API server that answers the
// full read-commit→blob→tree→commit→ref-update sequence with canned SHAs and a
// recorder. baseTree is the tree.sha returned by GET /git/commits/{sha}. Routing
// is on method+path suffix (never call order), per the Phase 1 spike.
func newGitDataServer(t *testing.T, rec *gitDataRecorder, baseTree string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			rec.record("get-commit", nil)
			_, _ = w.Write([]byte(`{"sha":"parent-sha","tree":{"sha":"` + baseTree + `"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			rec.record("blob", raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"blob-sha"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			rec.record("tree", raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"new-tree-sha"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			rec.record("commit", raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"new-commit-sha"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			rec.record("ref", raw)
			_, _ = w.Write([]byte(`{"ref":"refs/heads/x","object":{"sha":"new-commit-sha"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateBranch(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ref":"refs/heads/atcr-fix/x","object":{"sha":"base"}}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateBranch(context.Background(), "o", "r", "atcr-fix/x", "basesha")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/repos/o/r/git/refs", gotPath)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Equal(t, "refs/heads/atcr-fix/x", gotBody["ref"])
	assert.Equal(t, "basesha", gotBody["sha"])
}

func TestCreateBranchNormalizesRefPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"atcr-fix/y", "refs/heads/atcr-fix/y"},            // bare name gets prefixed
		{"refs/heads/atcr-fix/z", "refs/heads/atcr-fix/z"}, // already-prefixed is not doubled
	}
	for _, tc := range cases {
		var gotRef any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			gotRef = b["ref"]
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		}))
		c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
		require.NoError(t, c.CreateBranch(context.Background(), "o", "r", tc.in, "sha"))
		assert.Equal(t, tc.want, gotRef, "branch %q", tc.in)
		srv.Close()
	}
}

func TestCreateBranchRefAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateBranch(context.Background(), "o", "r", "b", "sha")
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "a 422 collision must surface as *APIError for the caller to inspect")
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Reference already exists")
}

func TestCreateBranchInvalidSHADistinguishable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Object does not exist"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateBranch(context.Background(), "o", "r", "b", "badsha")
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Object does not exist")
	assert.NotContains(t, apiErr.Message, "Reference already exists",
		"an invalid-SHA 422 must be distinguishable from a name collision by message text")
}

func TestCreateBranchRetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateBranch(context.Background(), "o", "r", "b", "sha")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3, "CreateBranch must inherit postDo's 5xx retry")
}

func TestCreateCommitSingleFile(t *testing.T) {
	rec := &gitDataRecorder{}
	srv := newGitDataServer(t, rec, "base-tree-sha")

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	sha, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "atcr-fix/x", Message: "fix", ParentSHA: "parent-sha",
		Files: []CommitFile{{Path: "a.go", Content: "package a\n"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-commit-sha", sha, "returns the commit-creation SHA, not the ref-update response")
	assert.Equal(t, []string{"get-commit", "blob", "tree", "commit", "ref"}, rec.seq())

	// base_tree is the tree.sha resolved from the parent commit's GET, not ParentSHA.
	assert.Equal(t, "base-tree-sha", rec.bodies["tree"]["base_tree"])
	// blob content is base64-encoded.
	assert.Equal(t, "base64", rec.bodies["blob"]["encoding"])
	dec, err := base64.StdEncoding.DecodeString(rec.bodies["blob"]["content"].(string))
	require.NoError(t, err)
	assert.Equal(t, "package a\n", string(dec))
	// commit references the new tree and the ParentSHA as parent.
	assert.Equal(t, "new-tree-sha", rec.bodies["commit"]["tree"])
	assert.Equal(t, []any{"parent-sha"}, rec.bodies["commit"]["parents"])
	assert.Equal(t, "fix", rec.bodies["commit"]["message"])
}

func TestCreateCommitMultiFileIsOneAtomicCommit(t *testing.T) {
	rec := &gitDataRecorder{}
	srv := newGitDataServer(t, rec, "bt")

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p",
		Files: []CommitFile{{Path: "a.go", Content: "1"}, {Path: "b.go", Content: "2"}, {Path: "c.go", Content: "3"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"get-commit", "blob", "blob", "blob", "tree", "commit", "ref"}, rec.seq(),
		"three files → three blobs then exactly one tree/commit/ref, never one commit per file")
	assert.Len(t, rec.bodies["tree"]["tree"].([]any), 3)
}

func TestCreateCommitDeletedFileUsesNullTreeEntry(t *testing.T) {
	rec := &gitDataRecorder{}
	srv := newGitDataServer(t, rec, "bt")

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p",
		Files: []CommitFile{{Path: "gone.go", Deleted: true}, {Path: "a.go", Content: "x"}},
	})
	require.NoError(t, err)

	blobs := 0
	for _, s := range rec.seq() {
		if s == "blob" {
			blobs++
		}
	}
	assert.Equal(t, 1, blobs, "a deleted file must not create a blob")
	assert.Contains(t, rec.raws["tree"], `"sha":null`,
		"a deleted file's tree entry must carry an explicit null sha (GitHub's path-removal mechanism)")
}

func TestCreateCommitEmptyFilesError(t *testing.T) {
	rec := &gitDataRecorder{}
	srv := newGitDataServer(t, rec, "bt")

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{Branch: "b", Message: "m", ParentSHA: "p"})
	require.Error(t, err, "an empty file set is a caller bug, not a silent no-op commit")
	assert.Empty(t, rec.seq(), "no HTTP call may be made for an empty file set")
}

func TestCreateCommitParentReadErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Missing/invalid base SHA: the parent-commit read 404s.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No commit found for SHA"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "missing", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "a missing base SHA on the parent-commit read must surface as a typed *APIError")
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestCreateCommitRejectsEmptyParentOrBranch(t *testing.T) {
	cases := []struct {
		name string
		req  CommitRequest
	}{
		{"empty parent", CommitRequest{Branch: "b", Message: "m", ParentSHA: "", Files: []CommitFile{{Path: "a.go", Content: "x"}}}},
		{"empty branch", CommitRequest{Branch: "", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &gitDataRecorder{}
			srv := newGitDataServer(t, rec, "bt")
			c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
			_, err := c.CreateCommit(context.Background(), "o", "r", tc.req)
			require.Error(t, err)
			assert.Empty(t, rec.seq(), "a malformed request must be rejected before any HTTP call, never orphaning a commit")
		})
	}
}

func TestCreateCommitCommitStepFailsNoRefUpdate(t *testing.T) {
	var refCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"bt"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"b"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"t"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"stale parent sha"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			refCalled = true
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.Error(t, err)
	assert.False(t, refCalled, "the ref must never be advanced after a failed commit creation")
	assert.Contains(t, err.Error(), "commit", "the error must name the failed step")
}

func TestCreateCommitRefUpdateFailureNamesStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"bt"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"b"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"t"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"c"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"update failed"}`))
		}
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "feature-x", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ref", "the error must make clear the ref-update step failed")
	assert.Contains(t, err.Error(), "feature-x", "the error must name the branch whose ref was not advanced")
}

func TestCreateCommitBlobRetriesOn5xx(t *testing.T) {
	blobAttempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"bt"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			blobAttempts++
			if blobAttempts < 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"b"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"t"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"new-commit-sha"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	sha, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-commit-sha", sha)
	assert.GreaterOrEqual(t, blobAttempts, 2, "a sub-call must inherit postDo's transient-failure retry")
}

func TestCreateCommitRefPatchRetriesOn429(t *testing.T) {
	patchAttempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"bt"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"b"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"t"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"c"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			patchAttempts++
			if patchAttempts < 2 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.NoError(t, err, "the PATCH ref-update must retry a 429 identically to the POST calls")
	assert.GreaterOrEqual(t, patchAttempts, 2, "the PATCH path must not fork a separate, unretried code path")
}

func TestCreateCommitRedactsTokenInError(t *testing.T) {
	const token = "ghp_SECRETTOKEN1234567890abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"bt"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials for ` + r.Header.Get("Authorization") + `"}`))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"x"}`))
		}
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: token, HTTPClient: srv.Client()}
	_, err := c.CreateCommit(context.Background(), "o", "r", CommitRequest{
		Branch: "b", Message: "m", ParentSHA: "p", Files: []CommitFile{{Path: "a.go", Content: "x"}},
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token, "a token echoed in an error body must not leak through a CreateCommit sub-call")
	assert.Contains(t, err.Error(), "[redacted]")
}

// --- Story 5: Pull Request lifecycle (CreatePullRequest / findOpenPullRequest / UpdatePullRequest) ---

func TestCreatePullRequest(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	num, err := c.CreatePullRequest(context.Background(), "o", "r", PullRequestRequest{
		Head: "atcr-fix/x", Base: "main", Title: "atcr: auto-fix TD-042", Body: "fixes the thing",
	})
	require.NoError(t, err)
	assert.Equal(t, 42, num, "the decoded PR number must be returned")
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/repos/o/r/pulls", gotPath)
	assert.Equal(t, "atcr-fix/x", gotBody["head"])
	assert.Equal(t, "main", gotBody["base"])
	assert.Equal(t, "atcr: auto-fix TD-042", gotBody["title"])
	assert.Equal(t, "fixes the thing", gotBody["body"])
}

func TestCreatePullRequest422Typed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"A pull request already exists for o:atcr-fix/x"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, err := c.CreatePullRequest(context.Background(), "o", "r", PullRequestRequest{Head: "h", Base: "main", Title: "t", Body: "b"})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "a 422 (duplicate/invalid base) must be a typed *APIError, not a generic error")
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
}

func TestCreatePullRequestRedactsOutboundTitleAndBody(t *testing.T) {
	const token = "ghp_SECRETTOKEN1234567890abcdef"
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":1}`))
	}))
	defer srv.Close()

	// A PR title/body built from validation diagnostics happens to echo a token.
	c := &Client{APIURL: srv.URL, Token: token, HTTPClient: srv.Client()}
	_, err := c.CreatePullRequest(context.Background(), "o", "r", PullRequestRequest{
		Head: "h", Base: "main",
		Title: "atcr fix (leaked " + token + ")",
		Body:  "validation stderr: authorization Bearer " + token,
	})
	require.NoError(t, err)
	assert.NotContains(t, gotBody["title"], token, "the outbound PR title must be redacted before it is sent to GitHub")
	assert.NotContains(t, gotBody["body"], token, "the outbound PR body must be redacted before it is sent to GitHub")
	assert.Contains(t, gotBody["title"].(string), "[redacted]")
	assert.Contains(t, gotBody["body"].(string), "[redacted]")
}

func TestCreatePullRequestRetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	num, err := c.CreatePullRequest(context.Background(), "o", "r", PullRequestRequest{Head: "h", Base: "main", Title: "t", Body: "b"})
	require.NoError(t, err)
	assert.Equal(t, 7, num)
	assert.GreaterOrEqual(t, attempts, 3, "PR creation must inherit postDo's 5xx retry")
}

func TestCreatePullRequestRedactsTokenInError(t *testing.T) {
	const token = "ghp_SECRETTOKEN1234567890abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad credentials for ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: token, HTTPClient: srv.Client()}
	_, err := c.CreatePullRequest(context.Background(), "o", "r", PullRequestRequest{Head: "h", Base: "main", Title: "t", Body: "b"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token)
	assert.Contains(t, err.Error(), "[redacted]")
}

func TestFindOpenPullRequestFound(t *testing.T) {
	var gotHead, gotState string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHead = r.URL.Query().Get("head")
		gotState = r.URL.Query().Get("state")
		_, _ = w.Write([]byte(`[{"number":17}]`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	num, found, err := c.findOpenPullRequest(context.Background(), "o", "r", "atcr-fix/x")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 17, num)
	assert.Equal(t, "o:atcr-fix/x", gotHead, "existence check must query head=owner:branch")
	assert.Equal(t, "open", gotState)
}

func TestFindOpenPullRequestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	num, found, err := c.findOpenPullRequest(context.Background(), "o", "r", "atcr-fix/x")
	require.NoError(t, err, "an empty result is found=false, not an error")
	assert.False(t, found)
	assert.Equal(t, 0, num)
}

func TestFindOpenPullRequestPicksLowestNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// More than one open PR matches the same head (e.g. a human opened one manually).
		_, _ = w.Write([]byte(`[{"number":20},{"number":8},{"number":15}]`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	num, found, err := c.findOpenPullRequest(context.Background(), "o", "r", "b")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 8, num, "on multiple matches the lowest-numbered open PR is chosen deterministically")
}

func TestFindOpenPullRequestEscapesBranch(t *testing.T) {
	var gotHead, gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHead = r.URL.Query().Get("head")
		gotRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	_, _, err := c.findOpenPullRequest(context.Background(), "o", "r", "feature/a b&x")
	require.NoError(t, err)
	assert.Equal(t, "o:feature/a b&x", gotHead, "branch with special chars must round-trip via a properly escaped query")
	assert.NotContains(t, gotRawQuery, "a b", "a raw space must not leak into the query string (must be escaped)")
}

func TestUpdatePullRequest(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"number":17}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.UpdatePullRequest(context.Background(), "o", "r", 17, PullRequestRequest{
		Head: "h", Base: "main", Title: "atcr: auto-fix TD-042 (updated)", Body: "refreshed",
	})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPatch, gotMethod)
	assert.Equal(t, "/repos/o/r/pulls/17", gotPath)
	assert.Equal(t, "atcr: auto-fix TD-042 (updated)", gotBody["title"])
	assert.Equal(t, "refreshed", gotBody["body"])
}

func TestUpdatePullRequestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.UpdatePullRequest(context.Background(), "o", "r", 17, PullRequestRequest{Title: "t", Body: "b"})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "a stale/closed PR number (404) must surface as a typed *APIError")
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestUpdatePullRequestRetriesOn429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"number":17}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.UpdatePullRequest(context.Background(), "o", "r", 17, PullRequestRequest{Title: "t", Body: "b"})
	require.NoError(t, err, "the PR-update PATCH must retry a 429 identically to other calls")
	assert.GreaterOrEqual(t, attempts, 2)
}

func TestUpdatePullRequestRedactsOutboundContent(t *testing.T) {
	const token = "ghp_SECRETTOKEN1234567890abcdef"
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"number":17}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: token, HTTPClient: srv.Client()}
	err := c.UpdatePullRequest(context.Background(), "o", "r", 17, PullRequestRequest{
		Title: "updated " + token, Body: "body " + token,
	})
	require.NoError(t, err)
	assert.NotContains(t, gotBody["title"], token, "an updated PR title must be redacted before it is sent")
	assert.NotContains(t, gotBody["body"], token, "an updated PR body must be redacted before it is sent")
}
