package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadQualitySignalSetting_AbsentFileReturnsNilNil verifies the neutral cases
// of the roster-independent reader the opt-in gate uses: an absent file, an empty
// file, and a present file lacking the quality_signal key all return (nil, nil).
func TestLoadQualitySignalSetting_AbsentFileReturnsNilNil(t *testing.T) {
	t.Run("absent file is neutral", func(t *testing.T) {
		got, err := LoadQualitySignalSetting(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, got, "a missing config file contributes nothing to the gate")
	})
	t.Run("present file without the key is neutral", func(t *testing.T) {
		got, err := LoadQualitySignalSetting(writeConfigDir(t, "agents: [bruce]\ntelemetry: true\n"))
		require.NoError(t, err)
		assert.Nil(t, got, "a config without a quality_signal key is neutral, not disabled")
	})
	t.Run("empty file is neutral", func(t *testing.T) {
		got, err := LoadQualitySignalSetting(writeConfigDir(t, ""))
		require.NoError(t, err)
		assert.Nil(t, got, "an empty (0-byte) config file is neutral")
	})
}

// TestSetQualitySignalSetting_RoundTrip verifies persistence round-trips through
// LoadQualitySignalSetting for both true and false, on a config that starts
// without the key.
func TestSetQualitySignalSetting_RoundTrip(t *testing.T) {
	dir := writeConfigDir(t, "agents: [bruce]\n")

	require.NoError(t, SetQualitySignalSetting(dir, true))
	got, err := LoadQualitySignalSetting(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, *got)

	require.NoError(t, SetQualitySignalSetting(dir, false))
	got, err = LoadQualitySignalSetting(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got)
}

// TestSetQualitySignalSetting_SiblingKeyPreserved proves the two keys are fully
// independent at the persistence layer: setting quality_signal leaves telemetry
// (and every other key) untouched, and setting telemetry leaves quality_signal
// untouched — the surgical yaml-node edit never rewrites siblings.
func TestSetQualitySignalSetting_SiblingKeyPreserved(t *testing.T) {
	dir := writeConfigDir(t, "agents:\n  - bruce\ntelemetry: false\npayload_mode: blocks\n")

	require.NoError(t, SetQualitySignalSetting(dir, true))

	tel, err := LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, tel)
	assert.False(t, *tel, "telemetry must survive a quality_signal set")
	cfg, err := LoadProjectConfig(DefaultProjectConfigPath(dir))
	require.NoError(t, err)
	assert.Equal(t, []string{"bruce"}, cfg.Agents)
	assert.Equal(t, "blocks", cfg.PayloadMode)

	// Converse: setting telemetry must leave quality_signal untouched.
	require.NoError(t, SetTelemetrySetting(dir, true))
	qs, err := LoadQualitySignalSetting(dir)
	require.NoError(t, err)
	require.NotNil(t, qs)
	assert.True(t, *qs, "quality_signal must survive a telemetry set")
}

// TestLoadQualitySignalSetting_MalformedValueFailsSafeToDisabled verifies a
// corrupt persisted value surfaces an error (which the gate maps to disabled),
// never silently falls to enabled.
func TestLoadQualitySignalSetting_MalformedValueFailsSafeToDisabled(t *testing.T) {
	_, err := LoadQualitySignalSetting(writeConfigDir(t, "agents: [bruce]\nquality_signal: maybe\n"))
	require.Error(t, err, "a corrupt quality_signal value must surface an error, not silently fall to enabled")
}

// TestSetQualitySignalSetting_SymlinkRejected verifies a symlinked config is
// rejected (never silently severed by the atomic rename), mirroring
// SetTelemetrySetting's existing guard.
func TestSetQualitySignalSetting_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr"), 0o755))
	target := filepath.Join(dir, "real-config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("agents: [bruce]\n"), 0o644))
	link := DefaultProjectConfigPath(dir)
	require.NoError(t, os.Symlink(target, link))

	err := SetQualitySignalSetting(dir, true)
	require.Error(t, err, "a symlinked config must be rejected, not silently severed")
	assert.Contains(t, err.Error(), "symlink")

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0, "the symlink must remain a symlink")
}
