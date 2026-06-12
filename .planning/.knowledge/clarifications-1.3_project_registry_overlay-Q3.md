---
id: mem-2026-06-11-989221
question: "Should config provenance (project/user/embedded tier) be surfaced by extending atcr doctor or by adding a separate atcr config dump command?"
created: 2026-06-11
last_retrieved: ""
sprints: []
files: [internal/doctor/render.go, internal/doctor/run.go, internal/registry/config.go, internal/registry/persona.go, docs/registry.md]
tags: [clarifications, epic-1.3_project_registry_overlay, architecture, scope, provenance]
retrievals: 0
status: active
type: clarifications
---

# Should config provenance (project/user/embedded tier) be sur

## Decision

Extend atcr doctor. Add a SOURCE column to the existing table and a "source" string field to AgentResult. Do not add a separate atcr config dump command in this epic — it is out-of-scope scope expansion the success criterion does not require.

Justification:
- internal/doctor/render.go:30 — current table header is AGENT | PROVIDER | MODEL | STATUS | LATENCY | HINT. Adding SOURCE at the end is a minimal additive change: one extra fmt.Fprintf field in RenderTable.
- internal/doctor/run.go:70-78 — AgentResult already has a stable JSON contract documented in docs/registry.md. Adding "source": "project|user|embedded" with omitempty preserves backwards compatibility.
- internal/registry/config.go:107-122 (LoadRegistry) — both approaches require propagating a source tag from the load layer to the render layer. The complexity difference is in the render surface only, not the data model.
- A separate atcr config dump command requires: new cobra subcommand, new render logic, new command-layer tests, new docs. The epic's task 4 says "expose entry source tier to doctor/config-dump surfaces" with doctor listed first as the primary surface; config-dump was the fallback escape hatch.
- internal/registry/persona.go:25 — the existing Source field in ResolvedPersona confirms the pattern. A new source field in AgentResult has no collision risk.
- No provenance/source field currently exists in AgentResult, Target, AgentTarget, or Registry structs — the addition is net-new.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/doctor/render.go
- internal/doctor/run.go
- internal/registry/config.go
- internal/registry/persona.go
- docs/registry.md
