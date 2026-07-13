# Code Review Report: 22.4_grounding_gitrunner_reuse

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 3 / 3
- **Approval Status:** Approved
- **Review Date:** July 13, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **.planning/epics/completed/22.4_grounding_gitrunner_reuse.md** – computeGroundingData no longer spawns its own git subprocesses when a memoized diff is available
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/review.go:411-431`, `internal/payload/rangebuilder.go:64-72`
- **.planning/epics/completed/22.4_grounding_gitrunner_reuse.md** – Grounding behavior unchanged (pure performance fix)
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/grounding.go:41-54`, `internal/payload/rangebuilder_test.go:47-70`
- **.planning/epics/completed/22.4_grounding_gitrunner_reuse.md** – Subprocess-count assertion confirms the reduction
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/rangebuilder_test.go:16-38`, `internal/payload/diff.go:95`

## 3. Evidence Map
- **AC1 — grounding reuses payload builder's runner**
  - Evidence: `internal/fanout/review.go:411-431`, `internal/fanout/review.go:752-777` (`buildPayloads` returns shared `rb`), `internal/payload/rangebuilder.go:64-72`, `internal/payload/diff.go:479-493`
  - Summary: `computeGroundingData` now accepts `rb *payload.RangeBuilder`; when non-nil it calls `rb.BuildChangedLines()`, which reuses the memoized validateRange + `--name-status` (and `--unified=0` when a files-mode payload was built). The diff-ingestion path passes `nil` and hits the range-less early return.
- **AC2 — grounding semantics unchanged**
  - Evidence: `internal/payload/grounding.go:41-54`, `internal/payload/rangebuilder_test.go:47-70`
  - Summary: Standalone `BuildChangedLines` and `RangeBuilder.BuildChangedLines` both funnel through the same runner-bound `changedLines` core; `TestRangeBuilder_ChangedLinesMatchesStandalone` asserts byte-for-byte equality. All `internal/payload` and `internal/fanout` tests pass unchanged.
- **AC3 — subprocess-count assertion**
  - Evidence: `internal/payload/rangebuilder_test.go:16-38`, `internal/payload/rangebuilder_test.go:110-134`, `internal/payload/diff.go:95`
  - Summary: `execCount++` sits in the single `output()` git-invocation choke-point, so the equality assertion after a files-mode payload build genuinely proves zero added subprocesses.

## 4. Remaining Unchecked Items
No remaining unchecked items — all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Tightly-scoped, well-tested pure-performance refactor. New `RangeBuilder` methods at 100% coverage; no correctness, security, concurrency, or error-handling regressions found. Reuse mechanism (shared `gitRunner` + `zeroCtx` memoization) is sound and used strictly sequentially.

## 6. Coverage Analysis
- **Coverage:** 88.9%
- **Baseline:** 80%
- **Delta:** ↑8.9%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l (epic files) |

## 8. Adversarial Analysis
- **Files Reviewed:** 7
- **Issues Found:** 4 (Critical: 0, High: 0, Medium: 1, Low: 3)

### Issues by Severity
- **MEDIUM — maintainability** `internal/payload/grounding.go:44` — Docstrings and the AC3 test claim grounding adds zero git subprocesses after any payload build, but only files-mode populates the `zeroCtx` cache. The default roster mode is `blocks` (`registry/project.go:16`), which never calls `zeroCtxChunks`, so grounding still spawns one `--unified=0` subprocess under the default config. Reuse elides validateRange + `--name-status` in every mode; the zero-context diff only in files-mode. Fix: qualify docstrings; add a blocks-mode test asserting `execCount` delta == 1.
- **LOW — maintainability** `internal/payload/rangebuilder.go:54` — Payload-mode validation string duplicated a 4th time (drift risk). Extract `validatePayloadMode`.
- **LOW — maintainability** `internal/fanout/review.go:411` — `computeGroundingData` grounds from `rb`'s own base/head; the coupling with `req.Range` is now implicit and unenforced. Add an assertion or doc contract.
- **LOW — testing** `internal/fanout/review.go:394` — No fan-out-level test asserting rb-threaded `PreparedReview.Changed` equals the standalone `BuildChangedLines` output. Add one plus a diff-ingestion range-less assertion.

Note: the epic already captured 2 distinct LOW items during execution (cache retention / peak heap at `review.go:756`; missing runtime concurrency guard at `rangebuilder.go:22`). The 4 findings above are net-new.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/22.4_grounding_gitrunner_reuse.md` to merge these findings into the TD README with reviewer attribution.
- Consider addressing the MEDIUM docstring/test-accuracy item so the epic's "zero subprocess" claim matches default-roster behavior.

---
*Generated by /execute-code-review on July 13, 2026 03:35:05PM*
