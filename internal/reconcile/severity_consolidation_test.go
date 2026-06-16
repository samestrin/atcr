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
