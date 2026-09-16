package personas

import (
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
