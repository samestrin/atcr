
### Criterion: blocks/files payload for N files issues a constant number of git processes (excluding files-mode `show`)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/diff.go:306` (binarySet — one `--numstat -M`), `diff.go:330,343,356,451` (fcChunks/plainChunks/rawChunks/rangeChunks — one whole-range diff each, memoized), `builder.go:95` (buildEntries serves per-file bodies from caches), test `pipeline_test.go:38`, `pipeline_test.go:147`
- **Notes:** Per-file fan-out replaced by whole-range caches keyed on `cacheKey`; `fileBody` reads from memoized maps. `TestBuildEntries_ConstantGitProcessCount` asserts 2-file and 8-file process counts are equal for diff+blocks; `TestBuildEntries_FilesModeOnlyShowScalesWithN` confirms files mode grows by exactly one `git show` per added file.

### Criterion: All existing `internal/payload` tests pass unchanged (verbatim-body, sentinel, fallback-record, rename, binary, entries-bridge)
- **Verdict:** VERIFIED ✅ (confirmed by Phase 4 test run)
- **Evidence:** `pipeline_test.go:52` (MixedChangeKinds: rename/binary/deleted/added), `pipeline_test.go:106` (diff-header spoof), `pipeline_test.go:128` (space-b-slash path); full `go test ./internal/payload/...` green in Phase 4
- **Notes:** Splitter `splitDiffByFile`/`chunkKey` (diff.go) keys chunks on the `diff --git` header against the known head-path set; per-file chunk is byte-identical to a per-file diff run.

### Criterion: A benchmark or test demonstrates the process-count reduction on a multi-file range
- **Verdict:** VERIFIED ✅
- **Evidence:** `pipeline_test.go:38` `TestBuildEntries_ConstantGitProcessCount` (compares execCount for 2 vs 8 changed files), `pipeline_test.go:147` `TestBuildEntries_FilesModeOnlyShowScalesWithN`
- **Notes:** `gitRunner.execCount` (diff.go) instruments every git subprocess; white-box test asserts constant count.

### Criterion: The blocks-mode function-context fallback and its slog record survive the restructure
- **Verdict:** VERIFIED ✅
- **Evidence:** `builder.go:164` (`slog.Warn("blocks mode: function context unavailable, falling back to plain context diff", ...)` then `g.contextFile` fallback), `diff.go:277` (added analogous splitter-unattributed record)
- **Notes:** `functionContextFile` returns ok=false on zero-hunk chunk (nil error), triggering the recorded fallback; a git failure is propagated, not masked (TD-010 contract preserved).

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md; epic Risks table folded into agent context)
**Files Reviewed:** 3 (internal/payload/diff.go, builder.go, pipeline_test.go + sibling contract tests)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Epic Risks table (3 risks) — agent verified all 3 handled

### Risk Verification Summary
- ✅ Anticipated & Addressed: 3 (splitter mis-attribution sidestepped via whole-range `-M` + longest-suffix key against known head set; fallback/marker drift pinned by tests; memory bounded by byte budget)
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6 (all latent/quality — none block the epic's success criteria)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 6

Notable verified-and-handled (NOT reported): column-0 `diff --git` boundary cannot be spoofed by diff-body lines; longest-suffix chunkKey disambiguates `c.txt` vs `sub/c.txt`; rename pairing preserved; `parseHeadRanges` skips pure-deletion hunks; ref injection blocked by `--end-of-options`; constant-process-count claim holds and is regression-tested.
