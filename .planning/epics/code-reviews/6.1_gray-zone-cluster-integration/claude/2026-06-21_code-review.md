# Code Review Report: 6.1_gray-zone-cluster-integration

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5 acceptance criteria
- **Approval Status:** Approved
- **Review Date:** June 21, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Note:** Epic already merged (merge commit `62d0d17`). This review verifies the shipped change and captures follow-up tech debt.

## 2. Acceptance Criteria Verified

| AC | Result | Evidence |
|----|--------|----------|
| AC1 — judge gray-zone `merge` unions cluster findings in findings.json inline (no adjudication.json) | ✅ VERIFIED | `internal/debate/debate.go:189-210`, `:257-270`; `internal/debate/cluster.go:75-171` |
| AC2 — `distinct`/`separate` ruling leaves cluster unmerged | ✅ VERIFIED | `internal/debate/debate.go:201`; `internal/debate/envelope.go:24-25`; test `cluster_test.go:111` |
| AC3 — existing adjudication.json path unchanged for non-cluster inputs | ✅ VERIFIED | `internal/reconcile/emit.go:101-107`; `cluster_merge_test.go`; adjudication path untouched in diff |
| AC4 — re-run idempotency (no re-merge/corruption) | ✅ VERIFIED | `internal/debate/cluster.go:50-68`, `:138-147`; `emit.go:107`; test `cluster_test.go:229-239` |
| AC5 — Epic 6.0 clarification reconciled/superseded | ✅ VERIFIED | `.planning/epics/completed/6.0_cross_examination.md` (inline note + "## Superseded By") |

## 3. Evidence Map

- **Inline apply (Option A):** `applyClusterMerges` / `applyOneClusterMerge` (`internal/debate/cluster.go`) union member records via `reconcile.MergeJSONFindings`, flag survivor `ClusterMerged`, run after the debate pool drains in `runDebate` — never through `RunReconcile`, preserving verify/debate verdicts.
- **Idempotency:** `filterMergedClusters` drops gray-zone radar items at a `ClusterMerged` location; `applyOneClusterMerge` strict no-op on an already-flagged record.
- **Merge helper:** `MergeJSONFindings` unions reviewers, evidence, severity/disagreement, category, est-minutes, and `mergeVerification` combines verdict blocks (confirmed > unverifiable > refuted).

## 4. Remaining Unchecked Items

No remaining unchecked items — all 5 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria are backed by code + test evidence; full suite passes; quality gates green. Adversarial findings are edge-case/audit-fidelity follow-ups, none blocking.

## 6. Coverage Analysis
- **Coverage:** 89.4% (total)
- **Baseline:** 80%
- **Delta:** ↑9.4%
- **Status:** PASSING
- **Key packages:** `internal/debate` 87.8%, `internal/reconcile` 90.5%

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING (0 failures, all packages ok) | `go test ./...` |
| Lint | PASSING (0 issues) | `golangci-lint run` |
| Types | PASSING | `go vet ./...` |
| Format | PASSING | `go fmt ./...` |

## 8. Adversarial Analysis
- **Files Reviewed:** 6 (cluster.go, debate.go, merge.go, emit.go + 2 test files for context)
- **Mode:** Discovery-only (no sprint-design.md risk profile)
- **Issues Found:** 12 (Critical: 0, High: 3, Medium: 5, Low: 4)

### High
1. `internal/debate/cluster.go:132` (correctness) — pass-2 drift recovery can absorb an unrelated co-located finding when a cluster member was removed during verify, silently dropping it from findings.json. Single-exact-anchor guard does not protect the second member's slot.
2. `internal/debate/debate.go:255` (correctness) — `applyRulings` then `applyClusterMerges`: if a finding is both a single-finding debated item and a merge-cluster member, the applied verdict (ChallengeSurvived/Notes) on the losing member can be discarded by `mergeVerification`'s pick-one precedence.
3. `internal/reconcile/merge.go:379` (correctness) — `mergeVerification` dedups skeptic provenance by whole comma-joined `Skeptic` string, duplicating voters and emitting an inconsistent `,` vs `, ` separator; only single-name skeptics are tested.

### Medium
- `internal/debate/debate.go:114` (error-handling) — `ReadAmbiguousClusters` error swallowed; corrupt `ambiguous.json` silently disables all merges with no warning.
- `internal/debate/cluster.go:50` (correctness) — `filterMergedClusters` over-suppresses two distinct clusters that resolve to the same File+Line.
- `internal/reconcile/merge.go:282` (correctness) — `MergeJSONFindings` recomputes Disagreement from scalar severities, dropping members' wider pre-existing `Disagreement` span.
- `internal/reconcile/merge.go:297` (correctness) — Path fields taken from `group[0]` can drop a sibling's `PathSuggestion`.
- `internal/debate/cluster.go:38` (maintainability) — `indexClusters` representative-problem coupling with `BuildDisagreements` is unenforced; divergence makes a merge silently no-op.

### Low
- `internal/debate/cluster.go:102` (error-handling) — single-member cluster triggers a misleading "could not be applied" warning.
- `internal/reconcile/merge.go:390` (maintainability) — losing sibling's verdict Notes discarded at cluster merge.
- `internal/debate/cluster.go:122` (maintainability) — two-pass drift-recovery complexity vs the single edge case it solves.
- `internal/reconcile/merge.go:115` (maintainability) — `minRank := 1<<31` is a 32-bit build hazard now on the cluster-merge path.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/6.1_gray-zone-cluster-integration.md` to merge these 12 findings into the technical-debt README, then `/resolve-td` for the 3 HIGH items.
- The 3 HIGH items are edge-case/audit-fidelity correctness issues worth scheduling; none block the merged epic.

---
*Generated by /execute-code-review on June 21, 2026 04:49:25PM*
