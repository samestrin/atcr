package main

import (
	"bytes"
	"context"
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
