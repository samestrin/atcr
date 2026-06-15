---
id: mem-2026-06-14-59b48c
question: "Should the provider-conformance test matrix for llmclient become a standalone epic or be resolved inline in a sprint?"
created: 2026-06-14
last_retrieved: ""
sprints: [3.0_adversarial_verification]
files: [internal/llmclient/chat.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-3.0_adversarial_verification, testing, scope, provider-conformance, llmclient]
retrievals: 0
status: active
type: clarifications skill, 2026-06-14
---

# Should the provider-conformance test matrix for llmclient be

## Decision

Defer to a new epic (14.0 openai-compatible-conformance-suite). The TD row for internal/llmclient/chat.go:1 explicitly flags scope as "a few days, not a quick-win" with an effort score of 480. Provider scope is OpenAI + Ollama only (both use the same OpenAI-compatible /v1/chat/completions surface — Ollama's OpenAI-compat endpoint means it's effectively one API shape). Anthropic native API is out of scope; the project uses OpenAI-compatible endpoints exclusively. Recorded-fixture capture requires live provider access, fixture storage, and a dedicated assertion harness — not suitable for inline sprint resolution even with narrowed scope.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/chat.go
- .planning/technical-debt/README.md
