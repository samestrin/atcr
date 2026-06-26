---
id: mem-2026-06-26-64c8d5
question: "Should the TD row citing exec_tools.go per-agent execution gating be deferred to Epic 11.1 or fixed with a minimal guard in 11.0?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/tools/exec_tools.go, internal/tools/dispatch.go]
tags: [clarifications, epic-11.0_executing_reviewers, scope, deferral, dispatcher, exec-gating, 11.1]
retrievals: 0
status: active
type: clarifications
---

# Should the TD row citing exec_tools.go per-agent execution g

## Decision

Defer to Epic 11.1 (dispatcher-structural-gating). exec_tools.go:69 is a plain argument struct (type runTestsArgs), not a gating point. The offering-layer gate is already fully structural: EnableExecution (exec_tools.go:60-66) only registers run_tests/run_script when explicitly called, so a non-exec Dispatcher never exposes those tools. A per-call guard would require threading agent exec-eligibility as a runtime parameter through Dispatcher/fanout — exactly the multi-file signature change deferred to 11.1. Epic 11.1 plan exists at .planning/epics/active/11.1_dispatcher-structural-gating.md.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tools/exec_tools.go
- internal/tools/dispatch.go
