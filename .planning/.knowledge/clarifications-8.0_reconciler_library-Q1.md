---
id: mem-2026-06-23-da16ce
question: "When extracting an internal Go package into a standalone module, should it be a nested module inside the same repo or a genuinely separate repository?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [go.mod, internal/reconcile/reconcile.go, internal/reconcile/merge.go, internal/reconcile/emit.go]
tags: [clarifications, epic-8.0_reconciler_library, architecture, process, module-extraction]
retrievals: 0
status: active
type: clarifications
---

# When extracting an internal Go package into a standalone mod

## Decision

Use a nested module (subdirectory with its own go.mod + a replace directive in the root go.mod) for the extraction phase. Creating a separate repo is a hard-to-reverse action that should follow the extraction work, not precede it. A replace directive is trivially dropped once the module is ready to publish externally. In the reconciler case: internal/reconcile has deep entanglements with internal/stream, internal/atomicfs, and the Verification struct — those can only be resolved while working in the same codebase. A nested reconcile/go.mod ships under a single PR and CI run.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- go.mod
- internal/reconcile/reconcile.go
- internal/reconcile/merge.go
- internal/reconcile/emit.go
