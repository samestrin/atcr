---
id: mem-2026-06-26-423367
question: "What is the intended scope for unifying verdictRank with merge.go's precedence in the repro package?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/repro/repro.go, internal/reconcile/merge.go]
tags: [clarifications, epic-11.0_executing_reviewers, architecture, reconcile, repro, verdict-rank]
retrievals: 0
status: active
type: clarifications
---

# What is the intended scope for unifying verdictRank with mer

## Decision

Export verdictRank as VerdictRank from internal/reconcile/merge.go — no new shared-constants package is needed. The repro package already imports internal/reconcile (repro.go:14), so the callers are one import hop apart. Two local copies currently diverge: merge.go:168 normalizes with strings.ToLower(strings.TrimSpace(verdict)) before the switch; repro.go:99 does not. Exporting from merge.go:167 unifies both call sites under the normalizing version. A separate constants package would duplicate what already lives in the public reclib library and is explicitly out of scope per the epic's "public library types NOT touched" constraint.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/repro/repro.go
- internal/reconcile/merge.go
