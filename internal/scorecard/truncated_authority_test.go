package scorecard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFindings drops a findings.json beside the verification.json the tests in
// this file write, mirroring the reconciled/ directory both artifacts share on
// disk.
func writeFindings(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "findings.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// TestEmit_TruncationAuthorityIsFindingsJSON closes the desync between the two
// artifacts that encode the same fact.
//
// report.md renders its truncated caveat from findings.json's
// `verification.truncated`. The precision exclusion used to read a DIFFERENT
// file — reconciled/verification.json's `trippedBudgets` — and the debate stage
// updates only the first: internal/debate/emit.go clears Truncated on a judge
// ruling because the recorded verdict is now the judge's, produced from the
// judge's own read, while internal/debate/debate.go deliberately does NOT
// recompute verification.json (it is a point-in-time verify audit artifact).
//
// After verify → debate → reconcile the two therefore disagreed: report.md
// showed a ruling with no caveat while the scorecard still dropped that finding
// from survived_skeptic_rate. findings.json is the authoritative post-debate
// record, so it is what the score must follow.
func TestEmit_TruncationAuthorityIsFindingsJSON(t *testing.T) {
	dir := t.TempDir()
	// verification.json is the STALE verify-stage snapshot: it still records the
	// tool-budget trip that produced the original skeptic verdict.
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)
	// findings.json is the post-debate record: the judge ruled on a.go from its
	// own read, so the caveat is gone — which is exactly what report.md shows.
	writeFindings(t, dir, `[
		{"file":"a.go","line":1,"problem":"p1","verification":{"verdict":"confirmed","skeptic":"bruce"}},
		{"file":"b.go","line":2,"problem":"p2","verification":{"verdict":"refuted","skeptic":"bruce"}}
	]`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := findReviewer(readJSONL(t, dir), "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	require.NotNil(t, bruce.FindingsRefuted)
	assert.Equal(t, 1, *bruce.FindingsVerified,
		"findings.json carries no truncation caveat for a.go, so the score must count the verdict report.md presents at full confidence")
	assert.Equal(t, 1, *bruce.FindingsRefuted)
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.5, *bruce.SurvivedSkepticRate, 1e-9,
		"1/(1+1): both verdicts count once the stale trippedBudgets snapshot stops overriding findings.json")
}

// TestEmit_TruncationAuthorityExcludesWhenFindingsJSONSaysTruncated is the other
// direction of the same rule: findings.json is authoritative when it marks a
// verdict truncated even though the verification.json snapshot does not.
//
// internal/verify/pipeline.go carries trippedBudgets forward across a re-verify
// only when the prior verdict matches, while the truncated flag on the
// findings.json block always survives — so this shape is reachable in practice.
func TestEmit_TruncationAuthorityExcludesWhenFindingsJSONSaysTruncated(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":[]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)
	writeFindings(t, dir, `[
		{"file":"a.go","line":1,"problem":"p1","verification":{"verdict":"confirmed","skeptic":"bruce","truncated":true}},
		{"file":"b.go","line":2,"problem":"p2","verification":{"verdict":"refuted","skeptic":"bruce"}}
	]`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := findReviewer(readJSONL(t, dir), "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 0, *bruce.FindingsVerified,
		"findings.json marks a.go truncated, so it leaves the ratio even though the verification.json snapshot lost the trip")
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.0, *bruce.SurvivedSkepticRate, 1e-9)
}

// TestEmit_TruncationFallsBackToTrippedBudgetsWithoutFindingsJSON pins the
// degraded path. A caller that hands Emit a verification.json with no
// findings.json beside it (a direct Emit, a pre-35.16.6.8 review directory) keeps
// the previous behaviour rather than silently scoring every truncated verdict at
// full weight.
func TestEmit_TruncationFallsBackToTrippedBudgetsWithoutFindingsJSON(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := findReviewer(readJSONL(t, dir), "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 0, *bruce.FindingsVerified,
		"with no findings.json to consult, trippedBudgets remains the signal")
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.0, *bruce.SurvivedSkepticRate, 1e-9)
}

// TestEmit_TruncationFallsBackPerFindingWhenFindingsJSONOmitsTheRecord keeps the
// fallback at record granularity rather than file granularity: a findings.json
// that exists but does not mention this location says nothing about it, so the
// snapshot still decides.
func TestEmit_TruncationFallsBackPerFindingWhenFindingsJSONOmitsTheRecord(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)
	// Only b.go is present; a.go is absent from the post-debate record.
	writeFindings(t, dir, `[
		{"file":"b.go","line":2,"problem":"p2","verification":{"verdict":"refuted","skeptic":"bruce"}}
	]`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := findReviewer(readJSONL(t, dir), "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 0, *bruce.FindingsVerified,
		"a.go is unmentioned by findings.json, so its trippedBudgets trip still excludes it")
	require.NotNil(t, bruce.FindingsRefuted)
	assert.Equal(t, 1, *bruce.FindingsRefuted)
}

// TestEmit_TruncationFallsBackWhenFindingsJSONIsMalformed keeps a corrupt
// post-debate record from silently promoting every truncated verdict to full
// weight — the fail-safe direction is the one that does not move a durable trust
// score on evidence the pipeline marked partial.
func TestEmit_TruncationFallsBackWhenFindingsJSONIsMalformed(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)
	writeFindings(t, dir, `{ not json at all`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := findReviewer(readJSONL(t, dir), "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 0, *bruce.FindingsVerified,
		"an unparseable findings.json is not authority to count a truncated verdict")
}
