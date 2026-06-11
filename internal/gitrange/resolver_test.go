package gitrange

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitCmd runs git argv in dir and fails the test on error.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// writeCommit writes content to file under dir and commits it, returning the SHA.
func writeCommit(t *testing.T, dir, file, content, msg string) string {
	t.Helper()
	require.NoError(t, writeFile(filepath.Join(dir, file), content))
	gitCmd(t, dir, "add", file)
	gitCmd(t, dir, "commit", "-m", msg)
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// initRepo creates a repo on branch main with a single initial commit.
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	first := writeCommit(t, dir, "a.txt", "one\n", "init")
	return dir, first
}

func TestResolve_Explicit(t *testing.T) {
	dir, base := initRepo(t)
	head := writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")

	res, err := Resolve(context.Background(), dir, Options{Base: base, Head: head})
	require.NoError(t, err)
	assert.Equal(t, base, res.Base)
	assert.Equal(t, head, res.Head)
	assert.Equal(t, ModeExplicit, res.DetectionMode)
	assert.Equal(t, 1, res.CommitCount)
	assert.False(t, res.Shallow)
	assert.False(t, res.ResolvedAt.IsZero())
}

func TestResolve_ExplicitRequiresBoth(t *testing.T) {
	dir, _ := initRepo(t)
	writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")

	_, err := Resolve(context.Background(), dir, Options{Head: "HEAD"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRef)
}

func TestResolve_MergeCommit(t *testing.T) {
	dir, base := initRepo(t)
	head := writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")

	res, err := Resolve(context.Background(), dir, Options{MergeCommit: head})
	require.NoError(t, err)
	assert.Equal(t, ModeMergeCommit, res.DetectionMode)
	assert.Equal(t, head, res.Head)
	assert.Equal(t, base, res.Base) // SHA^ of head is the initial commit
}

func TestResolve_AutoLocalMain(t *testing.T) {
	dir, _ := initRepo(t)
	// branch off main; default-branch detection should pick local main.
	gitCmd(t, dir, "checkout", "-q", "-b", "feature/foo")
	head := writeCommit(t, dir, "b.txt", "feature\n", "feature work")

	res, err := Resolve(context.Background(), dir, Options{})
	require.NoError(t, err)
	assert.Equal(t, ModeAuto, res.DetectionMode)
	assert.Equal(t, "main", res.DefaultBranch)
	assert.Equal(t, head, res.Head)
	assert.GreaterOrEqual(t, res.CommitCount, 1)
}

func TestResolve_AutoFallsBackToMaster(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "master")
	writeCommit(t, dir, "a.txt", "one\n", "init")
	gitCmd(t, dir, "checkout", "-q", "-b", "feature/foo")
	writeCommit(t, dir, "b.txt", "feature\n", "feature work")

	res, err := Resolve(context.Background(), dir, Options{})
	require.NoError(t, err)
	assert.Equal(t, "master", res.DefaultBranch)
}

func TestResolve_EmptyRangeSameCommit(t *testing.T) {
	dir, base := initRepo(t)

	_, err := Resolve(context.Background(), dir, Options{Base: base, Head: base})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyRange)
	assert.Contains(t, err.Error(), "same commit")
}

func TestResolve_EmptyRangeNoCommits(t *testing.T) {
	dir, base := initRepo(t)
	head := writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")
	// head..base has zero commits (base is the ancestor) without base==head.
	_, err := Resolve(context.Background(), dir, Options{Base: head, Head: base})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyRange)
}

func TestResolve_InvalidRef(t *testing.T) {
	dir, base := initRepo(t)
	writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")

	_, err := Resolve(context.Background(), dir, Options{Base: base, Head: "xyz999"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRef)
	assert.Contains(t, err.Error(), "does not resolve to a commit")
}

func TestResolve_NotARepository(t *testing.T) {
	dir := t.TempDir() // no git init
	_, err := Resolve(context.Background(), dir, Options{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotARepository)
}

func TestResolve_NoDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	// branch name that matches none of the probes, no origin.
	gitCmd(t, dir, "init", "-q", "-b", "trunk")
	writeCommit(t, dir, "a.txt", "one\n", "init")
	gitCmd(t, dir, "checkout", "-q", "-b", "wip")
	writeCommit(t, dir, "b.txt", "two\n", "work")

	_, err := Resolve(context.Background(), dir, Options{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDefaultBranch)
}

func TestResolve_LeadingDashRefRejected(t *testing.T) {
	dir, base := initRepo(t)
	writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")
	// A ref starting with '-' must not be parsed as a git flag.
	_, err := Resolve(context.Background(), dir, Options{Base: base, Head: "-x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRef)
}

func TestResolve_ContextCancelled(t *testing.T) {
	dir, base := initRepo(t)
	head := writeCommit(t, dir, "a.txt", "one\ntwo\n", "second")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Resolve(ctx, dir, Options{Base: base, Head: head})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestResolve_ShallowClone(t *testing.T) {
	origin, _ := initRepo(t)
	writeCommit(t, origin, "a.txt", "one\ntwo\n", "second")
	writeCommit(t, origin, "a.txt", "one\ntwo\nthree\n", "third")

	clone := t.TempDir()
	out, err := exec.Command("git", "clone", "-q", "--depth", "1", "file://"+origin, clone).CombinedOutput()
	require.NoErrorf(t, err, "clone: %s", out)

	_, err = Resolve(context.Background(), clone, Options{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrShallowClone)
	assert.Contains(t, err.Error(), "git fetch --unshallow")
}
