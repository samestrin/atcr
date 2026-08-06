package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoRoot_SymlinkToGitDirIsRejected(t *testing.T) {
	// repoRoot must NOT follow a .git symlink to an arbitrary directory —
	// os.Stat follows symlinks, which would let a .git symlink to another
	// directory bypass the repo-root check. Use os.Lstat instead.
	isolate(t)

	// Create a nested structure:
	//   outer/
	//     .git/         (real directory — valid repo root)
	//     inner/
	//       .git -> ../ (symlink to outer/.git)
	// chdir into inner/. repoRoot should skip the symlink, walk up to outer/,
	// and return outer/ — NOT inner/ (which would indicate the symlink was followed).
	outer := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outer, ".git"), 0o755))
	inner := filepath.Join(outer, "inner")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.Symlink("..", filepath.Join(inner, ".git")))

	require.NoError(t, os.Chdir(inner))

	got, err := repoRoot()
	require.NoError(t, err)

	// After the fix (os.Lstat), repoRoot skips the symlink and returns outer.
	// Before the fix (os.Stat), repoRoot follows the symlink and returns inner.
	// Resolve symlinks in outer to match the path os.Getwd() returns (macOS
	// symlinks /var to /private/var).
	expectedOuter, err := filepath.EvalSymlinks(outer)
	require.NoError(t, err)
	require.Equal(t, expectedOuter, got, "repoRoot must skip .git symlink and walk up to the real repo root")
}

// repoRoot must stay STRICT about .git being a directory. Eight consumers
// (config, telemetry consent, history, audit, resume, review) resolve their
// repo-root state through it, and accepting a .git FILE would treat a submodule
// or linked worktree as its own atcr repo — silently relocating that state, a
// recorded telemetry opt-out included, out of the parent repo. The debt store
// needs the broader rule and carries its own walk (debtRepoRoot); this test is
// what stops that rule from being "unified" back into here.
func TestRepoRoot_GitFileIsNotAMarkerForTheSharedWalk(t *testing.T) {
	isolate(t)

	parent := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(parent, ".git"), 0o755))
	sub := filepath.Join(parent, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: /elsewhere\n"), 0o644))
	require.NoError(t, os.Chdir(sub))

	got, err := repoRoot()
	require.NoError(t, err)

	expected, err := filepath.EvalSymlinks(parent)
	require.NoError(t, err)
	require.Equal(t, expected, got, "the shared walk resolves past a submodule to the parent repo")
}

func TestRepoRoot_FallbackToCwdWhenNoMarker(t *testing.T) {
	// When no .git or .atcr marker exists, repoRoot must return cwd.
	isolate(t)

	got, err := repoRoot()
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)

	require.Equal(t, cwd, got, "repoRoot must return cwd when no marker is found")
}

// TestRepoRoot_LinkedWorktreeResolvesToMainCheckout covers the TD fix
// (cli/root.go:30): a linked worktree's root holds a .git FILE, and before the
// fix the strict directory-only rule found no marker anywhere — config,
// telemetry consent, history, audit, resume and review all fell back to
// whatever subdirectory the command ran from. git's own linked-worktree marker
// (the commondir file in the gitdir target, absent from a submodule's
// .git/modules/<name>) distinguishes the two, so a worktree resolves to the
// MAIN checkout: every worktree of one repository converges on the same
// canonical .atcr/config.yaml. Uses a REAL `git worktree add` so the on-disk
// layout is git's own, not a hand-built approximation.
func TestRepoRoot_LinkedWorktreeResolvesToMainCheckout(t *testing.T) {
	isolate(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	main := t.TempDir()
	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	gitRun(main, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(main, "f"), []byte("x"), 0o644))
	gitRun(main, "add", "f")
	gitRun(main, "commit", "-qm", "init")

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(main, "worktree", "add", "-q", "--detach", wt)

	want, err := filepath.EvalSymlinks(main)
	require.NoError(t, err)

	// From the worktree TOP...
	require.NoError(t, os.Chdir(wt))
	got, err := repoRoot()
	require.NoError(t, err)
	assert.Equal(t, want, got, "a linked worktree must resolve to the main checkout, not its own root")

	// ...and from a SUBDIRECTORY of it (the pre-fix behavior differed by CWD).
	sub := filepath.Join(wt, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.Chdir(sub))
	got, err = repoRoot()
	require.NoError(t, err)
	assert.Equal(t, want, got, "the resolution must be CWD-independent inside a worktree")
}
