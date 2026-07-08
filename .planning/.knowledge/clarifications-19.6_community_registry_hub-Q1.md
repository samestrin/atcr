---
id: mem-2026-07-08-3d4e1e
question: "Tier-aware ListWithScores implementation for cmd/atcr/personas.go"
created: 2026-07-08
last_retrieved: ""
sprints: [19.6_community_registry_hub]
files: [internal/personas/list.go, cmd/atcr/personas.go]
tags: [clarifications, sprint-19.6_community_registry_hub, implementation, personas, go]
retrievals: 0
status: active
type: clarifications
---

# Tier-aware ListWithScores implementation for cmd/atcr/person

## Decision

Add ListTiersWithScores(projectDir, communityDir, scores) in internal/personas/list.go, implemented as ListTiers(projectDir, communityDir) followed by extracting ListWithScores' existing score-join+sort loop into a shared unexported helper (e.g. joinScores). Wire cmd/atcr/personas.go's listPersonasWithScores to compute projectDir (same as the plain-list branch already does) and call ListTiersWithScores instead of ListWithScores. This resolves TD-005 (personas list --scores disagreeing with plain list on the Source column) without duplicating the join/sort logic in the cmd layer.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/list.go
- cmd/atcr/personas.go
