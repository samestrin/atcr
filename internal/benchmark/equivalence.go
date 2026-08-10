package benchmark

import (
	"sort"

	"github.com/samestrin/atcr/reconcile"
)

// categoryFamilies is the scorer-side equivalence relation: for each COARSE
// category internal/benchmarkimport can emit as ground truth, the ATCR taxonomy
// members that satisfy it.
//
// It exists because the two sides of a benchmark score speak different
// vocabularies. aacr-bench labels a comment with one of four broad classes, and
// internal/benchmarkimport/suite.go's categoryMap collapses them to exactly
// correctness / security / maintainability / performance. The reviewer, meanwhile,
// is prompted with reconcile's 32-member closed vocabulary, whose disambiguation
// gloss actively steers it toward a FINER word for the same defect. Under exact
// string equality a reviewer that correctly identifies a readability problem and
// writes `style` scores zero against an expected `maintainability` — recall
// measures vocabulary agreement rather than detection.
//
// # Membership is the taxonomy's own steering set, and nothing wider
//
// Each family is exactly the words reconcile/category.go's scoring caveat
// (category.go:83-94) names as the ones its gloss steers a model toward IN PLACE
// OF each coarse word, plus the coarse word itself. That criterion is steering,
// not conceptual proximity: these are the words a reviewer reaches for instead of
// the coarse one when describing the same finding. Repairing exactly that is what
// makes a recall gain attributable to vocabulary agreement rather than to a
// relaxation of what counts as a hit.
//
// The relation runs COARSE-EXPECTED to FINE-RAISED only. It is not symmetric and
// must never become so: a vague `maintainability` finding does not corroborate a
// specific planted `style` defect, and the suite plants no fine categories anyway.
//
// # Why the unassigned members stay out
//
// 13 of the taxonomy's 32 members appear here. The rest are excluded because no
// gloss steers a reviewer from a coarse word to them — they make a DIFFERENT
// claim, and crediting one would credit a detection the reviewer never asserted.
// reconcile/category.go:135-137 states the governing rule in code ("coupling is
// not maintainability; race is not concurrency ... when in doubt the distinction
// is kept") and names `race` as its canonical example.
//
// Widening `correctness` to the functional-defect members (race, concurrency,
// state, invariant, error-handling, contract, api-contract, type, resource-leak,
// leak) is the tempting move, and it is the one to refuse: it takes the share of
// the taxonomy that can satisfy some expected category from 13/32 to 23/32, and
// `correctness` — planted in 14 of the suite's 17 cases — from 2 satisfying words
// to 12. Any defect-flavoured finding would then score on 82% of cases, and the
// leaderboard would rank reviewers by finding volume rather than by detection.
//
// Two exclusions are genuinely borderline and are deliberate:
//
//   - `coupling` — aacr-bench's "Maintainability and Readability" arguably spans
//     it, but category.go:136 names "coupling is not maintainability" as the
//     standing rule. Out.
//   - `docs` — category.go:68 ("documentation or comments made wrong by this
//     change") overlaps :61's "comments that lie". The closest real call in the
//     set. Out.
//
// A future widening must argue against this text, not merely find the table
// convenient to extend.
//
// # Routing values are hard-excluded
//
// `other` (category.go:73) is the escape hatch that makes the vocabulary closed
// rather than lossy, and `out-of-scope` routes a finding rather than classifying
// a defect. Neither is a key and neither may appear in any family: admitting
// `other` would make it a free hit on every case — the largest available way to
// inflate recall without detecting anything.
//
// # Scope
//
// This is a BENCHMARK-side relation. It merges nothing in the product: no
// consumer of Finding.Category sees it, and reconcile's own taxonomy is
// unchanged. It is deliberately NOT reconcile.CategoryMerges(), which answers a
// different question ("do these two words mean the identical thing?") and is epic
// 35.16.6's parse-boundary canonicalization table — it records only
// logic->correctness and omits every security and maintainability steer above.
//
// Keys and members are validated against reconcile.Categories() by
// TestEquivalence_EveryWordIsATaxonomyMember, so removing a category from the
// constant fails CI here rather than silently zeroing a case's recall.
var categoryFamilies = map[string][]string{
	// aacr-bench "Code Defect". `logic` is a member of the vocabulary because
	// personas/community/sonny.md:49's worked example emits it.
	reconcile.CategoryCorrectness: {
		reconcile.CategoryCorrectness,
		reconcile.CategoryLogic,
	},

	// aacr-bench "Security Vulnerability" — broad enough to span an exposed
	// credential and a missing trust-boundary check. Note reconcile deliberately
	// refuses to MERGE `secret` into `security` (category.go:162-166), because the
	// two draw a real triage line; that is a different question from whether both
	// fall under one coarse upstream label, which they plainly do.
	reconcile.CategorySecurity: {
		reconcile.CategorySecurity,
		reconcile.CategorySecret,
		reconcile.CategoryValidation,
		reconcile.CategoryInputValidation,
	},

	// aacr-bench "Maintainability and Readability" — the single upstream label the
	// gloss splits most finely, and the reason this relation exists.
	reconcile.CategoryMaintainability: {
		reconcile.CategoryMaintainability,
		reconcile.CategoryComplexity,
		reconcile.CategoryDuplication,
		reconcile.CategoryNaming,
		reconcile.CategoryBloat,
		reconcile.CategoryStyle,
	},

	// aacr-bench "Performance" — a singleton. The gloss steers nothing else here,
	// so the suite's performance-bearing cases still require the exact word and
	// gain nothing from this relation. That caps the achievable recall lift and is
	// worth stating in any before/after comparison.
	reconcile.CategoryPerformance: {
		reconcile.CategoryPerformance,
	},
}

