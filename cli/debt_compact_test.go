package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

func TestDebtCompact_RegisteredAndDiscoverable(t *testing.T) {
	cmd := newDebtCmd()
	var hasCompact bool
	for _, c := range cmd.Commands() {
		if c.Name() == "compact" {
			hasCompact = true
		}
	}
	assert.True(t, hasCompact, "debt must own a compact subcommand")

	out, err := runDebt(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "compact", "`atcr debt --help` must list the compact subcommand")
}

func TestDebtCompact_NoStoreReportsNoOp(t *testing.T) {
	dir := t.TempDir() // no store directory contents — Compact no-ops

	out, err := runDebt(t, "compact", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "No local TD store to compact.",
		"a no-op compaction must be distinguishable from a real fold")
	assert.NotContains(t, out, "Compacted ",
		"a no-op must not claim records were folded")
}

func TestDebtCompact_ErrorPathSurfacesWrappedError(t *testing.T) {
	// When Compact's underlying localdebt call fails (e.g., the --dir path is
	// unreachable because a parent component is a file, not a directory), the
	// command must surface a wrapped error that begins with "compact: " —
	// matching the fmt.Errorf("compact: %w", err) in runDebtCompact.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	storePath := filepath.Join(blocker, "store") // unreachable: "blocker" is a file
	_, err := runDebt(t, "compact", "--dir", storePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compact:")
}

// newerSchemaLine builds a raw JSONL line one schema version ahead of this binary,
// i.e. a record written by a newer atcr that this one preserves but cannot fold.
func newerSchemaLine(id string) string {
	return fmt.Sprintf(
		`{"schema_version":%d,"id":%q,"run_id":"2026-06-20T10:00:00Z-r","ts":"2026-06-20T10:00:00Z","severity":"HIGH","file":"n.go","line":1,"problem":"from a newer atcr"}`,
		localdebt.SchemaVersion+1, id)
}

// TestDebtCompact_ReportsPreservedNewerRecords locks the user-facing half of the
// preservation contract for a store this binary can partly fold: the fold counts
// alone would leave a user wondering why the store did not shrink as far as claimed,
// and "go delete something" is the wrong next move.
func TestDebtCompact_ReportsPreservedNewerRecords(t *testing.T) {
	dir := t.TempDir()
	rec := openRec("2026-06-14T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	require.NoError(t, localdebt.Append(dir, rec))

	shard := filepath.Join(dir, "2026-06.jsonl")
	existing, err := os.ReadFile(shard)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(shard,
		append(existing, []byte(newerSchemaLine("future1")+"\n")...), 0o600))

	out, err := runDebt(t, "compact", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Kept 1 record(s) written by a newer atcr version untouched",
		"a preserved record must be reported, not silently absorbed into the fold counts")
	assert.Contains(t, out, "Compacted 1 records into 1",
		"preserved lines are excluded from the fold counts")
}

// TestDebtCompact_PreservedOnlyStoreReportsCoherently locks the case where the
// binary can fold nothing at all. Reporting "No local TD store to compact." here
// would flatly contradict the preserved-record notice printed beside it.
func TestDebtCompact_PreservedOnlyStoreReportsCoherently(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"),
		[]byte(newerSchemaLine("future1")+"\n"), 0o600))

	out, err := runDebt(t, "compact", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "all 1 record(s) were written by a newer atcr version")
	assert.NotContains(t, out, "No local TD store to compact.",
		"claiming there is no store contradicts the records reported in the same breath")
	assert.NotContains(t, out, "Compacted ", "nothing was folded")
}

// TD: compact is the only destructive, irreversible subcommand in the namespace
// (every other one is read-only or append-only) and offered no preview, no
// confirmation and no backup — it went straight to localdebt.Compact, whose
// multi-shard rewrite is acknowledged non-atomic. --dry-run reports exactly what
// a real run would drop and writes nothing.
func TestDebtCompact_DryRunReportsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	rec := openRec("2026-06-14T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	require.NoError(t, localdebt.Append(dir, rec))
	resolved := rec
	resolved.RunID = "2026-06-16T10:00:00Z-resolved"
	resolved.Timestamp = "2026-06-16T10:00:00Z"
	resolved.Status = "resolved"
	require.NoError(t, localdebt.Append(dir, resolved))

	before, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)

	out, err := runDebt(t, "compact", "--dir", dir, "--dry-run")

	require.NoError(t, err)
	assert.Contains(t, out, "Would compact 2 records into 1 (1 superseded dropped).")
	after, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a dry run writes nothing")
}

// "Compacted N records into N (0 superseded dropped)" reads as a mutation that
// did not happen. Nothing superseded means nothing to do — say that instead.
func TestDebtCompact_NothingSupersededIsNotReportedAsAFold(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, localdebt.Append(dir,
		openRec("2026-06-14T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")))

	out, err := runDebt(t, "compact", "--dir", dir)

	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to compact: 1 record(s), none superseded.")
	assert.NotContains(t, out, "Compacted ")
}

// TD: a store whose shards hold nothing decodable is neither "no store" nor a
// fold. It used to reach the same branch as a missing directory (StoreFound was
// false for both), so `atcr debt compact` reported "No local TD store" over a
// shard sitting on disk.
func TestDebtCompact_UnreadableStoreIsNotReportedAsMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"),
		[]byte("{not json\n"), 0o600))

	out, err := runDebt(t, "compact", "--dir", dir)

	require.NoError(t, err)
	assert.NotContains(t, out, "No local TD store to compact.",
		"a shard on disk is a store, whatever this binary can read of it")
	assert.Contains(t, out, "no readable records")
	assert.NotContains(t, out, "Compacted ", "and nothing was folded")
}

func TestDebtCompact_PerformsCompaction(t *testing.T) {
	dir := t.TempDir()

	// Seed some records
	rec1 := openRec("2026-06-14T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec2 := openRec("2026-06-15T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak")

	require.NoError(t, localdebt.Append(dir, rec1))
	require.NoError(t, localdebt.Append(dir, rec2))

	// Resolve rec1 multiple times
	now := "2026-06-16T10:00:00Z"
	for i := 0; i < 3; i++ {
		resolved := rec1
		resolved.RunID = now + "-resolved"
		resolved.Timestamp = now
		resolved.Status = "resolved"
		resolved.ResolvedAt = now
		require.NoError(t, localdebt.Append(dir, resolved))
	}

	// Verify count before compaction
	recsBefore, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recsBefore, 5) // 2 open + 3 resolved

	// Run compact subcommand
	out, err := runDebt(t, "compact", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Compacted 5 records into 2 (3 superseded dropped).",
		"a real fold must report before/after/dropped counts, not a bare success line")

	// Verify count after compaction
	recsAfter, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	// Must only have:
	// - 1 resolved record for rec1
	// - 1 open record for rec2
	require.Len(t, recsAfter, 2)

	// Verify we can still run debt list or resolve list on it
	resolveOut, err := runDebt(t, "resolve", "--dir", dir, "--list")
	require.NoError(t, err)
	assert.Contains(t, resolveOut, "internal/y/b.go", "open finding 2 must still be listed")
	assert.NotContains(t, resolveOut, "internal/x/a.go", "resolved finding 1 must not be listed")
}
