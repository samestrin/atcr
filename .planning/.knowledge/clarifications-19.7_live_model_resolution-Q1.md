---
id: mem-2026-07-08-c6be52
question: "OpenRouter API key source for live spike calls (Sprint 19.7)"
created: 2026-07-08
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [.planning/sprints/active/19.7_live_model_resolution/sprint-plan.md]
tags: [clarifications, sprint-19.7_live_model_resolution, process, openrouter]
retrievals: 0
status: active
type: clarifications
---

# OpenRouter API key source for live spike calls (Sprint 19.7)

## Decision

When a sprint task needs a live, authenticated call to OpenRouter (e.g. the Sprint 19.7 Phase 1 alias-routability spike) and OPENROUTER_API_KEY is not set, reuse the value already held in LLM_OPENROUTER_API_KEY — read it inline at call time (e.g. `Authorization: Bearer $LLM_OPENROUTER_API_KEY`), never export/print/commit/echo it. This follows the sprint's API-key handling rule (sprint-plan.md:154): never print it, never commit it, never echo it to logs/terminal history — record only the outcome, never the raw request/response.
- User directive, this session (2026-07-08): "we can pull the LLM_OPENROUTER_API_KEY and reuse that. thats set, so we could set our OPENROUTER_API_KEY with the same value"
- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md:154 (API key handling rule)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md
