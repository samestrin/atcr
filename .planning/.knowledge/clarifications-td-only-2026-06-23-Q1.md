---
id: mem-2026-06-23-75366c
question: "When /resolve-td --group flags a TD item as out of group scope because its fix requires editing files in another package, should I re-run without --group or reassign the item to a different group in the TD README?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/stream/fileindex.go, internal/reconcile/validate.go, internal/reconcile/gate.go]
tags: [td-clarification, td-only, scope, resilience, context-threading, resolve-td, group-scope]
retrievals: 0
status: active
type: clarifications --from=resolve-td td-only 2026-06-23
---

# When /resolve-td --group flags a TD item as out of group sco

## Decision

Re-run `/resolve-td` without `--group`. When the fix for a grouped TD item is atomic across a cross-package call chain — and the collateral files are not themselves separate TD items in another group — the group boundary is artificial and reassigning the row label would not resolve the dependency. The correct action is to drop the `--group` restriction so the fix can touch all files in the call chain.

Example: internal/stream/fileindex.go:46 (Group 2) required editing internal/reconcile/validate.go:18 and internal/reconcile/gate.go:214 to thread context. Those reconcile files were not listed TD items in any group; they were collateral edits. The call chain is RunReconcile (gate.go) → validateFindingPaths (validate.go) → BuildFileIndex (fileindex.go), and the context thread is inseparable.

Evidence:
- internal/reconcile/gate.go:214: RunReconcile already holds ctx and calls validateFindingPaths(res.Findings, opts.Root)
- internal/reconcile/validate.go:18: validateFindingPaths calls stream.BuildFileIndex(root) — adding ctx is one argument
- internal/stream/fileindex.go:46: BuildFileIndex uses exec.Command("git", ...) with no context — fix is exec.CommandContext(ctx, "git", ...)
- The reconcile package already imports stream (validate.go:3), so no new import is needed

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/fileindex.go
- internal/reconcile/validate.go
- internal/reconcile/gate.go
