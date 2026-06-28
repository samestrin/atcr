---
id: mem-2026-06-27-aedca5
question: "Why must the benchmark's zero-FP false-positive assertion for AST grouping remain strict rather than being relaxed to a percentage tolerance?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/benchmark_test.go, internal/astgroup/grouper.go, internal/astgroup/cover.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, testing, benchmark, false-positive, ast-grouping, merkle-hash]
retrievals: 0
status: active
type: clarifications
---

# Why must the benchmark's zero-FP false-positive assertion fo

## Decision

The grouper key is `path + "\x00" + addr + "\x00" + MerkleHash(block)` with a "drift-invariant, sibling-distinguishing" structural address. By construction, two findings that map to genuinely distinct AST nodes always receive distinct keys — zero false positives is the expected invariant, not a stretch goal. Relaxing to a percentage tolerance in a test-only change without fixing the implementation is a reward hack that undermines the AC3 benchmark gating function. The root cause of any FP is either a corpus-labeling error (the AST correctly identifies the same covering node, but the corpus label says "different block") or a bug in CoveringBlock. Neither justifies relaxing the assertion. The epic's adopt-or-revert policy explicitly gates the AC3 production default-flip on the benchmark; a weakened threshold makes that gate meaningless.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/benchmark_test.go
- internal/astgroup/grouper.go
- internal/astgroup/cover.go
