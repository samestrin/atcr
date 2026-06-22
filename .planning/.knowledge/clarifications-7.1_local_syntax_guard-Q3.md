---
id: mem-2026-06-22-5c5fbe
question: "Is Epic 7.1 Go-only or does it include Python syntax checking?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [.planning/epics/active/7.1_local_syntax_guard.md:29, go.mod]
tags: [clarifications, epic-7.1_local_syntax_guard, scope, Go, Python, language-support]
retrievals: 0
status: active
type: clarifications
---

# Is Epic 7.1 Go-only or does it include Python syntax checkin

## Decision

Go-only. The AC (7.1_local_syntax_guard.md:29) says "at least Go" — Python appears only as a parenthetical example in the Proposed Solution prose. The project is a pure Go module (go.mod, Go 1.25.0) with no Python source files, no python/py_compile references anywhere in .go files, and no language-detection infrastructure in the executor. The only .py files are throwaway planning scripts. Adding Python would require subprocess dependency, language-detection keyed on file extension, and separate error handling — out of scope for a one-week epic.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/epics/active/7.1_local_syntax_guard.md:29
- go.mod
