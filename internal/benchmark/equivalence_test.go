package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/reconcile"
)

// A coarse expected category is satisfied by any member of the ATCR family it
// spans. aacr-bench's single upstream label "Maintainability and Readability"
// maps to `maintainability`, but reconcile/category.go's disambiguation gloss
// steers a reviewer toward `style` for the same finding — so `style` must count.
func TestScore_CoarseExpectedSatisfiedByFinerRaised(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"maintainability"}, Raised: []string{"style"}}},
	}})

	require.Len(t, got, 1)
	assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9,
		"a raised `style` must satisfy an expected `maintainability`")
}

// Every steered word in every family satisfies its coarse ground-truth category.
// Table-driven over the whole relation so a dropped row fails loudly rather than
// silently zeroing that word's recall contribution.
func TestScore_EveryFamilyMemberSatisfiesItsCoarseCategory(t *testing.T) {
	for coarse, members := range categoryFamilies {
		for _, member := range members {
			t.Run(coarse+"/"+member, func(t *testing.T) {
				got := Score([]ReviewerScore{{
					Model:   "m",
					Persona: "p",
					Cases:   []CaseScore{{Expected: []string{coarse}, Raised: []string{member}}},
				}})
				require.Len(t, got, 1)
				assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9,
					"%q must satisfy expected %q", member, coarse)
			})
		}
	}
}

// The relation is scorer-side and one-directional: it widens what satisfies a
// coarse GROUND-TRUTH word. It must not make a fine expected category satisfiable
// by its coarse sibling — the suite never plants one, and admitting it would let
// a vague `maintainability` finding claim a specific `style` defect.
func TestScore_EquivalenceDoesNotRunFineToCoarse(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"style"}, Raised: []string{"maintainability"}}},
	}})

	require.Len(t, got, 1)
	assert.InDelta(t, 0.0, got[0].CorroborationRate, 1e-9,
		"equivalence is coarse-expected -> fine-raised only")
}

// The same relation governs the cost-per-corroborated denominator. Under exact
// matching `recall > 0 <=> matchedFindings > 0` is a theorem; widening recall
// alone would break it and drop cost_per_corroborated_finding_usd for a
// perfect-recall reviewer — the encoding score.go reserves exclusively for "a
// priced reviewer that matched nothing".
func TestScore_EquivalenceGovernsCostDenominator(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		CostUSD: 0.12,
		// 3 findings, all in maintainability's family -> denominator 3, not 1.
		Cases: []CaseScore{{
			Expected: []string{"maintainability"},
			Raised:   []string{"style", "duplication", "maintainability"},
		}},
	}})

	require.Len(t, got, 1)
	require.NotNil(t, got[0].CostPerCorroboratedFindingUSD)
	assert.InDelta(t, 0.04, *got[0].CostPerCorroboratedFindingUSD, 1e-9,
		"0.12 / 3 findings whose category satisfies the expected family")
}

// A priced reviewer with perfect recall must never publish as having matched
// nothing. This is the invariant that forced equivalence into both loops.
func TestScore_PerfectRecallNeverOmitsCostPerCorroborated(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		CostUSD: 0.5,
		Cases:   []CaseScore{{Expected: []string{"maintainability"}, Raised: []string{"style"}}},
	}})

	require.Len(t, got, 1)
	require.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9)
	assert.NotNil(t, got[0].CostPerCorroboratedFindingUSD,
		"recall 1.0 with a nil cost-per-corroborated would read as `matched nothing`")
}

// The routing values are not defect classes — they route a finding. `other` is
// the escape hatch that makes the vocabulary closed rather than lossy, so
// admitting it to any family would make it a free hit on every case.
func TestEquivalence_RoutingValuesSatisfyNothing(t *testing.T) {
	routing := []string{reconcile.CategoryOther, reconcile.CategoryOutOfScope}

	for _, r := range routing {
		assert.NotContains(t, categoryFamilies, r, "%q must not be a family key", r)
		for coarse, members := range categoryFamilies {
			assert.NotContains(t, members, r, "%q must not be a member of %q's family", r, coarse)
		}

		for coarse := range categoryFamilies {
			got := Score([]ReviewerScore{{
				Model:   "m",
				Persona: "p",
				Cases:   []CaseScore{{Expected: []string{coarse}, Raised: []string{r}}},
			}})
			require.Len(t, got, 1)
			assert.InDelta(t, 0.0, got[0].CorroborationRate, 1e-9,
				"a raised %q must never satisfy expected %q", r, coarse)
		}
	}
}

