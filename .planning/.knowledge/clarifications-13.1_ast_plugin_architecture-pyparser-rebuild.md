---
id: mem-2026-06-27-61dc0b
question: "When fixing the pyparser's multi-line def/class header misparse, is regenerating python.wasm a breaking change due to Merkle hash differences, and should it be deferred to a new epic?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/pyparser/main.go, internal/astgroup/parsers/build.sh, internal/astgroup/host_test.go, internal/astgroup/grouper.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, scope, wasm, pyparser, merkle-hash, determinism, python]
retrievals: 0
status: active
type: clarifications
---

# When fixing the pyparser's multi-line def/class header mispa

## Decision

No new epic is needed and the wasm rebuild is not a breaking change. Two key facts:

1. Wasm rebuild is the normal commit cycle: pyparser/main.go:14 already instructs "Regenerate the vendored .wasm via internal/astgroup/parsers/build.sh" — this is not an exceptional step; the script lives at internal/astgroup/parsers/build.sh (not parsers/build.sh, which does not exist).

2. The Merkle hash change is a correctness fix, not a regression: merkle.go:8-14 explicitly excludes line numbers from MerkleHash to achieve drift-invariance; grouper.go:191-196 states the Merkle hash is a "defensive cross-check" rather than load-bearing (the addr segment carries the grouping identity). Findings previously assigned to wrong group keys (misparsed covering block) will be re-keyed to correct ones — desired behavior. There are no external consumers locked to the old incorrect hashes.

The fix crosses exactly 3 files: pyparser/main.go (algorithm: track open-paren depth across physical lines, join continuation lines before isHeader/classify), python.wasm (binary rebuild), host_test.go (new multi-line-header fixture). This is well within TD session scope but must NOT be absorbed into group 1 (which does not cover host_test.go fixtures or wasm rebuilds) — it warrants its own ungrouped TD session.

Risk of deferral: leaving the misparse unfixed means any Python file with multi-line function signatures silently falls back to ±3-line proximity grouping (grouper.go:148-150 returns "" for bad parse), defeating AC3 for the most common Python patterns.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/pyparser/main.go
- internal/astgroup/parsers/build.sh
- internal/astgroup/host_test.go
- internal/astgroup/grouper.go
