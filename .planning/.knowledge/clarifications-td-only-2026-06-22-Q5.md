---
id: mem-2026-06-22-320089
question: "Why does reconcile.Result.JSONFindings() hand-copy fields into JSONFinding, and which fields are intentionally omitted?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go]
tags: []
retrievals: 0
status: active
type: clarifications (td-only, 2026-06-22)
---

# Why does reconcile.Result.JSONFindings() hand-copy fields in

## Decision

reconcile.Result.JSONFindings() (internal/reconcile/emit.go:137) hand-copies each Merged finding into a JSONFinding literal. Three JSONFinding fields are intentionally NOT copied because they are output-only, set by downstream stages, never by reconcile: FixWarning (set only by the verify fix phase, emit.go:131-132), ClusterMerged and ClusterID (set only by the debate cross-examination apply path, emit.go:111,123-124). The documented contract is "reconcile-time producers MUST leave it empty." Consequence: any NEW omitempty JSONFinding field added later must be remembered in this serializer too (no compiler help) — a field-addition footgun. The safe guard is a reflection-based drift-guard test asserting JSONFindings() populates every JSONFinding field except the allowlist {FixWarning, ClusterMerged, ClusterID}; a struct-embedding restructure is heavier and riskier. "Add the field to the literal" does NOT compile because Merged has no such fields.</answer>
<parameter name="tags">td-clarification, td-only, maintainability, reconcile, serialization, epic-7.0

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
