package reconcile

import (
	"encoding/json"
	"testing"
)

// mf builds a per-source finding with the merge-relevant fields.
func mf(sev, file string, line int, problem, fix, category string, est int, evidence, reviewer string) Finding {
	return Finding{
		Severity: sev, File: file, Line: line, Problem: problem, Fix: fix,
		Category: category, EstMinutes: est, Evidence: evidence, Reviewer: reviewer,
	}
}

func TestMerge_OutOfScopeFailClosed(t *testing.T) {
	// Fail-closed: out-of-scope suppresses gating only when EVERY reviewer
	// tagged the finding out-of-scope. A 2-vs-1 out-of-scope majority must not
	// drop the real category — the real category wins and the finding still gates.
	mixed := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "mira"),
	}
	eq(t, Merge(mixed).Category, "security", "real category wins over an out-of-scope majority")

	unanimous := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
	}
	eq(t, Merge(unanimous).Category, CategoryOutOfScope, "unanimous out-of-scope stays annotated")
}

func TestMerge_ReviewersJoinedDedupedSorted(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e1", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e2", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e3", "kai"), // dup reviewer
	})
	deepEq(t, m.Reviewers, []string{"greta", "kai"}, "reviewers deduped and sorted")
	eq(t, m.Confidence, ConfHigh, "2 distinct reviewers → HIGH")
}

func TestMerge_ReviewerNamesEnforceNoCommaInvariant(t *testing.T) {
	// The reconciled reviewers cell is comma-joined on the wire (v1 findings and
	// the axi payload alike), so a comma in a source reviewer name would read back
	// as two names. Merge normalizes names to the no-comma invariant at reconcile
	// time, regardless of what an upstream source emitted.
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e1", "br,uce"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e2", "greta"),
	})
	deepEq(t, m.Reviewers, []string{"bruce", "greta"}, "comma-bearing reviewer name is normalized before dedup")
}

func TestMerge_SingleReviewerIsMedium(t *testing.T) {
	m := Merge([]Finding{mf("LOW", "a.go", 1, "p", "f", "style", 5, "e", "greta")})
	eq(t, m.Confidence, ConfMedium, "single reviewer → MEDIUM")
	eq(t, m.Disagreement, "", "no disagreement for a single finding")
}

func TestMerge_MaxSeverityWithDisagreement(t *testing.T) {
	m := Merge([]Finding{
		mf("CRITICAL", "a.go", 42, "long detailed problem text", "f", "sec", 30, "e", "greta"),
		mf("LOW", "a.go", 42, "short", "fix detail longer", "perf", 60, "e", "kai"),
	})
	eq(t, m.Severity, "CRITICAL", "max severity wins")
	eq(t, m.Disagreement, "LOW vs CRITICAL", "disagreement annotation")
}

func TestMerge_LongestProblemAndFix_MaxEst(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "short", "f1", "sec", 15, "e", "greta"),
		mf("HIGH", "a.go", 1, "a much longer and more detailed problem", "longer fix text", "sec", 45, "e", "kai"),
	})
	eq(t, m.Problem, "a much longer and more detailed problem", "longest problem")
	eq(t, m.Fix, "longer fix text", "longest fix")
	eq(t, m.EstMinutes, 45, "max estimate (most pessimistic)")
}

func TestMerge_ModalCategoryAlphabeticTiebreak(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e", "b"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "c"),
	})
	eq(t, m.Category, "security", "modal category")

	tie := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "correctness", 10, "e", "b"),
	})
	eq(t, tie.Category, "correctness", "alphabetical tiebreak")
}

func TestMerge_EmptyGroupDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Merge(nil) panicked: %v", r)
		}
	}()
	_ = Merge(nil)
}

func TestMerge_EmptyCategoryDoesNotHijackTie(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "b"),
	})
	eq(t, m.Category, "security", "real category preferred over empty on a tie")
}

func TestMerge_EvidenceReviewerPrefixedWhenMultiple(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw X", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw Y", "kai"),
	})
	contains(t, m.Evidence, "[greta] saw X", "reviewer-prefixed evidence")
	contains(t, m.Evidence, "[kai] saw Y", "reviewer-prefixed evidence")
}

func TestMerge_ModalCategoryCanonicalizesCase(t *testing.T) {
	nonCanonical := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "kai"),
	}
	eq(t, Merge(nonCanonical).Category, CategoryOutOfScope,
		"non-canonical 'Out-Of-Scope' normalized to canonical 'out-of-scope'")

	mixedCase := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "OUT-OF-SCOPE", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "out-of-scope", 10, "e", "kai"),
	}
	eq(t, Merge(mixedCase).Category, CategoryOutOfScope,
		"all-caps and canonical variants collapse to the same canonical form")
}

