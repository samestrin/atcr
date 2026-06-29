---
id: mem-2026-06-28-da5e5f
question: "Should raw-reviewer-name authority keying in PageRank be treated as accepted-risk for v1 or deferred to a v2 epic for authenticated source identity?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/pagerank.go, reconcile/merge.go]
tags: [clarifications, epic-13.3_signal_pagerank, scope, security, trust-model, pagerank, v2-scope, accepted-risk]
retrievals: 0
status: active
type: clarifications resolve-td 2026-06-28
---

# Should raw-reviewer-name authority keying in PageRank be tre

## Decision

Close as accepted-risk for v1. Raw-reviewer-name keying is identical to the trust model the pre-13.3 flat vote-counter already used — merge.go:77-81 (distinctReviewers) keys confidence on f.Reviewer, the same raw string. No new attack surface has been introduced; this is inherited risk, not new risk. Authenticated source identity only becomes load-bearing when severity-weighted edges arrive (explicitly v2 per the binding scope boundary). The existing empty-string guard (pagerank.go:56) and maxDistinctReviewers=64 cap already provide a DoS backstop. Opening a new epic now would be premature with no v1 benefit. The fix note "for v2 severity-weighted edges, bind authority to an authenticated source identity" is correct scope placement.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/pagerank.go
- reconcile/merge.go
