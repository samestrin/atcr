package webhook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureGit runs git in the fixture repository dir and returns trimmed stdout.
func fixtureGit(t *testing.T, dir string, arg ...string) string {
	t.Helper()
	cmd := gitexec.FixtureCommandContext(context.Background(), dir, arg...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", arg, out)
	return strings.TrimSpace(string(out))
}

// prFixture is an "upstream" repository that carries refs/pull/7/head, and a
// local checkout cloned from it, the shape webhook.repos points at.
type prFixture struct {
	checkout string
	prSHA    string
	scratch  string
}

func newPRFixture(t *testing.T) prFixture {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	require.NoError(t, os.MkdirAll(upstream, 0o755))
	fixtureGit(t, upstream, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("main\n"), 0o644))
	fixtureGit(t, upstream, "add", "a.txt")
	fixtureGit(t, upstream, "commit", "-q", "-m", "main")

	// The PR head lives only under refs/pull, as on GitHub: no branch names it.
	fixtureGit(t, upstream, "checkout", "-q", "-b", "pr")
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("pr head\n"), 0o644))
	fixtureGit(t, upstream, "commit", "-q", "-am", "pr")
	prSHA := fixtureGit(t, upstream, "rev-parse", "HEAD")
	fixtureGit(t, upstream, "update-ref", "refs/pull/7/head", prSHA)
	fixtureGit(t, upstream, "checkout", "-q", "main")
	fixtureGit(t, upstream, "branch", "-q", "-D", "pr")

	// Not `git clone`: clone treats an inherited GIT_DIR as the new repository's.
	checkout := filepath.Join(root, "checkout")
	require.NoError(t, os.MkdirAll(checkout, 0o755))
	fixtureGit(t, checkout, "init", "-q", "-b", "main")
	fixtureGit(t, checkout, "remote", "add", "origin", upstream)
	fixtureGit(t, checkout, "fetch", "-q", "origin")
	fixtureGit(t, checkout, "checkout", "-q", "-B", "main", "origin/main")

	scratch := filepath.Join(root, "scratch")
	require.NoError(t, os.MkdirAll(scratch, 0o755))
	return prFixture{checkout: checkout, prSHA: prSHA, scratch: scratch}
}

func (f prFixture) repos() map[string]string {
	return map[string]string{"acme/widget": f.checkout}
}

// assertNothingLeft checks the checkout is back to its starting state: one
// worktree, no webhook branch, and an empty scratch directory.
func assertNothingLeft(t *testing.T, f prFixture) {
	t.Helper()
	worktrees := fixtureGit(t, f.checkout, "worktree", "list", "--porcelain")
	assert.Equal(t, 1, strings.Count(worktrees, "worktree "), "leftover worktree:\n%s", worktrees)
	assert.Empty(t, fixtureGit(t, f.checkout, "for-each-ref", "refs/heads/"+branchPrefix), "leftover webhook branch")
	entries, err := os.ReadDir(f.scratch)
	require.NoError(t, err)
	assert.Empty(t, entries, "leftover scratch directory")
}

