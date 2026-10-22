package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// initGitRepo turns the current (isolated) working dir into a git repo with a
// single empty commit, so ref resolution has something to resolve.
//
// Every command goes through fixtureGit, which pins GIT_DIR and GIT_WORK_TREE to
// the working dir. That matters: an earlier version ran git against the ambient
// cwd with no pinning, and when a test-ordering change left cwd at the package
// directory these exact commits ("init", "second", author t@t.invalid) rewrote a
// live feature branch's ref, discarding nine commits from it. Under a git hook an
// inherited GIT_DIR did the same even with cwd correct (Epic 45.0).
//
// The guard below still refuses to run outside a fresh temp dir: the working dir
// is the fixture, so it must not be the checkout itself or an existing repo.
func initGitRepo(t *testing.T) {
	t.Helper()
	wd := requireIsolatedWorkdir(t)
	runFixtureGit(t, wd, "init", "-q")
	runFixtureGit(t, wd, "commit", "--allow-empty", "-q", "-m", "init")
	runFixtureGit(t, wd, "commit", "--allow-empty", "-q", "-m", "second")
}

func TestRangeCmd_BaseOnlyDefaultsHeadToHEAD(t *testing.T) {
	// The shipped CI integrations invoke `atcr range/review --base <ref>`
	// alone; head defaults to HEAD (clarification 2026-06-11).
	isolate(t)
	initGitRepo(t)
	out, err := execute(t, "range", "--base", "HEAD^")
	require.NoError(t, err)
	require.Contains(t, out, `"detection_mode": "explicit"`)
}

func TestRangeCmd_HeadOnlyIsUsageError(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	require.Equal(t, 2, execCmd(t, "range", "--head", "HEAD"))
}

func TestRangeCmd_ResolutionFailureIsUsageError(t *testing.T) {
	// `atcr review` maps the identical gitrange.Resolve failure to exit 2;
	// `atcr range` must classify it the same way so pre-flighting agrees with
	// the review path and exit 1 keeps meaning "gate failure" only.
	isolate(t)
	initGitRepo(t)
	require.Equal(t, 2, execCmd(t, "range", "--base", "HEAD", "--head", "bogusref"))
}

// requireIsolatedWorkdir fails the test unless the current working directory is
// a temp dir that is not already a git repository, and returns it. It is the
// precondition for any helper that runs `git init` in the working dir.
//
// Both halves matter. The TempDir check catches "forgot to t.Chdir at all"; the
// git-repo check catches a working dir that some earlier step already made a
// repository, whose refs `git init` and committing would then rewrite.
func requireIsolatedWorkdir(t *testing.T) string {
	t.Helper()
	wd := requireTempWorkdir(t)

	if out, err := fixtureGit(wd, "rev-parse", "--git-dir").CombinedOutput(); err == nil {
		t.Fatalf("refusing to run git against %s: it is already a git repository (%s). "+
			"Running `git init` and committing here would rewrite that repository's refs.",
			wd, strings.TrimSpace(string(out)))
	}
	return wd
}

// requireTempWorkdir fails the test unless the ambient working directory is a
// temp dir, and returns it. It is the half of requireIsolatedWorkdir that every
// working-dir git fixture needs — the "not already a repo" half above is specific
// to helpers that run `git init`, and would reject a helper that legitimately
// operates INSIDE a repo an earlier initGitRepo just created.
func requireTempWorkdir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)

	tmp, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(wd)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(resolved, tmp),
		"refusing to run git against %s: this helper operates on the ambient working "+
			"directory, so the test must t.Chdir into a temp dir first", wd)
	return wd
}
