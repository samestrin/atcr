package reconcile

import "testing"

// The Merge / sortMerged severity-normalization tests moved into the reconcile
// library with their code (Epic 8.0). The tests remaining here exercise the
// ATCR-internal disagreement radar (grayZoneItem / soloItem) and gate (AtOrAbove),
// which still live in internal/reconcile.

// TestGrayZoneItem_NormalizesMixedCaseSeverity proves the disagree.go boundary
// lookups normalize casing: a gray-zone cluster of mixed-case CRITICAL findings
// scores by rank 4 (scoreFor with spread 0 returns the severity rank), not
// collapsed to the unknown-severity floor of 1.
func TestGrayZoneItem_NormalizesMixedCaseSeverity(t *testing.T) {
	c := AmbiguousCluster{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []Finding{
			{Severity: "critical", File: "g.go", Line: 7, Reviewer: "r1"},
			{Severity: "Critical", File: "g.go", Line: 7, Reviewer: "r2"},
		},
	}
	got := grayZoneItem(c)
	if got.Score != 4 {
		t.Fatalf("grayZoneItem score = %v, want 4 (rank of CRITICAL); a raw lookup collapses it to the floor", got.Score)
	}
}

// TestSoloItem_LowercaseSeverityScoresCorrectly guards the finding-level
// SeverityRank lookup in soloItem: a lowercase "high" solo must score 3 (HIGH
// rank), not 0 from a map miss on the raw key.
func TestSoloItem_LowercaseSeverityScoresCorrectly(t *testing.T) {
	findings := []JSONFinding{
		{Severity: "high", File: "a.go", Line: 1, Problem: "solo",
			Reviewers: []string{"greta"}, Confidence: ConfMedium},
	}
	df := BuildDisagreements(findings, nil)
	solos := itemsByKind(df, KindSoloFinding)
	if len(solos) != 1 {
		t.Fatalf("expected 1 solo, got %d", len(solos))
	}
	if solos[0].Score != 3.0 {
		t.Fatalf("soloItem Score = %v, want 3.0 (HIGH rank); raw 'high' key misses map, scores 0", solos[0].Score)
	}
}

// TestGrayZoneItem_NormalizesSeverityField guards that maxSev is stored in
// normalized form so the returned Severity field is canonical and the
// sortDisagreements tiebreak ranks it correctly.
func TestGrayZoneItem_NormalizesSeverityField(t *testing.T) {
	c := AmbiguousCluster{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []Finding{
			{Severity: "critical", File: "g.go", Line: 7, Reviewer: "r1"},
		},
	}
	got := grayZoneItem(c)
	if got.Severity != "CRITICAL" {
		t.Fatalf("grayZoneItem Severity = %q, want normalized CRITICAL; raw maxSev corrupts sort tiebreak", got.Severity)
	}
}

// TestAtOrAbove_NormalizesMixedCaseSeverity guards the gate.go lookups: AtOrAbove
// must canonicalize a non-canonical severity or threshold itself, so a
// lower-cased input does not silently miss SeverityRank and flip a gate decision.
func TestAtOrAbove_NormalizesMixedCaseSeverity(t *testing.T) {
	if !AtOrAbove("high", "HIGH") {
		t.Fatalf("AtOrAbove(\"high\", \"HIGH\") = false, want true; a raw lookup misses the lowercase severity key")
	}
	if !AtOrAbove("HIGH", "high") {
		t.Fatalf("AtOrAbove(\"HIGH\", \"high\") = false, want true; a raw lookup misses the lowercase threshold key")
	}
}
