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

	var buf bytes.Buffer
	writeReviewSummary(&buf, reg, 142300*time.Millisecond, 10)
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
	var buf bytes.Buffer
	writeReviewSummary(&buf, reg, time.Second, 3)
	out := buf.String()
	if !strings.Contains(out, "Findings: 0\n") {
		t.Errorf("expected bare 'Findings: 0' line, got:\n%s", out)
	}
}
