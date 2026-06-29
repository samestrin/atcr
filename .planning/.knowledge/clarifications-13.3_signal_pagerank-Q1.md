---
id: mem-2026-06-28-73cc3a
question: "Should a float-margin fix (baseline + pageRankEpsilon) be added to the PageRank promotion threshold comparison at reconcile/pagerank.go:188?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/pagerank.go]
tags: [clarifications, epic-13.3_signal_pagerank, architecture, pagerank, float64, threshold, vertex-transitive, won't-fix]
retrievals: 0
status: active
type: clarifications resolve-td 2026-06-28
---

# Should a float-margin fix (baseline + pageRankEpsilon) be ad

## Decision

No — close as won't-fix/no-bug. Vertex-transitive graphs (complete graphs Kn, cycle graphs Cn) have a doubly-stochastic transition matrix whose PageRank steady state is exactly 1/N regardless of damping factor, making spurious promotion structurally impossible for those graph shapes. Empirically confirmed: K2-K8 and C3-C8 all land at EXACTLY 1/N. Adding the epsilon margin would (1) directly contradict the binding Q1 closure that the threshold should not be relaxed, (2) violate the inline design note at pagerank.go:204-207, and (3) conflate two unrelated roles: pageRankEpsilon is the L1 convergence gate for power iteration (not a promotion threshold guard). Rely on the committed invariant tests instead. The strict `> baseline` comparison is correct.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/pagerank.go
