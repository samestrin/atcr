---
id: mem-2026-07-19-3fd2c1
question: "Timeout vs cancellation disambiguation for opaque Backend.Run interfaces without a ctx-deadline backstop"
created: 2026-07-19
last_retrieved: ""
sprints: [32.0_sandbox_execution_environment]
files: [internal/verify/sandboxvalidate.go, internal/sandbox/docker.go, internal/verify/localvalidate.go]
tags: [clarifications, sprint-learning, 32.0_sandbox_execution_environment, architecture, timeout, context-cancellation]
retrievals: 0
status: active
type: clarifications
---

# Timeout vs cancellation disambiguation for opaque Backend.Ru

## Decision

A naive ctx-deadline backstop layered on top of an opaque Backend.Run interface is unsafe — it risks misrouting a genuine timeout into a StartError branch when the caller cannot independently tell whether an observed ctx.Err()!=nil caused the returned error or merely coincided with an unrelated spawn/backend fault. The correct fix location is inside each backend implementation's own timeout handling (where it has first-hand knowledge of what actually happened), not in a generic adapter/wrapper sitting above the interface. Concretely for atcr: fold context.Canceled into TimedOut alongside the existing DeadlineExceeded check, mirroring the host-exec path's belt-and-suspenders handling. internal/verify/localvalidate.go — host path folds both context.DeadlineExceeded and context.Canceled into TimedOut; internal/sandbox/docker.go — sandbox backend currently checks only DeadlineExceeded, not Canceled.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/sandboxvalidate.go
- internal/sandbox/docker.go
- internal/verify/localvalidate.go
