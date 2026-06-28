---
id: mem-2026-06-27-0220e0
question: "Where should the wazero Host singleton live — cmd/atcr, internal/reconcile, or internal/astgroup — and should the Grouper file-tree cache remain per-run?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/grouper.go, internal/astgroup/host.go, internal/reconcile/astgrouping.go, internal/reconcile/gate.go, internal/metrics/metrics.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, wazero, astgroup, singleton, object-lifetime]
retrievals: 0
status: active
type: clarifications
---

# Where should the wazero Host singleton live — cmd/atcr, in

## Decision

Split the lifecycle between the two objects. The wazero `Host` (compiled wasm module cache) belongs as a package-level lazy singleton inside `internal/astgroup` — not `internal/reconcile` and not `cmd/atcr`. The `Grouper` (which holds `root` + the file-tree `cache map[string]*parsedFile`) must remain per-`RunReconcile` to preserve file-tree cache isolation; promoting it to any shared lifetime would cause stale AST keys in MCP serve mode where files can change between tool calls. `cmd/atcr` should not own either object — the epic clarifications state "Plugin interface is the stable seam; Wasm vs pure-Go is an implementation detail behind it."

Implementation: change `NewGrouper` to accept an externally-shared `*Host` instead of constructing one; keep `Grouper` per-`RunReconcile`; `close()` sheds only the per-run file cache, not the `Host`. The project precedent for a process-wide singleton is a package-level `DefaultRegistry` inside the owning package (internal/metrics), not hoisted into `cmd/`. `Host` follows the same pattern.

Evidence:
- `internal/astgroup/grouper.go:23-30` — Grouper holds two logically separate caches: `host *Host` (wasm compilation, never stale) and `cache map[string]*parsedFile` (staleness-sensitive). Different safe lifetimes.
- `internal/astgroup/grouper.go:59` — `NewGrouper` calls `NewHost()` each time; the Host layer is the correct singleton target.
- `internal/astgroup/grouper.go:138-176` — `treeFor` caches parsed file trees by canonical path; a singleton Grouper in MCP serve mode would silently return stale AST grouping keys.
- `internal/astgroup/host.go:29-39` — `Host` is already thread-safe (`mu sync.Mutex`), keyed by language string (not file path). No staleness risk. Natural singleton candidate.
- `internal/reconcile/astgrouping.go:29-60` — `lazyGrouper` is the per-run wrapper; accepts a shared Host, passes it through.
- `internal/reconcile/gate.go:225-226` — per-run `opts.Root` can differ between RunReconcile calls (CLI vs MCP with different repos); Grouper must remain per-run.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/grouper.go
- internal/astgroup/host.go
- internal/reconcile/astgrouping.go
- internal/reconcile/gate.go
- internal/metrics/metrics.go
