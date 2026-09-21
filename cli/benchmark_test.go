package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// writeValidSuite writes a minimal valid benchmark suite (manifest + one diff
// file) into a fresh temp dir and returns its path.
func writeValidSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case-01.diff"),
		[]byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new // planted defect\n"), 0o600))
	manifest := `{"suite":"mini","suite_version":"1.2.0","cases":[` +
		`{"id":"case-01","diff":"case-01.diff","expected_categories":["correctness"]}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(manifest), 0o600))
	return dir
}

func TestBenchmarkVerify_ValidSuite(t *testing.T) {
	dir := writeValidSuite(t)
	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", dir)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "mini")
	require.Contains(t, out, "1.2.0")
	require.Contains(t, out, "1 case")
	require.Regexp(t, "[0-9a-f]{64}", out, "verify must print the 64-hex reproducibility hash")
}

func TestBenchmarkVerify_InvalidSuiteFails(t *testing.T) {
	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", t.TempDir())
	require.NotEqual(t, 0, code, "a directory without suite.json must fail verify: %s", out)
}

// `benchmark verify` is the documented suite-author pre-flight — its entire job is
// validation — so it must not print "valid" for a manifest `benchmark run` hard-
// rejects at load. The publishable-case-id gate is wired only into
// executeBenchmarkRun today, which makes the earliest surface an author touches the
// one surface that does not apply it. Third-party suites are supported, and the
// bundled importer's <owner>-<repo>-pr-<n> id shape is exactly what the scrub
// rewrites.
func TestBenchmarkVerify_RejectsSuiteThatRunWouldReject(t *testing.T) {
	for _, tc := range []struct{ name, id, want string }{
		{"id that scrubs away", "admin@internal.host-widgets-pr-1", "empty once scrubbed"},
		{"id carrying a non-printing rune", "acme-\u202Ecorp-pr-1", "non-printing rune"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "case-01.diff"),
				[]byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"), 0o600))
			manifest := `{"suite":"mini","suite_version":"1.2.0","cases":[` +
				`{"id":` + strconv.Quote(tc.id) + `,"diff":"case-01.diff","expected_categories":["correctness"]}]}`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(manifest), 0o600))

			code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", dir)
			require.NotEqual(t, 0, code,
				"verify must not report a run-rejecting suite as valid: %s", out)
			require.Contains(t, out, tc.want,
				"verify and run must give the SAME diagnostic for the same manifest")
		})
	}
}

// verify is the suite-author pre-flight, and `benchmark run` now routes BOTH
// tiers. A repo-state author whose only validation path is `benchmark run` has no
// free one at all — run proceeds straight into a paid panel. So verify must route
// on the same discriminator run does rather than hard-rejecting the tier the tool
// implements.
func TestBenchmarkVerify_AcceptsARepoStateSuite(t *testing.T) {
	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", repoStateMiniPath)
	require.Equal(t, 0, code, "verify must validate the tier `benchmark run` executes: %s", out)
	require.Contains(t, out, "repo-state-v1")
	require.Contains(t, out, "valid")
}

// The validation has to be REAL, not a discriminator check that prints "valid" for
// anything carrying the right suite string.
func TestBenchmarkVerify_RejectsAnInvalidRepoStateSuite(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"),
		[]byte(`{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"gone","dir":"gone"}]}`), 0o600))

	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", dir)
	require.NotEqual(t, 0, code, "a repo-state suite whose case directory is missing must fail verify: %s", out)
}

func TestBenchmarkVerify_RequiresSuitePath(t *testing.T) {
	code, _ := execCmdCapture(t, "benchmark", "verify")
	require.NotEqual(t, 0, code, "verify without --suite-path is a usage error")
}

func writeRunResult(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewers":[{"model":"claude-sonnet-4-6","persona":"bruce","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
		`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// Uses execCmdSplit, not execCmdCapture: this run-result predates coverage, so
