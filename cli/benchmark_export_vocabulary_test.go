package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRunResultWithVocabulary writes a minimal export-valid run-result carrying the
// given reviewer_vocabulary rows, and returns its path. Reviewers is fixed at two
// rows so a length/order mismatch is expressible.
func writeRunResultWithVocabulary(t *testing.T, rows []benchmark.ReviewerVocabulary) string {
	t.Helper()
	rr := benchmark.RunResult{
		Suite:        "suite-valid",
		SuiteVersion: "1",
		GeneratedAt:  "2026-08-15T00:00:00Z",
		Reviewers: []scorecard.PublicRecord{
			{Model: "m-a", Persona: "p-a"},
			{Model: "m-b", Persona: "p-b"},
		},
		Vocabulary: rows,
	}
	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func ptrFloat(v float64) *float64 { return &v }

// execExportErr drives the export command and returns the ERROR it produced along
// with the captured streams. execCmdSplit reports only an exit code: the root
// command silences error printing (cmd/atcr's main renders it), so a hard-rejection
// message never reaches errBuf and asserting on its text needs the error itself.
func execExportErr(t *testing.T, path string) (err error, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"benchmark", "export", "--in", path})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	return root.ExecuteContext(context.Background()), outBuf.String(), errBuf.String()
}

// A run-result may be hand-supplied, so every diagnostic it carries is untrusted at
// this boundary. out_of_vocabulary_rate has been range-checked since it existed; its
// sibling per-row rates were not, so a hand-written file could publish rates outside
// [0,1] or a drifted count exceeding its own denominator. Same rule, same severity:
// a corrupt measurement is a rejected file, not a pessimistic reading.
func TestBenchmarkExport_RejectsMalformedReviewerVocabularyRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []benchmark.ReviewerVocabulary
		want string
	}{
		{
			name: "rate above 1",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 10, Drifted: 2, Rate: ptrFloat(1.5)}},
			want: "outside [0,1]",
		},
		{
			name: "negative rate",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 10, Drifted: 2, Rate: ptrFloat(-0.1)}},
			want: "outside [0,1]",
		},
		{
			name: "drifted exceeds findings",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 3, Drifted: 4, Rate: ptrFloat(1.0)}},
			want: "drifted",
		},
		// Both negative cases keep drifted <= findings on purpose. The sibling
		// drifted-exceeds-findings guard catches any negative pair that does NOT —
		// {-10, 0} trips it — so a case like that would pass with the negative guard
		// deleted and prove nothing about it. These two fail only the negative check.
		{
			name: "negative findings with a more-negative drifted",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: -10, Drifted: -20}},
			want: "negative findings/drifted",
		},
		{
			name: "equal negative findings and drifted",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: -1, Drifted: -1}},
			want: "negative findings/drifted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			path := writeRunResultWithVocabulary(t, tc.rows)
			err, stdout, _ := execExportErr(t, path)
			require.Error(t, err, "a corrupt per-row measurement must not reach a public submission")
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "reviewer_vocabulary", "the error must name the field so the operator can find it")
			assert.Empty(t, stdout, "a rejected file must emit no submission at all")
		})
	}
}

// A NaN rate cannot arrive through the file path — encoding/json rejects the JSON
// spellings that would produce one — so the guard is exercised directly. It is kept
// because NaN compares false against every bound: a range check alone lets it
// through, and the sibling out_of_vocabulary_rate check carries the same guard for
// the same reason. (An earlier version of this test fed `1e999` through the command
// and passed with the guard removed, proving only that the JSON decoder works.)
func TestValidateReviewerVocabulary_RejectsNaNRate(t *testing.T) {
	nan := math.NaN()
	err := validateReviewerVocabulary(io.Discard, benchmark.RunResult{
		Reviewers:  []scorecard.PublicRecord{{Model: "m-a", Persona: "p-a"}},
		Vocabulary: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 4, Drifted: 1, Rate: &nan}},
	}, "rr.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer_vocabulary")
}

// The positional join (entry i describes reviewers[i]) is what the field doc
// instructs consumers to rely on, but nothing on the export path reads it today and
// the array may legitimately be absent or shorter. So a mismatch is a WARNING that
// still publishes — a new hard rejection at a publication gate needs a consumer that
// would actually be misled, and there is none yet.
func TestBenchmarkExport_WarnsButPublishesOnVocabularyLengthOrOrderMismatch(t *testing.T) {
	t.Run("length mismatch", func(t *testing.T) {
		isolate(t)
		path := writeRunResultWithVocabulary(t, []benchmark.ReviewerVocabulary{
			{Model: "m-a", Persona: "p-a", Findings: 4, Drifted: 1, Rate: ptrFloat(0.25)},
		}) // 1 row against 2 reviewers
		code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
		assert.Equal(t, 0, code, "a positional mismatch must not block publication")
		assert.Contains(t, stderr, "reviewer_vocabulary")
		assert.Contains(t, stderr, "warning")
		assert.NotEmpty(t, stdout, "the submission must still be emitted")
	})

	t.Run("order mismatch", func(t *testing.T) {
		isolate(t)
		path := writeRunResultWithVocabulary(t, []benchmark.ReviewerVocabulary{
			{Model: "m-b", Persona: "p-b", Findings: 4, Drifted: 1, Rate: ptrFloat(0.25)},
			{Model: "m-a", Persona: "p-a", Findings: 4, Drifted: 1, Rate: ptrFloat(0.25)},
		}) // right length, swapped against reviewers
		code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
		assert.Equal(t, 0, code)
		assert.Contains(t, stderr, "reviewer_vocabulary")
		assert.NotEmpty(t, stdout)
	})
}

