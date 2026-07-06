Let me analyze the diff carefully for snippet-level bugs.

Let me look at the changes:

1. `cmd/atcr/history.go` - Changed to use `LoadAll` instead of `Load`, with shardDir and legacyPath.

2. `cmd/atcr/resume.go` - Changed `recordResumeHistory` to use `history.ShardPath(filepath.Join(".", ".planning", "history"), ts)` instead of the old `.atcr` path.

3. `cmd/atcr/review.go` - Changed to use `history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)`.

4. `internal/history/shard.go` - New file with `ShardPath`, `LoadShards`, `LoadAll`.

5. `internal/history/shard_test.go` - Tests for the new shard functionality.

Let me look for bugs:

### Inconsistent path handling in resume.go vs review.go

In `review.go`:
```go
histPath := history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)
```
Uses `req.Root` which is ".".

In `resume.go`:
```go
histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
```
Uses "." directly.

In `history.go` (read path):
```go
shardDir := filepath.Join(root, ".planning", "history")
```
Uses `root` from `repoRoot()`.

So `review.go` uses `req.Root` which is "." (CWD-relative), while `history.go` uses `repoRoot()` (resolved). This is the same issue mentioned in the TD README — resume.go uses cwd-relative paths while history.go uses repoRoot(). But wait, this is actually a pre-existing issue noted as MEDIUM in the TD README.

Actually wait — looking more carefully at `review.go`:
```go
req := fanout.ReviewRequest{
    Repo: ".",
    Root: ".",
    ...
}
```

So `req.Root` is ".". And `history.go` uses `repoRoot()`. So if you run `atcr review` from a subdirectory, it writes to `./.planning/history/` but `atcr history` reads from `<repoRoot>/.planning/history/`. This is the same pre-existing issue.

But wait — this is the NEW code in review.go that was changed. The old code was:
```go
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
```

So the old code also used `req.Root`. The bug pre-exists but the new code perpetuates it. The TD README mentions this as a MEDIUM issue for resume.go:213.

Actually, looking at the diff for review.go, the change is from:
```go
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
```
to:
```go
histPath := history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)
```

So `req.Root` is still used. This is consistent with the old behavior. The issue is pre-existing and noted in TD.

### LoadShards glob pattern issue

The TD README already notes:
> `internal/history/shard.go:33` | LoadShards builds a glob pattern from the dir path, so a repo path containing filepath.Glob metacharacters (e.g. a [1] segment) is parsed as a character class and matches no shards

This is noted as a known issue. But I should still flag it if it's in the changed code.

Actually wait — the instructions say "Find problems the author would prefer you don't." The TD README is part of the diff, and it already lists these issues as known/deferred. But my job is to find bugs in the changed code. Let me focus on actual bugs that aren't already documented.

### LoadAll ordering issue

```go
func LoadAll(shardDir, legacyPath string) ([]Record, error) {
    shards, err := LoadShards(shardDir)
    if err != nil {
        return nil, err
    }
    legacy, err := Load(legacyPath)
    if err != nil {
        return nil, err
    }
    return append(shards, legacy...), nil
}
```

`LoadShards` returns records sorted by shard file name (chronological). But `Load` of the legacy file returns records in file order. Then `append(shards, legacy...)` puts all legacy records AFTER all shard records. But the legacy file contains pre-19.4 data, which would be OLDER than the shard data. So the merged result is not chronologically ordered — legacy records (older) appear after shard records (newer).

Is this a bug? Let me check if `Filter` or `RenderTable` depends on ordering...

Looking at the code, `Filter` is called with `time.Now()` and filters by `--since` window and package prefix. If `Filter` just checks timestamps against the window, ordering doesn't matter. But if there's deduplication that depends on order (keeping first or last occurrence), this could be an issue.

The CHANGELOG mentions: "deduplication now keeps the maximum severity for a repeated finding instead of whichever record happened to load first." So dedup keeps max severity, not first/last. So ordering shouldn't matter for dedup.

