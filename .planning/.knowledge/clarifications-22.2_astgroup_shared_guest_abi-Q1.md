---
id: mem-2026-07-13-32b2f1
question: "Is a test-only change the complete fix for a testing-category TD row?"
created: 2026-07-13
last_retrieved: ""
sprints: [22.2_astgroup_shared_guest_abi]
files: [internal/astgroup/host_test.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-22.2_astgroup_shared_guest_abi, testing, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only change the complete fix for a testing-categor

## Decision

Yes — when a TD row's Category is "testing" and its Fix column literally requests "Add a host test for X", a committed passing test satisfying that literal request IS the complete fix; no code change is implied. The over-simplification gate's test_only flag is a false positive for this row shape and should be overridden manually. Confirmed for braceparser's bad-pointer+negative-n row (README.md .planning/technical-debt line 72, resolved by TestHost_BraceParseBadPointerAndNegativeN, internal/astgroup/host_test.go:420) — braceparser's parse() already guards `n < 0 || int(n) > len(buf)` correctly, so the coverage gap was real but the underlying behavior was not buggy. A related but distinct correctness bug (negative-n panics in goparser/pyparser) is tracked as its own separate HIGH-severity row and must not be conflated with the testing-category row.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/host_test.go
- .planning/technical-debt/README.md
