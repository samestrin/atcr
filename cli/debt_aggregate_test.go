package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtRefNow is a fixed reference instant so age-bucket tests are deterministic.
var debtRefNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

// mkDebtRecord builds a minimal record for the aggregation tests. It mirrors the
// deleted internal/debt package's mkItem helper.
func mkDebtRecord(status, sev, file string, line int, ts string) localdebt.Record {
	r := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-run", Timestamp: ts,
		Status: status, Severity: sev, File: file, Line: line,
		Problem: "p", Fix: "f", Category: "c",
	}
	r.StampID()
	return r
}

// ---------------------------------------------------------------------------
// debtComponent — ported from internal/debt's Component. The :line strip is
// gone (localdebt.Record carries Line separately, so File never has a line
// suffix); the hasExtension/wordCount prose heuristic is carried verbatim,
// because File is still a free-text string on the wire and a hand-edited or
// third-party record can put prose there.
// ---------------------------------------------------------------------------

func TestDebtComponent_DepthTwo(t *testing.T) {
	assert.Equal(t, "internal/autofix", debtComponent("internal/autofix/apply.go"))
	assert.Equal(t, "cmd/atcr", debtComponent("cmd/atcr/review.go"))
	// A single-segment path is its own component.
	assert.Equal(t, "main.go", debtComponent("main.go"))
	// Free text with no path separator is bucketed under a stable sentinel.
	assert.Equal(t, "(unscoped)", debtComponent("see the design doc"))
	// A filename containing a space but carrying an extension is still a real
	// file, not free-text prose.
	assert.Equal(t, "my file.go", debtComponent("my file.go"))
	// An empty File (a record with no location) is bucketed, never dropped.
	assert.Equal(t, "(unscoped)", debtComponent(""))
}

// TD item E: File is a free-text wire string, so the same path arrives spelled
// different ways. debtComponent must normalize before bucketing: a leading ./
// and redundant separators collapse to the plain spelling's bucket, and an
// absolute path drops its leading separator so the two-segment rule applies to
// the real path instead of bucketing every absolute path under "/<first>".
func TestDebtComponent_NormalizesPathSpellings(t *testing.T) {
	// ./-prefixed and plain spellings of the same path share ONE bucket.
	assert.Equal(t, "internal/autofix", debtComponent("./internal/autofix/apply.go"))
	assert.Equal(t, debtComponent("internal/autofix/apply.go"), debtComponent("./internal/autofix/apply.go"))
	// Redundant separators collapse the same way.
	assert.Equal(t, "internal/autofix", debtComponent("internal//autofix/apply.go"))
	// An absolute path buckets as its first two real segments, not "/abs".
	assert.Equal(t, "abs/x", debtComponent("/abs/x/y.go"))
	// The guards that must survive normalization.
	assert.Equal(t, "(unscoped)", debtComponent(""))
	assert.Equal(t, "(unscoped)", debtComponent("see the design doc"))
	assert.Equal(t, "main.go", debtComponent("main.go"))
}

func TestSummarizeDebt_SeverityCounts(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)

	require.Len(t, s.BySeverity, 4)
	// Ordered most-severe first, regardless of corpus order.
	assert.Equal(t, "CRITICAL", s.BySeverity[0].Severity)
	assert.Equal(t, "LOW", s.BySeverity[3].Severity)

	// CRITICAL: 1 open. HIGH: 1 open. MEDIUM: 1 deferred. LOW: 1 resolved.
	bySev := map[string]debtSeverityCount{}
	for _, sc := range s.BySeverity {
		bySev[sc.Severity] = sc
	}
	assert.Equal(t, 1, bySev["CRITICAL"].Open)
	assert.Equal(t, 1, bySev["MEDIUM"].Deferred)
	assert.Equal(t, 1, bySev["LOW"].Resolved)
	assert.Equal(t, 1, bySev["HIGH"].Total)
}

func TestSummarizeDebt_Totals(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)
	assert.Equal(t, 4, s.Total)
	assert.Equal(t, 2, s.Open)     // HIGH + CRITICAL, both empty-status
	assert.Equal(t, 1, s.Deferred) // MEDIUM
	assert.Equal(t, 1, s.Resolved) // LOW
	assert.Equal(t, 0, s.Wontfix)
}

