package verify

import (
	reclib "github.com/samestrin/atcr/reconcile"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func v(verdict, skeptic, notes string) *reclib.Verification {
	return &reclib.Verification{Verdict: verdict, Skeptic: skeptic, Notes: notes}
}

func TestAggregateVerdicts_Unanimous(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "s1", "a"),
		v(verdictConfirmed, "s2", "b"),
		v(verdictConfirmed, "s3", "c"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictConfirmed, got.Verdict)
}

func TestAggregateVerdicts_Majority(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "s1", "holds"),
		v(verdictConfirmed, "s2", "also holds"),
		v(verdictRefuted, "s3", "nope"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictConfirmed, got.Verdict)
}

func TestAggregateVerdicts_MajorityRefuted(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictRefuted, "s1", "wrong"),
		v(verdictRefuted, "s2", "also wrong"),
		v(verdictConfirmed, "s3", "right"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictRefuted, got.Verdict)
}

func TestAggregateVerdicts_DisagreementTie(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "s1", "reason-confirm"),
		v(verdictRefuted, "s2", "reason-refute"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictUnverifiable, got.Verdict)
	// all reasonings preserved
	assert.Contains(t, got.Notes, "reason-confirm")
	assert.Contains(t, got.Notes, "reason-refute")
}

func TestAggregateVerdicts_SingleSkeptic(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{v(verdictRefuted, "s1", "single reason")})
	require.NotNil(t, got)
	assert.Equal(t, verdictRefuted, got.Verdict)
	assert.Equal(t, "single reason", got.Notes)
}

func TestAggregateVerdicts_EmptySlice(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts(nil)
	require.NotNil(t, got)
	assert.Equal(t, verdictUnverifiable, got.Verdict)
}

// TestAggregateVerdicts_TieNoSkepticNames covers aggregation when per-skeptic
// verdicts carry no Skeptic name: the combined notes fall back to the verdict as
// the label and the aggregate Skeptic field is empty (no names to join).
func TestAggregateVerdicts_TieNoSkepticNames(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "", "yes reason"),
		v(verdictRefuted, "", "no reason"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictUnverifiable, got.Verdict)
	assert.Empty(t, got.Skeptic)
	assert.Contains(t, got.Notes, "yes reason")
	assert.Contains(t, got.Notes, "no reason")
	assert.Contains(t, got.Notes, verdictConfirmed)
}

func TestAggregateVerdicts_ThreeWaySplit(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "s1", "c"),
		v(verdictRefuted, "s2", "r"),
		v(verdictUnverifiable, "s3", "u"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictUnverifiable, got.Verdict)
}

// TestAggregateVerdicts_MajorityRefuterAbsentFromSkeptic asserts that in a
// 2-confirmed/1-refuted majority the dissenter's name and reasoning are either
// both present or both absent — the current bug lists the dissenter in Skeptic
// but omits their reasoning, making it look like they agreed.
func TestAggregateVerdicts_MajorityRefuterAbsentFromSkeptic(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "alice", "evidence holds"),
		v(verdictConfirmed, "bob", "also holds"),
		v(verdictRefuted, "carol", "disagrees"),
	})
	require.NotNil(t, got)
	assert.Equal(t, verdictConfirmed, got.Verdict)
	// carol is the dissenter — name and reasoning must both be absent or both present
	carolInSkeptic := strings.Contains(got.Skeptic, "carol")
	carolInNotes := strings.Contains(got.Notes, "disagrees")
	assert.Equal(t, carolInSkeptic, carolInNotes, "dissenter name and reasoning must be consistent (both present or both absent)")
}

// truncatedV is v with the shortened-read caveat set.
func truncatedV(verdict, skeptic, notes string) *reclib.Verification {
	x := v(verdict, skeptic, notes)
	x.Truncated = true
	return x
}

// TestAggregateVerdicts_TieDropsALosingVotersTruncation pins the tie arm's
// narrowing of the Truncated caveat — the counterpart of the decisive-vote rule
// TestWinningAttribution already covers.
//
// A tie's verdict is unverifiable, and that verdict comes from the TIE, not from
// any one voter. Annotating it with a losing confirmed voter's truncation would
// say "this unverifiable was reached from a shortened read" about a verdict that
// voter never produced — and the caveat is no longer cosmetic: it drives the
// per-reviewer precision exclusion in internal/scorecard.
//
// Mutation-verified as unpinned: replacing
// anyTruncated(filterByVerdict(valid, verdictUnverifiable)) with
// anyTruncated(valid) left the whole suite green.
func TestAggregateVerdicts_TieDropsALosingVotersTruncation(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		truncatedV(verdictConfirmed, "s1", "reason-confirm"),
		v(verdictRefuted, "s2", "reason-refute"),
	})
	require.NotNil(t, got)
	require.Equal(t, verdictUnverifiable, got.Verdict)
	assert.False(t, got.Truncated,
		"the truncation belongs to a confirmed verdict this record does not report — carrying it forward describes the wrong answer")
}

// TestAggregateVerdicts_TieKeepsAnUnverifiableVotersTruncation is the other half
// of the same rule: a voter who was ITSELF unverifiable from a shortened read
// describes the verdict the tie actually reports, so its caveat survives.
func TestAggregateVerdicts_TieKeepsAnUnverifiableVotersTruncation(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		truncatedV(verdictUnverifiable, "s1", "ran out of budget"),
		v(verdictConfirmed, "s2", "holds"),
	})
	require.NotNil(t, got)
	require.Equal(t, verdictUnverifiable, got.Verdict)
	assert.True(t, got.Truncated,
		"an unverifiable voter's shortened read is exactly what the tie's unverifiable reports")
}

// TestAggregateVerdicts_DecisiveVoteKeepsTheWinnersTruncation states the
// symmetric decisive-vote case in the same place, so the two arms of
// anyTruncated's contract can be read side by side.
func TestAggregateVerdicts_DecisiveVoteKeepsTheWinnersTruncation(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		truncatedV(verdictConfirmed, "s1", "holds"),
		v(verdictConfirmed, "s2", "also holds"),
		v(verdictRefuted, "s3", "nope"),
	})
	require.NotNil(t, got)
	require.Equal(t, verdictConfirmed, got.Verdict)
	assert.True(t, got.Truncated,
		"a winning voter read short, and the record reports that winner's verdict")
}

// TestAggregateVerdicts_DecisiveVoteIgnoresALosersTruncation completes the
// matrix: a losing voter's caveat never reaches a decisive record either.
func TestAggregateVerdicts_DecisiveVoteIgnoresALosersTruncation(t *testing.T) {
	t.Parallel()
	got := aggregateVerdicts([]*reclib.Verification{
		v(verdictConfirmed, "s1", "holds"),
		v(verdictConfirmed, "s2", "also holds"),
		truncatedV(verdictRefuted, "s3", "nope"),
	})
	require.NotNil(t, got)
	require.Equal(t, verdictConfirmed, got.Verdict)
	assert.False(t, got.Truncated,
		"the shortened read belongs to the refuter, whose verdict lost and is not what this record reports")
}
