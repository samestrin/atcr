---
id: mem-2026-06-13-f4e935
question: "What should the per-agent 'scope' field in registry.yaml do — prompt-inject plus hard post-processing drop, prompt-inject only, or defer entirely?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/payload/scope.go, internal/reconcile/merge.go]
tags: [clarifications, epic-2.2_code_review_fanout_hardening, scope, architecture, registry, prompt-injection]
retrievals: 0
status: active
type: clarifications
---

# What should the per-agent 'scope' field in registry.yaml do 

## Decision

Prompt-inject only (soft constraint). AC5 requires parser acceptance; AC7 requires the registry entry carry the values — but no AC mandates post-processing drop of out-of-category findings. The existing ScopeRule (internal/payload/scope.go:22) is a spatial/line-range concept (diff vs. files mode); the epic's scope is a category concept — they coexist without conflict. Hard category drop risks discarding legitimate cross-cutting findings (e.g. a performance reviewer flagging a correctness bug) and has no AC backing. The existing CategoryOutOfScope in internal/reconcile/merge.go:32 handles spatial exclusions and is not designed for review-category filtering.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/scope.go
- internal/reconcile/merge.go
