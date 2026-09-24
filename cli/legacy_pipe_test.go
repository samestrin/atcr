package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonEmptyLines splits s into trimmed, non-empty lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

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

// resumeLegacyPipeCase runs one legacy-pipe resume scenario: pending (agents
// still to fan out) or all-complete (short-circuit re-reconcile), selected by the
// --legacy-pipe flag or the ATCR_LEGACY_PIPE=1 env switch (TD:
// cli/legacy_pipe_test.go:94 — review/resume parity is a stated contract, so the
// switch must work at BOTH resume write sites, resume.go:254 and :308).
func resumeLegacyPipeCase(t *testing.T, allComplete bool, viaFlag bool) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	if allComplete {
		liveReviewConfig(t, srv.URL, "bruce")
		require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))
	} else {
		liveReviewConfig(t, srv.URL, "bruce", "robin")
		base := gitRevParse(t, "HEAD^")
		head := gitRevParse(t, "HEAD")
		writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})
	}
	if !viaFlag {
		t.Setenv("ATCR_LEGACY_PIPE", "1")
	}
	code, stdout, stderr := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--legacy-pipe", "--base", "HEAD^")
	require.Equal(t, 0, code, "resume completes -> exit 0")
	assert.Contains(t, stdout, "review_summary[1|]{", "legacy switch must select the pipe encoder on the resume path")
	assert.NotContains(t, stdout, "review_summary[1]{", "standard TOON header must not appear")
	assert.Equal(t, 1, strings.Count(stderr, wantPipeDeprecation), "exactly one deprecation notice per invocation")
	assert.NotContains(t, stdout, "deprecated")
}

// TestResume_LegacyPipePendingFlag covers the pending-agent resume write site
// (resume.go:308) with the --legacy-pipe flag.
func TestResume_LegacyPipePendingFlag(t *testing.T) {
	resumeLegacyPipeCase(t, false, true)
}

// TestResume_LegacyPipePendingEnv covers the same site via ATCR_LEGACY_PIPE=1 —
// the flag is absent, so only the env switch can select the pipe encoder.
func TestResume_LegacyPipePendingEnv(t *testing.T) {
	resumeLegacyPipeCase(t, false, false)
}

// TestResume_LegacyPipeAllCompleteFlag covers the already-complete resume write
// site (resume.go:254) with the flag.
func TestResume_LegacyPipeAllCompleteFlag(t *testing.T) {
	resumeLegacyPipeCase(t, true, true)
}

// TestResume_LegacyPipeAllCompleteEnv covers the same site via the env switch.
func TestResume_LegacyPipeAllCompleteEnv(t *testing.T) {
	resumeLegacyPipeCase(t, true, false)
}

// TestReviewCmd_LegacyPipeEnvSelectsPipe covers `review --axi` under
// ATCR_LEGACY_PIPE=1: the env switch alone (no flag) must select the legacy pipe
// run-summary encoder on the fresh-review path.
func TestReviewCmd_LegacyPipeEnvSelectsPipe(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "review_summary[1|]{")
	assert.NotContains(t, stdout, "review_summary[1]{")
	assert.Equal(t, 1, strings.Count(stderr, wantPipeDeprecation))
}

// TestResume_LegacyPipePayloadShapeMatchesReview is the legacy variant of
// TestResume_AXIPayloadShapeMatchesReview: `review --axi --legacy-pipe` and the
// pending-agent `review --resume --axi --legacy-pipe` must emit the identical
// pipe run-summary header line.
func TestResume_LegacyPipePayloadShapeMatchesReview(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")

	_, reviewOut, _ := execCmdSplit(t, "review", "--axi", "--legacy-pipe", "--base", "HEAD^")
	reviewHeader := firstLine(reviewOut)
	require.Contains(t, reviewHeader, "review_summary[1|]{")

	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})
	_, resumeOut, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--legacy-pipe", "--base", "HEAD^")
	resumeHeader := firstLine(resumeOut)

	assert.Equal(t, reviewHeader, resumeHeader,
		"legacy-pipe review and resume must emit the identical run-summary payload header")
}

// TestLegacyPipeNoticeHonorsLogFormatJSON pins the NDJSON contract: under
// --log-format json, every stderr line parses as a JSON object — the pipe
// deprecation notice must ride the structured logger, not raw fmt.Fprintln
// (TD: cli/main.go:447).
func TestLegacyPipeNoticeHonorsLogFormatJSON(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, _, stderr := execCmdSplit(t, "--log-format", "json", "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	lines := nonEmptyLines(stderr)
	require.NotEmpty(t, lines, "deprecation notice must reach stderr")
	found := false
	for _, line := range lines {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v),
			"stderr line must be valid NDJSON under --log-format json: %q", line)
		msg, _ := v["msg"].(string)
		if strings.Contains(msg, "pipe-delimited AXI output is deprecated") {
			found = true
		}
	}
	assert.True(t, found, "deprecation notice must ride the structured logger in json mode")
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
