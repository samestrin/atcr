package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			path := writeRunResultWithVocabulary(t, tc.rows)
			code, _, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
			assert.NotEqual(t, 0, code, "a corrupt per-row measurement must not reach a public submission")
			assert.Contains(t, stderr, tc.want)
			assert.Contains(t, stderr, "reviewer_vocabulary", "the error must name the field so the operator can find it")
		})
	}
}

// A NaN rate is corrupt for the same reason the scalar's NaN check exists: it
// survives JSON round-trips through some encoders and compares false against every
// bound, so a range check alone would pass it through.
func TestBenchmarkExport_RejectsNaNReviewerVocabularyRate(t *testing.T) {
	isolate(t)
	// NaN cannot be marshalled by encoding/json, so the file is hand-written.
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
	  "suite": "suite-valid",
	  "suite_version": "1",
	  "generated_at": "2026-08-15T00:00:00Z",
	  "reviewers": [{"model":"m-a","persona":"p-a"}],
	  "reviewer_vocabulary": [{"model":"m-a","persona":"p-a","findings":4,"drifted":1,"rate":1e999}]
	}`), 0o644))

	code, _, stderr := execCmdSplit(t, "benchmark", "export", "--in", path)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "reviewer_vocabulary")
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
