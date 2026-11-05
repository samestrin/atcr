package localdebt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseTDFixture parses one testdata/tdtables fixture with ParseTDItems.
func parseTDFixture(t *testing.T, name string) []TDItem {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "tdtables", name))
	require.NoError(t, err)
	items, err := ParseTDItems(bytes.NewReader(b))
	require.NoError(t, err)
	return items
}

// TestParseTDItems_EveryLayoutYieldsEveryItemRow is epic 95 T1's success
// criterion: each of the three table layouts parses to exactly the fixture's item
// rows, and to the same rows ParseTDRows finds, so an import counts from one
// definition of "item row". The Stats table's `[ ]` row is not an item.
func TestParseTDItems_EveryLayoutYieldsEveryItemRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows int
	}{
		{"layout9.md", 5},
		{"layout10.md", 4},
		{"layout11.md", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := parseTDFixture(t, tc.name)
			assert.Len(t, items, tc.rows)

			b, err := os.ReadFile(filepath.Join("testdata", "tdtables", tc.name))
			require.NoError(t, err)
			rows, err := ParseTDRows(bytes.NewReader(b))
			require.NoError(t, err)
			require.Len(t, items, len(rows), "ParseTDItems and ParseTDRows must agree on which rows are items")
			for i := range rows {
				assert.Equal(t, rows[i].Line, items[i].Line)
				assert.Equal(t, rows[i].Marker, items[i].Marker)
				assert.Equal(t, rows[i].Section, items[i].Section)
			}
		})
	}
}

func TestParseTDItems_NineColumnLayout(t *testing.T) {
	items := parseTDFixture(t, "layout9.md")
	require.Len(t, items, 5)

	assert.Equal(t, TDItem{
		Line: 14, Section: "### [2026-09-01] From: epic-10-review", Marker: "[ ]",
		Group: "1", Severity: "HIGH", File: "internal/a/a.go", FileLine: 12,
		Problem: "A pipe `a | b` inside code", Fix: "Escape it", Category: "correctness",
		EstMinutes: 30, Source: "epic-review EST-1",
	}, items[0], "an escaped pipe is read as a literal pipe, not a cell boundary")

	assert.Equal(t, "internal/scorecard/trust.go", items[1].File)
	assert.Equal(t, 0, items[1].FileLine, "a File cell with no :line reads as line 0")
	assert.Equal(t, "U", items[1].Group)
	assert.Equal(t, "[x]", items[1].Marker)

	assert.Equal(t, "internal/reconcile/lib.go", items[2].File)
	assert.Equal(t, 109, items[2].FileLine, "a :start-end range reads as its start line")

	assert.Equal(t, "### [2026-09-02] From: epic-11-review", items[3].Section)
	assert.Equal(t, "~/.config/atcr/registry.yaml:vera", items[3].File,
		"a colon not followed by a line number belongs to the file name")
	assert.Equal(t, 0, items[3].FileLine)

	for _, it := range items[:4] {
		assert.Empty(t, it.Invalid, "row %d is well formed", it.Line)
		assert.Zero(t, it.Attempts)
		assert.Nil(t, it.Reviewers)
	}

	bad := items[4]
	assert.Equal(t, 23, bad.Line)
	assert.Equal(t, "[ ]", bad.Marker)
	assert.Contains(t, bad.Invalid, "10 cells")
	assert.Contains(t, bad.Invalid, "9")
	assert.Empty(t, bad.Problem, "a misaligned row's cells are not trusted")
}

func TestParseTDItems_AttemptsLayout(t *testing.T) {
	items := parseTDFixture(t, "layout10.md")
	require.Len(t, items, 4)

	assert.Equal(t, 0, items[0].Attempts)
	assert.Empty(t, items[0].Invalid)

	assert.Equal(t, "cli/leaderboard.go", items[1].File)
	assert.Equal(t, 0, items[1].FileLine)
	assert.Equal(t, 2, items[1].Attempts)
	assert.Equal(t, 60, items[1].EstMinutes)

	assert.Equal(t, "Escaped | pipe and a trailing one |", items[2].Problem)
	assert.Equal(t, "CRITICAL", items[2].Severity)
	assert.Equal(t, 1, items[2].Attempts)
	assert.Empty(t, items[2].Invalid)

	assert.Contains(t, items[3].Invalid, "Attempts")
	assert.Contains(t, items[3].Invalid, "many")
}

func TestParseTDItems_ReviewersConfidenceLayout(t *testing.T) {
	items := parseTDFixture(t, "layout11.md")
	require.Len(t, items, 3)

	assert.Equal(t, []string{"dax", "kai"}, items[0].Reviewers)
	assert.Equal(t, "MEDIUM", items[0].Confidence)
	assert.Equal(t, "execute-sprint", items[0].Source)
	assert.Empty(t, items[0].Invalid)

	assert.Equal(t, []string{"otto"}, items[1].Reviewers)
	assert.Equal(t, "HIGH", items[1].Confidence)

	assert.Equal(t, []string{"claude", "kimi-k3"}, items[2].Reviewers)
	assert.Contains(t, items[2].Invalid, "Est Minutes")
	assert.Contains(t, items[2].Invalid, "soon")
}

