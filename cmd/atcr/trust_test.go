package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTrustEnv points HOME at a temp dir and writes a project .atcr/registry.yaml
// defining one project provider, returning the trust store path under HOME.
func setupTrustEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	projReg := "" +
		"providers:\n" +
		"  team-llm:\n" +
		"    base_url: https://llm.team.example/v1\n" +
		"    api_key_env: TEAM_LLM_KEY\n" +
		"agents:\n" +
		"  reviewer:\n" +
		"    provider: team-llm\n" +
		"    model: m\n"
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "registry.yaml"), []byte(projReg), 0o644))

	return filepath.Join(home, ".config", "atcr", "trusted_providers.yaml")
}

func TestTrust_ListShowsUntrusted(t *testing.T) {
	setupTrustEnv(t)
	out, err := execute(t, "trust")
	require.NoError(t, err)
	assert.Contains(t, out, "team-llm")
	assert.Contains(t, out, "https://llm.team.example/v1")
	assert.Contains(t, out, "TEAM_LLM_KEY")
	assert.Contains(t, out, "UNTRUSTED")
}

func TestTrust_AuthorizesProvider(t *testing.T) {
	storePath := setupTrustEnv(t)
	out, err := execute(t, "trust", "team-llm")
	require.NoError(t, err)
	assert.Contains(t, out, "trusting team-llm")

	_, statErr := os.Stat(storePath)
	require.NoError(t, statErr, "trust store written under HOME")

	// listing now reports trusted
	out, err = execute(t, "trust")
	require.NoError(t, err)
	assert.Contains(t, out, "trusted")
	assert.NotContains(t, out, "UNTRUSTED")
}

func TestTrust_AllAuthorizesEveryProvider(t *testing.T) {
	setupTrustEnv(t)
	out, err := execute(t, "trust", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "trusting team-llm")
}

func TestTrust_UnknownProviderIsUsageError(t *testing.T) {
	setupTrustEnv(t)
	_, err := execute(t, "trust", "no-such-provider")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
}

func TestTrust_NoProjectProviders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir()) // no .atcr/registry.yaml
	out, err := execute(t, "trust")
	require.NoError(t, err)
	assert.Contains(t, out, "No project-defined providers")
}

// TestTrust_SummaryPhrasedAsNewEntries verifies that the trust summary message
// explicitly says "new" so a mixed run (some already-trusted, some new) does
// not mislead the user into thinking the count covers all trusted providers.
func TestTrust_SummaryPhrasedAsNewEntries(t *testing.T) {
	setupTrustEnv(t)
	out, err := execute(t, "trust", "team-llm")
	require.NoError(t, err)
	// The written-entry summary must qualify the count as "new" entries.
	assert.Contains(t, out, "new trust", "summary must say 'new trust' so mixed-run counts are unambiguous")
}

// TestTrust_AllWithArgsIsError verifies that --all and positional args are
// mutually exclusive: the user cannot combine them without a clear error.
func TestTrust_AllWithArgsIsError(t *testing.T) {
	setupTrustEnv(t)
	_, err := execute(t, "trust", "--all", "team-llm")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
}

// TestTrust_PartialFailureNoPrintBeforeValidation verifies that no "trusting X"
// lines are printed when a later argument is unknown: validation must run before
// any output or mutation so stdout never claims a write that was never persisted.
func TestTrust_PartialFailureNoPrintBeforeValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	// Two providers so we can reference a valid one first, then an unknown one.
	projReg := "providers:\n" +
		"  alpha:\n    base_url: https://alpha.example\n    api_key_env: KEY_A\n" +
		"  beta:\n    base_url: https://beta.example\n    api_key_env: KEY_B\n" +
		"agents:\n  r:\n    provider: alpha\n    model: m\n"
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "registry.yaml"), []byte(projReg), 0o644))

	out, err := execute(t, "trust", "alpha", "ghost")
	require.Error(t, err, "unknown provider must return error")
	assert.Equal(t, 2, exitCode(err))
	// No "trusting alpha" must have been printed before the unknown-name error.
	assert.NotContains(t, out, "trusting alpha", "output must not claim a write that was rolled back")
}

func TestTrust_GatesReviewUntilTrusted(t *testing.T) {
	// A project provider blocks `atcr review` config load until trusted; this is
	// the end-to-end security contract at the command boundary.
	setupTrustEnv(t)
	// Also need a user registry + project config so review reaches the gate.
	home, _ := os.UserHomeDir()
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"),
		[]byte("providers:\n  u:\n    api_key_env: U_KEY\nagents:\n  bruce:\n    provider: u\n    model: m\n"), 0o644))
	work, _ := os.Getwd()
	require.NoError(t, os.WriteFile(filepath.Join(work, ".atcr", "config.yaml"),
		[]byte("agents:\n  - reviewer\n"), 0o644))

	_, err := execute(t, "review")
	require.Error(t, err, "untrusted project provider blocks review")
	assert.Equal(t, 2, exitCode(err))
}
