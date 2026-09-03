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
	recs, err := ReadRecords(filepath.Join(dir, "2026-06.jsonl"), ReadOpts{})
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

	// The aggregate's denominator is the SUM of per-reviewer denominators, every
	// one of which includes the Tier-4-routed findings. Leaving the era flag off
	// the aggregate labelled it pre-epic while it carried post-epic numbers —
	// permanently misclassifying every aggregate line for any external analysis,
	// or for any future widening of the era rule. The flag must describe the
	// number on the same record.
	assert.True(t, agg.RaisedIncludesUnresolved,
		"the aggregate sums denominators that include routed findings, so it belongs to the same era they do")
	assert.Equal(t, RaisedDenominatorCurrent, agg.RaisedDenominator,
		"and it must say WHICH of those definitions, not just that routed findings are in there somewhere")
	assert.Equal(t, SchemaVersion, agg.SchemaVersion)
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

// TestEmit_OrphanVerdictIgnored verifies a verification finding with no matching
// raised finding is skipped (credits no reviewer) while a matching verdict in the
// same file is still attributed — the no-match path must not panic or miscount.
func TestEmit_OrphanVerdictIgnored(t *testing.T) {
	dir := t.TempDir()
	verPath := writeVerification(t, dir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed"},
		{"file":"ghost.go","line":9,"problem":"unmatched","verdict":"confirmed"}
	]}`)
	in := EmitInput{
		RunID:            testRunID,
		Findings:         []Finding{{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce"}}},
		Reviewers:        map[string]ReviewerMeta{"bruce": {Model: "claude-sonnet-4-6"}},
		VerificationPath: verPath,
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	recs := readJSONL(t, dir)
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	require.NotNil(t, bruce.FindingsVerified)
	assert.Equal(t, 1, *bruce.FindingsVerified, "matched verdict credited; orphan ghost.go verdict credited nobody")
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

// TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter locks Epic 3.4 AC1: the
// orphan-verdict warning (a verification finding with no matching raised finding)
// must be written to the injected EmitOpts.Diag, not the process-global os.Stderr,
// so it can be captured and asserted by text.
func TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter(t *testing.T) {
	dir := t.TempDir()
	verPath := filepath.Join(t.TempDir(), "verification.json")
	// A verdict whose (file,line,problem) matches no raised finding is an orphan.
	verJSON := `{"findings":[{"file":"ghost.go","line":99,"problem":"none","verdict":"confirmed"}]}`
	require.NoError(t, os.WriteFile(verPath, []byte(verJSON), 0o600))

	var buf bytes.Buffer
	in := EmitInput{
		RunID:            testRunID,
		Findings:         []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers:        map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
		VerificationPath: verPath,
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir, Diag: &buf}))
	assert.Contains(t, buf.String(), "has no matching raised finding",
		"orphan-verdict diagnostic must route to the injected EmitOpts.Diag")
}

// TestEmit_WriteFailureDiagnosticRoutesToDiagWriter locks Epic 3.4 AC1/AC2 for the
// "write failed" diagnostic: pointing the store under a regular file makes Append's
// MkdirAll fail, and the resulting warning must reach the injected EmitOpts.Diag.
func TestEmit_WriteFailureDiagnosticRoutesToDiagWriter(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	storeDir := filepath.Join(blocker, "scorecard") // under a regular file → MkdirAll fails

	var buf bytes.Buffer
	in := EmitInput{
		RunID:     testRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	err := Emit(in, EmitOpts{Dir: storeDir, Diag: &buf})
	require.Error(t, err, "a store path under a regular file must fail to write")
	assert.Contains(t, buf.String(), "write failed",
		"write-failure diagnostic must route to the injected EmitOpts.Diag")
}

// TestEmit_NilDiagDefaultsToStderr locks Epic 3.4 AC5: a zero EmitOpts (nil Diag)
// preserves prior behavior — emission succeeds and diagnostics fall back to
// os.Stderr without panicking.
func TestEmit_NilDiagDefaultsToStderr(t *testing.T) {
	dir := t.TempDir()
	in := EmitInput{
		RunID:     testRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir})) // nil Diag → os.Stderr, must not panic
}

// TestEmit_StampsRaisedIncludesUnresolvedOnACleanRun pins the era stamp at the
// EMITTER, which is the only place it can be pinned.
//
// Every other era test builds Record literals by hand and sets the flag itself,
// so the whole suite stayed green with `RaisedIncludesUnresolved: true` flipped to
// false in Emit — verified by mutation. That matters more than an ordinary
// coverage gap: the flag is the only discriminator between the two
// FindingsRaised definitions, and it is `omitempty`, so dropping it does not
// produce a wrong value, it produces an ABSENT key. unresolvedEraRuns reads absent
// as "pre-epic", so a regressed stamp makes every new record classify as old and
// the filter silently falls back to blending both definitions forever — the exact
// defect the flag was added to prevent, with nothing failing.
//
// The run deliberately has NO UnresolvedFindings. That is the case the
// unconditional stamp exists for: a clean run must still be distinguishable from a
// pre-epic record, and a `len(in.UnresolvedFindings) > 0` guard would be the
// natural wrong way to write this.
//
// The assertion is on the RAW JSON key rather than the decoded struct field,
// because absence is the failure mode and a decoded bool cannot tell an absent key
// from a present false one.
func TestEmit_StampsRaisedIncludesUnresolvedOnACleanRun(t *testing.T) {
	dir := t.TempDir()

	in := threeReviewerInput()
	require.Empty(t, in.UnresolvedFindings, "this fixture must be a clean run for the test to mean what it says")
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	data, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)

	var reviewerLines int
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		if m["record_type"] != string(RecordTypeReviewer) {
			continue
		}
		reviewerLines++
		require.Contains(t, m, "raised_includes_unresolved",
			"reviewer record %q omits the era key entirely; omitempty means a dropped stamp reads as a pre-epic record", m["reviewer"])
		assert.Equal(t, true, m["raised_includes_unresolved"],
			"every record this emitter writes uses the current denominator definition")
		// The version, at the JSON level, for the same reason: RaisedDenominator is
		// omitempty, so a dropped stamp does not fail to compile or fail a struct
		// assertion — it silently omits the key, and an absent version reads as the
		// PREVIOUS definition. Only a check on the emitted bytes catches that.
		require.Contains(t, m, "raised_denominator",
			"reviewer record %q omits the denominator version; omitempty makes a dropped stamp read as the older era", m["reviewer"])
		assert.EqualValues(t, RaisedDenominatorCurrent, m["raised_denominator"],
			"and it must be the current definition, not merely present")
	}
	require.Equal(t, 3, reviewerLines, "all three reviewer records must be checked, not just the first")
}

// TestRaisedDenominatorOf_ClampsAboveCurrent pins the one branch of
// raisedDenominatorOf that no in-tree caller can reach.
//
// Both callers — unresolvedEraRuns' two loops and reviewerAcc.add via
// ExportSelected — exclude above-current records BEFORE asking, so the clamp is
// a backstop for a future or embedding caller that asks directly. That makes it
// unreachable through the package's own paths and therefore untestable through
// them: deleting the branch leaves every other test green, which is exactly why
// its contract has to be asserted here rather than inferred from a caller.
//
// The contract is "reads as the CURRENT definition", not "defines a cohort of
// its own": an unrecognised denominator that returned itself would form a
// singleton era and silently split a reviewer's window.
func TestRaisedDenominatorOf_ClampsAboveCurrent(t *testing.T) {
	for name, denom := range map[string]int{
		"newer binary (current+1)":    RaisedDenominatorCurrent + 1,
		"benchmark-suite value (100)": RaisedDenominatorBenchmarkSuite,
		"corrupt hand-edit (999)":     999,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, RaisedDenominatorCurrent,
				raisedDenominatorOf(Record{RaisedDenominator: denom}),
				"an above-current denominator must read as the current definition, not as itself")
		})
	}

	// The clamp must not swallow the recognised values beneath it.
	assert.Equal(t, RaisedDenominatorCurrent, raisedDenominatorOf(Record{RaisedDenominator: RaisedDenominatorCurrent}))
	assert.Equal(t, raisedDenominatorAllRouted, raisedDenominatorOf(Record{RaisedDenominator: raisedDenominatorAllRouted}))
	assert.Equal(t, raisedDenominatorAllRouted, raisedDenominatorOf(Record{RaisedIncludesUnresolved: true}))
	assert.Equal(t, raisedDenominatorPreEpic, raisedDenominatorOf(Record{}))
}
