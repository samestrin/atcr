package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Epic 49.2 T2: --config-dir loads .atcr/config.yaml from another tree ----

// touchArgv is a validate_command that records it ran by creating marker.
func touchArgv(marker string) string {
	return `["sh", "-c", "touch '` + marker + `'"]`
}

// runReviewAutoFixWithConfigDir drives a one-shot `atcr review --auto-fix` in a
// fixture worktree (the CWD) while --config-dir names a second directory. The
// config directory gets the live roster plus configDirExtra; the worktree's
// .atcr/config.yaml is then replaced by worktreeConfig. It returns the exit
// code, the combined output, and the fake GitHub client.
func runReviewAutoFixWithConfigDir(t *testing.T, configDirExtra, worktreeConfig string) (int, string, *fakeGitHub) {
	t.Helper()
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	liveReviewConfig(t, autoFixDiffProvider(t).URL, "solo")

	// Move the live roster into the config directory, then give the worktree
	// its own config.
	configDir := t.TempDir()
	roster, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".atcr", "config.yaml"),
		append(roster, []byte("consensus: off\n"+configDirExtra)...), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte(worktreeConfig), 0o644))

	oldResolve := resolveHeadSHAFn
	resolveHeadSHAFn = func(context.Context, string) (string, error) { return "deadbeef", nil }
	t.Cleanup(func() { resolveHeadSHAFn = oldResolve })
	gh := &fakeGitHub{}
	oldClient := newAutoFixGitHubFn
	newAutoFixGitHubFn = func(string, string) autoFixGitHub { return gh }
	t.Cleanup(func() { newAutoFixGitHubFn = oldClient })

	code, out := execCmdCapture(t, "review", "--base", "HEAD^", "--fail-on", "LOW",
		"--auto-fix", "--no-sandbox", "--repo", "o/r", "--token", "tok",
		"--config-dir", configDir)
	return code, out, gh
}

// TestReviewCmd_ConfigDirValidateCommandWins: the worktree and the config
// directory each set a validate_command; the run validates with the config
// directory's and never runs the worktree's.
func TestReviewCmd_ConfigDirValidateCommandWins(t *testing.T) {
	markers := t.TempDir()
	fromConfigDir := filepath.Join(markers, "config-dir-ran")
	fromWorktree := filepath.Join(markers, "worktree-ran")

	code, out, gh := runReviewAutoFixWithConfigDir(t,
		"auto_fix:\n  validate_command: "+touchArgv(fromConfigDir)+"\n",
		"agents: [solo]\nconsensus: off\nauto_fix:\n  validate_command: "+touchArgv(fromWorktree)+"\n")
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, gh.createPR, "validation passed, so the run opens one pull request: %s", out)
	require.FileExists(t, fromConfigDir, "the config directory's validate_command must run")
	require.NoFileExists(t, fromWorktree, "the worktree's validate_command must never run")
}

// TestReviewCmd_ConfigDirNeverReadsWorktreeConfig: a worktree config that
// cannot even be parsed does not matter, so nothing reads it.
func TestReviewCmd_ConfigDirNeverReadsWorktreeConfig(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "config-dir-ran")
	code, out, gh := runReviewAutoFixWithConfigDir(t,
		"auto_fix:\n  validate_command: "+touchArgv(marker)+"\n",
		"this is: [not valid yaml\n")
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, gh.createPR, out)
	require.FileExists(t, marker)
}

// TestReviewCmd_ConfigDirMustBeADirectory: a missing or non-directory value is
// a usage error (exit 2) before any review work.
func TestReviewCmd_ConfigDirMustBeADirectory(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	for _, v := range []string{"", "  ", filepath.Join(t.TempDir(), "missing"), file} {
		t.Run(v, func(t *testing.T) {
			code, out := execCmdCapture(t, "review", "--config-dir", v)
			require.Equal(t, 2, code, out)
			require.Contains(t, out, "--config-dir")
		})
	}
}

// TestReviewCmd_ConfigDirWithoutConfigRefuses: a config directory with no
// .atcr/config.yaml refuses (exit 2) rather than falling back to the worktree.
func TestReviewCmd_ConfigDirWithoutConfigRefuses(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	liveReviewConfig(t, "http://127.0.0.1:1", "solo")
	code, out := execCmdCapture(t, "review", "--base", "HEAD^", "--config-dir", t.TempDir())
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "config.yaml not found")
}

// TestReviewCmd_ResumeRejectsConfigDir: --resume branches off before the fresh
// path reads --config-dir, so it is refused there rather than silently dropped.
func TestReviewCmd_ResumeRejectsConfigDir(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--config-dir", t.TempDir())
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "--resume does not support --config-dir")
}
