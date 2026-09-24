package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantPipeDeprecation is the stderr notice every legacy pipe route emits.
// Each route appends " (enabled by <trigger>)" before the newline, so the
// constant pins only the shared prefix.
const wantPipeDeprecation = "warning: pipe-delimited AXI output is deprecated and will be removed in a future release; migrate to standard TOON."

// TestLegacyPipeNoticeNamesTrigger pins that the deprecation notice says HOW
// legacy pipe was enabled, so an env-switch user can turn it off without
// reading the source (TD: cli/axi.go:34).
func TestLegacyPipeNoticeNamesTrigger(t *testing.T) {
	code, stdout, stderr := execCmdSplit(t, "--axi", "--legacy-pipe")
	require.Equal(t, 0, code)
	assert.Contains(t, stderr, "(enabled by --legacy-pipe")
	assert.NotContains(t, stdout, "deprecated")

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr = execCmdSplit(t, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stderr, "(enabled by ATCR_LEGACY_PIPE")
	assert.NotContains(t, stdout, "deprecated")
}

// TestReportCmd_FormatPipeEmitsLegacyWithDeprecation is AC3: `--format pipe`
// routes to the legacy pipe encoder (paginated, with the old header-N contract)
// and warns on stderr, never on stdout.
func TestReportCmd_FormatPipeEmitsLegacyWithDeprecation(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	code, stdout, stderr := execCmdSplit(t, "report", "--format", "pipe", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "findings[3|]{", "legacy pipe header")
	assert.Contains(t, stdout, "truncated: false\n")
	assert.NotContains(t, stdout, "total:", "the legacy path keeps its frozen shape")
	assert.Contains(t, stderr, wantPipeDeprecation)
	assert.NotContains(t, stdout, "deprecated")
}

// TestReportCmd_LegacyPipeEnvReroutesAXI is clarification 1: ATCR_LEGACY_PIPE=1
// is a global switch that re-routes `report --format axi` to the legacy encoder.
func TestReportCmd_LegacyPipeEnvReroutesAXI(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "findings[3|]{")
	assert.Contains(t, stderr, wantPipeDeprecation)
}

// TestReportCmd_LegacyPipeEnvLeavesNonAXIAlone pins that the env switch touches
// the AXI surface only.
func TestReportCmd_LegacyPipeEnvLeavesNonAXIAlone(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "report", "--format", "md", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "# atcr Review Report")
	assert.NotContains(t, stderr, "deprecated")
}

// TestLegacyPipeEnvBadValueWarns pins that an unparseable ATCR_LEGACY_PIPE
// value warns on stderr and fails open to standard TOON, mirroring
// axiMaxLinesFromEnv's unrecognized-value warning (TD: cli/axi.go:47).
func TestLegacyPipeEnvBadValueWarns(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	t.Setenv("ATCR_LEGACY_PIPE", "yes")
	code, stdout, stderr := execCmdSplit(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "findings[3]{", "unparseable value fails open to standard TOON")
	assert.NotContains(t, stdout, "findings[3|]{")
	assert.Contains(t, stderr, `unrecognized ATCR_LEGACY_PIPE value "yes"`)
}

// TestReportCmd_AXIDefaultIsStandardWithoutWarning pins the default: standard
// TOON on stdout and no deprecation notice.
func TestReportCmd_AXIDefaultIsStandardWithoutWarning(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	code, stdout, stderr := execCmdSplit(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "findings[3]{")
	assert.NotContains(t, stderr, "deprecated")
}

// TestRootCmd_HomeLegacyPipe covers bare `atcr --axi` with the flag and with the
// env switch, and the default standard form.
func TestRootCmd_HomeLegacyPipe(t *testing.T) {
	code, stdout, stderr := execCmdSplit(t, "--axi", "--legacy-pipe")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "home[1|]{", "--legacy-pipe emits the pipe home payload")
	assert.Contains(t, stderr, wantPipeDeprecation)

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr = execCmdSplit(t, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "home[1|]{", "ATCR_LEGACY_PIPE=1 emits the pipe home payload")
	assert.Contains(t, stderr, wantPipeDeprecation)

	t.Setenv("ATCR_LEGACY_PIPE", "")
	code, stdout, stderr = execCmdSplit(t, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "home[1]{", "default is standard TOON")
	assert.NotContains(t, stderr, "deprecated")
}

// TestRootCmd_LegacyPipeWithoutAXIIsSilent pins that the switch only affects AXI
// output: the human home view carries no deprecation notice.
func TestRootCmd_LegacyPipeWithoutAXIIsSilent(t *testing.T) {
	code, _, stderr := execCmdSplit(t, "--legacy-pipe")
	require.Equal(t, 0, code)
	assert.NotContains(t, stderr, "deprecated")
}

// TestReviewCmd_LegacyPipeEmitsPipeSummary covers `review --axi --legacy-pipe`:
// the run summary uses the pipe encoder, the notice goes to stderr, and the exit
// code is unchanged (AC6).
func TestReviewCmd_LegacyPipeEmitsPipeSummary(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--legacy-pipe", "--base", "HEAD^")
	require.Equal(t, 0, code, "legacy pipe does not change the exit code")
	assert.Contains(t, stdout, "review_summary[1|]{")
	assert.Contains(t, stderr, wantPipeDeprecation)
	assert.NotContains(t, stdout, "deprecated")
}
