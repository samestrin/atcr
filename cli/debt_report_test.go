package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// reportREADME has two dated sections, written oldest first, so a test can see
// the report order them by date rather than by the order they were imported.
const reportREADME = `# Technical Debt Tracking

### [2026-09-01] From: epic 10

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |
|-------|---|----------|------|---------|-----|----------|-------------|--------|----------|
| 1 | [x] | LOW | a/old.go:3 | old resolved problem | old fix | docs | 5 | me | 1 |
| 1 | [ ] | HIGH | a/old.go:9 | old open problem with a \| pipe | fix it | correctness | 10 | me | 0 |

### [2026-10-28] From: epic 85.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |
|-------|---|----------|------|---------|-----|----------|-------------|--------|----------|
| 1 | [/] | MEDIUM | b/new.go:12 | new deferred problem | defer it | design | 20 | me | 2 |
| 1 | [-] | CRITICAL | b/new.go | new unreproducible <b>problem</b> | none | security | 30 | me | 0 |
`

// writeReportStore imports reportREADME into a fresh store and adds one record
// that came from no README (a reconcile finding), returning the store dir.
func writeReportStore(t *testing.T) string {
	t.Helper()
	readme := filepath.Join(t.TempDir(), "README.md")
	require.NoError(t, os.WriteFile(readme, []byte(reportREADME), 0o644))
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")
	_, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err)

	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-11-01-run1",
		Timestamp:     "2026-11-01T00:00:00Z",
		Severity:      "LOW",
		File:          "c/reconciled.go",
		Line:          4,
		Problem:       "found by a review",
		Fix:           "tidy",
		Category:      "maintainability",
		EstMinutes:    5,
	}
	rec.StampID()
	require.NoError(t, localdebt.Append(dir, rec))
	return dir
}

func TestDebtReport_Wiring(t *testing.T) {
	sub := debtSubcommand(t, newDebtCmd(), "report")
	for _, name := range []string{"format", "output", "check", "store"} {
		assert.NotNil(t, sub.Flags().Lookup(name), "report registers --%s", name)
	}
	assert.Equal(t, "md", sub.Flags().Lookup("format").DefValue)
	assert.Equal(t, "", sub.Flags().Lookup("output").DefValue, "an empty --output means stdout")
}

func TestDebtReport_GroupsBySectionNewestFirst(t *testing.T) {
	dir := writeReportStore(t)

	got, err := runDebt(t, "report", "--format", "md", "--store", dir)
	require.NoError(t, err)

	lines := strings.Split(got, "\n")
	require.GreaterOrEqual(t, len(lines), 3)
	assert.Equal(t, "# Technical Debt Report", lines[0])
	assert.Equal(t, debtReportGeneratedMarker, lines[1], "the generated marker is the report's literal second line")
	assert.Equal(t, "", lines[2], "a blank line after the marker keeps it a paragraph of its own")

	newer := strings.Index(got, "## [2026-10-28] From: epic 85.0")
	older := strings.Index(got, "## [2026-09-01] From: epic 10")
	loose := strings.Index(got, "## Not from a README section")
	require.True(t, newer > 0 && older > 0 && loose > 0, "every group has a heading:\n%s", got)
	assert.Less(t, newer, older, "the newest section comes first")
	assert.Less(t, older, loose, "records from no README section come last")

	// Each row lands under its own section, most severe first.
	section := func(from, to int) string { return got[from:to] }
	newSec, oldSec, looseSec := section(newer, older), section(older, loose), got[loose:]
	assert.Contains(t, newSec, "new deferred problem")
	assert.Contains(t, newSec, "| deferred | MEDIUM | b/new.go:12 |")
	assert.Contains(t, newSec, "| unreproducible | CRITICAL | b/new.go |", "a File without a line renders bare")
	assert.Less(t, strings.Index(newSec, "CRITICAL"), strings.Index(newSec, "MEDIUM"))
	assert.Contains(t, oldSec, "| resolved | LOW | a/old.go:3 |")
	assert.Contains(t, oldSec, "| open | HIGH | a/old.go:9 |")
	assert.Less(t, strings.Index(oldSec, "HIGH"), strings.Index(oldSec, "LOW"))
	assert.Contains(t, looseSec, "| open | LOW | c/reconciled.go:4 |")

	// Cells are Markdown-safe: a pipe is escaped rather than rewritten, so it
	// cannot add a column and still reads as a pipe; HTML is escaped.
	assert.Contains(t, oldSec, `old open problem with a \| pipe`)
	assert.Contains(t, newSec, "&lt;b&gt;problem&lt;/b&gt;")
	assert.NotContains(t, got, "<b>")

	// The reporting seat and Attempts are reported, in the README's column order.
	assert.Contains(t, got, "| ID | Status | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |")
	assert.Contains(t, newSec, "| design | 20 | me | 2 |")
	assert.Contains(t, looseSec, "| maintainability | 5 |  | 0 |", "a record with no reporter has an empty Source cell")
}