// wontfix has no counterpart in the .planning/-scoped store this aggregation was
// ported from; it exists in .atcr/debt/ (Epic 24.0) and must not be silently
// folded into Open or Resolved. It is a terminal, non-live status: counted in its
// own column and excluded from the age backlog and top-priority list.
func TestSummarizeDebt_WontfixIsItsOwnTerminalBucket(t *testing.T) {
	recs := append(debtSampleRecords(),
		mkDebtRecord("wontfix", "CRITICAL", "internal/x/y.go", 3, "2026-06-20T09:00:00Z"))

	s := summarizeDebt(recs, debtRefNow, 10)
	assert.Equal(t, 5, s.Total)
	assert.Equal(t, 2, s.Open, "a wontfix record is not open")
	assert.Equal(t, 1, s.Resolved, "a wontfix record is not resolved")
	assert.Equal(t, 1, s.Wontfix)

	var bySev debtSeverityCount
	for _, sc := range s.BySeverity {
		if sc.Severity == "CRITICAL" {
			bySev = sc
		}
	}
	assert.Equal(t, 1, bySev.Wontfix)

	for _, r := range s.Top {
		assert.NotEqual(t, "wontfix", r.Status, "a dismissed finding is not top-priority work")
	}
}

func TestSummarizeDebt_ByComponent(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)
	byComp := map[string]int{}
	for _, c := range s.ByComponent {
		byComp[c.Component] = c.Total
	}
	// Live-only counts: internal/autofix loses its resolved LOW item, cmd/atcr
	// keeps the open CRITICAL and the deferred MEDIUM.
	assert.Equal(t, 1, byComp["internal/autofix"])
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

// TD item F: By Component is the dashboard's prioritization rollup, so it must
// count the same live (open+deferred) scope as By Age and Top Priority — a
// component whose findings are all settled is not work to do and must not rank.
func TestSummarizeDebt_ByComponentCountsLiveOnly(t *testing.T) {
	recs := []localdebt.Record{
		mkDebtRecord("resolved", "HIGH", "closedonly/sub/a.go", 1, "2026-06-01T09:00:00Z"),
		mkDebtRecord("wontfix", "HIGH", "closedonly/sub/b.go", 2, "2026-06-01T09:00:00Z"),
		mkDebtRecord("", "HIGH", "mixed/sub/a.go", 1, "2026-06-01T09:00:00Z"),
		mkDebtRecord("deferred", "HIGH", "mixed/sub/b.go", 2, "2026-06-01T09:00:00Z"),
		mkDebtRecord("resolved", "HIGH", "mixed/sub/c.go", 3, "2026-06-01T09:00:00Z"),
	}
	s := summarizeDebt(recs, debtRefNow, 5)

	byComp := map[string]int{}
	for _, c := range s.ByComponent {
		byComp[c.Component] = c.Total
	}
	_, hasClosedOnly := byComp["closedonly/sub"]
	assert.False(t, hasClosedOnly, "a component holding only settled items must not appear in By Component")
	assert.Equal(t, 2, byComp["mixed/sub"], "only live (open+deferred) items count toward a component")
}

// The rendered column header names the scope so "Unresolved" cannot be misread
// as an all-statuses count.
func TestRenderDebtDashboard_ByComponentHeaderNamesTheScope(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 10)
	assert.Contains(t, out, "| Component | Unresolved |")
	assert.NotContains(t, out, "| Component | Items |")
}

func TestSummarizeDebt_ByAge_UnresolvedOnly(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)
	total := 0
	for _, b := range s.ByAge {
		total += b.Count
	}
	// Only the 3 unresolved items (open+deferred) are aged; the resolved LOW
	// item is excluded from the age backlog.
	assert.Equal(t, 3, total)
}

