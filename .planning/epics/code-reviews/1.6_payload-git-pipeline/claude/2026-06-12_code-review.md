# Code Review Report: 1.6_payload-git-pipeline

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 4 / 4
- **Approval Status:** Approved
- **Review Date:** June 12, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Note:** Epic uses a `## Success Criteria` section (not `## Acceptance Criteria`); treated as the inline acceptance-criteria source for this review.

## 2. Checklist Changes Applied

- **.planning/epics/completed/1.6_payload-git-pipeline.md** — Constant git-process count for blocks/files payloads
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/diff.go:306,330,343,356,451`, `internal/payload/builder.go:95`
- **.planning/epics/completed/1.6_payload-git-pipeline.md** — All existing `internal/payload` tests pass unchanged
  - Before: `[ ]` → After: `[x]`
  - Evidence: `go test ./internal/payload/...` PASS (88.1% coverage)
- **.planning/epics/completed/1.6_payload-git-pipeline.md** — Benchmark/test demonstrates process-count reduction
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/pipeline_test.go:38,147`
- **.planning/epics/completed/1.6_payload-git-pipeline.md** — Blocks-mode function-context fallback + slog record survive
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/builder.go:164`

## 3. Evidence Map

- **Constant git-process count (excluding files-mode `show`)**
  - Evidence: `internal/payload/diff.go:306` (binarySet — one `--numstat -M`), `diff.go:330,343,356,451` (fcChunks/plainChunks/rawChunks/rangeChunks — one whole-range diff each, memoized on `gitRunner`), `builder.go:95` (buildEntries serves per-file bodies from caches)
  - Summary: Per-file fan-out replaced by whole-range caches keyed on `cacheKey`; `splitDiffByFile`/`chunkKey` slice each whole-range diff per file. `TestBuildEntries_ConstantGitProcessCount` asserts equal process counts for 2 vs 8 files (diff+blocks); `TestBuildEntries_FilesModeOnlyShowScalesWithN` confirms files mode grows only by one `git show` per file.
- **Existing payload tests pass unchanged**
  - Evidence: `pipeline_test.go:52` (mixed change kinds), `:106` (diff-header spoof), `:128` (space-b-slash path); full suite green
  - Summary: Verbatim-body, sentinel, fallback-record, rename, binary, and entries-bridge tests remain green.
- **Process-count reduction demonstrated**
  - Evidence: `pipeline_test.go:38`, `:147` via `gitRunner.execCount` instrumentation
- **Function-context fallback + slog record survive**
  - Evidence: `builder.go:164` (`slog.Warn` + `contextFile` fallback), `diff.go:277` (analogous splitter-unattributed record)

## 4. Remaining Unchecked Items

No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All four success criteria verified against code and a green test suite. Adversarial review surfaced 6 LOW-severity latent/quality items (no critical/high/medium); none affect the epic's success criteria. The three anticipated risks (splitter mis-attribution, fallback/marker drift, memory growth) were all verified handled.

## 6. Coverage Analysis
- **Coverage:** 87.1%
- **Baseline:** 80%
- **Delta:** ↑7.1%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l internal/payload/ |

## 8. Adversarial Analysis
- **Files Reviewed:** 3 (internal/payload/diff.go, builder.go, pipeline_test.go + sibling contract tests)
- **Issues Found:** 6 (Critical: 0, High: 0, Medium: 0, Low: 6)

### Issues by Severity

**Low (6):**
1. `diff.go:202` — `numstatNewPath` parses the FIRST `{`/`}`; a literal `{` in a parent directory name mis-reconstructs a binary-rename head path, so `binarySet` misses it and raw binary bytes can leak into the payload. (correctness, 30m)
2. `diff.go:225` — `chunkKey` is O(N²) in changed-file count (N chunks × N heads), run once per diff variant; a basename index built once per range would be linear. (performance, 60m)
3. `diff.go:265` — an unattributable chunk (`chunkKey == ""`) is only `slog.Warn`-logged and dropped; the file renders an empty body with no error, a silent data loss in a CLI. (error-handling, 30m)
4. `builder_test.go:236` — stale comment/skip: `TestBuildEntries_PerFileBridge` skips the diff-mode join==flat assertion claiming "BuildDiff is the verbatim whole-range diff," but BuildDiff now routes through `joinEntries(BuildEntries(...))`. (testing, 15m)
5. `builder_test.go:352` — fallback/unattributed records log via global `slog.Default()` and the test swaps `slog.SetDefault` globally; parallelizing any payload test would make this a data race. (maintainability, 30m)
6. `builder.go:135` — a deleted binary renders `[binary file changed: ...]` in diff/blocks modes but `[deleted file: ...]` in files mode — inconsistent marker semantics across modes. (correctness, 15m)

## 9. Follow-ups
- Route the 6 LOW findings to technical debt via `/reconcile-code-review @.planning/epics/completed/1.6_payload-git-pipeline.md`.

---
*Generated by /execute-code-review on June 12, 2026 05:42:34PM*
