---
id: mem-2026-06-16-2c9ea4
question: "Where should the canonical severity-rank map + normalizeSeverity live, and is internal/reconcile.SeverityRank already the de facto owner?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/fanout/postprocess.go, internal/reconcile/merge.go, internal/verify/severity.go, internal/report/render.go, internal/registry/config.go, internal/stream/parser.go]
tags: []
retrievals: 0
status: active
type: clarifications
---

# Where should the canonical severity-rank map + normalizeSeve

## Decision

internal/reconcile.SeverityRank is already the de facto canonical severity-rank owner: internal/report/render.go:418 and internal/verify/severity.go:14-17 both consume it. Only internal/fanout/postprocess.go:21 and the registry set-form reviewSeverities at internal/registry/config.go:125 are truly independent literals — and the registry one is a presence-only validation set, not a rank map, so it does not consolidate cleanly. A future consolidation should place the canonical rank map + NormalizeSeverity in internal/stream, the lowest shared package (it imports no other internal package and is already imported by both fanout/postprocess.go:9 and reconcile/merge.go:7, so zero import-cycle risk). Do NOT host it in internal/reconcile (would force a fanout->reconcile dependency that postprocess.go:14-16 deliberately avoids) and do NOT create a new internal/severity package (internal/stream already fits). Known desync risk to fix during extraction: fanout/postprocess.go:50,70 upper-cases before lookup while reconcile/merge.go:104 looks up the raw value, and the stream parser stores severity raw (parser.go:193) — decide raw-vs-normalized at one site. Estimated ~120 min, cross-cutting; tracked as a deferred plan (3.5 severity-rank-consolidation).</answer>
<parameter name="tags">clarifications, sprint-3.3_per_run_scorecard, architecture, severity, internal/stream, import-graph

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/postprocess.go
- internal/reconcile/merge.go
- internal/verify/severity.go
- internal/report/render.go
- internal/registry/config.go
- internal/stream/parser.go