// TD: the only production caller (renderDebtDashboard) passes a ZERO now, so
// every live record was date-parsed and bucketed on every render and every result
// landed in "0-7d" — now.Sub(d) is hugely negative and clamped to age 0. A future
// caller reading ByAge off that call path would get a silently wrong profile.
// With no clock there is no age profile: ByAge is nil and no record is bucketed.
func TestSummarizeDebt_ZeroNowYieldsNoAgeProfile(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), time.Time{}, 5)

	assert.Nil(t, s.ByAge, "an age profile computed against a zero clock is garbage, not data")
	// The clock-independent halves are unaffected.
	assert.Equal(t, len(debtSampleRecords()), s.Total)
	assert.NotEmpty(t, s.Top)
	assert.NotEmpty(t, s.ByComponent)
}

// TD: `if sc, ok := sevIdx[...]; ok` silently dropped every record whose severity
// is not exactly CRITICAL/HIGH/MEDIUM/LOW, while those records still counted in
// Total and Open — the dashboard printed "Total: 5" over a By Severity table that
// summed to 3, with no unknown row and no warning, while `debt list` rendered
// both records normally. `debt add` validates severity but nothing else that
// writes to the store does, so the schema-skew case lands exactly here.
func TestSummarizeDebt_OffEnumSeverityIsCountedInAnUnknownRow(t *testing.T) {
	recs := append(debtSampleRecords(),
		mkDebtRecord("", "BLOCKER", "a.go", 1, "2026-06-13T09:00:00Z"),
		mkDebtRecord("", "", "b.go", 2, "2026-06-13T09:00:00Z"),
	)

	s := summarizeDebt(recs, debtRefNow, 5)

	var summed int
	for _, c := range s.BySeverity {
		summed += c.Total
	}
	assert.Equal(t, s.Total, summed, "the severity breakdown must reconcile to Total")

	var unknown *debtSeverityCount
	for i, c := range s.BySeverity {
		if c.Severity == "(unknown)" {
			unknown = &s.BySeverity[i]
		}
	}
	require.NotNil(t, unknown, "off-enum records land in an (unknown) row")
	assert.Equal(t, 2, unknown.Total)
	assert.Equal(t, "(unknown)", s.BySeverity[len(s.BySeverity)-1].Severity, "the unknown row is appended last")

	out := renderDebtDashboard(recs, 5)
	assert.Contains(t, out, "| (unknown) |", "and it is rendered, so the table reconciles on screen too")
}

// TD: the dashboard's --help claims "Secret-shaped tokens in finding text are
// scrubbed", but red.Redact was applied to exactly ONE field (Top Priority's
// Problem). The location — which carries whatever `debt add --file` was given —
// and the component labels derived from it went out verbatim, in a file that is
// routinely written to a published docs path.
func TestRenderDebtDashboard_RedactsEveryFreeTextCell(t *testing.T) {
	rec := mkDebtRecord("", "CRITICAL", "internal/keys/sk-live0000SECRET111/app.go", 4, "2026-06-13T09:00:00Z")
	rec.Problem = "leaked sk-live0000SECRET222 in the header"
	rec.StampID()

	out := renderDebtDashboard([]localdebt.Record{rec}, 5)

	assert.NotContains(t, out, "sk-live0000SECRET111",
		"the location cell (and the component rollup derived from it) is scrubbed")
	assert.NotContains(t, out, "sk-live0000SECRET222", "the problem cell stays scrubbed")
	assert.Contains(t, out, "[redacted]")
}

// TD: debtMarkdownCell collapsed only CRLF, LF and the pipe, so ESC/C0/C1 bytes
// and raw HTML passed straight through into the dashboard — which writes to the
// TERMINAL by default. Store content is model-generated by the reconcile fan-out
// rather than hand-curated, so this is a third renderer needing the same
// control-character strip debt list and resolve --list already got, plus HTML
// neutralization for the published-file path.
func TestRenderDebtDashboard_StripsControlSequencesAndNeutralizesHTML(t *testing.T) {
	rec := mkDebtRecord("", "HIGH", "internal/a.go", 3, "2026-06-13T09:00:00Z")
	rec.Problem = "erase\x1b[1Athis\x9bK and <img src=x onerror=alert(1)>"
	rec.StampID()

	out := renderDebtDashboard([]localdebt.Record{rec}, 5)

	assert.NotContains(t, out, "\x1b", "no ESC byte reaches the terminal")
	assert.NotContains(t, out, "", "no C1 control byte either")
	assert.NotContains(t, out, " ", "no line separator")
	assert.NotContains(t, out, "<img", "raw HTML is escaped rather than rendered")
	assert.Contains(t, out, "erase", "the surrounding finding text survives")
}

