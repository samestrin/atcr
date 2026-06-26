---
id: mem-2026-06-26-d793d9
question: "What concrete change distinguishes Docker daemon/runtime exit codes (125+) from the workload's exit status in the sandbox Docker backend?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go, internal/sandbox/sandbox.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, docker, sandbox, exit-codes]
retrievals: 0
status: active
type: clarifications
---

# What concrete change distinguishes Docker daemon/runtime exi

## Decision

Two distinct locations are involved. The `cctx.Err()==context.DeadlineExceeded` note applies to `dockerCmd` (docker.go:232), which currently checks `!= nil`, conflating cancellation and deadline — a separate, smaller issue. The 125+ masking is in `Run` (docker.go:185–189): `errors.As(runErr, &ee)` succeeds and `ee.ExitCode()` is stored as the workload exit code unconditionally, even when the code is 125 (docker run itself failed), 126 (command not invocable), or 127 (command not found). The concrete fix: gate before treating the code as a workload result — if `ec == 125 || ec == 126 || ec == 127`, return a backend error (`fmt.Errorf("docker run: runtime error (exit %d): %w", ec, runErr)`) instead of populating `res.ExitCode` and returning `nil`. The existing non-ExitError spawn-failure path (docker.go:191–193) shows the intended pattern for backend faults. Docker's documented convention: exit 125 = docker run itself errored, 126 = command not invocable, 127 = command not found; these are backend faults, not workload results.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
- internal/sandbox/sandbox.go