func TestMerge_ModalCategoryCanonicalizesAllCategories(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Security", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "SECURITY", 10, "e", "kai"),
	})
	eq(t, m.Category, "security", "non-canonical category casing normalized to lowercase")
}

func TestMerge_MixedCaseDuplicateIsNotADisagreement(t *testing.T) {
	got := Merge([]Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "CRITICAL", File: "a.go", Line: 1, Reviewer: "r2"},
	})
	eq(t, got.Disagreement, "", "mixed-case duplicate is one severity, no disagreement")
	eq(t, got.Severity, "CRITICAL", "canonical CRITICAL")
}

func TestMerge_NormalizesMixedCaseSeverity(t *testing.T) {
	got := Merge([]Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "LOW", File: "a.go", Line: 1, Reviewer: "r2"},
	})
	eq(t, SeverityRank[NormalizeSeverity(got.Severity)], 4, "lowercase critical still ranks 4")
}

// TestMerge_AllUnknownSeverityFallbackIsNormalized proves the all-unknown-severity
// fallback in MergeSeverity returns a normalized form, consistent with every
// known-severity path that also returns canonical uppercase.
func TestMerge_AllUnknownSeverityFallbackIsNormalized(t *testing.T) {
	group := []Finding{
		{Severity: "xyzzy", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "also-unknown", File: "a.go", Line: 1, Reviewer: "r2"},
	}
	eq(t, Merge(group).Severity, NormalizeSeverity(group[0].Severity), "all-unknown fallback normalized")
}

func TestMaxEstMinutes_NegativeEstimates(t *testing.T) {
	group := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", -20, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", -10, "e", "b"),
	}
	eq(t, MaxEstMinutes(group), -10, "max of negative estimates is the least negative")
}

func TestMaxEstMinutes_EmptyGroup(t *testing.T) {
	eq(t, MaxEstMinutes(nil), 0, "empty group returns 0")
}

// TestMerge_VerificationNotPropagated documents the TD-005 disposition: input
// Verification blocks are intentionally dropped by Merge; the caller stamps
// Verification post-reconcile after the verify stage resolves verdicts.  This
// test guards against accidentally propagating Verification in a future refactor.
func TestMerge_VerificationNotPropagated(t *testing.T) {
	v := &Verification{Verdict: "CONFIRMED", Skeptic: "greta"}
	group := []Finding{
		{File: "a.go", Line: 1, Problem: "p", Fix: "f", Reviewer: "greta", Verification: v},
		{File: "a.go", Line: 1, Problem: "p", Fix: "f", Reviewer: "kai"},
	}
	merged := Merge(group)
	if merged.Verification != nil {
		t.Errorf("Merge must not propagate Verification (TD-005: stamped post-reconcile by caller), got %+v", merged.Verification)
	}
}

func TestMerge_SourceCategoriesKeepEachSourcesOwnWord(t *testing.T) {
	// A mixed-category cluster: the modal Category is the cluster's vote, and
	// SourceCategories keeps what each source finding itself said, in group
	// order, so a consumer that needs a lens's own word can still read it.
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", " Security ", 10, "e1", "bruce"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e2", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e3", "kai"),
	})
	eq(t, m.Category, "performance", "modal category is unchanged")
	deepEq(t, m.SourceCategories, []SourceCategory{
		{Reviewer: "bruce", Category: "security"},
		{Reviewer: "greta", Category: "performance"},
		{Reviewer: "kai", Category: "performance"},
	}, "each source keeps its own canonicalized category")
}

func TestMerge_SourceCategoriesNormalizeReviewerLikeReviewers(t *testing.T) {
	// The reviewer on a SourceCategory is normalized exactly as Reviewers is (the
	// no-comma invariant), so the two can be matched by string equality.
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e1", "br,uce"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e2", "greta"),
	})
	eq(t, m.Category, "security", "fail-closed modal still drops out-of-scope")
	deepEq(t, m.SourceCategories, []SourceCategory{
		{Reviewer: "bruce", Category: CategoryOutOfScope},
		{Reviewer: "greta", Category: "security"},
	}, "a source's out-of-scope word survives beside the modal value")
}

func TestMerged_SourceCategoriesNotOnTheWire(t *testing.T) {
	// SourceCategories is in-process provenance: a reconciled finding must
	// serialize exactly as it did before the field existed.
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e1", "bruce"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e2", "greta"),
	})
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(m.Finding)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, string(got), string(want), "Merged marshals as its Finding alone")
}

// Epic 35.16.17 T2: Merge discounts the panel-wide-instruction class. Reviewer
// count alone does not make the class HIGH; the discounted finding lands at
// MEDIUM and is kept.

func fivePredicateFindings() []Finding {
	var g []Finding
	for _, r := range []string{"bruce", "greta", "kai", "mira", "otto"} {
		g = append(g, predicateFinding(r, CategoryInvariant, "key.go", siblingQuote))
	}
	return g
}