// The unknown row is emitted only when it is non-zero: a clean store's dashboard
// must not grow a permanent empty row.
func TestSummarizeDebt_NoUnknownRowWhenEverySeverityIsOnEnum(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)

	for _, c := range s.BySeverity {
		assert.NotEqual(t, "(unknown)", c.Severity)
	}
	assert.NotContains(t, renderDebtDashboard(debtSampleRecords(), 5), "(unknown)")
}

func TestSummarizeDebt_TopPriority_SeverityThenAge(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 5)
	require.NotEmpty(t, s.Top)
	// Only unresolved items are candidates; most-severe first.
	assert.Equal(t, "CRITICAL", s.Top[0].Severity)
	for _, r := range s.Top {
		assert.NotEqual(t, "resolved", r.Status)
	}
	assert.LessOrEqual(t, len(s.Top), 3)
}

func TestSummarizeDebt_TopPriority_RespectsLimit(t *testing.T) {
	s := summarizeDebt(debtSampleRecords(), debtRefNow, 1)
	assert.Len(t, s.Top, 1)
	assert.Equal(t, "CRITICAL", s.Top[0].Severity)
}

func TestSummarizeDebt_UnparseableTimestampIsUnknownBucket(t *testing.T) {
	recs := []localdebt.Record{mkDebtRecord("", "HIGH", "x.go", 1, "not-a-timestamp")}
	s := summarizeDebt(recs, debtRefNow, 5)
	var unknown int
	for _, b := range s.ByAge {
		if b.Label == "unknown" {
			unknown = b.Count
		}
	}
	assert.Equal(t, 1, unknown)
}

// ---------------------------------------------------------------------------
// renderDebtDashboard — ported from internal/debt's RenderDashboard.
// ---------------------------------------------------------------------------

func TestRenderDebtDashboard_Deterministic(t *testing.T) {
	recs := debtSampleRecords()
	a := renderDebtDashboard(recs, 10)
	b := renderDebtDashboard(recs, 10)
	assert.Equal(t, a, b, "dashboard render must be byte-identical across runs (no timestamp)")
}

func TestRenderDebtDashboard_HasSections(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 10)
	assert.Contains(t, out, "# Technical Debt Dashboard")
	assert.Contains(t, out, "## By Severity")
	assert.Contains(t, out, "## By Component")
	assert.Contains(t, out, "## By Age")
	assert.Contains(t, out, "## Top Priority")
	// A CRITICAL open item should surface in the severity table and top list.
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "cmd/atcr/autofix.go:248")
}

func TestRenderDebtDashboard_TotalsRow(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 10)
	// 4 total, 2 open, 1 deferred, 1 resolved.
	assert.Contains(t, out, "**Total:** 4")
	assert.Contains(t, out, "**Open:** 2")
}

func TestRenderDebtDashboard_ScrubsSecretTokens(t *testing.T) {
	rec := mkDebtRecord("", "CRITICAL", "internal/x.go", 1, "2026-06-26T09:00:00Z")
	rec.Problem = "leaked key sk-ABCDEF0123456789 in the log line"
	rec.StampID()

	out := renderDebtDashboard([]localdebt.Record{rec}, 10)
	assert.NotContains(t, out, "sk-ABCDEF0123456789")
	assert.Contains(t, out, "[redacted]")
}

func TestRenderDebtDashboard_SanitizesFileAndComponent(t *testing.T) {
	rec := mkDebtRecord("", "CRITICAL", "internal/x | y.go", 1, "2026-06-26T09:00:00Z")
	rec.Problem = "pipe in file"
	rec.StampID()

	out := renderDebtDashboard([]localdebt.Record{rec}, 10)
	// A literal pipe in the File cell would break the Markdown table column count.
	assert.NotContains(t, out, "| internal/x | y.go:1 |")
	assert.Contains(t, out, "| internal/x / y.go:1 |")
	// The By Component rollup must sanitize the component label as well.
	assert.Contains(t, out, "| internal/x / y.go | 1 |")
}

