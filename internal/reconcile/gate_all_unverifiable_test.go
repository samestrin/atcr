package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeReconciledFile(t *testing.T, reviewDir, name, body string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

// TestValidateRequireVerified_AllUnverifiableIsNotACleanReview closes the silent
// collapse.
//
// --require-verified counts ONLY confirmed findings, so a run in which every
// verdict came back unverifiable gates on nothing and exits 0. That is
// indistinguishable, from the outside, from a review that found nothing wrong.
//
// It is reachable from ordinary configuration: internal/verify/invoke.go floors
// and short-circuits every DECLARED context_window_tokens whose ceiling cannot
// fund one real tool result, so a registry declaring 16384 — legal, admitted by
// internal/registry/config.go — turns real CRITICALs into exit 0. With
// verify.votes >= 2, one small-window agent forcing a plurality tie does the same
// to findings a healthy skeptic confirmed.
//
// This function is the warning path for exactly this class (TD-004: "the gate
// counts only VERIFIED findings, so it will pass"), so the starving case belongs
// here rather than in a separate opt-in command like atcr doctor.
func TestValidateRequireVerified_AllUnverifiableIsNotACleanReview(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[
			{"file":"a.go","line":1,"problem":"p1","verdict":"unverifiable","skeptic":"bruce"},
			{"file":"b.go","line":2,"problem":"p2","verdict":"unverifiable","skeptic":"bruce"}
		],
		"verdictCounts":{"confirmed":0,"refuted":0,"unverifiable":2}
	}`)

	err := ValidateRequireVerified(dir)
	require.Error(t, err,
		"every verdict is unverifiable: the gate has nothing to count and passes green — that must be surfaced, not inferred")
	assert.Contains(t, err.Error(), "unverifiable")
}

// TestValidateRequireVerified_AllUnverifiableNamesTheStarvingWindow checks the
// message points at the cause when findings.json records it. An operator told
// only "all unverifiable" has to go looking; one told the window is below the
// prompt overhead has the fix.
func TestValidateRequireVerified_AllUnverifiableNamesTheStarvingWindow(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[{"file":"a.go","line":1,"problem":"p1","verdict":"unverifiable","skeptic":"bruce"}],
		"verdictCounts":{"confirmed":0,"refuted":0,"unverifiable":1}
	}`)
	writeReconciledFile(t, dir, "findings.json", `[
		{"file":"a.go","line":1,"problem":"p1","verification":{"verdict":"unverifiable","skeptic":"bruce","notes":"window_below_prompt_overhead"}}
	]`)

	err := ValidateRequireVerified(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "window_below_prompt_overhead",
		"the cause is recorded in findings.json — name it rather than leaving the operator to find it")
}

// TestValidateRequireVerified_OneConfirmedIsEnough is the boundary. The check
// fires on "the gate can count nothing", not on "some verdict was unverifiable"
// — a mixed run is an ordinary, healthy result.
func TestValidateRequireVerified_OneConfirmedIsEnough(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[
			{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce"},
			{"file":"b.go","line":2,"problem":"p2","verdict":"unverifiable","skeptic":"bruce"}
		],
		"verdictCounts":{"confirmed":1,"refuted":0,"unverifiable":1}
	}`)
	assert.NoError(t, ValidateRequireVerified(dir))
}

// TestValidateRequireVerified_AllRefutedIsAGenuineResult guards the other
// direction: a run where the skeptic disproved everything counted every finding.
// Nothing collapsed, so nothing is warned about.
func TestValidateRequireVerified_AllRefutedIsAGenuineResult(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[{"file":"a.go","line":1,"problem":"p1","verdict":"refuted","skeptic":"bruce"}],
		"verdictCounts":{"confirmed":0,"refuted":1,"unverifiable":0}
	}`)
	assert.NoError(t, ValidateRequireVerified(dir))
}

// TestValidateRequireVerified_NothingToTallyStaysQuiet keeps the existing
// contract for a file this check can read no verdicts from. A review with
// nothing to verify has not collapsed, and an unparseable file is not evidence
// of one — in both cases the only true statement remains "the verify stage ran".
func TestValidateRequireVerified_NothingToTallyStaysQuiet(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{}`,
		"no findings":  `{"findings":[],"verdictCounts":{"confirmed":0,"refuted":0,"unverifiable":0}}`,
		"unparseable":  `{ not json`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeReconciledFile(t, dir, "verification.json", body)
			assert.NoError(t, ValidateRequireVerified(dir))
		})
	}
}

// TestValidateRequireVerified_TalliesFromTheVerdictsNotTheCounts pins WHERE the
// tally comes from. verdictCounts is a denormalized summary written beside the
// verdicts; the verdicts are the data. Reading the summary would leave a
// pre-verdictCounts file — and any file whose summary drifted from its findings
// — silently exempt from the very check this is.
func TestValidateRequireVerified_TalliesFromTheVerdictsNotTheCounts(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[{"file":"a.go","line":1,"problem":"p1","verdict":"unverifiable","skeptic":"bruce"}]
	}`)
	require.Error(t, ValidateRequireVerified(dir),
		"a file with no verdictCounts block still records an all-unverifiable run")
}
