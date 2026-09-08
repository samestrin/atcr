package scorecard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmit_TruncatedVerdictIsExcludedFromThePrecisionRatio closes the third and
// most durable leg of the truncation signal.
//
// A verdict whose skeptic tripped a window-DERIVED tool ceiling still STANDS —
// internal/verify's tripsVoidTheVerdict exempts it — but it was reached from a
// shortened read. Charging it to a reviewer's survived_skeptic_rate the same as
// a full-confidence read moves a durable trust score on evidence the pipeline
// itself marks as partial, in whichever direction the truncated read happened
// to land.
//
// So it is excluded from the ratio entirely: neither numerator nor denominator.
// It is not counted AGAINST the reviewer either — the point is that this verdict
// is not evidence about the reviewer at all.
//
// Only a confirmed/refuted record can carry the marker: a DECLARED ceiling's
// trip voids the verdict to unverifiable, which the tally already ignores.
func TestEmit_TruncatedVerdictIsExcludedFromThePrecisionRatio(t *testing.T) {
	dir := t.TempDir()
	// bruce raises a.go (confirmed, truncated) and b.go (refuted, clean).
	// Without the exclusion bruce scores 1/(1+1) = 0.5; with it, only the clean
	// refuted counts, so bruce scores 0/(0+1) = 0 over ONE observation.
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	require.NotNil(t, bruce.FindingsRefuted)
	assert.Equal(t, 0, *bruce.FindingsVerified,
		"the confirmed verdict came from a truncated read — it is not evidence about the reviewer")
	assert.Equal(t, 1, *bruce.FindingsRefuted,
		"the clean refuted verdict is untouched")
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.0, *bruce.SurvivedSkepticRate, 1e-9,
		"0/(0+1): the truncated verdict left the ratio entirely rather than inflating it to 0.5")
}

// TestEmit_TruncatedUnverifiableChangesNothing guards the boundary: the
// exclusion must key on the truncation marker riding a verdict the tally
// actually counts, not on the presence of a tripped budget anywhere.
func TestEmit_TruncatedUnverifiableChangesNothing(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":[]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"unverifiable","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(firstLine(data), &m))

	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 1, *bruce.FindingsVerified,
		"an unverifiable record was never counted, truncated or not — nothing about this case changes")
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 1.0, *bruce.SurvivedSkepticRate, 1e-9)
}
