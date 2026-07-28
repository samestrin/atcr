package payload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// replaySignal names the four independent escalation signals the replay harness
// attributes promotions to. diffNativeFires short-circuits across the first
// three, so per-signal attribution has to evaluate each predicate on its own —
// otherwise a file that fires churn AND adjacency would only ever be counted
// against churn, and the "which lever is the worst offender" question the
// measurement exists to answer could not be asked.
type replaySignal int

const (
	sigChurn replaySignal = iota
	sigHunkCount
	sigAdjacency
	sigComplexity
	sigCount
)

// replayStats accumulates escalation outcomes over a replay window. It is the
// arithmetic the AC2 measurement and the AC3 band decision rest on, so it is
// tested directly rather than trusted: a wrong denominator here would silently
// invalidate every number this epic reports.
type replayStats struct {
	commits  int
	files    int
	fired    [sigCount]int
	promoted int
	toBlocks int
	toFiles  int
}

// record measures one changed file against cfg and folds the outcome in.
func (r *replayStats) record(_ EscalationConfig, _ PayloadMode, _ fileSignals) {
	r.files++
}

// promotionRate is the percentage of analyzed files promoted above their
// configured mode.
func (r *replayStats) promotionRate() float64 { return 0 }

// signalRate is the percentage of analyzed files for which sig fired, counted
// independently of whether another signal fired on the same file.
func (r *replayStats) signalRate(_ replaySignal) float64 { return 0 }

// TestReplayStats_RatesAndAttribution pins the harness arithmetic against a
// hand-computed fixture: five files with known signals, so every rate the
// measurement reports has an independently-derived expected value.
func TestReplayStats_RatesAndAttribution(t *testing.T) {
	cfg := DefaultEscalationConfig()
	var r replayStats

	// A: churn only (60/100 >= 0.5). One hunk, no complexity.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 60, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 60}},
	})
	// B: complexity only. Churn not applicable (added file), single hunk.
	r.record(cfg, ModeDiff, fileSignals{
		headLines: 100, hunks: []lineRange{{start: 1, end: 4}}, cyclomatic: 20,
	})
	// C: churn AND complexity -> the only file that reaches files mode.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 90, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 90}}, cyclomatic: 20,
	})
	// D: nothing fires. 5/100 churn, one hunk, trivial complexity.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 5, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 5}}, cyclomatic: 3,
	})
	// E: adjacency only. Gap between hunks is 15-12-1 = 2 < 10.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 10, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 10, end: 12}, {start: 15, end: 20}},
	})

	require.Equal(t, 5, r.files)
	require.Equal(t, 4, r.promoted, "A, B, C and E promote; D does not")
	require.Equal(t, 3, r.toBlocks, "A, B and E reach blocks")
	require.Equal(t, 1, r.toFiles, "only C fires both sides of the ladder")

	require.InDelta(t, 80.0, r.promotionRate(), 0.001)
	require.InDelta(t, 40.0, r.signalRate(sigChurn), 0.001, "A and C")
	require.InDelta(t, 0.0, r.signalRate(sigHunkCount), 0.001, "no file reaches 4 hunks")
	require.InDelta(t, 20.0, r.signalRate(sigAdjacency), 0.001, "E only")
	require.InDelta(t, 40.0, r.signalRate(sigComplexity), 0.001, "B and C")
}

// TestReplayStats_EmptyWindowIsNotADivideByZero guards the degenerate case: a
// window that analyzed nothing must report 0%, not NaN, or the reported rate
// would be unusable as an acceptance number.
func TestReplayStats_EmptyWindowIsNotADivideByZero(t *testing.T) {
	var r replayStats
	require.Equal(t, 0.0, r.promotionRate())
	require.Equal(t, 0.0, r.signalRate(sigChurn))
}
