---
id: mem-2026-06-29-9a6f3c
question: "braceparser empty-source EndLine convention: should it be 0 or 1?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/main.go, internal/astgroup/parsers/src/braceparser/parse_core.go]
tags: [clarifications, epic-13.4_brace_language_parsers, implementation, braceparser]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q1 (2026-06-29)
---

# braceparser empty-source EndLine convention: should it be 0 

## Decision

EndLine=1 is the correct, deliberate output for empty source — it matches the goparser/pyparser empty-source contract. The empty-source branch in main.go emits node{Kind:"file", StartLine:1, EndLine:1} with an explicit comment confirming this. The host uses 1-indexed lines throughout; EndLine=0 would be anomalous. The negative-n guard at main.go:46 is an independent fix for the bad-pointer panic path and has no effect on the empty-source emit path. These are separate code paths.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/main.go
- internal/astgroup/parsers/src/braceparser/parse_core.go
