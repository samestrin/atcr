---
id: mem-2026-06-22-d09d0c
question: "Should the syntaxguard add JSON/key:value pre-detection to avoid spuriously flagging unfenced config snippets with block braces?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/syntaxguard.go]
tags: [clarifications, epic-7.1_local_syntax_guard, scope, syntaxguard, conservative-recall, deferred]
retrievals: 0
status: active
type: clarifications skill — epic 7.1_local_syntax_guard
---

# Should the syntaxguard add JSON/key:value pre-detection to a

## Decision

Keep deferred. The nonGoFenceLangs map already suppresses the common LLM output shape (fenced ```json blocks). The unfenced case is a narrow residual. Adding JSON/key:value pre-detection is scope expansion beyond the baseline AC and contradicts the conservative-recall decision. This is tracked as Epic 7.2 syntax-guard-refinements for a future iteration after the baseline guard is proven.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/syntaxguard.go
