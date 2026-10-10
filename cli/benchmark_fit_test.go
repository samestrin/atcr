package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fitReportRow is one parsed data row of `atcr benchmark fit` output.
type fitReportRow struct {
	verdict, silent, findings, reason string
}

var fitColumnSep = regexp.MustCompile(`\s{2,}`)

// runFitReport runs `atcr benchmark fit` on a fixture and returns its rows keyed
// "persona + model". Columns are split on runs of two or more spaces (tabwriter's
// padding), since the "fit (warning)" verdict holds a single space.
func runFitReport(t *testing.T, fixture string) map[string]fitReportRow {
	t.Helper()
	code, out, stderr := execCmdSplit(t, "benchmark", "fit", "--in", filepath.Join("testdata", "benchmark_fit", fixture))
	require.Equal(t, 0, code, out+stderr)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines)
	require.True(t, strings.HasPrefix(lines[0], "PERSONA"), out)
	rows := map[string]fitReportRow{}
	for _, line := range lines[1:] {
		cols := fitColumnSep.Split(strings.TrimSpace(line), -1)
		require.Len(t, cols, 12, line)
		rows[cols[0]+" + "+cols[1]] = fitReportRow{verdict: cols[2], silent: cols[8], findings: cols[9], reason: cols[11]}
	}
	return rows
}

// The fixtures are shaped like epic 35.16.11.2.2.2's T1 results table (one case, the
// 4c588b6b..c08f657c range, two chunks per persona). They encode that epic's
// verdicts, not its raw silent counts: its "silent chunk" was any NO FINDINGS chunk,
// where silent_chunks is token-based (see the epic 35.16.11.2.2.8 Risks).
func TestBenchmarkFit_ReproducesEpic22_2Verdicts(t *testing.T) {
	want := map[string]map[string]string{
		"t1-settings.json": {
			"archer-strict + qwen3.8-flash": fitVerdictFit,   // a-strict, the archer winner
			"archer + qwen3.8-flash":        fitVerdictUnfit, // a-off, unparseable 1/3
			"mira-strict + llm-large":       fitVerdictFit,   // m-strict
			"ronin + nemotron-3-super-120b": fitVerdictUnfit, // r-unset, truncated 3/3
			"pace + nemotron-3-super-120b":  fitVerdictFit,   // p-unset: healthy calls, 2,1,1; findings are not gated
		},
		"t1-json-mode.json": {
			"archer + qwen3.8-flash": fitVerdictUnfit, // a-json, replicate 3 silent on both chunks
		},
		"t1b-ronin-repoints.json": {
			"ronin + minimax-m2.7":     fitVerdictWarn,  // r-mm, the ronin-backup winner
			"ronin + gpt-oss-120b":     fitVerdictUnfit, // r-gpt, unparseable 1/2
			"ronin + glm-5.2":          fitVerdictUnfit, // r-g52, truncated 2/2
			"ronin + nemotron-3-ultra": fitVerdictUnfit, // r-ul, HTTP 408 timeouts
		},
		"t1c-pace-candidates.json": {
			"pace + gpt-oss-120b":     fitVerdictFit,   // p-gpt, the pace winner
			"pace + minimax-m2.7":     fitVerdictUnfit, // p-mm, silent
			"pace + nemotron-3-ultra": fitVerdictWarn,  // p-ul, see TestBenchmarkFit_PULReading
		},
	}
	for fixture, pairs := range want {
		t.Run(fixture, func(t *testing.T) {
			rows := runFitReport(t, fixture)
			assert.Len(t, rows, len(pairs))
			for pair, verdict := range pairs {
				require.Contains(t, rows, pair)
				assert.Equal(t, verdict, rows[pair].verdict, pair)
			}
		})
	}

	// The four pairs the epic's T3 success criterion names.
	assert.Equal(t, fitVerdictUnfit, runFitReport(t, "t1-settings.json")["ronin + nemotron-3-super-120b"].verdict)
	assert.Equal(t, fitVerdictUnfit, runFitReport(t, "t1c-pace-candidates.json")["pace + minimax-m2.7"].verdict)
	assert.Equal(t, fitVerdictWarn, runFitReport(t, "t1b-ronin-repoints.json")["ronin + minimax-m2.7"].verdict)
	assert.Equal(t, fitVerdictFit, runFitReport(t, "t1-settings.json")["archer-strict + qwen3.8-flash"].verdict)
}

