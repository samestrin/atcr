---
id: mem-2026-06-27-4856d8
question: "AST gate vs ±3-line proximity gate in the reconciler: should AST structural identity replace or augment the existing line proximity gate?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/reconcile/cluster.go, /Users/samestrin/Documents/GitHub/atcr/reconcile/reconcile.go, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, reconciler, clustering, proximity-gate]
retrievals: 0
status: active
type: clarifications epic-13.1_ast_plugin_architecture Q3 2026-06-27
---

# AST gate vs ±3-line proximity gate in the reconciler: shoul

## Decision

AST structural identity should REPLACE the ±3-line proximity gate (lineProximity=3 in reconcile/cluster.go:5-7) as the primary grouping signal, not augment it. The ±3-line gate is the sole pre-filter today: findings more than 3 lines apart are split into separate clusters before DedupeCluster ever runs, making cross-window dedup structurally impossible. Augmenting inside the ±3 window would still leave cross-window grouping broken — exactly the failure mode the epic targets. AC3 requires grouping "findings that are offset by whitespace or minor line-number drift"; whitespace offsets can easily exceed ±3. The ±3 window may be retained only as a fallback when no .wasm parser is available for a language.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/reconcile/cluster.go
- /Users/samestrin/Documents/GitHub/atcr/reconcile/reconcile.go
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md
