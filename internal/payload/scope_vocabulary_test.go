package payload

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/reconcile"
)

// Epic 35.16.4: the closed CATEGORY vocabulary reaches every reviewer through
// {{.ScopeRule}}, the one template field present in all 29 shipped prompts (10
// in personas/, 14 in personas/community/, 5 in ~/.config/atcr/personas/). No
// prompt file is edited, so no prompt file can fall out of sync — but that
// guarantee only holds while the injected text is generated from
// reconcile.Categories() rather than duplicated as a literal. These tests are
// what make that structural.

// allModes is every payload mode a persona prompt can be rendered under, plus an
// unknown mode to cover ScopeRule's conservative default arm. A reviewer in any
// of them must be offered the same vocabulary.
var allModes = []PayloadMode{ModeDiff, ModeBlocks, ModeFiles, PayloadMode("unknown-mode")}

// TestScopeRule_CarriesEveryCategory is the AC4 guard: deleting a category from
// reconcile.Categories() without regenerating the prompt text, or rendering a
// mode whose rule was never extended, fails here.
func TestScopeRule_CarriesEveryCategory(t *testing.T) {
	for _, mode := range allModes {
		t.Run(string(mode), func(t *testing.T) {
			rule := ScopeRule(mode)
			for _, cat := range reconcile.Categories() {
				if !strings.Contains(rule, cat) {
					t.Errorf("mode %q scope rule omits category %q — reviewers in this mode are never offered it", mode, cat)
				}
			}
		})
	}
}

// TestScopeRuleForPayload_CarriesEveryCategory covers the path agents actually
// receive (internal/fanout/review.go:2056), including the per-file escalation
// override that swaps in the files-mode rule mid-payload.
func TestScopeRuleForPayload_CarriesEveryCategory(t *testing.T) {
	payloads := map[string]string{
		"plain diff":     "@@ -1,3 +1,4 @@\n+added line\n",
		"escalated file": filesHeaderPrefix + "internal/x/y.go\n\npackage x\n",
	}
	for name, text := range payloads {
		for _, mode := range allModes {
			t.Run(name+"/"+string(mode), func(t *testing.T) {
				rule := ScopeRuleForPayload(mode, text)
				for _, cat := range reconcile.Categories() {
					if !strings.Contains(rule, cat) {
						t.Errorf("payload %q in mode %q omits category %q", name, mode, cat)
					}
				}
			})
		}
	}
}

// TestScopeRule_EnumerationIsGeneratedNotDuplicated is the AC3 guard. A literal
// enumeration in scope.go would satisfy the membership tests above while
// silently drifting from the constant the moment a category is added. Adding a
// member to reconcile.Categories() must change the rendered text; if it does
// not, the text is a hardcoded copy.
func TestScopeRule_EnumerationIsGeneratedNotDuplicated(t *testing.T) {
	// Reconstruct the expected enumeration from the EXPORTED vocabulary, not from
	// categoryEnumeration(). Comparing the rule against categoryEnumeration()
	// would compare the generator with itself: replacing its body with a
	// hardcoded literal identical to today's list would still pass, which is
	// precisely the drift this test claims to catch.
	fromVocabulary := strings.Join(reconcile.Categories(), ", ")
	for _, mode := range allModes {
		rule := ScopeRule(mode)

		if !strings.Contains(rule, fromVocabulary) {
			t.Errorf("mode %q does not embed reconcile.Categories() joined verbatim — the rule text appears to be a hand-maintained copy", mode)
		}
	}

	// The generator, not a literal, is the source: every member appears in
	// vocabulary order, so a reordering or addition in reconcile/category.go
	// propagates without touching scope.go.
	cats := reconcile.Categories()
	enumeration := categoryEnumeration()
	pos := -1
	for _, cat := range cats {
		at := strings.Index(enumeration, cat)
		if at < 0 {
			t.Fatalf("generated enumeration omits %q", cat)
		}
		if at <= pos {
			t.Errorf("category %q appears out of vocabulary order in the generated enumeration", cat)
		}
		pos = at
	}
}

