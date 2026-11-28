package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// homeAXITelemetryCell runs bare `atcr --axi` and returns the telemetry cell of
// the home row plus the captured stderr. The cell is the last comma-separated
// field of the single data row (on/off never contains a comma, so LastIndex is
// safe even though the quoted description carries commas).
func homeAXITelemetryCell(t *testing.T) (cell, stderr string) {
	t.Helper()
	code, stdout, stderr := execCmdSplit(t, "--axi")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	body, _, ok := strings.Cut(stdout, "help[")
	require.True(t, ok, "home --axi output carries a help[] block: %q", stdout)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	require.Len(t, lines, 2, "home payload is header + one row: %q", body)
	require.Equal(t, "home[1]{exec_path,description,review_id,review_status,telemetry}:", lines[0],
		"the home header declares the telemetry column (epic 165 T4)")
	idx := strings.LastIndex(lines[1], ",")
	require.Greater(t, idx, 0, "home row: %q", lines[1])
	return lines[1][idx+1:], stderr
}

// TestHomeAXI_TelemetryDefaultOn pins the disclosure's default state: with no
// opt-out anywhere, the AXI home view reports telemetry on — the same default-on
// posture the usage ping itself has. The suite-wide consent pin
// (telemetrytest.PinConsentEnv) sets ATCR_TELEMETRY=0, so the test unsets it
// per-test; the home view only READS the gate and never sends.
func TestHomeAXI_TelemetryDefaultOn(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	cell, stderr := homeAXITelemetryCell(t)
	assert.Equal(t, "on", cell, "no opt-out means the default-on ping is disclosed as on")
	assert.NotContains(t, stderr, "warning:", "a clean env produces no warning")
}

// TestHomeAXI_TelemetryEnvOptOut: ATCR_TELEMETRY=0 alone turns the reported
// state off.
func TestHomeAXI_TelemetryEnvOptOut(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "0")
	cell, _ := homeAXITelemetryCell(t)
	assert.Equal(t, "off", cell)
}

// TestHomeAXI_TelemetryConfigOptOut: the persisted config opt-out
// (`atcr config set telemetry false`) alone turns the reported state off.
func TestHomeAXI_TelemetryConfigOptOut(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	writeAtcrConfig(t, "telemetry: false\n")
	cell, _ := homeAXITelemetryCell(t)
	assert.Equal(t, "off", cell)
}

// TestHomeAXI_TelemetryEnvOptOutBeatsConfigTrue pins the OR-disables precedence
// on the home view: config true can never re-enable a ping the env disabled.
func TestHomeAXI_TelemetryEnvOptOutBeatsConfigTrue(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "0")
	writeAtcrConfig(t, "telemetry: true\n")
	cell, _ := homeAXITelemetryCell(t)
	assert.Equal(t, "off", cell, "env opt-out wins over config true")
}

// TestHomeAXI_TelemetryConfigTrueKeepsOn: an explicit config true with no env
// opt-out stays on.
func TestHomeAXI_TelemetryConfigTrueKeepsOn(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	writeAtcrConfig(t, "telemetry: true\n")
	cell, _ := homeAXITelemetryCell(t)
	assert.Equal(t, "on", cell)
}

// TestHomeAXI_TelemetryMalformedConfigFailsSafe: a corrupt persisted value fails
// safe to disabled on the home view exactly as it does at the send gate — a
// corrupt value can never re-enable (or claim) a ping the user may have disabled.
func TestHomeAXI_TelemetryMalformedConfigFailsSafe(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	writeAtcrConfig(t, "telemetry: maybe\n")
	cell, _ := homeAXITelemetryCell(t)
	assert.Equal(t, "off", cell, "a malformed config value fails safe to disabled")
}

// TestHomeAXI_TelemetryUnparseableEnvFailsOpenAndWarns: an unrecognized
// ATCR_TELEMETRY value fails OPEN to the documented default (on) and the
// misspelled-opt-out warning stays visible on stderr — the home view is the only
// gate read on a bare invocation, so it routes the warning to cmd.ErrOrStderr()
// rather than swallowing it.
func TestHomeAXI_TelemetryUnparseableEnvFailsOpenAndWarns(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "flase")
	cell, stderr := homeAXITelemetryCell(t)
	assert.Equal(t, "on", cell, "an unparseable env value fails open to the default-on posture")
	assert.Contains(t, stderr, "unrecognized ATCR_TELEMETRY", "a misspelled opt-out warns on stderr")
}

// TestHomeAXI_LegacyPipeHomeHasNoTelemetryColumn pins that the deprecated pipe
// home payload stays byte-identical: the telemetry column reaches only the
// standard TOON encoder (the frozen legacy golden in internal/report proves the
// same at the renderer layer).
func TestHomeAXI_LegacyPipeHomeHasNoTelemetryColumn(t *testing.T) {
	isolate(t)
	code, stdout, _ := execCmdSplit(t, "--axi", "--legacy-pipe")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "home[1|]{exec_path|description|review_id|review_status}:",
		"the legacy pipe home header keeps its frozen four columns")
	assert.NotContains(t, stdout, "telemetry", "the legacy pipe payload never gains the telemetry column")
}

// TestHomeHuman_TelemetryNotShown: the human home view is unchanged by the
// disclosure — telemetry is an AXI-only field (T1: humans on a TTY see no change).
func TestHomeHuman_TelemetryNotShown(t *testing.T) {
	isolate(t)
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	code, stdout, stderr := execCmdSplit(t)
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout+stderr, "telemetry", "the human home view does not render the telemetry field")
}
