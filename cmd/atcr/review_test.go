package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/require"
)

// slotWithKeys builds a one-slot chain whose agents read the given env vars.
func slotWithKeys(envs ...string) fanout.Slot {
	s := fanout.Slot{Primary: fanout.Agent{Invocation: llmclient.Invocation{APIKeyEnv: envs[0]}}}
	for _, e := range envs[1:] {
		s.Fallbacks = append(s.Fallbacks, fanout.Agent{Invocation: llmclient.Invocation{APIKeyEnv: e}})
	}
	return s
}

func TestCLIOverrides_MaxParallelSet(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--max-parallel", "3"}))
	o := cliOverrides(cmd)
	require.NotNil(t, o.MaxParallel, "--max-parallel must populate the override")
	require.Equal(t, 3, *o.MaxParallel)
}

func TestCLIOverrides_MaxParallelUnset(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	o := cliOverrides(cmd)
	require.Nil(t, o.MaxParallel, "an unset flag must not override lower tiers")
}

func TestPreflightAPIKeys_AllChainsKeylessIsUsageError(t *testing.T) {
	// AC 03-02 Error Scenario 1: a missing API key is a configuration error
	// (exit 2) naming the env var — never the gate's exit 1.
	err := preflightAPIKeys([]fanout.Slot{
		slotWithKeys("ATCR_TEST_KEY_A", "ATCR_TEST_KEY_B"),
		slotWithKeys("ATCR_TEST_KEY_C"),
	})
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "API key env var not set")
	require.Contains(t, err.Error(), "ATCR_TEST_KEY_A")
	require.Contains(t, err.Error(), "ATCR_TEST_KEY_C")
}

func TestPreflightAPIKeys_KeyedFallbackRescuesRun(t *testing.T) {
	// Partial success is binding (original-requirements: exit 0 if ≥1 agent
	// succeeds), so the pre-flight must pass when ANY agent in any slot's
	// chain can authenticate — keys resolve per-invocation at run time.
	t.Setenv("ATCR_TEST_KEY_FB", "k")
	require.NoError(t, preflightAPIKeys([]fanout.Slot{
		slotWithKeys("ATCR_TEST_KEY_A", "ATCR_TEST_KEY_FB"),
	}))
}

// writeReviewFixtureConfig writes a user registry (under the isolated HOME)
// and a project .atcr/config.yaml with a single agent whose provider key env
// is never set.
func writeReviewFixtureConfig(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`providers:
  testprov:
    api_key_env: ATCR_TEST_REVIEW_KEY
    base_url: http://127.0.0.1:1/v1
agents:
  bruce:
    provider: testprov
    model: test-model
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))
}

// initGitRepoWithChange creates a repo in the current dir with two commits
// that differ in file content, so payload building has a real diff.
func initGitRepoWithChange(t *testing.T) {
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
	require.NoError(t, os.WriteFile("a.txt", []byte("one\n"), 0o644))
	run("add", "a.txt")
	run("commit", "-q", "-m", "init")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\ntwo\n"), 0o644))
	run("add", "a.txt")
	run("commit", "-q", "-m", "second")
}

func TestReviewCmd_MissingAPIKeyIsUsageError(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t)
	require.Equal(t, 2, execCmd(t, "review", "--base", "HEAD^"))
}

func TestOutputDirFromFlags_Unset(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	require.Equal(t, "", dir, "unset --output-dir must yield empty (default review dir)")
}

func TestOutputDirFromFlags_MutuallyExclusiveWithID(t *testing.T) {
	// AC: --output-dir and --id together are a usage error (exit 2).
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "/tmp/x", "--id", "y"}))
	_, err := outputDirFromFlags(cmd)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
}

func TestOutputDirFromFlags_RelativeResolvedToAbs(t *testing.T) {
	// A relative --output-dir resolves against CWD at flag-parse time.
	isolate(t)
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "out/review"}))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(dir), "relative path must resolve to absolute, got %q", dir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cwd, "out", "review"), dir)
}

func TestOutputDirFromFlags_EmptyValueIsUsageError(t *testing.T) {
	// An explicit empty --output-dir is a usage error, not a scaffold into CWD.
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", ""}))
	_, err := outputDirFromFlags(cmd)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
}

func TestOutputDirFromFlags_WhitespacePaddedResolvesTrimmed(t *testing.T) {
	// The validated value (TrimSpace) must equal the resolved absolute path;
	// a leading-space input must not produce a path with a literal space component.
	isolate(t)
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "  out"}))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	require.Equal(t, filepath.Join(cwd, "out"), dir, "leading spaces must be trimmed before filepath.Abs")
}

// TestReviewCmd_VerifyFlagsRegistered verifies the --verify chaining flags exist
// on reviewCmd with the documented defaults (AC 04-02).
func TestReviewCmd_VerifyFlagsRegistered(t *testing.T) {
	cmd := newReviewCmd()
	for _, name := range []string{"verify", "fresh", "thorough", "min-severity"} {
		require.NotNil(t, cmd.Flags().Lookup(name), "review must define --%s", name)
	}
	v, err := cmd.Flags().GetBool("verify")
	require.NoError(t, err)
	require.False(t, v, "--verify defaults to false")
}

// TestReviewCmd_VerifyInvalidMinSeverity verifies `review --verify` validates
// --min-severity before any review work, failing fast as a usage error (exit 2).
func TestReviewCmd_VerifyInvalidMinSeverity(t *testing.T) {
	isolate(t) // empty CWD: no git repo, but validation runs before range resolution
	code, out := execCmdCapture(t, "review", "--verify", "--min-severity", "BLOCKER")
	require.Equal(t, 2, code)
	require.Contains(t, out, "CRITICAL")
}