// export emits an unverifiable-coverage warning on STDERR. The two streams are
// separate in the real CLI, and conflating them here would make "export stdout must
// be valid JSON" assert something the command never actually does.
func TestBenchmarkExport_ProducesSuiteTaggedJSON(t *testing.T) {
	in := writeRunResult(t)
	code, out, stderr := execCmdSplit(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out+stderr)

	var sub struct {
		SubmissionSchema int    `json:"submission_schema"`
		Source           string `json:"source"`
		Suite            string `json:"suite"`
		SuiteVersion     string `json:"suite_version"`
		Reviewers        []struct {
			Persona string `json:"persona"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub), "export stdout must be valid JSON: %s", out)
	require.Equal(t, 2, sub.SubmissionSchema)
	require.Equal(t, "benchmark-suite", sub.Source, "distinct from production export")
	require.Equal(t, "mini", sub.Suite)
	require.Equal(t, "1.2.0", sub.SuiteVersion)
	require.Len(t, sub.Reviewers, 1)
	require.Equal(t, "bruce", sub.Reviewers[0].Persona)
	// Distinct from production --export: the production envelope has no source/suite.
	require.NotContains(t, out, `"filters"`)
}

func TestBenchmarkExport_OutputFlagWritesFile(t *testing.T) {
	in := writeRunResult(t)
	dest := filepath.Join(t.TempDir(), "nested", "submission.json")
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--output", dest)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "submission_schema", "JSON goes to the file, not stdout")

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "benchmark-suite")
}

func TestBenchmarkExport_UsesGeneratedAtForSubmittedAt(t *testing.T) {
	in := writeRunResult(t)
	code, out, stderr := execCmdSplit(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out+stderr)

	var sub struct {
		SubmittedAt string `json:"submitted_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub))
	// The run-result has generated_at="2026-06-24T12:00:00Z" (see writeRunResult).
	// submitted_at must match it for reproducibility, not be time.Now().
	require.Equal(t, "2026-06-24T12:00:00Z", sub.SubmittedAt,
		"submitted_at must use run-result's generated_at for reproducibility")
}

func TestBenchmarkExport_RejectsWhitespaceOnlySuiteFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":" ","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewers":[{"model":"claude-sonnet-4-6","persona":"bruce","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
		`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "whitespace-only suite must fail: %s", out)
	require.Contains(t, out, "missing suite/suite_version")
}

func TestBenchmarkExport_RejectsEmptyReviewers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z","reviewers":[]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "empty reviewers must fail: %s", out)
	require.Contains(t, out, "no reviewers")
}

// A reviewer row with an empty (or whitespace-only) model or persona is an
// unidentifiable row: it would publish to a public leaderboard with no
// identity. Export must reject it the same way it rejects an empty suite
// identity, rather than emitting the row at exit 0.
func TestBenchmarkExport_RejectsEmptyReviewerIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		model   string
		persona string
	}{
		{name: "empty model", model: "", persona: "bruce"},
		{name: "whitespace model", model: "  ", persona: "bruce"},
		{name: "empty persona", model: "claude-sonnet-4-6", persona: ""},
		{name: "whitespace persona", model: "claude-sonnet-4-6", persona: " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run-result.json")
			body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
				`"reviewers":[{"model":` + strconv.Quote(tc.model) + `,"persona":` + strconv.Quote(tc.persona) + `,"runs":2,` +
				`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
				`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
			require.NotEqual(t, 0, code, "%s must fail export: %s", tc.name, out)
			require.Contains(t, out, "empty model/persona")
		})
	}
}

// A hand-supplied run-result carrying an out-of-range out_of_vocabulary_rate is
// rejected rather than accepted as a measurement. The value is a RATE — a share of
// findings — so anything outside [0,1] is not a pessimistic reading, it is a
// corrupt file, and export is the boundary where a hand-authored run-result first
// enters the tool. In range (including a real 0.0 and a real 1.0) still exports.
func TestBenchmarkExport_RejectsOutOfRangeVocabularyRate(t *testing.T) {
	const reviewers = `"reviewers":[{"model":"m","persona":"p","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,"latency_p50_ms":8900}]`

	for _, tc := range []struct {
		name    string
		rate    string
		wantErr bool
	}{
		{name: "above one", rate: "1.5", wantErr: true},
		{name: "negative", rate: "-0.25", wantErr: true},
		{name: "exactly zero", rate: "0.0"},
		{name: "exactly one", rate: "1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run-result.json")
			body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
				`"out_of_vocabulary_rate":` + tc.rate + `,` + reviewers + `}`
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
			if tc.wantErr {
				require.NotEqual(t, 0, code, "rate %s must fail export: %s", tc.rate, out)
				require.Contains(t, out, "out_of_vocabulary_rate")
				return
			}
			require.Equal(t, 0, code, "rate %s is a legal measurement: %s", tc.rate, out)
		})
	}
}

func TestBenchmarkExport_MissingInputFails(t *testing.T) {
	code, out := execCmdCapture(t, "benchmark", "export", "--in", filepath.Join(t.TempDir(), "nope.json"))
	require.NotEqual(t, 0, code, "a missing run-result file must fail export: %s", out)
}

func TestBenchmarkExport_RequiresInput(t *testing.T) {
	code, _ := execCmdCapture(t, "benchmark", "export")
	require.NotEqual(t, 0, code, "export without --in is a usage error")
}

// The --checkpoint flag routes through writeExportFile, which replaces a symlink
// at the target path rather than following it. That behavior must be documented
// in the flag help for parity with --out/--output.
func TestBenchmarkRunCmd_CheckpointHelpMentionsSymlink(t *testing.T) {
	cmd := newBenchmarkRunCmd()
	f := cmd.Flags().Lookup("checkpoint")
	require.NotNil(t, f, "benchmark run exposes a --checkpoint flag")
	require.Contains(t, f.Usage, "symlink", "checkpoint help must document symlink replace-not-follow behavior")
}

// The two case-failure exit flags are inert on standard-v1: executeBenchmarkRun
// never populates CaseFailures, so caseFailureExitGate returns nil on the empty
// slice no matter how the flags are set. Their help must say so — scoped to
// repo-state-v1 like --max-consecutive-case-failures — rather than promising a
// tolerance the standard tier does not offer.
func TestBenchmarkRunCmd_CaseFailureFlagsDeclareTierScope(t *testing.T) {
	cmd := newBenchmarkRunCmd()
	for _, name := range []string{"fail-on-case-failure", "max-case-failures"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f, "benchmark run exposes a --%s flag", name)
		require.Contains(t, f.Usage, "repo-state-v1 only",
			"--%s help must scope its exit contract to repo-state-v1: the flag is inert on standard-v1, whose runner never populates case_failures", name)
	}
}

// --output is the canonical run-result destination flag (matching benchmark
// export and every other output-destination flag); --out remains a deprecated
// hidden alias resolving identically.
func TestBenchmarkRunCmd_OutputCanonicalOutAlias(t *testing.T) {
	_, helpOut := execCmdCapture(t, "benchmark", "run", "--help")
	require.Contains(t, helpOut, "--output")
	require.NotContains(t, helpOut, "--out ")

	// The production resolution rule, mirrored: canonical --output wins, the
	// deprecated --out alias is honored when --output is unset.
	resolveOut := func(cmd *cobra.Command) string {
		out, _ := cmd.Flags().GetString("output")
		if !cmd.Flags().Changed("output") && cmd.Flags().Changed("out") {
			out, _ = cmd.Flags().GetString("out")
		}
		return out
	}

	canonical := newBenchmarkRunCmd()
	require.NoError(t, canonical.Flags().Parse([]string{"--suite-path", "s", "--output", "a.json"}))
	alias := newBenchmarkRunCmd()
	require.NoError(t, alias.Flags().Parse([]string{"--suite-path", "s", "--out", "a.json"}))
	require.Equal(t, resolveOut(alias), resolveOut(canonical),
		"--out and --output must resolve to the same destination")
}

// pflag's UnquoteUsage reads the first backquoted span in a usage string as the
// flag's value name, so backquotes around the example command in --in's usage
// would render `--in atcr benchmark run` instead of `--in string`.
func TestBenchmarkExport_InFlagRendersStringValueName(t *testing.T) {
	_, out := execCmdCapture(t, "benchmark", "export", "--help")
	require.Contains(t, out, "--in string",
		"--in must render its real value name, not a backquoted example command")
	require.NotContains(t, out, "--in atcr benchmark run")
}

// The DEFAULT exit contract is unchanged: a partial run still exits 0, because it is
// a real measurement of the cases that ran and the run-result records which ones did
// not. The two opt-in flags exist for the one caller that cannot read a stderr
// warning -- a CI step gating on the exit code -- and each is evaluated against the
// same failure count.
func TestCaseFailureExitGate(t *testing.T) {
	// A 10-case suite, parameterised by how many of them were lost.
	run := func(failed int) *benchmark.RunResult {
		rr := &benchmark.RunResult{}
		for i := 1; i <= 10; i++ {
			id := "case-" + strconv.Itoa(i)
			rr.SuiteCaseIDs = append(rr.SuiteCaseIDs, id)
			if i <= failed {
				rr.CaseFailures = append(rr.CaseFailures,
					benchmark.CaseFailure{CaseID: id, Reason: benchmark.CaseFailurePrepare})
			}
		}
		return rr
	}

	for _, tc := range []struct {
		name      string
		failed    int
		failOnAny bool
		maxFail   int
		wantErr   bool
	}{
		{"default tolerates one failure", 1, false, -1, false},
		{"default tolerates half", 5, false, -1, false},
		{"default tolerates all but one", 9, false, -1, false},
		{"default on a clean run", 0, false, -1, false},

		{"fail-on-case-failure rejects one", 1, true, -1, true},
		{"fail-on-case-failure rejects half", 5, true, -1, true},
		{"fail-on-case-failure rejects all but one", 9, true, -1, true},
		{"fail-on-case-failure passes a clean run", 0, true, -1, false},

		{"max-case-failures 0 rejects one", 1, false, 0, true},
		{"max-case-failures 2 tolerates two", 2, false, 2, false},
		{"max-case-failures 2 rejects three", 3, false, 2, true},
		{"max-case-failures 2 rejects all but one", 9, false, 2, true},
		{"max-case-failures passes a clean run", 0, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := caseFailureExitGate(run(tc.failed), tc.failOnAny, tc.maxFail)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "run-result was still written",
				"the gate changes the exit code, not the artifact")
		})
	}
}

