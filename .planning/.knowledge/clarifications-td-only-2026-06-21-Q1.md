---
id: mem-2026-06-21-34c064
question: "applyRulings replaces reconcile.Verification wholesale — should the fix add a Judge field to reconcile.Verification (option a), preserve Skeptic/Notes in-place (option b), or defer (option c)?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/debate/emit.go, .planning/epics/completed/6.0_cross_examination.md]
tags: [td-clarification, td-only, architecture, correctness, verification, debate, reconcile]
retrievals: 0
status: active
type: clarifications skill — td-only 2026-06-21
---

# applyRulings replaces reconcile.Verification wholesale — s

## Decision

Option (a) — add `Judge string \`json:"judge,omitempty"\`` to `reconcile.Verification` following the established additive omitempty pattern (same as `ChallengeSurvived`). In `applyRulings` (internal/debate/emit.go:109-120), check if `findings[i].Verification != nil` and mutate in-place — preserving the original `Skeptic` (multi-voter list from verify) and `Notes` (verify reasoning) — setting only `Verdict`, `ChallengeSurvived`, `Judge`, and reasoning from the ruling. Construct a fresh struct only when `Verification` is nil (pre-verify finding). Option (b) in-place with `Skeptic = judge` is the known ambiguous path (flagged in TD row at internal/debate/emit.go:118 in the epic-6.0 section). The epic design explicitly chose additive omitempty fields on Verification (see ChallengeSurvived, reconcile/emit.go:51). Justification: reconcile/emit.go:40-52 (struct definition + pattern); debate/emit.go:109-120 (wholesale replacement to fix); epic-6.0 Clarifications Q1 (additive omitempty is the contract); TD row emit.go:118 (confirms Skeptic=judge is ambiguous). The fix spans reconcile/ and internal/debate/ — requires running /resolve-td without --group.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/debate/emit.go
- .planning/epics/completed/6.0_cross_examination.md
