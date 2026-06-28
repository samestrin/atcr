---
id: mem-2026-06-27-3c0b59
question: "AST grouping benchmark and adopt-or-revert policy for epic 13.1: does a benchmark suite exist, and what happens if AST grouping fails to beat Jaccard?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/reconcile/cluster_test.go, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/completed/13.0_semantic_ncd_deduplication.md, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md]
tags: [clarifications, epic-13.1_ast_plugin_architecture, testing, process, benchmark, adopt-or-revert]
retrievals: 0
status: active
type: clarifications epic-13.1_ast_plugin_architecture Q4 2026-06-27
---

# AST grouping benchmark and adopt-or-revert policy for epic 1

## Decision

(a) No AST-grouping benchmark suite exists — the fixture set must be built during the epic, following the same pattern as 13.0's ncd_corpus.json (33 labeled pairs). reconcile/cluster_test.go:10-59 are unit correctness tests for the ±3-line Cluster() only, not an accuracy benchmark. (b) The 13.0 "NOT ADOPTED, revert" precedent does not map cleanly onto 13.1. AC1+AC2 (Wazero runtime + plugin caching) have standalone downstream value: the 13.0 Outcome section marks 13.2 Bipartite+DBSCAN as HIGH impact, sourcing edge weights from 13.1 AST isomorphism — a full revert would break the stated plan for 13.2. The adopt-or-revert boundary MUST be recorded in the epic's Clarifications section before execution begins: whether AC1+AC2 ship independently if AC3 is falsified, or everything reverts, needs an explicit pre-decision (as 13.0's Q6 was).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/reconcile/cluster_test.go
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/completed/13.0_semantic_ncd_deduplication.md
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/13.1_ast_plugin_architecture.md
