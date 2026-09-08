package scorecard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawRecords returns each emitted JSONL line as a generic map, so a test can
// assert a key is ABSENT. Record's typed pointers cannot express that: a nil
// pointer and an omitted key are the same value once unmarshalled back.
func rawRecords(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)

	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		out = append(out, m)
	}
	return out
}

func rawRecordFor(t *testing.T, recs []map[string]any, recordType, reviewer string) map[string]any {
	t.Helper()
	for _, m := range recs {
		if m["record_type"] == recordType && (reviewer == "" || m["reviewer"] == reviewer) {
			return m
		}
	}
	t.Fatalf("no %s record for reviewer %q", recordType, reviewer)
	return nil
}

// TestEmit_AllTruncatedRunOmitsSurvivedSkepticRate closes the degenerate case
// the exclusion created.
//
// A verdict reached from a truncated read leaves the precision ratio entirely.
// When EVERY verdict in a run is truncated, nothing is left: verified and
// refuted are both zero, and ratio(0,0) is 0. Publishing that 0.0 is worse than
// publishing nothing — on the leaderboard and in benchmark.BuildSubmission it is
// indistinguishable from a reviewer whose findings were ALL refuted, which is
// the strongest negative signal the metric can carry. A reviewer whose evidence
// was merely unreadable is reported as one that was proven wrong.
//
// internal/scorecard/export.go says exactly this at its own gate, and cannot
// enforce it: the emitter hands it a stored 0.0 rate, so the "no rate to report"
// branch is never reached. The gate has to be here, at the source.
func TestEmit_AllTruncatedRunOmitsSurvivedSkepticRate(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	recs := rawRecords(t, dir)

	bruce := rawRecordFor(t, recs, RecordTypeReviewer, "bruce")
	assert.NotContains(t, bruce, "survived_skeptic_rate",
		"every verdict was truncated, so there is no rate to report — a 0.0 here reads as all-refuted")
	assert.Equal(t, float64(0), bruce["findings_verified"],
		"the counts stay: zero countable verdicts is a true statement about the run")
	assert.Equal(t, float64(0), bruce["findings_refuted"])

	agg := rawRecordFor(t, recs, RecordTypeAggregate, "")
	assert.NotContains(t, agg, "survived_skeptic_rate",
		"the aggregate carries the same degenerate ratio and must omit it for the same reason")
}

// TestEmit_PartiallyTruncatedRunStillPublishesTheRate is the boundary: the gate
// keys on "no countable verdict survived", not on "some verdict was truncated".
// One surviving verdict is a real, publishable observation.
func TestEmit_PartiallyTruncatedRunStillPublishesTheRate(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	bruce := rawRecordFor(t, rawRecords(t, dir), RecordTypeReviewer, "bruce")
	require.Contains(t, bruce, "survived_skeptic_rate",
		"one clean refuted verdict survived — 0/(0+1) is a genuine measurement, not a degenerate one")
	assert.InDelta(t, 0.0, bruce["survived_skeptic_rate"], 1e-9)
}

// TestEmit_ReviewerWithNoVerdictsOmitsTheRate covers the same degeneracy reached
// without truncation at all: a reviewer whose findings simply drew no verdict in
// this run. greta and kai raise findings that verification.json never mentions.
func TestEmit_ReviewerWithNoVerdictsOmitsTheRate(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"b.go","line":2,"problem":"p2","verdict":"confirmed","trippedBudgets":[]}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	recs := rawRecords(t, dir)

	kai := rawRecordFor(t, recs, RecordTypeReviewer, "kai")
	assert.NotContains(t, kai, "survived_skeptic_rate",
		"kai's finding drew no verdict — reporting 0.0 would charge an unmeasured reviewer as fully refuted")

	bruce := rawRecordFor(t, recs, RecordTypeReviewer, "bruce")
	require.Contains(t, bruce, "survived_skeptic_rate",
		"bruce has a real verdict and keeps a real rate")
	assert.InDelta(t, 1.0, bruce["survived_skeptic_rate"], 1e-9)
}
