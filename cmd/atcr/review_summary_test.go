package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
)

func TestWriteReviewSummary(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.Counter(metrics.NameAgentsSucceeded).Add(8)
	reg.Counter(metrics.NameAgentsFailed).Add(1)
	reg.Counter(metrics.NameAgentsTimedOut).Add(1)
	reg.Counter(metrics.NameAPICallsTotal).Add(12)
	reg.Counter(metrics.NameFindingsTotal).Add(5)
	reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, "HIGH")).Add(2)
	reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, "MEDIUM")).Add(3)

	// Seed a zero baseline (fresh registry) so the delta equals the seeded counts —
	// the assertions below are unchanged from when the helper read the registry directly.
	snap := snapshotSummaryMetrics(reg).sub(snapshotSummaryMetrics(metrics.NewRegistry()))

	var buf bytes.Buffer
	writeReviewSummary(&buf, snap, 142300*time.Millisecond, 10)
	out := buf.String()

	for _, want := range []string{
		"Total elapsed: 142.3s",
		"Agents: 8/10 succeeded, 1 failed, 1 timed out",
		"API calls: 12",
		"Findings: 5 (2 HIGH, 3 MEDIUM)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n---\n%s", want, out)
		}
	}
}

// TestWriteReviewSummaryNoFindings verifies the breakdown suffix is omitted when
// there are no findings.
func TestWriteReviewSummaryNoFindings(t *testing.T) {
	reg := metrics.NewRegistry()
	snap := snapshotSummaryMetrics(reg).sub(snapshotSummaryMetrics(metrics.NewRegistry()))
	var buf bytes.Buffer
	writeReviewSummary(&buf, snap, time.Second, 3)
	out := buf.String()
	if !strings.Contains(out, "Findings: 0\n") {
		t.Errorf("expected bare 'Findings: 0' line, got:\n%s", out)
	}
}

// TestWriteReviewSummaryIsolatesThisReview verifies the summary reports only this
// review's contribution (post-review snapshot minus a pre-review baseline), not the
// cumulative registry totals. Regression guard for the multi-review case: a process
// that already ran a prior review (serve mode) must not report succeeded greater
// than the current review's totalAgents.
func TestWriteReviewSummaryIsolatesThisReview(t *testing.T) {
	reg := metrics.NewRegistry()
	// A prior review already left cumulative counts in the registry.
	reg.Counter(metrics.NameAgentsSucceeded).Add(7)
	reg.Counter(metrics.NameAPICallsTotal).Add(20)
	reg.Counter(metrics.NameFindingsTotal).Add(9)
	baseline := snapshotSummaryMetrics(reg)

	// This review contributes 2 succeeded, 1 failed, 3 API calls, 1 HIGH finding.
	reg.Counter(metrics.NameAgentsSucceeded).Add(2)
	reg.Counter(metrics.NameAgentsFailed).Add(1)
	reg.Counter(metrics.NameAPICallsTotal).Add(3)
	reg.Counter(metrics.NameFindingsTotal).Add(1)
	reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, "HIGH")).Add(1)

	delta := snapshotSummaryMetrics(reg).sub(baseline)

	var buf bytes.Buffer
	writeReviewSummary(&buf, delta, time.Second, 3)
	out := buf.String()

	for _, want := range []string{
		"Agents: 2/3 succeeded, 1 failed, 0 timed out",
		"API calls: 3",
		"Findings: 1 (1 HIGH)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n---\n%s", want, out)
		}
	}
}

// TestWriteReviewSummaryDenominatorIsPerAttempt verifies the "Agents: X/Y" line
// uses a per-attempt denominator (atcr_agents_total, incremented once per agent
// invocation including every fallback) so numerator and denominator share one unit.
// A slot whose primary fails and fallback succeeds is 2 attempts: the line must read
// 1/2, never 1/1 (which mixes per-attempt successes against a per-slot denominator
// and can print succeeded+failed greater than the denominator).
func TestWriteReviewSummaryDenominatorIsPerAttempt(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.Counter(metrics.NameAgentsTotal).Add(2)     // 2 attempts: primary + fallback
	reg.Counter(metrics.NameAgentsSucceeded).Add(1) // fallback succeeded
	reg.Counter(metrics.NameAgentsFailed).Add(1)    // primary failed
	snap := snapshotSummaryMetrics(reg).sub(snapshotSummaryMetrics(metrics.NewRegistry()))

	var buf bytes.Buffer
	writeReviewSummary(&buf, snap, time.Second, 1) // caller's per-slot total is 1
	out := buf.String()

	if !strings.Contains(out, "Agents: 1/2 succeeded, 1 failed, 0 timed out") {
		t.Errorf("denominator must be per-attempt agents_total (=2), got:\n%s", out)
	}
}
