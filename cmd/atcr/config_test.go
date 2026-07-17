package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

// execConfig runs the atcr command tree with args and returns the combined
// out+err output, the resolved exit code, and the raw error. The root sets
// SilenceErrors, so an error's message reaches the returned error (as main()
// sees it), not the output buffer — message assertions use the error.
func execConfig(t *testing.T, args ...string) (string, int, error) {
	t.Helper()
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), exitCode(err), err
}

func TestConfigCmd_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		names[c.Name()] = true
	}
	assert.True(t, names["config"], "config command must be registered on the root")
}

// TestConfig_NoSubcommandPrintsHelp mirrors newDebtCmd's RunE: cmd.Help so a
// bare `atcr config` prints help and exits 0 (AC 02-02 Scenario 3).
func TestConfig_NoSubcommandPrintsHelp(t *testing.T) {
	out, code, _ := execConfig(t, "config")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "set", "bare `atcr config` help must mention the set subcommand")
}

func TestConfigSetTelemetry_PersistsFalse(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents:\n  - bruce\npayload_mode: blocks\n")

	out, code, _ := execConfig(t, "config", "set", "telemetry", "false")
	require.Equal(t, 0, code, "config set telemetry false must succeed: %s", out)

	got, err := registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "config set telemetry false must persist telemetry: false")
}

func TestConfigSetTelemetry_TrueReenables(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\n")

	_, code, _ := execConfig(t, "config", "set", "telemetry", "true")
	require.Equal(t, 0, code)

	got, err := registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, *got)
}

// TestConfigSetTelemetry_AcceptsParseBoolVocabulary covers AC 02-02 EC2: the
// value axis accepts exactly strconv.ParseBool's set (0/1/True/etc.).
func TestConfigSetTelemetry_AcceptsParseBoolVocabulary(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, _ := execConfig(t, "config", "set", "telemetry", "0")
	require.Equal(t, 0, code)
	got, err := registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "`0` must be accepted as false")

	_, code, _ = execConfig(t, "config", "set", "telemetry", "True")
	require.Equal(t, 0, code)
	got, err = registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, *got, "`True` must be accepted as true")
}

func TestConfigSet_RejectsUnknownKey(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, err := execConfig(t, "config", "set", "foo", "bar")
	assert.Equal(t, 2, code, "an unknown config key is a usage error (exit 2)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported config key "foo"`)
}

func TestConfigSet_RejectsNonBoolValue(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, err := execConfig(t, "config", "set", "telemetry", "maybe")
	assert.Equal(t, 2, code, "a non-boolean value is a usage error (exit 2)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid value "maybe"`)
}

func TestConfigSet_WrongArgCount(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, _ := execConfig(t, "config", "set", "telemetry")
	assert.Equal(t, 2, code, "too few args is a usage error (exit 2)")

	_, code, _ = execConfig(t, "config", "set", "telemetry", "false", "extra")
	assert.Equal(t, 2, code, "too many args is a usage error (exit 2)")
}

// TestConfigSet_MissingConfigFileIsIOError covers AC 02-02 Error Scenario 4: a
// missing .atcr/config.yaml is an environment failure (exit 1), NOT a usage
// error (exit 2) — config set never silently creates the file.
func TestConfigSet_MissingConfigFileIsIOError(t *testing.T) {
	isolate(t) // fresh cwd, no .atcr/ directory

	_, code, _ := execConfig(t, "config", "set", "telemetry", "false")
	assert.Equal(t, 1, code, "a missing config file is an I/O error (exit 1), not a usage error")
}

// TestConfigSetTelemetry_ResolvesRepoRoot covers the cwd-independence fix: when
// run from a subdirectory of the repo, config set should locate .atcr/config.yaml
// via the repo root rather than failing with a cwd-relative path.
func TestConfigSetTelemetry_ResolvesRepoRoot(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))
	subdir := filepath.Join(repo, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	t.Chdir(subdir)

	_, code, _ := execConfig(t, "config", "set", "telemetry", "false")
	require.Equal(t, 0, code, "config set from a repo subdirectory must succeed")

	got, err := registry.LoadTelemetrySetting(repo)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "telemetry must be persisted at the discovered repo root")
}

// TestConfigSetQualitySignal_PersistsTrueFalse covers AC 02-03 Scenarios 1-2: the
// allowlist admits quality_signal and both true and false round-trip through
// LoadQualitySignalSetting.
func TestConfigSetQualitySignal_PersistsTrueFalse(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	out, code, _ := execConfig(t, "config", "set", "quality_signal", "true")
	require.Equal(t, 0, code, "config set quality_signal true must succeed: %s", out)
	got, err := registry.LoadQualitySignalSetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, *got, "config set quality_signal true must persist quality_signal: true")

	_, code, _ = execConfig(t, "config", "set", "quality_signal", "false")
	require.Equal(t, 0, code)
	got, err = registry.LoadQualitySignalSetting(".")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, *got, "config set quality_signal false must persist quality_signal: false")
}

// TestConfigSetQualitySignal_SiblingKeyUntouched covers AC 02-03 Scenarios 3-4:
// the allowlist extension does not regress single-key behavior — setting one key
// leaves the other untouched in either direction.
func TestConfigSetQualitySignal_SiblingKeyUntouched(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\npayload_mode: blocks\n")

	_, code, _ := execConfig(t, "config", "set", "quality_signal", "true")
	require.Equal(t, 0, code)
	tel, err := registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, tel)
	assert.False(t, *tel, "telemetry must survive a quality_signal set")

	_, code, _ = execConfig(t, "config", "set", "telemetry", "true")
	require.Equal(t, 0, code)
	qs, err := registry.LoadQualitySignalSetting(".")
	require.NoError(t, err)
	require.NotNil(t, qs)
	assert.True(t, *qs, "quality_signal must survive a telemetry set")
}

// TestConfigSetQualitySignal_UnknownKeyStillRejected proves the allowlist is an
// explicit two-entry set, not a loosened match: an unknown key is still a usage
// error (exit 2) after the extension.
func TestConfigSetQualitySignal_UnknownKeyStillRejected(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, err := execConfig(t, "config", "set", "foo", "bar")
	assert.Equal(t, 2, code, "an unknown config key is still a usage error (exit 2)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported config key "foo"`)
}

// TestConfigSetQualitySignal_NonBooleanValueRejected covers AC 02-03 Error
// Scenario 2: a non-boolean value for quality_signal is a usage error (exit 2)
// with a key-specific message.
func TestConfigSetQualitySignal_NonBooleanValueRejected(t *testing.T) {
	isolate(t)
	writeAtcrConfig(t, "agents: [bruce]\n")

	_, code, err := execConfig(t, "config", "set", "quality_signal", "maybe")
	assert.Equal(t, 2, code, "a non-boolean value is a usage error (exit 2)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid value "maybe" for quality_signal`)
}

// TestConfigSetQualitySignal_ResolvesRepoRoot covers AC 02-03 EC2: config set
// quality_signal works from a repo subdirectory, persisting at the discovered root.
func TestConfigSetQualitySignal_ResolvesRepoRoot(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))
	subdir := filepath.Join(repo, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	t.Chdir(subdir)

	_, code, _ := execConfig(t, "config", "set", "quality_signal", "true")
	require.Equal(t, 0, code, "config set from a repo subdirectory must succeed")

	got, err := registry.LoadQualitySignalSetting(repo)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, *got, "quality_signal must be persisted at the discovered repo root")
}
