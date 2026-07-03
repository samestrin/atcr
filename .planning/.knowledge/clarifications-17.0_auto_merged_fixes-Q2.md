---
id: mem-2026-07-03-4ce444
question: "Should a test-only DoD item be considered resolved, or does it need an accompanying implementation change when the diff_smell gate flags it as test-only?"
created: 2026-07-03
last_retrieved: ""
sprints: [17.0_auto_merged_fixes]
files: [internal/verify/localvalidate.go, internal/verify/localvalidate_test.go]
tags: []
retrievals: 0
status: active
type: project
---

# Should a test-only DoD item be considered resolved, or does 

## Decision

A test-only change IS a valid resolution when the DoD item was "add the missing adversarial/security test" and the implementation already enforces the invariant by construction. The diff_smell "test-only" flag is a NEEDS_REVIEW heuristic for reward-hacks (test-only patches that dodge a real fix or weaken/skip assertions), not a hard failure. Confirm it is a false positive by verifying the property actually holds in code; only require an implementation change if the test had to weaken or skip an assertion to pass. Example: internal/verify/localvalidate.go structurally prevents model-derived strings from reaching argv — argv is operator-config-only or the Go default, no shell interprets it, "no PR/diff/model-derived value can reach it" (localvalidate.go:75-76,145), and Passed() is exit-code-only per AC 02-03 (localvalidate.go:61-67). The added adversarial test covers that already-correct property, so it resolves the item with no code change.</answer>
<parameter name="tags">clarifications, sprint-17.0_auto_merged_fixes, testing, process, diff_smell, tdd

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/localvalidate.go
- internal/verify/localvalidate_test.go
