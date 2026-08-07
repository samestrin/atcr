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

// An unreadable in-window shard is skipped so the rest stay queryable — but
// the warning must reach the command's stderr (not the process's os.Stderr),
// or the table undercounts findings while looking authoritative, with the
// notice invisible to the cobra harness and any embedded caller.
func TestHistoryCmd_UnreadableShardWarnsOnCommandStderr(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod does not block root")
	}
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	older := time.Now().Add(-40 * 24 * time.Hour) // still inside the default 90d window
	for _, ts := range []time.Time{recent, older} {
		writeHistoryShard(t, root, ts, map[string]any{
			"ts": ts.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
			"id": "s-" + ts.UTC().Format("2006-01"), "file": "p/a.go", "category": "C",
		})
	}
	unreadable := filepath.Join(root, ".atcr", "history", older.UTC().Format("2006-01")+".jsonl")
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) }) // let TempDir removal succeed

	t.Chdir(root)
	cmd := newHistoryCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stderr.String(), "skipping unreadable history shard",
		"the undercount warning must land on the command's stderr")
	assert.Contains(t, stdout.String(), "| Package |",
		"the table still renders from the readable shards")
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

	// Verify merged record ordering: legacy precedes shards in the result of the
	// windowed loader the command itself uses (LoadAllSince over the default
	// window) — the unwindowed LoadAll is never invoked by runHistory.
	defaultWindow, err := history.ParseSince(defaultHistorySinceFlag)
	require.NoError(t, err)
	recs, err := history.LoadAllSince(
		filepath.Join(root, ".atcr", "history"),
		legacyPath,
		defaultWindow,
		time.Now(),
	)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "legacy/pkg", recs[0].Package, "first record should be from legacy")
	assert.Equal(t, "shard/pkg", recs[1].Package, "second record should be from shard")
}

// AC2's "deduplicated consistently", pinned at CLI level: the identical
// (ts, id) record present in BOTH the legacy ledger and a shard must be counted
// once in the rendered table, not twice. (The library-level union test compares
// an ID set, which is structurally blind to duplicates.)
func TestHistoryCmd_DedupesIdenticalLegacyAndShardRecords(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	rec := map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "dup1", "file": "p/a.go", "category": "C",
	}
	writeHistoryLedger(t, root, rec)
	writeHistoryShard(t, root, recent, rec)

	out, err := runHistoryIn(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "| Package |")
	assert.Regexp(t, `\*\*Total\*\*.*\|\s*0\s*\|\s*1\s*\|\s*0\s*\|\s*0\s*\|\s*1\s*\|`, out,
		"the duplicated (ts, id) record must be counted exactly once")
}

func TestHistoryCmd_AbsentHistoryExitsZeroWithMessage(t *testing.T) {
	root := t.TempDir()
	out, err := runHistoryIn(t, root)
	require.NoError(t, err) // absent history is NOT an error (AC3)
	// Assert the DISCRIMINATING substring: a bare "no history" matches both of
	// the command's two empty-result messages and cannot tell which branch ran.
	assert.Contains(t, out, "run 'atcr review' first", "truly-absent history earns the first-run hint")
}

func TestHistoryCmd_EmptyAfterFilterExitsZero(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	writeHistoryLedger(t, root, map[string]any{
		"ts": old, "package": "internal/registry", "severity": "HIGH", "id": "1",
		"file": "internal/registry/x.go", "category": "CORRECTNESS",
	})
	// A 30d window filters out the 100-day-old record; still exit 0 with a message.
	// Assert the DISCRIMINATING substring ("no history" alone matches both
	// empty-result messages), and pin that the first-run hint is NOT shown — a
	// HasAny regression that misclassified this populated ledger as "nothing
	// recorded" would otherwise stay green here.
	out, err := runHistoryIn(t, root, "--since", "30d")
	require.NoError(t, err)
	assert.Contains(t, out, "no history for the selected window")
	assert.NotContains(t, out, "run 'atcr review' first")
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

// An unreadable ledger is a filesystem failure, not a bad invocation: it must
// exit 1, not the usage code (2) CI scripts read as "misconfigured command" —
// the same classification the prune error path already applies nine lines above.
func TestHistoryCmd_UnreadableShardsDirIsFailureNotUsage(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod does not block root")
	}
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "s1", "file": "p/a.go", "category": "C",
	})
	shardDir := filepath.Join(root, ".atcr", "history")
	require.NoError(t, os.Chmod(shardDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(shardDir, 0o700) }) // let TempDir removal succeed

	_, err := runHistoryIn(t, root)
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err),
		"a permission fault on the ledger is not a usage error")
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

// The sibling "nothing to prune" notice is held to the same stream contract: it
// is a diagnostic and must land on stderr, never in a redirected report.md.
func TestHistoryCmd_PruneNothingToRemoveNoticeGoesToStderrNotStdout(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
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

	assert.Contains(t, stderr.String(), "no shards older than the retention horizon")
	assert.NotContains(t, stdout.String(), "no shards", "the nothing-to-prune notice must not corrupt stdout")
	assert.Contains(t, stdout.String(), "| Package |", "stdout still carries the table")
}

