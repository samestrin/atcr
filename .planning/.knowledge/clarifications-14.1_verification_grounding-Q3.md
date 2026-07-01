---
id: mem-2026-06-30-85a389
question: "What line tolerance and fallback should a patch-grounding/diff-matching validator use?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [internal/reconcile/cluster.go, internal/reconcile/grouper.go, internal/reconcile/finding.go]
tags: [clarifications, epic-14.1_verification_grounding, implementation]
retrievals: 0
status: active
type: clarifications
---

# What line tolerance and fallback should a patch-grounding/di

## Decision

Use a ±3 line tolerance with an evidence-snippet fallback before dropping an ungrounded finding. ±3 is not arbitrary — it is the codebase's already-established line-proximity convention: `const lineProximity = 3` at internal/reconcile/cluster.go, documented as "the inclusive line distance that clusters two findings on the same location," and used by proximityClusters (internal/reconcile/grouper.go) as the single-linkage gap threshold for location-based dedupe. Reusing ±3 keeps the codebase's two proximity concepts (dedupe clustering vs. patch grounding) numerically aligned. The evidence-snippet fallback (match the finding's EVIDENCE field against the patch's added/modified text when the line is outside tolerance) should be enabled — it reuses the existing `Evidence string` wire field on the Finding struct (internal/reconcile/finding.go) with no schema change, and it is the prescribed mitigation in epic risk tables for "LLMs provide approximate lines" (fuzzy/snippet matching to avoid dropping real issues).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/cluster.go
- internal/reconcile/grouper.go
- internal/reconcile/finding.go
