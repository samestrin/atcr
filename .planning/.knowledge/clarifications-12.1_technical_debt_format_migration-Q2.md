---
id: mem-2026-06-27-ccab1f
question: "Does the registerExec / ExecutionTools() invariant (sandbox-reaching handlers registered only via registerExec) need a production-code guard, or is documenting ExecutionTools() as the authoritative registry sufficient?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/tools/exec_tools_test.go, internal/tools/dispatch.go]
tags: [clarifications, epic-12.1_technical_debt_format_migration, architecture, security boundary, execution tools, registerExec, ExecutionTools]
retrievals: 0
status: active
type: clarifications/epic-12.1
---

# Does the registerExec / ExecutionTools() invariant (sandbox-

## Decision

No production-code guard is needed — the enforcement is already structural and complete via three interlocking layers: (1) the Execute() gate at dispatch.go:225 checks execGated && !execEligible(ctx) and fails closed; (2) registerExec is the only unexported writer of execTools (dispatch.go:186-192), the field itself unexported (dispatch.go:73), so no external package can set it; (3) runInSandbox and execBackend are unexported (dispatch.go:80, dispatch.go:212), making it structurally impossible for a public-API-registered handler to reach the sandbox. The test TestEnableExecution_EveryExecToolIsGated (exec_tools_test.go:59-82) already verifies the invariant in all three directions. Documenting ExecutionTools() as the authoritative registry is sufficient because the structural enforcement is already in place. A test-only fix that asserts the invariant without anchoring assertions to the three existing production-side enforcement points is incomplete — the correct fix updates the test to reference dispatch.go:225, dispatch.go:186-192, and dispatch.go:80/212, with no production-code change required.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/tools/exec_tools_test.go
- internal/tools/dispatch.go
