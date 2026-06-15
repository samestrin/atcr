---
id: mem-2026-06-14-e3e614
question: "How should solo findings be scored and ranked against severity splits when there is no per-reviewer strength data?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/reconcile/merge.go, internal/reconcile/emit.go, internal/stream/parser.go]
tags: [clarifications, epic-3.2_disagreement_radar, architecture, scoring, solo-findings, severity-splits]
retrievals: 0
status: active
type: clarifications /3.2_disagreement_radar.md
---

# How should solo findings be scored and ranked against severi

## Decision

Score solo findings by their own severity rank using the existing severityRank map (internal/reconcile/merge.go:17: CRITICAL=4, HIGH=3, MEDIUM=2, LOW=1) on the same numeric axis as spread (range 1-3). There is no per-reviewer strength field anywhere in the schema (stream.Finding, reconcile.Merged, reconcile.JSONFinding) — all solo finders are equal weight (MEDIUM confidence by definition, merge.go:227). A CRITICAL solo (rank=4) naturally outranks a LOW-vs-MEDIUM split (spread=1), matching the epic's intent. Placing solo findings on the same scale as splits — not in a secondary tier — is the only achievable interpretation with current data. The Disagreement field (merge.go:36, emit.go:63) already encodes spread as a string; no schema changes are needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/merge.go
- internal/reconcile/emit.go
- internal/stream/parser.go
