package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// importREADME is a technical-debt table holding one row of each kind the import
// tells apart: the four markers, an Attempts count, a duplicate id, a malformed
// row, a row under an undated section and a cell carrying control characters.
const importREADME = "# Technical Debt\n\n" +
	"## Stats\n\n| Status | Count |\n|---|---|\n| [ ] | 4 |\n\n" +
	"### [2026-10-01] From: branch-a\n\n" +
	"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
	"|---|---|---|---|---|---|---|---|---|---|\n" +
	"| g | [ ] | HIGH | a.go:10 | open problem | fix a | bug | 15 | paperclip-builder | 0 |\n" +
	"| g | [/] | medium | b.go:20 | deferred problem | fix b | bug | 5 | paperclip-builder | 2 |\n" +
	"| g | [x] | LOW | c.go | resolved problem | fix c | style | 1 | paperclip-builder | 1 |\n" +
	"| g | [-] | LOW | d.go:4 | unreproducible \\| piped | fix d | bug | 1 | paperclip-builder | 0 |\n" +
	"| g | [ ] | LOW | e.go:1 | broken | row | with | an | extra | cell | here |\n" +
	"| g | [ ] | LOW | f.go:1 | bell\x07 and \x1b[31mred\x1b[0m\u202e | fix f | bug | 1 | paperclip-builder | 0 |\n\n" +
	"### [November 17, 2026] From Sprint: 35.16.10\n\n" +
	"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
	"|---|---|---|---|---|---|---|---|---|\n" +
	"| g | [x] | HIGH | a.go:10 | open problem | fix a later | bug | 15 | paperclip-reviewer |\n\n" +
	"### Undated notes\n\n" +
	"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
	"|---|---|---|---|---|---|---|---|---|\n" +
	"| g | [ ] | LOW | g.go:1 | undated | fix g | bug | 1 | paperclip-builder |\n"

func writeImportREADME(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "README.md")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// storeSnapshot maps every file under dir to a hash of its bytes, so two
// snapshots differ exactly when a file was added, removed or changed.
func storeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || d.IsDir() {
			return err
		}
		if strings.Contains(p, ".lock") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(dir, p)
		snap[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	require.NoError(t, err)
	return snap
}

func importedByProblem(t *testing.T, dir string) map[string]localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	got := map[string]localdebt.Record{}
	for _, r := range recs {
		got[r.Problem] = r
	}
	return got
}

func TestDebtImport_DryRunWritesNothingAndCountsEveryRow(t *testing.T) {
	readme := writeImportREADME(t, importREADME)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir, "--dry-run")
	require.NoError(t, err, out)

	// 8 item rows (the Stats checkbox is not one): 5 imported, 1 merged, 2 rejected.
	assert.Contains(t, out, "8 item rows")
	assert.Contains(t, out, "5 imported")
	assert.Contains(t, out, "1 merged")
	assert.Contains(t, out, "2 rejected")
	assert.Contains(t, out, "would write 5")
	assert.Contains(t, out, "has 11 cells")
	assert.Contains(t, out, "no dated")

	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "a dry run must not create the store")
}

func TestDebtImport_WritesStatusAttemptsAndSectionTime(t *testing.T) {
	readme := writeImportREADME(t, importREADME)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "wrote 5")

	got := importedByProblem(t, dir)
	require.Len(t, got, 5)

	// The duplicate a.go:10 row keeps the newest section's version.
	open := got["open problem"]
	assert.Equal(t, localdebt.StatusResolved, open.Status)
	assert.Equal(t, "fix a later", open.Fix)
	assert.Equal(t, "2026-11-17T00:00:00Z", open.Timestamp)
	assert.Equal(t, "2026-11-17T00:00:00Z-import", open.RunID)
	assert.Equal(t, "2026-11-17T00:00:00Z", open.ResolvedAt)

	def := got["deferred problem"]
	assert.Equal(t, localdebt.StatusDeferred, def.Status)
	assert.Equal(t, "MEDIUM", def.Severity)
	assert.Equal(t, 2, def.Attempts)
	assert.Equal(t, "2026-10-01T00:00:00Z", def.Timestamp)
	assert.Equal(t, "2026-10-01T00:00:00Z-import", def.RunID)
	assert.Equal(t, "b.go", def.File)
	assert.Equal(t, 20, def.Line)
	assert.Equal(t, localdebt.OriginManual, def.Origin)
	assert.Equal(t, localdebt.SchemaVersion, def.SchemaVersion)
	require.NotNil(t, def.SourceReport)
	assert.Equal(t, "### [2026-10-01] From: branch-a", def.SourceReport.Section)

	res := got["resolved problem"]
	assert.Equal(t, localdebt.StatusResolved, res.Status)
	assert.Equal(t, 0, res.Line)
	assert.Equal(t, 1, res.Attempts)

	assert.Equal(t, localdebt.StatusUnreproducible, got["unreproducible | piped"].Status)

	// Control characters are stripped before the record is written.
	clean, ok := got["bell and [31mred[0m"]
	require.True(t, ok, "stripped problem text; have %v", keys(got))
	assert.Equal(t, "", clean.Status)
	assert.Equal(t, clean.ID, func() string { r := clean; r.StampID(); return r.ID }())
}