func TestRenderDebtDashboard_TopRespectsLimitAndExcludesResolved(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 1)
	// Only the single highest-priority (CRITICAL) item is listed in Top Priority.
	top := out[strings.Index(out, "## Top Priority"):]
	assert.Contains(t, top, "cmd/atcr/autofix.go:248")          // CRITICAL open
	assert.NotContains(t, top, "internal/autofix/revert.go:41") // resolved LOW, excluded
}

func TestRenderDebtDashboard_TopZeroShowsSuppressed(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 0)
	// A zero cap hides the list but should not pretend the backlog is empty.
	top := out[strings.Index(out, "## Top Priority"):]
	assert.Contains(t, top, "(top list suppressed)")
	assert.NotContains(t, top, "_No unresolved items._")
}

// AC9 (dashboard): the Top Priority table carries the id, so the view a human
// scans for what to fix next is also the view they copy the resolve argument from.
func TestRenderDebtDashboard_TopPriorityCarriesIDColumn(t *testing.T) {
	recs := debtSampleRecords()
	out := renderDebtDashboard(recs, 10)
	top := out[strings.Index(out, "## Top Priority"):]

	assert.Contains(t, top, "| ID | Severity | File | Est | Problem |")
	for _, r := range recs {
		if !debtIsLive(r) {
			continue
		}
		assert.Contains(t, top, r.ID, "each listed row carries its id")
	}
}

// Determinism is load-bearing for --check: the id is a pure content hash with no
// clock component, but pin it rather than reasoning about it.
func TestRenderDebtDashboard_IDColumnStaysDeterministic(t *testing.T) {
	recs := debtSampleRecords()
	assert.Equal(t, renderDebtDashboard(recs, 10), renderDebtDashboard(recs, 10))
}

// The id is a content hash, not free text: redacting it would break the join
// contract the dashboard exists to serve, even on a record whose problem text
// does get scrubbed.
func TestRenderDebtDashboard_IDIsNotRedacted(t *testing.T) {
	rec := mkDebtRecord("", "CRITICAL", "internal/x.go", 1, "2026-06-26T09:00:00Z")
	rec.Problem = "leaked key sk-ABCDEF0123456789 in the log line"
	rec.StampID()

	out := renderDebtDashboard([]localdebt.Record{rec}, 10)
	assert.NotContains(t, out, "sk-ABCDEF0123456789")
	assert.Contains(t, out, rec.ID, "the id passes through the redactor untouched")
}

// Matches the table renderer: a hand-edited record with no id renders "-", never
// a blank cell and never a computed fallback.
func TestRenderDebtDashboard_EmptyIDRendersDash(t *testing.T) {
	rec := mkDebtRecord("", "CRITICAL", "internal/x.go", 1, "2026-06-26T09:00:00Z")
	rec.Problem = "no id on this one"
	rec.ID = "" // only a hand-edited store can produce this

	out := renderDebtDashboard([]localdebt.Record{rec}, 10)
	top := out[strings.Index(out, "## Top Priority"):]
	assert.Contains(t, top, "| - | CRITICAL |")
}

// A zero cap still suppresses the list rather than emitting a bare header row.
func TestRenderDebtDashboard_TopZeroEmitsNoHeaderRow(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 0)
	top := out[strings.Index(out, "## Top Priority"):]
	assert.NotContains(t, top, "| ID | Severity |")
}

func TestRenderDebtDashboard_AgeByMonthIsTimeInvariant(t *testing.T) {
	out := renderDebtDashboard(debtSampleRecords(), 10)
	age := out[strings.Index(out, "## By Age"):]
	// Unresolved items are timestamped 2026-06 (HIGH, CRITICAL, MEDIUM).
	require.Contains(t, age, "2026-06")
}

func TestDebtMonthHistogram_MalformedTimestampGoesToUnknown(t *testing.T) {
	got := debtMonthHistogram([]localdebt.Record{mkDebtRecord("", "HIGH", "x.go", 1, "2026-7x")})
	require.Len(t, got, 1)
	assert.Equal(t, "unknown", got[0].month)
}
