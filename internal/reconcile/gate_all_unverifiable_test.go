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

// TestValidateRequireVerified_AllUnverifiablePointsAtTheDiagnostic checks the
// message gives the operator somewhere to go.
//
// It deliberately does NOT splice a cause out of findings.json: this runs after
// RunReconcile, which rebuilds that file from sources/ and strips every
// verification block, so the skeptic lane's own notes are gone by then. Naming
// the command that can still diagnose it is the honest substitute — an
// enrichment that never fires would read, in review, as one that works.
func TestValidateRequireVerified_AllUnverifiablePointsAtTheDiagnostic(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFile(t, dir, "verification.json", `{
		"findings":[{"file":"a.go","line":1,"problem":"p1","verdict":"unverifiable","skeptic":"bruce"}],
		"verdictCounts":{"confirmed":0,"refuted":0,"unverifiable":1}
	}`)

	err := ValidateRequireVerified(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "atcr doctor",
		"the operator needs the command that can identify the starving agent")
	assert.Contains(t, err.Error(), "context_window_tokens",
		"and the field to look at when they run it")
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

// TestAllUnverifiableCollapse_TakesOnlyThePathItReads pins the signature.
//
// The function took a reviewDir it never used. verPath is already absolute and
// every read goes through it; the doc comment above it discusses findings.json,
// which the function deliberately does NOT read. An unused parameter on a new
// unexported function invites the next caller to assume it means something.
//
// A signature is pinned by calling it: this file fails to COMPILE if reviewDir
// comes back, which is the only assertion available for "this parameter does not
// exist" and a stronger one than any runtime check.
func TestAllUnverifiableCollapse_TakesOnlyThePathItReads(t *testing.T) {
	dir := t.TempDir()
	verPath := filepath.Join(dir, "verification.json")
	require.NoError(t, os.WriteFile(verPath, []byte(`{"findings":[{"verdict":"unverifiable"}]}`), 0o600))

	err := allUnverifiableCollapse(verPath)
	require.Error(t, err, "one finding, unverifiable — the collapse the gate must not pass over")
	assert.Contains(t, err.Error(), "atcr doctor")
}

// TestAllUnverifiableCollapse_UnreadableFileIsNotAnError covers the ReadFile
// fallback the caller's os.Stat almost always makes unreachable.
//
// It can otherwise only fire if the file disappears between that successful Stat
// and this read. A directory at the path reaches the same branch deterministically
// and without fault injection: Stat succeeds, IsDir() is false only for files, so
// the caller skips it — but called directly, ReadFile fails and the function must
// return nil, exactly the pre-change behaviour.
func TestAllUnverifiableCollapse_UnreadableFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	notAFile := filepath.Join(dir, "verification.json")
	require.NoError(t, os.MkdirAll(notAFile, 0o755))

	assert.NoError(t, allUnverifiableCollapse(notAFile),
		"the stage still ran, which is all this function ever claimed to check — an unreadable snapshot is not a gate failure")
}
