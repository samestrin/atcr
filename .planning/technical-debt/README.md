# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 6 | 0 |
| LOW | 0 | 1 | 0 |

**Last Modified:** 2026-06-12 | **Open Items:** 0 | **Deferred Items:** 7 | **Resolved Items:** 0 | **Total Items:** 7

## Directory Structure

```
technical-debt/
├── README.md                    # This file (staging area)
├── CLAUDE.md                    # AI assistant guidelines
└── sprints/
    ├── active/                  # Currently being addressed
    ├── pending/                 # Prioritized, not yet started
    └── completed/               # Resolved items
```

## How to Use

1. **Small items**: Add to this README under "Staging Area" below
2. **Larger items**: Create a new document in `sprints/pending/`
3. **During sprint planning**: Move items from pending to active
4. **After resolution**: Move items from active to completed


### [2026-06-11] From Sprint: 1.0_atcr_core

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | MEDIUM | internal/fanout/engine.go:88 | Every non-serial slot spawns its own goroutine with no semaphore or worker-pool bound (intent_note: deferred per sprint-plan TD-017) (Deferred: Epic Plan 1.4) | bound the parallel lane with a buffered semaphore channel sized from a new max_parallel setting | performance | 0 | execute-sprint | execute-sprint | MEDIUM |
| 2 | [/] | MEDIUM | internal/fanout/status.go:48 | ReadReviewStatus reads manifest.json then sources/pool/summary.json in two separate steps while the background fan-out (started by the atcr_review handler) is writing both (intent_note: deferred per sprint-plan TD-023) (Deferred: Epic Plan 1.5) | write a single status.json snapshot at fan-out completion (or a completion sentinel) so status is read from one atomic file instead of two | correctness | 0 | execute-sprint | execute-sprint | MEDIUM |
| 2 | [/] | MEDIUM | internal/fanout/status.go:48 | There is no terminal state for dead reviews: ReadReviewStatus treats absent summary.json as in_progress unconditionally. If ExecuteReview fails after fan-out (WritePool/WriteManifest I/O error returns nil summary, MCP background path only logs to stderr), or the MCP server dies / is killed past the 5s drain while a fan-out may run 600s, summary.json never appears and atcr status reports in_progress forever. The Skill polling loop cannot distinguish a running review from a dead one; .atcr/latest already points at the orphan, poisoning default-anchor calls. (Deferred: Epic Plan 1.5) | On post-fan-out persistence failure write a best-effort failure marker the status reader understands, and in ReadReviewStatus compare manifest StartedAt plus the global timeout (persist effective timeout in the manifest) against now to report a distinct stale/abandoned status. Verify with tests injecting a WritePool failure and a stale manifest with no summary.json. | integration | 60 | code-review | claude | MEDIUM |
| 5 | [/] | LOW | internal/payload/builder.go:114 | blocks/files modes invoke up to 4-5 git processes per changed file (numstat, function-context, context, show, unified=0) (intent_note: deferred per sprint-plan TD-011) (Deferred: Epic Plan 1.6) | batch classification (`--numstat`/`--name-status` once) and split a single diff per file | performance | 0 | execute-sprint | execute-sprint | MEDIUM |
| 9 | [/] | MEDIUM | skill/SKILL.md:33 | [Story 05] DoD item not verified: "Orchestration loop verified end-to-end with a real review run" (AC 05-03 manual gate). The range → review → poll → host review → reconcile → report sequence is verified only by static SKILL.md section tests; no real review run artifacts exist in the repo. (Deferred: Epic Plan 1.7) | Run the full Skill orchestration loop against a real branch with a configured provider and record the review directory path and outcome. | incomplete | 30 | code-review | claude | MEDIUM |
| 9 | [/] | MEDIUM | skill/SKILL.md:61 | [Story 05] DoD item not verified: "Adversarial tone of host review verified in a real review run" (AC 05-04 manual gate). The adversarial personality clause exists in SKILL.md:61-63 and is statically tested, but no real host review output exists to confirm the tone lands in practice. (Deferred: Epic Plan 1.7) | During the real-run verification, inspect sources/host/review.md for adversarial tone and absence of praise, per the AC. | incomplete | 15 | code-review | claude | MEDIUM |
| 9 | [/] | MEDIUM | skill/SKILL.md:96 | [Story 05] DoD item not verified: "Ambiguity adjudication produces sensible merge/distinct decisions" (AC 05-04 manual gate). The adjudication mechanics (stable cluster ids, adjudication.json, idempotent re-runs) are unit-tested, but no real review run with gray-zone clusters has exercised host adjudication quality. (Deferred: Epic Plan 1.7) | During a real review run that yields ambiguous.json clusters, perform host adjudication per SKILL.md:96-113 and review the merge/distinct decisions for sensibility. | incomplete | 30 | code-review | claude | MEDIUM |