func keys(m map[string]localdebt.Record) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestDebtImport_RerunChangesNothing(t *testing.T) {
	for _, layout := range []string{"shards", "events"} {
		t.Run(layout, func(t *testing.T) {
			readme := writeImportREADME(t, importREADME)
			dir := filepath.Join(t.TempDir(), "debt")
			if layout == "events" {
				require.NoError(t, localdebt.EnableEventLayout(dir))
			}

			_, err := runDebt(t, "import", readme, "--store", dir)
			require.NoError(t, err)
			before := storeSnapshot(t, dir)
			require.NotEmpty(t, before)

			out, err := runDebt(t, "import", readme, "--store", dir)
			require.NoError(t, err, out)
			assert.Contains(t, out, "wrote 0")
			assert.Contains(t, out, "5 already in the store")
			assert.Equal(t, before, storeSnapshot(t, dir), "a second import must write no new or changed files")
		})
	}
}

func TestDebtImport_EventLayoutWritesEventFiles(t *testing.T) {
	readme := writeImportREADME(t, importREADME)
	dir := filepath.Join(t.TempDir(), "debt")
	require.NoError(t, localdebt.EnableEventLayout(dir))

	_, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err)
	recs, err := localdebt.ReadEventRecords(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, recs, 5)
}

func TestDebtImport_RejectsUnusableRows(t *testing.T) {
	body := "### [2026-10-01] From: x\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n" +
		"| g | [?] | LOW | a.go:1 | odd marker | f | c | 1 | s | 0 |\n" +
		"| g | [ ] | URGENT | a.go:2 | bad severity | f | c | 1 | s | 0 |\n" +
		"| g | [ ] | LOW |  | no file | f | c | 1 | s | 0 |\n" +
		"| g | [ ] | LOW | a.go:3 |  | f | c | 1 | s | 0 |\n" +
		"| g | [ ] | LOW | a.go:4 | huge est | f | c | 999999 | s | 0 |\n" +
		"| g | [ ] | LOW | a.go:5 | bad attempts | f | c | 1 | s | many |\n" +
		"| g | [ ] | LOW | a.go:6 | fine | f | c | 1 | s | 0 |\n"
	readme := writeImportREADME(t, body)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "7 item rows")
	assert.Contains(t, out, "1 imported")
	assert.Contains(t, out, "6 rejected")
	for _, why := range []string{"marker", "severity", "File", "Problem", "Est Minutes", "Attempts"} {
		assert.Contains(t, out, why)
	}
	assert.Len(t, importedByProblem(t, dir), 1)
}

func TestDebtImport_Args(t *testing.T) {
	_, err := runDebt(t, "import")
	require.Error(t, err)
	_, err = runDebt(t, "import", filepath.Join(t.TempDir(), "missing.md"), "--store", t.TempDir())
	require.Error(t, err)
}

