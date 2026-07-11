# Sprint 19.10: Reviewer Payload Sizing

**Type:** Infrastructure 🏗️ (bugfix characteristics)
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 6–8 days
**Phases:** 5
**Execution Mode:** Gated 🚧 (adversarial ENABLED, inline-fix CRITICAL/HIGH)
**Branch:** `feature/19.10_reviewer_payload_sizing`

---

## Overview

Per-Model Payload Sizing & Graceful Degradation for the atcr multi-agent reviewer. Today a single global byte budget is shipped to a heterogeneous roster whose models span 32k→144k-token windows, so a large diff either gets gutted (files shed against one byte budget) or overflows small-window models entirely. Confirmed in the 19.6 run: a 101-file / 6,429-insertion diff returned **1 finding from 11 reviewers (5 ok, 3 timeout, 3 failed)**.

This sprint sizes each reviewer's payload to its own model's token window (reserving the output-token budget) and, when a payload still doesn't fit, chunks it to fit via the existing Epic 14.3 chunker made window-aware — degrading gracefully through a configurable `on_overflow` policy (`chunk` default → `truncate` → `fallback`/`fail`) instead of silently dropping content.

## Timeline

| Phase | Focus | Tasks | Est. |
|-------|-------|-------|------|
| 1. Foundation | Window resolver + `on_overflow` config surface | 01, 05 | ≈1 day |
| 2. Core Sizing | Effective budget, window-aware chunking, sprint-plan limit | 02, 03, 11 | ≈2 days |
| 3. Overflow & Provenance | Policy dispatch, fallback provenance + reconcile de-weighting | 04, 06, 07 | ≈2 days |
| 4. Integration | Timeout scaling, cache-key correctness, diagnosability | 08, 09, 10 | ≈2 days |
| 5. Validation | Live audit harness + full regression | 12 | ≈1 day + buffer |

Each phase ends with a `N.LAST` phase-boundary gate (fresh-subagent adversarial review); `/execute-sprint` stops at each gate.

## Expected Outcomes

- The reviewer reviews its own 6,400-line sprint without gutting the panel
- No agent hard-fails on context overflow; degradation is visible in `summary.json`, never silent
- Panel model-diversity preserved on the default `chunk` path; any fallback swap recorded (protecting reconcile's distinct-reviewer CONFIDENCE)
- The confirmed `dax` boundary arithmetic (`24577 + 8192 > 32768`) can no longer recur
- The 5 previously-failing agents (`dax`, `otto`, `greta`, `vera`, `brad`) all complete `status=ok` in the AC-Live replay

## Risk Summary (top 3)

1. **Byte→token ratio too optimistic → residual overflow** (Med/High) — mitigated by the conservative ~3.5 B/token ratio (not the codebase's optimistic ~4.1) plus a safety margin; the `on_overflow` net catches the tail.
2. **Chunking a 32k model on a slow backend re-triggers timeouts** (Med/High) — mitigated by co-designing Task 08 (timeout scaling) directly against Task 03's real chunk count.
3. **Cache serves a stale full-payload for a per-agent-sized request** (Med/High) — mitigated by Task 09's explicit cache-key fold-in + a regression test verified to catch a reversion.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — executable phase/task plan (gated)
- [metadata.md](metadata.md) — sprint tracking + complexity/schedule
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest
- [plan/](plan/) — archived plan (original-requirements, sprint-design, plan.md, 12 tasks, documentation)

---

**Next:** `/refine-sprint @.planning/sprints/active/19.10_reviewer_payload_sizing/`
