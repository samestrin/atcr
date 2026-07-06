package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runHistoryIn(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(root)
	cmd := newHistoryCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// writeHistoryLedger lays down .atcr/findings-history.jsonl with the given
// records (JSON-encoded, one per line).
func writeHistoryLedger(t *testing.T, root string, lines ...map[string]any) {
	t.Helper()
	dir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		require.NoError(t, enc.Encode(l))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings-history.jsonl"), buf.Bytes(), 0o644))
}

// writeHistoryShard lays down a monthly shard .planning/history/<YYYY-MM>.jsonl
// (Epic 19.4) with the given records. It also drops a .git marker so repoRoot()
// resolves to root even when no .atcr tree is present.
func writeHistoryShard(t *testing.T, root string, ts time.Time, lines ...map[string]any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	dir := filepath.Join(root, ".planning", "history")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		require.NoError(t, enc.Encode(l))
	}
	shard := filepath.Join(dir, ts.UTC().Format("2006-01")+".jsonl")
	require.NoError(t, os.WriteFile(shard, buf.Bytes(), 0o644))
}

// AC2: `atcr history` reads monthly shards under .planning/history without the
// caller naming a shard.
func TestHistoryCmd_ReadsMonthlyShards(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "internal/registry", "severity": "HIGH",
		"id": "s1", "file": "internal/registry/a.go", "category": "C",
	})
	out, err := runHistoryIn(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "| Package |")
	assert.Contains(t, out, "internal/registry")
}

// Legacy migration: the pre-19.4 flat .atcr ledger and the new shards are merged
// into one query result.
func TestHistoryCmd_MergesLegacyAndShards(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryLedger(t, root, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "legacy/pkg", "severity": "HIGH",
		"id": "L1", "file": "legacy/pkg/a.go", "category": "C",
	})
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "shard/pkg", "severity": "MEDIUM",
		"id": "S1", "file": "shard/pkg/b.go", "category": "C",
	})
	legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	before, err := os.ReadFile(legacyPath)
	require.NoError(t, err)

	out, err := runHistoryIn(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "legacy/pkg")
	assert.Contains(t, out, "shard/pkg")

	after, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "legacy ledger must not be mutated")

	// Verify merged record ordering: legacy precedes shards in the raw LoadAll result.
	recs, err := history.LoadAll(
		filepath.Join(root, ".planning", "history"),
		legacyPath,
	)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "legacy/pkg", recs[0].Package, "first record should be from legacy")
	assert.Equal(t, "shard/pkg", recs[1].Package, "second record should be from shard")
}

func TestHistoryCmd_AbsentHistoryExitsZeroWithMessage(t *testing.T) {
	root := t.TempDir()
	out, err := runHistoryIn(t, root)
	require.NoError(t, err) // absent history is NOT an error (AC3)
	assert.Contains(t, out, "no history")
}

func TestHistoryCmd_EmptyAfterFilterExitsZero(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	writeHistoryLedger(t, root, map[string]any{
		"ts": old, "package": "internal/registry", "severity": "HIGH", "id": "1",
		"file": "internal/registry/x.go", "category": "CORRECTNESS",
	})
	// A 30d window filters out the 100-day-old record; still exit 0 with a message.
	out, err := runHistoryIn(t, root, "--since", "30d")
	require.NoError(t, err)
	assert.Contains(t, out, "no history")
}

func TestHistoryCmd_FiltersAndRendersTable(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	writeHistoryLedger(t, root,
		map[string]any{"ts": recent, "package": "internal/registry", "severity": "HIGH", "id": "1", "file": "internal/registry/a.go", "category": "C"},
		map[string]any{"ts": recent, "package": "internal/registry", "severity": "MEDIUM", "id": "2", "file": "internal/registry/b.go", "category": "C"},
		map[string]any{"ts": recent, "package": "internal/registry2", "severity": "HIGH", "id": "3", "file": "internal/registry2/c.go", "category": "C"},
		map[string]any{"ts": old, "package": "internal/registry", "severity": "LOW", "id": "4", "file": "internal/registry/d.go", "category": "C"},
	)

	out, err := runHistoryIn(t, root, "--since", "30d", "--package", "internal/registry")
	require.NoError(t, err)
	// Table rendered, scoped to internal/registry (not the sibling registry2),
	// windowed to 30d (the 60-day LOW excluded).
	assert.Contains(t, out, "| Package |")
	assert.Contains(t, out, "internal/registry")
	assert.NotContains(t, out, "registry2")
	// 1 HIGH + 1 MEDIUM in-window for internal/registry, grand total 2.
	assert.Regexp(t, `\*\*Total\*\*.*\|\s*0\s*\|\s*1\s*\|\s*1\s*\|\s*0\s*\|\s*2\s*\|`, out)
}

func TestHistoryCmd_InvalidSinceIsUsageError(t *testing.T) {
	root := t.TempDir()
	writeHistoryLedger(t, root, map[string]any{
		"ts": time.Now().UTC().Format(time.RFC3339), "package": "a", "severity": "HIGH", "id": "1",
	})
	_, err := runHistoryIn(t, root, "--since", "banana")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestHistoryCmd_DefaultSinceWhenUnset(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-1 * 24 * time.Hour).UTC().Format(time.RFC3339)
	writeHistoryLedger(t, root, map[string]any{
		"ts": recent, "package": "a", "severity": "HIGH", "id": "1", "file": "a/x.go", "category": "C",
	})
	// No --since: defaults to a wide window and still renders.
	out, err := runHistoryIn(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "| Package |")
}

func TestHistoryCmd_ResolvesRepoRootFromSubdir(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-1 * 24 * time.Hour).UTC().Format(time.RFC3339)
	writeHistoryLedger(t, root, map[string]any{
		"ts": recent, "package": "a", "severity": "HIGH", "id": "1", "file": "a/x.go", "category": "C",
	})
	sub := filepath.Join(root, "subdir")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	// Run from a subdirectory; the command must walk up to find the .atcr ledger.
	out, err := runHistoryIn(t, sub)
	require.NoError(t, err)
	assert.Contains(t, out, "| Package |")
}
