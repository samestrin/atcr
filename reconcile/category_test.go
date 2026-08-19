package reconcile

import (
	"strings"
	"testing"
	"unicode"
)

// The three derivation sources for the closed CATEGORY vocabulary (epic
// 35.16.4 T1). They are restated here as literal data, independent of
// category.go, so the taxonomy cannot silently stop covering a source: these
// lists come from the dry-run measurement and from files outside this module
// (personas/community_test.go, personas/_base.md), which this module cannot
// import.

// observedEmittedCount is how many DISTINCT category words the 35.16.2 AC3
// dry-run emitted, as measured and reported at
// .planning/.../35.16.2_.../claude/2026-08-07_code-review.md:118 ("72.3% of
// findings (154 of 213) use a category outside ATCR's vocabulary — 34 distinct
// categories emitted").
//
// observedRecordedCount is how many of those 34 were ever written down. The
// write-ups record a frequency table and an out-of-vocabulary tail, not the full
// set, so 9 emitted words exist only as a count and cannot be recovered from the
// artifacts. Keep this equal to len(observedCategories) — the test above enforces it.
const (
	observedEmittedCount  = 34
	observedRecordedCount = 25
)

// observedCategories is every category word the 35.16.2 AC3 dry-run emitted THAT
// EITHER WRITE-UP RECORDED (kimi-k3 + qwen3.8-max over the bundled standard-v1
// suite) — a faithful union of the epic body's frequency table and the
// out-of-vocabulary tail, but not the full emitted set: see
// observedEmittedCount. The 9 unrecorded words are consequently unguarded, so a
// clean run of TestCategories_ObservedWordsAreAccountedFor proves the claim for
// the recorded words only. Recovering one from a future run artifact means
// appending it here and raising observedRecordedCount together.
var observedCategories = []string{
	"security", "correctness", "state", "contract", "failure", "input",
	"bug", "resource", "resources", "duplication", "coupling", "clarity",
	"concurrency", "structure", "consistency", "cleanliness", "stability",
	"extensibility", "race", "style", "performance", "maintainability",
	"testing", "out-of-scope", "naming",
}

// rosterCategories is the 14 category words bound in code by
// personas/community_test.go:117-132, each required by
// TestCommunityPersonas_FixtureAndPromptCategory to appear in its persona's own
// prompt template. A word here that is not accounted for leaves that persona
// authored to find something the enumeration never offers.
var rosterCategories = []string{
	"coupling", "logic", "contract", "validation", "race", "leak",
	"complexity", "type", "dependency", "observability", "secret",
	"duplication", "invariant", "bloat",
}

// baseCategories is the six-word list at personas/_base.md:44 — the only
// enumeration that existed anywhere before this epic, and a resolution fallback
// no shipped persona reaches.
var baseCategories = []string{
	"security", "correctness", "performance", "testing", "style", "docs",
}

// categorySet indexes Categories() for membership assertions.
func categorySet() map[string]bool {
	set := make(map[string]bool, len(Categories()))
	for _, c := range Categories() {
		set[c] = true
	}
	return set
}

// TestCategories_MandatoryMembers locks the members epic 35.16.4 T1 names as
// mandatory: the two control values, performance, the dimensions the dry-run
// proved reviewers need, and maintainability as distinct from style. These are
// the members whose absence caused the measured recall failure, so they are
// asserted by name rather than by count.
func TestCategories_MandatoryMembers(t *testing.T) {
	set := categorySet()
	for _, want := range []string{
		CategoryOutOfScope, CategoryOther,
		CategoryPerformance, CategoryConcurrency, CategoryAPIContract,
		CategoryErrorHandling, CategoryMaintainability, CategoryStyle,
	} {
		if !set[want] {
			t.Errorf("mandatory category %q is not a member of the closed vocabulary", want)
		}
	}
	if CategoryMaintainability == CategoryStyle {
		t.Error("maintainability and style must stay distinct members")
	}
}

// TestCategories_RosterWordsAreAccountedFor enforces AC5: every one of the 14
// CI-bound community roster words is a member, or is recorded in categoryMerges
// as folding into a member. A roster word that is neither leaves its persona
// emitting a word its own injected enumeration does not offer.
func TestCategories_RosterWordsAreAccountedFor(t *testing.T) {
	assertAccountedFor(t, rosterCategories, "community roster (personas/community_test.go:117-132)")
}

// TestCategories_ObservedWordsAreAccountedFor enforces T1's success criterion
// for the dry-run vocabulary, over the words that were recorded: no word a
// reviewer actually emitted AND that either write-up wrote down may fall through
// to nothing. See observedCategories for why that is narrower than "every word
// the run emitted".
func TestCategories_ObservedWordsAreAccountedFor(t *testing.T) {
	assertAccountedFor(t, observedCategories, "35.16.2 dry-run")
}

