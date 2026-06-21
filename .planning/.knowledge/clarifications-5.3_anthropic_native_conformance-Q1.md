---
id: mem-2026-06-20-6d8596
question: "How does ATCR's internal/llmclient reach the Anthropic provider — native Messages API or OpenAI-compatible gateway?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/llmclient/chat.go, internal/llmclient/rates.go]
tags: [clarifications, epic-5.3_anthropic_native_conformance, architecture, llmclient, anthropic, openai-compatible]
retrievals: 0
status: active
type: clarifications — epic-5.3_anthropic_native_conformance (2026-06-20)
---

# How does ATCR's internal/llmclient reach the Anthropic provi

## Decision

ATCR reaches Anthropic exclusively through an OpenAI-compatible gateway. The client posts to `{BaseURL}/chat/completions` for every provider with no branch that switches to a different URL or request envelope for Anthropic. Response decoding uses `choices[].finish_reason` and `choices[].message.tool_calls` (OpenAI fields only) — native Anthropic fields (`stop_reason`, `content` blocks, `tool_use`) are absent from every struct in the package. The Anthropic case is explicitly documented in chat.go as "Anthropic-via-gateway shims." There is no native Anthropic Messages API decode path in the codebase. Consequence: any epic predicated on a native Anthropic wire adapter (content blocks, tool_use/tool_result, stop_reason) is based on a false premise and should be closed or reframed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/llmclient/chat.go
- internal/llmclient/rates.go
