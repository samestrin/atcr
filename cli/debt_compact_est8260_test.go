package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// TestDebtCompact_NoStoreCreatesNothing is the EST-8260 probe: with no store,
// `debt compact` printed "No local TD store to compact." but created the store
// directory (a mistyped --store too) and a .compact-watermark in it.
func TestDebtCompact_NoStoreCreatesNothing(t *testing.T) {
	for _, args := range [][]string{nil, {"--dry-run"}} {
		missing := filepath.Join(t.TempDir(), "typo", "debt")
		out, err := runDebt(t, append([]string{"compact", "--store", missing}, args...)...)
		require.NoError(t, err)
		assert.Contains(t, out, "No local TD store to compact.")
		_, statErr := os.Lstat(filepath.Dir(missing))
		assert.True(t, os.IsNotExist(statErr), "args %v: the store path must not be created", args)

		empty := t.TempDir()
		_, err = runDebt(t, append([]string{"compact", "--store", empty}, args...)...)
		require.NoError(t, err)
		entries, rerr := os.ReadDir(empty)
		require.NoError(t, rerr)
		assert.Empty(t, entries, "args %v: nothing may be written into an empty store dir", args)
	}
}

// TestDebtCompact_ReportsDroppedMalformedLines is the second probe finding:
// compact said "Nothing to compact: 2 record(s), none superseded" while its
// rewrite dropped 4 malformed lines, and --dry-run did not mention them.
func TestDebtCompact_ReportsDroppedMalformedLines(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, localdebt.Append(dir, openRec("2026-06-14T10:00:00Z-a", "HIGH", "a.go", 1, "one")))
	require.NoError(t, localdebt.Append(dir, openRec("2026-06-14T10:00:01Z-b", "HIGH", "b.go", 2, "two")))
	shard := filepath.Join(dir, "2026-06.jsonl")
	data, err := os.ReadFile(shard)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(shard, append(data, []byte("{bad\nnot json\n[1,2]\n{\"id\":\n")...), 0o600))

	out, err := runDebt(t, "compact", "--store", dir, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Would remove 4 malformed line(s)")
	assert.NotContains(t, out, "Nothing to compact", "a run that rewrites the shard is not a no-op")

	out, err = runDebt(t, "compact", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Removed 4 malformed line(s)")
	assert.NotContains(t, out, "Nothing to compact")

	out, err = runDebt(t, "compact", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to compact: 2 record(s), none superseded.", "the second run is a true no-op")
}
