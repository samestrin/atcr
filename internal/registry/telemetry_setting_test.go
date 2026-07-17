package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	t.Run("empty existing config gets a mapping", func(t *testing.T) {
		dir := writeConfigDir(t, "")
		require.NoError(t, SetTelemetrySetting(dir, false), "an empty (0-byte) config must accept an opt-out")
		got, err := LoadTelemetrySetting(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)
	})
	t.Run("non-mapping config is rejected", func(t *testing.T) {
		dir := writeConfigDir(t, "- a\n- b\n")
		err := SetTelemetrySetting(dir, false)
		require.Error(t, err, "a key cannot be set on a YAML list root")
	})
}

// TestSetTelemetrySetting_SymlinkRejected verifies a symlinked .atcr/config.yaml
// is rejected with a clear error instead of silently severed: the atomic
// os.Rename would replace the symlink with a regular file (Stat/ReadFile follow
// the link, Rename does not), writing to the wrong logical location.
func TestSetTelemetrySetting_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr"), 0o755))
	target := filepath.Join(dir, "real-config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("agents: [bruce]\ntelemetry: false\n"), 0o644))
	link := DefaultProjectConfigPath(dir)
	require.NoError(t, os.Symlink(target, link))

	err := SetTelemetrySetting(dir, true)
	require.Error(t, err, "a symlinked config must be rejected, not silently severed")
	assert.Contains(t, err.Error(), "symlink")

	// The symlink itself must survive the rejected write.
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0, "the symlink must remain a symlink")
}

// TestSetTelemetrySetting_PreservesValueInlineComment verifies an inline comment
// on the telemetry VALUE node survives a config-set. setMappingBool must mutate
// the existing value node in place (preserving its LineComment/FootComment) rather
// than swapping in a fresh node that drops the comment.
func TestSetTelemetrySetting_PreservesValueInlineComment(t *testing.T) {
	dir := writeConfigDir(t, "agents: [bruce]\ntelemetry: true  # forced on by CI\n")
	require.NoError(t, SetTelemetrySetting(dir, false))

	got, err := LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "telemetry must be flipped to false")

	data, err := os.ReadFile(DefaultProjectConfigPath(dir))
	require.NoError(t, err)
	assert.Contains(t, string(data), "forced on by CI", "the value node's inline comment must survive the surgical edit")
}

// TestSetTelemetrySetting_PreservesCommentOnlyStubComments covers the
// telemetry_setting.go:190 TD: a config file containing ONLY comments unmarshals
// to a document with empty Content but a non-empty HeadComment. configMapping must
// preserve those comments when it synthesizes the mapping for the appended key,
// honoring the "every other key and its comments survive" promise for the stub case.
func TestSetTelemetrySetting_PreservesCommentOnlyStubComments(t *testing.T) {
	dir := writeConfigDir(t, "# managed by ops - do not delete\n")
	require.NoError(t, SetTelemetrySetting(dir, false))

	got, err := LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "the opt-out must still be recorded on a comment-only stub")

	data, err := os.ReadFile(DefaultProjectConfigPath(dir))
	require.NoError(t, err)
	assert.Contains(t, string(data), "managed by ops",
		"a comment-only stub config's head comment must survive config set")
}

// TestSetTelemetrySetting_LocksRMW verifies SetTelemetrySetting acquires a
// mkdir-based lock under .atcr/ to serialize concurrent reads-modify-writes.
func TestSetTelemetrySetting_LocksRMW(t *testing.T) {
	dir := writeConfigDir(t, "agents: [bruce]\ntelemetry: true\n")
	lockDir := filepath.Join(dir, ".atcr", "config.lock")

	// Manually acquire the lock directory
	require.NoError(t, os.MkdirAll(lockDir, 0o755))

	// Write a dummy owner file
	ownerFile := filepath.Join(lockDir, "owner.txt")
	epoch := time.Now().Unix()
	require.NoError(t, os.WriteFile(ownerFile, []byte(fmt.Sprintf("session=test-holder|epoch=%d", epoch)), 0o644))

	lockReleased := make(chan struct{})
	go func() {
		// Hold the lock for 200ms, then release it
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(lockDir)
		close(lockReleased)
	}()

	// This call should block until the background goroutine releases the lock, then succeed.
	start := time.Now()
	err := SetTelemetrySetting(dir, false)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// Verify that it actually blocked and waited for the lock
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond, "SetTelemetrySetting should have waited for the lock to be released")

	// Verify the telemetry was successfully updated
	got, err := LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got)

	// Make sure the background goroutine finished
	<-lockReleased
}
