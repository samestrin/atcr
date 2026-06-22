---
id: mem-2026-06-22-a5324a
question: "For UNDER_ENGINEERING TD items whose fix is purely a test assertion change, is the over-simplification gate's test_only/hard flag a blocker or a structural false positive?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/registry/executor_config_test.go]
tags: [td-clarification, td-only, UNDER_ENGINEERING, over-simplification-gate, test_only, resolve-td]
retrievals: 0
status: active
type: clarifications/td-only/2026-06-22
---

# For UNDER_ENGINEERING TD items whose fix is purely a test as

## Decision

It is a structural false positive. UNDER_ENGINEERING items by definition address test quality, not production correctness. When the TD row's Fix description says to add or strengthen an assertion, a test-only commit is the complete and intended resolution. The over-simplification gate fires because it cannot distinguish between a reward-hack (weakened/narrowed test) and a legitimate test-quality fix — the UNDER_ENGINEERING category is the signal that the test-only change is correct. Confirm the fix matches the TD Fix column verbatim and that the test passes; then mark the row [x]. No production change is needed unless the problem statement also cites a missing implementation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/executor_config_test.go
