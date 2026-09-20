package scorecard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInOpportunitySet_MatchingRemitCategoryIsInScope is AC 03-04 Happy Path
// Scenario 1. Membership is independent of whether the named persona itself
// appears among the case's reviewers: it answers only "was this persona's remit
// topic in play".
func TestInOpportunitySet_MatchingRemitCategoryIsInScope(t *testing.T) {
	assert.True(t, InOpportunitySet("sasha", []string{"security", "correctness"}),
		"sasha's remit (security) is in play on this case")
	assert.True(t, InOpportunitySet("dax", []string{"testing", "style"}),
		"dax's remit (testing) is in play on this case")
}

// TestInOpportunitySet_NonMatchingRemitIsOutOfScope is AC 03-04 Happy Path
// Scenario 2 — the epic's headline property: a lens correctly silent on an
// out-of-remit case must not enter that case into its denominator.
func TestInOpportunitySet_NonMatchingRemitIsOutOfScope(t *testing.T) {
	assert.False(t, InOpportunitySet("sasha", []string{"performance", "style"}),
		"no security-flavoured category was raised, so this case is not sasha's opportunity")
	assert.False(t, InOpportunitySet("dax", []string{"performance", "naming"}),
		"no test-flavoured category was raised, so this case is not dax's opportunity")
}

// TestInOpportunitySet_GeneralistIsInScopeOnMoreCases is AC 03-04 Happy Path
// Scenario 3: the breadth falls out of the remit mapping, with no special case
// for the generalist inside the predicate itself.
func TestInOpportunitySet_GeneralistIsInScopeOnMoreCases(t *testing.T) {
	caseA := []string{"security", "correctness"}
	caseB := []string{"performance", "style"}

	assert.True(t, InOpportunitySet("bruce", caseA), "bruce's remit covers correctness")
	assert.True(t, InOpportunitySet("sasha", caseA), "sasha's remit covers security")
	assert.False(t, InOpportunitySet("sasha", caseB), "sasha has no remit here")
}

// TestInOpportunitySet_ZeroRaisedCategoriesIsOutOfScopeForEveryone is AC 03-04
// Edge Case 1.
//
// READ THE COMMENT BEFORE CHANGING THIS TEST. The empty slice here is a
// MEASURED-empty case — a genuinely clean run where the panel raised nothing.
// It is NOT the unmeasured pre-schema-2 record. Those two must never be
// conflated: the predicate is correct to answer false for a measured-empty set,
// and opportunitySetRuns is the layer responsible for never handing it an
// unmeasured record at all (see TestOpportunitySetRuns_UnmeasuredRecordsAreNot
// JudgedAsOutOfRemit).
func TestInOpportunitySet_ZeroRaisedCategoriesIsOutOfScopeForEveryone(t *testing.T) {
	for _, p := range append(append([]string{}, inRepoPersonas...), "vera", "nosuchpersona") {
		assert.False(t, InOpportunitySet(p, nil), "nil raised set must be out-of-scope for %q", p)
		assert.False(t, InOpportunitySet(p, []string{}), "empty raised set must be out-of-scope for %q", p)
	}
}

// TestInOpportunitySet_UnmappedPersonaIsFalseAndDoesNotPanic is AC 03-04 Edge
// Case 2 and Error Scenario 1. An unmapped persona is never "in scope for
// everything".
func TestInOpportunitySet_UnmappedPersonaIsFalseAndDoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		assert.False(t, InOpportunitySet("vera", []string{"api-contract", "contract"}),
			"vera is unmapped under Option A, so the predicate reports false")
		assert.False(t, InOpportunitySet("nosuchpersona", []string{"correctness"}))
		assert.False(t, InOpportunitySet("", []string{"correctness"}))
	})
}

// TestInOpportunitySet_UnknownRaisedCategoryNeverMatches is AC 03-04 Edge Case 3:
// a corrupted or pre-vocabulary value is a plain string that matches no remit —
// neither an error nor a wildcard.
func TestInOpportunitySet_UnknownRaisedCategoryNeverMatches(t *testing.T) {
	junk := []string{"NOT-A-CATEGORY", "", "correctness-ish", "SECURITY"}
	for _, p := range inRepoPersonas {
		assert.False(t, InOpportunitySet(p, junk),
			"unrecognised raised values must not match %q's remit", p)
	}
}

// TestInOpportunitySet_IsCaseInsensitiveOnThePersonaKey keeps the predicate
// consistent with RemitCategories' documented lowercase key convention.
func TestInOpportunitySet_IsCaseInsensitiveOnThePersonaKey(t *testing.T) {
	assert.True(t, InOpportunitySet("Sasha", []string{"security"}))
	assert.True(t, InOpportunitySet("SASHA", []string{"security"}))
}

// TestInOpportunitySet_DoesNotMutateItsInput proves the predicate leaves the
// caller's slice alone — it is called once per (persona, case) pair over a
// shared, unioned slice, so an in-place sort or dedupe would corrupt every
// later persona's answer for the same case.
func TestInOpportunitySet_DoesNotMutateItsInput(t *testing.T) {
	raised := []string{"style", "security", "correctness"}
	before := append([]string{}, raised...)
	InOpportunitySet("sasha", raised)
	assert.Equal(t, before, raised, "InOpportunitySet must not reorder or rewrite its input")
}
