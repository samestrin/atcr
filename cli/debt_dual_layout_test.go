package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// Epic 92 T2: resolve hydration, compact and backfill read both store layouts.

// writeEventStore writes recs into a fresh store; the records from splitAt on land
// in event files (splitAt 0 is an event-only store).
func writeEventStore(t *testing.T, splitAt int, recs ...localdebt.Record) string {
	t.Helper()
	dir := t.TempDir()
	for i, r := range recs {
		// openRec stamps the run id as the timestamp; an event file name needs the
		// RFC3339 instant it starts with.
		if z := strings.IndexByte(r.Timestamp, 'Z'); z >= 0 {
			r.Timestamp = r.Timestamp[:z+1]
		}
		if i == splitAt {
			require.NoError(t, localdebt.EnableEventLayout(dir))
		}
		require.NoError(t, localdebt.Append(dir, r))
	}
	return dir
}

func TestSelectOpenDebt_HydratesAcrossBothLayouts(t *testing.T) {
	first := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	second := first
	second.RunID = "2026-07-01T10:00:00Z-b"
	second.Timestamp = "2026-07-01T10:00:00Z"
	second.Severity = "LOW"
	other := openRec("2026-07-02T10:00:00Z-c", "MEDIUM", "internal/y/b.go", 3, "leak")

	for name, split := range map[string]int{"event-only": 0, "mixed": 1} {
		t.Run(name, func(t *testing.T) {
			dir := writeEventStore(t, split, first, second, other)
			got, err := selectOpenDebt(dir, "", 0, localdebt.ReadOpts{Writer: io.Discard})
			require.NoError(t, err)
			require.Len(t, got, 2, "both ids hydrate from event files")
			bySev := map[string]string{}
			for _, r := range got {
				bySev[r.ID] = r.Severity
			}
			assert.Equal(t, "LOW", bySev[first.ID], "the event file's later occurrence wins over the shard's")
			assert.Equal(t, "MEDIUM", bySev[other.ID])
		})
	}
}

func TestDebtResolve_ResolvesAnIDHeldInEventFiles(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeEventStore(t, 0, rec)

	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "Marked "+rec.ID)

	out, err = runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "already closed", "the resolution event is read back")
}

func TestDebtCompact_EventOnlyStoreExplainsTheNoOp(t *testing.T) {
	a := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	b := a
	b.RunID, b.Timestamp, b.Status = "2026-07-02T10:00:00Z-r", "2026-07-02T10:00:00Z", "resolved"
	for _, dry := range []bool{false, true} {
		dir := writeEventStore(t, 0, a, b)
		args := []string{"compact", "--dir", dir}
		if dry {
			args = append(args, "--dry-run")
		}
		out, err := runDebt(t, args...)
		require.NoError(t, err)
		assert.Contains(t, out, "Nothing to compact: all 2 record(s) are event files", "dry=%v", dry)
		assert.NotContains(t, out, "No local TD store")
		assert.NotContains(t, out, "ompact 2 records")
		files, err := filepath.Glob(filepath.Join(localdebt.ItemsDir(dir), "*.json"))
		require.NoError(t, err)
		assert.Len(t, files, 2, "event files are kept")
	}
}

func TestDebtCompact_MixedStoreSaysEventFilesWereLeftAlone(t *testing.T) {
	a := openRec("2026-06-14T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	ra := a
	ra.RunID, ra.Timestamp, ra.Status = "2026-06-16T10:00:00Z-r", "2026-06-16T10:00:00Z", "resolved"
	c := openRec("2026-07-01T10:00:00Z-c", "LOW", "internal/y/b.go", 3, "leak")
	dir := writeEventStore(t, 2, a, ra, c)

	out, err := runDebt(t, "compact", "--dir", dir, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Would compact 3 records into 2 (1 superseded dropped).")
	assert.Contains(t, out, "Left 1 record(s) in event files untouched")
}

func TestDebtBackfillJustifications_ReportsEventFileSkips(t *testing.T) {
	store, reviewRoot := backfillFixture(t)
	// Move the fixture's one record into an event file.
	b, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(store, "2026-08.jsonl")))
	require.NoError(t, localdebt.EnableEventLayout(store))
	ev := filepath.Join(localdebt.ItemsDir(store), "aaaa1111.20260801T000000.000000000Z-000000000000.json")
	require.NoError(t, os.WriteFile(ev, b, 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications", "--store", store, "--review-root", reviewRoot)
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "1 left unrepaired (held in event files, which are never rewritten)")
	after, err := os.ReadFile(ev)
	require.NoError(t, err)
	assert.Equal(t, string(b), string(after))
}

func TestDebtBackfillJustifications_ShardStoreOutputUnchanged(t *testing.T) {
	store, reviewRoot := backfillFixture(t)
	code, out := execCmdCapture(t, "debt", "backfill-justifications", "--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	assert.NotContains(t, out, "event files", "a month-shard store's report does not change")
}
