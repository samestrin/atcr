---
id: mem-2026-06-23-31472b
question: "Should the unbounded-input risk in reconcile/adapter/json Decode be fixed by adding a maxBytes param, a DecodeReader variant, or closed as-is via godoc contract?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/adapter/json/adapter.go, reconcile/adapter/json/adapter_test.go]
tags: [clarifications, sprint-8.0_reconciler_library, architecture, scope, json-adapter, decode, unbounded-input, godoc-contract, lift-as-is]
retrievals: 0
status: active
type: clarifications
---

# Should the unbounded-input risk in reconcile/adapter/json De

## Decision

Close as-is — option (c). The godoc at reconcile/adapter/json/adapter.go:61-63 already explicitly documents caller responsibility for bounding input size ("the caller is responsible for bounding total input size (for example, with an io.LimitReader before reading untrusted input)"). Option (a) is logically incoherent: Decode accepts []byte (not io.Reader), so the data is already fully buffered in memory by the time Decode is called — a maxBytes param cannot limit what is already allocated. Option (b) adds new exported API surface violating the sprint's lift-as-is binding constraint ("no API reshaping"). The risk is theoretical, not active — zero production callers of the JSON adapter exist (all callers are tests at adapter_test.go). When external callers eventually exist, the godoc places sizing responsibility correctly at the transport layer, which is the idiomatic Go pattern (io.LimitReader before buffering into []byte).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/adapter/json/adapter.go
- reconcile/adapter/json/adapter_test.go