// TestObservedCategories_MatchesItsDocumentedCount ties the slice to the number
// its comment claims, so the two cannot drift apart again. The list previously
// declared itself "every category word the dry-run actually emitted" while
// holding 25 of the 34 distinct words the run produced — the test above then
// proved 74% of what it claimed. Adding a recovered word means raising
// observedRecordedCount in the same edit, which is the point.
func TestObservedCategories_MatchesItsDocumentedCount(t *testing.T) {
	if len(observedCategories) != observedRecordedCount {
		t.Errorf("observedCategories holds %d words but its comment documents %d — update both together",
			len(observedCategories), observedRecordedCount)
	}
	if observedRecordedCount > observedEmittedCount {
		t.Errorf("observedRecordedCount (%d) exceeds the %d distinct words the dry-run emitted",
			observedRecordedCount, observedEmittedCount)
	}
}

// TestCategories_BaseWordsAreAccountedFor enforces the same for the six words
// personas/_base.md:44 offers, so closing the vocabulary never removes one that
// was already on offer.
func TestCategories_BaseWordsAreAccountedFor(t *testing.T) {
	assertAccountedFor(t, baseCategories, "personas/_base.md:44")
}

// assertAccountedFor fails for any word that is neither a member nor a recorded
// merge into a member. A merge whose target is not itself a member is also a
// failure: it would route findings to a word no prompt offers.
func assertAccountedFor(t *testing.T, words []string, source string) {
	t.Helper()
	set := categorySet()
	for _, w := range words {
		if set[w] {
			continue
		}
		target, merged := categoryMerges[w]
		if !merged {
			t.Errorf("%s word %q is neither a member nor a recorded merge — it would have no home in the closed vocabulary", source, w)
			continue
		}
		if !set[target] {
			t.Errorf("%s word %q merges into %q, which is not itself a member", source, w, target)
		}
	}
}

// TestCategories_WellFormed guards the shape every member must have: the
// enumeration is rendered verbatim into reviewer prompts and the emitted word
// travels the pipe-delimited wire format, so whitespace, pipes, uppercase, and
// duplicates are all defects.
func TestCategories_WellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Categories() {
		// Independent ifs, not a switch: a switch short-circuits at the first
		// matching arm, so a member that is both non-lowercase AND duplicated
		// would report only the casing failure and never reach the duplicate
		// check. Every violation on one member should surface in a single run.
		if c == "" {
			t.Error("empty category in the closed vocabulary")
		}
		if c != strings.ToLower(c) {
			t.Errorf("category %q is not lowercase — persona prompts specify a lowercase word", c)
		}
		// Every whitespace class, not just space and tab. Newline is the one that
		// actually breaks the wire format: findings are line-delimited, so an
		// embedded newline splits one finding into two lines and the second fails
		// the severity-prefix check. unicode.IsSpace covers CR, LF, vertical tab,
		// form feed and NBSP (U+00A0), so no class needs listing separately;
		// TrimSpace additionally rejects leading and trailing whitespace.
		if strings.TrimSpace(c) != c || strings.ContainsFunc(c, unicode.IsSpace) {
			t.Errorf("category %q contains whitespace — an embedded newline would split one finding into two wire-format lines", c)
		}
		if strings.Contains(c, "|") {
			t.Errorf("category %q contains a pipe — it would corrupt the pipe-delimited wire format", c)
		}
		if seen[c] {
			t.Errorf("category %q appears twice", c)
		}
		seen[c] = true
	}
	if len(Categories()) < len(rosterCategories) {
		t.Errorf("closed vocabulary has %d members, fewer than the %d CI-bound roster words alone", len(Categories()), len(rosterCategories))
	}
}

// TestCategories_LockedSet pins the vocabulary exactly, in order.
//
// The membership tests above are derived from the same sources the constant is,
// so they cannot catch the removal of a member no source names — `configuration`
// (from personas/mira.md:11's Focus list) had exactly that hole. Epic 35.16.4
// AC4 requires that removing ANY category fails a test, so the expected set is
// spelled out here. This literal lives in a test on purpose: it is the tripwire
// that makes a vocabulary change deliberate, and it cannot silently drift
// because any divergence from Categories() fails immediately.
func TestCategories_LockedSet(t *testing.T) {
	want := []string{
		"correctness", "logic", "security", "secret", "performance",
		"concurrency", "race", "error-handling", "state", "invariant", "type",
		"api-contract", "contract", "validation", "input-validation",
		"resource-leak", "leak", "dependency", "configuration",
		"coupling", "complexity", "bloat", "duplication", "extensibility",
		"maintainability", "naming", "style",
		"observability", "testing", "docs",
		"out-of-scope", "other",
	}
	got := Categories()
	if len(got) != len(want) {
		t.Fatalf("vocabulary has %d members, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member %d is %q, want %q — a vocabulary change must be deliberate", i, got[i], want[i])
		}
	}
}

