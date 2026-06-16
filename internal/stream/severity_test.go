package stream

import "testing"

// TestSeverityRank_CanonicalOrdering pins the canonical rubric values and the
// strictly-descending ordering that every consumer relies on. Unknown tokens
// must be absent (rank 0).
func TestSeverityRank_CanonicalOrdering(t *testing.T) {
	want := map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
	for sev, rank := range want {
		if got := SeverityRank[sev]; got != rank {
			t.Errorf("SeverityRank[%q] = %d, want %d", sev, got, rank)
		}
	}
	if !(SeverityRank["CRITICAL"] > SeverityRank["HIGH"] &&
		SeverityRank["HIGH"] > SeverityRank["MEDIUM"] &&
		SeverityRank["MEDIUM"] > SeverityRank["LOW"] &&
		SeverityRank["LOW"] > 0) {
		t.Errorf("ordering not strictly descending CRITICAL>HIGH>MEDIUM>LOW>0")
	}
	if _, ok := SeverityRank["unknown"]; ok {
		t.Errorf("unknown severity must be absent from the rank map (rank 0)")
	}
}

// TestNormalizeSeverity proves the canonical normalizer upper-cases and trims so
// a mixed-case or padded token resolves identically through the rank map.
func TestNormalizeSeverity(t *testing.T) {
	cases := map[string]string{
		"critical": "CRITICAL",
		"  High  ": "HIGH",
		"MEDIUM":   "MEDIUM",
		" low":     "LOW",
		"":         "",
	}
	for in, want := range cases {
		if got := NormalizeSeverity(in); got != want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", in, got, want)
		}
	}
	if SeverityRank[NormalizeSeverity(" critical ")] != 4 {
		t.Errorf("normalized mixed-case severity must resolve to its canonical rank")
	}
}
