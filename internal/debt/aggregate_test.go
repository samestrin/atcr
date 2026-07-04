package debt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

// refNow is a fixed reference instant so age-bucket tests are deterministic.
var refNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

func TestComponent_DepthTwo(t *testing.T) {
	assert.Equal(t, "internal/autofix", Component("internal/autofix/apply.go:108"))
	assert.Equal(t, "cmd/atcr", Component("cmd/atcr/review.go"))
	// A single-segment path is its own component.
	assert.Equal(t, "main.go", Component("main.go:3"))
	// Free text with no path separator is bucketed under a stable sentinel.
	assert.Equal(t, "(unscoped)", Component("see the design doc"))
	// A filename containing a space but carrying an extension is still a real
	// file, not free-text prose.
	assert.Equal(t, "my file.go", Component("my file.go:3"))
}

func TestSummarize_SeverityCounts(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 5)

	require.Len(t, s.BySeverity, 4)
	// Ordered most-severe first, regardless of corpus order.
	assert.Equal(t, "CRITICAL", s.BySeverity[0].Severity)
	assert.Equal(t, "LOW", s.BySeverity[3].Severity)

	// CRITICAL: 1 open. HIGH: 1 open. MEDIUM: 1 deferred. LOW: 1 resolved.
	bySev := map[string]SeverityCount{}
	for _, sc := range s.BySeverity {
		bySev[sc.Severity] = sc
	}
	assert.Equal(t, 1, bySev["CRITICAL"].Open)
	assert.Equal(t, 1, bySev["MEDIUM"].Deferred)
	assert.Equal(t, 1, bySev["LOW"].Resolved)
	assert.Equal(t, 1, bySev["HIGH"].Total)
}

func TestSummarize_Totals(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 5)
	assert.Equal(t, 4, s.Total)
	assert.Equal(t, 2, s.Open)     // HIGH + CRITICAL
	assert.Equal(t, 1, s.Deferred) // MEDIUM
	assert.Equal(t, 1, s.Resolved) // LOW
}

func TestSummarize_ByComponent(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 5)
	byComp := map[string]int{}
	for _, c := range s.ByComponent {
		byComp[c.Component] = c.Total
	}
	assert.Equal(t, 2, byComp["internal/autofix"])
	assert.Equal(t, 2, byComp["cmd/atcr"])

	// Deterministic order: Total desc, then component name asc.
	for i := 1; i < len(s.ByComponent); i++ {
		prev, cur := s.ByComponent[i-1], s.ByComponent[i]
		if prev.Total == cur.Total {
			assert.LessOrEqual(t, prev.Component, cur.Component)
		} else {
			assert.Greater(t, prev.Total, cur.Total)
		}
	}
}

func TestSummarize_ByAge_UnresolvedOnly(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 5)
	total := 0
	for _, b := range s.ByAge {
		total += b.Count
	}
	// Only the 3 unresolved items (open+deferred) are aged; the resolved LOW
	// item is excluded from the age backlog.
	assert.Equal(t, 3, total)
}

func TestSummarize_TopPriority_SeverityThenAge(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 5)
	require.NotEmpty(t, s.Top)
	// Only unresolved items are candidates; most-severe first.
	assert.Equal(t, "CRITICAL", s.Top[0].Severity)
	for _, r := range s.Top {
		assert.NotEqual(t, "resolved", r.Status)
	}
	assert.LessOrEqual(t, len(s.Top), 3)
}

func TestSummarize_TopPriority_RespectsLimit(t *testing.T) {
	s := Summarize(Flatten(sampleShards()), refNow, 1)
	assert.Len(t, s.Top, 1)
	assert.Equal(t, "CRITICAL", s.Top[0].Severity)
}

func TestAgeDays_UnparseableDateIsUnknownBucket(t *testing.T) {
	recs := []Record{{Date: "not-a-date", Item: mkItem("open", "HIGH")}}
	s := Summarize(recs, refNow, 5)
	var unknown int
	for _, b := range s.ByAge {
		if b.Label == "unknown" {
			unknown = b.Count
		}
	}
	assert.Equal(t, 1, unknown)
}

// mkItem is a tiny helper for constructing minimal valid-ish items in tests.
func mkItem(status, sev string) tdmigrate.Item {
	return tdmigrate.Item{Status: status, Severity: sev, Group: "1", File: "x.go:1",
		Problem: "p", Fix: "f", Category: "c", Source: "s"}
}
