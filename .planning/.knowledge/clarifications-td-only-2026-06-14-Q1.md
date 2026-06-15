---
id: mem-2026-06-14-b4527b
question: "Should isVerificationTie over-inclusion be fixed by extending verification.json schema, or is a code comment sufficient?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/reconcile/disagree.go]
tags: [td-clarification, td-only, architecture, CONTRACT, verification, disagree-radar]
retrievals: 0
status: active
type: td-clarification
---

# Should isVerificationTie over-inclusion be fixed by extendin

## Decision

Accept the over-inclusion with a code comment — no schema extension. The isVerificationTie function (internal/reconcile/disagree.go:153-165) already documents the v1 heuristic limitation: a unanimous-unverifiable multi-skeptic block is indistinguishable from a genuine confirmed-vs-refuted tie; precise detection requires per-verdict vote counts from the verify stage. The schema extension is an Epic 3.0 contract change and was explicitly out of scope for the projection-only Epic 3.2. The godoc comment at disagree.go:153-165 is the accepted resolution.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/disagree.go
