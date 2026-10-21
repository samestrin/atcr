package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.20: secret_guard selects what happens when the egress scan finds a
// credential in a review prompt (off/warn/redact/block). It resolves once per
// run through the same registry → project chain as on_overflow.

func TestSecretGuardValid(t *testing.T) {
	for _, v := range []string{"off", "warn", "redact", "block", "", "  "} {
		assert.True(t, secretGuardValid(v), "%q should be config-valid (all four policies + unset)", v)
	}
	for _, v := range []string{"yolo", "WARN", "deny", "redacted", "on"} {
		assert.False(t, secretGuardValid(v), "%q should be rejected", v)
	}
}

func TestLoadProjectConfig_SecretGuard_DefaultUnset(t *testing.T) {
	// No secret_guard key: the loader leaves it unset ("") — defaulting to
	// "warn" happens in ResolveSettings, not the loader.
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	assert.Equal(t, "", cfg.SecretGuard)
}

func TestLoadProjectConfig_SecretGuard_ExplicitValid(t *testing.T) {
	for _, v := range []string{"off", "warn", "redact", "block"} {
		t.Run(v, func(t *testing.T) {
			cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nsecret_guard: "+v+"\n"))
			require.NoError(t, err)
			assert.Equal(t, v, cfg.SecretGuard)
		})
	}
}

func TestLoadProjectConfig_SecretGuard_Invalid(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nsecret_guard: yolo\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret_guard")
	assert.Contains(t, err.Error(), "yolo")
	assert.Contains(t, err.Error(), "off, warn, redact, block")
	assert.Contains(t, err.Error(), "config.yaml", "the load error names the offending file")
}

func TestLoadProjectConfig_SecretGuard_TypoKey_StrictModeRejects(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nsecret_gaurd: block\n"))
	require.Error(t, err)
}

func TestRegistry_SecretGuard_ExplicitValid(t *testing.T) {
	for _, v := range []string{"off", "warn", "redact", "block"} {
		t.Run(v, func(t *testing.T) {
			reg := loadRegistryWith(t, "secret_guard: "+v+"\n")
			assert.Equal(t, v, reg.SecretGuard)
		})
	}
}

func TestRegistryValidate_SecretGuard_Invalid(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
secret_guard: yolo
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret_guard")
	assert.Contains(t, err.Error(), "off, warn, redact, block")
}

func TestResolveSettings_SecretGuard_DefaultsToWarn(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultSecretGuard, s.SecretGuard)
	assert.Equal(t, "warn", s.SecretGuard)
}

func TestResolveSettings_SecretGuard_EmptyResolvesToWarn(t *testing.T) {
	reg := loadRegistryWith(t, "secret_guard: \"\"\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nsecret_guard: \"\"\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "warn", s.SecretGuard, "an empty value at every tier falls through to the default")
}

func TestResolveSettings_SecretGuard_RegistryOverridesDefault(t *testing.T) {
	reg := loadRegistryWith(t, "secret_guard: redact\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "redact", s.SecretGuard, "registry global wins over embedded default")
}

func TestResolveSettings_SecretGuard_ProjectOverridesRegistry(t *testing.T) {
	reg := loadRegistryWith(t, "secret_guard: redact\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nsecret_guard: block\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "block", s.SecretGuard, "project config wins over registry")
}

func TestResolveSettings_SecretGuard_ProjectOffOverridesRegistry(t *testing.T) {
	// "off" is a real value, not "unset": it must win over a stricter registry tier.
	s, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{SecretGuard: "off"}, &Registry{SecretGuard: "block"})
	require.NoError(t, err)
	assert.Equal(t, "off", s.SecretGuard)
}

func TestResolveSettings_SecretGuard_WhitespaceTreatedAsUnset(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{SecretGuard: "   "}, &Registry{SecretGuard: "  "})
	require.NoError(t, err)
	assert.Equal(t, "warn", s.SecretGuard)
}

func TestResolveSettings_SecretGuard_DirectlyConstructedInvalidRejected(t *testing.T) {
	// A directly-constructed proj (bypassing the file loader) with an
	// out-of-range value is caught by the post-resolution sanity check.
	_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{SecretGuard: "turbo"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret_guard")
	assert.Contains(t, err.Error(), "off, warn, redact, block")
}

// secret_guard in the DEFINITIONS overlay (.atcr/registry.yaml) is a misplaced
// shared setting; the strict decoder must redirect it to .atcr/config.yaml.
func TestLoadProjectRegistry_SecretGuardKeyHint(t *testing.T) {
	root := t.TempDir()
	writeProjectRegistry(t, root, "providers: {}\nagents: {}\nsecret_guard: block\n")
	_, err := LoadProjectRegistry(DefaultProjectRegistryPath(root))
	require.Error(t, err, "strict decoder must reject shared-settings keys in the overlay")
	assert.Contains(t, err.Error(), "secret_guard")
	assert.Contains(t, err.Error(), "config.yaml")
}

// The config `atcr init` installs emits secret_guard at its default with a help
// comment, and still round-trips through the strict loader.
func TestDefaultProjectConfigYAML_DocumentsSecretGuard(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"bruce"})
	assert.Contains(t, out, "\nsecret_guard: warn\n")
	assert.Contains(t, out, "# secret_guard:")
	for _, v := range []string{"off", "warn", "redact", "block"} {
		assert.Contains(t, out, v)
	}

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	path := DefaultProjectConfigPath(root)
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultSecretGuard, cfg.SecretGuard)
}
