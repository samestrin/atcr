---
id: mem-2026-06-25-4d95c9
question: "Should behavioral modifiers (like persona/system-prompt) be included in a checkpoint roster guard for AC4 suite-identity protection?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run.go, cmd/atcr/benchmark_checkpoint.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, AC4, roster-guard, persona]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Should behavioral modifiers (like persona/system-prompt) be 

## Decision

Yes. Persona (system prompt) is a behavioral modifier sourced from the registry config — a change between runs produces different reviewer outputs, which constitutes a "reviewer change" under AC4 (never silently mix a checkpoint from a different reviewer configuration). It should be included in the roster signature using the same string-packing pattern as agent+model, extending to "agent=model=persona". The "=" delimiter is safe when agent names and model IDs use registry-key conventions (alphanumeric + hyphens/underscores, no "="). Structured-tuple alternative requires a breaking JSON schema change for no correctness benefit. Pattern: roster guard must include every config field that materially affects reviewer output — not just agent and model, but also persona/prompt-modifiers.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run.go
- cmd/atcr/benchmark_checkpoint.go
