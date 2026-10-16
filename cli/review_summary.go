package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/report"
	"github.com/spf13/cobra"
)

// severityOrder is the high-to-low display order for the findings breakdown.
var severityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// summarySnapshot captures the values of the counters writeReviewSummary reports,
// read from a registry at a point in time. Diffing a post-review snapshot against a
// pre-review baseline (snapshotSummaryMetrics before fan-out, sub after) isolates a
// single review's contribution. That keeps the summary correct even in a long-lived
// process where DefaultRegistry accumulates across reviews (serve mode) instead of
// holding one review's totals. Every agent count here is per-attempt — each
// invocation, including each fallback in a slot's chain, increments these counters
// once (see engine.invokeAgent), matching atcr_agents_total and the MCP export. The
// "Agents: X/Y" line therefore draws numerator and denominator from one unit, so a
// slot whose primary fails and fallback succeeds reads 1/2, not 1/1.
type summarySnapshot struct {
	agentsTotal        int64
	agentsSucceeded    int64
	agentsFailed       int64
	agentsTimedOut     int64
	apiCalls           int64
	findingsTotal      int64
	findingsBySeverity map[string]int64
}

// snapshotSummaryMetrics reads the current values of the metrics the review summary
// reports from reg. Take one before fan-out and one after, then sub to get the
// review's deltas.
func snapshotSummaryMetrics(reg *metrics.Registry) summarySnapshot {
	bySeverity := make(map[string]int64, len(severityOrder))
	for _, sev := range severityOrder {
		bySeverity[sev] = reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, sev)).Value()
	}
	return summarySnapshot{
		agentsTotal:        reg.Counter(metrics.NameAgentsTotal).Value(),
		agentsSucceeded:    reg.Counter(metrics.NameAgentsSucceeded).Value(),
		agentsFailed:       reg.Counter(metrics.NameAgentsFailed).Value(),
		agentsTimedOut:     reg.Counter(metrics.NameAgentsTimedOut).Value(),
		apiCalls:           reg.Counter(metrics.NameAPICallsTotal).Value(),
		findingsTotal:      reg.Counter(metrics.NameFindingsTotal).Value(),
		findingsBySeverity: bySeverity,
	}
}

// sub returns s minus baseline per metric — this review's contribution to each
// counter when s is the post-review snapshot and baseline the pre-review one.
func (s summarySnapshot) sub(baseline summarySnapshot) summarySnapshot {
	out := summarySnapshot{
		agentsTotal:        s.agentsTotal - baseline.agentsTotal,
		agentsSucceeded:    s.agentsSucceeded - baseline.agentsSucceeded,
		agentsFailed:       s.agentsFailed - baseline.agentsFailed,
		agentsTimedOut:     s.agentsTimedOut - baseline.agentsTimedOut,
		apiCalls:           s.apiCalls - baseline.apiCalls,
		findingsTotal:      s.findingsTotal - baseline.findingsTotal,
		findingsBySeverity: make(map[string]int64, len(severityOrder)),
	}
	for _, sev := range severityOrder {
		out.findingsBySeverity[sev] = s.findingsBySeverity[sev] - baseline.findingsBySeverity[sev]
	}
	return out
}

// writeReviewSummary prints the end-of-review metrics summary (Epic 4.4 AC3):
// duration, agent success/failure/timeout counts, API calls, and findings with a
// severity breakdown. m holds this review's deltas (post-review snapshot minus the
// pre-review baseline), so the counts reflect this review alone rather than the
// process-cumulative registry — these are the same metrics the MCP server exports.
// The "Agents: X/Y" denominator is m.agentsTotal (per-attempt), the same unit as the
// numerator, so the two never disagree on granularity. m is a parameter so the helper
// is unit testable against a seeded snapshot.
func writeReviewSummary(w io.Writer, m summarySnapshot, elapsed time.Duration) {
	// elapsed is total wall-clock from before config load to review completion,
	// not just the agent fan-out window the atcr_review_duration_seconds histogram measures.
	_, _ = fmt.Fprintf(w, "Total elapsed: %.1fs\n", elapsed.Seconds())
	_, _ = fmt.Fprintf(w, "Agents: %d/%d succeeded, %d failed, %d timed out\n",
		m.agentsSucceeded, m.agentsTotal, m.agentsFailed, m.agentsTimedOut)
	_, _ = fmt.Fprintf(w, "API calls: %d\n", m.apiCalls)
	_, _ = fmt.Fprintf(w, "Findings: %d%s\n", m.findingsTotal, severityBreakdown(m))
}

