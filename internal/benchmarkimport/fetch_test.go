package benchmarkimport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestFetchDataset_RejectsAnOversizedDataset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxDatasetBytes+1))
	}))
	defer srv.Close()

	_, err := FetchDataset(context.Background(), srv.Client(), srv.URL)

	require.Error(t, err, "a truncated download must not surface later as an opaque JSON parse error")
	assert.Contains(t, err.Error(), "exceeds", "the operator is told this is a size problem, not a parse problem")
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

func TestCompareAPIFetcher_ReportsAGoneRangeAsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	assert.ErrorIs(t, err, ErrDiffUnavailable,
		"a 404 compare range is one dead PR, not a broken ingestion, so it must be distinguishable")
}

func TestCompareAPIFetcher_RetriesAThrottledRequestAndHonorsRetryAfter(t *testing.T) {
	shortenBackoff(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte("real diff"))
		}
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	got, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.NoError(t, err,
		"rate limiting is the anticipated failure mode, not an exotic one: it must not abort an 18-record ingestion")
	assert.Equal(t, "real diff", string(got))
	assert.Equal(t, 3, calls, "both the 429 and the 502 are retried")
}

func TestCompareAPIFetcher_RetriesASecondaryRateLimit403(t *testing.T) {
	shortenBackoff(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
			return
		}
		_, _ = w.Write([]byte("real diff"))
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	got, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.NoError(t, err, "GitHub reports a secondary rate limit as 403, and it is transient")
	assert.Equal(t, "real diff", string(got))
	assert.Equal(t, 2, calls)
}

func TestCompareAPIFetcher_DoesNotRetryAPlainForbidden(t *testing.T) {
	shortenBackoff(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.Error(t, err)
	assert.Equal(t, 1, calls, "a bad token is permanent — retrying it just burns the run's wall clock")
}

func TestCompareAPIFetcher_GivesUpAfterBoundedRetries(t *testing.T) {
	shortenBackoff(t)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	require.Error(t, err, "retry is bounded — a sustained outage must surface, not spin")
	assert.Contains(t, err.Error(), "429", "the final status is still reported")
	assert.Equal(t, maxFetchAttempts, calls, "exactly the configured attempt budget is spent")
}

// shortenBackoff collapses the retry delay so a bounded-retry test costs
// milliseconds rather than the real backoff schedule.
func shortenBackoff(t *testing.T) {
	t.Helper()
	orig := retryBaseDelay
	retryBaseDelay = time.Millisecond
	t.Cleanup(func() { retryBaseDelay = orig })
}

func TestCompareAPIFetcher_EnforcesTheDiffCeilingExactlyAtTheBoundary(t *testing.T) {
	// The off-by-one here is what distinguishes "at the limit" from "truncated":
	// a flipped +1 or >= accepts a truncated diff or rejects a legal one.
	for _, tc := range []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{"exactly at the ceiling", maxDiffBytes, false},
		{"one byte over the ceiling", maxDiffBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(make([]byte, tc.size))
			}))
			defer srv.Close()

			f := &CompareAPIFetcher{Client: srv.Client(), baseURL: srv.URL}
			got, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

			if tc.wantErr {
				require.Error(t, err, "a truncated read must not be accepted as a complete diff")
				assert.Contains(t, err.Error(), "exceeds", "the operator is told this is a size problem")
			} else {
				require.NoError(t, err, "a diff exactly at the ceiling is legal")
				assert.Len(t, got, int(tc.size), "the full at-ceiling body is returned")
			}
		})
	}
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

func TestCloneFetcher_DiffIgnoresAPoisonedExternalDiffDriver(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, head := seedRepo(t)

	// diff.external supplied through env config, which gitexec's system/global
	// hardening does not cover: without --no-ext-diff at the call site git would
	// execute it and its output would replace the real diff bytes.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "diff.external")
	t.Setenv("GIT_CONFIG_VALUE_0", "echo PWNED-EXTERNAL-DIFF")

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	got, err := f.FetchDiff(context.Background(), "o", "r", base, head)

	require.NoError(t, err)
	assert.Contains(t, string(got), "+added line", "the real diff bytes are returned")
	assert.NotContains(t, string(got), "PWNED-EXTERNAL-DIFF",
		"every diff-family call site must pass --no-ext-diff; these bytes land in a committed benchmark diff")
}

func TestCloneFetcher_RejectsAnOversizedDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, head := seedRepo(t)

	orig := maxDiffBytes
	maxDiffBytes = 4 // the seeded diff is larger than this
	t.Cleanup(func() { maxDiffBytes = orig })

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	_, err := f.FetchDiff(context.Background(), "o", "r", base, head)

	require.Error(t, err,
		"an oversized diff fails at the record that caused it, not after being written and committed")
	assert.Contains(t, err.Error(), "exceeds", "the runner ceiling is named so the size problem is diagnosable")
}

func TestCloneFetcher_ReportsAFailedClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: filepath.Join(t.TempDir(), "nope")}
	_, err := f.FetchDiff(context.Background(), "o", "r", "a", "b")

	assert.Error(t, err, "a missing upstream fails loudly rather than yielding an empty diff")
}

