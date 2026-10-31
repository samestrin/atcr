package reconcile

import (
	"encoding/json"
	"testing"
)

// Epic 99 T4: the discount reason comes from the rule that applied it, not from
// comparing the discounted confidence to the undiscounted one, and the
// independence test reads per-reviewer quotes carried on Merged.

func TestDiscountedConfidence_ReturnsTheReasonWithTheConfidence(t *testing.T) {
	cases := []struct {
		name        string
		count       int
		category    string
		independent bool
		conf, why   string
	}{
		{"panel-wide invariant", 5, CategoryInvariant, false, ConfMedium, PanelWideReason},
		{"canonicalized category", 2, " Invariant ", false, ConfMedium, PanelWideReason},
		{"independent invariant", 5, CategoryInvariant, true, ConfHigh, ""},
		{"non-invariant", 5, CategoryCorrectness, false, ConfHigh, ""},
		{"singleton", 1, CategoryInvariant, false, ConfMedium, ""},
	}
	for _, tc := range cases {
		conf, why := DiscountedConfidence(tc.count, tc.category, tc.independent)
		if conf != tc.conf || why != tc.why {
			t.Errorf("%s: DiscountedConfidence = (%q, %q), want (%q, %q)", tc.name, conf, why, tc.conf, tc.why)
		}
		if got := ConfidenceForCategory(tc.count, tc.category, tc.independent); got != conf {
			t.Errorf("%s: ConfidenceForCategory = %q, want DiscountedConfidence's %q", tc.name, got, conf)
		}
	}
}

func TestMerge_CarriesEachFindingsQuote(t *testing.T) {
	g := []Finding{
		predicateFinding("bru,ce", " Invariant", "key.go", siblingQuote),
		predicateFinding("kai", CategoryCorrectness, "key.go", "x == y"),
		predicateFinding("kai", CategoryInvariant, "key.go", ""),
	}
	got := Merge(g).SourceQuotes
	want := []SourceQuote{
		{Reviewer: "bruce", File: "key.go", Category: CategoryInvariant, Evidence: siblingQuote},
		{Reviewer: "kai", File: "key.go", Category: CategoryCorrectness, Evidence: "x == y"},
		{Reviewer: "kai", File: "key.go", Category: CategoryInvariant, Evidence: ""},
	}
	if len(got) != len(want) {
		t.Fatalf("SourceQuotes = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SourceQuotes[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// IsPanelWide over the findings and IsPanelWideQuotes over the quotes Merge
// carries are one predicate, so a caller holding only a Merged decides
// independence exactly as Merge did.
func TestIsPanelWideQuotes_AgreesWithIsPanelWide(t *testing.T) {
	groups := map[string][]Finding{
		"panel-wide": fivePredicateFindings(),
		"distinct files": {
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "hash.go", siblingQuote),
		},
		"empty evidence": {
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "key.go", ""),
		},
		"singleton": {predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote)},
	}
	for name, g := range groups {
		m := Merge(g)
		if got, want := IsPanelWideQuotes(m.SourceQuotes), IsPanelWide(g); got != want {
			t.Errorf("%s: IsPanelWideQuotes = %v, IsPanelWide = %v", name, got, want)
		}
		conf, why := DiscountedConfidence(len(m.Reviewers), m.Category, !IsPanelWideQuotes(m.SourceQuotes))
		if m.Confidence != conf || m.ConfidenceDiscount != why {
			t.Errorf("%s: Merge = (%q, %q), DiscountedConfidence = (%q, %q)", name, m.Confidence, m.ConfidenceDiscount, conf, why)
		}
	}
}

func TestMerged_SourceQuotesNotOnTheWire(t *testing.T) {
	m := Merge(fivePredicateFindings())
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(m.Finding)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, string(got), string(want), "a Merged marshals as its Finding alone")
}
