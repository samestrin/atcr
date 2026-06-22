---
id: mem-2026-06-22-0b753c
question: "What should the executor's default temperature be when the user does not configure one?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/executor.go, internal/llmclient/client.go, internal/registry/config.go]
tags: [clarifications, epic-7.0.1_executor_model_configuration, implementation, temperature, executor, defaults]
retrievals: 0
status: active
type: clarifications
---

# What should the executor's default temperature be when the u

## Decision

0.0. The epic Proposed Solution says "defaulting to 0.0 or a very low value" and Success Criteria says "deterministic low value" — only 0.0 is technically deterministic. Today the executor omits temperature entirely via omitempty on llmclient.Invocation.Temperature (client.go:154), so the provider applies its own default (~0.7). The existing DefaultTemperature=0.7 (config.go:23) applies only to reviewer AgentConfig entries, not ExecutorConfig. Adding ExecutorConfig.Temperature with a 0.0 default is purely additive.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/executor.go
- internal/llmclient/client.go
- internal/registry/config.go
