---
id: mem-2026-06-23-1a2edd
question: "TD row where Problem and Fix are mismatched (different concerns conflated in one row) — how to dispose and which source-tree file to correct?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/dedupe.go, reconcile/ambiguous.go, .planning/sprints/active/8.0_reconciler_library/sprint-plan.md]
tags: [clarifications, sprint-8.0_reconciler_library, process, TD-004, resolve-td, AmbiguousCluster, conflated-row]
retrievals: 0
status: active
type: clarifications
---

# TD row where Problem and Fix are mismatched (different conce

## Decision

When a TD row's Problem and Fix are from two distinct concerns (e.g., a doc-accuracy problem paired with an external repo-admin fix), resolve them separately: (1) evaluate whether the Problem is already resolved by correct code execution — in the AmbiguousCluster / TD-004 case, reconcile/dedupe.go:27 correctly declares AmbiguousCluster and the code shipped correctly; mark the Problem side [x] (resolved); (2) the mismatched Fix (branch protection) belongs to a separate row and should not be actioned here. The only source-tree artifact to correct is sprint-plan.md:312 task 2.2.7, which still lists AmbiguousCluster under the ambiguous.go move step despite the sprint carrying the correct carry-forward note at sprint-plan.md:295. This is a doc-only fix in an already-executed plan — no code change required. General rule: a TD row with file=unknown:0 and no code citation where the Problem is a task-text accuracy issue and the Fix is an external action should be split mentally — mark [x] for the accuracy side if the code is correct, [/] for the external action in its own row.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go
- .planning/sprints/active/8.0_reconciler_library/sprint-plan.md
