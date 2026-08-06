package cli

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

// writeHistoryShard lays down a monthly shard .atcr/history/<YYYY-MM>.jsonl
// (Epic 35.14) with the given records. It also drops a .git marker so repoRoot()
// resolves to root even when no .atcr tree is present.
func writeHistoryShard(t *testing.T, root string, ts time.Time, lines ...map[string]any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	dir := filepath.Join(root, ".atcr", "history")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		require.NoError(t, enc.Encode(l))
	}
	shard := filepath.Join(dir, ts.UTC().Format("2006-01")+".jsonl")
	require.NoError(t, os.WriteFile(shard, buf.Bytes(), 0o644))
}

// AC2: `atcr history` reads monthly shards under .atcr/history without the
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
		filepath.Join(root, ".atcr", "history"),
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

// Windowed shard selection (Epic 35.14 AC4) means an empty result no longer
// implies an empty ledger: a repo whose only shards fall outside --since loads
// zero records. The first-run hint ("run 'atcr review' first") must not be shown
// in that case — it tells the user their history is missing when it is merely
// out of window.
func TestHistoryCmd_OutOfWindowHistoryReportsWindowNotFirstRun(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "internal/registry", "severity": "HIGH",
		"id": "old1", "file": "internal/registry/a.go", "category": "C",
	})

	out, err := runHistoryIn(t, root, "--since", "30d")
	require.NoError(t, err) // AC6: still exit 0
	assert.Contains(t, out, "no history")
	assert.NotContains(t, out, "run 'atcr review' first",
		"history exists but is out of window — the first-run hint is misleading")
}

// AC6: with nothing recorded anywhere, the first-run hint IS the right message.
func TestHistoryCmd_TrulyAbsentHistoryKeepsFirstRunHint(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	out, err := runHistoryIn(t, root, "--since", "30d")
	require.NoError(t, err)
	assert.Contains(t, out, "run 'atcr review' first")
}

// AC7: pruning is opt-in. Without --prune, a query over a narrow window leaves
// every out-of-window shard on disk — reading history never deletes it.
func TestHistoryCmd_WithoutPruneDeletesNothing(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

	_, err := runHistoryIn(t, root, "--since", "30d")
	require.NoError(t, err)
	assert.FileExists(t, shard, "a read must never delete a shard")
}

// --prune <horizon> removes whole shards older than the horizon and reports what
// it removed, so the deletion is never silent.
func TestHistoryCmd_PruneRemovesOutOfHorizonShards(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "new1", "file": "p/b.go", "category": "C",
	})
	oldShard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")
	newShard := filepath.Join(root, ".atcr", "history", recent.UTC().Format("2006-01")+".jsonl")

	out, err := runHistoryIn(t, root, "--prune", "90d")
	require.NoError(t, err)
	assert.NoFileExists(t, oldShard)
	assert.FileExists(t, newShard)
	assert.Contains(t, out, "pruned")
	assert.Contains(t, out, old.UTC().Format("2006-01"), "the removed shard must be named in the output")
}

// A whitespace-only --prune is a passed-but-degenerate value: the user typed a
// destructive flag, so silently treating it as unset (no parse error, no prune,
// no notice) lets them believe retention was applied. It must fail with the same
// exit-2 usage error as a garbage value like "banana" — and delete nothing.
func TestHistoryCmd_BlankPruneIsUsageErrorAndDeletesNothing(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

	_, err := runHistoryIn(t, root, "--prune", "   ")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.FileExists(t, shard, "a rejected prune must not delete anything")
}

// A --prune horizon is parsed the same way as --since, and a bad one is a usage
// error that deletes nothing.
func TestHistoryCmd_InvalidPruneIsUsageErrorAndDeletesNothing(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

	_, err := runHistoryIn(t, root, "--prune", "banana")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.FileExists(t, shard)
}

// Pruning with nothing past the horizon says so rather than printing nothing,
// and still exits 0.
func TestHistoryCmd_PruneWithNothingToRemoveExitsZero(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "new1", "file": "p/b.go", "category": "C",
	})

	out, err := runHistoryIn(t, root, "--prune", "90d")
	require.NoError(t, err)
	assert.Contains(t, out, "no shards")
}

// Pruning is file-granular and cannot be scoped to a package: a shard holds
// every package's records for its month. Silently ignoring --package next to a
// destructive --prune would let a user delete every package's history believing
// they had scoped the deletion, so the combination is rejected outright.
func TestHistoryCmd_PruneWithPackageIsUsageErrorAndDeletesNothing(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

	_, err := runHistoryIn(t, root, "--prune", "90d", "--package", "p")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "cannot be combined", "the error must name the rejected flag combination")
	assert.FileExists(t, shard, "a rejected prune must not delete anything")
}

// Prune notices are diagnostics, not payload: stdout carries the markdown table
// and nothing else, so `atcr history --prune 365d > report.md` stays a valid
// document.
func TestHistoryCmd_PruneNoticeGoesToStderrNotStdout(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "new1", "file": "p/b.go", "category": "C",
	})

	t.Chdir(root)
	cmd := newHistoryCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--prune", "90d"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stderr.String(), "pruned")
	assert.NotContains(t, stdout.String(), "pruned")
	assert.Contains(t, stdout.String(), "| Package |", "stdout still carries the table")
}

// A destructive retention horizon must be long enough that it cannot plausibly
// be a mistyped query window: ParseSince's fallback is time.ParseDuration, where
// "m" means MINUTES, so `--prune 6m` ("six months") would otherwise compute a
// 6-minute cutoff and irreversibly delete every shard but the current month.
// Sub-floor horizons are a usage error that names the unit meanings, and nothing
// is removed.
func TestHistoryCmd_PruneBelowSafetyFloorIsUsageErrorAndDeletesNothing(t *testing.T) {
	for _, horizon := range []string{"6m", "30s", "1h"} {
		t.Run(horizon, func(t *testing.T) {
			root := t.TempDir()
			old := time.Now().Add(-400 * 24 * time.Hour)
			writeHistoryShard(t, root, old, map[string]any{
				"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
				"id": "old1", "file": "p/a.go", "category": "C",
			})
			shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

			_, err := runHistoryIn(t, root, "--prune", horizon)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err))
			assert.FileExists(t, shard, "a rejected prune horizon must not delete anything")
		})
	}

	// The floor must not reject a genuine long retention horizon.
	t.Run("365d still prunes", func(t *testing.T) {
		root := t.TempDir()
		old := time.Now().Add(-400 * 24 * time.Hour)
		writeHistoryShard(t, root, old, map[string]any{
			"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
			"id": "old1", "file": "p/a.go", "category": "C",
		})
		shard := filepath.Join(root, ".atcr", "history", old.UTC().Format("2006-01")+".jsonl")

		_, err := runHistoryIn(t, root, "--prune", "365d")
		require.NoError(t, err)
		assert.NoFileExists(t, shard)
	})
}

// Pruning everything must not then tell the user to run their first review —
// that would contradict the prune notice printed a line earlier.
func TestHistoryCmd_PruneEverythingDoesNotSuggestFirstRun(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeHistoryShard(t, root, old, map[string]any{
		"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "old1", "file": "p/a.go", "category": "C",
	})

	out, err := runHistoryIn(t, root, "--prune", "90d")
	require.NoError(t, err)
	assert.Contains(t, out, "pruned")
	assert.NotContains(t, out, "run 'atcr review' first")
}
