---
id: mem-2026-06-27-db8e16
question: "Should splitRow implement backslash-escaped pipe support in parse.go, or is documentation that cells must not contain literal pipes sufficient?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/tdmigrate/parse.go, internal/tdmigrate/generate.go]
tags: [clarifications, epic-12.1_technical_debt_format_migration, architecture, parse, pipe escaping, splitRow, cell sanitization]
retrievals: 0
status: active
type: clarifications/epic-12.1
---

# Should splitRow implement backslash-escaped pipe support in 

## Decision

Documentation-only is sufficient. The architecture already enforces the invariant at the output layer: cell() (generate.go:67-73) replaces any | with / before writing to the README table, so splitRow never encounters a literal pipe in any toolchain-generated row. A live scan of .planning/technical-debt/README.md found zero pipe characters inside data cells. TestGenerateTable_SanitizesPipesAndNewlines (parse_test.go:151-176) already verifies this round-trip. Adding backslash-unescaping to splitRow would be speculative scope, introduce a convention not used anywhere in the existing table, and could silently corrupt user-authored rows containing intentional \| sequences. Correct fix: add a one-line comment to splitRow documenting that literal | is prevented at generate-time by cell().

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tdmigrate/parse.go
- internal/tdmigrate/generate.go