// TestParseTDItems_HeaderlessTableUsesCanonicalLayout mirrors ParseTDRows: a
// table with no header row is read in the canonical 10-column layout.
func TestParseTDItems_HeaderlessTableUsesCanonicalLayout(t *testing.T) {
	in := "### s\n\n| 3 | [/] | LOW | a.go:4 | P | F | style | 7 | src | 1 |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, TDItem{
		Line: 3, Section: "### s", Marker: "[/]", Group: "3", Severity: "LOW",
		File: "a.go", FileLine: 4, Problem: "P", Fix: "F", Category: "style",
		EstMinutes: 7, Source: "src", Attempts: 1,
	}, items[0])
}

// TestParseTDItems_EmptyNumericCellsAreZero: an empty Est Minutes or Attempts
// cell is "not recorded", not malformed.
func TestParseTDItems_EmptyNumericCellsAreZero(t *testing.T) {
	in := "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n" +
		"| 1 | [ ] | LOW | a.go:1 | P | F | style |  | src |  |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Invalid)
	assert.Zero(t, items[0].EstMinutes)
	assert.Zero(t, items[0].Attempts)
}

// TestParseTDItems_NegativeCountIsInvalid: a count below zero is not a count.
func TestParseTDItems_NegativeCountIsInvalid(t *testing.T) {
	in := "| 1 | [ ] | LOW | a.go:1 | P | F | style | 5 | src | -1 |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Contains(t, items[0].Invalid, "Attempts")
}

// TestParseTDItems_ShortRowIsInvalid: a row missing cells is as misaligned as a
// row with extra ones.
func TestParseTDItems_ShortRowIsInvalid(t *testing.T) {
	in := "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|---|---|---|---|---|---|---|---|---|\n" +
		"| 1 | [ ] | LOW | a.go:1 | P | F | style | 5 |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Contains(t, items[0].Invalid, "8 cells")
}

// --- schema v4: attempts ----------------------------------------------------

// TestRecord_V4AttemptsRoundTrip is epic 95 T1's second success criterion: a v4
// record round-trips through the store with and without attempts, and a record
// without attempts carries no attempts key on the wire.
func TestRecord_V4AttemptsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
	}{
		{"with attempts", 3},
		{"without attempts", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := Record{
				SchemaVersion: SchemaVersion, RunID: "2026-10-01T00:00:00Z-import", Timestamp: "2026-10-01T00:00:00Z",
				Severity: "LOW", File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"otto"},
				Attempts: tc.attempts,
			}
			rec.StampID()

			b, err := json.Marshal(rec)
			require.NoError(t, err)
			assert.Equal(t, tc.attempts != 0, strings.Contains(string(b), `"attempts"`),
				"attempts is omitempty: present only when non-zero")

			dir := t.TempDir()
			require.NoError(t, Append(dir, rec))
			var diag bytes.Buffer
			got, err := ReadAll(dir, ReadOpts{Writer: &diag})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Empty(t, diag.String())
			assert.Equal(t, rec, got[0])
		})
	}
}

// TestRecord_V3RecordDecodesWithZeroAttempts: the bump is backward-compatible on
// read.
func TestRecord_V3RecordDecodesWithZeroAttempts(t *testing.T) {
	dir := t.TempDir()
	line := `{"schema_version":3,"id":"id3","run_id":"r3","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["otto"]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(line+"\n"), 0o644))
	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Zero(t, recs[0].Attempts)
	assert.Empty(t, diag.String())
}

// TestAppend_RejectsNegativeAttempts: the write gate keeps a negative count off
// the append-only store.
func TestAppend_RejectsNegativeAttempts(t *testing.T) {
	rec := Record{SchemaVersion: SchemaVersion, RunID: "2026-10-01T00:00:00Z-import", Timestamp: "2026-10-01T00:00:00Z",
		Severity: "LOW", File: "a.go", Line: 1, Problem: "p", Attempts: -1}
	rec.StampID()
	err := Append(t.TempDir(), rec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attempts")
}

// TestParseTDItems_HeaderlessEdgeLayouts covers a headerless row with no Group
// cell and one without the Attempts cell (the 9-column layout).
func TestParseTDItems_HeaderlessEdgeLayouts(t *testing.T) {
	in := "| [ ] | LOW | a.go:1 | P | F | style | 5 | src | 2 |\n\n" +
		"| 4 | [x] | HIGH | b.go:2 | Q | G | logic | 6 | src2 |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Empty(t, items[0].Invalid)
	assert.Equal(t, "", items[0].Group)
	assert.Equal(t, "P", items[0].Problem)
	assert.Equal(t, 2, items[0].Attempts)
	assert.Empty(t, items[1].Invalid)
	assert.Equal(t, "4", items[1].Group)
	assert.Equal(t, "src2", items[1].Source)
	assert.Zero(t, items[1].Attempts)
}

// TestParseTDItems_EstHeaderAlias: the canonical layout names the column `Est`,
// the tables atcr writes name it `Est Minutes`; both read.
func TestParseTDItems_EstHeaderAlias(t *testing.T) {
	in := "| Group | | Severity | File | Problem | Fix | Category | Est | Source |\n" +
		"|---|---|---|---|---|---|---|---|---|\n" +
		"| 1 | [ ] | LOW | a.go:1 | P | F | style | 25 | src |\n"
	items, err := ParseTDItems(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 25, items[0].EstMinutes)
}
