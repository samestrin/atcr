---
id: mem-2026-06-23-29882b
question: "Should the discover.Source → reconcile.Source conversion at lib.go:133-160 be moved to the adapter layer to satisfy TD-002, or deferred alongside TD-006?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [internal/reconcile/lib.go, internal/reconcile/adapter/adapter.go, internal/reconcile/discover.go, internal/reconcile/gate.go]
tags: [clarifications, sprint-8.0_reconciler_library, architecture, td-002, td-006, import-cycle, discover-source, source-conversion]
retrievals: 0
status: active
type: clarifications
---

# Should the discover.Source → reconcile.Source conversion a

## Decision

Defer alongside TD-006 — do not attempt a structural fix independently. The inline conversion at internal/reconcile/lib.go:133-160 is already correct and self-documented: code comment at lines 140-142 names the import cycle and TD-006 as the reason for the duplicate. The conversion is a pure 11-field struct copy identical to adapter.ToFinding at adapter.go:27-41; both exist because the import cycle (adapter imports internal/reconcile) forces duplication. The adapter package doc at lines 8-14 explicitly notes it has "zero non-test callers today." No structural change avoids the cycle without moving JSONFinding and Verification out of internal/reconcile — that is Phase 3 scope. Moving discover.Source conversion in isolation would leave a dangling half-fix. Resolve only when processing TD-006 (which eliminates the lib.go transitional Reconcile wrapper and this inline copy simultaneously). Mark as blocked-by-TD-006.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/lib.go
- internal/reconcile/adapter/adapter.go
- internal/reconcile/discover.go
- internal/reconcile/gate.go
