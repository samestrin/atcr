package debt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDashboard_Deterministic(t *testing.T) {
	recs := Flatten(sampleShards())
	a := RenderDashboard(recs, 10)
	b := RenderDashboard(recs, 10)
	assert.Equal(t, a, b, "dashboard render must be byte-identical across runs (no timestamp)")
}

func TestRenderDashboard_HasSections(t *testing.T) {
	out := RenderDashboard(Flatten(sampleShards()), 10)
	assert.Contains(t, out, "# Technical Debt Dashboard")
	assert.Contains(t, out, "## By Severity")
	assert.Contains(t, out, "## By Component")
	assert.Contains(t, out, "## By Age")
	assert.Contains(t, out, "## Top Priority")
	// A CRITICAL open item should surface in the severity table and top list.
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "cmd/atcr/autofix.go:248")
}

func TestRenderDashboard_TotalsRow(t *testing.T) {
	out := RenderDashboard(Flatten(sampleShards()), 10)
	// 4 total, 2 open, 1 deferred, 1 resolved.
	assert.Contains(t, out, "**Total:** 4")
	assert.Contains(t, out, "**Open:** 2")
}

func TestRenderDashboard_ScrubsSecretTokens(t *testing.T) {
	recs := []Record{{
		Date:  "2026-06-26",
		Label: "leak",
		Item:  mkItem("open", "CRITICAL"),
	}}
	recs[0].File = "internal/x.go:1"
	recs[0].Problem = "leaked key sk-ABCDEF0123456789 in the log line"

	out := RenderDashboard(recs, 10)
	assert.NotContains(t, out, "sk-ABCDEF0123456789")
	assert.Contains(t, out, "[redacted]")
}

func TestRenderDashboard_TopRespectsLimitAndExcludesResolved(t *testing.T) {
	out := RenderDashboard(Flatten(sampleShards()), 1)
	// Only the single highest-priority (CRITICAL) item is listed in Top Priority.
	top := out[strings.Index(out, "## Top Priority"):]
	assert.Contains(t, top, "cmd/atcr/autofix.go:248")          // CRITICAL open
	assert.NotContains(t, top, "internal/autofix/revert.go:41") // resolved LOW, excluded
}

func TestRenderDashboard_AgeByMonthIsTimeInvariant(t *testing.T) {
	out := RenderDashboard(Flatten(sampleShards()), 10)
	age := out[strings.Index(out, "## By Age"):]
	// Unresolved items are dated 2026-06 (CRITICAL, MEDIUM) — grouped by month.
	require.Contains(t, age, "2026-06")
}
