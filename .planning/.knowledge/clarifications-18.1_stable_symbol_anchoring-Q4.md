---
id: mem-2026-07-04-0b6084
question: "What delimiter convention should a new symbol-name prefix use in the TD README Problem cell?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [.planning/technical-debt/README.md, internal/tdmigrate/parse.go, internal/debt/debt.go, internal/astgroup/cover.go]
tags: [clarifications, epic-18.1_stable_symbol_anchoring, implementation, technical-debt-readme]
retrievals: 0
status: active
type: clarifications
---

# What delimiter convention should a new symbol-name prefix us

## Decision

Use a literal `(symbolName) ` prefix (opening paren, identifier, closing paren, single space, then the original Problem text) anchored at position 0 of the cell; emit nothing (byte-identical cell) when no named block resolves. Every existing Problem-cell annotation convention (Deferred:, Won't-fix:, intent_note:, disagreement:) is a TRAILING parenthetical appended at the end of the text (40 occurrences in .planning/technical-debt/README.md) — none occur at cell-start, so a start-anchored prefix cannot collide as long as the parser only checks position 0. One existing start-of-cell precedent uses square brackets, not parens (README.md:139, "[Story 01 / Story 06] ..."), which is visually/lexically distinct. No current code path (internal/tdmigrate/parse.go:113, internal/debt/debt.go:122 sanitizeCell) strips or expects a leading token in Problem — nothing to migrate. astgroup.CoveringBlock's block.Name (internal/astgroup/cover.go:41) returns a bare identifier token with no internal spaces/parens, so it composes cleanly into `(symbolName) ` with no escaping needed. Edge case: if block.Name is itself empty/anonymous (e.g. an anonymous func literal) while ok=true, treat as "no named block" (AC2 graceful degradation) rather than emitting `() `.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/tdmigrate/parse.go
- internal/debt/debt.go
- internal/astgroup/cover.go
