---
id: mem-2026-06-22-ab38f1
question: "When executor system_prompt is set, does it replace the default framing line or prepend to it? Is persona still honored?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/executor.go, internal/llmclient/client.go]
tags: [clarifications, epic-7.0.1_executor_model_configuration, architecture, system-prompt, executor, persona]
retrievals: 0
status: active
type: clarifications
---

# When executor system_prompt is set, does it replace the defa

## Decision

system_prompt replaces the default framing line ("You are %s, a code-fix executor...") entirely — persona is superseded for that call. Finding metadata, snippet, and rules are still appended after the custom framing. Option (b) (prepend/augment) would produce two competing role-definition sentences: there is no API-level system-message slot — llmclient.Invocation has a single Prompt field mapped to a single role:user message (client.go:257). buildFixPrompt constructs two separable regions: framing line (executor.go:212) and payload block (executor.go:207-219). system_prompt replaces only the framing region.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/executor.go
- internal/llmclient/client.go
