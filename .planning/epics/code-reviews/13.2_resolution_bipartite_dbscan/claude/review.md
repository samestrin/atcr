# Code Review Stream - 13.2_resolution_bipartite_dbscan (Epic)

**Started:** June 28, 2026 01:28:58PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — Reconciler deduplication pipeline rewritten to use Bipartite Matching
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/bipartite.go:128` (`bipartiteGroups`), `reconcile/bipartite.go:20` (`hungarian` Kuhn-Munkres), `reconcile/dedupe.go:103` (`dedupeCluster` calls `bipartiteGroups`), `reconcile/reconcile.go:78`
- **Notes:** Greedy single-linkage union-find replaced by optimal 1:1 assignment (Hungarian/Kuhn-Munkres) with incremental pairwise N-way reduction. Acceptance gated by integer-exact `mergeable` predicate, not the float cost. Composite edge weight per clarification (AST GroupKey → 0, else 1−Jaccard) in `reconcile/distance.go`.

### Criterion: AC2 — Benchmark tests prove Bipartite Matching resolves known greedy-clustering failure edge cases
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/ac2_greedy_failures_test.go:25` (single-source over-absorb), `:43` (transitive bridge), `:60` (optimal closest pairing)
- **Notes:** Three benchmark tests each document the old greedy output and assert the corrected bipartite partition. Plus `reconcile/dedupe_test.go`, `bipartite_test.go`, `distance_test.go` unit coverage.

### Criterion: AC3 — DBSCAN isolates known model hallucinations into ambiguous.json without arbitrary k
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/dbscan.go:27` (`dbscanLabels`, minPts=2, no k), `reconcile/dedupe.go:175` (cross-source `denseNeighbor`), `reconcile/ac3_dbscan_test.go:9`, `internal/reconcile/ac3_isolation_test.go:18` (end-to-end emit to ambiguous.json)
- **Notes:** DBSCAN params principled (minPts=2 = "corroborated by ≥1 other"; eps-neighborhood = the integer-exact merge boundary, cross-source only). Noise emitted as single-finding `AmbiguousCluster` (Similarity 0) per clarification — debate's `len<2` guard skips it. Double-representation guarded (gray-paired/merged/adjudicated findings excluded from noise).

## Adversarial Analysis (Discovery Mode — no sprint-design.md risk profile)

**Mode:** Full hostile review (2 parallel reviewers over 5 core source files)
**Files Reviewed:** 5 (`bipartite.go`, `dbscan.go`, `distance.go`, `dedupe.go`, `reconcile.go`)
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not available (epic mode)

### Triage notes
- 2 raw findings **rejected** as intended+tested behavior (same-source duplicates not merged — asserted by `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`, cross-source-only by design).
- All 4 claimed-HIGH findings **downgraded to MEDIUM**: ACs met, all tests green, triggers are narrow N≥3 / unattributed / same-source edge cases the benchmark does not cover; none corrupt output or block merge.
- Confirmed `AmbiguousID` collision (same-source identical noise singletons) violates the explicit "design forbids" invariant at `ambiguous.go:30-33` — kept as MEDIUM (narrow trigger, no output corruption, adjudication-handle collision only).

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0 (discovery mode)
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 10

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 4
- Low: 6

### Incomplete sprint items routed to TD
- 0 (all 3 acceptance criteria VERIFIED)
