---
id: mem-2026-06-23-bbb9d2
question: "Is the adapter.go:62 JSONFinding field-map collapse blocked by the import cycle, and should TD-006 defer to a new epic or close in-session?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [internal/reconcile/adapter/adapter.go, internal/reconcile/lib.go, internal/reconcile/emit.go, internal/reconcile/helpers_test.go]
tags: [clarifications, sprint-8.0_reconciler_library, architecture, td-006, import-cycle, json-finding, field-map, inline]
retrievals: 0
status: active
type: clarifications
---

# Is the adapter.go:62 JSONFinding field-map collapse blocked 

## Decision

The three field maps are NOT equivalent and were never intended to collapse into one helper — and the inline work is already done. lib.go's stream.Finding→reconcile.Finding map is inlined at internal/reconcile/lib.go:144-157 (comment at line 140 confirms TD-006); emit.go's reconcile.Finding→stream.Finding map for toAmbiguousWire is inlined at internal/reconcile/emit.go:193-206 (comment at line 192 notes TD-006 ready); adapter.ToJSONFinding at adapter.go:70-88 serves a different direction and purpose (two-argument, returns JSONFinding). Named helpers toLibFinding/fromLibFinding are already gone — only comment references remain at helpers_test.go:20. A neutral sub-package to break the cycle would require migrating JSONFinding and all its consumers — multi-package refactor, out of single-session scope. TD-006 resolution is: confirm no live toLibFinding/fromLibFinding call sites (confirmed — comment-only), remove stale comment references, close as resolved-by-inline. Single-session close, not an epic deferral.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/adapter/adapter.go
- internal/reconcile/lib.go
- internal/reconcile/emit.go
- internal/reconcile/helpers_test.go
