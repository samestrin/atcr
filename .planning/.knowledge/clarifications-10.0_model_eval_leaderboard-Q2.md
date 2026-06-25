---
id: mem-2026-06-24-d32ae1
question: "How to eliminate a double-Load / double-parse without changing a public API signature in Go?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/benchmark/benchmark.go:128, cmd/atcr/benchmark.go:60]
tags: [clarifications, epic-10.0_model_eval_leaderboard, architecture, implementation, api-design, double-io]
retrievals: 0
status: active
type: clarifications/10.0_model_eval_leaderboard
---

# How to eliminate a double-Load / double-parse without changi

## Decision

Add an unexported helper that accepts the already-loaded struct (e.g., reproHashManifest(m *Manifest, suitePath string)) as the implementation body. Keep the exported function stable (ReproHash(suitePath string)) and have it call the unexported helper after Load. Update the single call site that already has the struct in scope to call the unexported form directly. This eliminates the double-parse at zero API cascade cost: Load's return type is unchanged, no struct fields added, no second return value, tests continue to call the exported form. Applies whenever a function internally re-loads something the caller already holds.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/benchmark/benchmark.go:128
- cmd/atcr/benchmark.go:60
