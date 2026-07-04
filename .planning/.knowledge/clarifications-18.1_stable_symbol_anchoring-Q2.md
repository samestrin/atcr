---
id: mem-2026-07-04-6dbb53
question: "Where should TD Problem-field prefixes be injected in atcr — reconciler emit layer or downstream group_td?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/reconcile/gate.go, internal/astgroup/grouper.go]
tags: [clarifications, epic-18.1_stable_symbol_anchoring, architecture, reconcile]
retrievals: 0
status: active
type: clarifications
---

# Where should TD Problem-field prefixes be injected in atcr �

## Decision

The atcr reconciler's emit layer (internal/reconcile) is correct. The reconciler never writes README.md anywhere in this repo — it only writes reconciledDir artifacts (findings.txt/json, report.md, summary.json, ambiguous.json, disagreements.json) via Emit (internal/reconcile/emit.go:254-293). README.md generation is downstream/external (llm_support_group_td, outside this repo). JSONFinding.Problem (emit.go:66) is populated from the merged finding in JSONFindings() (emit.go:155-191, line 168) and stamped before Emit runs, at the RunReconcile call site (internal/reconcile/gate.go:238-240) — the same pattern used for other derived-field stamping before caching/emit. The AST data needed already exists: astgroup.Node.Name (internal/astgroup/node.go:20-23) via Grouper.GroupKey's CoveringBlock resolution (internal/astgroup/grouper.go:150-211), wired at gate.go:225-227 for clustering only — the Name field itself isn't currently surfaced and needs new code at the gate.go:238 stamping point.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/reconcile/gate.go
- internal/astgroup/grouper.go
