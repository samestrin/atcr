---
id: mem-2026-06-27-404ac7
question: "When the diff-smell gate flags a test-only fix for registerExec/ExecutionTools() in ATCR, is strengthening the test assertion sufficient or is a production-code invariant required?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/tools/dispatch.go, internal/tools/exec_tools.go, internal/tools/exec_tools_test.go]
tags: [clarifications, epic-12.2_sprint_plan_scoping, architecture, testing, diff-smell-gate]
retrievals: 0
status: active
type: clarifications
---

# When the diff-smell gate flags a test-only fix for registerE

## Decision

A test-only assertion is architecturally sound given registerExec's current single call site (EnableExecution in internal/tools/exec_tools.go:142-143), but it will be re-flagged by the diff-smell gate on every future resolve-td run. The correct fix is a panic guard inside registerExec (internal/tools/dispatch.go:183) that iterates ExecutionTools() at init time and panics if the registered name is not declared there. This makes ExecutionTools() authoritative by construction. The existing test assertion at internal/tools/exec_tools_test.go:79 should be kept as belt-and-suspenders. Threat model note: an exec handler absent from ExecutionTools() cannot be invoked by agents (fanout.wireToolDefs only offers tools in ExecutionTools()), so this is a consistency/discoverability invariant, not a security gate — but the diff-smell gate treats both the same.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tools/dispatch.go
- internal/tools/exec_tools.go
- internal/tools/exec_tools_test.go
