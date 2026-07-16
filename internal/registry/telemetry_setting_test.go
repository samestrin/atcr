package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectConfig_TelemetryPointerRoundTrip verifies the new Telemetry *bool
// field decodes as a pointer so an explicit false survives (the Sandbox/AutoFix
// pointer idiom) and an absent key stays nil (neutral / default-enabled).
func TestProjectConfig_TelemetryPointerRoundTrip(t *testing.T) {
	t.Run("explicit false", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntelemetry: false\n"))
		require.NoError(t, err)
		require.NotNil(t, cfg.Telemetry, "explicit telemetry: false must survive as a non-nil pointer")
		assert.False(t, *cfg.Telemetry)
	})
	t.Run("explicit true", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntelemetry: true\n"))
		require.NoError(t, err)
		require.NotNil(t, cfg.Telemetry)
		assert.True(t, *cfg.Telemetry)
	})
	t.Run("absent key stays nil", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
		require.NoError(t, err)
		assert.Nil(t, cfg.Telemetry, "an absent telemetry key must decode to nil (neutral)")
	})
	t.Run("malformed value rejected by strict load", func(t *testing.T) {
		_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntelemetry: maybe\n"))
		require.Error(t, err, "a non-boolean telemetry value must not silently fall open to enabled")
	})
}

// writeConfigDir writes content to <dir>/.atcr/config.yaml and returns dir, so a
// test can point LoadTelemetrySetting / SetTelemetrySetting (which resolve
// .atcr/config.yaml under a root) at a hermetic fixture.
func writeConfigDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(dir), []byte(content), 0o644))
	return dir
}

// TestLoadTelemetrySetting verifies the lightweight, roster-independent reader
// the opt-out gate uses: absent file -> (nil, nil) neutral; present true/false
// -> the pointer; a malformed value -> an error (never silently nil/enabled).
func TestLoadTelemetrySetting(t *testing.T) {
	t.Run("absent file is neutral", func(t *testing.T) {
		got, err := LoadTelemetrySetting(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, got, "a missing config file contributes nothing to the gate")
	})
	t.Run("present false", func(t *testing.T) {
		got, err := LoadTelemetrySetting(writeConfigDir(t, "agents: [bruce]\ntelemetry: false\n"))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)
	})
	t.Run("present true", func(t *testing.T) {
		got, err := LoadTelemetrySetting(writeConfigDir(t, "agents: [bruce]\ntelemetry: true\n"))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, *got)
	})
	t.Run("field absent is neutral", func(t *testing.T) {
		got, err := LoadTelemetrySetting(writeConfigDir(t, "agents: [bruce]\npayload_mode: blocks\n"))
		require.NoError(t, err)
		assert.Nil(t, got, "a config without a telemetry key is neutral, not disabled")
	})
	t.Run("malformed value errors", func(t *testing.T) {
		_, err := LoadTelemetrySetting(writeConfigDir(t, "agents: [bruce]\ntelemetry: maybe\n"))
		require.Error(t, err, "a corrupt telemetry value must surface an error, not silently fall open")
	})
	t.Run("ignores unknown sibling keys", func(t *testing.T) {
		// The reader is permissive about the rest of the file (unlike strict
		// LoadProjectConfig) so it can resolve telemetry without a valid roster.
		got, err := LoadTelemetrySetting(writeConfigDir(t, "totally_unknown_key: 1\ntelemetry: false\n"))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)
	})
}

// TestSetTelemetrySetting verifies config-set persistence: it mutates only the
// telemetry key, preserves sibling keys, is idempotent, and errors when the
// config file is absent (an environment failure, not a usage error).
func TestSetTelemetrySetting(t *testing.T) {
	t.Run("sets false on an existing config, preserving siblings", func(t *testing.T) {
		dir := writeConfigDir(t, "agents:\n  - bruce\npayload_mode: blocks\n")
		require.NoError(t, SetTelemetrySetting(dir, false))

		got, err := LoadTelemetrySetting(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)

		// Sibling keys survive the surgical edit.
		cfg, err := LoadProjectConfig(DefaultProjectConfigPath(dir))
		require.NoError(t, err)
		assert.Equal(t, []string{"bruce"}, cfg.Agents)
		assert.Equal(t, "blocks", cfg.PayloadMode)
	})
	t.Run("true re-enables and round-trips", func(t *testing.T) {
		dir := writeConfigDir(t, "agents: [bruce]\ntelemetry: false\n")
		require.NoError(t, SetTelemetrySetting(dir, true))
		got, err := LoadTelemetrySetting(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, *got)
	})
	t.Run("idempotent", func(t *testing.T) {
		dir := writeConfigDir(t, "agents: [bruce]\ntelemetry: false\n")
		require.NoError(t, SetTelemetrySetting(dir, false))
		require.NoError(t, SetTelemetrySetting(dir, false))
		got, err := LoadTelemetrySetting(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)
	})
	t.Run("missing file is an error", func(t *testing.T) {
		err := SetTelemetrySetting(t.TempDir(), false)
		require.Error(t, err, "config set must not silently create a config; a missing file is an I/O error")
	})
}
