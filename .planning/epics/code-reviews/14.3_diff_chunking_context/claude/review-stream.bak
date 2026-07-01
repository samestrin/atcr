# Code Review Stream - 14.3_diff_chunking_context (Epic)

**Started:** July 01, 2026 10:44:57AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — Toggle between `bulk` (default) and `chunked` review strategies
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/project.go:21` (DefaultReviewStrategy="bulk"), `internal/registry/config.go:402` (Registry.ReviewStrategy), `internal/registry/project.go:47` (ProjectConfig.ReviewStrategy), `internal/registry/precedence.go:127,156,165,227` (resolve + defense-in-depth revalidation), `internal/registry/review_strategy.go:10-18` (enum + validity)
- **Notes:** Run-wide flag resolved once per run (registry→project→embedded), default `bulk`. Validated at load and re-validated post-resolution. Matches the recorded clarification (run-wide on Settings).

### Criterion: AC2 — `max_context_lines` field on `AgentConfig` with a reasonable default
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:346` (AgentConfig.MaxContextLines *int), `config.go:36` (DefaultMaxContextLines=1500), `config.go:42` (MaxContextLinesCap=1_000_000), `config.go:714-716` (validation 1..cap), `config.go:942-947` (EffectiveMaxContextLines)
- **Notes:** Pointer distinguishes unset (inherit 1500) from explicit override. Default 1500 matches the recorded clarification. Backward-compatible.

### Criterion: AC3 — Bin-packing that groups files into chunks without exceeding `max_context_lines`
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/chunker.go:82-110` (chunkDiff greedy next-fit), `chunker.go:49-70` (splitDiffFiles on `diff --git a/` boundary), `chunker.go:25` (countLines via strings.Count), `internal/fanout/review.go:820-864` (chunked branch in buildSlots emits one slot per chunk via renderAgent)
- **Notes:** Single file never split; a lone file exceeding the budget becomes its own oversized chunk with a stderr warning (review.go:838-842). Implements its own split per the epic Technical Constraint (no internal/payload coupling). Next-fit preserves file order.

### Criterion: AC4 — Reconciler merges findings from multiple chunks of the same persona
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/chunker.go:122-226` (mergeChunkResults / mergeResultGroup), `internal/fanout/review.go:570` (invoked in runEngine after Engine.Run, before writePool at review.go:625)
- **Notes:** Option A (aggregate-at-source): N chunk results collapse into one result per persona with plain `Reviewer=<persona>`, so writePool's duplicate-dir guard is satisfied and the already-merged 14.2 consensus filter (counts distinct Reviewer) treats the persona as ONE voter. Merge precedes artifact write — ordering correct. Usage/telemetry accumulated across chunks; partial-chunk-failure surfaced on stderr.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (full hostile review; no sprint-design risk profile)
**Files Reviewed:** 6
**Issues Found:** 12 (verified from TD_STREAM; 1 raw finding dropped as duplicate of existing TD README line 69)
**Risk Profile:** Not Available (epic mode)

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 4
- Low: 7

### Notable
- HIGH: partial-chunk-failure reports StatusOK with only a stderr signal — CI gate false-green over unreviewed diff (chunker.go:212).
- Several LOW items from the epic's own independent review are already tracked in .planning/technical-debt/README.md (marker/combined-diff, oversize-warning re-emit, truncation metadata, DurationMS serial-lane). The dropped DurationMS-max finding duplicated README line 69.
