package debt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestInsertRow_DoesNotCrossIntoLaterNonDatedTable(t *testing.T) {
	// A dated section followed by a `##` section that ALSO has a pipe table. The
	// new row must land in the dated section, never spliced into the later table.
	content := "# TD\n\n" +
		"### [2026-07-03] From Sprint: manual\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | LOW | a.go:1 | old | oldfix | correctness | 5 | code-review |\n\n" +
		"## Reference\n\n" +
		"| Col A | Col B |\n|-------|-------|\n| x | y |\n"
	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}

	out, err := insertRow(content, sec, newItemForAdd())
	require.NoError(t, err)

	// The new row must appear before the `## Reference` heading, not spliced into
	// the later table — the point of scanning to any `#` header, not just `### `.
	assert.Less(t, strings.Index(out, "internal/x/y.go:12"), strings.Index(out, "## Reference"))
	// The reference table is left untouched.
	assert.Contains(t, out, "| Col A | Col B |")
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

// readmeWithStats is a README carrying a Stats block plus one existing item, so
// stat-refresh behavior can be asserted end-to-end.
const readmeWithStats = "# Technical Debt\n\n" +
	"## Stats\n\n" +
	"| Severity | Open | Deferred | Resolved |\n" +
	"|----------|------|----------|----------|\n" +
	"| CRITICAL | 0 | 0 | 0 |\n" +
	"| HIGH | 0 | 0 | 0 |\n" +
	"| MEDIUM | 0 | 0 | 0 |\n" +
	"| LOW | 0 | 0 | 0 |\n\n" +
	"**Last Modified:** 2026-01-01 | **Open Items:** 0 | **Deferred Items:** 0 | **Resolved Items:** 0 | **Total Items:** 0\n\n" +
	"## How to Use\n\nprose here\n\n" +
	"### [2026-06-30] From Sprint: prior\n\n" +
	"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
	"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
	"| 1 | [ ] | MEDIUM | a.go:1 | old | oldfix | correctness | 5 | code-review |\n"

func TestAppendItem_RefreshesStats(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")
	require.NoError(t, os.WriteFile(readme, []byte(readmeWithStats), 0o644))

	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}
	var stderr bytes.Buffer
	require.NoError(t, AppendItem(readme, items, sec, newItemForAdd(), &stderr))

	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	s := string(got)

	// Two items now: the prior open MEDIUM and the new open HIGH.
	assert.Contains(t, s, "**Open Items:** 2")
	assert.Contains(t, s, "**Total Items:** 2")
	assert.Contains(t, s, "**Last Modified:** 2026-07-03")
	// Per-severity row reflects the counts (HIGH now has 1 open).
	assert.Contains(t, s, "| HIGH | 1 | 0 | 0 |")
	assert.Contains(t, s, "| MEDIUM | 1 | 0 | 0 |")
	// The intervening prose and prior section survive the stats rewrite.
	assert.Contains(t, s, "prose here")
	assert.Contains(t, s, "### [2026-06-30] From Sprint: prior")
}

func TestRefreshStats_NoStatsBlockIsNoOp(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# TD\n\nno stats here\n"), 0o644))
	require.NoError(t, RefreshStats(readme, "2026-07-03"))

	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	assert.Equal(t, "# TD\n\nno stats here\n", string(got), "a README without a Stats block is left untouched")
}

func TestRefreshStats_PreservesContentBetweenStatsAndLastModified(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	content := "# Technical Debt\n\n" +
		"## Stats\n\n" +
		"| Severity | Open | Deferred | Resolved |\n" +
		"|----------|------|----------|----------|\n" +
		"| CRITICAL | 0 | 0 | 0 |\n" +
		"| HIGH | 0 | 0 | 0 |\n" +
		"| MEDIUM | 0 | 0 | 0 |\n" +
		"| LOW | 0 | 0 | 0 |\n\n" +
		"**Note:** do not delete this intervening note\n\n" +
		"**Last Modified:** 2026-01-01 | **Open Items:** 0 | **Deferred Items:** 0 | **Resolved Items:** 0 | **Total Items:** 0\n\n" +
		"### [2026-06-30] From Sprint: prior\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | MEDIUM | a.go:1 | old | oldfix | correctness | 5 | code-review |\n"
	require.NoError(t, os.WriteFile(readme, []byte(content), 0o644))
	require.NoError(t, RefreshStats(readme, "2026-07-03"))

	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	s := string(got)
	assert.Contains(t, s, "**Note:** do not delete this intervening note",
		"content between the Stats table and the Last Modified line must survive")
	assert.Contains(t, s, "**Last Modified:** 2026-07-03")
	// The refreshed README must still parse cleanly.
	_, err = tdmigrate.ParseREADME(s)
	require.NoError(t, err)
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

func TestAppendItem_RollsBackREADMEOnShardSyncFailure(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")
	require.NoError(t, os.WriteFile(readme, []byte(readmeWithStats), 0o644))

	// Make the items path a regular file so WriteShards cannot create a directory
	// there and SyncShards fails after the README has already been updated.
	require.NoError(t, os.WriteFile(items, []byte("not a directory"), 0o644))

	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}
	var stderr bytes.Buffer
	err := AppendItem(readme, items, sec, newItemForAdd(), &stderr)
	require.Error(t, err)

	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "internal/x/y.go:12",
		"README should be rolled back to pre-write bytes when shard sync fails")
	assert.Contains(t, string(got), "## How to Use",
		"intervening prose must survive the rollback")
}

func TestWithReadmeLock_SerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, ".planning", ".locks", "td-readme.lock")

	var counter int
	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- withReadmeLock(dir, "test", func() error {
				v := counter
				time.Sleep(time.Millisecond)
				counter = v + 1
				return nil
			})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, 10, counter, "concurrent callers must be serialized")
	_, err := os.Stat(lockDir)
	assert.True(t, os.IsNotExist(err), "lock directory must be released after fn returns")
}

func TestAppendItem_ConcurrentAddsSurvive(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")
	require.NoError(t, os.WriteFile(readme, []byte("# Technical Debt\n\n"), 0o644))

	sec := Section{Date: "2026-07-03", SourceType: "Sprint", Label: "manual"}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			it := newItemForAdd()
			it.File = fmt.Sprintf("internal/x/y%d.go:12", n)
			var stderr bytes.Buffer
			require.NoError(t, AppendItem(readme, items, sec, it, &stderr))
		}(i)
	}
	wg.Wait()

	recs, err := Load(items)
	require.NoError(t, err)
	require.Len(t, recs, 5, "all concurrent adds must survive")
}
