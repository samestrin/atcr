package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// initGitRepo turns the current (isolated) working dir into a git repo with a
// single empty commit, so ref resolution has something to resolve.
//
// It operates on the AMBIENT working directory — no -C, no cmd.Dir — so every
// caller must t.Chdir into a temp dir first. That contract is load-bearing: run
// without it, `git init` and the two commits land on whatever repository the cwd
// belongs to, which in a normal checkout is atcr itself. This has happened: a
// test-ordering change once left cwd at the package directory and these exact
// commits ("init", "second", author t@t.invalid) rewrote a live feature branch's
// ref, discarding nine commits from it. The objects survived and the branch was
// recoverable from the reflog, but nothing warned at the time.
//
// The guard below makes that failure loud instead of destructive. Keep it: the
// helper's dependence on ambient cwd is invisible at the call site, and Go's
// t.Chdir is explicitly incompatible with t.Parallel, so the preconditions can
// be broken by an unrelated test in another file.
func initGitRepo(t *testing.T) {
	t.Helper()
	requireIsolatedWorkdir(t)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "init")
	run("commit", "--allow-empty", "-q", "-m", "second")
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
// a temp dir that is not already inside a git repository. It is the precondition
// for any helper that runs git against the ambient cwd.
//
// Both halves matter. The TempDir check catches "forgot to t.Chdir at all"; the
// git-repo check catches a temp dir nested inside a checkout, and is what would
// have caught the branch-clobbering incident described on initGitRepo.
func requireIsolatedWorkdir(t *testing.T) {
	t.Helper()
	wd := requireTempWorkdir(t)

	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("refusing to run git against %s: it is already inside a git repository (%s). "+
			"Running `git init` and committing here would rewrite that repository's refs.",
			wd, strings.TrimSpace(string(out)))
	}
}

// requireTempWorkdir fails the test unless the ambient working directory is a
// temp dir, and returns it. It is the half of requireIsolatedWorkdir that every
// ambient-cwd git helper needs — the "not already a repo" half above is specific
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
