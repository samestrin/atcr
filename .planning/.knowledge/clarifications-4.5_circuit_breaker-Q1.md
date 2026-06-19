---
id: mem-2026-06-19-d8f564
question: "How should a per-provider circuit breaker be keyed — on Invocation.BaseURL or on the registry provider name?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/fanout/review.go, internal/registry/config.go]
tags: []
retrievals: 0
status: active
type: clarifications epic 4.5_circuit_breaker
---

# How should a per-provider circuit breaker be keyed — on In

## Decision

Key on the provider NAME, not BaseURL. `llmclient.Invocation` carries only BaseURL + APIKeyEnv (internal/llmclient/client.go:122-133), no provider name. BaseURL is a colliding key: `Provider.BaseURL` is `omitempty` (internal/registry/config.go:39) and validated only when non-empty (config.go:265), so providers omitting base_url or sharing a gateway collapse onto one breaker keyed "". The provider name is the registry's unique map key (`Providers map[string]Provider`, config.go:151) and cannot collide. Threading it down is small, not cross-cutting: `ac.Provider` is already resolved at both Invocation construction sites (internal/fanout/review.go:583,638) and merely dropped (only BaseURL/APIKeyEnv copied at review.go:600-601,673-674) — add one `Provider` field to Invocation plus `Provider: ac.Provider` at those two lines. Non-fanout constructors (internal/doctor/run.go:200, internal/verify/invoke.go:112) can leave Provider empty.</answer>
<parameter name="tags">clarifications, epic-4.5_circuit_breaker, architecture, circuit-breaker, llmclient, registry

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/fanout/review.go
- internal/registry/config.go
