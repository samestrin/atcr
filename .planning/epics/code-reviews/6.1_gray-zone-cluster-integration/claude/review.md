# Code Review Stream - 6.1_gray-zone-cluster-integration (Epic)

**Started:** June 21, 2026 04:49:25PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — judge gray-zone `merge` physically unions cluster findings in findings.json on unattended run (no adjudication.json)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/debate.go:189-210`, `internal/debate/debate.go:257-270`, `internal/debate/cluster.go:75-171`
- **Notes:** When `it.Kind == KindGrayZone` and `ir.ClusterDecision == ClusterMerge`, the cluster is captured (`oc.clusterMerge`) and after the pool drains `applyClusterMerges` unions members directly in the post-verify `findings` slice via `MergeJSONFindings`, flagging the survivor `ClusterMerged`. Applied inline — never through `RunReconcile`, so verify/debate verdicts are preserved (Option A).

### Criterion: AC2 — judge `distinct`/`separate` ruling leaves the cluster unmerged
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/debate.go:201`, `internal/debate/envelope.go:24-25`, `internal/debate/cluster_test.go:111`
- **Notes:** The gray-zone branch only sets `oc.clusterMerge` when `ClusterDecision == ClusterMerge` ("merge"). `ClusterSeparate` ("separate") and any other value fall through with no ruling and no cluster captured → no union. Test asserts no record is flagged `cluster_merged` on a separate ruling.

### Criterion: AC3 — existing adjudication.json path unchanged for inputs without a judge cluster decision
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:101-107` (ClusterMerged `never set by any reconcile-time path`), `internal/reconcile/cluster_merge_test.go`
- **Notes:** The authored adjudication path (`ambiguous.go`/`gate.go`/`dedupe.go`) is NOT in the epic diff — untouched. `MergeJSONFindings` is additive. `ClusterMerged` is only ever set by the debate apply path, so reconcile output for non-cluster inputs is byte-identical (omitempty).

### Criterion: AC4 — re-run idempotency: second debate run does not re-merge or corrupt an already-applied cluster
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/cluster.go:50-68` (filterMergedClusters), `internal/debate/cluster.go:138-147` (strict no-op), `internal/reconcile/emit.go:107` (ClusterMerged flag), `internal/debate/cluster_test.go:229-239`
- **Notes:** Two-layer guard: (1) gray-zone radar items at a `ClusterMerged` location are dropped before debate; (2) `applyOneClusterMerge` returns a strict no-op if any matched record is already `ClusterMerged`. Idempotency test applies twice and asserts the second run is a no-op.

### Criterion: AC5 — Epic 6.0 clarification/AC reconciled with new behavior (updated or explicitly superseded)
- **Verdict:** VERIFIED ✅
- **Evidence:** `.planning/epics/completed/6.0_cross_examination.md` (inline supersession note on the "Unattended CI run" AC + new "## Superseded By" section)
- **Notes:** 6.0's binding "recorded only" clarification is explicitly marked superseded by 6.1; the 6.0 VERIFIED checkbox history is preserved per the recorded decision. Matches Clarifications Q5.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md risk profile)
**Files Reviewed:** 6 (cluster.go, debate.go, merge.go, emit.go + 2 test files for context)
**Issues Found:** 12 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 12

### Issues by Severity (verified)
- Critical: 0
- High: 3
- Medium: 5
- Low: 4

### High-severity findings
1. `internal/debate/cluster.go:132` — drift recovery can absorb an unrelated co-located finding when a cluster member was removed during verify, silently dropping it from findings.json.
2. `internal/debate/debate.go:255` — applyRulings then applyClusterMerges can discard a member's just-applied single-finding debate verdict (ChallengeSurvived/Notes) when the same finding is also a merge-cluster member.
3. `internal/reconcile/merge.go:379` — mergeVerification skeptic dedup treats a comma-joined multi-voter Skeptic string as one key, duplicating voters and emitting an inconsistent separator.

> Note: all 12 are follow-up tech debt on an already-merged, fully-passing epic. None block the epic; the 3 HIGH items are edge-case/audit-fidelity correctness issues worth scheduling.