// FamilyKeys returns the coarse ground-truth categories the equivalence relation
// spans, sorted, as a copy.
//
// It exists SOLELY so internal/benchmarkimport can bind its categoryMap output
// values to this table in a test — the two must name the same four words, and
// nothing enforced that across the package boundary. It deliberately does NOT
// expose family MEMBERSHIP: this table is a benchmark-side relation that merges
// nothing in the product (see the header above), and handing out the full
// map[string][]string would offer a reusable equivalence relation — and a mutable
// map — inviting exactly the product-side merge that constraint forbids.
//
// Keys only, copied, sorted. Do not widen this signature.
func FamilyKeys() []string {
	out := make([]string, 0, len(categoryFamilies))
	for k := range categoryFamilies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// familyOf returns the categories that satisfy an expected category: its family
// when it has one, and otherwise itself.
//
// The identity fallback is what keeps this change additive. An expected category
// with no family — a suite that plants a finer word, or a test using a synthetic
// one — is still scored by exact match, exactly as before.
//
// That claim holds only because the fallback is TOTAL, and it is Manifest.Validate
// that makes it so. The fallback otherwise OVERLAPS the table: an unfamilied `style`
// is satisfied by itself AND, if the same case also expects `maintainability`,
// counted a second time through that family — one raised finding satisfying two
// distinct expected categories, measuring recall 1.0 where exact matching gave 0.5.
// Validate rejects a case whose expected categories overlap that way, so no valid
// suite can reach the double-count. Do not relax that check without replacing this
// paragraph.
func familyOf(cat string) []string {
	if family, ok := categoryFamilies[cat]; ok {
		return family
	}
	return []string{cat}
}

// satisfactions resolves the equivalence relation for ONE case in a single walk
// of the expected categories' families, returning both quantities scoreOne needs:
//
//   - hit — how many expected categories had at least one member of their OWN
//     family raised. This is recall's numerator.
//   - satisfying — the UNION of those families. This is the membership test
//     behind the cost-per-corroborated denominator: every finding whose category
//     satisfied some expected category.
//
// Both scoring quantities resolve through this one walk so a single definition of
// "matched" governs recall and the cost-per-corroborated denominator. Keeping them
// in step is not cosmetic — under exact matching `recall > 0` implies
// `matchedFindings > 0`, and widening only the recall side would drop
// cost_per_corroborated_finding_usd for a perfect-recall reviewer, the encoding
// scoreOne reserves exclusively for a priced reviewer that matched nothing.
//
// The two must nonetheless stay DISTINCT quantities. Recall asks whether cat's own
// family was raised; answering it from the union would credit an expected category
// with a sibling category's hit. They are computed together only because both walk
// the same families — which, split across a separate set-builder and a per-category
// predicate, cost two familyOf resolutions per expected category per case.
// Both sides are compared RAW: expected/raised arrive already normalized from
// normalizeSet, and the table's own keys and members are asserted normalized by
// TestEquivalence_EveryWordIsATaxonomyMember. That test is what lets this function
// skip a per-member normalize call — do not delete it and leave this comment.
func satisfactions(expected, raised map[string]bool) (hit int, satisfying map[string]bool) {
	satisfying = make(map[string]bool, len(expected))
	for cat := range expected {
		catHit := false
		// Read the table directly rather than through familyOf: the identity
		// fallback there returns a fresh one-element slice, which would heap-allocate
		// once per unfamilied expected category per case for no gain. Semantics are
		// identical — this IS familyOf's two branches, inlined.
		family, ok := categoryFamilies[cat]
		if !ok {
			satisfying[cat] = true
			catHit = raised[cat]
		} else {
			for _, member := range family {
				satisfying[member] = true
				if raised[member] {
					catHit = true
				}
			}
		}
		if catHit {
			hit++
		}
	}
	return hit, satisfying
}
