---
id: mem-2026-07-11-0b3f12
question: "Is a test-only diff acceptable for closing a TD item flagged as a latent (not-live) coverage hole, despite resolve-td's over-simplification gate flagging it test_only?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [skill/skill_test.go]
tags: [clarifications, sprint-20.0_standalone_skill_release, testing, resolve-td, over-simplification-gate]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only diff acceptable for closing a TD item flagged

## Decision

Accept the test-only diff as complete — this is a genuine false positive on the test_only heuristic. When a TD row's own Problem statement says the content is "currently clean... a latent coverage hole, not a live defect," there is no implementation bug to fix, so requiring an implementation-file edit is a false demand. The prescribed FIX text itself described a test-only change (loop existing assertions over additional embedded constants), which is exactly what should land. The test_only gate's default suspicion (weakened assertion, stubbed body) doesn't apply to an additive, falsifiable assertion-loop widening verified by a plant-and-fail check.

Justification:
- TD row (code-review/reconciled/td-stream-merged.txt) explicitly states "Currently clean (verified by grep) — a latent coverage hole, not a live defect."
- The prescribed FIX text was itself test-only ("loop the same assertions... verify by planting /Users/foo and confirming the test fails"), matching what was implemented in skill/skill_test.go's TestSkill_NoAbsoluteOrClaudePaths.
- General principle: treat "coverage-hole" TD items (where the Problem statement itself says content is already clean) as a recognized exception category to the test_only over-simplification gate — note this in the fix commit message so a human reviewer has the same context.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- skill/skill_test.go
