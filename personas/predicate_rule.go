package personas

import "strings"

// predicateRuleAnchor is the LENS phrase — one of the TWO phrases every built-in
// persona carries VERBATIM (the filing anchor is defined below).
//
// The rule around it is deliberately re-voiced per persona: nine agents handed a
// byte-identical paragraph converge, and correlated findings inflate reconcile's
// CONFIDENCE = HIGH (2+ distinct reviewers) without adding independent evidence.
// A single short invariant anchor is what makes the class checkable anyway, so
// the prose varies and this phrase does not.
const predicateRuleAnchor = "enumerate every branch of that predicate and every field it is contracted to cover"

// predicateFilingAnchor is the second phrase every built-in persona carries
// verbatim: the rule's filing mechanic, without which the rule is unreportable.
//
// A predicate's sibling branch is normally UNCHANGED code, and the grounding
// gate keeps a finding only via one of five arms: the CATEGORY out-of-scope
// exemption (annotated and never promoted), a binary/mode-only fail-open, a
// file-level citation on a changed file, a line within ±groundingTolerance of a
// changed range, or an EVIDENCE match against changed text
// (internal/fanout/grounding.go:66-97). A finding whose only citation is the
// untouched sibling line clears none of them, so a reviewer that correctly
// spots the asymmetry and cites the sibling alone has its finding deleted
// before it reaches the report — on exactly the defect the rule was written
// for. Anchoring the finding on the edited branch and quoting the sibling as
// EVIDENCE keeps it inside the gate and promotable.
//
// Unlike the lens above, this is a mechanical citation instruction rather than a
// judgement, so sharing its wording across the panel costs no independence.
const predicateFilingAnchor = "file it on the edited branch's changed line"

// CarriesPredicateRule reports whether a resolved persona prompt carries the
// panel-wide predicate-exhaustiveness rule — both anchor phrases verbatim,
// anywhere in the text. The class guard (predicate_exhaustiveness_test.go) holds
// the EMBEDDED BUILT-INS to something strictly stronger: both phrases on ONE
// numbered bullet under ## Focus. This predicate is deliberately the weaker of
// the two, because its callers check other persona tiers (community, project)
// whose section layout atcr does not own. What it buys is that no caller
// re-states the phrases, so the strings cannot drift from the constants above.
func CarriesPredicateRule(text string) bool {
	return strings.Contains(text, predicateRuleAnchor) && strings.Contains(text, predicateFilingAnchor)
}