func TestWithPRCheckout_ChecksOutPRHead(t *testing.T) {
	f := newPRFixture(t)
	var seenDir string
	err := WithPRCheckout(context.Background(), f.repos(), "acme/widget", 7, f.scratch, func(_ context.Context, co *Checkout) error {
		seenDir = co.Dir
		assert.Equal(t, f.prSHA, co.Head)
		assert.Equal(t, f.prSHA, fixtureGit(t, co.Dir, "rev-parse", "HEAD"))
		body, err := os.ReadFile(filepath.Join(co.Dir, "a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "pr head\n", string(body))
		assert.True(t, strings.HasPrefix(co.Dir, f.scratch+string(filepath.Separator)), "worktree %s outside scratch", co.Dir)
		assert.NotEmpty(t, fixtureGit(t, f.checkout, "for-each-ref", "refs/heads/"+co.Branch))
		return nil
	})
	require.NoError(t, err)
	assert.NoDirExists(t, seenDir)
	assertNothingLeft(t, f)
	// The operator's own checkout is untouched.
	body, err := os.ReadFile(filepath.Join(f.checkout, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "main\n", string(body))
}

func TestWithPRCheckout_CancelledContextRemovesWorktreeAndBranch(t *testing.T) {
	f := newPRFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var seen *Checkout
	err := WithPRCheckout(ctx, f.repos(), "acme/widget", 7, f.scratch, func(jobCtx context.Context, co *Checkout) error {
		seen = co
		cancel()
		<-jobCtx.Done()
		return jobCtx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, seen)
	assert.NoDirExists(t, seen.Dir)
	assert.Empty(t, fixtureGit(t, f.checkout, "for-each-ref", "refs/heads/"+seen.Branch))
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_TimeoutRemovesWorktree(t *testing.T) {
	f := newPRFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := WithPRCheckout(ctx, f.repos(), "acme/widget", 7, f.scratch, func(jobCtx context.Context, _ *Checkout) error {
		<-jobCtx.Done()
		return jobCtx.Err()
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_JobErrorRemovesWorktree(t *testing.T) {
	f := newPRFixture(t)
	boom := errors.New("boom")
	err := WithPRCheckout(context.Background(), f.repos(), "acme/widget", 7, f.scratch, func(context.Context, *Checkout) error {
		return boom
	})
	require.ErrorIs(t, err, boom)
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_JobPanicRemovesWorktree(t *testing.T) {
	f := newPRFixture(t)
	assert.Panics(t, func() {
		_ = WithPRCheckout(context.Background(), f.repos(), "acme/widget", 7, f.scratch, func(context.Context, *Checkout) error {
			panic("boom")
		})
	})
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_AlreadyCancelledLeavesNothing(t *testing.T) {
	f := newPRFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := WithPRCheckout(ctx, f.repos(), "acme/widget", 7, f.scratch, func(context.Context, *Checkout) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_MissingPRLeavesNothing(t *testing.T) {
	f := newPRFixture(t)
	called := false
	err := WithPRCheckout(context.Background(), f.repos(), "acme/widget", 8, f.scratch, func(context.Context, *Checkout) error {
		called = true
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refs/pull/8/head")
	assert.False(t, called)
	assertNothingLeft(t, f)
}

func TestWithPRCheckout_RefusesBadInput(t *testing.T) {
	f := newPRFixture(t)
	never := func(context.Context, *Checkout) error {
		t.Fatal("job must not run")
		return nil
	}
	cases := []struct {
		name    string
		repos   map[string]string
		repo    string
		number  int
		scratch string
		want    error
	}{
		{"unlisted repo", f.repos(), "acme/other", 7, f.scratch, ErrRepoNotAllowed},
		{"zero PR number", f.repos(), "acme/widget", 0, f.scratch, ErrBadPRNumber},
		{"negative PR number", f.repos(), "acme/widget", -1, f.scratch, ErrBadPRNumber},
		{"relative checkout", map[string]string{"acme/widget": "checkout"}, "acme/widget", 7, f.scratch, ErrBadCheckout},
		{"checkout is not a repository", map[string]string{"acme/widget": f.scratch}, "acme/widget", 7, f.scratch, ErrBadCheckout},
		{"missing scratch", f.repos(), "acme/widget", 7, filepath.Join(f.scratch, "nope"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := WithPRCheckout(context.Background(), tc.repos, tc.repo, tc.number, tc.scratch, never)
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
			}
			assertNothingLeft(t, f)
		})
	}
}

func TestWithPRCheckout_ErrorDoesNotCarryCheckoutPath(t *testing.T) {
	f := newPRFixture(t)
	err := WithPRCheckout(context.Background(), f.repos(), "acme/other", 7, f.scratch, nil)
	require.ErrorIs(t, err, ErrRepoNotAllowed)
	assert.NotContains(t, err.Error(), f.checkout)
}

func TestWithPRCheckout_TwoJobsOnOnePRDoNotCollide(t *testing.T) {
	f := newPRFixture(t)
	err := WithPRCheckout(context.Background(), f.repos(), "acme/widget", 7, f.scratch, func(_ context.Context, outer *Checkout) error {
		return WithPRCheckout(context.Background(), f.repos(), "acme/widget", 7, f.scratch, func(_ context.Context, inner *Checkout) error {
			assert.NotEqual(t, outer.Dir, inner.Dir)
			assert.NotEqual(t, outer.Branch, inner.Branch)
			assert.Equal(t, outer.Head, inner.Head)
			return nil
		})
	})
	require.NoError(t, err)
	assertNothingLeft(t, f)
}
