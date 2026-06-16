---
id: mem-2026-06-16-b8808b
question: "What is the correct scope for wiring tests in the scorecard diagnostics epic — CLI-level only, full cross-layer CLI+MCP, or deferred?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/scorecard/scorecard_test.go, cmd/atcr/reconcile.go, internal/mcp/handlers.go]
tags: [clarifications, epic-3.4_scorecard-diagnostics-writer, testing, scope, mcp, cli]
retrievals: 0
status: active
type: clarifications epic-3.4_scorecard-diagnostics-writer 2026-06-16
---

# What is the correct scope for wiring tests in the scorecard 

## Decision

Write only the CLI-level malformed-store→ErrOrStderr integration test now; defer the MCP cross-layer half. AC4 (Epic 3.4) resolved the MCP path by supplying os.Stderr explicitly at the handleReconcile call site — NOT by adapting e.log to an io.Writer — so any test gated on e.log routing has no contract to verify under this epic. The unit-level buffer-injection test (AC1) at scorecard_test.go:291-312 already covers the orphan-verdict warning. The missing wiring test is a narrow CLI integration test in cmd/atcr/ asserting that cmd.ErrOrStderr() is wired through on the reconcile path. The MCP cross-layer half must be deferred to a future epic that explicitly scopes e.log adaptation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/scorecard_test.go
- cmd/atcr/reconcile.go
- internal/mcp/handlers.go