func TestCloneFetcher_DiffErrorCarriesGitsStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, _ := seedRepo(t)
	unrelated := seedUnrelatedCommit(t, root)

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	_, err := f.FetchDiff(context.Background(), "o", "r", base, unrelated)

	require.Error(t, err, "a range with no shared history cannot yield a three-dot diff")
	assert.Contains(t, err.Error(), "no merge base",
		"git's own diagnostic travels with the error; a bare exit status sends the operator nowhere")
}

// seedUnrelatedCommit adds a root commit with no shared history to the repo
// seedRepo built, and returns its SHA.
func seedUnrelatedCommit(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "o", "r.git")

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

	run("checkout", "-q", "--orphan", "unrelated")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.txt"), []byte("elsewhere\n"), 0o644))
	run("add", "other.txt")
	run("commit", "-q", "-m", "unrelated root")
	return run("rev-parse", "HEAD")
}

func TestCloneFetcher_ReportsAGoneCommitAsUnavailable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, _ := seedRepo(t)
	gone := "0123456789abcdef0123456789abcdef01234567"

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	_, err := f.FetchDiff(context.Background(), "o", "r", base, gone)

	assert.ErrorIs(t, err, ErrDiffUnavailable,
		"a force-pushed or garbage-collected PR is one dead record, not a broken ingestion — it must be skippable like the compare fetcher's 404")
}

func TestCloneFetcher_ReusesOneTempWorkDirAcrossCalls(t *testing.T) {
	f := &CloneFetcher{}

	first, err := f.workDir()
	require.NoError(t, err)
	second, err := f.workDir()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(first) })

	assert.Equal(t, first, second,
		"a fresh temp dir per call would defeat the clone cache and leave one full clone per case on disk")
}

func TestCloneFetcher_CleanupRemovesOnlyASelfAllocatedWorkDir(t *testing.T) {
	f := &CloneFetcher{}
	selfAlloc, err := f.workDir()
	require.NoError(t, err)

	require.NoError(t, f.Cleanup())
	assert.NoDirExists(t, selfAlloc, "a self-allocated clone root is removed at end of run")

	supplied := t.TempDir()
	g := &CloneFetcher{WorkDir: supplied}
	require.NoError(t, g.Cleanup())
	assert.DirExists(t, supplied, "a caller-supplied WorkDir is the caller's to manage, never removed")
}

func TestCloneFetcher_DefaultsToGitHub(t *testing.T) {
	f := &CloneFetcher{}

	assert.Equal(t, "https://github.com/o/r.git", f.cloneURL("o", "r"),
		"the fallback targets github.com unless explicitly redirected")
}

func TestCloneFetcher_DiffFormatIsPinnedAgainstLocalGitConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, base, head := seedFormatRepo(t)

	// The three knobs a git config can turn that silently change the diff bytes
	// committed into a benchmark suite. gitexec neutralizes system and global
	// config, but not env-injected config, and never the clone's own local
	// config — so the format has to be pinned at the call site.
	t.Setenv("GIT_CONFIG_COUNT", "3")
	t.Setenv("GIT_CONFIG_KEY_0", "diff.context")
	t.Setenv("GIT_CONFIG_VALUE_0", "7")
	t.Setenv("GIT_CONFIG_KEY_1", "diff.noprefix")
	t.Setenv("GIT_CONFIG_VALUE_1", "true")
	t.Setenv("GIT_CONFIG_KEY_2", "diff.renames")
	t.Setenv("GIT_CONFIG_VALUE_2", "false")

	f := &CloneFetcher{WorkDir: t.TempDir(), BaseURL: root}
	got, err := f.FetchDiff(context.Background(), "o", "r", base, head)

	require.NoError(t, err)
	out := string(got)

	// line 10 of a 20-line file changed: three lines of context each side is
	// @@ -7,7 +7,7 @@; seven would be @@ -3,15 +3,15 @@.
	assert.Contains(t, out, "@@ -7,7 +7,7 @@",
		"context width must be pinned at the call site — diff.context changes the committed bytes and the reproducibility hash")
	assert.Contains(t, out, "+++ b/wide.txt",
		"a/ and b/ prefixes must be pinned — diff.noprefix rewrites every file header")
	assert.Contains(t, out, "rename from old.txt",
		"rename detection must be pinned ON: the compare API emits `similarity index`/`rename from`, so disabling it diverges from the primary fetcher")
}

// seedFormatRepo builds a repository whose diff exposes the config-sensitive
// parts of git's output: a change with enough surrounding lines to distinguish
// context widths, and a pure rename to distinguish rename detection.
func seedFormatRepo(t *testing.T) (root, base, head string) {
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

	wide := func(line10 string) []byte {
		lines := make([]string, 0, 20)
		for i := 1; i <= 20; i++ {
			if i == 10 {
				lines = append(lines, line10)
				continue
			}
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		return []byte(strings.Join(lines, "\n") + "\n")
	}

	run("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.txt"), wide("line 10"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old.txt"), []byte("moved unchanged\n"), 0o644))
	run("add", "wide.txt", "old.txt")
	run("commit", "-q", "-m", "one")
	base = run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.txt"), wide("changed line 10"), 0o644))
	run("mv", "old.txt", "new.txt")
	run("add", "wide.txt")
	run("commit", "-q", "-m", "two")
	head = run("rev-parse", "HEAD")

	run("config", "uploadpack.allowAnySHA1InWant", "true")
	return root, base, head
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
