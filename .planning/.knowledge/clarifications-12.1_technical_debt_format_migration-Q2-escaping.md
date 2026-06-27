---
id: mem-2026-06-27-3fe8f1
question: "Should cell() in generate.go escape backticks/asterisks/brackets for Markdown, and should splitRow unescape them to restore round-trip fidelity?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/tdmigrate/generate.go, internal/tdmigrate/parse.go]
tags: [clarifications, epic-12.1_technical_debt_format_migration, architecture, generate, parse, round-trip, cell escaping, markdown]
retrievals: 0
status: active
type: clarifications/epic-12.1
---

# Should cell() in generate.go escape backticks/asterisks/brac

## Decision

No — neither escaping nor unescaping should be added. The revert (commit 4b740d3f) is correct and final. Backticks, asterisks, and brackets are structurally inert in pipe-delimited tables; only | and \n (which cell() already sanitizes) can cause phantom-column corruption. Adding escape prefixes makes table-cell values differ from shard values, breaking TestGenerateTable_SemanticRoundTrip and TestLiveREADME_SemanticRoundTrip (the AC2 contract tests). The live README already contains backticks and brackets in cells (README.md:69,104,118) and round-trips correctly today. Adding unescaping to splitRow would also be wrong: td_stats/td_filter read the table directly, and backslash-prefixed values would silently mismatch raw shard values. The design contract is explicit in GenerateTable's docstring (generate.go:10-16): round-trip equality holds at the shard layer, not at verbatim table-cell level.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tdmigrate/generate.go
- internal/tdmigrate/parse.go
