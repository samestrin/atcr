---
id: mem-2026-06-14-e5a271
question: "Should inclusion thresholds for disagreement radar items be wired to config.yaml now, or is a hardcoded default acceptable?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, .planning/epics/active/4.5_circuit_breaker.md, .planning/epics/active/4.1_graceful_shutdown.md]
tags: [clarifications, epic-3.2_disagreement_radar, scope, configuration, thresholds, hardcoded-defaults]
retrievals: 0
status: active
type: clarifications /3.2_disagreement_radar.md
---

# Should inclusion thresholds for disagreement radar items be 

## Decision

Hardcoded default is correct and follows the project's established pattern. Epic 4.5 sets the exact precedent: "These are hardcoded initially; can be made configurable later" (4.5_circuit_breaker.md:116), with configurable thresholds listed as Out of Scope (4.5_circuit_breaker.md:133). Epic 4.1 applies the same pattern to its grace period (4.1_graceful_shutdown.md:82). The project convention in internal/registry/config.go:44-48 defines operational defaults as constants until a concrete need for user control is established. For the radar, wire a hardcoded default (spread >= 1 tier, all gray-zone clusters, all verification ties) now and defer config.yaml plumbing to a later epic. "Auto-tuning the ranking weights" is explicitly Out of Scope for epic 3.2.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- .planning/epics/active/4.5_circuit_breaker.md
- .planning/epics/active/4.1_graceful_shutdown.md
