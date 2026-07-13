---
id: mem-2026-07-13-cfabf3
question: "Is a test-only change the complete fix for pyparser's bad-pointer testing-category TD row?"
created: 2026-07-13
last_retrieved: ""
sprints: [22.2_astgroup_shared_guest_abi]
files: [internal/astgroup/host_test.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-22.2_astgroup_shared_guest_abi, testing, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only change the complete fix for pyparser's bad-po

## Decision

Yes, same pattern as [[clarifications-22.2_astgroup_shared_guest_abi-Q1]]: pyparser/main.go:44's row (Category=testing, Fix="Add host test with invalid pointer for pyparser") is fully satisfied by the committed, passing TestHost_PyParseBadPointer (internal/astgroup/host_test.go:493). The separate negative-n correctness bug in pyparser (pyparser/main.go:43) is tracked as its own HIGH-severity row and is explicitly out of scope for this testing row.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/host_test.go
- .planning/technical-debt/README.md
