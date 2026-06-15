---
id: mem-2026-06-15-099e76
question: "Where should TrippedBudgets nil-to-empty-slice fixes be applied in internal/verify/pipeline.go?"
created: 2026-06-15
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go]
tags: [td-clarification, td-only, correctness, TrippedBudgets, nil-slice, pipeline]
retrievals: 0
status: active
type: td-clarification
---

# Where should TrippedBudgets nil-to-empty-slice fixes be appl

## Decision

Three distinct locations need `nil` coerced to `[]string{}`: (1) carry-forward path at pipeline.go:287 — after `rec.TrippedBudgets = prior.TrippedBudgets`, add nil guard; (2) winningAttribution return at pipeline.go:499 — coerce budgets before returning; (3) base struct at pipeline.go:402 inside verifyFinding — add `base.TrippedBudgets = []string{}` after the struct literal to cover early-exit paths. TD rows citing lines 386 and 395 address location 3; the line 242 TD row addresses locations 1 and 2. The Fix column text in the line 242 TD rows is a copy-paste error (streaming decoder text).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
