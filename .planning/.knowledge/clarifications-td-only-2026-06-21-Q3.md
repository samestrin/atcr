---
id: mem-2026-06-21-e2c000
question: "debate.go applyRulings before applyClusterMerges: can a gray-zone cluster member's single-finding verdict be silently discarded (HIGH \"lost debate verdict\" path)?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/debate/debate.go, internal/reconcile/disagree.go]
tags: [td-clarification, td-only, correctness, debate, gray-zone, invariant, not-reproducible, applyRulings, applyClusterMerges]
retrievals: 0
status: active
type: clarifications skill 2026-06-21
---

# debate.go applyRulings before applyClusterMerges: can a gray

## Decision

Close as not-reproducible — the HIGH "lost verdict" path is structurally unreachable by construction. Two independent gates prevent co-entry into rulings and mergeClusters: (1) BuildDisagreements (internal/reconcile/disagree.go ~line 127) populates grayKeys from all cluster member locations and excludes any matching finding from solo/split/verification tiers with `continue` — so no gray member ever enters df.Items as a non-gray-zone item; (2) the KindGrayZone branch in the debate.go goroutine switch (internal/debate/debate.go ~line 195) routes gray-zone items to clusterMerge/separate and never sets oc.apply=true — so no gray-zone item ever builds a rulings entry. Both gates must break simultaneously for the loss-of-verdict path to activate. The defensive invariant-pinning test is already tracked as the separate LOW item at internal/debate/cluster.go:applyClusterMerges in the epic-6.1 section. Union ChallengeSurvived in mergeVerification is out of scope until a future radar change breaks the invariant.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debate/debate.go
- internal/reconcile/disagree.go
