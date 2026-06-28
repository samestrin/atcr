---
id: mem-2026-06-28-5b187d
question: "Should the alloc/free/emit/pins guest Wasm ABI be extracted into a shared guest package across goparser, pyparser, and future parsers?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/goparser/main.go, internal/astgroup/parsers/src/pyparser/main.go, internal/astgroup/parsers/build.sh]
tags: [clarifications, epic-13.1_ast_plugin_architecture, wasm, guest-abi, architecture, premature-optimization]
retrievals: 0
status: active
type: clarifications
---

# Should the alloc/free/emit/pins guest Wasm ABI be extracted 

## Decision

No — not for the 13.1 PoC. Defer to a post-PoC TD item.

Key facts:
1. The ABI boilerplate is ~29 lines per parser (pins var + alloc/free/emit), duplicated across exactly 2 files (goparser/main.go:48-57, 59-60, 183-191 and pyparser/main.go:31-45, 301-309). ~58 lines total is below extraction threshold.
2. No 13.4 brace parsers exist yet — the duplication is strictly 2 files.
3. Epic binding: "OUT of scope: parsers for any language beyond Go + Python (PoC scope)." The PoC never grows beyond 2 parsers.
4. Extracting would require `replace` directives in each parser's go.mod and path-sensitive build.sh changes (build.sh:31-38 uses a plain cd + GOOS=wasip1 GOARCH=wasm go build) — non-trivial complexity for zero functional gain.
5. Keep the pinned-pointer ABI. It is correct and stable for Go ≥ 1.21 wasip1/wasm; the GC non-moving assumption is documented at goparser/main.go:41-45; build.sh:18-24 enforces Go ≥ 1.24.
6. A reserved-arena alternative would require the wazero host to know the slab layout — a host refactor with no PoC benefit.

Post-PoC trigger: if parser count grows beyond 2, extract to a shared guest package under internal/astgroup/parsers/src/wasmabi/ with replace directives.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/goparser/main.go
- internal/astgroup/parsers/src/pyparser/main.go
- internal/astgroup/parsers/build.sh
