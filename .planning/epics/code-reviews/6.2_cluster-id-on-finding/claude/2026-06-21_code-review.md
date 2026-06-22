# Code Review Report: 6.2_cluster-id-on-finding

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 21, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **.planning/epics/completed/6.2_cluster-id-on-finding.md** — AC1 JSONFinding carries ClusterID (omitempty)
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/reconcile/emit.go:118`
- **.planning/epics/completed/6.2_cluster-id-on-finding.md** — AC2 stamp ClusterID on merged survivor
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/debate/cluster.go:214`
- **.planning/epics/completed/6.2_cluster-id-on-finding.md** — AC3 identity-keyed filterMergedClusters
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/debate/cluster.go:61-81`
- **.planning/epics/completed/6.2_cluster-id-on-finding.md** — AC4 co-located distinct clusters not over-suppressed
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/debate/cluster_test.go:432`
- **.planning/epics/completed/6.2_cluster-id-on-finding.md** — AC5 full test suite green
  - Before: `[ ]` → After: `[x]`
  - Evidence: `go test ./...` — 27 packages ok, 0 failures

## 3. Evidence Map
- **AC1 — cluster_id,omitempty field**
  - Evidence: `internal/reconcile/emit.go:110-118`, `internal/reconcile/emit_test.go:374`, `internal/reconcile/emit_test.go:384`
  - Summary: Field added with omitempty; non-merged records omit the key (test-pinned both omission and round-trip).
- **AC2 — stamp from cluster ID**
  - Evidence: `internal/debate/cluster.go:209-214`, `internal/debate/cluster_test.go:88`
  - Summary: `merged.ClusterID = c.ID` set alongside `ClusterMerged`; survivor asserted to carry `amb-1`.
- **AC3 — identity-keyed suppression**
  - Evidence: `internal/debate/cluster.go:61-81`, `internal/debate/cluster_test.go:469`
  - Summary: `mergedIDs` built from `ClusterMerged && ClusterID != ""`; suppression keyed on cluster ID; legacy empty-ID suppresses nothing.
- **AC4 — over-suppression test inverted**
  - Evidence: `internal/debate/cluster_test.go:432`
  - Summary: Re-run with amb-1 merged keeps amb-2's item; surviving item asserted to be cluster #2.
- **AC5 — suite green**
  - Evidence: Phase 4 `go test -coverprofile ./...`
  - Summary: All 27 packages ok; total coverage 89.4%; idempotency suite passes.

## 4. Remaining Unchecked Items
No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All 5 acceptance criteria implemented with code + test evidence. Two independent adversarial reviewers confirmed the AC1 schema contract is byte-correct and the ID derivation is stable; findings are quality/robustness improvements, none blocking.

## 6. Coverage Analysis
- **Coverage:** 89.4%
- **Baseline:** 80%
- **Delta:** ↑9.4%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 3 production source files (cluster.go, debate.go, emit.go)
- **Issues Found:** 8 (Critical: 0, High: 0, Medium: 1, Low: 7)

### Issues by Severity

**Medium**
- `internal/debate/cluster.go:74` (correctness) — Residual over-suppression: `indexClusters` keys on File+Line+longestProblem with last-write-wins, so two distinct clusters sharing canonical File+Line AND identical longest-member problem can mis-resolve the item→cluster lookup. Non-corrupting, self-healing on fresh reconcile, narrow. Fix: index value as a slice, or key on the stable `AmbiguousCluster.ID`.

**Low**
- `internal/debate/cluster.go:74` (error-handling) — lookup not self-guarding on `c.ID != ""` (safe only via the producer-side guard).
- `internal/debate/cluster.go:214` (error-handling) — unconditional empty-ID stamp (unreachable from current code; only via malformed ambiguous.json) writes a non-self-healing merged survivor.
- `internal/debate/cluster.go:21` (maintainability) — `clusterDisplayProblem` duplicates `reconcile.longestProblem` (drift surface; currently test-guarded).
- `internal/debate/cluster.go:71` (performance) — full slice copy whenever ≥1 merged ID exists.
- `internal/reconcile/emit.go:66` (maintainability) — JSONFinding doc comment understates the additively-extended schema.
- `internal/reconcile/emit.go:110` (maintainability) — ClusterID comment lacks a cross-reference to debate/cluster.go.
- `internal/reconcile/emit_test.go:371` (testing) — no negative test that `JSONFindings()`/`RenderJSON` never emit cluster_id/cluster_merged.

## 9. Follow-ups
Run `/reconcile-code-review @.planning/epics/completed/6.2_cluster-id-on-finding.md` to merge these findings into the technical-debt README. None of the 8 findings block the epic; the single MEDIUM is a non-corrupting residual edge worth scheduling alongside the 13.2 DBSCAN work where stable cluster IDs emerge organically.

---
*Generated by /execute-code-review on June 21, 2026 08:51:42PM*
