//go:build integration

// Integration tests for the full scorecard pipeline (emit -> store -> read ->
// aggregate). Gated behind the `integration` build tag so the default unit-test
// run stays fast and hermetic; run with `go test -tags=integration ./...`.
// Every test pins the store to t.TempDir() via EmitOpts.Dir, so no test ever
// touches the real ~/.config/atcr/scorecard/ store.
package scorecard

import (
	"os"
	"path/filepath"
	"testing"
)

const integRunID = "2026-06-14T10:00:00Z-abc123"
const integMonthFile = "2026-06.jsonl"

// TestIntegration_ReconcileEmitRead drives emit -> FindByRunID and asserts the
// round-tripped reviewer/model/schema fields survive the JSONL store.
func TestIntegration_ReconcileEmitRead(t *testing.T) {
	dir := t.TempDir()

	in := EmitInput{
		RunID: integRunID,
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce", "alice"}}, // corroborated
			{File: "b.go", Line: 2, Problem: "y", Reviewers: []string{"bruce"}},          // solo
		},
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "claude-sonnet-4-6", TokensIn: 14200, TokensOut: 4000, LatencyMS: 9100},
		},
	}
	if err := Emit(in, EmitOpts{Dir: dir}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	recs, err := FindByRunID(dir, integRunID, ReadOpts{})
	if err != nil {
		t.Fatalf("FindByRunID: %v", err)
	}

	var bruce *Record
	var sawAggregate bool
	for i := range recs {
		switch recs[i].RecordType {
		case RecordTypeReviewer:
			if recs[i].Reviewer == "bruce" {
				bruce = &recs[i]
			}
		case RecordTypeAggregate:
			sawAggregate = true
		}
	}
	if bruce == nil {
		t.Fatalf("no reviewer record for bruce in %d records", len(recs))
	}
	if !sawAggregate {
		t.Errorf("expected one aggregate record alongside the reviewer record")
	}

	if bruce.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", bruce.SchemaVersion, SchemaVersion)
	}
	if bruce.RunID != integRunID {
		t.Errorf("run_id = %q, want %q", bruce.RunID, integRunID)
	}
	if bruce.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", bruce.Model)
	}
	if bruce.Role != "reviewer" {
		t.Errorf("role = %q, want reviewer", bruce.Role)
	}
	if bruce.FindingsRaised != 2 || bruce.FindingsCorroborated != 1 || bruce.FindingsSolo != 1 {
		t.Errorf("counts raised/corr/solo = %d/%d/%d, want 2/1/1",
			bruce.FindingsRaised, bruce.FindingsCorroborated, bruce.FindingsSolo)
	}
	if bruce.CorroborationRate != 0.5 {
		t.Errorf("corroboration_rate = %v, want 0.5", bruce.CorroborationRate)
	}
	if bruce.TokensIn != 14200 || bruce.TokensOut != 4000 {
		t.Errorf("tokens in/out = %d/%d, want 14200/4000", bruce.TokensIn, bruce.TokensOut)
	}
	if bruce.LatencyMS != 9100 {
		t.Errorf("latency_ms = %d, want 9100", bruce.LatencyMS)
	}
	// No verification.json was supplied: conditional fields must be omitted (nil).
	if bruce.FindingsVerified != nil || bruce.FindingsRefuted != nil || bruce.SurvivedSkepticRate != nil {
		t.Errorf("verification fields should be nil without verification.json")
	}
}

// TestIntegration_ReconcileEmitRead_WithVerification drives emit with a valid
// verification.json and asserts the conditional skeptic fields are populated
// (the emission path complementing the omission path above).
func TestIntegration_ReconcileEmitRead_WithVerification(t *testing.T) {
	dir := t.TempDir()

	verificationJSON := `{"findings":[
		{"file":"a.go","line":1,"problem":"x","verdict":"confirmed"},
		{"file":"b.go","line":2,"problem":"y","verdict":"refuted"}
	]}`
	verPath := filepath.Join(t.TempDir(), "verification.json")
	if err := os.WriteFile(verPath, []byte(verificationJSON), 0o600); err != nil {
		t.Fatalf("write verification.json: %v", err)
	}

	in := EmitInput{
		RunID: integRunID,
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce", "alice"}}, // confirmed
			{File: "b.go", Line: 2, Problem: "y", Reviewers: []string{"bruce"}},          // refuted
		},
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "claude-sonnet-4-6", TokensIn: 100, TokensOut: 50, LatencyMS: 1000},
		},
		VerificationPath: verPath,
	}
	if err := Emit(in, EmitOpts{Dir: dir}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	recs, err := FindByRunID(dir, integRunID, ReadOpts{})
	if err != nil {
		t.Fatalf("FindByRunID: %v", err)
	}
	var bruce *Record
	for i := range recs {
		if recs[i].RecordType == RecordTypeReviewer && recs[i].Reviewer == "bruce" {
			bruce = &recs[i]
		}
	}
	if bruce == nil {
		t.Fatalf("no reviewer record for bruce in %d records", len(recs))
	}
	if bruce.FindingsVerified == nil || bruce.FindingsRefuted == nil || bruce.SurvivedSkepticRate == nil {
		t.Fatalf("verification fields must be populated when verification.json is present")
	}
	if *bruce.FindingsVerified != 1 {
		t.Errorf("findings_verified = %d, want 1", *bruce.FindingsVerified)
	}
	if *bruce.FindingsRefuted != 1 {
		t.Errorf("findings_refuted = %d, want 1", *bruce.FindingsRefuted)
	}
	if *bruce.SurvivedSkepticRate != 0.5 {
		t.Errorf("survived_skeptic_rate = %v, want 0.5", *bruce.SurvivedSkepticRate)
	}
}

