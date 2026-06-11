package main

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// initGitRepo turns the current (isolated) working dir into a git repo with a
// single empty commit, so ref resolution has something to resolve.
func initGitRepo(t *testing.T) {
	t.Helper()
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
}

func TestRangeCmd_ResolutionFailureIsUsageError(t *testing.T) {
	// `atcr review` maps the identical gitrange.Resolve failure to exit 2;
	// `atcr range` must classify it the same way so pre-flighting agrees with
	// the review path and exit 1 keeps meaning "gate failure" only.
	isolate(t)
	initGitRepo(t)
	require.Equal(t, 2, execCmd(t, "range", "--base", "HEAD", "--head", "bogusref"))
}
