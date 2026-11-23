package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// wontfixRec is openRec closed with status and reason at ts.
func wontfixRec(open localdebt.Record, ts, status, reason string) localdebt.Record {
	r := open
	r.RunID, r.Timestamp = ts+"-r", ts
	r.Status, r.Justification, r.ResolvedAt = status, reason, ts
	return r
}

func TestExportWontfixRecords_OneEffectiveWontfixPerIDOnBothLayouts(t *testing.T) {
	// a: open, then dismissed -> exported with its reason.
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	a.Reviewers = []string{"security", "correctness"}
	aWontfix := wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "fixtures are generated, not hand-written")
	// b: dismissed, then re-detected -> wontfix survives re-detection.
	b := openRec("2026-06-01T11:00:00Z-b", "LOW", "internal/y/b.go", 3, "leak")
	b.Category = "performance"
	bWontfix := wontfixRec(b, "2026-06-02T11:00:00Z", "wontfix", "bounded by the request size")
	bAgain := b
	bAgain.RunID, bAgain.Timestamp = "2026-07-01T10:00:00Z-c", "2026-07-01T10:00:00Z"
	// c: resolved -> not exported.
	c := openRec("2026-06-01T12:00:00Z-d", "MEDIUM", "internal/z/c.go", 7, "race")
	cResolved := wontfixRec(c, "2026-06-03T10:00:00Z", "resolved", "added a mutex")
	// d: still open -> not exported.
	d := openRec("2026-06-01T13:00:00Z-e", "MEDIUM", "internal/z/d.go", 9, "nil deref")

	recs := []localdebt.Record{a, b, c, d, aWontfix, bWontfix, cResolved, bAgain}
	for name, split := range map[string]int{"shard-only": len(recs), "event-only": 0, "mixed": 4} {
		t.Run(name, func(t *testing.T) {
			dir := writeEventStore(t, split, recs...)
			got, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: io.Discard})
			require.NoError(t, err)
			assert.Equal(t, []WontfixRecord{
				{
					ID: a.ID, Reason: "fixtures are generated, not hand-written",
					File: "internal/x/a.go", Line: 12, Category: "correctness",
					Personas: []string{"security", "correctness"}, ResolvedAt: "2026-06-02T10:00:00Z",
				},
				{
					ID: b.ID, Reason: "bounded by the request size",
					File: "internal/y/b.go", Line: 3, Category: "performance",
					Personas: []string{"claude"}, ResolvedAt: "2026-06-02T11:00:00Z",
				},
			}, got)
		})
	}
}

func TestExportWontfixRecords_StatusIsNormalized(t *testing.T) {
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, a, wontfixRec(a, "2026-06-02T10:00:00Z", " WontFix ", "not reachable"))
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "not reachable", got[0].Reason)
}

func TestExportWontfixRecords_PersonasAreACopy(t *testing.T) {
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "ok"))
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1)
	got[0].Personas[0] = "mutated"
	again, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, []string{"claude"}, again[0].Personas)
}

func TestExportWontfixRecords_MissingStoreIsEmpty(t *testing.T) {
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: filepath.Join(t.TempDir(), "absent")})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestExportWontfixRecords_DoesNotCreateTheStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	_, err := ExportWontfixRecords(WontfixExportOptions{Store: dir})
	require.NoError(t, err)
	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "a read-only export must not create the store")
}

func TestExportWontfixRecords_ReadsTheConfiguredDebtStore(t *testing.T) {
	root, _ := storeConfigRepo(t)
	writeProjectStoreConfig(t, root, "planning/debt")
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	require.NoError(t, localdebt.Append(filepath.Join(root, "planning", "debt"),
		wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "from the configured store")))
	// A record in the default store must not be read once debt.store is set.
	require.NoError(t, localdebt.Append(localdebt.DefaultDir(root),
		wontfixRec(openRec("2026-06-01T10:00:00Z-b", "LOW", "x.go", 1, "other"), "2026-06-02T10:00:00Z", "wontfix", "default store")))

	t.Run("from the working directory", func(t *testing.T) {
		got, err := ExportWontfixRecords(WontfixExportOptions{Diagnostics: io.Discard})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "from the configured store", got[0].Reason)
	})
	t.Run("from a subdirectory", func(t *testing.T) {
		sub := filepath.Join(root, "internal", "x")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		t.Chdir(sub)
		got, err := ExportWontfixRecords(WontfixExportOptions{Diagnostics: io.Discard})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "from the configured store", got[0].Reason)
	})
	t.Run("from an explicit RepoRoot", func(t *testing.T) {
		t.Chdir(t.TempDir())
		got, err := ExportWontfixRecords(WontfixExportOptions{RepoRoot: root, Diagnostics: io.Discard})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "from the configured store", got[0].Reason)
	})
	t.Run("an explicit Store wins over debt.store", func(t *testing.T) {
		got, err := ExportWontfixRecords(WontfixExportOptions{Store: localdebt.DefaultDir(root), Diagnostics: io.Discard})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "default store", got[0].Reason)
	})
}