// A --prune horizon SHORTER than the report window deletes data the same
// invocation is about to ask for: prune runs first so the report describes what is
// left on disk, so `--prune 30d` under the default 90d window silently removes
// month 2 and 3 and then reports a 90-day window that can no longer contain them.
// The combination is legal — it is a narrowing retention policy — but it must not
// be silent, or the report reads as "no findings in that period" rather than "you
// just deleted that period".
func TestHistoryCmd_WarnsWhenPruneHorizonIsShorterThanReportWindow(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "new1", "file": "p/b.go", "category": "C",
	})

	t.Chdir(root)
	cmd := newHistoryCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// 30d retention against the default 90d report window.
	cmd.SetArgs([]string{"--prune", "30d"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stderr.String(), "shorter than the 90d report window",
		"a horizon inside the report window must be called out before the report is read")
	assert.NotContains(t, stdout.String(), "shorter than", "the warning is a diagnostic — stdout stays a clean table")
	assert.Contains(t, stdout.String(), "| Package |", "stdout still carries the table")
}

// The warning is specific to the overlap, not to --prune in general: a horizon at
// or beyond the report window deletes nothing the report would have shown, so
// warning there would train users to ignore it.
func TestHistoryCmd_NoWarningWhenPruneHorizonCoversReportWindow(t *testing.T) {
	root := t.TempDir()
	recent := time.Now().Add(-2 * 24 * time.Hour)
	writeHistoryShard(t, root, recent, map[string]any{
		"ts": recent.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
		"id": "new1", "file": "p/b.go", "category": "C",
	})

	t.Chdir(root)
	cmd := newHistoryCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--prune", "90d", "--since", "30d"})
	require.NoError(t, cmd.Execute())

	assert.NotContains(t, stderr.String(), "report window",
		"a horizon wider than the window removes nothing the report would show")
}

// A destructive retention horizon must be long enough that it cannot plausibly
// be a mistyped query window. The unit hazard this floor was introduced for is
// gone at the root — the shared timewindow grammar has no clock units, so "30s"
// and "1h" no longer parse at all and `--prune 6m` is unambiguously six months
// (see TestHistoryCmd_PruneMonthsAreMonths below). Sub-floor horizons expressed
// in the accepted units are still a usage error, and nothing is removed.
func TestHistoryCmd_PruneBelowSafetyFloorIsUsageErrorAndDeletesNothing(t *testing.T) {
	for _, horizon := range []string{"1d", "2w", "27d", "30s", "1h"} {
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

// `--prune 6m` means six MONTHS, the only reading a user typing it intends. It
// used to compute a six-MINUTE cutoff (time.ParseDuration's units), which the
// 28-day floor caught only by rejecting the value outright; now it parses to a
// real 180-day horizon and prunes what is genuinely older than that.
func TestHistoryCmd_PruneMonthsAreMonths(t *testing.T) {
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

	_, err := runHistoryIn(t, root, "--prune", "6m")
	require.NoError(t, err, "6m is six months — well above the 28-day floor")
	assert.NoFileExists(t, oldShard, "a 400-day-old shard is outside a six-month horizon")
	assert.FileExists(t, newShard, "a 2-day-old shard is inside a six-month horizon; a 6-MINUTE cutoff would have deleted it")
}

// The prune failure path (cli/history.go: on a PruneShards error the command
// must return a NON-usage error — exit 1, not the exit-2 code CI reads as
// "misconfigured command") is exercised here at CLI level: an unremovable shard
// dir fails the first deletion, so every shard survives and the error names the
// shard that could not be removed.
func TestHistoryCmd_PruneUnremovableShardIsFailureNotUsage(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod does not block root")
	}
	root := t.TempDir()
	old1 := time.Now().Add(-400 * 24 * time.Hour)
	old2 := time.Now().Add(-500 * 24 * time.Hour)
	for _, old := range []time.Time{old1, old2} {
		writeHistoryShard(t, root, old, map[string]any{
			"ts": old.UTC().Format(time.RFC3339), "package": "p", "severity": "HIGH",
			"id": "old-" + old.UTC().Format("2006-01"), "file": "p/a.go", "category": "C",
		})
	}
	shardDir := filepath.Join(root, ".atcr", "history")
	require.NoError(t, os.Chmod(shardDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shardDir, 0o700) }) // let TempDir removal succeed

	_, err := runHistoryIn(t, root, "--prune", "90d")
	require.Error(t, err)
	assert.NotEqual(t, exitUsage, exitCode(err),
		"a filesystem failure mid-prune is not a usage error")
	assert.Contains(t, err.Error(), ".jsonl", "the error must name the shard that could not be removed")
	for _, old := range []time.Time{old1, old2} {
		assert.FileExists(t, filepath.Join(shardDir, old.UTC().Format("2006-01")+".jsonl"),
			"a shard that was never removed must still be on disk")
	}
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
