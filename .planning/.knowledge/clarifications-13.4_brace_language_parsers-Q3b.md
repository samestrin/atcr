---
id: mem-2026-06-29-efc299
question: "braceparser: should JS/TS regex-literal scanning be implemented or formally documented as unhandled?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/parse_core.go]
tags: [clarifications, epic-13.4_brace_language_parsers, scope, braceparser, regex, scanner-state]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q3 second-run (2026-06-29)
---

# braceparser: should JS/TS regex-literal scanning be implemen

## Decision

Formally document as unhandled — do not implement a new scanner state. The state machine has seven states (stNormal, stLineComment, stBlockComment, stString, stRawString, stHeredoc, stParamExp) with no stRegex. The / character in stNormal falls through to addHeader(c), silently allowing { or } inside a regex literal to desync brace depth. This is accepted degradation per the epic's heuristic contract ("misparse degrades to line-proximity grouping, never breaks a reconcile"). The epic's Risks section explicitly lists regex as a known accepted degradation. The only deliverable is a // NOTE: comment near the / fall-through in stNormal (~parse_core.go:290) formally acknowledging the gap.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/parse_core.go
