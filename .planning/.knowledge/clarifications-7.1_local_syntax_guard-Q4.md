---
id: mem-2026-06-22-7b4e6a
question: "Should the fix-warning line in render.go switch from the raw ⚠️ emoji to a plain-text marker?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/report/render.go]
tags: [clarifications, epic-7.1_local_syntax_guard, implementation, output-format, render, wontfix]
retrievals: 0
status: active
type: clarifications skill — epic 7.1_local_syntax_guard
---

# Should the fix-warning line in render.go switch from the raw

## Decision

WONTFIX. The ⚠️ at render.go:167 is intentionally consistent with writePathWarning which uses the same glyph at lines 350 and 353. Both are part of the same rendered-output contract. Changing one requires changing the other, and altering output format is outside Epic 7.1 scope. Confirmed WONTFIX — no code change needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/render.go
