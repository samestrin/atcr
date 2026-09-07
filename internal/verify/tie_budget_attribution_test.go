package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/registry"
	reclib "github.com/samestrin/atcr/reconcile"
)

// TestWinningAttribution_TieCreditsBudgetsOnlyToUnverifiableVoters separates the
// three histories that used to serialize identically in
// reconciled/verification.json.
//
// winningAttribution credits tripped budgets by who WON. On a TIE nobody won,
// and the tie arm therefore credited EVERY participant — including a skeptic
// that voted confirmed while carrying a derived tool_budget_bytes trip that
// invokeSkeptic deliberately exempted. Three different histories then produced
// the same record:
//
//	(a) a DECLARED budget voided a skeptic's verdict → unverifiable
//	(b) a DERIVED ceiling truncated a read and the verdict stood
//	(c) a TIE produced unverifiable and some participant happened to be truncated
//
// Case (c) was impossible before the derived-ceiling exemption, because a
// tripped skeptic was always itself unverifiable and so was never a distinct
// contributor. A reader of verification.json cannot tell (a) from (c), which
// matters because they carry opposite meanings: in (a) a budget CAUSED the
// unverifiable, in (c) the tie did and the budget is incidental.
//
// The rule under test: on a tie, credit a budget only from a participant whose
// own verdict was unverifiable — the skeptics whose trip is causally connected
// to an inability to verify. The decisive arm is untouched, so (b) still
// records the trip on a confirmed/refuted winner.
func TestWinningAttribution_TieCreditsBudgetsOnlyToUnverifiableVoters(t *testing.T) {
	t.Parallel()

	skepticNamed := func(name, model string) Skeptic {
		sk := testSkeptic()
		sk.Name = name
		sk.Config = registry.AgentConfig{Provider: "p", Model: model, Role: registry.RoleSkeptic, SupportsFC: true}
		return sk
	}

	t.Run("case (c): a tie whose truncated participant WON nothing credits no budget", func(t *testing.T) {
		t.Parallel()
		// 1 confirmed (carrying an exempted derived trip) + 1 refuted = tie.
		models, budgets := winningAttribution(
			[]Skeptic{skepticNamed("s1", "m-s1"), skepticNamed("s2", "m-s2")},
			[]*reclib.Verification{
				{Verdict: verdictConfirmed, Skeptic: "s1"},
				{Verdict: verdictRefuted, Skeptic: "s2"},
			},
			[][]string{{budgetToolBytes}, nil},
			verdictUnverifiable,
		)
		assert.Contains(t, models, "m-s1", "a tie still names every participant's model")
		assert.Contains(t, models, "m-s2", "a tie still names every participant's model")
		assert.Empty(t, budgets,
			"the tie made this unverifiable, not the budget — crediting it makes a tie read as a budget-voided verdict")
	})

	t.Run("case (a): a tie whose unverifiable voter tripped a budget still credits it", func(t *testing.T) {
		t.Parallel()
		// 1 confirmed + 1 unverifiable-from-a-declared-trip = tie. The trip here
		// IS why that skeptic could not verify, so the audit record keeps it.
		_, budgets := winningAttribution(
			[]Skeptic{skepticNamed("s1", "m-s1"), skepticNamed("s2", "m-s2")},
			[]*reclib.Verification{
				{Verdict: verdictConfirmed, Skeptic: "s1"},
				{Verdict: verdictUnverifiable, Skeptic: "s2"},
			},
			[][]string{nil, {budgetToolBytes}},
			verdictUnverifiable,
		)
		assert.Equal(t, []string{budgetToolBytes}, budgets,
			"a participant that could not verify BECAUSE of a budget must keep that budget on the record")
	})

	t.Run("case (b): a decisive winner still carries its own exempted trip", func(t *testing.T) {
		t.Parallel()
		// 2 confirmed > 1 refuted: decisive. The exemption's whole purpose is that
		// this line survives — see invokeSkeptic's derived-ceiling branch.
		_, budgets := winningAttribution(
			[]Skeptic{skepticNamed("s1", "m-s1"), skepticNamed("s2", "m-s2"), skepticNamed("s3", "m-s3")},
			[]*reclib.Verification{
				{Verdict: verdictConfirmed, Skeptic: "s1"},
				{Verdict: verdictConfirmed, Skeptic: "s2"},
				{Verdict: verdictRefuted, Skeptic: "s3"},
			},
			[][]string{{budgetToolBytes}, nil, nil},
			verdictConfirmed,
		)
		assert.Equal(t, []string{budgetToolBytes}, budgets,
			"a decisive skeptic that answered from a truncated read must still say so")
	})
}
