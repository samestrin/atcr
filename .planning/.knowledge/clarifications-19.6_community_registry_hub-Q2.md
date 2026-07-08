---
id: mem-2026-07-08-a42eb2
question: "How should InstallBundle members route through InstallUnit (community personas)?"
created: 2026-07-08
last_retrieved: ""
sprints: [19.6_community_registry_hub]
files: [internal/personas/bundles.go, internal/personas/unit.go, internal/personas/install.go]
tags: [clarifications, sprint-19.6_community_registry_hub, implementation, personas, go]
retrievals: 0
status: active
type: clarifications
---

# How should InstallBundle members route through InstallUnit (

## Decision

Call InstallUnit directly per member in the InstallBundle loop (internal/personas/bundles.go) — no separate shared helper needed, since InstallUnit(client, baseURL, name, destDir) is already signature-compatible with the Install() call it replaces. InstallUnit already IS the self-contained fetch+validate+paired-write primitive (per Clarification C2 — one unit, one delivery path), so it's a drop-in substitution. Bundle members are already strict-decoded via ValidateCommunityPersonaYAML today (Install already calls it), so switching to InstallUnit adds .md prompt delivery + C3 guardrails without changing YAML validation strictness. Resolves TD-006; confirms TD-012 needs only a missing test, not a validator relaxation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/bundles.go
- internal/personas/unit.go
- internal/personas/install.go