// The relation is keyed off the 35.16.4 taxonomy constant: every key and every
// member must be a live member of reconcile.Categories(). A category removed
// from the taxonomy therefore surfaces here rather than silently zeroing the
// recall of every case that plants it.
//
// Scope of that guarantee: it holds for a reconcile release plus a go.mod pin
// bump, which is what CI compiles. An uncommitted in-tree edit to
// reconcile/category.go does NOT reach CI — go.work is gitignored and CI sets
// GOWORK=off, so the root module builds against the pinned reconcile and a
// member deleted from the in-tree slice leaves this test green. See
// equivalence.go's categoryFamilies doc and ci.yml:36-39 for why that is an
// accepted, release-discipline-governed gap rather than a hole to plug here.
func TestEquivalence_EveryWordIsATaxonomyMember(t *testing.T) {
	members := make(map[string]bool)
	for _, c := range reconcile.Categories() {
		members[c] = true
	}

	for coarse, family := range categoryFamilies {
		assert.True(t, members[coarse], "family key %q is not in reconcile.Categories()", coarse)
		assert.Contains(t, family, coarse, "a family must contain its own coarse category")
		// The lookup runs on NORMALIZED input — satisfactions receives its keys from
		// normalizeSet — but the table itself holds raw reconcile constants. A future
		// constant carrying an upper-case letter or stray padding would be a perfectly
		// valid Categories() member that no raised finding could ever match: recall
		// silently zero for that word, with every other assertion here still green.
		// Pin the table side too, not just the caller side.
		assert.Equal(t, normalize(coarse), coarse, "family key %q must already be normalized", coarse)
		for _, m := range family {
			assert.True(t, members[m], "family member %q is not in reconcile.Categories()", m)
			assert.Equal(t, normalize(m), m, "family member %q must already be normalized", m)
		}
	}
}

// The MEMBERSHIP of every family, pinned as an explicit fixture rather than by
// iterating the table against itself.
//
// TestScore_EveryFamilyMemberSatisfiesItsCoarseCategory and
// TestEquivalence_EveryWordIsATaxonomyMember both range over categoryFamilies, so
// they are vacuous in the DELETION direction — verified by mutation: removing
// `validation`, `input-validation`, `naming`, `bloat`, and `complexity` left
// `go test ./internal/benchmark/` fully green. Each deletion silently zeroes the
// recall contribution of a reviewer that writes that word for a planted coarse
// defect: exactly the silent-zeroing failure equivalence.go's header claims is
// guarded against. The Clarifications specified this membership verbatim; encode it.
//
// Adding a member here is not a formality — it must be justified against
// equivalence.go's "Membership is the taxonomy's own steering set" rationale first.
func TestEquivalence_FamilyMembershipIsExactlyAsSpecified(t *testing.T) {
	want := map[string][]string{
		reconcile.CategoryCorrectness: {
			reconcile.CategoryCorrectness,
			reconcile.CategoryLogic,
		},
		reconcile.CategorySecurity: {
			reconcile.CategorySecurity,
			reconcile.CategorySecret,
			reconcile.CategoryValidation,
			reconcile.CategoryInputValidation,
		},
		reconcile.CategoryMaintainability: {
			reconcile.CategoryMaintainability,
			reconcile.CategoryComplexity,
			reconcile.CategoryDuplication,
			reconcile.CategoryNaming,
			reconcile.CategoryBloat,
			reconcile.CategoryStyle,
		},
		reconcile.CategoryPerformance: {
			reconcile.CategoryPerformance,
		},
	}

	require.Len(t, categoryFamilies, len(want), "a family was added or removed wholesale")
	for coarse, members := range want {
		got, ok := categoryFamilies[coarse]
		require.True(t, ok, "family %q is missing from the table", coarse)
		assert.ElementsMatch(t, members, got,
			"family %q's membership drifted from the specified steering set", coarse)
	}
}

// The family table has exactly these four keys.
//
// This is an IN-PACKAGE assertion against a literal — it does NOT bind the keys to
// internal/benchmarkimport's categoryMap, which is where the ground truth actually
// comes from. That cross-package binding lives in
// TestCategoryMap_OutputsMatchScorerFamilyKeys (internal/benchmarkimport/suite_test.go),
// which asserts ElementsMatch against benchmark.FamilyKeys(); it cannot live here,
// because internal/benchmarkimport imports this package and an in-package test
// importing back is an import cycle.
//
// Kept as a fast local guard on the count and spelling. If you change the keys,
// the benchmarkimport test is the one that decides whether the change is correct.
func TestEquivalence_CoversEveryGroundTruthCategory(t *testing.T) {
	want := []string{
		reconcile.CategoryCorrectness,
		reconcile.CategoryMaintainability,
		reconcile.CategoryPerformance,
		reconcile.CategorySecurity,
	}

	got := make([]string, 0, len(categoryFamilies))
	for k := range categoryFamilies {
		got = append(got, k)
	}

	assert.ElementsMatch(t, want, got,
		"family keys drifted from this literal; the authoritative binding to "+
			"internal/benchmarkimport's categoryMap is TestCategoryMap_OutputsMatchScorerFamilyKeys")
}

