package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/telemetry"
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
// code, the combined output, and the fake GitHub client. Each setup, when
// given, runs with the config directory's path once both trees exist.
func runReviewAutoFixWithConfigDir(t *testing.T, configDirExtra, worktreeConfig string, setup ...func(configDir string)) (int, string, *fakeGitHub) {
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
	for _, fn := range setup {
		fn(configDir)
	}

	oldResolve := resolveHeadSHAFn
	resolveHeadSHAFn = func(context.Context, string) (string, error) { return "deadbeef", nil }
	t.Cleanup(func() { resolveHeadSHAFn = oldResolve })
	gh := &fakeGitHub{}
	oldClient := newAutoFixGitHubFn
	newAutoFixGitHubFn = func(string, string) autoFixGitHub { return gh }
	t.Cleanup(func() { newAutoFixGitHubFn = oldClient })

	code, out := execCmdCapture(t, "review", "--base", "HEAD^", "--fail-on", "LOW",
		"--auto-fix", "--auto-fix-base", "main", "--no-sandbox", "--repo", "o/r", "--token", "tok",
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

// aTxtIgnoreRule suppresses the one finding autoFixDiffProvider raises.
const aTxtIgnoreRule = "rules:\n" +
	"  - file: a.txt\n" +
	"    category: correctness\n" +
	"    match: wrong word\n" +
	"    reason: the pull request says so\n"

// TestReviewCmd_ConfigDirIgnoresWorktreeIgnoreRules (epic 99 T1, TD
// internal/reconcile/gate.go:259): under --config-dir the reviewed tree is an
// untrusted pull request head, so an .atcr/ignore.yaml it ships must not hide
// its own finding. The finding still reaches the auto-fix, which opens a PR.
func TestReviewCmd_ConfigDirIgnoresWorktreeIgnoreRules(t *testing.T) {
	code, out, gh := runReviewAutoFixWithConfigDir(t,
		"auto_fix:\n  validate_command: [\"true\"]\n",
		"agents: [solo]\nconsensus: off\n",
		func(string) {
			require.NoError(t, os.WriteFile(filepath.Join(".atcr", "ignore.yaml"), []byte(aTxtIgnoreRule), 0o644))
		})
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, gh.createPR, "the worktree's ignore rule must not suppress the finding: %s", out)
}

// TestReviewCmd_ConfigDirIgnoreRulesApply: the config directory's
// .atcr/ignore.yaml is the one that applies, so its rule suppresses the finding
// and the auto-fix has nothing to open a PR for.
func TestReviewCmd_ConfigDirIgnoreRulesApply(t *testing.T) {
	code, out, gh := runReviewAutoFixWithConfigDir(t,
		"auto_fix:\n  validate_command: [\"true\"]\n",
		"agents: [solo]\nconsensus: off\n",
		func(configDir string) {
			require.NoError(t, os.WriteFile(filepath.Join(configDir, ".atcr", "ignore.yaml"), []byte(aTxtIgnoreRule), 0o644))
		})
	require.Equal(t, 0, code, out)
	require.Equal(t, 0, gh.createPR, "the config directory's ignore rule suppresses the only finding: %s", out)
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

// TestQualitySignalGateFrom_ReadsConfigRoot: under --config-dir the
// quality-signal opt-in is read from the config directory, so a reviewed tree
// that sets quality_signal: true cannot switch on a transmission.
func TestQualitySignalGateFrom_ReadsConfigRoot(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_QUALITY_SIGNAL")
	writeAtcrConfig(t, "agents: [bruce]\nquality_signal: true\n")
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".atcr", "config.yaml"),
		[]byte("agents: [bruce]\n"), 0o644))

	require.False(t, qualitySignalGateFrom(io.Discard, configDir),
		"the reviewed tree's quality_signal must not count under --config-dir")
	require.True(t, qualitySignalGateFrom(io.Discard, ""),
		"without a config root the gate still reads the reviewed tree")
}

// runReviewSendWithConfigDir is runReviewSend with --config-dir: the roster moves
// to a config directory, then worktreeExtra and configDirExtra are appended to
// the worktree's and the config directory's .atcr/config.yaml. It returns how
// many quality-signal payloads the run built.
func runReviewSendWithConfigDir(t *testing.T, worktreeExtra, configDirExtra string) int32 {
	t.Helper()
	isolate(t)
	unsetEnvForTest(t, "ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	initGitRepoWithChange(t)
	writeBackendContractConfig(t, mockFindingsServer(t).URL)

	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".atcr", "config.yaml"),
		[]byte("agents: [bruce]\n"+configDirExtra), 0o644))
	appendProjectConfig(t, worktreeExtra)

	captureSendBodies(t) // keep the transport hermetic
	builds := countingQualityBuilder(t)
	client := telemetry.NewSingleDestination(qsEndpoint)
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	cmd := newReviewCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.ParseFlags([]string{"--base", "HEAD^", "--head", "HEAD", "--config-dir", configDir}))
	require.NoError(t, runReview(cmd, cmd.Flags().Args()))
	waitQualitySignalInFlight()
	client.Wait()
	return atomic.LoadInt32(builds)
}

// TestReviewCmd_ConfigDirWorktreeCannotOptIntoQualitySignal: under --config-dir
// a reviewed tree that sets quality_signal: true does not start a quality-signal
// send; the config directory's (unset) opt-in decides.
func TestReviewCmd_ConfigDirWorktreeCannotOptIntoQualitySignal(t *testing.T) {
	require.Equal(t, int32(0), runReviewSendWithConfigDir(t, "quality_signal: true\n", ""),
		"the reviewed tree's quality_signal must not opt the run in under --config-dir")
}

// TestReviewCmd_ConfigDirQualitySignalOptInHonored is the control for the test
// above: the config directory's own opt-in does start the send, so a zero there
// means the gate said no, not that the harness cannot see a send.
func TestReviewCmd_ConfigDirQualitySignalOptInHonored(t *testing.T) {
	require.Equal(t, int32(1), runReviewSendWithConfigDir(t, "", "quality_signal: true\n"),
		"the config directory's quality_signal opt-in must start the send")
}
