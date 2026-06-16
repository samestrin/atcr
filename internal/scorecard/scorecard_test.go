package scorecard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRunID = "2026-06-14T10:00:00Z-abc123"

// threeReviewerInput builds an EmitInput with three reviewers and a small finding
// set exercising corroboration (a 2-reviewer finding) and solo findings.
func threeReviewerInput() EmitInput {
	return EmitInput{
		RunID: testRunID,
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce", "greta"}}, // corroborated
			{File: "b.go", Line: 2, Problem: "p2", Reviewers: []string{"bruce"}},          // bruce solo
			{File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"greta"}},          // greta solo
			{File: "d.go", Line: 4, Problem: "p4", Reviewers: []string{"kai"}},            // kai solo
		},
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "claude-sonnet-4-6", TokensIn: 14200, TokensOut: 4000, LatencyMS: 9100},
			"greta": {Model: "claude-haiku-4-5", TokensIn: 8000, TokensOut: 2000, LatencyMS: 5000},
			"kai":   {Model: "gpt-4o", TokensIn: 5000, TokensOut: 1000, LatencyMS: 3000},
		},
	}
}

func readJSONL(t *testing.T, dir string) []Record {
	t.Helper()
	recs, err := ReadRecords(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	return recs
}

func TestEmit_CreatesJSONLFile(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "scorecard")

	require.NoError(t, Emit(threeReviewerInput(), EmitOpts{Dir: store}))

	assert.FileExists(t, filepath.Join(store, "2026-06.jsonl"))
	recs := readJSONL(t, store)
	// 3 reviewer records + 1 aggregate.
	assert.Len(t, recs, 4)
}

func TestEmit_SchemaValidation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Emit(threeReviewerInput(), EmitOpts{Dir: dir}))

	// Inspect raw JSON of the first record to confirm every required key present.
	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	first := firstLine(data)
	var m map[string]any
	require.NoError(t, json.Unmarshal(first, &m))

	for _, k := range []string{
		"schema_version", "record_type", "run_id", "reviewer", "model", "role",
		"findings_raised", "findings_corroborated", "findings_solo",
		"corroboration_rate", "cost_usd", "tokens_in", "tokens_out", "latency_ms",
	} {
		assert.Contains(t, m, k, "required field %q must be present", k)
	}
	// schema_version is the integer 1, not a string.
	assert.EqualValues(t, 1, m["schema_version"])
}

func TestEmit_PerReviewerMetricsAndCost(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Emit(threeReviewerInput(), EmitOpts{Dir: dir}))
	recs := readJSONL(t, dir)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, 2, bruce.FindingsRaised)       // a.go + b.go
	assert.Equal(t, 1, bruce.FindingsCorroborated) // a.go (2 reviewers)
	assert.Equal(t, 1, bruce.FindingsSolo)         // b.go
	assert.InDelta(t, 0.5, bruce.CorroborationRate, 1e-9)
	assert.Equal(t, "reviewer", bruce.Role)
	assert.Equal(t, "claude-sonnet-4-6", bruce.Model)
	assert.Equal(t, 14200, bruce.TokensIn)
	assert.Equal(t, 4000, bruce.TokensOut)
	assert.EqualValues(t, 9100, bruce.LatencyMS)
	// cost = 14200/1e6*3 + 4000/1e6*15 = 0.0426 + 0.06 = 0.1026
	assert.InDelta(t, 0.1026, bruce.CostUSD, 1e-9)
}

func TestEmit_ZeroFindingsCorroborationRate(t *testing.T) {
	dir := t.TempDir()
	in := EmitInput{
		RunID:    testRunID,
		Findings: nil,
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "unknown-model", TokensIn: 0, TokensOut: 0},
		},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))
	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, 0, bruce.FindingsRaised)
	assert.Equal(t, 0.0, bruce.CorroborationRate, "no NaN/Inf on zero denominator")
	assert.Equal(t, 0.0, bruce.CostUSD, "unknown model yields zero cost")
}

