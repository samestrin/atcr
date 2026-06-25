---
id: mem-2026-06-24-944b80
question: "How should a community persona whose file name matches a built-in name be handled in atcr personas list?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/personas/list.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, error-handling, implementation]
retrievals: 0
status: active
type: clarifications /resolve-td session 2026-06-24
---

# How should a community persona whose file name matches a bui

## Decision

Skip the community file and return a warning alongside the valid rows (same accumulation pattern as per-file error surfacing). Built-in names are canonical and take priority. Renaming would confuse users; flagging as a hard error conflicts with AC 02-02 Error Scenario 1 ("exit 0 + stderr warning on unreadable personas dir — graceful degradation") and the overall exit-0 posture of the list command. The collision check belongs at internal/personas/list.go:139 after name derivation: if isBuiltin(name) is true, accumulate a warning and skip (continue). The CLI caller emits the warning to stderr; list exits 0 with no duplicate rows.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/list.go
