---
id: mem-2026-06-30-98107c
question: "Should the braceparser's 4+-quote C# raw-string desync (degrade-to-proximity) be fixed with a length-tracked triple-quote implementation, or kept as an accepted limitation?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/parse_core.go, internal/astgroup/parsers/src/braceparser/parse_core_test.go, internal/astgroup/parsers/src/braceparser/configs.go]
tags: [clarifications, epic-13.6_brace_languages_expansion, implementation, scope, braceparser, tripleQuote, csharp]
retrievals: 0
status: active
type: clarifications
---

# Should the braceparser's 4+-quote C# raw-string desync (degr

## Decision

Keep the accepted limitation — do not implement run-length tracking for the shared tripleQuote/stTripleString scanner state. The fixed-length-3 open/close match is the correct trade-off: it makes the common empty-triple-quote case (`""""""`) parse correctly for free, while a length-tracked fix for 4+-quote C# raw strings is structurally ambiguous (a 6-quote run can't be distinguished from two adjacent empty triple-strings without per-language semantics) and would either break the pinned TestParseSource_EmptyTripleQuoteDoesNotDesync or require C#-specific control flow, which conflicts with the braceparser's "shared scanner, no per-language control flow" design constraint. This is the same family of accepted limitation as C++ raw strings / C# verbatim strings (`@"..."`) being out of scope — a misparse in the brace AST grouping parser only degrades a finding to ±3 line proximity and never breaks reconcile.

Justification:
- internal/astgroup/parsers/src/braceparser/parse_core.go documents the limitation inline near the tripleQuote/stTripleString state handling.
- TestParseSource_CSharpFourQuoteRawStringDegrades only pins resilience (no panic, enclosing block still recoverable), not correct internal structure — confirming the degrade is the intended/tested outcome.
- TestParseSource_EmptyTripleQuoteDoesNotDesync relies on the current fixed-length-3 behavior; a length-tracked implementation would break it.
- tripleQuote is a shared config flag used by Java/Kotlin/C# configs in configs.go — any C#-specific run-length logic would violate the shared-scanner, data-only design.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/parse_core.go
- internal/astgroup/parsers/src/braceparser/parse_core_test.go
- internal/astgroup/parsers/src/braceparser/configs.go