// p-ul (pace + nemotron-3-ultra) found 5 findings, then 0, with 1 silent chunk.
// Epic 35.16.11.2.2.2 called it "fails (silent)". The reading this fixture pins: the
// silent chunk fell on replicate 2, whose other chunk was a full, non-silent clean
// review, so no replicate was silent on every chunk and no call was truncated,
// unparseable or failed. Under the health-only rule the board chose (2026-10-10, Q2
// any-bad-or-whole-silent) that is a warning and the pair stays fit; its weak
// findings are reported, not gated. This is a deliberate divergence from 2.2.2.
func TestBenchmarkFit_PULReading(t *testing.T) {
	row := runFitReport(t, "t1c-pace-candidates.json")["pace + nemotron-3-ultra"]
	assert.Equal(t, fitVerdictWarn, row.verdict)
	assert.Equal(t, "5,0", row.findings)
	assert.Equal(t, "1/4", row.silent)
	assert.Equal(t, "1/4 chunks silent", row.reason)
}

func TestBenchmarkFit_ReasonsNameTheFailures(t *testing.T) {
	rows := runFitReport(t, "t1b-ronin-repoints.json")
	assert.Equal(t, "timed out 2/2", rows["ronin + nemotron-3-ultra"].reason)
	assert.Equal(t, "unparseable 1/2", rows["ronin + gpt-oss-120b"].reason)
	assert.Equal(t, "16,16", rows["ronin + minimax-m2.7"].findings)
	assert.Equal(t, "silent on every chunk 3/5", runFitReport(t, "t1c-pace-candidates.json")["pace + minimax-m2.7"].reason)
}

func writeFitRunResult(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestBenchmarkFit_NoFitRowsIsAnError(t *testing.T) {
	in := writeFitRunResult(t, `{"suite":"s","suite_version":"1","reviewers":[]}`)
	code, out := execCmdCapture(t, "benchmark", "fit", "--in", in)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "no reviewer_fit rows")
}

// An outcome this binary does not know fails the report instead of reading as a
// healthy call.
func TestBenchmarkFit_UnknownOutcomeFailsClosed(t *testing.T) {
	in := writeFitRunResult(t, `{"suite":"s","suite_version":"1","reviewer_fit":[`+
		`{"model":"m","persona":"p","case_id":"c","replicate":1,"outcome":"silent","findings":0,"tokens_out":5,"chunk_count":1,"silent_chunks":0}]}`)
	code, out := execCmdCapture(t, "benchmark", "fit", "--in", in)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, `outcome "silent"`)
}

func TestBuildFitReport_Rules(t *testing.T) {
	rows, err := buildFitReport([]benchmark.ReviewerFit{
		// A single-chunk reviewer silent on its only chunk is wholly silent.
		{Model: "m1", Persona: "p", CaseID: "a", Replicate: 1, Outcome: benchmark.OutcomeClean, ChunkCount: 1, SilentChunks: 1},
		// A row with no chunk count reads as one chunk, so its silent chunk is not a minority.
		{Model: "m2", Persona: "p", CaseID: "a", Replicate: 1, Outcome: benchmark.OutcomeClean, SilentChunks: 1},
		// Findings are summed over cases per replicate; incomplete (byte budget) is not a call-health failure.
		{Model: "m3", Persona: "p", CaseID: "a", Replicate: 1, Outcome: benchmark.OutcomeFindings, Findings: 2, TokensOut: 10, ChunkCount: 1},
		{Model: "m3", Persona: "p", CaseID: "b", Replicate: 1, Outcome: benchmark.OutcomeIncomplete, Findings: 3, TokensOut: 20, ChunkCount: 1},
		{Model: "m3", Persona: "p", CaseID: "a", Replicate: 2, Outcome: benchmark.OutcomeClean, ChunkCount: 1},
		// A plain failure is counted as failed, not timed out.
		{Model: "m4", Persona: "p", CaseID: "a", Replicate: 1, Outcome: benchmark.OutcomeFailed},
	})
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, fitVerdictUnfit, rows[0].Verdict)
	assert.Equal(t, []string{"silent on every chunk 1/1"}, rows[0].Reasons)
	assert.Equal(t, fitVerdictUnfit, rows[1].Verdict)
	assert.Equal(t, fitVerdictFit, rows[2].Verdict)
	assert.Equal(t, []int{5, 0}, rows[2].FindingsByReplicate)
	assert.Equal(t, []int{30, 0}, rows[2].TokensOutByReplicate)
	assert.Equal(t, 3, rows[2].Calls)
	assert.Equal(t, fitVerdictUnfit, rows[3].Verdict)
	assert.Equal(t, 1, rows[3].Failed)
	assert.Equal(t, 0, rows[3].TimedOut)

	_, err = buildFitReport([]benchmark.ReviewerFit{{Model: "m", Persona: "p", CaseID: "a", Replicate: 0, Outcome: benchmark.OutcomeClean}})
	require.ErrorContains(t, err, "replicate 0")
}