// An absent array is the normal case for any run-result written before the field
// existed, and for a run with no reviewers. It must stay silent — a warning on every
// legacy file is the "warning nobody reads" failure this codebase argues against.
func TestBenchmarkExport_SilentOnAbsentReviewerVocabulary(t *testing.T) {
	isolate(t)
	path := writeRunResultWithVocabulary(t, nil)
	code, _, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
	assert.Equal(t, 0, code)
	assert.NotContains(t, stderr, "reviewer_vocabulary",
		"an omitted diagnostic array is not a defect")
}

// A well-formed, correctly-aligned array is silent too — the guard must not fire on
// the shape production actually writes.
func TestBenchmarkExport_SilentOnWellFormedAlignedVocabulary(t *testing.T) {
	isolate(t)
	path := writeRunResultWithVocabulary(t, []benchmark.ReviewerVocabulary{
		{Model: "m-a", Persona: "p-a", Findings: 4, Drifted: 1, Rate: ptrFloat(0.25)},
		{Model: "m-b", Persona: "p-b", Findings: 8, Drifted: 0, Rate: ptrFloat(0)},
	})
	code, _, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
	assert.Equal(t, 0, code)
	assert.NotContains(t, stderr, "reviewer_vocabulary")
}

// The misalignment warning prints FOUR identity strings straight to the operator's
// terminal, and this boundary's own doc calls its input "an untrusted (possibly
// hand-supplied) run-result" — strictly more attacker-controlled than the
// provider-reported strings the sibling warnings in this file already sanitize with
// stripTerminalControlRunes. An unsanitized ESC here can erase the line and forge a
// reassuring one over the very warning that says the positional join is broken.
func TestValidateReviewerVocabulary_StripsTerminalControlRunesFromMisalignmentWarning(t *testing.T) {
	var buf bytes.Buffer
	err := validateReviewerVocabulary(&buf, benchmark.RunResult{
		Reviewers: []scorecard.PublicRecord{
			{Model: "m-reviewer\x1b[2K", Persona: "p-reviewer\x07"},
		},
		Vocabulary: []benchmark.ReviewerVocabulary{
			{Model: "m-vocab\x1b[1G", Persona: "p-vocab\x08", Findings: 4, Drifted: 1, Rate: ptrFloat(0.25)},
		},
	}, "rr.json")
	require.NoError(t, err, "a positional mismatch warns, it does not reject")
	got := buf.String()

	require.Contains(t, got, "positional join", "precondition: the misalignment warning fired")
	assert.NotContains(t, got, "\x1b", "an ESC from an untrusted run-result must never reach the terminal")
	assert.NotContains(t, got, "\x07", "nor a BEL")
	assert.NotContains(t, got, "\x08", "nor a BACKSPACE, which can rewrite the line in place")
	// The identities must still be readable — sanitizing must not blank the row.
	assert.Contains(t, got, "m-vocab")
	assert.Contains(t, got, "p-vocab")
	assert.Contains(t, got, "m-reviewer")
	assert.Contains(t, got, "p-reviewer")
}

// checkCoverage's shortfall rows land in a stderr warning under
// --allow-partial-coverage, built from the same untrusted run-result identities the
// vocabulary warnings sanitize. Left raw they are the same terminal-injection hazard
// one file over.
func TestCheckCoverage_StripsTerminalControlRunesFromShortfallWarning(t *testing.T) {
	var buf bytes.Buffer
	err := checkCoverage(&buf, benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02"},
		Reviewers:    []scorecard.PublicRecord{{Model: "m-a\x1b[2K", Persona: "p-a\x07", Runs: 1}},
		Coverage: []benchmark.ReviewerCoverage{
			{Model: "m-a\x1b[2K", Persona: "p-a\x07", CaseIDs: []string{"case-01"}}, // 1 of 2: short
		},
	}, "rr.json", true)
	require.NoError(t, err, "--allow-partial-coverage warns rather than rejecting")
	got := buf.String()

	require.Contains(t, got, "partial coverage", "precondition: the shortfall warning fired")
	assert.NotContains(t, got, "\x1b", "an ESC from an untrusted run-result must never reach the terminal")
	assert.NotContains(t, got, "\x07", "nor a BEL")
	assert.Contains(t, got, "m-a", "the identity must still be readable")
	assert.Contains(t, got, "p-a")
}