But wait — is there any dedup in `Filter`? I don't have the `Filter` source code in the diff. Let me not assume.

Actually, the ordering issue (legacy after shards) means the merged history is not chronologically sorted. This could be a MEDIUM issue if anything depends on chronological order, but it might not be a bug if nothing does. Let me flag it as LOW.

### LoadShards error handling

```go
func LoadShards(dir string) ([]Record, error) {
    matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
    if err != nil {
        return nil, fmt.Errorf("globbing history shards: %w", err)
    }
    sort.Strings(matches)
    var all []Record
    for _, path := range matches {
        recs, err := Load(path)
        if err != nil {
            return nil, err
        }
        all = append(all, recs...)
    }
    return all, nil
}
```

The TD README already notes the glob metacharacter issue and the single-unreadable-shard issue. These are known.

### Test issue: writeHistoryShard vs writeHistoryLedger

Looking at the test helpers:

`writeHistoryLedger` (existing, not shown in full but referenced):
```go
func writeHistoryLedger(t *testing.T, root string, lines ...map[string]any) {
    ...
    require.NoError(t, os.WriteFile(filepath.Join(dir, "findings-history.jsonl"), buf.Bytes(), 0o644))
}
```

`writeHistoryShard` (new):
```go
func writeHistoryShard(t *testing.T, root string, ts time.Time, lines ...map[string]any) {
    t.Helper()
    require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
    dir := filepath.Join(root, ".planning", "history")
    ...
}
```

The `writeHistoryShard` creates a `.git` marker but `writeHistoryLedger` doesn't (or maybe it does — I can't see the full implementation). But in `TestHistoryCmd_MergesLegacyAndShards`, both `writeHistoryLedger` and `writeHistoryShard` are called. The `writeHistoryShard` creates `.git`, so `repoRoot()` should resolve. But `writeHistoryLedger` writes to `.atcr/findings-history.jsonl`. Let me check if `writeHistoryLedger` also creates `.git`...

I can see from the diff context that `writeHistoryLedger` is at line 38 (before the diff), and the new `writeHistoryShard` starts at line 41. The `writeHistoryLedger` function body isn't fully shown in the diff, so I can't verify. But the test `TestHistoryCmd_MergesLegacyAndShards` calls `writeHistoryLedger` first, then `writeHistoryShard`. Since `writeHistoryShard` creates `.git`, `repoRoot()` should work. This seems fine.

### resume.go path inconsistency

In `resume.go`:
```go
func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
    histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
```

This uses `"."` (CWD-relative) while `review.go` uses `req.Root` (which is also `"."`). And `history.go` uses `repoRoot()`. So resume and review both write CWD-relative, but history reads via repoRoot(). This is the pre-existing issue noted in TD.

But wait — the TD README says:
> `cmd/atcr/resume.go:213` | recordResumeHistory writes shards to a cwd-relative ./.planning/history while review.go uses req.Root and atcr history reads via repoRoot()

Hmm, but `req.Root` IS `"."` in review.go. So review.go also writes CWD-relative. The TD note says "review.go uses req.Root" as if that's different from CWD-relative, but `req.Root = "."` which is CWD-relative. So actually both review.go and resume.go write CWD-relative, and history.go reads via repoRoot(). The TD note seems to imply review.go is correct, but it's actually the same issue.

Actually, let me re-read the TD note: "recordResumeHistory writes shards to a cwd-relative ./.planning/history while review.go uses req.Root and atcr history reads via repoRoot(), so a resume run from a subdirectory writes shards history never reads"

So the claim is review.go uses `req.Root` (which is "."), and history.go uses `repoRoot()`. If you run from a subdirectory, `req.Root = "."` resolves to the subdirectory, while `repoRoot()` resolves to the repo root. So both review.go and resume.go have the same issue — they write to CWD, not repo root. But the TD note only flags resume.go. This is a discrepancy in the TD note, but the actual code issue exists in both.

However, this is pre-existing (review.go always used `req.Root`), so it's out of scope for the changed lines. The changed line in review.go is just changing the path from `.atcr` to `.planning/history` via `ShardPath`, but the root resolution (`req.Root`) is unchanged.