// writeReviewSummaryAXI is the --axi analogue of writeReviewSummary: instead of
// the four human lines it emits one token-dense TOON run-summary payload carrying
// the review identity (id, dir) and the same per-attempt agent/finding deltas m
// holds. review.go and resume.go both call it so their --axi stdout is
// byte-identical for equivalent data (AC 01-04). The encoder lives in the report
// package so the review-summary payload and the report --axi findings payload
// share one TOON serializer. Elapsed is intentionally omitted — the AC enumerates
// id/dir/agent-counts/findings-counts, and dropping the wall-clock value keeps the
// payload deterministic for a given run.
//
// legacyPipe selects the deprecated pipe-delimited encoder (--legacy-pipe /
// ATCR_LEGACY_PIPE); the deprecation notice is written once at flag-parse time.
//
// cov carries the run's chunk coverage (Epic 35.16.13 T5) as scalars; an
// unmeasured cov adds no columns, so a bulk or diff run's payload is unchanged.
// The legacy pipe encoder never adds them: its column set is frozen.
func writeReviewSummaryAXI(w io.Writer, legacyPipe bool, id, dir string, m summarySnapshot, cov runCoverage) error {
	render := report.RenderReviewSummaryAXI
	if legacyPipe {
		render = report.RenderReviewSummaryPipe
	}
	return render(w, report.ReviewSummaryAXI{
		ID:              id,
		Dir:             dir,
		AgentsSucceeded: m.agentsSucceeded,
		AgentsTotal:     m.agentsTotal,
		AgentsFailed:    m.agentsFailed,
		AgentsTimedOut:  m.agentsTimedOut,
		APICalls:        m.apiCalls,
		FindingsTotal:   m.findingsTotal,
		// Carry the same per-severity breakdown the human summary emits via
		// severityBreakdown, so the AXI payload is no less informative than stdout.
		FindingsCritical: m.findingsBySeverity["CRITICAL"],
		FindingsHigh:     m.findingsBySeverity["HIGH"],
		FindingsMedium:   m.findingsBySeverity["MEDIUM"],
		FindingsLow:      m.findingsBySeverity["LOW"],
		CoverageMeasured: cov.measured,
		BinsReviewed:     int64(cov.BinsReviewed),
		BinsPlanned:      int64(cov.BinsPlanned),
		FilesCovered:     int64(cov.FilesCovered),
		FilesInScope:     int64(cov.FilesInScope),
	})
}

// runCoverage is a review's chunk coverage as read from its chunks.json records
// (Epic 35.16.13 T5). measured is false when no persona wrote a record (a bulk
// or diff review bins nothing) and when the records could not be read; err
// distinguishes the second case, which the coverage gate fails closed on.
type runCoverage struct {
	fanout.Coverage
	measured bool
	err      error
}

// readRunCoverage reads dir's coverage, logging an unreadable record rather than
// failing the run: coverage reporting must never cost a review its artifacts.
//
// recordErr is the run's chunks.json write failure (ReviewResult.ChunkRecordErr),
// nil when there is no run in hand. A failed write may have left no record, which
// would read as "binned nothing", so it makes the coverage unmeasurable instead.
func readRunCoverage(ctx context.Context, dir string, recordErr error) runCoverage {
	if recordErr != nil {
		err := fmt.Errorf("a chunks.json write failed: %w", recordErr)
		log.FromContext(ctx).Warn("review coverage cannot be measured", "dir", dir, "err", err)
		return runCoverage{err: err}
	}
	cov, ok, err := fanout.ReadCoverage(dir)
	if err != nil {
		log.FromContext(ctx).Warn("review coverage could not be read from chunks.json", "dir", dir, "err", err)
		return runCoverage{err: err}
	}
	return runCoverage{Coverage: cov, measured: ok}
}

