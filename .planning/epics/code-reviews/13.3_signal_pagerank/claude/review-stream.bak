# Code Review Stream - 13.3_signal_pagerank (Epic)

**Started:** June 28, 2026 06:18:29PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — Agreement graph generation implemented during the reconcile resolution phase
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/pagerank.go:24-66` (agreementGraph type + addAgreement/addEdge), `reconcile/pagerank.go:159-173` (modelAuthority builds graph from merge groups), `reconcile/reconcile.go:86-97` (first pass collects allGroups across all clusters, then builds graph)
- **Notes:** Undirected count-weighted graph built run-globally from every merge group with ≥2 distinct reviewers. Matches the epic's "build once per run" two-pass design exactly.

### Criterion: AC2 — Deterministic PageRank calculates authority scores per model
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/pagerank.go:88-151` (damped power method, damping=0.85, epsilon=1e-12, maxIter=1000 backstop, sorted node + neighbor traversal, dangling-mass redistribution)
- **Notes:** Determinism enforced via sorted iteration order (fixes float accumulation order); iteration cap satisfies the epic's convergence-failure risk mitigation. Scores sum to 1; empty graph returns empty map.

### Criterion: AC3 — Confidence scoring logic updated to factor in PageRank weights, producing nuanced Confidence labels
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/pagerank.go:183-192` (promoteByAuthority: MEDIUM→HIGH when sole reviewer authority > 1/N baseline), `reconcile/reconcile.go:103` (promoteByAuthority(Merge(g), authority) in second pass)
- **Notes:** Promote-only (never demotes), reuses existing {LOW,MEDIUM,HIGH,VERIFIED} enum, no wire-schema change. Backward-compat invariant holds: empty authority map / non-isolated / non-MEDIUM findings pass through unchanged (`reconcile/pagerank.go:184`), so pre-13.3 vote-count confidence is byte-identical when no model has differential authority. ConfidenceFor at `reconcile/merge.go:222-227` unchanged.

### Supplementary: Success Criteria (functional)
- **Verdict:** VERIFIED ✅
- **Evidence:** Count-weighted edges (`reconcile/pagerank.go:34-66`) make frequent agreement raise authority; isolated high-authority findings promoted MEDIUM→HIGH (`reconcile/pagerank.go:188-190`) — ranked above isolated low-authority findings which stay MEDIUM.
- **Notes:** Confirmed by golden tests `reconcile/pagerank_confidence_test.go:35-36`.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic)
**Files Reviewed:** 4 (reconcile/pagerank.go, reconcile/reconcile.go, pagerank_test.go, pagerank_confidence_test.go)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic has a Risks table but no machine-readable risk profile)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 3

### Notable
- Determinism story is genuinely solid: sorted node/neighbor traversal fixes float accumulation order; integer out-weight sums are order-independent; `next` allocated once (no per-iteration alloc); mass conservation correct; iteration cap guarantees termination. No O(n^3). Promote-only guard chain correctly ordered.
- The one real correctness concern (baseline strict-`>` float fragility for symmetric N>=4 graphs) is narrowly reachable and promote-only, hence MEDIUM not HIGH — but the fix (epsilon margin) is cheap and hardens the epic's stated backward-compat invariant.
- 2 of 6 findings are test-coverage / doc-accuracy debt, not behavioral defects in shipped code.