// The `v.Rate == nil` continue branch carries the nil-vs-zero distinction at the
// export boundary: a reviewer that raised nothing is UNMEASURED, and an unmeasured row
// is not a defect. It was uncovered and survived deletion — with the branch gone the
// dereference below reads a nil pointer, so a legitimate run-result with an unmeasured
// reviewer panics the export instead of publishing.
func TestBenchmarkExport_PublishesUnmeasuredVocabularyRow(t *testing.T) {
	isolate(t)
	path := writeRunResultWithVocabulary(t, []benchmark.ReviewerVocabulary{
		{Model: "m-a", Persona: "p-a", Findings: 0, Drifted: 0, Rate: nil}, // raised nothing
		{Model: "m-b", Persona: "p-b", Findings: 8, Drifted: 0, Rate: ptrFloat(0)},
	})
	code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)

	assert.Equal(t, 0, code, "an unmeasured reviewer is the normal shape of a run that raised nothing")
	assert.NotEmpty(t, stdout, "the submission must still be emitted")
	assert.NotContains(t, stderr, "reviewer_vocabulary",
		"a nil rate is absence of a measurement, never a malformed one")
}

// Two shapes describe no run at all and were accepted at the publication gate.
//
//   - A rate that contradicts its own counts. PerReviewerVocabulary writes Rate as
//     exactly Drifted/Findings (vocabulary.go), so 1/10 published as 0.99 cannot have
//     come from any run — and the rate is the number a leaderboard reads.
//   - A non-nil rate on a zero denominator. The producer leaves Rate nil when a
//     reviewer raised nothing; a 0.0 there is the nil-vs-zero collapse the pointer
//     exists to prevent, publishing a reviewer that found nothing as measured-and-clean.
func TestBenchmarkExport_RejectsVocabularyRowsThatDescribeNoRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []benchmark.ReviewerVocabulary
		want string
	}{
		{
			name: "rate contradicts its own counts",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 10, Drifted: 1, Rate: ptrFloat(0.99)}},
			want: "does not match",
		},
		{
			name: "measured rate on a reviewer that raised nothing",
			rows: []benchmark.ReviewerVocabulary{{Model: "m-a", Persona: "p-a", Findings: 0, Drifted: 0, Rate: ptrFloat(0)}},
			want: "raised no findings",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			path := writeRunResultWithVocabulary(t, tc.rows)
			err, stdout, _ := execExportErr(t, path)
			require.Error(t, err, "a row that describes no run must not reach a public submission")
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "reviewer_vocabulary")
			assert.Empty(t, stdout)
		})
	}
}

// The consistency check must tolerate a rate written to fewer decimal places than the
// producer emits — a hand-assembled but HONEST file is not the target. Only a
// contradiction is.
func TestValidateReviewerVocabulary_AcceptsARoundedButHonestRate(t *testing.T) {
	err := validateReviewerVocabulary(io.Discard, benchmark.RunResult{
		Reviewers: []scorecard.PublicRecord{{Model: "m-a", Persona: "p-a"}},
		Vocabulary: []benchmark.ReviewerVocabulary{
			{Model: "m-a", Persona: "p-a", Findings: 3, Drifted: 1, Rate: ptrFloat(0.333333)},
		},
	}, "rr.json")
	assert.NoError(t, err, "1/3 written to six places is rounding, not a contradiction")
}

// The stricter row validation above rejects shapes no run can produce. That claim is
// only safe if the PRODUCER is actually held to it — and every test in this file feeds
// the gate hand-built rows, so a change to PerReviewerVocabulary's rate expression
// (macro-averaging per case, excluding routing values from the numerator, emitting 0.0
// for an unmeasured reviewer) would start rejecting real run-results with nothing
// failing here. Drive a real run's own breakdown through the gate.
//
// Both rosters matter: one that raises findings (exercises the rate/counts
// consistency check) and one that raises none (exercises the zero-denominator check
// from the producer side, where Rate must be nil rather than 0.0).
func TestValidateReviewerVocabulary_AcceptsARealRunsOwnBreakdown(t *testing.T) {
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		completer fanout.Completer
		measured  bool
	}{
		{"a roster that raised findings", stubCategoryCompleter{category: "bug"}, true},
		{"a roster that raised nothing", stubNoFindingsCompleter{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
			rr, err := executeBenchmarkRun(context.Background(), cfg, tc.completer, suiteValidPath, gen, "")
			require.NoError(t, err)
			require.NotEmpty(t, rr.Vocabulary, "precondition: the run produced the breakdown the gate reads")

			measured := false
			for _, v := range rr.Vocabulary {
				if v.Rate != nil {
					measured = true
				}
			}
			require.Equal(t, tc.measured, measured,
				"precondition: this roster must produce %v rows", map[bool]string{true: "measured", false: "unmeasured"}[tc.measured])

			assert.NoError(t, validateReviewerVocabulary(io.Discard, *rr, "rr.json"),
				"the export gate must never reject the shape `atcr benchmark run` actually writes")
		})
	}
}
