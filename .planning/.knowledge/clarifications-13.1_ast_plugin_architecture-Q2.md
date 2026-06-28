---
id: mem-2026-06-27-389394
question: "tree-sitter-in-wazero feasibility: how to approach the technical uncertainty of running tree-sitter parsers in wazero without CGO?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/go.mod, /Users/samestrin/Documents/GitHub/atcr/reconcile/go.mod, /Users/samestrin/Documents/GitHub/atcr/internal/verify/syntaxguard.go, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, wasm, wazero, tree-sitter, spike]
retrievals: 0
status: active
type: clarifications epic-13.1_ast_plugin_architecture Q2 2026-06-27
---

# tree-sitter-in-wazero feasibility: how to approach the techn

## Decision

Spike-first, testing the combined engine+grammar .wasm (built via `tree-sitter build --wasm`, which bundles the Emscripten-compiled runtime alongside the grammar) — NOT a bare grammar .wasm, which is a non-starter. The combined Emscripten path is technically plausible in wazero (wazero can host Emscripten modules if "env" host imports are satisfied manually), but unproven. Time-box to ≤1 day. If blocked, keep the plugin abstraction interface unchanged and back each language with a pure-Go implementation (Go: stdlib go/ast/go/parser, already proven in this project at internal/verify/syntaxguard.go:4-6; Python: WASI-targeted pure-Go grammar). The abstraction is the durable design asset, not the Wasm backend. Hard-stopping on Wasm-only sacrifices a sound architectural improvement over a backend implementation detail.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/go.mod
- /Users/samestrin/Documents/GitHub/atcr/reconcile/go.mod
- /Users/samestrin/Documents/GitHub/atcr/internal/verify/syntaxguard.go
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md
