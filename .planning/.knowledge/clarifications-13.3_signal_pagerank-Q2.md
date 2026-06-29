---
id: mem-2026-06-28-d6d840
question: "What is the intended single source of truth for deduplication between distinctReviewers and addAgreement in the PageRank reconciler?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/pagerank.go, reconcile/merge.go]
tags: [clarifications, epic-13.3_signal_pagerank, architecture, reconcile, pagerank, deduplication, api-design]
retrievals: 0
status: active
type: clarifications /execute-epic 2026-06-28
---

# What is the intended single source of truth for deduplicatio

## Decision

addAgreement is the intended single source of truth for deduplication — it unconditionally deduplicates its input via an internal seen map, making it safe for any caller. The distinctReviewers call in modelAuthority is NOT a second dedup; it is a pre-filter that computes the ≥2 distinct reviewer count needed for the hasAgreement guard before graph mutation. The two calls serve different roles and are not in conflict. If the second call were to be eliminated, the correct refactor is to have addAgreement return a bool (or edge count) so modelAuthority can drop distinctReviewers entirely and let addAgreement own both the dedup and the ≥2 gate — but this is an optional cleanup, not a correctness fix. A clarifying comment at the distinctReviewers call site is the minimum recommended action.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/pagerank.go
- reconcile/merge.go
