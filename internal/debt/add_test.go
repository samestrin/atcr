package debt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

func newItemForAdd() tdmigrate.Item {
	return tdmigrate.Item{
		Group: "1", Status: "open", Severity: "HIGH",
		File: "internal/x/y.go:12", Problem: "boom", Fix: "guard it",
		Category: "correctness", EstMinutes: 30, Source: "manual",
	}
}

func TestInsertRow_CreatesNewSectionWhenAbsent(t *testing.T) {
	content := "# Technical Debt\n\nsome preamble\n"
	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}

	out, err := insertRow(content, sec, newItemForAdd())
	require.NoError(t, err)

	assert.Contains(t, out, "### [2026-07-03] From Sprint: manual")
	assert.Contains(t, out, "| 1 | [ ] | HIGH | internal/x/y.go:12 | boom | guard it | correctness | 30 | manual |")
	// Preamble is preserved.
	assert.Contains(t, out, "some preamble")

	// The result must round-trip through the authoritative parser.
	shards, err := tdmigrate.ParseREADME(out)
	require.NoError(t, err)
	require.Len(t, shards, 1)
	require.Len(t, shards[0].Items, 1)
	assert.Equal(t, "HIGH", shards[0].Items[0].Severity)
}

func TestInsertRow_AppendsToExistingSection(t *testing.T) {
	content := "# TD\n\n" +
		"### [2026-07-03] From Sprint: manual\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | LOW | a.go:1 | old | oldfix | correctness | 5 | code-review |\n" +
		"\n## Trailing section\n"
	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}

	out, err := insertRow(content, sec, newItemForAdd())
	require.NoError(t, err)

	shards, err := tdmigrate.ParseREADME(out)
	require.NoError(t, err)
	require.Len(t, shards, 1, "must append to the one existing section, not create a second")
	require.Len(t, shards[0].Items, 2)
	// New row lands after the existing row.
	assert.Less(t,
		strings.Index(out, "a.go:1"),
		strings.Index(out, "internal/x/y.go:12"))
	// The trailing section is untouched.
	assert.Contains(t, out, "## Trailing section")
}

func TestInsertRow_SanitizesPipesAndNewlines(t *testing.T) {
	it := newItemForAdd()
	it.Problem = "a | b\nc"
	it.Fix = "do | this"
	out, err := insertRow("# TD\n", Section{Date: "2026-07-03", SourceType: "Review", Label: "l"}, it)
	require.NoError(t, err)

	// Row still parses (pipes inside cells would otherwise break the table).
	shards, err := tdmigrate.ParseREADME(out)
	require.NoError(t, err)
	require.Len(t, shards, 1)
	assert.Equal(t, "a / b c", shards[0].Items[0].Problem)
	assert.Equal(t, "do / this", shards[0].Items[0].Fix)
}

func TestInsertRow_RejectsInvalidItem(t *testing.T) {
	bad := newItemForAdd()
	bad.Severity = "URGENT" // not a valid enum
	_, err := insertRow("# TD\n", Section{Date: "2026-07-03", SourceType: "Sprint", Label: "l"}, bad)
	require.Error(t, err)
}

func TestInsertRow_RejectsInvalidSection(t *testing.T) {
	_, err := insertRow("# TD\n", Section{Date: "2026-07-03", SourceType: "Bogus", Label: "l"}, newItemForAdd())
	require.Error(t, err)
}

func TestAppendItem_WritesREADMEAndRegeneratesShards(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")
	require.NoError(t, os.WriteFile(readme, []byte("# Technical Debt\n\nstaging area\n"), 0o644))

	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}
	var stderr bytes.Buffer
	require.NoError(t, AppendItem(readme, items, sec, newItemForAdd(), &stderr))

	// README on disk now contains the row.
	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	assert.Contains(t, string(got), "internal/x/y.go:12")

	// Shards were regenerated and now contain the item (the whole point of the
	// write-README-then-migrate flow).
	recs, err := Load(items)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity)
}