// TestIntegration_ReconcileEmitAggregate drives emit (multiple reviewers) ->
// Aggregate and asserts the ranked-by-corroboration-rate ordering.
func TestIntegration_ReconcileEmitAggregate(t *testing.T) {
	dir := t.TempDir()

	in := EmitInput{
		RunID: integRunID,
		Findings: []Finding{
			// bruce: 2 raised, both corroborated -> rate 1.0
			{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce", "dave"}},
			{File: "b.go", Line: 2, Problem: "y", Reviewers: []string{"bruce", "dave"}},
			// carol: 2 raised, none corroborated -> rate 0.0
			{File: "c.go", Line: 3, Problem: "z", Reviewers: []string{"carol"}},
			{File: "d.go", Line: 4, Problem: "w", Reviewers: []string{"carol"}},
		},
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "model-a", TokensIn: 100, TokensOut: 50, LatencyMS: 1000},
			"carol": {Model: "model-b", TokensIn: 200, TokensOut: 80, LatencyMS: 2000},
		},
	}
	if err := Emit(in, EmitOpts{Dir: dir}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	all, err := ReadAll(dir, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	rows := Aggregate(all)
	if len(rows) != 2 {
		t.Fatalf("Aggregate returned %d rows, want 2 (aggregate record must be skipped)", len(rows))
	}
	if rows[0].Reviewer != "bruce" || rows[0].CorroborationRate != 1.0 {
		t.Errorf("rank[0] = %s @ %v, want bruce @ 1.0", rows[0].Reviewer, rows[0].CorroborationRate)
	}
	if rows[1].Reviewer != "carol" || rows[1].CorroborationRate != 0.0 {
		t.Errorf("rank[1] = %s @ %v, want carol @ 0.0", rows[1].Reviewer, rows[1].CorroborationRate)
	}
}

// TestIntegration_NoScorecardSuppresses drives emit with NoScorecard=true and
// asserts the store stays empty (no file, no directory entries written).
func TestIntegration_NoScorecardSuppresses(t *testing.T) {
	dir := t.TempDir()

	in := EmitInput{
		RunID:     integRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	if err := Emit(in, EmitOpts{Dir: dir, NoScorecard: true}); err != nil {
		t.Fatalf("Emit(NoScorecard): %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, integMonthFile)); !os.IsNotExist(err) {
		t.Errorf("month file should not exist when suppressed; stat err = %v", err)
	}
	recs, err := ReadAll(dir, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("store has %d records when suppressed, want 0", len(recs))
	}
}

// --- Phase 3 (Story 03): opportunity-set differential ----------------------

// representativeCorpus is a hand-built stand-in for a real panel's output
// distribution, per AC 03-05's Test Data Requirements. Each entry is ONE case's
// union of raised categories, drawn across the five groups reconcile/category.go
// documents: defect classes, contract/interface, resource/dependency,
// structure/design, and cross-cutting concerns.
//
// It is deliberately NOT the adversarial shape from Edge Case 2 (every case
// touching every remit), which is covered separately below.
var representativeCorpus = [][]string{
	// API-contract-heavy cases.
	{"api-contract", "contract"},
	{"api-contract", "correctness"},
	{"contract", "naming"},
	// Error-handling / testing-heavy cases.
	{"error-handling", "testing"},
	{"testing", "correctness"},
	{"error-handling", "resource-leak"},
	{"testing"},
	// Security cases.
	{"security", "input-validation"},
	{"secret", "observability"},
	{"security", "correctness"},
	// Pure structure / style cleanup — no specialist remit in play.
	{"style", "naming"},
	{"docs", "naming"},
	{"style", "maintainability"},
	{"maintainability", "complexity"},
	{"docs"},
	// Performance cases.
	{"performance", "complexity"},
	{"performance", "leak"},
	// Mixed correctness cases.
	{"correctness", "state"},
	{"logic", "invariant"},
	{"correctness", "error-handling", "state"},
	// One fully clean case: nobody raised anything.
	{},
}

// opportunitySetSize counts the corpus cases that are an opportunity for
// persona. Test-only helper (AC 03-05 explicitly keeps it out of the production
// file — it has no production caller).
func opportunitySetSize(persona string, cases [][]string) int {
	n := 0
	for _, c := range cases {
		if InOpportunitySet(persona, c) {
			n++
		}
	}
	return n
}

// TestOpportunitySet_SpecialistsMaterialSmallerThanGeneralist is AC 03-05 Happy
// Path Scenario 1, with C11's substitution applied: vera is unmapped under
// Option A, so its opportunity-set size would be a constant 0 and the assertion
// would pass for the wrong reason. The specialists measured here are the in-repo
// dax (testing/error-handling) and sasha (security), exactly as AC 03-05's own
// blocked-on note instructs.
func TestOpportunitySet_SpecialistsMaterialSmallerThanGeneralist(t *testing.T) {
	bruce := opportunitySetSize("bruce", representativeCorpus)
	dax := opportunitySetSize("dax", representativeCorpus)
	sasha := opportunitySetSize("sasha", representativeCorpus)

	if bruce <= dax+2 {
		t.Errorf("generalist bruce (%d) must be MATERIALLY larger than specialist dax (%d)", bruce, dax)
	}
	if bruce <= sasha+2 {
		t.Errorf("generalist bruce (%d) must be MATERIALLY larger than specialist sasha (%d)", bruce, sasha)
	}
	if bruce >= len(representativeCorpus) {
		t.Errorf("even the generalist must not be in-remit on every case (%d of %d)", bruce, len(representativeCorpus))
	}
}

// TestOpportunitySet_SilentSpecialistDoesNotDisturbOthers is AC 03-05 Happy Path
// Scenario 2: each persona's membership is computed independently from the same
// shared raised-category data, so a specialist's correct silence cannot inflate
// or deflate anyone else's set.
func TestOpportunitySet_SilentSpecialistDoesNotDisturbOthers(t *testing.T) {
	bruceBefore := opportunitySetSize("bruce", representativeCorpus)
	daxBefore := opportunitySetSize("dax", representativeCorpus)

	// Re-running over the same corpus, in a different persona order, must not
	// change either answer — the predicate holds no cross-persona state.
	if got := opportunitySetSize("dax", representativeCorpus); got != daxBefore {
		t.Errorf("dax's set changed between runs: %d then %d", daxBefore, got)
	}
	if got := opportunitySetSize("bruce", representativeCorpus); got != bruceBefore {
		t.Errorf("bruce's set changed between runs: %d then %d", bruceBefore, got)
	}
}

// TestOpportunitySet_CleanCaseCountsForNobody is AC 03-05 Error Scenario 1,
// asserted explicitly rather than allowed to pass as an off-by-one in the corpus
// size.
func TestOpportunitySet_CleanCaseCountsForNobody(t *testing.T) {
	clean := [][]string{{}}
	for _, p := range []string{"bruce", "dax", "sasha", "otto", "penny", "kai", "mira", "greta", "ingrid"} {
		if got := opportunitySetSize(p, clean); got != 0 {
			t.Errorf("a clean case must contribute 0 to %q's set, got %d", p, got)
		}
	}
}

// TestOpportunitySet_CorpusWithNoMatchingCategoryIsAnExplicitZero is AC 03-05
// Edge Case 1: a degenerate zero is a valid outcome, not a crash and not a test
// failure.
func TestOpportunitySet_CorpusWithNoMatchingCategoryIsAnExplicitZero(t *testing.T) {
	noSecurity := [][]string{{"style", "naming"}, {"docs"}, {"performance"}}
	if got := opportunitySetSize("sasha", noSecurity); got != 0 {
		t.Errorf("sasha's opportunity set over a security-free corpus must be 0, got %d", got)
	}
}

// TestOpportunitySet_AdversarialCorpusCollapsesTheDifferential is AC 03-05 Edge
// Case 2, documented as a known collapse point rather than silently ignored:
// when every case touches every remit, specialist and generalist sets are equal.
// That is a correct consequence of the membership rule, and it is why the
// representative corpus above is deliberately NOT built this way.
func TestOpportunitySet_AdversarialCorpusCollapsesTheDifferential(t *testing.T) {
	everything := []string{}
	for _, p := range []string{"bruce", "dax", "sasha", "otto", "penny", "kai", "mira", "greta", "ingrid"} {
		cats, ok := RemitCategories(p)
		if !ok {
			t.Fatalf("persona %q must be mapped", p)
		}
		everything = append(everything, cats...)
	}
	adversarial := [][]string{everything, everything, everything}

	bruce := opportunitySetSize("bruce", adversarial)
	dax := opportunitySetSize("dax", adversarial)
	if bruce != len(adversarial) || dax != len(adversarial) {
		t.Errorf("every persona must be in-remit on every adversarial case: bruce=%d dax=%d of %d",
			bruce, dax, len(adversarial))
	}
}
