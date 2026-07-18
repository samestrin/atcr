package main

import (
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
