---
id: mem-2026-06-24-a05989
question: "How do you call the unexported validateAgent from outside internal/registry (e.g., from internal/personas/install.go)?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/registry/config.go, internal/registry/validate.go, internal/personas/install.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, architecture, registry, validation, install]
retrievals: 0
status: active
type: clarifications
---

# How do you call the unexported validateAgent from outside in

## Decision

Add a small exported helper in a new internal/registry/validate.go (~20 lines): strict-unmarshal the raw bytes into AgentConfig via decodeStrictYAML, build a minimal throwaway Registry with a single synthesized providers entry (map[string]Provider{agent.Provider: {APIKeyEnv: "PLACEHOLDER"}}), then call the unexported validateAgent on it. This is already the idiomatic test pattern in config_test.go:636-656. The helper stays in the registry package (single validation authority), satisfies AC 02-01 literally, and costs zero changes to existing logic. Do NOT duplicate the validation logic in the calling package — validateAgent is extended per sprint (e.g. Language field guard in 9.0) and a duplicate would silently drift.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/registry/validate.go
- internal/personas/install.go
