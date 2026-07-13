---
id: mem-2026-07-13-9b4e1b
question: "How should an ambiguous, Est-0-minute 'assert the host does X' TD item be resolved — doc note, test, or runtime check?"
created: 2026-07-13
last_retrieved: ""
sprints: [22.2_astgroup_shared_guest_abi]
files: [internal/astgroup/parsers/src/guestabi/guestabi.go, internal/astgroup/host.go, .planning/sprints/active/22.2_astgroup_shared_guest_abi/tech-debt-captured.md]
tags: [clarifications, sprint-22.2_astgroup_shared_guest_abi, correctness, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# How should an ambiguous, Est-0-minute 'assert the host does 

## Decision

Prefer a doc-comment note over a new test or runtime assertion, when: (1) the TD row's Est Minutes is 0 (signals a documentation-weight fix, not new test infrastructure), and (2) the asserted behavior is host-side and unobservable from the guest, so no runtime assertion is possible from guest code. Precedent in this project: guestabi's TD-001 (Lookup bounds-check contract) resolved via a doc-note addition to guestabi.go (commit 69f82c2a), also Est 0. Applied to TD-003 (guestabi.go:63, Emit relies on host to free resPtr): host.go:362 already unconditionally defers free(rptr) in the host's Parse() path, so the fix is a doc note on Emit's comment documenting the host-must-free contract, mirroring the Lookup precedent — not a runtime assertion (guest can't observe host free() calls) and not required to be a new test, though tech-debt-captured.md's own "documentation / host-contract test" wording means a lightweight host test would also satisfy the row if preferred.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/guestabi/guestabi.go
- internal/astgroup/host.go
- .planning/sprints/active/22.2_astgroup_shared_guest_abi/tech-debt-captured.md