// A slot failure IS an infrastructure failure (internal/benchmark/slot_failure.go):
// one reviewer lost one case the rest of the panel scored. --fail-on-case-failure
// and --max-case-failures promise a non-zero exit when a case was lost to an
// infrastructure failure, so caseFailureExitGate must fold slot failures into its
// trigger — otherwise a run that lost reviewer SLOTS exits 0, and the coverage gate
// (checkCoverage) then hard-rejects the very run-result the CI step just accepted.
func TestCaseFailureExitGateCountsSlotFailures(t *testing.T) {
	run := func(caseFailures, slotFailures int) *benchmark.RunResult {
		rr := &benchmark.RunResult{}
		for i := 1; i <= 10; i++ {
			rr.SuiteCaseIDs = append(rr.SuiteCaseIDs, "case-"+strconv.Itoa(i))
		}
		for i := 0; i < caseFailures; i++ {
			rr.CaseFailures = append(rr.CaseFailures,
				benchmark.CaseFailure{CaseID: "case-" + strconv.Itoa(i+1), Reason: benchmark.CaseFailurePrepare})
		}
		for i := 0; i < slotFailures; i++ {
			rr.SlotFailures = append(rr.SlotFailures,
				benchmark.SlotFailure{Model: "m-primary", Persona: "brad", CaseID: "case-01", Reason: benchmark.SlotFailureCall})
		}
		return rr
	}

	for _, tc := range []struct {
		name         string
		caseFailures int
		slotFailures int
		failOnAny    bool
		maxFail      int
		wantErr      bool
	}{
		{"default tolerates a slot failure", 0, 1, false, -1, false},
		{"fail-on-case-failure rejects a slot-only run", 0, 1, true, -1, true},
		{"max-case-failures 0 rejects a slot-only run", 0, 1, false, 0, true},
		{"combined counts drive the threshold over", 1, 2, false, 2, true},
		{"combined counts drive the threshold at", 1, 1, false, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := caseFailureExitGate(run(tc.caseFailures, tc.slotFailures), tc.failOnAny, tc.maxFail)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "run-result was still written",
				"the gate changes the exit code, not the artifact")
		})
	}
}

