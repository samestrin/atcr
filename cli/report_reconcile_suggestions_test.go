package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
)

// Epic 42.0 T3: `atcr report --format axi` ends with a help[] block, and
// `atcr reconcile` prints human Next: lines on stderr. Both name `debt list` and
// `verify <id>`; no other report format and no reconcile stdout changes.

// wantReportHelp is the exact help[] block report --format axi appends.
const wantReportHelp = "help[2]: atcr debt list,atcr verify <id>\n"

// TestReportCmd_AXIEndsWithHelpBlock: the AXI payload is unchanged and only
// appended to (AC4), and the appended block names both next steps (AC1).
func TestReportCmd_AXIEndsWithHelpBlock(t *testing.T) {
	isolate(t)
	js := manyFindingsJSON(t, 3)
	fixtureReconciled(t, "r", js)
	code, stdout, _ := execCmdSplit(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)

	var fs []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal([]byte(js), &fs))
	var body bytes.Buffer
	require.NoError(t, report.RenderAXIPaginated(&body, fs, report.AXIMaxLinesDefault))
	assert.Equal(t, body.String()+wantReportHelp, stdout,
		"report --format axi is the unchanged payload followed by the help[] block")
}

// TestReportCmd_AXIHelpBlockReachesOutputFile: --output writes the same bytes
// stdout would carry, help[] block included.
func TestReportCmd_AXIHelpBlockReachesOutputFile(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	out := filepath.Join(t.TempDir(), "report.toon")
	require.Equal(t, 0, execCmd(t, "report", "--format", "axi", "--output", out, "r"))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "\ntruncated: false\n"+wantReportHelp),
		"the file ends with the help[] block: %q", data)
}

// TestReportCmd_NonAXIFormatsCarryNoHelp: md, json, checklist, sarif and pipe
// output is byte-identical to its renderer, with no help[] block appended.
func TestReportCmd_NonAXIFormatsCarryNoHelp(t *testing.T) {
	isolate(t)
	js := manyFindingsJSON(t, 3)
	fixtureReconciled(t, "r", js)
	var fs []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal([]byte(js), &fs))

	for _, format := range []string{report.FormatMarkdown, report.FormatJSON, report.FormatChecklist, report.FormatSarif, report.FormatPipe} {
		t.Run(format, func(t *testing.T) {
			code, stdout, _ := execCmdSplit(t, "report", "--format", format, "r")
			require.Equal(t, 0, code)
			assert.NotContains(t, stdout, "help[", "--format %s must not carry a help[] block", format)

			var want bytes.Buffer
			switch format {
			case report.FormatMarkdown:
				require.NoError(t, report.RenderMarkdownWithContested(&want, fs,
					reconcile.LoadDisagreements(filepath.Join(".atcr", "reviews", "r"), fs), report.ContestedReport{}))
			case report.FormatPipe:
				require.NoError(t, report.RenderPipeAXIPaginated(&want, fs, report.AXIMaxLinesDefault))
			default:
				require.NoError(t, report.Render(&want, fs, format))
			}
			assert.Equal(t, want.String(), stdout, "--format %s output is byte-identical to its renderer", format)
		})
	}
}

// TestReportCmd_LegacyPipeAXICarriesNoHelp: --format axi under ATCR_LEGACY_PIPE
// renders the legacy pipe encoder, which is pipe output and so gets no help[].
func TestReportCmd_LegacyPipeAXICarriesNoHelp(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, _ := execCmdSplit(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "findings[3|]{")
	assert.NotContains(t, stdout, "help[")
}

// TestReportCmd_FailedAXIRunCarriesNoHelp: no help[] block without output.
func TestReportCmd_FailedAXIRunCarriesNoHelp(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "report", "--format", "axi")
	require.NotEqual(t, 0, code)
	assert.NotContains(t, out, "help[")
}

// nextStepsReviewFixture is a one-finding review whose reconcile exits 0.
func nextStepsReviewFixture(t *testing.T) {
	t.Helper()
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
}

// TestReconcileCmd_NextLinesOnStderr: stderr ends with the Next: lines and
// stdout is exactly the pre-existing summary line.
func TestReconcileCmd_NextLinesOnStderr(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, stderr := execCmdSplit(t, "reconcile", "--no-scorecard", "--no-local-debt", "r")
	require.Equal(t, 0, code)
	assert.Equal(t, "reconciled 1 finding(s) from 1 source(s) -> "+filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout,
		"reconcile stdout is unchanged")
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr debt list\nNext: atcr verify <id>\n"),
		"stderr ends with the Next: lines: %q", stderr)
}

// TestReconcileCmd_NextLinesCarryRepoRoot: --repo-root is the one allowlisted
// carry (AC2); its value is carried, quoted when it is not a plain shell word.
// The suggested `debt list` names the store that root's findings persisted to
// (Epic 88 T1), not the CWD store a bare `debt list` would read.
func TestReconcileCmd_NextLinesCarryRepoRoot(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	root := filepath.Join(t.TempDir(), "my repo")
	require.NoError(t, os.MkdirAll(root, 0o755))
	code, _, stderr := execCmdSplit(t, "reconcile", "--no-scorecard", "--no-local-debt", "--repo-root", root, "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stderr, "Next: atcr debt list --store='"+filepath.Join(root, ".atcr", "debt")+"'\nNext: atcr verify <id> --repo-root='"+root+"'\n")
}

// TestReconcileCmd_NextLinesCarryDeprecatedRepoAlias: a value typed under the
// deprecated --repo alias carries forward under its canonical name.
func TestReconcileCmd_NextLinesCarryDeprecatedRepoAlias(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	root := t.TempDir()
	code, _, stderr := execCmdSplit(t, "reconcile", "--no-scorecard", "--no-local-debt", "--repo", root, "r")
	require.Equal(t, 0, code)
	assert.Contains(t, stderr, "Next: atcr debt list --store="+carryValue(filepath.Join(root, ".atcr", "debt"))+"\n")
	assert.Contains(t, stderr, "Next: atcr verify <id> --repo-root="+carryValue(root)+"\n")
}

// TestReconcileCmd_NextLinesAfterGateFailure: a tripped --fail-on gate still
// reconciled the findings, so the next steps still apply.
func TestReconcileCmd_NextLinesAfterGateFailure(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, _, stderr := execCmdSplit(t, "reconcile", "--no-scorecard", "--no-local-debt", "--fail-on", "LOW", "r")
	require.Equal(t, exitFailure, code)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr debt list\nNext: atcr verify <id>\n"),
		"stderr ends with the Next: lines: %q", stderr)
}

// TestReconcileCmd_NoNextLinesOnUsageError: a reconcile that never ran prints
// no next steps.
func TestReconcileCmd_NoNextLinesOnUsageError(t *testing.T) {
	isolate(t)
	code, stdout, stderr := execCmdSplit(t, "reconcile", "missing")
	require.Equal(t, exitUsage, code)
	assert.NotContains(t, stdout+stderr, "Next:")
}
