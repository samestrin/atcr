package scorecard

import (
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The opportunity-scoped marker (epic 35.16.11.3.2 T2): ResolveTrustPriorsForReview
// returns, beside the priors, which denominator definition produced each one, so
// the reconcile call sites can attach it to reclib.Options.OpportunityScoped.

// mappedLenses and unmappedLenses are the panel the marker is defined over: the
// nine personas with an in-repo ## Focus remit, and the five registry-only
// lenses remit.go records as unmapped.
var (
	mappedLenses   = []string{"bruce", "dax", "greta", "ingrid", "kai", "mira", "otto", "penny", "sasha"}
	unmappedLenses = []string{"vera", "pace", "brad", "archer", "ronin"}
)

// The marker's own premise: every mapped lens has a remit and no unmapped one
// does. If the remit table grows, this test (not the flag) is what must change.
func TestOpportunityFlag_PanelMatchesRemitTable(t *testing.T) {
	for _, name := range mappedLenses {
		_, ok := remitFor(name)
		assert.True(t, ok, "%s is mapped", name)
	}
	for _, name := range unmappedLenses {
		_, ok := remitFor(name)
		assert.False(t, ok, "%s is unmapped", name)
	}
	assert.Len(t, personaRemit, len(mappedLenses))
}

// Done when: the flag is carried for each reviewer — the five unmapped lenses
// flagged not-opportunity-scoped, the mapped nine opportunity-scoped — and the
// five unmapped lenses keep scoring unchanged.
func TestResolveTrustPriorsForReview_FlagsEachReviewersDenominator(t *testing.T) {
	store := isolatedStore(t)
	panel := append(append([]string{}, mappedLenses...), unmappedLenses...)
	agents := make([]fanout.AgentStatus, 0, len(panel))
	for _, name := range panel {
		for _, r := range modelRuns("hist", name, "m", DefaultTrustMinRuns, 2, 1) {
			require.NoError(t, Append(store, r))
		}
		agents = append(agents, fanout.AgentStatus{Agent: name, Model: "m"})
	}
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir, agents...)

	priors, scoped, _ := ResolveTrustPriorsForReview(reviewDir)
	require.Len(t, priors, len(panel), "every lens clears the floor")
	require.Len(t, scoped, len(panel), "one flag per prior, keyed like the priors")

	for _, name := range mappedLenses {
		require.Contains(t, scoped, name)
		assert.True(t, scoped[name], "%s is scored over the opportunity-scoped denominator", name)
	}
	for _, name := range unmappedLenses {
		require.Contains(t, scoped, name)
		assert.False(t, scoped[name], "%s keeps the full denominator", name)
		// Scoring unchanged: the marker is read off the remit table, never fed
		// back into the rate, so an unmapped lens scores its plain raised ratio.
		assert.InDelta(t, 0.5, priors[name], 1e-9, "%s scores 1/2 exactly as before", name)
	}
}

// A flag describes a rate, so a reviewer with no prior has no flag, and an
// empty priors map gives a nil flag map (reconcile's "changes no output" case).
func TestResolveTrustPriorsForReview_FlagsOnlyReviewersWithAPrior(t *testing.T) {
	store := isolatedStore(t)
	for _, r := range modelRuns("hist", "vera", "m", DefaultTrustMinRuns, 2, 1) {
		require.NoError(t, Append(store, r))
	}
	for _, r := range modelRuns("hist", "sasha", "m", DefaultTrustMinRuns-1, 2, 1) {
		require.NoError(t, Append(store, r))
	}
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "vera", Model: "m"},
		fanout.AgentStatus{Agent: "sasha", Model: "m"})

	priors, scoped, _ := ResolveTrustPriorsForReview(reviewDir)
	assert.Equal(t, map[string]bool{"vera": false}, scoped,
		"sasha is under the floor: no prior, so no flag")
	assert.NotContains(t, priors, "sasha")

	_, scoped, _ = ResolveTrustPriorsForReview(t.TempDir())
	assert.Nil(t, scoped, "no priors, no flags")
}
