---
id: mem-2026-06-21-e9cb26
question: "Should the debate engine auto-fall-back to same-model personas when fewer than 3 distinct models are configured, or require explicit opt-in?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/verify/select.go]
tags: [clarifications, epic-6.0_cross_examination, architecture, model-registry, debate, single-model-fallback]
retrievals: 0
status: active
type: clarifications
---

# Should the debate engine auto-fall-back to same-model person

## Decision

Require explicit opt-in via `debate.allow_single_model: true` or `--single-model` (option A). The current registry has zero agents assigned skeptic or judge roles — AgentsByRole at registry/config.go:434-449 normalizes all agents to reviewer, making single-model the runtime baseline today. RoleSkeptic/RoleJudge are parsed but inert until Epic 6.0 (config.go:99-100). The existing verify stage precedent (verify/select.go:36-49) records "no_eligible_skeptic" rather than silently degrading, confirming the intended pattern. Physical models are sufficient (11 distinct IDs across providers); the gap is role assignment not model count. Auto-fallback would silently produce weaker debate results while Success Criterion #2 claims they are full debates.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/verify/select.go
