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
// gate keeps a finding only via one of six arms: the CATEGORY out-of-scope
// exemption (annotated and never promoted), a binary/mode-only fail-open, a
// file-level citation on a changed file, a line within ±groundingTolerance of a
// changed range, an EVIDENCE match against changed text, or — for a file present
// ONLY because context-aware pre-fetching retrieved a snippet of it — a line
// inside an exact retrieved span (the PrefetchOnly arm,
// internal/fanout/grounding.go:100-102). That sixth arm is strictly NARROWER than
// the others rather than an extra allowance: it bypasses the file-level and
// binary/mode fail-open arms, both of which are justified by the patch having
// demonstrably touched the file (internal/fanout/grounding.go:103-107).
//
// A finding whose only citation is the untouched sibling line clears none of
// them, so a reviewer that correctly spots the asymmetry and cites the sibling
// alone has its finding deleted before it reaches the report — on exactly the
// defect the rule was written for. Anchoring the finding on the edited branch
// and quoting the sibling as EVIDENCE keeps it inside the gate and promotable.
// The anchor is not where the defect lives, though: the sibling's real location
// travels beside it as the sibling marker (predicateSiblingAnchor, below).
//
// Unlike the lens above, this is a mechanical citation instruction rather than a
// judgement, so sharing its wording across the panel costs no independence.
const predicateFilingAnchor = "file it on the edited branch's changed line"

// predicateSiblingAnchor is the third phrase every BUILT-IN persona carries
// verbatim on the rule bullet: it tells the reviewer to declare the unchanged
// sibling arm's real location as the cross-branch routing marker
// (stream.SiblingToken, read by stream.ParseSiblingLocation; Epic 35.16.15).
//
// The filing anchor above puts the finding on a line the gate can ground, but
// that line is the half of the predicate that was fixed, not the half that is
// still wrong. The marker carries the defect site: reconcile validates it and
// emits it as sibling_location, verify reads the sibling arm, and the
// repo-state benchmark matches the finding at the sibling instead of its
// FILE:LINE. The grounding gate never consults it (isGrounded grounds a marked
// finding on its edited-line FILE:LINE exactly like an unmarked one), so the
// marker routes the finding without loosening the gate. Prose that justified the
// edited-line anchor only by the gate ("so the grounding rule below does not
// discard it") coached a citation that passes while pointing away from the
// defect; the class guard rejects that framing (forbiddenRelocationFramings).
//
// Deliberately NOT part of CarriesPredicateRule. That predicate feeds doctor's
// PredicateRuleGaps and the stamp-less staleness verdict for installed,
// community and project personas; requiring this phrase there would turn every
// persona written to the pre-marker wording into a gap overnight, though the rule
// it carries still reaches the report. Only the embedded built-ins, whose prose
// atcr owns, are held to it.
const predicateSiblingAnchor = "in EVIDENCE as `sibling: <file>:<line>`"

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
