---
id: mem-2026-06-22-c21bc7
question: "Should the executor temperature config use per-provider normalization (clamp Anthropic to 0..1, others to 0..2) or mirror AgentConfig's flat 0..2 range?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/llmclient/client.go, internal/verify/executor.go]
tags: [clarifications, epic-7.0.1_executor_model_configuration, architecture, temperature, provider-routing]
retrievals: 0
status: active
type: clarifications
---

# Should the executor temperature config use per-provider norm

## Decision

Mirror AgentConfig exactly: validate 0.0..2.0, pass through, no per-provider clamping. The Provider struct has only APIKeyEnv and BaseURL — no type discriminator — and all providers route through a single /chat/completions endpoint (resolveEndpoint in client.go). Adding Anthropic detection to the executor alone would be an isolated anomaly with no hook point; the risk is handled at the LiteLLM/gateway layer. The existing AgentConfig pattern is the correct precedent.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/llmclient/client.go
- internal/verify/executor.go