func TestDebtReport_RerunIsByteIdenticalAndCheckPasses(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "docs", "DEBT.md")

	msg, err := runDebt(t, "report", "--store", dir, "--output", out)
	require.NoError(t, err)
	assert.Empty(t, msg, "--output writes nothing to stdout")
	first, err := os.ReadFile(out)
	require.NoError(t, err)

	_, err = runDebt(t, "report", "--store", dir, "--output", out)
	require.NoError(t, err, "a file the report generated is overwritten")
	second, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "a rerun is byte-identical")

	msg, err = runDebt(t, "report", "--store", dir, "--output", out, "--check")
	require.NoError(t, err)
	assert.Contains(t, msg, "is up to date")
}

func TestDebtReport_CheckFailsOnDrift(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "DEBT.md")
	_, err := runDebt(t, "report", "--store", dir, "--output", out)
	require.NoError(t, err)

	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-11-02-run2", Timestamp: "2026-11-02T00:00:00Z",
		Severity: "HIGH", File: "d/later.go", Line: 1, Problem: "arrived after the report", Category: "correctness",
	}
	rec.StampID()
	require.NoError(t, localdebt.Append(dir, rec))

	_, err = runDebt(t, "report", "--store", dir, "--output", out, "--check")
	require.Error(t, err)
	var coded *codedError
	require.True(t, errors.As(err, &coded))
	assert.Equal(t, exitDrift, coded.code)
	assert.Contains(t, err.Error(), "atcr debt report --output")
}

func TestDebtReport_CheckMissingFileIsDrift(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "DEBT.md")
	_, err := runDebt(t, "report", "--store", dir, "--output", out, "--check")
	var coded *codedError
	require.True(t, errors.As(err, &coded), "got %v", err)
	assert.Equal(t, exitDrift, coded.code)
}

func TestDebtReport_RefusesToOverwriteUngeneratedFile(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "NOTES.md")
	const hand = "# My notes\n\nWritten by hand.\n"
	require.NoError(t, os.WriteFile(out, []byte(hand), 0o644))

	_, err := runDebt(t, "report", "--store", dir, "--output", out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")
	assert.Contains(t, err.Error(), "atcr debt report")
	got, rerr := os.ReadFile(out)
	require.NoError(t, rerr)
	assert.Equal(t, hand, string(got), "the file is left untouched")
}

// A dashboard file carries the dashboard's marker, not the report's, so the
// report does not claim it either.
func TestDebtReport_RefusesToOverwriteDashboard(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")
	_, err := runDebt(t, "dashboard", "--store", dir, "--output", out)
	require.NoError(t, err)

	_, err = runDebt(t, "report", "--store", dir, "--output", out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")
}

func TestDebtReport_UsageErrors(t *testing.T) {
	dir := writeReportStore(t)
	for name, args := range map[string][]string{
		"unknown format":        {"report", "--store", dir, "--format", "json"},
		"check without output":  {"report", "--store", dir, "--check"},
		"output in system dir":  {"report", "--store", dir, "--output", "/etc/atcr-debt-report.md"},
		"positional args given": {"report", "--store", dir, "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runDebt(t, args...)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err), "got %v", err)
		})
	}
}

func TestDebtReport_EmptyStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")
	got, err := runDebt(t, "report", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, debtReportGeneratedMarker, strings.Split(got, "\n")[1], "an empty report has the marker on line 2 too")
	assert.Contains(t, got, "_No items._")
}

