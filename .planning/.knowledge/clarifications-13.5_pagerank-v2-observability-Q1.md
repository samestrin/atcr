---
id: mem-2026-06-30-9d8e11
question: "Should a test oracle that independently recounts authority-driven MEDIUM→HIGH confidence flips live in test code or be exported as a production helper?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [reconcile/pagerank_confidence_test.go, reconcile/reconcile.go, reconcile/merge.go]
tags: [clarifications, epic-13.5_pagerank-v2-observability, testing]
retrievals: 0
status: active
type: clarifications epic 13.5_pagerank-v2-observability
---

# Should a test oracle that independently recounts authority-d

## Decision

Accept the test oracle as-is in test code. The production counter (reconcile/reconcile.go:118) already implements the canonical predicate directly using before/after comparison in the same loop body — no exported helper would fill a logic gap. The oracle (reconcile/pagerank_confidence_test.go:175-182) recounts from final Result output via a different but equivalent path (ConfidenceFor(len(Reviewers)) proxy), which is exactly what makes it a valid independent cross-check. A diff_smell "test_only" flag is a process gate for reward hacks; an oracle that strengthens rather than weakens assertions is a false positive for that gate. Exporting would add production API surface with no existing consumer and would blur the line between the counter and its validator.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/pagerank_confidence_test.go
- reconcile/reconcile.go
- reconcile/merge.go
