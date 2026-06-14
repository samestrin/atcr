---
id: mem-2026-06-13-f47065
question: "When truncating to max_findings, should findings be sorted by severity first or preserved in emission order?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/reconcile/merge.go, internal/reconcile/reconcile.go]
tags: [clarifications, epic-2.2_code_review_fanout_hardening, implementation, truncation, severity, max_findings]
retrievals: 0
status: active
type: clarifications
---

# When truncating to max_findings, should findings be sorted b

## Decision

Sort by severity (CRITICAL>HIGH>MEDIUM>LOW) descending, then keep top N — never preserve emission order. Emission order is adversarial input: the motivating incident was 498 LOW findings emitted before 3 HIGH + 8 MEDIUM findings, which preserve-emission-order with a cap would repeat. The canonical severityRank map is already defined at internal/reconcile/merge.go:17; sortMerged at internal/reconcile/reconcile.go:104-116 uses the same descending order. Reuse both for the fan-out post-processing step. AC3 does not specify ordering, so the implementation is free to sort-first.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/merge.go
- internal/reconcile/reconcile.go
