---
id: mem-2026-07-01-875ae5
question: "diff_smell test_only=hard flag on the per-command flag-validation drift guard (cmd/atcr/docs_audit_test.go:188) — does the committed test-only change resolve it?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: [cmd/atcr/docs_audit_test.go]
tags: [clarifications, epic-15.0_documentation_audit, testing, resolve-td, diff_smell]
retrievals: 0
status: active
type: clarifications
---

# diff_smell test_only=hard flag on the per-command flag-valid

## Decision

The test-only change fully resolves the TD item; no non-test change is required. This is a documentation-drift regression guard by design (epic 15.0's own clarification frames T1/T4 as "Go tests that parse the docs and compare against the compiled binary / config schema"), there is no production requirement for atcr to validate its own docs at runtime, and `validateInvocationTokens`/`reachableFlags` have zero callers outside `docs_audit_test.go` — a test-only home is correct here, not a fix masked as a test.

General pattern: when `diff_smell` flags a change as `test_only=hard` because it only touches a `_test.go` file, check whether the test IS the deliverable (a drift/regression guard whose job is to compare docs/config against live code or the compiled binary) rather than a test disguising a missing production fix. Evidence that distinguishes a legitimate test-only guard from reward-hacking: (1) grep the changed helper/logic for callers outside `_test.go` files — zero hits means it was never meant to ship in production code; (2) confirm a genuine RED→GREEN commit pair exists (a failing test added first, then the real logic implemented, not just loosened assertions); (3) check the epic/sprint's own recorded Clarifications for language explicitly designing the deliverable as a test.

Justification:
- cmd/atcr/docs_audit_test.go:186-230 (`reachableFlags`) resolves the specific command chain (root → ancestors → resolved command) and unions ancestor persistent flags + resolved command's local flags + help/version — real per-command logic, not a weakened assertion.
- cmd/atcr/docs_audit_test.go:277-287 `validateInvocationTokens` now calls `reachableFlags(tokens)` per invocation instead of a global flag union.
- cmd/atcr/docs_audit_test.go:333-364 `TestFlagValidationIsPerCommand` asserts both negative (`review --checkpoint`, `init --json` rejected) and positive (`benchmark run --checkpoint`, `doctor --json` pass) cases; `go test ./cmd/atcr/...` is green.
- Git history: commit `dc85269f` ("test: RED - reproduce per-command flag validation gap") added a failing test, `c84ac98b` ("fix: GREEN - validate documented flags per-command...") implemented the real fix — a genuine RED→GREEN cycle.
- `grep -rn "validateInvocationTokens\|reachableFlags\|validateSubcommandChain" --include="*.go" .` (excluding `_test.go`) returns zero hits — confirms no equivalent production validation exists or is missing.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/docs_audit_test.go
