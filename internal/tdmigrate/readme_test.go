package tdmigrate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureReadme mirrors the real README: a static preamble, a 9-column section,
// and an 11-column (Reviewers|Confidence) section, with all three checkbox states.
const fixtureReadme = `# Technical Debt Tracking

Intro prose.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| LOW | 1 | 0 | 0 |

**Last Modified:** 2026-06-26 | **Open Items:** 1 | **Deferred Items:** 1 | **Resolved Items:** 1 | **Total Items:** 3

## How to Use

Stuff.

### [2026-06-26] From Sprint: epic-11.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [ ] | LOW | internal/tools/dispatch.go:123 | substring matching rejects names | match on token boundaries | REGRESSION_RISK | 30 | execute-epic-independent |
| U | [x] | LOW | internal/tools/exec_tools.go:66 | exported widens trust surface | document closed set | SECURITY | 30 | execute-epic-independent |

### [2026-06-23] From Sprint: 8.0_reconciler_library

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | HIGH | internal/reconcile/discover.go:25 | Source shape changed | adapter converts | correctness | 0 | execute-sprint | execute-sprint, claude | MEDIUM |
`

func TestParseReadme_PreambleAndItems(t *testing.T) {
	preamble, items, err := ParseReadme(fixtureReadme)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(preamble, "# Technical Debt Tracking"))
	assert.Contains(t, preamble, "## How to Use")
	assert.False(t, strings.Contains(preamble, "### [2026-06-26]"), "preamble stops before first dated section")

	require.Len(t, items, 3)

	// Sequential IDs in document order.
	assert.Equal(t, "TD-0001", items[0].ID)
	assert.Equal(t, 1, items[0].Order)
	assert.Equal(t, "TD-0003", items[2].ID)

	// First item: 9-column section, open.
	it0 := items[0]
	assert.Equal(t, "[2026-06-26] From Sprint: epic-11.2", it0.Section)
	assert.Equal(t, "2026-06-26", it0.Date)
	assert.Equal(t, "1", it0.Group)
	assert.Equal(t, "open", it0.Status)
	assert.Equal(t, "LOW", it0.Severity)
	assert.Equal(t, "internal/tools/dispatch.go:123", it0.File)
	assert.Equal(t, "substring matching rejects names", it0.Problem)
	assert.Equal(t, "match on token boundaries", it0.Fix)
	assert.Equal(t, "REGRESSION_RISK", it0.Category)
	assert.Equal(t, "30", it0.EstMinutes)
	assert.Equal(t, "execute-epic-independent", it0.Source)
	assert.False(t, it0.HasReviewCols)
	assert.Empty(t, it0.Reviewers)
	assert.Empty(t, it0.Confidence)

	// Second item resolved.
	assert.Equal(t, "resolved", items[1].Status)

	// Third item: 11-column section, deferred, with reviewer metadata.
	it2 := items[2]
	assert.Equal(t, "2026-06-23", it2.Date)
	assert.Equal(t, "deferred", it2.Status)
	assert.True(t, it2.HasReviewCols)
	assert.Equal(t, "execute-sprint, claude", it2.Reviewers)
	assert.Equal(t, "MEDIUM", it2.Confidence)
	assert.Equal(t, "0", it2.EstMinutes)
}

func TestRenderTable_RoundTripsThroughParse(t *testing.T) {
	_, items, err := ParseReadme(fixtureReadme)
	require.NoError(t, err)

	table := RenderTable(items)

	// Re-parse the regenerated table (prepend a minimal preamble boundary so the
	// parser treats the whole thing as dated sections).
	_, items2, err := ParseReadme("# x\n\n" + table)
	require.NoError(t, err)

	require.Len(t, items2, len(items))
	for i := range items {
		// IDs/Order are reassigned by parse; compare the data fields that matter.
		a, b := items[i], items2[i]
		assert.Equal(t, a.Section, b.Section, "section %d", i)
		assert.Equal(t, a.Status, b.Status, "status %d", i)
		assert.Equal(t, a.Severity, b.Severity, "severity %d", i)
		assert.Equal(t, a.File, b.File, "file %d", i)
		assert.Equal(t, a.Problem, b.Problem, "problem %d", i)
		assert.Equal(t, a.Fix, b.Fix, "fix %d", i)
		assert.Equal(t, a.Category, b.Category, "category %d", i)
		assert.Equal(t, a.EstMinutes, b.EstMinutes, "est %d", i)
		assert.Equal(t, a.Source, b.Source, "source %d", i)
		assert.Equal(t, a.Group, b.Group, "group %d", i)
		assert.Equal(t, a.HasReviewCols, b.HasReviewCols, "hasReview %d", i)
		assert.Equal(t, a.Reviewers, b.Reviewers, "reviewers %d", i)
		assert.Equal(t, a.Confidence, b.Confidence, "confidence %d", i)
	}
}

// Full migrate -> generate cycle: README -> item files -> back to items, lossless.
func TestMigrateGenerate_FullCycleLossless(t *testing.T) {
	_, items, err := ParseReadme(fixtureReadme)
	require.NoError(t, err)

	// Serialize every item to file form and parse it back.
	var back []Item
	for _, it := range items {
		content, err := RenderItemFile(it)
		require.NoError(t, err)
		parsed, err := ParseItemFile(content)
		require.NoError(t, err)
		back = append(back, parsed)
	}
	assert.Equal(t, items, back, "every item must survive the file round-trip exactly")

	// Filenames must be unique across the set.
	seen := map[string]bool{}
	for _, it := range items {
		name := it.Filename()
		assert.False(t, seen[name], "duplicate filename: %s", name)
		seen[name] = true
	}
}

func TestParseReadme_NoSections(t *testing.T) {
	preamble, items, err := ParseReadme("# Only preamble\n\nNo dated sections.\n")
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Contains(t, preamble, "Only preamble")
}