// Equivalence resolves AFTER normalize, so a coarse expected category spelled
// with different case or padding is still satisfied by a finer raised one.
// normalizeSet feeds both sides, but the family lookup is a separate map hit —
// nothing guarantees it sees normalized input unless this pins it.
func TestScore_EquivalenceIsCaseAndWhitespaceInsensitive(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"  Maintainability "}, Raised: []string{"STYLE"}}},
	}})

	require.Len(t, got, 1)
	assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9,
		"family lookup must run on the normalized category, not the raw one")
}

// No word may belong to two families. Overlap would let one raised finding
// satisfy two distinct planted categories in a case that plants both, inflating
// recall from a single detection — and it would mean the gloss steers the same
// word toward two different coarse labels, which is a defect in the table rather
// than a property to score against.
func TestEquivalence_FamiliesAreDisjoint(t *testing.T) {
	owner := map[string]string{}
	for coarse, members := range categoryFamilies {
		for _, m := range members {
			if prev, dup := owner[m]; dup {
				assert.Failf(t, "overlapping families",
					"%q belongs to both %q and %q", m, prev, coarse)
			}
			owner[m] = coarse
		}
	}
}

// Widening is monotone: every family contains its own coarse category, so a
// reviewer that emits the exact ground-truth word is never scored worse than it
// was under exact matching. This is what makes the change safe to apply to the
// cost denominator, where a shrinking value would flip a published rate.
func TestScore_ExactMatchStillCountsForEveryFamily(t *testing.T) {
	for coarse := range categoryFamilies {
		t.Run(coarse, func(t *testing.T) {
			got := Score([]ReviewerScore{{
				Model:   "m",
				Persona: "p",
				CostUSD: 1.0,
				Cases:   []CaseScore{{Expected: []string{coarse}, Raised: []string{coarse}}},
			}})
			require.Len(t, got, 1)
			assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9)
			require.NotNil(t, got[0].CostPerCorroboratedFindingUSD)
			assert.InDelta(t, 1.0, *got[0].CostPerCorroboratedFindingUSD, 1e-9)
		})
	}
}

// readRepoFile reads a file by its path relative to the repository root, so a test
// can assert against a source this package only cites in prose.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	require.NoError(t, err, "cited source %s must be readable", rel)
	return string(data)
}

// The `correctness` family admits `logic` on ONE piece of evidence: the worked
// example in personas/community/sonny.md emits that word. Nothing bound the two,
// so editing the persona's example would silently narrow what a raised finding can
// satisfy — a change to the recall contract, made from a file that looks like
// documentation. Bind them.
func TestEquivalence_SonnyWorkedExampleJustifiesLogicMembership(t *testing.T) {
	require.Contains(t, categoryFamilies[reconcile.CategoryCorrectness], reconcile.CategoryLogic,
		"the family this test justifies must actually contain `logic`")

	sonny := readRepoFile(t, "personas/community/sonny.md")
	assert.Contains(t, sonny, "|"+reconcile.CategoryLogic+"|",
		"personas/community/sonny.md's worked example is the sole justification for "+
			"`logic` in the correctness family; if it no longer emits that word, re-argue "+
			"the family membership in equivalence.go rather than deleting this test")
}

// The family table is a HAND COPY of prose that lives in reconcile/category.go, and
// equivalence.go's rationale cites that prose by line number. Nothing bound the
// citations to the source, so an edit to the gloss — or any insertion that shifts
// its lines — silently desynchronized the recall contract's stated justification
// from its actual authority. (One citation was already nine lines stale when this
// test was written.) Pin every cited line so either edit fails CI.
//
// SCOPE: this reads the LOCAL reconcile/ tree, which is where a developer edits the
// gloss. reconcile is a separate module pinned at v0.6.0 with no replace directive,
// so the COMPILED dependency is the module cache — byte-identical today. A version
// bump that changed the gloss without touching the local tree would not fail here.
func TestEquivalence_CitedRationaleLinesStillMatchReconcile(t *testing.T) {
	lines := strings.Split(readRepoFile(t, "reconcile/category.go"), "\n")

	for _, c := range []struct {
		line int    // 1-indexed, as cited in equivalence.go's doc comment
		want string // distinctive substring the citation relies on
		why  string
	}{
		{61, "comments that lie", "the `docs` borderline exclusion cites :61 as what `docs` overlaps"},
		{68, "documentation or comments made wrong by this change", "the `docs` exclusion gloss"},
		{73, "escape hat", "`other` is hard-excluded as the closed-vocabulary escape hatch"},
		{83, "Scoring caveat", "start of the gloss the families are derived from verbatim"},
		{94, "regression there as a regression in this vocabulary.", "end of that gloss"},
		{136, `"coupling is not maintainability; race is not concurrency"`, "the standing exclusion rule"},
		{140, "derivation provenance, not a runtime normalizer", "why CategoryMerges is not this table"},
		{162, "`secret` is NOT folded", "start of the no-merge note the security family cites"},
		{166, "security-related on their own terms", "end of that note"},
	} {
		require.Greater(t, len(lines), c.line, "reconcile/category.go is shorter than cited line %d", c.line)
		assert.Contains(t, lines[c.line-1], c.want,
			"equivalence.go cites reconcile/category.go:%d for %s — that line now reads %q. "+
				"Update the citation in equivalence.go (do not relax this test).",
			c.line, c.why, lines[c.line-1])
	}
}

