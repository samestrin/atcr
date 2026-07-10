---
id: mem-2026-07-09-85489b
question: "Does `internal/personas`'s `atcr models check` snapshot-refresh consumption model (recompile-and-ship vs. user-writable override) still need a Phase-8 design decision?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/snapshot.go, cmd/atcr/models.go, .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md]
tags: [clarifications, sprint-19.7_live_model_resolution, architecture, catalog-snapshot]
retrievals: 0
status: active
type: clarifications
---

# Does `internal/personas`'s `atcr models check` snapshot-refr

## Decision

No — already resolved by what Phase 8 shipped (Epic 19.7). The default path compiles the checked-in `internal/personas/testdata/catalog_snapshot.json` into the binary via `//go:embed` (refreshed only through a reviewed PR diff, matching the project's reproducible-by-default posture), and a user-writable override already exists via the `ATCR_CATALOG_SNAPSHOT` env var (documented in `newModelsCheckCmd`'s help text, cmd/atcr/models.go:164-166) for pointing `models check` at a different snapshot (e.g. one just produced by `atcr models refresh`) without recompiling. Both options TD-009 asked to choose between are already implemented as a coherent default+override pair; no need to relocate the snapshot outside `testdata/`. When a TD row phrased as "defer until Phase X" is being triaged, check whether Phase X has actually landed (sprint-plan.md phase DoD/gate checkboxes) before treating the deferral as still open — the phase may have already shipped the very feature being asked about.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/snapshot.go
- cmd/atcr/models.go
- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md
