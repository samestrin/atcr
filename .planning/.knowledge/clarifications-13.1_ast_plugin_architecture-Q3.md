---
id: mem-2026-06-27-54ae9b
question: "Why does blockIdx increment for skipped block siblings in cover.go, and why is this the correct behavior?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/cover.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, cover, blockIdx, sibling-indexing, ast-addressing]
retrievals: 0
status: active
type: clarifications
---

# Why does blockIdx increment for skipped block siblings in co

## Decision

The blockIdx counter tracks how many block-typed siblings were skipped before the covering child, giving the covering child a positional index that is stable and unique among block siblings. Without incrementing for non-covering block siblings, two sibling blocks separated by a non-block child (e.g., positions 0 and 2 in the child list) would both receive blockIdx=0 and collide in the structural address. The increment is consumed via segment(ch, blockIdx) when the covering child is itself a block. Any "fix" that removes or conditions away the increment for skipped siblings would destroy the uniqueness guarantee the address scheme is built on, directly breaking the Merkle-hash grouping invariant in the 13.1 AST plugin architecture.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/cover.go