// TestScopeRule_PreservesExistingInstructions guards against the injection
// swallowing what the rules already said. The out-of-scope routing instruction
// and the changed-regions discipline predate this epic and are load-bearing:
// grounding discards an out-of-range finding that is not tagged out-of-scope.
func TestScopeRule_PreservesExistingInstructions(t *testing.T) {
	changed := ScopeRule(ModeDiff)
	if !strings.Contains(changed, "Stay on the diff") || !strings.Contains(changed, "changed regions") {
		t.Error("changed-only rule lost its scope discipline text")
	}
	// Assert on the routing SENTENCE, not the bare token: "out-of-scope" is
	// itself a member of the injected enumeration, so a token check would pass
	// even if the whole routing instruction were deleted from the base rule.
	for _, mode := range allModes {
		rule := ScopeRule(mode)
		if !strings.Contains(rule, "annotates rather than") {
			t.Errorf("mode %q lost the out-of-scope routing instruction (the sentence, not just the word)", mode)
		}
		if !strings.Contains(rule, "routing value rather than a defect class") {
			t.Errorf("mode %q does not mark out-of-scope as a routing value, so it reads as an ordinary defect class", mode)
		}
	}
}

// TestScopeRule_DisambiguatesConfusablePairs guards the gloss. The vocabulary
// deliberately keeps near-pairs distinct (race/concurrency, contract/
// api-contract, resource-leak/leak, input-validation/validation, secret/
// security, correctness/logic); a reviewer handed both words with no rule for
// choosing between them just relocates the drift this epic closes.
func TestScopeRule_DisambiguatesConfusablePairs(t *testing.T) {
	pairs := [][2]string{
		{reconcile.CategoryRace, reconcile.CategoryConcurrency},
		{reconcile.CategoryContract, reconcile.CategoryAPIContract},
		{reconcile.CategoryResourceLeak, reconcile.CategoryLeak},
		{reconcile.CategoryInputValidation, reconcile.CategoryValidation},
		{reconcile.CategorySecret, reconcile.CategorySecurity},
		{reconcile.CategoryCorrectness, reconcile.CategoryLogic},
	}
	for _, mode := range allModes {
		rule := ScopeRule(mode)
		// Locate the gloss ONCE per mode, and check the result. An unchecked
		// strings.Index would return -1 the moment the sentence is reworded and
		// slice-panic, which kills the whole internal/payload test binary — every
		// other guard in this file would stop reporting and a one-word prompt edit
		// would present as a stack trace instead of a named failure. The lookup is
		// also loop-invariant, so hoisting it out of the pairs loop is free.
		at := strings.Index(rule, "When two members are close")
		if at < 0 {
			t.Errorf("mode %q lost the disambiguation gloss entirely", mode)
			continue
		}
		gloss := rule[at:]
		for _, pair := range pairs {
			// Both words must appear inside the gloss clause, not merely
			// somewhere in the enumeration.
			if !strings.Contains(gloss, "`"+pair[0]+"`") || !strings.Contains(gloss, "`"+pair[1]+"`") {
				t.Errorf("mode %q does not disambiguate %q vs %q", mode, pair[0], pair[1])
			}
		}
	}
}

// TestCategoryEnumeration_WellFormed checks the rendered fragment itself: it is
// concatenated into a prompt, so a stray pipe or newline would corrupt either
// the persona template or the pipe-delimited finding format the reviewer is
// asked to emit.
func TestCategoryEnumeration_WellFormed(t *testing.T) {
	enumeration := categoryEnumeration()
	if enumeration == "" {
		t.Fatal("category enumeration rendered empty")
	}
	if strings.ContainsAny(enumeration, "|\n") {
		t.Errorf("enumeration contains a pipe or newline: %q", enumeration)
	}
	if strings.Contains(enumeration, "{{") || strings.Contains(enumeration, "}}") {
		t.Errorf("enumeration contains a template action: %q", enumeration)
	}
}
