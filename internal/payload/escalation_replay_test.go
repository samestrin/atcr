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

// record measures one changed file against cfg and folds the outcome in. The
// promotion decision goes through the production escalate(), so the harness
// measures the shipped heuristic rather than a parallel reimplementation of it.
func (r *replayStats) record(cfg EscalationConfig, base PayloadMode, s fileSignals) {
	r.files++
	if cfg.churnFires(s) {
		r.fired[sigChurn]++
	}
	if cfg.hunkCountFires(s) {
		r.fired[sigHunkCount]++
	}
	if cfg.hunksAreAdjacent(s.hunks) {
		r.fired[sigAdjacency]++
	}
	if cfg.complexityFires(s) {
		r.fired[sigComplexity]++
	}
	switch got := cfg.escalate(base, s); got {
	case base:
		return
	case ModeFiles:
		r.promoted++
		r.toFiles++
	default:
		r.promoted++
		r.toBlocks++
	}
}

// promotionRate is the percentage of analyzed files promoted above their
// configured mode.
func (r *replayStats) promotionRate() float64 { return percentOf(r.promoted, r.files) }

// signalRate is the percentage of analyzed files for which sig fired, counted
// independently of whether another signal fired on the same file.
func (r *replayStats) signalRate(sig replaySignal) float64 {
	if sig < 0 || sig >= sigCount {
		return 0
	}
	return percentOf(r.fired[sig], r.files)
}

// percentOf is n/total as a percentage, defined as 0 for an empty denominator
// so a window that analyzed nothing reports 0% rather than NaN.
func percentOf(n, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

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
