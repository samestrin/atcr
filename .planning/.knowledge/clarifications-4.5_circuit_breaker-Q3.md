---
id: mem-2026-06-19-ca5266
question: "Where in llmclient should cross-cutting logic (e.g. a circuit breaker) integrate to cover all production LLM call paths?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/llmclient/chat.go, internal/fanout/engine.go, internal/fanout/loop.go]
tags: []
retrievals: 0
status: active
type: clarifications epic 4.5_circuit_breaker
---

# Where in llmclient should cross-cutting logic (e.g. a circui

## Decision

Integrate at the shared `send()` chokepoint (internal/llmclient/client.go:297) — the only place the HTTP attempt/retry loop runs, shared by both single-shot and tool-loop paths. `Complete` (client.go:221) is a two-line pass-through to `CompleteWithUsage` (client.go:229,243); production prefers `CompleteWithUsage` via the UsageCompleter interface (internal/fanout/engine.go:488), so the `Complete` branch is effectively dead. Tool-enabled agents go through `Chat` (internal/llmclient/chat.go:133→send at chat.go:152), invoked at internal/fanout/loop.go:116 — never touching Complete/CompleteWithUsage. Therefore wrapping only `Complete` is inert in production. send()-level integration covers all three entry points uniformly. Nuance: send() fires once per tool-loop turn, so RecordSuccess/RecordFailure at send() give correct per-HTTP-call health accounting, but decide explicitly whether the Allow() fail-fast gate belongs per-HTTP-call (inside send()) or once per logical request (one level up) to avoid tripping a circuit mid-conversation.</answer>
<parameter name="tags">clarifications, epic-4.5_circuit_breaker, architecture, llmclient, integration-point, fanout

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/llmclient/chat.go
- internal/fanout/engine.go
- internal/fanout/loop.go