// Export reads an operator-supplied run-result that every coverage gate then walks
// again, so its size multiplies through the whole path. The read is capped the way
// loadCheckpoint's is, and the rejection is loud rather than an unbounded read.
func TestBenchmarkExport_RejectsAnOversizeRunResult(t *testing.T) {
	orig := maxRunResultBytes
	maxRunResultBytes = 64
	defer func() { maxRunResultBytes = orig }()

	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01"],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":1,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.Greater(t, int64(len(body)), maxRunResultBytes, "the fixture has to actually exceed the ceiling")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)

	require.NotEqual(t, 0, code, "an oversize run-result must not be read unbounded: %s", out)
	require.Contains(t, out, "exceeds size limit", "the rejection names the ceiling it hit")
}

// The two oversize arms are pinned SEPARATELY, because they mask each other: both
// wrap errRunResultTooLarge and the test above asserts only the shared "exceeds size
// limit" text, so disabling either arm alone left ./cli green. The stat arm's
// distinct diagnostic (it names the actual size) and the LimitReader arm's distinct
// purpose (the file grew between stat and read) each get their own assertion here.
// Memory stays bounded by the LimitReader call itself — this is a diagnostic-quality
// gap, not an unbounded read.
func TestBenchmarkExport_StatArmNamesTheActualSize(t *testing.T) {
	orig := maxRunResultBytes
	maxRunResultBytes = 64
	defer func() { maxRunResultBytes = orig }()

	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01"],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":1,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.Greater(t, int64(len(body)), maxRunResultBytes, "the fixture has to actually exceed the ceiling")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)

	require.NotEqual(t, 0, code, "an oversize run-result must be rejected: %s", out)
	require.Contains(t, out, fmt.Sprintf("is %d bytes (limit %d)", int64(len(body)), int64(64)),
		"the stat arm's distinct diagnostic — naming the actual size — must be pinned, not masked by the shared text")
	require.NotContains(t, out, "grew past",
		"the stat arm fired here; the growth arm's message must not appear")
}

