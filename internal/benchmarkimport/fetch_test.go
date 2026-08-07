package benchmarkimport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchDataset_ReturnsTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"githubPrUrl":"x"}]`))
	}))
	defer srv.Close()

	got, err := FetchDataset(context.Background(), srv.Client(), srv.URL)

	require.NoError(t, err)
	assert.Equal(t, `[{"githubPrUrl":"x"}]`, string(got))
}

func TestFetchDataset_ReportsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := FetchDataset(context.Background(), srv.Client(), srv.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404", "the status is surfaced so a moved dataset is obvious")
}

func TestFetchDataset_RejectsAnUnbuildableRequest(t *testing.T) {
	_, err := FetchDataset(context.Background(), nil, "://not a url")

	assert.Error(t, err, "a malformed URL fails before any network call")
}

func TestCompareAPIFetcher_RequestsADiffAndReturnsIt(t *testing.T) {
	var gotPath, gotAccept, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAccept, gotAuth = r.URL.Path, r.Header.Get("Accept"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte("--- a/x\n+++ b/x\n"))
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), Token: "tok", baseURL: srv.URL}
	got, err := f.FetchDiff(context.Background(), "o", "r", "base1", "head2")

	require.NoError(t, err)
	assert.Equal(t, "--- a/x\n+++ b/x\n", string(got))
	assert.Equal(t, "/repos/o/r/compare/base1...head2", gotPath, "compare range is pinned to the record's commit pair")
	assert.Equal(t, "application/vnd.github.v3.diff", gotAccept, "the diff media type is what makes this return a patch")
	assert.Equal(t, "Bearer tok", gotAuth, "the token is sent so the run is not held to the anonymous rate limit")
}

func TestCompareAPIFetcher_OmitsAuthorizationWithoutAToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("diff"))
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.NoError(t, err)
	assert.Empty(t, gotAuth, "no token means no Authorization header, not an empty Bearer")
}

func TestCompareAPIFetcher_ReportsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "403", "a rate-limit rejection is surfaced, not treated as an empty diff")
}

func TestCloneFetcher_ProducesTheSameDiffFromALocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, head := seedRepo(t)

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	got, err := f.FetchDiff(context.Background(), "o", "r", base, head)

	require.NoError(t, err, "the documented clone fallback must actually work")
	assert.Contains(t, string(got), "+added line", "the fallback yields the same content the compare API would")
}

func TestCloneFetcher_ReportsAFailedClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: filepath.Join(t.TempDir(), "nope")}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	assert.Error(t, err, "a missing upstream fails loudly rather than yielding an empty diff")
}

func TestCloneFetcher_DefaultsToGitHub(t *testing.T) {
	f := &CloneFetcher{}

	assert.Equal(t, "https://github.com/o/r.git", f.cloneURL("o", "r"),
		"the fallback targets github.com unless explicitly redirected")
}

// seedRepo builds a two-commit git repository laid out as <root>/o/r.git so a
// CloneFetcher with BaseURL=<root> resolves it, and returns root plus the base
// and head SHAs.
func seedRepo(t *testing.T) (root, base, head string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "o", "r.git")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	run("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("first\n"), 0o644))
	run("add", "f.txt")
	run("commit", "-q", "-m", "one")
	base = run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("first\nadded line\n"), 0o644))
	run("add", "f.txt")
	run("commit", "-q", "-m", "two")
	head = run("rev-parse", "HEAD")

	// Serving a clone from a local path requires the receiving side to accept
	// fetching arbitrary SHAs.
	run("config", "uploadpack.allowAnySHA1InWant", "true")
	return root, base, head
}
