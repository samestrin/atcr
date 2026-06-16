package reconcile

import (
	"testing"

	"github.com/samestrin/atcr/internal/stream"
)

// TestMerge_NormalizesMixedCaseSeverity proves the merge.go:104 boundary lookup
// normalizes casing: a lower-cased "critical" from raw reviewer output still
// outranks a canonical "LOW" instead of being silently dropped as unknown.
func TestMerge_NormalizesMixedCaseSeverity(t *testing.T) {
	group := []stream.Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "LOW", File: "a.go", Line: 1, Reviewer: "r2"},
	}
	got := Merge(group)
	if rank := SeverityRank[stream.NormalizeSeverity(got.Severity)]; rank != 4 {
		t.Fatalf("Merge severity = %q (rank %d), want a CRITICAL-rank (4) winner", got.Severity, rank)
	}
}

// TestGrayZoneItem_NormalizesMixedCaseSeverity proves the disagree.go:338/360
// boundary lookups normalize casing: a gray-zone cluster of mixed-case CRITICAL
// findings scores by rank 4 (scoreFor with spread 0 returns the severity rank),
// not collapsed to the unknown-severity floor of 1.
func TestGrayZoneItem_NormalizesMixedCaseSeverity(t *testing.T) {
	c := AmbiguousCluster{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []stream.Finding{
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
		Findings: []stream.Finding{
			{Severity: "critical", File: "g.go", Line: 7, Reviewer: "r1"},
		},
	}
	got := grayZoneItem(c)
	if got.Severity != "CRITICAL" {
		t.Fatalf("grayZoneItem Severity = %q, want normalized CRITICAL; raw maxSev corrupts sort tiebreak", got.Severity)
	}
}

// TestMerge_MixedCaseDuplicateIsNotADisagreement guards the adversarial fix: a
// group whose only severities are casing variants of one level must merge to a
// single canonical severity with no disagreement annotation. Before the seen-set
// was keyed by the normalized form, "critical" + "CRITICAL" produced a spurious
// "critical vs CRITICAL" disagreement.
func TestMerge_MixedCaseDuplicateIsNotADisagreement(t *testing.T) {
	group := []stream.Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "CRITICAL", File: "a.go", Line: 1, Reviewer: "r2"},
	}
	got := Merge(group)
	if got.Disagreement != "" {
		t.Fatalf("Disagreement = %q, want empty (mixed-case duplicate is one severity)", got.Disagreement)
	}
	if got.Severity != "CRITICAL" {
		t.Fatalf("Severity = %q, want canonical CRITICAL", got.Severity)
	}
}
