package cli

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtCleanFixture is the store twin of testdata/debt_clean/fixture_README.md:
// the same ten rows, each checkbox stored as the status TDMarkerStatus maps it
// to. golden_clean.json and golden_cleaned_README.md were captured by running
// `llm-support td-clean --json --today 2026-11-02` (v1.5.0) over that README, so
// these tests assert the kept items and the counts without the original binary.
var debtCleanFixture = []struct {
	status, sev, file string
	line              int
}{
	{"", "HIGH", "a.go", 10},
	{localdebt.StatusResolved, "CRITICAL", "c.go", 5},
	{localdebt.StatusDeferred, "LOW", "b.go", 20},
	{localdebt.StatusUnreproducible, "MEDIUM", "d.go", 7},
	{localdebt.StatusResolved, "HIGH", "e.go", 1},
	{localdebt.StatusResolved, "LOW", "f.go", 2},
	{"", "CRITICAL", "g.go", 3},
	{"", "MEDIUM", "h.go", 4},
}

func debtCleanRecord(status, sev, file string, line int) localdebt.Record {
	r := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
		Status: status, Severity: sev, File: file, Line: line, EstMinutes: 10,
		Confidence: "high", Problem: "p", Fix: "f", Category: "cat",
	}
	r.StampID()
	return r
}

func debtCleanStore(t *testing.T, extra ...localdebt.Record) string {
	t.Helper()
	var recs []localdebt.Record
	for _, f := range debtCleanFixture {
		recs = append(recs, debtCleanRecord(f.status, f.sev, f.file, f.line))
	}
	return writeLocalDebt(t, append(recs, extra...)...)
}

// tdCleanGolden is td-clean's --json document as captured.
type tdCleanGolden struct {
	RemovedRows     int `json:"removed_rows"`
	RemovedSections int `json:"removed_sections"`
	Open            int `json:"open"`
	Deferred        int `json:"deferred"`
	Resolved        int `json:"resolved"`
	Unreproducible  int `json:"unreproducible"`
	Total           int `json:"total"`
}

// readCleanedREADME pulls the two things td-clean wrote into the README: the
// File cell of every kept item row, and the regenerated Stats table.
func readCleanedREADME(t *testing.T) (files []string, stats []debtCleanSeverityRow) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "debt_clean", "golden_cleaned_README.md"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	inStats := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "## Stats":
			inStats = true
			continue
		case strings.HasPrefix(line, "#"):
			inStats = false
			continue
		case !strings.HasPrefix(line, "|"):
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if inStats {
			if cells[0] == "Severity" || strings.HasPrefix(cells[0], "-") {
				continue
			}
			n := func(i int) int {
				v, err := strconv.Atoi(cells[i])
				require.NoError(t, err)
				return v
			}
			stats = append(stats, debtCleanSeverityRow{Severity: cells[0], Open: n(1), Deferred: n(2), Resolved: n(3), Unreproducible: n(4)})
			continue
		}
		if len(cells) > 3 && strings.HasPrefix(cells[1], "[") {
			files = append(files, strings.Split(cells[3], ":")[0])
		}
	}
	require.NoError(t, sc.Err())
	return files, stats
}

func runDebtCleanJSON(t *testing.T, dir string) debtCleanDoc {
	t.Helper()
	out, err := runDebt(t, "clean", "--dir", dir)
	require.NoError(t, err)
	var got debtCleanDoc
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	return got
}

func TestDebtClean_GoldenMatchesTdClean(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "debt_clean", "golden_clean.json"))
	require.NoError(t, err)
	var want tdCleanGolden
	require.NoError(t, json.Unmarshal(raw, &want))
	wantFiles, wantStats := readCleanedREADME(t)

	got := runDebtCleanJSON(t, debtCleanStore(t))
	s := got.Summary

	assert.Equal(t, want.Open, s.Open)
	assert.Equal(t, want.Deferred, s.Deferred)
	assert.Equal(t, want.Resolved, s.Resolved)
	assert.Equal(t, want.Unreproducible, s.Unreproducible)
	assert.Equal(t, want.Total, s.Total)
	// Intended difference: removed_rows is excluded_resolved, since nothing is
	// removed; removed_sections has no store equivalent (no sections) and is gone.
	assert.Equal(t, want.RemovedRows, s.ExcludedResolved)
	assert.Equal(t, wantStats, s.BySeverity, "the regenerated Stats table")

	// Intended difference: td-clean keeps README order; the store has none, so the
	// view is most severe first. The set of kept items is the same.
	gotFiles := filesOf(got.Items)
	assert.Equal(t, []string{"g.go", "a.go", "d.go", "h.go", "b.go"}, gotFiles)
	sort.Strings(wantFiles)
	sort.Strings(gotFiles)
	assert.Equal(t, wantFiles, gotFiles)
	assert.Len(t, got.Items, s.Total)
}

