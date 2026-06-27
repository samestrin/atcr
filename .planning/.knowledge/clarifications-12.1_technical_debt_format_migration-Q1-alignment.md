---
id: mem-2026-06-27-77e9e6
question: "What alignment strategy should generate.go use for the Markdown table — dedicated library or standardized spacing?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/tdmigrate/generate.go, internal/tdmigrate/parse.go]
tags: [clarifications, epic-12.1_technical_debt_format_migration, implementation, generate, parse, markdown table, alignment]
retrievals: 0
status: active
type: clarifications/epic-12.1
---

# What alignment strategy should generate.go use for the Markd

## Decision

No change needed. The current single-space-around-each-cell approach (generate.go:47-53) is already correct. splitRow (parse.go:83-91) calls strings.TrimSpace on every cell unconditionally, making it whitespace-agnostic. Column-width alignment is purely cosmetic; no consumer depends on it. No table library addition is warranted — fmt.Fprintf + strings.Builder is sufficient and adding a library (e.g. tablewriter) would violate the no-unjustified-external-deps constraint. The separator rows (generate.go:37) use fixed-width dashes regardless of content width, confirming the design intent: fixed-layout headers, variable-width data cells, no padding alignment needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tdmigrate/generate.go
- internal/tdmigrate/parse.go
