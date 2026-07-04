---
id: mem-2026-07-04-80f3d2
question: "Should safeSymbolAnchor's filter be tightened to reject markdown-active characters (backtick, brackets, angle), or left permissive?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/reconcile/symbol_anchor.go, internal/astgroup/parsers/src/braceparser/parse_core.go, internal/astgroup/parsers/src/goparser/main.go, internal/astgroup/parsers/src/pyparser/main.go]
tags: [clarifications, epic-18.1_stable_symbol_anchoring, implementation]
retrievals: 0
status: active
type: clarifications
---

# Should safeSymbolAnchor's filter be tightened to reject mark

## Decision

Leave permissive, close as accepted. Every supported AST parser's name extraction is byte-restricted to identifier characters ([A-Za-z0-9_]) with generics/backticks/brackets already stripped before Node.Name is set: the C-family brace parser's funcParenName does a reverse scan using isIdentByte (internal/astgroup/parsers/src/braceparser/parse_core.go:406-408,541-548) so a C++ destructor `~Dtor()` yields `Dtor`, not `~Dtor`; identAfter calls skipGenericList before/after reading the identifier (parse_core.go:432-450,469-482) so Rust/C++ generics like `Foo<T>` never surface; Go's nodeName returns go/ast's Ident.Name (internal/astgroup/parsers/src/goparser/main.go:156-164), and Go generic type params live in FuncDecl.TypeParams, not the identifier; Python's parser emits the def/class identifier constrained by Python's own grammar (internal/astgroup/parsers/src/pyparser/main.go:223). Markdown-active characters structurally cannot reach safeSymbolAnchor from any supported parser, so tightening the filter would be speculative hardening against unreachable input.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/symbol_anchor.go
- internal/astgroup/parsers/src/braceparser/parse_core.go
- internal/astgroup/parsers/src/goparser/main.go
- internal/astgroup/parsers/src/pyparser/main.go
