package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGateThreshold_Precedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()

	// Nothing configured anywhere → no gate (opt-in stays opt-in).
	v, err := ResolveGateThreshold(root, "")
	require.NoError(t, err)
	assert.Equal(t, "", v)

	// Explicit value passes through raw (enum validation is the caller's).
	v, err = ResolveGateThreshold(root, " high ")
	require.NoError(t, err)
	assert.Equal(t, "high", v)

	// Registry tier (user-global, lowest file tier).
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\nagents:\n  a:\n    provider: p\n    model: m\nfail_on: LOW\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))
	v, err = ResolveGateThreshold(root, "")
	require.NoError(t, err)
	assert.Equal(t, "LOW", v)

	// Project config overrides the registry tier.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nfail_on: MEDIUM\n"), 0o644))
	v, err = ResolveGateThreshold(root, "")
	require.NoError(t, err)
	assert.Equal(t, "MEDIUM", v)

	// A present-but-broken project config is an error (the repo's own config),
	// never a silent skip.
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte("agents: []\n"), 0o644))
	_, err = ResolveGateThreshold(root, "")
	require.Error(t, err)
}
