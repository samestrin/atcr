package scorecard

// InOpportunitySet reports whether a case is an opportunity for persona —
// whether that lens's remit topic was in play at all.
//
// raisedCategories is the union of the categories ANY reviewer raised on that
// case, which the caller assembles from the sibling Record.CategoriesRaised
// values sharing a RunID. Membership is deliberately independent of whether
// persona itself appears among the case's reviewers or raised anything: that is
// the entire point. A security lens that stayed silent while another reviewer
// raised `security` WAS presented with a security question and chose not to
// answer, which is a judgement worth scoring. The same lens on a pure style
// cleanup was never asked, and scoring it there is what a raw frequency count
// gets backwards.
//
// Three cases all answer false, and they are different situations that happen to
// share an answer:
//
//   - The persona is mapped but none of its remit categories were raised. The
//     lens was correctly silent on an out-of-remit case; it must not enter that
//     case into its denominator.
//   - No category was raised at all (a fully clean case). Out-of-remit for
//     everyone by construction. NOTE this is the MEASURED-empty case only — an
//     unmeasured pre-schema-2 record presents identically here, which is why
//     opportunitySetRuns never hands one to this predicate.
//   - The persona is unmapped. Routed through RemitCategories' ok == false
//     branch, so a future caller can distinguish it, but never "in scope for
//     everything".
//
// An unrecognised raised value (corrupt store, pre-vocabulary record) is just a
// string that matches no remit. It is neither an error nor a wildcard.
//
// The input slice is read only — never sorted, deduped, or rewritten in place.
// One case's union is shared across every persona asked about that case, so a
// mutation here would corrupt every later persona's answer for the same case.
func InOpportunitySet(persona string, raisedCategories []string) bool {
	remit, ok := RemitCategories(persona)
	if !ok {
		return false
	}
	// Both sides are bounded by the 32-member vocabulary, so the nested scan is
	// cheaper than allocating a set per call on a path that runs per (persona,
	// case) pair across the whole store.
	for _, want := range remit {
		for _, got := range raisedCategories {
			if got == want {
				return true
			}
		}
	}
	return false
}