// Within a section, rows of one severity order by file, then line, then id,
// so the render never depends on the order the store returned them in.
func TestDebtReport_OrdersRowsBySeverityFileLineID(t *testing.T) {
	sec := &localdebt.SourceReport{Path: "README.md", Section: "### [2026-10-01] From: epic 1"}
	mk := func(id, sev, file string, line int) localdebt.Record {
		return localdebt.Record{ID: id, Severity: sev, File: file, Line: line, Problem: id, SourceReport: sec}
	}
	recs := []localdebt.Record{
		mk("z1", "LOW", "b.go", 1),
		mk("y2", "LOW", "a.go", 9),
		mk("x3", "LOW", "a.go", 2),
		mk("b4", "LOW", "a.go", 2),
		mk("a5", "HIGH", "z.go", 1),
	}
	want := renderDebtReport(recs)
	var order []string
	for _, line := range strings.Split(want, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| ID") {
			order = append(order, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	assert.Equal(t, []string{"a5", "b4", "x3", "y2", "z1"}, order)

	reversed := make([]localdebt.Record, len(recs))
	for i, r := range recs {
		reversed[len(recs)-1-i] = r
	}
	assert.Equal(t, want, renderDebtReport(reversed), "input order does not change the bytes")
}

// A pipe in item text is written as `\|`, the escape `debt import` reads back,
// so the report's text is the store's text: a row with `a|b` in it, read by the
// import's own table parser, yields `a|b` again rather than `a/b`.
func TestDebtReport_PipeInTextRoundTripsThroughImportParser(t *testing.T) {
	sec := &localdebt.SourceReport{Path: "README.md", Section: "### [2026-10-01] From: epic 1"}
	rec := localdebt.Record{
		ID: "p1", Severity: "HIGH", File: "a|b.go", Line: 3, Problem: "O_NONBLOCK|O_NOFOLLOW",
		Fix: "go build ./... | tee build.log", Category: "a|b", EstMinutes: 5, SourceReport: sec,
	}
	got := renderDebtReport([]localdebt.Record{rec})
	assert.Contains(t, got, `| O_NONBLOCK\|O_NOFOLLOW | go build ./... \| tee build.log |`)

	// Give the report's table the checkbox column an item table has, and parse it
	// as `debt import` does.
	var table []string
	for _, line := range strings.Split(got, "\n") {
		switch {
		case strings.HasPrefix(line, "| ID "):
			table = append(table, "| |"+strings.TrimPrefix(line, "|"))
		case strings.HasPrefix(line, "|----"):
			table = append(table, "|---"+line)
		case strings.HasPrefix(line, "| p1 "):
			table = append(table, "| [ ] "+line)
		}
	}
	require.Len(t, table, 3, "the report has a header, a separator and one row:\n%s", got)
	items, err := localdebt.ParseTDItems(strings.NewReader(strings.Join(table, "\n") + "\n"))
	require.NoError(t, err)
	require.Len(t, items, 1)
	it := items[0]
	assert.Empty(t, it.Invalid, "the escaped pipes add no column")
	assert.Equal(t, "O_NONBLOCK|O_NOFOLLOW", it.Problem)
	assert.Equal(t, "go build ./... | tee build.log", it.Fix)
	assert.Equal(t, "a|b", it.Category)
	assert.Equal(t, "a|b.go", it.File)
	assert.Equal(t, 3, it.FileLine)
}

// A report written before the marker moved to line 2 (title, blank line, marker)
// still carries the marker in its head, so --output recognises it as generated
// and replaces it with the new layout rather than refusing.
func TestDebtReport_OverwritesReportWithTheOldHeaderLayout(t *testing.T) {
	dir := writeReportStore(t)
	out := filepath.Join(t.TempDir(), "DEBT.md")
	old := "# Technical Debt Report\n\n" + debtReportGeneratedMarker + "\n\n_No items._\n"
	require.NoError(t, os.WriteFile(out, []byte(old), 0o644))

	_, err := runDebt(t, "report", "--store", dir, "--output", out, "--check")
	require.Error(t, err, "the old layout is out of date")
	assert.Equal(t, exitDrift, exitCode(err))

	_, err = runDebt(t, "report", "--store", dir, "--output", out)
	require.NoError(t, err, "a report in the old layout is still the report's own file")
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, debtReportGeneratedMarker, strings.Split(string(got), "\n")[1])
}

// A store in which no record names a reporter (every store imported before
// reported_by existed) renders without a Source column, byte for byte as it did
// before, so `report --check` keeps passing on an already-generated README.
func TestDebtReport_NoReporterKeepsTheLayoutWithoutSource(t *testing.T) {
	sec := &localdebt.SourceReport{Path: "README.md", Section: "### [2026-10-01] From: epic 1"}
	rec := localdebt.Record{
		ID: "p1", Severity: "HIGH", File: "a.go", Line: 3, Problem: "p",
		Fix: "f", Category: "bug", EstMinutes: 5, Attempts: 1, SourceReport: sec,
	}
	want := "# Technical Debt Report\n" + debtReportGeneratedMarker + "\n\n" +
		"**Items:** 1 in 1 sections\n\n## [2026-10-01] From: epic 1\n\n" +
		"| ID | Status | Severity | File | Problem | Fix | Category | Est Minutes | Attempts |\n" +
		"|----|--------|----------|------|---------|-----|----------|-------------|----------|\n" +
		"| p1 | open | HIGH | a.go:3 | p | f | bug | 5 | 1 |\n"
	assert.Equal(t, want, renderDebtReport([]localdebt.Record{rec}))
}

// The Source cell a report writes reads back as the row's Source, so a README
// regenerated from the store and imported again keeps the reporting seat.
func TestDebtReport_SourceRoundTripsThroughImportParser(t *testing.T) {
	sec := &localdebt.SourceReport{Path: "README.md", Section: "### [2026-10-01] From: epic 1"}
	rec := localdebt.Record{
		ID: "p1", Severity: "HIGH", File: "a.go", Line: 3, Problem: "p",
		Fix: "f", Category: "bug", EstMinutes: 5, Attempts: 2, ReportedBy: "paperclip-cto", SourceReport: sec,
	}
	got := renderDebtReport([]localdebt.Record{rec})
	var table []string
	for _, line := range strings.Split(got, "\n") {
		switch {
		case strings.HasPrefix(line, "| ID "):
			table = append(table, "| |"+strings.TrimPrefix(line, "|"))
		case strings.HasPrefix(line, "|----"):
			table = append(table, "|---"+line)
		case strings.HasPrefix(line, "| p1 "):
			table = append(table, "| [ ] "+line)
		}
	}
	require.Len(t, table, 3, got)
	items, err := localdebt.ParseTDItems(strings.NewReader(strings.Join(table, "\n") + "\n"))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Invalid)
	assert.Equal(t, "paperclip-cto", items[0].Source)
	assert.Equal(t, 2, items[0].Attempts)
}
