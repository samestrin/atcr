---
id: mem-2026-07-04-955626
question: "Should ambiguous.json gray-zone cluster Problem cells be symbol-anchored like findings.json, or is the asymmetry accepted?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/reconcile/symbol_anchor.go, internal/debate/cluster.go, cmd/atcr/report.go, internal/mcp/handlers.go, docs/technical-debt-format.md]
tags: [clarifications, epic-18.1_stable_symbol_anchoring, scope]
retrievals: 0
status: active
type: clarifications
---

# Should ambiguous.json gray-zone cluster Problem cells be sym

## Decision

Accepted as-is — do not stamp toAmbiguousWire. internal/reconcile/symbol_anchor.go:82-90 documents the asymmetry as intentional: findings.json is symbol-anchored while ambiguous.json cluster members stay raw, with StripSymbolAnchors provided specifically so identity-correlation consumers (internal/debate/cluster.go:209,218) normalize it away. No current consumer of ambiguous.json (cmd/atcr/report.go:84-107, internal/mcp/handlers.go:400-406 — the gray-zone radar) needs the anchor, and no TD README/ToC pipeline reads ambiguous.json. Revisit only if a TD ToC begins consuming ambiguous.json directly.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/reconcile/symbol_anchor.go
- internal/debate/cluster.go
- cmd/atcr/report.go
- internal/mcp/handlers.go
- docs/technical-debt-format.md