func TestExportWontfixRecords_InvalidDebtStoreIsAnError(t *testing.T) {
	root, _ := storeConfigRepo(t)
	writeProjectStoreConfig(t, root, "../outside")
	_, err := ExportWontfixRecords(WontfixExportOptions{Diagnostics: io.Discard})
	require.Error(t, err)
}

func TestExportWontfixRecords_RejectsABadStore(t *testing.T) {
	for name, store := range map[string]string{
		"blank":            "   ",
		"system directory": "/etc",
		"unknown user":     "~nosuchuser/debt",
		"traversal":        "../outside",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ExportWontfixRecords(WontfixExportOptions{Store: store, Diagnostics: io.Discard})
			require.Error(t, err)
		})
	}
}

func TestExportWontfixRecords_ExpandsHomeInStore(t *testing.T) {
	isolate(t)
	home := os.Getenv("HOME")
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	require.NoError(t, localdebt.Append(filepath.Join(home, "debt"), wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "home")))
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: "~/debt", Diagnostics: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestExportWontfixRecords_MalformedLineGoesToDiagnostics(t *testing.T) {
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "ok"))
	shards, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, shards)
	f, err := os.OpenFile(shards[0], os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("{not json\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	var diag bytes.Buffer
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: &diag})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.NotEmpty(t, diag.String(), "a skipped line is reported to Diagnostics")
}

func TestExportWontfixRecords_RelativeRepoRoot(t *testing.T) {
	root, _ := storeConfigRepo(t)
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	require.NoError(t, localdebt.Append(localdebt.DefaultDir(root), wontfixRec(a, "2026-06-02T10:00:00Z", "wontfix", "rel")))
	sub := filepath.Join(root, "internal")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)
	got, err := ExportWontfixRecords(WontfixExportOptions{RepoRoot: "..", Diagnostics: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "rel", got[0].Reason)
}

// TestExportWontfixRecords_RejectsARelativeStoreInASystemDirectory pins the
// re-check after filepath.Abs: "." passes the check on the raw value and is
// refused only once it resolves to /etc.
func TestExportWontfixRecords_RejectsARelativeStoreInASystemDirectory(t *testing.T) {
	if _, err := os.Stat("/etc"); err != nil {
		t.Skip("no /etc on this host")
	}
	t.Chdir("/etc")
	_, err := ExportWontfixRecords(WontfixExportOptions{Store: ".", Diagnostics: io.Discard})
	require.Error(t, err)
}

func TestExportWontfixRecords_SortedByIDWhateverTheReadOrder(t *testing.T) {
	var recs []localdebt.Record
	for i, problem := range []string{"zeta", "alpha", "mu", "beta", "omega"} {
		r := openRec("2026-06-01T10:00:00Z-a", "LOW", "internal/x/a.go", i+1, problem)
		recs = append(recs, wontfixRec(r, "2026-06-02T10:00:00Z", "wontfix", problem))
	}
	dir := writeDebtStore(t, recs...)
	got, err := ExportWontfixRecords(WontfixExportOptions{Store: dir, Diagnostics: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, len(recs))
	ids := make([]string, len(got))
	for i, r := range got {
		ids[i] = r.ID
	}
	require.False(t, sort.StringsAreSorted(func() []string {
		written := make([]string, len(recs))
		for i, r := range recs {
			written[i] = r.ID
		}
		return written
	}()), "fixture must be written out of id order")
	assert.True(t, sort.StringsAreSorted(ids), "ids %v are not ascending", ids)
}