// TestCategories_EquivalentMembersRecordTheirFold covers the one case where a
// word is BOTH a member and a recorded merge.
//
// The rendered prompt tells every reviewer that `logic` is accepted as the
// equivalent of `correctness`. Two words the prompt itself calls equivalent, with
// no recorded fold, split identical findings across every category-keyed
// consumer — ModalCategory clustering, SARIF rule ids, the 35.16.5 scorer — which
// re-creates the unscoreability this epic exists to close. The member stays
// (personas/community/sonny.md:49's worked example emits it, and a prompt whose
// example contradicts its own vocabulary is the defect this epic removes), so the
// fold is recorded for the ingestion boundary instead.
//
// `secret` is deliberately NOT here: the gloss draws a real triage line between an
// exposed credential and every other vulnerability, so those two are distinct, not
// equivalent. Only a pair the prompt declares equivalent belongs in this test.
func TestCategories_EquivalentMembersRecordTheirFold(t *testing.T) {
	target, recorded := categoryMerges[CategoryLogic]
	if !recorded {
		t.Fatalf("the prompt calls %q equivalent to %q but categoryMerges records no fold, so every category-keyed consumer will split them",
			CategoryLogic, CategoryCorrectness)
	}
	if target != CategoryCorrectness {
		t.Errorf("categoryMerges[%q] = %q, want %q", CategoryLogic, target, CategoryCorrectness)
	}
	if !categorySet()[CategoryLogic] {
		t.Errorf("%q must stay a member — sonny.md:49's worked example emits it, and the prompt may not contradict its own vocabulary", CategoryLogic)
	}
}

// TestCategories_MergeTargetsAreNotThemselvesMerged rejects a chained merge
// (a -> b -> c). Every recorded merge must land directly on a member, so a
// reader of categoryMerges never has to follow more than one hop.
func TestCategories_MergeTargetsAreNotThemselvesMerged(t *testing.T) {
	for word, target := range categoryMerges {
		if _, chained := categoryMerges[target]; chained {
			t.Errorf("merge %q -> %q chains through another merge", word, target)
		}
		if word == target {
			t.Errorf("category %q merges into itself", word)
		}
	}
}

// TestCategoryMerges_IsReachableByItsDeclaredConsumer covers the structural half
// of the merge map's purpose. The map documents epic 51.0's parse-boundary
// canonicalizer as its intended consumer, but that canonicalizer lives in module
// github.com/samestrin/atcr, which cannot reach an unexported identifier in the
// separately-versioned github.com/samestrin/atcr/reconcile. Unexported, the
// declared consumer would have to duplicate the map — the exact drift this epic
// exists to eliminate.
func TestCategoryMerges_IsReachableByItsDeclaredConsumer(t *testing.T) {
	got := CategoryMerges()
	if len(got) != len(categoryMerges) {
		t.Fatalf("CategoryMerges() returned %d entries, want %d", len(got), len(categoryMerges))
	}
	for word, target := range categoryMerges {
		if got[word] != target {
			t.Errorf("CategoryMerges()[%q] = %q, want %q", word, got[word], target)
		}
	}
}

// TestCategoryMerges_ReturnsCopy mirrors Categories()' contract: this module is
// published and embedded, so a shared map would let one consumer corrupt the
// canonicalization every other consumer applies in the same process.
func TestCategoryMerges_ReturnsCopy(t *testing.T) {
	first := CategoryMerges()
	if len(first) == 0 {
		t.Fatal("CategoryMerges() returned an empty map")
	}
	first["bug"] = "mutated"
	delete(first, CategoryLogic)

	second := CategoryMerges()
	if second["bug"] == "mutated" {
		t.Error("CategoryMerges() shares its backing map: a caller's write is visible to the next caller")
	}
	if _, ok := second[CategoryLogic]; !ok {
		t.Error("CategoryMerges() shares its backing map: a caller's delete is visible to the next caller")
	}
}

// TestCategories_ReturnsCopy verifies callers cannot mutate the vocabulary
// through the returned slice. This module is published and embedded by external
// tools; a shared backing array would let any consumer corrupt every prompt
// rendered afterwards in the same process.
func TestCategories_ReturnsCopy(t *testing.T) {
	first := Categories()
	if len(first) == 0 {
		t.Fatal("Categories() returned an empty vocabulary")
	}
	original := first[0]
	first[0] = "mutated"

	if got := Categories()[0]; got != original {
		t.Errorf("Categories() shares its backing array: got %q after caller mutation, want %q", got, original)
	}
}
