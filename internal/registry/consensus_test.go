package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveConsensus_ProjectRegistryOverlayHasNoEffect mirrors the fail_on
// boundary test: the project registry overlay (.atcr/registry.yaml) carries only
// providers and agents, so a consensus level must never be sourced from it.
func TestResolveConsensus_ProjectRegistryOverlayHasNoEffect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()

	atcrDir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	overlayYAML := "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\nagents:\n  a:\n    provider: p\n    model: m\n"
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "registry.yaml"), []byte(overlayYAML), 0o644))

	v, err := ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "", v, "project registry overlay must not contribute consensus")
}

// TestResolveConsensus_Precedence walks the documented chain: explicit >
// project config (.atcr/config.yaml) > user-global registry > "". The embedded
// DefaultConsensus is deliberately NOT applied inside the resolver (the
// ResolveGateThreshold shape) — the call site maps "" to strict.
func TestResolveConsensus_Precedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()

	// Nothing configured anywhere → "" (the call site maps it to strict).
	v, err := ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "", v)

	// Explicit value passes through raw+trimmed (enum validation is the caller's).
	v, err = ResolveConsensus(root, " lenient ")
	require.NoError(t, err)
	assert.Equal(t, "lenient", v)

	// Registry tier (user-global, lowest file tier).
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\nagents:\n  a:\n    provider: p\n    model: m\nconsensus: off\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))
	v, err = ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "off", v)

	// Project config overrides the registry tier.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nconsensus: lenient\n"), 0o644))
	v, err = ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "lenient", v)

	// An explicit value still beats the project config (AC3).
	v, err = ResolveConsensus(root, "off")
	require.NoError(t, err)
	assert.Equal(t, "off", v)

	// A present-but-broken project config is an error (the repo's own config),
	// never a silent skip.
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte("agents: []\n"), 0o644))
	_, err = ResolveConsensus(root, "")
	require.Error(t, err)
}

// TestResolveConsensus_ProjectConfigWithoutKeyFallsThrough pins the path every
// repository initialized before epic 35.9.1 takes: a project config that is
// present but carries no consensus key (or only whitespace) must fall through
// to the user-global registry tier, not count as "project tier decided".
func TestResolveConsensus_ProjectConfigWithoutKeyFallsThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()

	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\nagents:\n  a:\n    provider: p\n    model: m\nconsensus: off\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))

	// Project config present WITHOUT a consensus key → registry tier decides.
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\n"), 0o644))
	v, err := ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "off", v, "keyless project config must fall through to the user-global registry")

	// A whitespace-only project value is not a decision either.
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nconsensus: \"   \"\n"), 0o644))
	v, err = ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "off", v, "whitespace-only project value must fall through to the user-global registry")
}

// TestResolveConsensus_BrokenUserRegistrySkipped documents the asymmetry the
// gate resolver established: a broken user-global registry is skipped
// best-effort so it never blocks a reconcile that does not otherwise need it.
func TestResolveConsensus_BrokenUserRegistrySkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()

	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"),
		[]byte("providers: [this is not a map\n"), 0o644))

	v, err := ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "", v)
}

// TestDefaultProjectConfigYAML_SeedsConsensus pins AC4: `atcr init` writes
// `consensus: strict` with a levels-comment next to fail_on.
func TestDefaultProjectConfigYAML_SeedsConsensus(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"a"})
	assert.Contains(t, out, "consensus: "+DefaultConsensus)
	assert.Equal(t, "strict", DefaultConsensus)
	assert.Contains(t, out, "# consensus:", "the levels-comment must document the knob")
	assert.Contains(t, out, "lenient")
	assert.Contains(t, out, "off")

	// The template must still round-trip through the strict loader.
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	path := DefaultProjectConfigPath(root)
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultConsensus, cfg.Consensus)
}

// TestSharedSettingsKeys_IncludesConsensus pins the misplaced-key hint
// behaviorally: a consensus key in .atcr/registry.yaml must be told where it
// belongs, exactly as fail_on is. (Asserts the user-facing contract, not the
// private sharedSettingsKeys slice that merely implements it.)
func TestSharedSettingsKeys_IncludesConsensus(t *testing.T) {
	root := t.TempDir()
	atcrDir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	overlayYAML := "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\nagents:\n  a:\n    provider: p\n    model: m\nconsensus: off\n"
	overlayPath := filepath.Join(atcrDir, "registry.yaml")
	require.NoError(t, os.WriteFile(overlayPath, []byte(overlayYAML), 0o644))

	_, err := LoadProjectRegistry(overlayPath)
	require.Error(t, err, "a consensus key in the project overlay must be rejected")
	assert.Contains(t, err.Error(), "consensus")
	assert.Contains(t, err.Error(), ".atcr/config.yaml", "the hint must redirect the key to the project config")
}