// snapshotStore maps every file under dir to its bytes.
func snapshotStore(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		snap[p] = string(b)
		return err
	}))
	return snap
}

func TestDebtClean_NeverWritesTheStore(t *testing.T) {
	dir := debtCleanStore(t)
	before := snapshotStore(t, dir)
	require.NotEmpty(t, before)

	_ = runDebtCleanJSON(t, dir)
	code, _, _ := execCmdSplit(t, "debt", "clean", "--dir", dir, "--axi")
	require.Equal(t, 0, code)

	assert.Equal(t, before, snapshotStore(t, dir), "clean is a view: no store file is created, deleted or rewritten")
	// And the resolved items are still in the store for every other command.
	again := runDebtCleanJSON(t, dir)
	assert.Equal(t, 3, again.Summary.ExcludedResolved)
}

func TestDebtClean_StoreOnlyStatuses(t *testing.T) {
	// wontfix and attempts-exhausted have no README marker, so td-clean never sees
	// them. The view keeps the three statuses the task names and counts the rest
	// as excluded, so nothing leaves the view uncounted.
	dir := debtCleanStore(t,
		debtCleanRecord(localdebt.StatusWontfix, "HIGH", "w.go", 1),
		debtCleanRecord(localdebt.StatusAttemptsExhausted, "LOW", "x.go", 1),
		debtCleanRecord("", "high", "y.go", 1),
		debtCleanRecord("", "BOGUS", "z.go", 1),
	)
	got := runDebtCleanJSON(t, dir)
	s := got.Summary
	assert.Equal(t, 1, s.ExcludedWontfix)
	assert.Equal(t, 1, s.ExcludedAttemptsExhausted)
	assert.Equal(t, 5, s.Open)
	assert.Equal(t, 7, s.Total)
	assert.NotContains(t, filesOf(got.Items), "w.go")
	assert.NotContains(t, filesOf(got.Items), "x.go")

	// Severity folds to upper case, as td-clean's Stats table does; a severity
	// outside the four follows them, as td-clean appends it.
	require.Len(t, s.BySeverity, 5)
	assert.Equal(t, debtCleanSeverityRow{Severity: "HIGH", Open: 2}, s.BySeverity[1])
	assert.Equal(t, debtCleanSeverityRow{Severity: "BOGUS", Open: 1}, s.BySeverity[4])
}

func TestDebtClean_EmptyStore(t *testing.T) {
	got := runDebtCleanJSON(t, t.TempDir())
	assert.Empty(t, got.Items)
	assert.Equal(t, 0, got.Summary.Total)
	require.Len(t, got.Summary.BySeverity, 4, "the four severities always appear, as in td-clean's Stats table")

	out, err := runDebt(t, "clean", "--dir", t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, out, `"items": []`)
}

func TestDebtClean_AXI(t *testing.T) {
	dir := debtCleanStore(t)
	code, stdout, _ := execCmdSplit(t, "debt", "clean", "--dir", dir, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "items[5]{id,severity,confidence,status,file,line,category,est_minutes,problem,fix}:")
	assert.Contains(t, stdout, "excluded_resolved: 3")
	assert.Contains(t, stdout, "by_severity[4]{severity,open,deferred,resolved,unreproducible}:")
}

func TestDebtClean_UsageErrors(t *testing.T) {
	dir := debtCleanStore(t)
	_, err := runDebt(t, "clean", "--dir", dir, "extra")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))

	code, _, _ := execCmdSplit(t, "debt", "clean", "--dir", dir, "--axi", "--legacy-pipe")
	assert.Equal(t, 2, code)
}
