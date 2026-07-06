
### Criterion: AC1 — `atcr review` appends new records into a `YYYY-MM.jsonl` file under `.planning/history/`
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/history/shard.go:21` (`ShardPath(dir, ts)` → `<dir>/<ts.UTC() "2006-01">.jsonl`), `cmd/atcr/review.go:404` (`history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)`), `cmd/atcr/resume.go:213` (same for resume)
- **Notes:** Both write call sites route findings into the current month's shard under `.planning/history/`. `Append` (writer.go) MkdirAll's the parent, so the tracked dir is created on first write.

### Criterion: AC2 — `atcr history` queries across multiple sharded files without specifying which shard
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/history.go:50-52` (`LoadAll(shardDir, legacyPath)`), `internal/history/shard.go:32` (`LoadShards` globs `*.jsonl`, sorts, merges), `internal/history/shard.go:57` (`LoadAll` = shards + legacy)
- **Notes:** The command passes only the shard dir; the reader globs every `*.jsonl` and merges before the existing `Filter`/`RenderTable`. No shard name required.

### Criterion: AC3 — Older monthly shards stop receiving writes once the month rolls over
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/history/shard.go:21` (filename derived from `ts.UTC().Format("2006-01")`), test `internal/history/shard_test.go` `TestShardPath_RolloverWritesSeparateFiles`
- **Notes:** Because the shard name is a pure function of the run timestamp's UTC month, a run in a new month targets a new file and never reopens a prior month's shard — so old shards stop producing new git blobs. Rollover proven by test (July + August writes → two distinct files, July shard unchanged).

### Criterion: AC4 — All existing `cmd/atcr/history_test.go` and `internal/history/*_test.go` tests pass with the new layout
- **Verdict:** VERIFIED ✅
- **Evidence:** Full suite run in Phase 4 (`go test ./...`); legacy read path (`LoadAll` merges `.atcr/findings-history.jsonl` in place) keeps the existing `history_test.go` fixtures (which write to `.atcr`) queryable
- **Notes:** Confirmed green in Phase 4. Existing tests pass because the legacy flat ledger is still read; new shard tests added alongside.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no risk profile — epic)
**Files Reviewed:** 5 (internal/history/shard.go, internal/history/record.go, cmd/atcr/history.go, cmd/atcr/review.go, cmd/atcr/resume.go)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 3

**Notes:** Security surface is clean — shard names derive from `ts.UTC()` (no injection), the glob is dir-scoped (no traversal), no new sensitive fields. The 3 MEDIUM findings (cwd-relative writes vs repoRoot reads; LoadShards aborting on one bad shard; no `--since` window pruning) overlap in theme with items already captured to the TD README during /execute-epic — reconcile will dedup and attribute. The cwd-relative finding was proposed HIGH by the adversarial agent; recorded MEDIUM here because the whole review command is documented to run at repo root (Repo/Root="."), the mismatch is pre-existing (the old `.atcr` path had it), and the epic only changed the misrouted files from gitignored to tracked.
