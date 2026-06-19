---
id: mem-2026-06-18-fd2ace
question: "Is the staged validate()→ValidateFallbacks() ordering in validateMerged()/LoadRegistry() intentional, or should both run unconditionally and be combined via errors.Join?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/registry/overlay.go, internal/registry/config.go]
tags: [td-clarification, td-only, architecture, registry, validation, error-accumulation]
retrievals: 0
status: active
type: clarifications td-only 2026-06-18
---

# Is the staged validate()→ValidateFallbacks() ordering in v

## Decision

The staged order is intentional. Epic 4.2 AC6 scoped error accumulation to within each function individually, not across them. Fallback-chain checks (ValidateFallbacks) assume structurally-valid agents — running them against a malformed registry could surface misleading or redundant errors. The correct resolution is to add a comment in validateMerged() (overlay.go:223) and LoadRegistry() (config.go:193) documenting this constraint, not to combine the two calls. TD row was closed as documented-intentional.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/overlay.go
- internal/registry/config.go
