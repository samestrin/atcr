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
// member must be a live member of reconcile.Categories(). Removing a category
// from the constant therefore fails CI here rather than silently zeroing the
// recall of every case that plants it.
func TestEquivalence_EveryWordIsATaxonomyMember(t *testing.T) {
	members := make(map[string]bool)
	for _, c := range reconcile.Categories() {
		members[c] = true
	}

	for coarse, family := range categoryFamilies {
		assert.True(t, members[coarse], "family key %q is not in reconcile.Categories()", coarse)
		assert.Contains(t, family, coarse, "a family must contain its own coarse category")
		for _, m := range family {
			assert.True(t, members[m], "family member %q is not in reconcile.Categories()", m)
		}
	}
}

// The families must cover exactly the ground truth internal/benchmarkimport can
// emit. A fifth key would be dead weight; a missing one is a coarse category
// scored by exact match while its siblings are scored by family.
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
		"family keys must be exactly the values internal/benchmarkimport's categoryMap emits")
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
