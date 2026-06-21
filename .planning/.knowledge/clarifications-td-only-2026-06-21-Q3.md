---
id: mem-2026-06-21-a6d245
question: "Debate rulings map key uses raw Problem string — does it need normalization (trim/lowercase) or a stable UUID identity?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/debate/debate.go, internal/reconcile/disagree.go, internal/debate/emit.go]
tags: [td-clarification, td-only, maintainability, architecture, debate, rulings-key]
retrievals: 0
status: active
type: td-clarification
---

# Debate rulings map key uses raw Problem string — does it n

## Decision

No normalization is needed for the current implementation. deduplicateFindings at debate.go:98 ensures {File, Line, Problem} uniqueness before the per-item loop runs; LoadDisagreements copies Problem byte-for-byte from JSONFinding.Problem (disagree.go:284, 300, 324), so it.Problem and f.Problem are always identical strings from the same source (ReadReconciledFindings). Gray-zone items never reach the rulings map (guarded at debate.go:147 by it.Kind != KindGrayZone). Trimming/lowercasing would change behavior without fixing any real mismatch. The long-term stable-UUID approach is already tracked as open MEDIUM items at internal/debate/emit.go:104 (claude) and internal/debate/emit.go:103 (epic-6.0). The immediate fix for the TD item is a documentation comment above rulings at debate.go:131 explaining that deduplicateFindings is the guard — no code change to the key or normalization needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debate/debate.go
- internal/reconcile/disagree.go
- internal/debate/emit.go
