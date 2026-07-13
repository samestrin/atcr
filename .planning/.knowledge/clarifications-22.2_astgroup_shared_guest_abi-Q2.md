---
id: mem-2026-07-13-b42940
question: "Is a test-only change the complete fix for goparser's bad-pointer testing-category TD row?"
created: 2026-07-13
last_retrieved: ""
sprints: [22.2_astgroup_shared_guest_abi]
files: [internal/astgroup/host_test.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-22.2_astgroup_shared_guest_abi, testing, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only change the complete fix for goparser's bad-po

## Decision

Yes, same pattern as [[clarifications-22.2_astgroup_shared_guest_abi-Q1]]: goparser/main.go:54's row (Category=testing, Fix="Add host test calling parse with invalid pointer") is fully satisfied by the committed, passing TestHost_GoParseBadPointer (internal/astgroup/host_test.go:464). The separate negative-n correctness bug in goparser (goparser/main.go:52) is tracked as its own HIGH-severity row and is explicitly out of scope for this testing row — do not require a code fix here on that basis.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/host_test.go
- .planning/technical-debt/README.md
