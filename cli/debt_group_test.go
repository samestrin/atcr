package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// tdGroupItem is one row of testdata/debt_group/fixture_items.json, in the
// shape `llm-support group-td` reads. The golden_*.json files were captured by
// running `llm-support group-td --file fixture_items.json --json
// --validate-lines=false` (v1.5.0) with each case's flags over that file. The
// rows are in file:line order, the order debt group reads the store in, so the
// original's input order and ours agree and the goldens compare item for item.
type tdGroupItem struct {
	Severity   string `json:"SEVERITY"`
	FileLine   string `json:"FILE_LINE"`
	Problem    string `json:"PROBLEM"`
	Fix        string `json:"FIX"`
	Category   string `json:"CATEGORY"`
	EstMinutes string `json:"EST_MINUTES"`
}

type tdGroupGolden struct {
	Groups []struct {
		Theme        string        `json:"theme"`
		PathPattern  string        `json:"path_pattern"`
		Items        []tdGroupItem `json:"items"`
		Count        int           `json:"count"`
		TotalMinutes int           `json:"total_minutes"`
		Frontend     bool          `json:"frontend"`
	} `json:"groups"`
	Ungrouped []tdGroupItem      `json:"ungrouped"`
	Summary   debtGroupSummary `json:"summary"`
}

func readDebtGroupJSON(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "debt_group", name))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, v))
}

func debtGroupRecord(status string, it tdGroupItem) localdebt.Record {
	file, line := it.FileLine, 0
	if i := strings.LastIndex(file, ":"); i > 0 {
		line, _ = strconv.Atoi(file[i+1:])
		file = file[:i]
	}
	est, _ := strconv.Atoi(it.EstMinutes)
	r := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
		Status: status, Severity: it.Severity, File: file, Line: line, EstMinutes: est,
		Confidence: "high", Problem: it.Problem, Fix: it.Fix, Category: it.Category,
	}
	r.StampID()
	return r
}

// debtGroupStore is the store twin of fixture_items.json, plus one resolved
// CRITICAL item group-td never saw: debt group reads the open items only, so it
// must not reach the critical group.
func debtGroupStore(t *testing.T) string {
	t.Helper()
	var items []tdGroupItem
	readDebtGroupJSON(t, "fixture_items.json", &items)
	recs := []localdebt.Record{debtGroupRecord(localdebt.StatusResolved, tdGroupItem{
		Severity: "CRITICAL", FileLine: "internal/db/old.go:1", Problem: "resolved", Fix: "f", Category: "security", EstMinutes: "99",
	})}
	for _, it := range items {
		recs = append(recs, debtGroupRecord("", it))
	}
	return writeLocalDebt(t, recs...)
}

func runDebtGroupJSON(t *testing.T, dir string, args ...string) debtGroupDoc {
	t.Helper()
	out, err := runDebt(t, append([]string{"group", "--dir", dir}, args...)...)
	require.NoError(t, err)
	var got debtGroupDoc
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	return got
}

func problemsOfItems(items []tdGroupItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Problem)
	}
	return out
}

func problemsOf(recs []localdebt.Record) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.Problem)
	}
	return out
}

func TestDebtGroup_GoldenMatchesGroupTd(t *testing.T) {
	dir := debtGroupStore(t)
	cases := map[string][]string{
		"path":             nil,
		"path_depth3_min2": {"--path-depth", "3", "--min-group-size", "2"},
		"path_depth1":      {"--path-depth", "1"},
		"category":         {"--group-by", "category"},
		"file_min2":        {"--group-by", "file", "--min-group-size", "2"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var want tdGroupGolden
			readDebtGroupJSON(t, "golden_"+name+".json", &want)
			got := runDebtGroupJSON(t, dir, args...)

			assert.Equal(t, want.Summary, got.Summary)
			require.Len(t, got.Groups, len(want.Groups))
			for i, wg := range want.Groups {
				g := got.Groups[i]
				assert.Equal(t, wg.Theme, g.Theme, "group %d", i)
				assert.Equal(t, wg.PathPattern, g.PathPattern, "group %s", wg.Theme)
				assert.Equal(t, wg.Count, g.Count, "group %s", wg.Theme)
				assert.Equal(t, wg.TotalMinutes, g.TotalMinutes, "group %s", wg.Theme)
				assert.Equal(t, wg.Frontend, g.Frontend, "group %s", wg.Theme)
				assert.Equal(t, problemsOfItems(wg.Items), problemsOf(g.Items), "group %s items", wg.Theme)
			}
			assert.Equal(t, problemsOfItems(want.Ungrouped), problemsOf(got.Ungrouped), "ungrouped")
		})
	}
}