func TestMerge_PanelWideClassIsDiscountedToMedium(t *testing.T) {
	m := Merge(fivePredicateFindings())
	if m.Confidence != ConfMedium {
		t.Fatalf("five reviewers filing one predicate finding: confidence = %q, want %q", m.Confidence, ConfMedium)
	}
	if len(m.Reviewers) != 5 {
		t.Fatalf("reviewers = %v, want all five kept", m.Reviewers)
	}
}

func TestMerge_IndependentInvariantCorroborationStaysHigh(t *testing.T) {
	cases := map[string][]Finding{
		"distinct files": {
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "hash.go", siblingQuote),
		},
		"distinct evidence": {
			predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote),
			predicateFinding("kai", CategoryInvariant, "key.go", "func (k Key) Hash() uint64 { return fnv(k.ID) }"),
		},
	}
	for name, g := range cases {
		if got := Merge(g).Confidence; got != ConfHigh {
			t.Errorf("%s: confidence = %q, want %q", name, got, ConfHigh)
		}
	}
}

func TestMerge_NonInvariantAgreementStaysHigh(t *testing.T) {
	g := []Finding{
		predicateFinding("bruce", CategoryCorrectness, "key.go", siblingQuote),
		predicateFinding("kai", CategoryCorrectness, "key.go", siblingQuote),
	}
	if got := Merge(g).Confidence; got != ConfHigh {
		t.Fatalf("confidence = %q, want %q", got, ConfHigh)
	}
}

func TestConfidenceForCategory(t *testing.T) {
	cases := []struct {
		name        string
		count       int
		category    string
		independent bool
		want        string
	}{
		{"single reviewer", 1, CategoryInvariant, false, ConfMedium},
		{"zero reviewers", 0, CategoryInvariant, true, ConfMedium},
		{"correlated invariant agreement", 5, CategoryInvariant, false, ConfMedium},
		{"category matched case-insensitively", 2, " Invariant ", false, ConfMedium},
		{"independent invariant agreement", 2, CategoryInvariant, true, ConfHigh},
		{"correlated agreement outside the class", 3, CategoryCorrectness, false, ConfHigh},
		{"independent agreement outside the class", 2, CategoryCorrectness, true, ConfHigh},
	}
	for _, c := range cases {
		if got := ConfidenceForCategory(c.count, c.category, c.independent); got != c.want {
			t.Errorf("%s: ConfidenceForCategory(%d, %q, %v) = %q, want %q",
				c.name, c.count, c.category, c.independent, got, c.want)
		}
		if c.independent || c.category != CategoryInvariant {
			if got, want := ConfidenceForCategory(c.count, c.category, c.independent), ConfidenceFor(c.count); got != want {
				t.Errorf("%s: outside the class must equal ConfidenceFor: got %q, want %q", c.name, got, want)
			}
		}
	}
}

// The consensus filter sidecars a finding below HIGH on a 3+ reviewer panel. A
// discounted finding is MEDIUM but corroborated by 2+ reviewers, so it is not an
// uncorroborated singleton and must reach Findings, never the sidecar.
func TestReconcile_PanelWideFindingIsKeptAtMedium(t *testing.T) {
	var sources []Source
	for _, f := range fivePredicateFindings() {
		sources = append(sources, Source{Name: f.Reviewer, Findings: []Finding{f}})
	}
	for _, level := range []string{"", ConsensusStrict, ConsensusLenient, ConsensusOff} {
		res := Reconcile(sources, Options{Consensus: level})
		if len(res.Findings) != 1 {
			t.Fatalf("consensus %q: findings = %d, want 1 (ambiguous: %d)", level, len(res.Findings), len(res.Ambiguous))
		}
		got := res.Findings[0]
		if got.Confidence != ConfMedium || len(got.Reviewers) != 5 {
			t.Fatalf("consensus %q: confidence = %q reviewers = %v, want MEDIUM with all five", level, got.Confidence, got.Reviewers)
		}
		if res.Summary.ConsensusFiltered != 0 {
			t.Fatalf("consensus %q: ConsensusFiltered = %d, want 0", level, res.Summary.ConsensusFiltered)
		}
	}
}

// A singleton is never in the class, so ConfidenceForCategory leaves it at
// MEDIUM and promoteByAuthority may still lift it to HIGH.
func TestPromoteByAuthority_InvariantSingletonStillPromoted(t *testing.T) {
	base := Merge([]Finding{predicateFinding("bruce", CategoryInvariant, "key.go", siblingQuote)})
	m := promoteByAuthority(base, map[string]float64{"bruce": 0.6, "kai": 0.4}, 0.5)
	if m.Confidence != ConfHigh {
		t.Fatalf("authority-promoted invariant singleton: confidence = %q, want %q", m.Confidence, ConfHigh)
	}
}
