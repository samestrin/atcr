package debt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

// sampleShards is a small, deterministic corpus spanning severities, statuses,
// components, groups, and two dates so filter/sort/aggregate tests share it.
func sampleShards() []tdmigrate.Shard {
	return []tdmigrate.Shard{
		{
			Date: "2026-06-13", SourceType: tdmigrate.SourceTypeSprint, Label: "old-sprint",
			Items: []tdmigrate.Item{
				{Group: "1", Status: "open", Severity: "HIGH", File: "internal/autofix/apply.go:108",
					Problem: "clobber", Fix: "stat first", Category: "correctness", EstMinutes: 60, Source: "code-review"},
				{Group: "1", Status: "resolved", Severity: "LOW", File: "internal/autofix/revert.go:41",
					Problem: "perm loss", Fix: "assert mode", Category: "correctness", EstMinutes: 30, Source: "code-review"},
			},
		},
		{
			Date: "2026-06-26", SourceType: tdmigrate.SourceTypeReview, Label: "new-review",
			Items: []tdmigrate.Item{
				{Group: "2", Status: "open", Severity: "CRITICAL", File: "cmd/atcr/autofix.go:248",
					Problem: "remote leftover", Fix: "message", Category: "docs", EstMinutes: 15, Source: "claude"},
				{Group: "2", Status: "deferred", Severity: "MEDIUM", File: "cmd/atcr/review.go",
					Problem: "exit gate", Fix: "document", Category: "docs", EstMinutes: 0, Source: "execute-sprint"},
			},
		},
	}
}

func TestFlatten_PreservesOrderAndProvenance(t *testing.T) {
	recs := Flatten(sampleShards())
	require.Len(t, recs, 4)

	// First record keeps its shard date/label alongside the item fields.
	assert.Equal(t, "2026-06-13", recs[0].Date)
	assert.Equal(t, "old-sprint", recs[0].Label)
	assert.Equal(t, "HIGH", recs[0].Severity)
	assert.Equal(t, "internal/autofix/apply.go:108", recs[0].File)

	// Cross-shard flattening preserves shard-then-item order.
	assert.Equal(t, "2026-06-26", recs[2].Date)
	assert.Equal(t, "CRITICAL", recs[2].Severity)
}

func TestLoad_RoundTripsWrittenShards(t *testing.T) {
	dir := t.TempDir()
	_, err := tdmigrate.WriteShards(dir, sampleShards())
	require.NoError(t, err)

	recs, err := Load(dir)
	require.NoError(t, err)
	assert.Len(t, recs, 4)
}

func TestLoad_EmptyDirIsNotAnError(t *testing.T) {
	recs, err := Load(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestFilter_Match(t *testing.T) {
	recs := Flatten(sampleShards())

	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"zero filter passes all", Filter{}, 4},
		{"severity case-insensitive", Filter{Severity: "high"}, 1},
		{"status open", Filter{Status: "open"}, 2},
		{"status resolved", Filter{Status: "resolved"}, 1},
		{"category substring", Filter{Category: "doc"}, 2},
		{"group exact", Filter{Group: "1"}, 2},
		{"component prefix", Filter{Component: "internal/autofix"}, 2},
		{"component prefix cmd", Filter{Component: "cmd/atcr"}, 2},
		{"combined severity+component", Filter{Severity: "CRITICAL", Component: "cmd/atcr"}, 1},
		{"no match", Filter{Severity: "HIGH", Component: "cmd/atcr"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(recs, tc.f)
			assert.Len(t, got, tc.want)
		})
	}
}

func TestApply_ReturnsNonNilEmpty(t *testing.T) {
	got := Apply(nil, Filter{Severity: "HIGH"})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestFilePath_StripsLineSuffixOnly(t *testing.T) {
	assert.Equal(t, "cmd/atcr/autofix.go", filePath("cmd/atcr/autofix.go:248"))
	assert.Equal(t, "cmd/atcr/autofix.go", filePath("cmd/atcr/autofix.go:248-260"))
	assert.Equal(t, "cmd/atcr/review.go", filePath("cmd/atcr/review.go"))
	// A colon with a non-numeric tail (free text) is left untouched.
	assert.Equal(t, "see docs: the thing", filePath("see docs: the thing"))
}

func TestSort_Severity(t *testing.T) {
	recs := Flatten(sampleShards())
	require.NoError(t, Sort(recs, SortSeverity))
	got := []string{recs[0].Severity, recs[1].Severity, recs[2].Severity, recs[3].Severity}
	assert.Equal(t, []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}, got)
}

func TestSort_Age_OldestFirst(t *testing.T) {
	recs := Flatten(sampleShards())
	require.NoError(t, Sort(recs, SortAge))
	assert.Equal(t, "2026-06-13", recs[0].Date)
	assert.Equal(t, "2026-06-26", recs[3].Date)
}

func TestSort_Est_LargestFirst(t *testing.T) {
	recs := Flatten(sampleShards())
	require.NoError(t, Sort(recs, SortEst))
	assert.Equal(t, 60, recs[0].EstMinutes)
	assert.Equal(t, 0, recs[3].EstMinutes)
}

func TestSort_UnknownKeyIsError(t *testing.T) {
	err := Sort(Flatten(sampleShards()), "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sort key")
}

func TestSyncShards_RegeneratesFromREADME(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")

	// A minimal authoritative README with one section/one item.
	content := "# TD\n\n" +
		"### [2026-07-01] From Sprint: demo\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | HIGH | x.go:1 | p | f | correctness | 15 | code-review |\n"
	require.NoError(t, os.WriteFile(readme, []byte(content), 0o644))

	var stderr bytes.Buffer
	require.NoError(t, SyncShards(readme, items, &stderr))

	recs, err := Load(items)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity)
	assert.Equal(t, "2026-07-01", recs[0].Date)
}
