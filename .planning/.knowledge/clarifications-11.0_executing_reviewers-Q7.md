---
id: mem-2026-06-26-6e8162
question: "Should Dispatcher.Execute be hardened to structurally enforce the exec-tool gating (threading allowed-tool-set/Exec flag into Execute)?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/tools/dispatch.go, internal/fanout/loop.go, internal/verify/executor.go]
tags: [clarifications, epic-11.0_executing_reviewers, security, tool-gating, dispatcher, deferred, 11.1]
retrievals: 0
status: active
type: clarifications
---

# Should Dispatcher.Execute be hardened to structurally enforc

## Decision

Defer to a new epic (11.1 dispatcher-structural-gating). The offering-layer gate is already structural: a non-exec Dispatcher never registers run_tests/run_script (dispatch.go:78-91), so Execute's lack of an allow-list check is unreachable by a non-exec agent — there is no live exploit. Threading the exec flag into Execute would touch ~15 call sites (fanout/loop.go:248, verify/executor.go:260, and test helpers) for no exploitable threat. Revisit if a shared-Dispatcher architecture is introduced.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tools/dispatch.go
- internal/fanout/loop.go
- internal/verify/executor.go
