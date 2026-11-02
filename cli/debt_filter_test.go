package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtFilterFixture is the store twin of testdata/debt_filter/fixture_README.md:
// the same eight rows, with the [x] row stored as resolved. The golden_*.json
// files were captured by running `llm-support td-filter` over that README, so
// these tests assert the selection and the counts without the original binary.
var debtFilterFixture = []struct {
	status, sev, file string
	line, est         int
	conf              string
}{
	{"", "HIGH", "a.go", 10, 10, "high"},
	{"", "LOW", "b.go", 20, 45, "low"},
	{localdebt.StatusResolved, "CRITICAL", "c.go", 5, 5, "high"},
	{"", "MEDIUM", "d.go", 7, 3000, "medium"},
	{"", "CRITICAL", "e.go", 1, 29, "high"},
	{"", "HIGH", "f.go", 2, 30, "medium"},
	{"", "LOW", "g.go", 3, 2879, "low"},
	{"", "MEDIUM", "h.go", 4, 2880, "high"},
}

func debtFilterStore(t *testing.T) string {
	t.Helper()
	var recs []localdebt.Record
	for _, f := range debtFilterFixture {
		r := localdebt.Record{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
			Status: f.status, Severity: f.sev, File: f.file, Line: f.line, EstMinutes: f.est,
			Confidence: f.conf, Problem: "p", Fix: "f", Category: "cat",
		}
		r.StampID()
		recs = append(recs, r)
	}
	return writeLocalDebt(t, recs...)
}

type debtFilterGolden struct {
	Files   []string          `json:"files"`
	Summary debtFilterSummary `json:"summary"`
}

func TestDebtFilter_GoldenMatchesTdFilter(t *testing.T) {
	dir := debtFilterStore(t)
	cases := map[string][]string{
		"quick":    {"--mode", "quick-wins", "--max", "100"},
		"backlog":  {"--mode", "backlog", "--max", "100"},
		"all":      {"--mode", "all", "--max", "100"},
		"sev":      {"--mode", "all", "--max", "100", "--severity", "high,critical"},
		"conf":     {"--mode", "all", "--max", "100", "--confidence", "high,medium"},
		"both":     {"--mode", "quick-wins", "--max", "100", "--severity", "high,critical,low", "--confidence", "high"},
		"combined": {"--mode", "all", "--max", "100", "--severity", "high,critical,low", "--confidence", "high"},
		"capped":   {"--mode", "all", "--max", "2"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "debt_filter", "golden_"+name+".json"))
			require.NoError(t, err)
			var want debtFilterGolden
			require.NoError(t, json.Unmarshal(raw, &want))

			out, err := runDebt(t, append([]string{"filter", "--dir", dir}, args...)...)
			require.NoError(t, err)
			var got debtFilterDoc
			require.NoError(t, json.Unmarshal([]byte(out), &got))

			assert.Equal(t, want.Summary, got.Summary)
			if name == "capped" {
				// Intended difference: the original keeps the first N in source
				// order, the store has no source order, so the cap keeps the N most
				// severe (a.go HIGH is the older of the two HIGHs, e.go CRITICAL first).
				assert.Equal(t, []string{"e.go", "a.go"}, filesOf(got.Items))
				return
			}
			var wantFiles []string
			for _, f := range want.Files {
				wantFiles = append(wantFiles, strings.Split(f, ":")[0])
			}
			gotFiles := filesOf(got.Items)
			sort.Strings(wantFiles)
			sort.Strings(gotFiles)
			assert.Equal(t, wantFiles, gotFiles)
		})
	}
}

func filesOf(recs []localdebt.Record) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.File)
	}
	return out
}

func TestDebtFilter_DefaultsAndStatus(t *testing.T) {
	dir := debtFilterStore(t)
	out, err := runDebt(t, "filter", "--dir", dir)
	require.NoError(t, err)
	var got debtFilterDoc
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, 10, got.Summary.Max)
	assert.Equal(t, "quick-wins", got.Summary.Mode)
	assert.Equal(t, []string{"e.go", "a.go"}, filesOf(got.Items), "default --status open, most severe first")

	out, err = runDebt(t, "filter", "--dir", dir, "--status", "resolved", "--mode", "all")
	require.NoError(t, err)
	got = debtFilterDoc{}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, []string{"c.go"}, filesOf(got.Items))
	assert.Equal(t, 1, got.Summary.Total)
}

func TestDebtFilter_EmptyStoreEmitsEmptyItems(t *testing.T) {
	out, err := runDebt(t, "filter", "--dir", t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, out, `"items": []`)
}

func TestDebtFilter_UsageErrors(t *testing.T) {
	dir := debtFilterStore(t)
	for _, args := range [][]string{
		{"--mode", "bogus"}, {"--severity", "nope"}, {"--confidence", "maybe"},
		{"--status", "nope"}, {"--max", "-1"},
	} {
		_, err := runDebt(t, append([]string{"filter", "--dir", dir}, args...)...)
		require.Error(t, err, strings.Join(args, " "))
		assert.Equal(t, exitUsage, exitCode(err), strings.Join(args, " "))
	}
}

func TestDebtFilter_AXI(t *testing.T) {
	dir := debtFilterStore(t)
	cmd := newDebtCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"filter", "--dir", dir, "--mode", "all", "--max", "3"})
	require.NoError(t, cmd.ExecuteContext(newAXIContext(context.Background(), true)))
	out := buf.String()
	assert.Contains(t, out, "items[3]{id,severity,confidence,status,file,line,category,est_minutes,problem,fix}:")
	assert.Contains(t, out, "matched: 7")
}
