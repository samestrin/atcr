---
id: mem-2026-06-29-80adc5
question: "braceparser: does changing > to >= in the arrow-function/keyword tiebreak have any effect, and is a \"not preceded by function\" guard needed?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/parse_core.go, internal/astgroup/parsers/src/braceparser/configs.go]
tags: [clarifications, epic-13.4_brace_language_parsers, implementation, braceparser, arrow-function]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q2 (2026-06-29)
---

# braceparser: does changing > to >= in the arrow-function/key

## Decision

The > vs >= distinction is structurally inert: isIdentByte excludes = (and digit bytes), so the byte offset of `=>` can never coincide with the byte offset of any keyword token — they are different byte classes. The actual condition is at parse_core.go:300 (not line 260 as a stale TD citation stated). No "not preceded by function" guard is needed for valid TS/JS because `function foo() {}` never contains `=>` in its block header and vice versa. The depth-0 paren guard is the meaningful protection against false positives like `arr.map(i => i.id)`.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/parse_core.go
- internal/astgroup/parsers/src/braceparser/configs.go