// The case_failures gate is pinned at the COMMAND, not only at validateCaseFailures.
// Its unit tests prove the function rejects a bad reason; they say nothing about
// whether runBenchmarkExport still calls it, or still calls it before checkCoverage —
// and both are load-bearing. checkCoverage DROPS an out-of-vocabulary entry rather
// than rejecting it, so with the call deleted (or moved after the gate) this fixture
// exports at exit 0 under --allow-partial-coverage with the case reported as plainly
// missing. Same shape as TestBenchmarkExport_SuitePathScrubErrorPrecedesAnchor: assert
// the earlier gate's text fires AND the later gate's text does not.
func TestBenchmarkExport_CaseFailureGateIsInstalledBeforeTheCoverageGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"case_failures":[{"case_id":"case-02","reason":"vibes"}],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01","case-03"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":2,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path, "--allow-partial-coverage")

	require.NotEqual(t, 0, code,
		"an unvalidated case_failures reason must not reach an operator-facing diagnostic: %s", out)
	require.Contains(t, out, "outside the failure vocabulary",
		"the case_failures gate's own diagnostic must fire")
	require.NotContains(t, out, "not comparable",
		"the coverage gate must not be the last word on a file the failure gate rejects")
}

// The slot_failures gate gets the same COMMAND-level pin, and it needs it for the
// same demonstrated reason: validateSlotFailures had eleven unit tests and nothing
// proved runBenchmarkExport still calls it. Verified by mutation — deleting the call
// left the whole cli suite green, which is exactly how the sibling gate's call site
// went unpinned until a --post round caught it.
//
// --allow-partial-coverage is passed so the coverage gate CANNOT be what fails the
// command: with the slot gate removed this fixture exports at exit 0, so a green
// assertion here would be meaningless without it.
func TestBenchmarkExport_SlotFailureGateIsInstalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"slot_failures":[{"model":"m-primary","persona":"brad","case_id":"case-02","reason":"vibes"}],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01","case-03"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":2,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path, "--allow-partial-coverage")

	require.NotEqual(t, 0, code,
		"an unvalidated slot_failures reason must not reach an operator-facing diagnostic: %s", out)
	require.Contains(t, out, "outside the failure vocabulary",
		"the slot_failures gate's own diagnostic must fire")
	require.Contains(t, out, "slot_failures",
		"and must name the channel, so the operator edits the right array")
}
