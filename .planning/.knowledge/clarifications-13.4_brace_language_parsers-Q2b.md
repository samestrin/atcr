---
id: mem-2026-06-29-c2d201
question: "braceparser pins-map \"leak\": should the guest cap growth defensively, or is the fix host-side?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/main.go, internal/astgroup/parsers/src/goparser/main.go, internal/astgroup/host.go]
tags: [clarifications, epic-13.4_brace_language_parsers, architecture, braceparser, wasm, pins-map, abi]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q2 second-run (2026-06-29)
---

# braceparser pins-map "leak": should the guest cap growth def

## Decision

Not a real leak — the host already satisfies the contract. host.go registers two unconditional deferred free() calls in Parse(): one for the input pointer immediately after alloc, one for the result pointer after p.parse.Call succeeds. The trap path discards the entire module instance via discardOnTrap(), making the pins map moot. The pins-map pattern is verbatim-identical in goparser and pyparser (documented in braceparser/main.go:17-20 header comment). Any defensive guest-side cap would require uniform changes across all three parsers and is therefore a host-owned cross-plugin ABI decision, not a braceparser-only fix. Close as not-a-bug.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/main.go
- internal/astgroup/parsers/src/goparser/main.go
- internal/astgroup/host.go