### Actual bug: `LoadAll` doesn't sort merged results

Let me think about this more carefully. `LoadShards` sorts by filename (chronological for YYYY-MM). But `LoadAll` appends legacy records after shard records. If the legacy file has records from before 19.4 (which it does — it's the pre-19.4 ledger), those records are older than all shard records but appear last in the merged slice.

If `Filter` does any dedup or ordering-dependent logic, this could be a problem. But even without that, if `RenderTable` expects chronological order for trend display, the legacy records would appear out of order.

Actually, I don't have enough context to know if this is a real bug. Let me flag it as LOW.

### Actual bug: `filepath.Glob` with metacharacters

The TD README already documents this, but it IS a real bug in the new code. Let me flag it since it's in the changed lines.

Actually, the instructions say "Find problems the author would prefer you didn't." The TD README is part of the same diff, and the author already documented this. So they know about it. But it's still a bug in the code. Let me flag it.

Wait, but the TD README entries are marked as LOW and are "execute-epic-independent" — meaning they were found independently and are being tracked. The author is aware. Should I still flag it?

The instructions say "Find problems the author would prefer you didn't." If the author already documented it in the TD README in the same diff, they're aware. But my job is to find bugs, not to second-guess what the author knows. Let me flag genuine bugs.

Actually, let me re-read the instructions: "Find problems the author would prefer you didn't." This means I should find bugs that are NOT already documented. If it's in the TD README, the author already knows. So I should focus on undocumented bugs.

Let me look more carefully...

### Test bug: `writeHistoryShard` creates `.git` but `writeHistoryLedger` might not

In `TestHistoryCmd_MergesLegacyAndShards`:
```go
writeHistoryLedger(t, root, ...)  // writes to .atcr/findings-history.jsonl
writeHistoryShard(t, root, ...)   // creates .git and writes shard
```

`writeHistoryShard` creates `.git` so `repoRoot()` works. But does `writeHistoryLedger` create `.git`? I can see from the existing test code (before the diff) that `writeHistoryLedger` is defined at line 38. The diff starts at line 41 for `writeHistoryShard`. So `writeHistoryLedger` is lines 38-40ish. Let me look at the context...

From the diff:
```
@@ -38,6 +38,57 @@ func writeHistoryLedger(t *testing.T, root string, lines ...map[string]any) {
 	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings-history.jsonl"), buf.Bytes(), 0o644))
 }
```

So the line after `writeHistoryLedger` is the closing `}`. The function body isn't fully shown. But looking at the existing tests like `TestHistoryCmd_AbsentHistoryExitsZeroWithMessage`, they call `runHistoryIn(t, root)` which internally calls `repoRoot()`. For `repoRoot()` to work, there needs to be a `.git` directory. The existing `writeHistoryLedger` probably creates `.git` or `.atcr` (since the old code looked for `.atcr`).

Actually, `repoRoot()` probably walks up looking for `.git`. If `writeHistoryLedger` creates `.atcr` but not `.git`, then `repoRoot()` would fail. But the existing tests pass, so `writeHistoryLedger` must create `.git` or something that `repoRoot()` recognizes.

Wait, looking at `writeHistoryShard`:
```go
func writeHistoryShard(t *testing.T, root string, ts time.Time, lines ...map[string]any) {
    t.Helper()
    require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
```

It explicitly creates `.git`. This suggests `writeHistoryLedger` might NOT create `.git`, and the new function needs to because the shard path is under `.planning/history` which doesn't have `.git`-like markers. But in `TestHistoryCmd_MergesLegacyAndShards`, `writeHistoryLedger` is called first (which might not create `.git`), then `writeHistoryShard` is called (which creates `.git`). So the order works. But if `writeHistoryLedger` doesn't create `.git`, then tests that only call `writeHistoryLedger` (like the existing ones) would fail unless `writeHistoryLedger` creates `.git` too.

Since the existing tests pass, `writeHistoryLedger` must create `.git`. So the `.git` creation in `writeHistoryShard` is redundant but harmless when both are called. Not a bug.

### Let me look at the `LoadAll` function more carefully

```go
func LoadAll(shardDir, legacyPath string) ([]Record, error) {
    shards, err := LoadShards(shardDir)
    if err != nil {
        return nil, err
    }
    legacy, err := Load(legacyPath)
    if err != nil {
        return nil, err
    }
    return append(shards, legacy...), nil
}
```

`LoadShards` returns `[]Record` which could be `nil` if no shards exist. `append(nil, legacy...)` works fine in Go — it allocates a new slice. So no nil issue here.

But wait — if `shards` is non-nil and has capacity, `append(shards, legacy...)` might modify the backing array of `shards`. But since `LoadShards` returns a fresh slice each time, this shouldn't be an issue. No aliasing bug.

### Let me look at `ShardPath` and the month format

```go
const shardMonthLayout = "2006-01"

func ShardPath(dir string, ts time.Time) string {
    return filepath.Join(dir, ts.UTC().Format(shardMonthLayout)+".jsonl")
}
```

The format `"2006-01"` produces `2026-07` for July 2026. This is correct. The `.jsonl` suffix is appended correctly.

### Let me look at `LoadShards` sorting

```go
sort.Strings(matches)
```

This sorts file paths lexicographically. For paths like `dir/2026-07.jsonl`, `dir/2026-08.jsonl`, this is chronological. But if `dir` contains paths with different lengths or the dir itself has varying components, this could be wrong. But since all files are in the same dir with `YYYY-MM.jsonl` names, lexicographic sort = chronological. Fine.

### Let me look at the test `TestLoadShards_MergesAllMonthlyFiles`

```go
recs, err := LoadShards(dir)
require.NoError(t, err)
require.Len(t, recs, 3)
ids := map[string]bool{}
for _, r := range recs {
    ids[r.ID] = true
}
assert.Equal(t, map[string]bool{"j1": true, "a1": true, "a2": true}, ids)
```

This checks IDs but not ordering. The test would pass regardless of whether records are sorted. Not a bug in the test, but it doesn't verify the sorting claim.

### Let me look at `recordResumeHistory` in resume.go more carefully

```go
func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
    histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
    if n, err := history.RecordReview(histPath, dir, ts); err != nil {
        log.FromContext(ctx).Warn("failed to append finding history", "error", err)
    } else if n > 0 {
        log.FromContext(ctx).Debug("appended finding history", "records", n, "path", histPath)
    }
}
```

The `dir` parameter is the review directory (where pool findings.txt lives). The `histPath` is computed from `"."` (CWD) and `ts`. This is CWD-relative, which is the pre-existing issue. But the change here is just the path from `.atcr` to `.planning/history` with `ShardPath`. The CWD-relative issue pre-exists.

But wait — there's a subtle issue. The `ShardPath` function creates the path but doesn't create the directory. If `.planning/history/` doesn't exist, `RecordReview` (which calls `Append`) needs to create it. Let me check if `Append` creates parent directories...

I don't have the `Append` source in the diff. But the old code used `filepath.Join(".", ".atcr", "findings-history.jsonl")` — the `.atcr` directory was likely already created by other code. Now with `.planning/history/`, the directory might not exist. If `Append` doesn't `os.MkdirAll` the parent directory, the write will fail.

But this is a non-fatal error (logged and swallowed), so it wouldn't crash. But it would mean history is silently lost on first run. Let me check...

Actually, `RecordReview` likely calls `Append` which likely opens the file with `O_APPEND|O_CREATE`. If the directory doesn't exist, `os.OpenFile` returns an error. The error is logged and swallowed. So the first review run from a fresh repo would silently fail to write history.

But wait — `.planning/` is a tracked directory (per the clarifications), so it likely exists. But `.planning/history/` might not. Let me flag this as a potential issue.

Actually, looking at the test `TestShardPath_RolloverWritesSeparateFiles`:
```go
require.NoError(t, Append(ShardPath(dir, july), []Record{...}))
```

This test calls `Append` directly with a path in a temp dir. If `Append` doesn't create the parent directory, this test would fail. Since the test is expected to pass, `Append` probably does create the parent directory. So this is likely not a bug.

Hmm, but I can't be sure without seeing `Append`'s implementation. Let me not flag this.

### Let me look at the `LoadAll` ordering more carefully

The key question: does `LoadAll` return records in an order that matters?

`LoadShards` returns records sorted by shard file name (chronological). `Load(legacyPath)` returns records in file order (which is append order, so also chronological within the legacy file). But `append(shards, legacy...)` puts ALL legacy records AFTER ALL shard records.

If the legacy file has records from 2026-01 to 2026-06 (pre-19.4), and shards have records from 2026-07 onward, then the merged result would be:
- shard records (2026-07+) first
- legacy records (2026-01 to 2026-06) last

This is REVERSE chronological for the legacy portion. If `Filter` or `RenderTable` depends on chronological order, this is a bug.

But does anything depend on order? Let me think about what `Filter` does. It filters by time window and package prefix. If it just iterates and checks each record's timestamp, order doesn't matter. If it does dedup (keeping first or last occurrence), order matters.

The CHANGELOG says: "deduplication now keeps the maximum severity for a repeated finding instead of whichever record happened to load first." This suggests dedup keeps max severity regardless of order. So order doesn't matter for dedup.

But `RenderTable` renders counts by severity per package. Order shouldn't matter for counting.

So the ordering issue in `LoadAll` is probably not a functional bug, but it's a code smell. Let me flag it as LOW.

Actually, wait. Let me reconsider. The `LoadShards` function sorts by filename and claims "which, for the YYYY-MM naming, is chronological." But `LoadAll` then appends legacy records at the end, breaking that chronological order. This is a logic issue — the code goes to the trouble of sorting shards chronologically, then immediately breaks that ordering by appending older legacy records at the end. This is at minimum a code smell and potentially a bug if any downstream code assumes chronological order.

Let me flag this as MEDIUM — the ordering is wrong and could cause subtle issues.

### Let me look at `resume_test.go` changes

```go
histDir := filepath.Join(".", ".planning", "history")
before, err := history.LoadShards(histDir)
require.NoError(t, err)
require.NotEmpty(t, before, "fresh review should have appended history")
```

This uses `LoadShards` (not `LoadAll`) with a CWD-relative path. The test runs in an isolated git repo (via `initGitRepoWithChange`), so CWD should be the repo root. This seems fine.

But wait — `LoadShards` doesn't read the legacy file. The test is checking that review writes to shards, not that it reads legacy data. This is correct for the test's purpose.

### Let me look at `history.go` changes more carefully

```go
shardDir := filepath.Join(root, ".planning", "history")
legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
recs, err := history.LoadAll(shardDir, legacyPath)
```

This reads both shards and legacy. The `root` comes from `repoRoot()`. This is correct.

But there's a subtle issue: `LoadAll` calls `LoadShards(shardDir)` and `Load(legacyPath)`. If the shard directory doesn't exist, `filepath.Glob` returns empty (no error). If the legacy file doesn't exist, `Load` returns empty (no error). So `LoadAll` returns empty. This is correct — "absent history is not an error."

### Let me look at the glob metacharacter issue more carefully

```go
matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
```

If `dir` contains glob metacharacters like `[`, `*`, `?`, the glob pattern will interpret them. For example, if the repo path is `/home/user/my[1]repo/.planning/history`, the glob pattern becomes `/home/user/my[1]repo/.planning/history/*.jsonl`, where `[1]` is a character class matching `1`. This would fail to match the directory.

This is a real bug, already documented in TD. But it's in the changed code. Let me flag it.

Actually, the TD README says this is "execute-epic-independent" and LOW severity. The author is aware. But it's still a bug in the new code. Let me flag it since my job is to find bugs, not to check what's already documented.

Hmm, but the instructions say "Find problems the author would prefer you didn't." If the author already documented it, they know about it. Let me focus on undocumented bugs.

### Let me look at something else — the `LoadAll` function and `append` behavior

```go
return append(shards, legacy...), nil
```

If `shards` is `nil` (no shards exist) and `legacy` is non-nil, `append(nil, legacy...)` works fine — it allocates a new slice.

If `shards` is non-nil and `legacy` is `nil`, `append(shards, nil...)` works fine — it returns `shards` unchanged.

If both are nil, `append(nil, nil...)` returns nil. Fine.

No nil issue here.

### Let me look at the `TestHistoryCmd_MergesLegacyAndShards` test more carefully

```go
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
    out, err := runHistoryIn(t, root)
    require.NoError(t, err)
    assert.Contains(t, out, "legacy/pkg")
    assert.Contains(t, out, "shard/pkg")
}
```

Both the legacy and shard records use the same timestamp (`recent`). The shard is named after `recent.UTC().Format("2006-01")`, so it's the current month. The `Filter` with `defaultHistorySince = 90 days` would include both. This test seems correct.

But wait — `writeHistoryShard` creates `.git` but `writeHistoryLedger` might create `.atcr`. Does `writeHistoryLedger` also create `.git`? If not, and if `writeHistoryShard` is called after, then `.git` exists. But if `writeHistoryLedger` is called alone (in other tests), `repoRoot()` might fail.

Looking at the test order: `writeHistoryLedger` is called first, then `writeHistoryShard`. `writeHistoryShard` creates `.git`. So by the time `runHistoryIn` is called, `.git` exists. This is fine for this test.

But what about other tests that only call `writeHistoryLedger`? Those are pre-existing tests, not in the diff. They must work, so `writeHistoryLedger` must create `.git` too. Not a bug.

### Let me look at the `TestHistoryCmd_ReadsMonthlyShards` test

```go
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
```

This test creates a shard but no legacy file. `LoadAll` would call `LoadShards` (finds the shard) and `Load(legacyPath)` (legacy doesn't exist, returns empty). The merged result has one record. `Filter` with 90-day window includes it. `RenderTable` outputs a table. The test checks for the package name. This seems correct.

### Let me look at the `resume_test.go` change more carefully

```go
histDir := filepath.Join(".", ".planning", "history")
before, err := history.LoadShards(histDir)
require.NoError(t, err)
require.NotEmpty(t, before, "fresh review should have appended history")
```

After `execCmd(t, "review", "--base", "HEAD^")`, the review should have written to a shard. The test loads shards from `./.planning/history/`. But `review.go` writes to `history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)` where `req.Root = "."`. So the write path is `./.planning/history/YYYY-MM.jsonl`. The test reads from `./.planning/history/` via `LoadShards`. This should work.

But wait — `LoadShards` uses `filepath.Glob(filepath.Join(dir, "*.jsonl"))`. If `dir = "./.planning/history"`, the glob pattern is `./.planning/history/*.jsonl`. This should work. Fine.

### Let me look at the `TestLoadAll_MergesShardsAndLegacy` test

```go
func TestLoadAll_MergesShardsAndLegacy(t *testing.T) {
    root := t.TempDir()
    shardDir := filepath.Join(root, ".planning", "history")
    legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")

    ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
    require.NoError(t, Append(ShardPath(shardDir, ts), []Record{{Timestamp: ts, ID: "shard1", File: "a.go"}}))
    require.NoError(t, Append(legacyPath, []Record{{Timestamp: ts, ID: "legacy1", File: "b.go"}}))

    recs, err := LoadAll(shardDir, legacyPath)
    require.NoError(t, err)
    require.Len(t, recs, 2)
    ids := map[string]bool{}
    for _, r := range recs {
        ids[r.ID] = true
    }
    assert.Equal(t, map[string]bool{"shard1": true, "legacy1": true}, ids)
}
```

This test uses `Append` directly, which must create parent directories (since `shardDir` and `legacyPath`'s parent don't exist yet). If `Append` doesn't `MkdirAll`, this test would fail. Since it's expected to pass, `Append` must create parent dirs. Good — this confirms `Append` creates parent directories, so the `recordResumeHistory` and `runReview` paths won't fail on missing directories.

### Now let me focus on finding actual bugs

Let me re-examine the `LoadAll` function:

```go
func LoadAll(shardDir, legacyPath string) ([]Record, error) {
    shards, err := LoadShards(shardDir)
    if err != nil {
        return nil, err
    }
    legacy, err := Load(legacyPath)
    if err != nil {
        return nil, err
    }
    return append(shards, legacy...), nil
}
```

The ordering issue: shards are sorted chronologically (by filename), but legacy records are appended at the end. Legacy records are older (pre-19.4). So the merged result is NOT chronologically sorted. This could be a problem if downstream code (Filter, RenderTable) expects chronological order.

Let me check if `Filter` cares about order. I don't have the `Filter` source, but the CHANGELOG mentions dedup keeps max severity. If dedup is order-independent (keeps max), then order doesn't matter for correctness. But if there's any first-wins or last-wins logic, order matters.

Actually, even if current code doesn't depend on order, the `LoadShards` function explicitly sorts by filename for chronological order, and `LoadAll` breaks that by appending legacy at the end. This is at minimum a code smell.

Let me flag this as MEDIUM — the ordering is wrong and could cause subtle issues.

### Another look at `resume.go`

```go
func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
    histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
```

The `dir` parameter is passed to `RecordReview` as the review directory. But the `histPath` uses `"."` instead of a resolved root. This is the pre-existing CWD-relative issue. The change from `.atcr` to `.planning/history` is the only new part. The CWD-relative issue pre-exists.

But wait — the TD README says this is a MEDIUM issue at `resume.go:213`. Let me check the line number. The diff shows:

```
@@ -208,19 +208,19 @@ func runResume(cmd *cobra.Command, anchor string) error {
 // recordResumeHistory persists a resumed review's pool findings to the
 // append-only history ledger, mirroring the fresh-review hook in review.go. A
 // history write failure is non-fatal: it must never fail an otherwise-successful
 // resume, so it is logged and swallowed.
 func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
-	histPath := filepath.Join(".", ".atcr", "findings-history.jsonl")
+	histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
```

So line 213 is where `histPath` is assigned. The TD README flags this as MEDIUM. But it's a pre-existing issue (the old code also used `"."`). The change just moves the path. So this is out of scope — the CWD-relative issue pre-exists.

But actually, the change introduces a NEW inconsistency: the old code wrote to `.atcr/findings-history.jsonl` (CWD-relative), and `history.go` read from `repoRoot()/.atcr/findings-history.jsonl`. The new code writes to `.planning/history/YYYY-MM.jsonl` (CWD-relative), and `history.go` reads from `repoRoot()/.planning/history/`. The same CWD-vs-repoRoot mismatch exists, just with a different path. So the bug pre-exists and is just relocated.

### Let me look at something I might have missed

In `review.go`:
```go
histPath := history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)
```

`req.Root` is `"."`. So `histPath` is `./.planning/history/2026-07.jsonl`. And `history.go` reads from `repoRoot()/.planning/history/`. If CWD != repoRoot, the write and read paths differ. Pre-existing issue.

But there's a NEW aspect: the old code wrote to `.atcr/findings-history.jsonl` and the audit ledger also wrote to `.atcr/audit.log.jsonl`. Both were CWD-relative (via `req.Root = "."`). Now the history writes to `.planning/history/` but the audit still writes to `.atcr/`. So the two ledgers are now in different directories. This is by design (per the clarifications), not a bug.

### Let me look at the `now` variable in review.go

```go
now := time.Now()
...
histPath := history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)
if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
```

`now` is captured once and used for both the shard path and the record timestamp. This is correct — all records from a single run go to the same shard.

### Let me look at the `ts` variable in resume.go

```go
func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
    histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)
    if n, err := history.RecordReview(histPath, dir, ts); err != nil {
```

`ts` is used for both the shard path and the record