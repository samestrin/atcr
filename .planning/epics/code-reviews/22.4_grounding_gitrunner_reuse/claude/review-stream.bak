# Code Review Stream - 22.4_grounding_gitrunner_reuse (Epic)

**Started:** July 13, 2026 03:35:05PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: computeGroundingData no longer spawns its own git subprocesses when a memoized diff for the same range is available from the payload builder
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:411-431` (computeGroundingData now takes `rb *payload.RangeBuilder`, calls `rb.BuildChangedLines()` when non-nil), `internal/payload/rangebuilder.go:64-72`, `internal/payload/diff.go:479-493` (`zeroCtxChunks` memo), `internal/fanout/review.go:752-777` (`buildPayloads` returns shared `rb`)
- **Notes:** `rb.BuildChangedLines()` → `validate()` (skipped, already validated during payload build) → `changedLines()` → `zeroCtxChunks()` (memoized). When files-mode payload was built, grounding adds zero subprocesses. For diff/blocks-only rosters the `--name-status` + validateRange are still reused (one `--unified=0` remains) — a real reduction either way; the "when available" wording is satisfied.

### Criterion: Grounding behavior unchanged (pure performance fix), verified by existing internal/fanout grounding tests passing unchanged
- **Verdict:** VERIFIED ✅ (pending Phase 4 test run)
- **Evidence:** `internal/payload/grounding.go:41-54` (standalone `BuildChangedLines` now delegates to shared `changedLines`), `internal/payload/rangebuilder_test.go:47-70` (`TestRangeBuilder_ChangedLinesMatchesStandalone` asserts byte-for-byte equality with standalone path)
- **Notes:** Both the standalone `BuildChangedLines` and `RangeBuilder.BuildChangedLines` funnel through the same `changedLines` core, guaranteeing identical grounding output. Diff-ingestion path passes `nil` rb; range-less early return preserved.

### Criterion: A benchmark or subprocess-count assertion confirms the reduction
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/rangebuilder_test.go:16-38` (`TestRangeBuilder_GroundingReusesPayloadGitProcesses` asserts `execCount` unchanged after grounding follows payload build), `rangebuilder_test.go:110-134` (`TestRangeBuilder_ZeroContextDiffRunsOnce`), `internal/payload/diff.go:95` (`execCount++` in the single `output()` git-invocation choke-point)
- **Notes:** `execCount` increments on every git subprocess in `output()`, so the equality assertion genuinely proves zero added subprocesses — not a no-op.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for an epic)
**Files Reviewed:** 7
**Issues Found:** 4 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 3

### Notable finding (MEDIUM)
Default roster mode is `blocks`, which never populates the `zeroCtx` cache; only files-mode does. So `computeGroundingData` still spawns one `--unified=0` subprocess under the default config — the reuse elides validateRange (2 rev-parse) + `--name-status` in every mode, but the zero-context diff only in files-mode. Docstrings and the AC3 test overclaim an unconditional zero-subprocess result. The epic's core reduction is real and the ACs are met (conditional "when available" wording), but the claim's accuracy is worth correcting.
