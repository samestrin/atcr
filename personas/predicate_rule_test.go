package personas

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// CarriesPredicateRule is a CONJUNCTION of two independent anchors, and the
// conjunction is the whole point: the lens phrase tells a reviewer what to look
// for, the filing phrase tells it how to cite the finding so the grounding gate
// does not delete it. A prompt carrying only the lens produces findings on the
// untouched sibling line that are discarded before the report — the exact defect
// the rule was written for, reached by a persona the gap check called a carrier.
//
// Only the callers exercised it (internal/registry.PredicateRuleGaps), and they
// pass whole persona files that carry both phrases or neither, so no test ever
// distinguished AND from OR: replacing the operator left the suite green. This
// table is the guard for that, and every half-carrier row below fails against an
// OR.
//
// Deliberately asserted on the unexported constants rather than on inlined string
// literals: a copy here could drift from the anchors the class guard greps for,
// and then this test would pin a phrase no persona actually ships.
func TestCarriesPredicateRule_RequiresBothAnchors(t *testing.T) {
	const surrounding = "You are a reviewer.\n\n## Focus\n1. Correctness\n"

	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{
			name: "both anchors present",
			text: surrounding + "6. " + predicateRuleAnchor + " — " + predicateFilingAnchor + ".\n",
			want: true,
		},
		{
			name: "lens anchor only — unreportable, so not a carrier",
			text: surrounding + "6. " + predicateRuleAnchor + ".\n",
			want: false,
		},
		{
			name: "filing anchor only — no lens, so nothing tells the reviewer to look",
			text: surrounding + "6. " + predicateFilingAnchor + ".\n",
			want: false,
		},
		{
			name: "neither anchor",
			text: surrounding,
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CarriesPredicateRule(tc.text))
		})
	}
}

// The predicate is deliberately weaker than the built-in class guard: it accepts
// the anchors ANYWHERE in the text, because its callers check community and
// project personas whose section layout atcr does not own. Pin that weakness
// explicitly — a later "tighten it to match the class guard" would start
// reporting every community persona as a gap, which is the Out-of-Scope decision
// this check was built around.
func TestCarriesPredicateRule_AcceptsAnchorsOutsideFocus(t *testing.T) {
	text := "# owasp\n\n## Notes\n" + predicateRuleAnchor + " and " + predicateFilingAnchor + "\n"

	assert.False(t, strings.Contains(text, "## Focus"),
		"precondition: this prompt has no ## Focus section at all")
	assert.True(t, CarriesPredicateRule(text),
		"the tier-agnostic predicate must not require the built-ins' section layout")
}

// predicateFilingAnchor's doc block explains the grounding gate by citing exact
// line ranges in a file this package does not own, and those ranges are the first
// thing an unrelated edit to internal/fanout/grounding.go invalidates. A stale
// citation is worse than none: it sends a reader to a neighbouring arm and lets
// them conclude the narrowing works differently than it does.
//
// Resolve each arm by its source line and require the doc to name the span it
// actually occupies. Asserted against the file on disk rather than a copy, so the
// guard cannot itself go stale.
func TestPredicateFilingAnchorDoc_CitesTheRealGroundingArms(t *testing.T) {
	grounding, err := os.ReadFile("../internal/fanout/grounding.go")
	if err != nil {
		t.Fatalf("read grounding source: %v", err)
	}
	doc, err := os.ReadFile("predicate_rule.go")
	if err != nil {
		t.Fatalf("read predicate_rule.go: %v", err)
	}

	// lineOf returns the 1-based line carrying the only occurrence of want.
	lineOf := func(want string) int {
		t.Helper()
		found := 0
		for i, line := range strings.Split(string(grounding), "\n") {
			if strings.TrimSpace(line) == want {
				if found != 0 {
					t.Fatalf("grounding.go: %q is not unique (lines %d and %d) — the citation cannot be resolved", want, found, i+1)
				}
				found = i + 1
			}
		}
		if found == 0 {
			t.Fatalf("grounding.go: no line %q — the arm the doc describes has moved or been renamed", want)
		}
		return found
	}

	// The PrefetchOnly arm: condition through its return.
	prefetch := lineOf("if fc.PrefetchOnly {")
	// The two permissive arms the doc says PrefetchOnly bypasses: the binary/mode
	// fail-open and the file-level citation, cited as one contiguous span.
	binaryMode := lineOf("if len(fc.Ranges) == 0 && len(fc.ChangedText) == 0 {")
	fileLevel := lineOf("if f.Line <= 0 {")

	prefetchCite := fmt.Sprintf("internal/fanout/grounding.go:%d-%d", prefetch, prefetch+2)
	bypassedCite := fmt.Sprintf("internal/fanout/grounding.go:%d-%d", binaryMode, fileLevel+1)

	assert.Contains(t, string(doc), prefetchCite,
		"the doc must cite the PrefetchOnly arm where it actually sits")
	assert.Contains(t, string(doc), bypassedCite,
		"the doc must cite the two bypassed arms where they actually sit, not a range that excludes them")
}
