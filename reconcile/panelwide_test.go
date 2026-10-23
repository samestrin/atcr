package reconcile

import (
	"strings"
	"testing"
)

// predicateFinding is one reviewer's filing under the predicate-exhaustiveness
// rule: CATEGORY invariant on the edited branch, quoting the sibling branch.
func predicateFinding(reviewer, category, file, evidence string) Finding {
	return mf("MEDIUM", file, 42,
		"equal() compares Name in the edited branch but the sibling branch omits it",
		"compare Name in both branches", category, 15, evidence, reviewer)
}

const siblingQuote = "case b: return a.ID == b.ID && a.Kind == b.Kind"

func TestIsPanelWide(t *testing.T) {
	five := func(category string) []Finding {
		var g []Finding
		for _, r := range []string{"bruce", "greta", "kai", "mira", "otto"} {
			g = append(g, predicateFinding(r, category, "key.go", siblingQuote))
		}
		return g
	}

	cases := []struct {
		name  string
		group []Finding
		want  bool
	}{
		{"five reviewers file the same predicate finding", five(CategoryInvariant), true},
		{"reworded quote of the same sibling branch is still near-identical", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "key.go", "`case b: return a.ID == b.ID && a.Kind == b.Kind` (sibling)"),
		}, true},
		{"penny's correctness variant joins an invariant majority", append(five(CategoryInvariant),
			predicateFinding("penny", CategoryCorrectness, "key.go", siblingQuote)), true},
		{"category matched case-insensitively, as ModalCategory does", []Finding{
			predicateFinding("bruce", "Invariant", "key.go", siblingQuote),
			predicateFinding("kai", " INVARIANT ", "key.go", siblingQuote),
		}, true},

		{"two reviewers, invariant, distinct files", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "cache.go", siblingQuote),
		}, false},
		{"two reviewers, invariant, distinct evidence", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "key.go", "cache.Put(k, v) runs before the lock is taken in Refresh"),
		}, false},
		{"one member with distinct evidence breaks a five-reviewer group", append(five(CategoryInvariant)[:4],
			predicateFinding("otto", CategoryInvariant, "key.go", "TestEqual only covers the Kind mismatch")), false},
		{"one quote against an empty evidence field is not near-identical", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "key.go", ""),
		}, false},
		{"two empty evidence fields quote nothing, so they are not near-identical", emptyEvidencePair(), false},
		{"one empty evidence field among quotes breaks the group", append(five(CategoryInvariant)[:4],
			predicateFinding("otto", CategoryInvariant, "key.go", "")), false},
		{"non-invariant group", five(CategoryCorrectness), false},
		{"mixed cluster whose modal category is correctness", append(five(CategoryCorrectness),
			predicateFinding("penny", CategoryInvariant, "key.go", siblingQuote)), false},
		{"single reviewer has no agreement to discount", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
		}, false},
		{"one reviewer filing twice is not two reviewers", []Finding{
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
		}, false},
		{"empty group", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPanelWide(tc.group); got != tc.want {
				t.Errorf("IsPanelWide = %v, want %v", got, tc.want)
			}
		})
	}
}

// emptyEvidencePair is two reviewers filing one invariant finding in one file,
// neither quoting any evidence.
func emptyEvidencePair() []Finding {
	return []Finding{
		predicateFinding("bruce", CategoryInvariant, "key.go", ""),
		predicateFinding("kai", CategoryInvariant, "key.go", " "),
	}
}

// Epic 75 T2: a group whose members quote nothing cites no shared sibling
// branch, so Merge keeps its reviewer-count confidence and records no discount.
func TestMerge_EmptyEvidenceIsNotDiscounted(t *testing.T) {
	m := Merge(emptyEvidencePair())
	if m.ConfidenceDiscount != "" {
		t.Errorf("ConfidenceDiscount = %q, want empty", m.ConfidenceDiscount)
	}
	if want := ConfidenceFor(2); m.Confidence != want {
		t.Errorf("Confidence = %q, want %q", m.Confidence, want)
	}
}

func TestPanelWideReason_IsShortAndNamesTheClass(t *testing.T) {
	if strings.ContainsAny(PanelWideReason, "\n|") {
		t.Errorf("reason must fit one TD table cell: %q", PanelWideReason)
	}
	if !strings.Contains(PanelWideReason, CategoryInvariant) {
		t.Errorf("reason should name the category it keys on: %q", PanelWideReason)
	}
	if n := len(PanelWideReason); n >= 80 {
		t.Errorf("reason is %d characters, want under 80 (the long form belongs in the doc block): %q", n, PanelWideReason)
	}
	// internal/localdebt asserts the stored record carries this prefix.
	if !strings.HasPrefix(PanelWideReason, "panel-wide instruction:") {
		t.Errorf("reason must start with %q: %q", "panel-wide instruction:", PanelWideReason)
	}
}
