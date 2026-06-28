---
id: mem-2026-06-27-b0c215
question: "Wasm binary asset acquisition: where do .wasm parsers come from and how are they served in a hermetic CI environment?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/.github/workflows/ci.yml, /Users/samestrin/Documents/GitHub/atcr/internal/personas/bundles.go, /Users/samestrin/Documents/GitHub/atcr/skill/skill.go, /Users/samestrin/Documents/GitHub/atcr/personas/personas.go, /Users/samestrin/Documents/GitHub/atcr/internal/personas/client.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, CI/CD, go:embed, wasm]
retrievals: 0
status: active
type: clarifications epic-13.1_ast_plugin_architecture Q1 2026-06-27
---

# Wasm binary asset acquisition: where do .wasm parsers come f

## Decision

Vendor the .wasm parsers in-repo and serve via go:embed. "Fetch" in AC2 means "load from the embedded FS"; "cache" means the Wazero compiled-module instance cache. This is the only hermetic option with zero new toolchain requirements, consistent with the project's established pattern (internal/personas/bundles.go:14, skill/skill.go, personas/personas.go). Runtime download (option b) requires a network seam and supply-chain trust decision — unnecessary for a two-parser PoC. Build-time generation (option c) requires a Makefile and Wasm toolchain in CI — neither exists. A runtime override path (check a configurable directory first, fall back to embedded) satisfies the "drop in new .wasm files" business criterion as an extension.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/.github/workflows/ci.yml
- /Users/samestrin/Documents/GitHub/atcr/internal/personas/bundles.go
- /Users/samestrin/Documents/GitHub/atcr/skill/skill.go
- /Users/samestrin/Documents/GitHub/atcr/personas/personas.go
- /Users/samestrin/Documents/GitHub/atcr/internal/personas/client.go