// writeCoverageLine prints the human coverage line after the summary block:
// persona-bins reviewed / planned summed across personas, and files covered by
// at least one persona / files in scope. Nothing is printed when the run binned
// nothing, so a bulk or diff run's summary is unchanged.
func writeCoverageLine(w io.Writer, cov runCoverage) {
	switch {
	case cov.err != nil:
		_, _ = fmt.Fprintf(w, "Coverage: unavailable (%v)\n", cov.err)
	case cov.measured:
		_, _ = fmt.Fprintf(w, "Coverage: %d/%d persona-bins reviewed, %d/%d files covered by at least one persona (%.1f%%)\n",
			cov.BinsReviewed, cov.BinsPlanned, cov.FilesCovered, cov.FilesInScope, cov.FilePercent())
		// Epic 35.16.14 fix AC1b: name the reason for bins no reviewer could take.
		if cov.BinsOverWindow > 0 {
			_, _ = fmt.Fprintf(w, "Coverage: %d unreviewed persona-bin(s) hold a single file larger than the reviewer's context window; they were sent once and not retried (over_window in chunks.json)\n",
				cov.BinsOverWindow)
		}
	}
}

// requireCoverageFlag is the --require-coverage threshold flag name.
const requireCoverageFlag = "require-coverage"

// requireCoverageThreshold returns the --require-coverage percentage and whether
// it was set; a value outside 0-100, NaN or an infinity is a usage error (exit 2).
func requireCoverageThreshold(cmd *cobra.Command) (float64, bool, error) {
	if !cmd.Flags().Changed(requireCoverageFlag) {
		return 0, false, nil
	}
	pct, err := cmd.Flags().GetFloat64(requireCoverageFlag)
	if err != nil {
		return 0, false, usageError(err)
	}
	// NaN passes both bound checks and no percentage is ever below it, so it
	// would silently disable the gate.
	if math.IsNaN(pct) || math.IsInf(pct, 0) || pct < 0 || pct > 100 {
		return 0, false, usageError(fmt.Errorf("--%s must be between 0 and 100, got %g", requireCoverageFlag, pct))
	}
	return pct, true, nil
}

// coverageGate applies --require-coverage to a finished run's FILE coverage. It
// runs only after the artifacts and summary are written, and only when the flag
// was set, so a run without it keeps today's exit code. Unreadable records fail
// closed (exit 1): the gate cannot vouch for coverage it could not measure. A run
// that binned nothing (bulk or diff) has no chunk coverage to gate; that is
// noted on stderr and passes.
func coverageGate(stderr io.Writer, cov runCoverage, pct float64) error {
	switch {
	case cov.err != nil:
		return &codedError{code: exitFailure, err: fmt.Errorf("--%s %g: coverage could not be measured: %w", requireCoverageFlag, pct, cov.err)}
	case !cov.measured:
		_, _ = fmt.Fprintf(stderr, "note: --%s not applied: this run wrote no chunks.json (only a multi-chunk --all/--scope review is binned)\n", requireCoverageFlag)
		return nil
	case cov.FilePercent() < pct:
		return &codedError{code: exitFailure, err: fmt.Errorf("--%s %g: %d/%d files covered (%.1f%%) is below the threshold",
			requireCoverageFlag, pct, cov.FilesCovered, cov.FilesInScope, cov.FilePercent())}
	}
	return nil
}

// severityBreakdown renders " (2 HIGH, 3 MEDIUM)" for the non-zero severities in
// high-to-low order, or "" when no findings were recorded.
func severityBreakdown(m summarySnapshot) string {
	var parts []string
	for _, sev := range severityOrder {
		if n := m.findingsBySeverity[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
