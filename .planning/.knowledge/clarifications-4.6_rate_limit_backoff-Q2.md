---
id: mem-2026-06-19-1021f6
question: "At what config level should Epic 4.6 retry tunables (max_retries, initial_backoff_ms) live — per-agent only, global only, or both?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/registry/precedence.go]
tags: [clarifications, epic-4.6_rate_limit_backoff, architecture, config, registry, precedence]
retrievals: 0
status: active
type: clarifications
---

# At what config level should Epic 4.6 retry tunables (max_ret

## Decision

Both global and per-agent, mirroring the TimeoutSecs pattern exactly: Registry.TimeoutSecs *int (global tier), AgentConfig.TimeoutSecs *int (per-agent tier), Settings.TimeoutSecs int (resolved value), AgentConfig.EffectiveTimeoutSecs(s Settings) int (override method). Add MaxRetries *int and InitialBackoffMs *int to both Registry and AgentConfig, add MaxRetries int and InitialBackoffMs int to Settings, seed them in ResolveSettings, and add EffectiveMaxRetries/EffectiveInitialBackoffMs methods on AgentConfig.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/registry/precedence.go