func TestEmit_AggregateRecord(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Emit(threeReviewerInput(), EmitOpts{Dir: dir}))
	recs := readJSONL(t, dir)

	// Aggregate is the LAST record.
	agg := recs[len(recs)-1]
	assert.Equal(t, RecordTypeAggregate, agg.RecordType)
	// findings_raised sums across reviewers: bruce(2)+greta(2)+kai(1) = 5
	assert.Equal(t, 5, agg.FindingsRaised)
	// corroborated: bruce(1)+greta(1)+kai(0) = 2
	assert.Equal(t, 2, agg.FindingsCorroborated)
	// rate computed from totals 2/5 = 0.4, not an average of per-reviewer rates.
	assert.InDelta(t, 0.4, agg.CorroborationRate, 1e-9)
	// tokens summed
	assert.Equal(t, 14200+8000+5000, agg.TokensIn)
	assert.Equal(t, 4000+2000+1000, agg.TokensOut)
	// every per-reviewer record carries record_type "reviewer"
	for _, r := range recs[:len(recs)-1] {
		assert.Equal(t, RecordTypeReviewer, r.RecordType)
	}
}

func TestEmit_NoScorecardFlag(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "scorecard")
	require.NoError(t, Emit(threeReviewerInput(), EmitOpts{Dir: store, NoScorecard: true}))

	// No directory and no file created — zero I/O.
	_, err := os.Stat(store)
	assert.True(t, os.IsNotExist(err), "NoScorecard must create nothing")
}

func TestEmit_ConditionalFields_WithVerification(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed"},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted"}
	]}`)

	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(firstLine(data), &m))
	assert.Contains(t, m, "findings_verified")
	assert.Contains(t, m, "findings_refuted")
	assert.Contains(t, m, "survived_skeptic_rate")

	// a.go confirmed → credited to bruce and greta; b.go refuted → bruce.
	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce.FindingsVerified)
	require.NotNil(t, bruce.FindingsRefuted)
	assert.Equal(t, 1, *bruce.FindingsVerified)
	assert.Equal(t, 1, *bruce.FindingsRefuted)
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.5, *bruce.SurvivedSkepticRate, 1e-9) // 1/(1+1)
}

// TestEmit_VerdictCreditsAllReviewersOfDuplicateLocation pins that when two
// findings share the same (file, line, problem) key but carry different
// reviewers, a verdict on that location credits BOTH reviewers — the second
// finding must not overwrite the first's reviewers in the lookup map.
func TestEmit_VerdictCreditsAllReviewersOfDuplicateLocation(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p","verdict":"confirmed"}
	]}`)
	in := EmitInput{
		RunID: testRunID,
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"bruce"}},
			{File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"greta"}},
		},
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "claude-sonnet-4-6"},
			"greta": {Model: "claude-haiku-4-5"},
		},
		VerificationPath: verPath,
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	greta := findReviewer(recs, "greta")
	require.NotNil(t, bruce)
	require.NotNil(t, greta)
	require.NotNil(t, bruce.FindingsVerified)
	require.NotNil(t, greta.FindingsVerified)
	assert.Equal(t, 1, *bruce.FindingsVerified, "first finding's reviewer must still be credited")
	assert.Equal(t, 1, *greta.FindingsVerified, "second finding at same key must not overwrite the first")
}

func TestEmit_ConditionalFields_NoVerification(t *testing.T) {
	dir := t.TempDir()
	in := threeReviewerInput()
	in.VerificationPath = "" // no verification
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(firstLine(data), &m))
	assert.NotContains(t, m, "findings_verified")
	assert.NotContains(t, m, "findings_refuted")
	assert.NotContains(t, m, "survived_skeptic_rate")
}

func TestEmit_ConditionalFields_MalformedVerificationOmitted(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{not valid json`)
	in := threeReviewerInput()
	in.VerificationPath = verPath
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(firstLine(data), &m))
	assert.NotContains(t, m, "findings_verified", "malformed verification.json → fields omitted")
}

// --- helpers ---

func writeVerification(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "verification.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func firstLine(data []byte) []byte {
	for i, b := range data {
		if b == '\n' {
			return data[:i]
		}
	}
	return data
}

func findReviewer(recs []Record, name string) *Record {
	for i := range recs {
		if recs[i].RecordType == RecordTypeReviewer && recs[i].Reviewer == name {
			return &recs[i]
		}
	}
	return nil
}