func TestDebtGroup_NeverWritesTheStore(t *testing.T) {
	dir := debtGroupStore(t)
	before := snapshotStore(t, dir)
	require.NotEmpty(t, before)

	got := runDebtGroupJSON(t, dir)
	code, _, _ := execCmdSplit(t, "debt", "group", "--dir", dir, "--axi")
	require.Equal(t, 0, code)
	assert.Equal(t, before, snapshotStore(t, dir), "group is read-only: no store file is created, deleted or rewritten")

	// No stored group field: the items are the store records as they are, with
	// no group label injected (group-td's --assign-numbers is not ported).
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(string(raw)), `"group":`)
}

func TestDebtGroup_CriticalOverrideAndCase(t *testing.T) {
	// A lower-case critical still goes to the critical group, as group-td upper-
	// cases SEVERITY before comparing; a single CRITICAL forms a group below
	// --min-group-size.
	dir := writeLocalDebt(t,
		debtGroupRecord("", tdGroupItem{Severity: "critical", FileLine: "a/b/c.go:1", Problem: "c1", Category: "x", EstMinutes: "7"}),
		debtGroupRecord("", tdGroupItem{Severity: "LOW", FileLine: "a/b/d.go:1", Problem: "l1", Category: "x", EstMinutes: "3"}),
	)
	got := runDebtGroupJSON(t, dir)
	require.Len(t, got.Groups, 1)
	assert.Equal(t, "critical", got.Groups[0].Theme)
	assert.Equal(t, "CRITICAL severity items", got.Groups[0].PathPattern)
	assert.Equal(t, 7, got.Groups[0].TotalMinutes)
	assert.Equal(t, []string{"l1"}, problemsOf(got.Ungrouped))
}

func TestDebtGroup_EmptyStore(t *testing.T) {
	out, err := runDebt(t, "group", "--dir", t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, out, `"groups": []`)
	assert.Contains(t, out, `"ungrouped": []`)

	got := runDebtGroupJSON(t, t.TempDir())
	assert.Equal(t, debtGroupSummary{}, got.Summary)
}

func TestDebtGroup_AXI(t *testing.T) {
	dir := debtGroupStore(t)
	code, stdout, _ := execCmdSplit(t, "debt", "group", "--dir", dir, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "groups[4]:")
	assert.Contains(t, stdout, "theme: internal-api")
	assert.Contains(t, stdout, "path_pattern: internal/api/*")
	assert.Contains(t, stdout, "items[4]{id,severity,confidence,status,file,line,category,est_minutes,problem,fix}:")
	assert.Contains(t, stdout, "ungrouped[3]{id,severity,confidence,status,file,line,category,est_minutes,problem,fix}:")
	assert.Contains(t, stdout, "group_count: 4")
}

func TestDebtGroup_UsageErrors(t *testing.T) {
	dir := debtGroupStore(t)
	for _, args := range [][]string{
		{"extra"},
		{"--group-by", "severity"},
		{"--path-depth", "0"},
		{"--min-group-size", "0"},
	} {
		_, err := runDebt(t, append([]string{"group", "--dir", dir}, args...)...)
		require.Error(t, err, "%v", args)
		assert.Equal(t, exitUsage, exitCode(err), "%v", args)
	}

	code, _, _ := execCmdSplit(t, "debt", "group", "--dir", dir, "--axi", "--legacy-pipe")
	assert.Equal(t, 2, code)
}

func TestDebtGroupFrontend(t *testing.T) {
	// The three OR'd rules of group-td's detectFrontendItem.
	cases := []struct {
		file, category string
		want           bool
	}{
		{"web/App.TSX", "logic", true},
		{"app/page.py", "UX polish", true},
		{"app/page.py", "logic", false},
		{"svc/x.go", "Responsive layout", true},
		{"svc/x.go", "security", false},
		{"", "", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, debtGroupFrontend(localdebt.Record{File: c.file, Category: c.category}), "%+v", c)
	}
}
