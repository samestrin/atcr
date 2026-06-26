---
id: mem-2026-06-26-4d70ba
question: "What is the concrete fix for Docker runtime exit codes (125+, 128+N) being masked as app exit in the sandbox backend?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go, internal/sandbox/sandbox.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, docker, sandbox, exit-codes, signal-death]
retrievals: 0
status: active
type: clarifications
---

# What is the concrete fix for Docker runtime exit codes (125+

## Decision

Extend the existing 125-127 guard at docker.go:203 to `ec >= 125`, treating all 128+N signal-death codes as backend faults. The timeout path (DeadlineExceeded → 137) is already caught first at docker.go:189-195, so any 137 that reaches the ExitError branch is an OOM-kill or daemon kill. In this hardened sandbox (--cap-drop ALL, --user 65534:65534, --network none), PID 1 cannot receive SIGKILL from the workload itself; every 128+N exit originates from the Docker daemon or kernel. Fix: change `if ec == 125 || ec == 126 || ec == 127` to `if ec >= 125`, and split the error message — add `if ec >= 128 { return res, fmt.Errorf("docker run: container killed by signal %d (OOM or daemon kill, exit %d): %w", ec-128, ec, runErr) }` before the 125-127 branch. The Backend.Run contract (sandbox.go:92-94) states err is reserved for backend faults; signal-death codes (128+N) belong in the error return, not in RunResult.ExitCode.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
- internal/sandbox/sandbox.go
