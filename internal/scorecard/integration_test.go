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

	recs, err := FindByRunID(dir, integRunID)
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

	all, err := ReadAll(dir)
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
	recs, err := ReadAll(dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("store has %d records when suppressed, want 0", len(recs))
	}
}
