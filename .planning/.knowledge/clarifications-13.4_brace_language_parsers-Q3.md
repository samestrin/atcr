---
id: mem-2026-06-29-e72be2
question: "braceparser parse_core.go line 283: is the issue \\r line endings or catch-clause misclassification via funcParenName?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/parse_core.go]
tags: [clarifications, epic-13.4_brace_language_parsers, implementation, braceparser, line-endings]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q3 (2026-06-29)
---

# braceparser parse_core.go line 283: is the issue \r line end

## Decision

The TD item conflates two separate issues and has a wrong line number. Line 283 is `var bestKw blockKeyword` inside classifyHeader — unrelated to either. (1) The catch-clause guard (`case "catch", "with", "switch": return "", false`) is already present at parse_core.go:442-445 inside funcParenName — that half of the TD item is stale/already-fixed. (2) The \r gap is real but separate: the main scanner only increments `line` on `\n` (parse_core.go:259), so bare-\r files miscount lines. However, addHeader normalises \r to space (parse_core.go:91-92) and heredocLineMatches strips it (parse_core.go:606), limiting practical impact to line-number accuracy only.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/parse_core.go