// AC6's hardest constraint is a NEGATIVE one: `correctness` must not be widened to
// the functional-defect members equivalence.go's rationale refuses by name. Nothing
// executable defended it. TestScore_EveryFamilyMemberSatisfiesItsCoarseCategory
// ranges over categoryFamilies itself, so an added row simply becomes another
// passing subtest — verified by mutation: adding all ten refused words to
// categoryFamilies[CategoryCorrectness] left `go test ./internal/benchmark/` fully
// green. That change takes the satisfiable share of the taxonomy from 13/32 to
// 23/32 and `correctness` — planted in 14 of the suite's 17 cases — from 2
// satisfying words to 12, ranking reviewers by finding volume rather than by
// detection. Enumerate the refusal as a literal so widening the table fails HERE,
// forcing the widener to argue against the rationale rather than around it.
func TestEquivalence_RefusedMembersSatisfyNothing(t *testing.T) {
	refused := []string{
		// The functional-defect members `correctness` must never absorb.
		reconcile.CategoryRace, reconcile.CategoryConcurrency, reconcile.CategoryState,
		reconcile.CategoryInvariant, reconcile.CategoryErrorHandling, reconcile.CategoryContract,
		reconcile.CategoryAPIContract, reconcile.CategoryType, reconcile.CategoryResourceLeak,
		reconcile.CategoryLeak,
		// The two borderlines the rationale argues out explicitly (category.go:136's
		// "coupling is not maintainability", and `docs` overlapping :61).
		reconcile.CategoryCoupling, reconcile.CategoryDocs,
	}

	for _, w := range refused {
		assert.NotContains(t, categoryFamilies, w, "%q must not be a family key", w)
		for coarse, members := range categoryFamilies {
			assert.NotContains(t, members, w,
				"%q is refused by equivalence.go's rationale but appears in %q's family — "+
					"widening the table requires arguing against that text first", w, coarse)
		}
	}

	// End-to-end, so the refusal is pinned at the metric and not only in the table:
	// a raised `race` asserts a different defect than a planted `correctness`.
	got := Score([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised:   []string{reconcile.CategoryRace},
		}},
	}})
	require.Len(t, got, 1)
	assert.InDelta(t, 0.0, got[0].CorroborationRate, 1e-9,
		"a raised %q must not satisfy a planted %q", reconcile.CategoryRace, reconcile.CategoryCorrectness)
}

// A fine expected category — one that is a taxonomy MEMBER but never a family KEY —
// is satisfied by itself through familyOf's identity fallback. That is what makes
// the equivalence relation additive: it widens what satisfies a coarse word and
// changes nothing else.
//
// The path was previously covered only incidentally, by synthetic non-taxonomy
// strings in sort-stability fixtures ("a"/"a", "x"/"x" in score_test.go). Nothing
// exercised it with a REAL taxonomy word, which is the case a suite can actually
// plant.
//
// Written against reconcile.CategoryStyle rather than the literal "style" on
// purpose: if a future taxonomy edit dropped `style` or promoted it to a family
// key, a literal would keep passing through the identity fallback on a now-synthetic
// word — silently degrading into a duplicate of the score_test.go fixtures while
// still appearing to guard this.
func TestScore_FineExpectedCategorySatisfiedByItself(t *testing.T) {
	// The premise: `style` is a MEMBER of maintainability's family, never a KEY.
	// If that ever inverts, this test is measuring something else.
	require.NotContains(t, categoryFamilies, reconcile.CategoryStyle,
		"%q must not be a family key, or the identity fallback is not what is under test",
		reconcile.CategoryStyle)
	require.Contains(t, categoryFamilies[reconcile.CategoryMaintainability], reconcile.CategoryStyle,
		"%q must be a member of maintainability's family", reconcile.CategoryStyle)

	got := Score([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryStyle},
			Raised:   []string{reconcile.CategoryStyle},
		}},
	}})

	require.Len(t, got, 1)
	assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9,
		"an expected %q with no family of its own is scored by exact match", reconcile.CategoryStyle)
}