// A row whose cells fit the parser but whose record does not fit the store is
// rejected: every read path skips a line over MaxRecordBytes, so writing it
// would file a record nobody could see.
func TestDebtImport_RejectsOversizedRecord(t *testing.T) {
	// Each backslash encodes as two bytes, so ~600 KB of cell is a ~1.2 MB record.
	huge := strings.Repeat(`\`, localdebt.MaxRecordBytes*6/10)
	body := "### [2026-10-01] From: x\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n" +
		"| g | [ ] | LOW | a.go:1 | too big | " + huge + " | c | 1 | s | 0 |\n"
	readme := writeImportREADME(t, body)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "0 imported")
	assert.Contains(t, out, "1 rejected")
	assert.Contains(t, out, fmt.Sprintf("over the store's %d", localdebt.MaxRecordBytes))
	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "a rejected row must write nothing")
}

// A section dated in a year the store cannot shard is rejected for that reason,
// not as undated, and does not fail the rest of the import.
func TestDebtImport_RejectsUnshardableSectionYear(t *testing.T) {
	body := "### [0000-01-01] From: x\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n" +
		"| g | [ ] | LOW | a.go:1 | year zero | f | c | 1 | s | 0 |\n\n" +
		"### [2026-10-01] From: y\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n" +
		"| g | [ ] | LOW | a.go:2 | fine | f | c | 1 | s | 0 |\n"
	readme := writeImportREADME(t, body)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "1 imported")
	assert.Contains(t, out, "1 rejected")
	assert.Contains(t, out, `section date "0000-01-01"`)
	assert.NotContains(t, out, "no dated")
	assert.Len(t, importedByProblem(t, dir), 1)
}

// TestDebtImport_CarriesTheSourceCellAsReportedBy: a row's Source cell (the seat
// that reported it) is kept on the record as ReportedBy, separate from an
// 11-column table's Reviewers cell, and a rerun still writes nothing.
func TestDebtImport_CarriesTheSourceCellAsReportedBy(t *testing.T) {
	body := importREADME + "\n### [2026-10-05] From: wide\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| g | [ ] | LOW | w.go:2 | wide row | fix w | bug | 1 | atcr\x07 | vera, kai | high |\n"
	readme := writeImportREADME(t, body)
	dir := filepath.Join(t.TempDir(), "debt")

	out, err := runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	got := importedByProblem(t, dir)

	assert.Equal(t, "paperclip-builder", got["deferred problem"].ReportedBy)
	assert.Equal(t, "paperclip-reviewer", got["open problem"].ReportedBy, "a merged id keeps the newest row's Source")
	wide := got["wide row"]
	assert.Equal(t, "atcr", wide.ReportedBy, "the Source cell is stripped of control characters")
	assert.Equal(t, []string{"vera", "kai"}, wide.Reviewers, "Reviewers stays its own cell")

	before := storeSnapshot(t, dir)
	out, err = runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "wrote 0")
	assert.Equal(t, before, storeSnapshot(t, dir), "a second import must write no new or changed files")
}

// TestDebtImport_RestoresReportedByOnAnEventStore: a store imported before
// reported_by existed is restored by importing the same table again. Each row
// then has two records that tie on id, time and status, and an event store breaks
// that tie by the hash in the file name, so the fold must keep the reporter
// whichever of the two it picks.
func TestDebtImport_RestoresReportedByOnAnEventStore(t *testing.T) {
	var noSource []string
	for _, line := range strings.Split(importREADME, "\n") {
		noSource = append(noSource, strings.ReplaceAll(line, "| paperclip-builder |", "|  |"))
	}
	old := writeImportREADME(t, strings.ReplaceAll(strings.Join(noSource, "\n"), "| paperclip-reviewer |", "|  |"))
	readme := writeImportREADME(t, importREADME)
	dir := filepath.Join(t.TempDir(), "debt")
	require.NoError(t, localdebt.EnableEventLayout(dir))

	out, err := runDebt(t, "import", old, "--store", dir)
	require.NoError(t, err, out)
	out, err = runDebt(t, "import", readme, "--store", dir)
	require.NoError(t, err, out)
	assert.Contains(t, out, "wrote 5 records (0 already in the store)")

	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	folded := localdebt.FoldRecords(recs)
	require.Len(t, folded, 5)
	for _, r := range folded {
		want := "paperclip-builder"
		if r.Problem == "open problem" {
			want = "paperclip-reviewer"
		}
		assert.Equal(t, want, r.ReportedBy, "%s keeps its reporter", r.Problem)
	}
}
