---
id: mem-2026-07-12-937129
question: "Is a PID-reuse race in process-group reap test helpers a production risk requiring an identity check?"
created: 2026-07-12
last_retrieved: ""
sprints: []
files: [internal/verify/localvalidate_pgroup_unix.go, internal/verify/localvalidate_pgroup_unix_test.go, internal/verify/localvalidate.go]
tags: [clarifications, epic-22.0_process_group_reaping, testing, scope, process-groups, syscall]
retrievals: 0
status: active
type: clarifications
---

# Is a PID-reuse race in process-group reap test helpers a pro

## Decision

Test-only scope is sufficient — no production-code identity check is required. The PID-reuse race lives entirely inside the test's own verification helper (`processAlive`, a signal-0 liveness probe used only to confirm the assertion), not in the production reap path, which kills by process-group id rather than by re-identifying a PID.

- The production kill path (internal/verify/localvalidate_pgroup_unix.go:30-37) sets `Setpgid: true` and calls `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` — a pgid-scoped group kill, not a PID-liveness comparison, so it isn't subject to PID-reuse races.
- The flagged risk is confined to internal/verify/localvalidate_pgroup_unix_test.go:24-34, a test-only helper (`processAlive`) that polls `syscall.Kill(pid, 0)` purely to assert the kill already happened.
- internal/verify/localvalidate.go:100-136 shows the production timeout/cancel path relies solely on `configureProcessGroup` + `cmd.WaitDelay` as backstop — there is no PID-identity logic in production code.
- General pattern: kernel-enforced pgid-scoped kills (`syscall.Kill(-pid, ...)`) are not exposed to the PID-reuse race that a naive PID-liveness re-check would be — useful precedent when a code-review gate flags a test-only fix touching a similar liveness-probe helper.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/localvalidate_pgroup_unix.go
- internal/verify/localvalidate_pgroup_unix_test.go
- internal/verify/localvalidate.go
