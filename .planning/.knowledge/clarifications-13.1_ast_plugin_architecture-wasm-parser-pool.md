---
id: mem-2026-06-27-dc4500
question: "Should a wasmParser module pool be added to parallelize same-language parses in astgroup, and what pool size is acceptable?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/host.go, internal/astgroup/grouper.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, concurrency, wazero, wasm-parser, pool, premature-optimization]
retrievals: 0
status: active
type: clarifications
---

# Should a wasmParser module pool be added to parallelize same

## Decision

No — pooling is premature for the PoC. No pool size or memory budget expansion should be specified.

Key facts:
1. The epic's only performance NFR is <10ms instantiation overhead (satisfied by the compiled-module cache). No parse-throughput or parallelism NFR exists.
2. treeFor() already releases g.mu BEFORE calling parser.Parse() (grouper.go:232-234, comment "Read+parse OUTSIDE g.mu so distinct files parse concurrently"), meaning multiple Groupers already parse DIFFERENT files in parallel via the SharedHost. Serialization only applies to same-language concurrent parses of different files within one Grouper.
3. The realistic bottleneck is the 5-second per-parse timeout and 8 MiB source cap (host.go:268,:254), not lock contention.
4. The 256 MiB cap is already a 16× safety ceiling (host.go:256-261); multiplying by pool size has no budget justification.
5. wasmParser.mu serialization is accepted by design: "A wasm module instance is not safe for concurrent calls, so every Parse is serialized by mu." (host.go:234-235)

If profiling post-PoC confirms real contention, the correct remedy is a per-language instance pool sized to GOMAXPROCS — not SharedHost (which would complicate Close-draining and the singleton contract). That decision requires measured data, not speculation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/host.go
- internal/astgroup/grouper.go
