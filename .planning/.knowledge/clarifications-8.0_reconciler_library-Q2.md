---
id: mem-2026-06-23-bd1cf8
question: "What module path should the extracted reconciler library use?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [go.mod]
tags: [clarifications, epic-8.0_reconciler_library, architecture, module-path, namespace]
retrievals: 0
status: active
type: clarifications
---

# What module path should the extracted reconciler library use

## Decision

Use github.com/samestrin/atcr/reconcile. The real ATCR module is github.com/samestrin/atcr (go.mod:1); the github.com/atcr org referenced in the epic plan does not exist — every occurrence in the repo is in planning documents written aspirationally. Keeping the path under github.com/samestrin/atcr/ is consistent with the existing namespace, makes the ATCR relationship obvious to consumers, and works cleanly with the nested-module approach (physical location ./reconcile/ in the repo, module path github.com/samestrin/atcr/reconcile, consumed via a replace directive during development).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- go.mod
