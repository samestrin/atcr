# Code Review Stream - 6.2_cluster-id-on-finding (Epic)

**Started:** June 21, 2026 08:51:42PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — JSONFinding carries ClusterID (cluster_id,omitempty); non-merged record byte-identical to pre-6.2
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:118` (field + omitempty tag); `internal/reconcile/emit_test.go:374` (omitted when empty), `internal/reconcile/emit_test.go:384` (round-trips)
- **Notes:** `ClusterID string` with `json:"cluster_id,omitempty"` added. omitempty guarantees absent key for non-merged records. Two tests pin both the omission and the round-trip.

### Criterion: AC2 — applyOneClusterMerge stamps survivor's ClusterID from cluster's ID
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/cluster.go:214` (`merged.ClusterID = c.ID`); `internal/debate/cluster_test.go:88` (asserts survivor carries `amb-1`)
- **Notes:** Stamped alongside `merged.ClusterMerged = true` at the merge survivor.

### Criterion: AC3 — filterMergedClusters suppresses only when a ClusterMerged survivor with same ClusterID present
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/cluster.go:61-81` (builds `mergedIDs` from `ClusterMerged && ClusterID != ""`, suppresses gray-zone item only if its cluster ID is in mergedIDs); `internal/debate/cluster_test.go:469` (legacy empty-ID does not suppress)
- **Notes:** Identity-keyed, not File+Line. Legacy empty ClusterID matches nothing → self-heals on re-debate.

### Criterion: AC4 — two distinct co-located clusters: merging #1 no longer suppresses #2 (over-suppression test inverted)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/cluster_test.go:432` (`TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`) — re-run with amb-1 merged keeps amb-2's item, asserts surviving item is cluster #2
- **Notes:** The former over-suppression pin is inverted exactly as the epic specified.

### Criterion: AC5 — full `go test ./...` green; no regression in cluster-merge idempotency tests
- **Verdict:** VERIFIED ✅
- **Evidence:** Phase 4 `go test -coverprofile ./...` — all 27 packages `ok`, 0 failures; idempotency suite `cluster_test.go:234` passes (debate pkg 88.1%)
- **Notes:** Total coverage 89.4%. No regression.

---

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (Full hostile review, 2 independent reviewers)
**Files Reviewed:** 3 production source files (cluster.go, debate.go, emit.go) + 2 test files for context
**Issues Found:** 8 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic has no sprint-design.md)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 8

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 7

### Notes
- The AC1 schema contract (byte-identical non-merged record) was independently re-verified as CORRECT: omitempty tag right, JSONFindings() structurally cannot stamp ClusterID (Merged struct has no such field), reader round-trips cleanly.
- AmbiguousCluster.ID is a content-addressed sha256 hex (reconcile/ambiguous.go) and stable across runs; the ID derivation is NOT the weak point. The one MEDIUM is a residual edge in the radar-item→cluster *lookup* (keyed on File+Line+longestProblem, not the stable ID), non-corrupting and self-healing — same class as the original TD.
