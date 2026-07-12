---
id: mem-2026-07-11-74d940
question: "Is a test-only diff acceptable when a TD item's root cause is the test's own assertion-continuation strategy (require vs assert), not a production defect?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [cmd/atcr/backend_contract_test.go]
tags: [clarifications, sprint-20.0_standalone_skill_release, testing, resolve-td, over-simplification-gate]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only diff acceptable when a TD item's root cause i

## Decision

Approve as complete. A test-only diff is the ONLY correct shape for this class of TD item — the defect lives in the test's own assertion strategy (require.FileExists aborting on first miss vs assert.FileExists surfacing all misses), not in application code, so there is nothing else to touch. Strengthening assert vs require (surfacing MORE failures per run) is the opposite of a reward-hack (which would weaken assertions). Confirm the fix closes a gap against a pre-existing sprint-design requirement rather than inventing new scope, and confirm surrounding require checks that gate meaningful downstream reads were deliberately left untouched (proving the diff is scoped, not a blanket sweep).

Justification:
- cmd/atcr/backend_contract_test.go:146-148 — single-line require.FileExists -> assert.FileExists change, nothing else touched (commit 6d75c0c3).
- Sprint-design.md's own "Defensive Measures Required" section pre-dates this TD item and already specifies favoring assert over require for output-tree checks — the fix closes a gap against an existing spec, not invented scope.
- General principle: when a TD item's Problem statement is about the test's OWN correctness/strategy (not the code under test), a test-only diff is not merely acceptable — it is the only fix shape that can exist.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/backend_contract_test.go
