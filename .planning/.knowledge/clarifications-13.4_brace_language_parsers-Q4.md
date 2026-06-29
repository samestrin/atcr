---
id: mem-2026-06-29-a6a59a
question: "braceparser: what should happen when the scanner reaches EOF while inside a heredoc state — warning, reset, or swallow?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/parse_core.go, internal/astgroup/parsers/src/pyparser/main.go]
tags: [clarifications, epic-13.4_brace_language_parsers, implementation, braceparser, heredoc, wasm]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q4 (2026-06-29)
---

# braceparser: what should happen when the scanner reaches EOF

## Decision

Keep the swallow-to-EOF behavior. When the byte loop exits with state==stHeredoc, the post-loop stack flush (`for len(stack) > 1 { closeBlock() }`) closes all open blocks correctly — identical to how every other unterminated state (stString, stBlockComment, stRawString, stParamExp) is handled at EOF. Emitting a warning is not viable because the wasip1 reactor exports only alloc/free/parse and has no stderr channel. An explicit state reset before termination provides zero benefit. The epic's "degrades, never breaks" design principle fully covers this: an unterminated heredoc causes grouping inside the heredoc body to be lost, but the tree is always returned.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/parse_core.go
- internal/astgroup/parsers/src/pyparser/main.go
