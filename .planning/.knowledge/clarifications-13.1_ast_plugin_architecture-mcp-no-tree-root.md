---
id: mem-2026-06-27-80b3d4
question: "When the MCP server has no checked-out source tree, should the astgroup grouper hard-disable file reads or require an explicit root?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/grouper.go, internal/mcp/handlers.go, internal/reconcile/astgrouping.go, internal/reconcile/gate.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, mcp, grouper, empty-root, hard-disable]
retrievals: 0
status: active
type: clarifications
---

# When the MCP server has no checked-out source tree, should t

## Decision

Hard-disable reads: MCP handler passes Root="" to signal no checked-out tree; grouper short-circuits GroupKey to return "" (proximity fallback) when root is empty, without any file I/O.

Three reasons this is correct:
1. internal/reconcile/astgrouping.go:74-78 already documents this scenario: "the source file is absent (e.g. an MCP reconcile without a checked-out tree), GroupKey returns '' and that finding falls back to proximity grouping, so a missing parser or absent tree never errors a reconcile."
2. The canonical project pattern is established at internal/reconcile/gate.go:237: empty root = "feature disabled" not "use cwd." The grouper must follow this same convention.
3. internal/mcp/handlers.go:323-327 currently hardcodes Root:"." which silently resolves to cwd — incorrect when there is no checkout. Changing to Root:"" makes the no-tree case intentional.

The gap to close: internal/astgroup/grouper.go:111-113 (canonicalPath) currently converts root=="" to "." silently — that must be changed to treat empty root as a hard-disable short-circuit.

Requiring an explicit root would contradict the documented design goal and break valid MCP use cases (remote review, CI without a source checkout).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/grouper.go
- internal/mcp/handlers.go
- internal/reconcile/astgrouping.go
- internal/reconcile/gate.go
