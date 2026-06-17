---
id: mem-2026-06-16-38d6fb
question: "For MCP handler wiring tests, should the engine expose an injectable diag io.Writer field (Option B) or use os.Pipe to capture os.Stderr (Option A)?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/mcp/handlers.go, internal/mcp/handlers_test.go, internal/scorecard/scorecard_test.go]
tags: [clarifications, epic-3.6_scorecard-wiring-tests, testing, scope, injectable-writer, mcp]
retrievals: 0
status: active
type: clarifications
---

# For MCP handler wiring tests, should the engine expose an in

## Decision

Option B (injectable field) is correct. AC4 requires asserting a "non-default writer" and catching regressions — Option A cannot do either because nil Diag defaults back to os.Stderr inside the scorecard package, so removing the wiring is undetectable. The Risk mitigation "assert at the construction site" maps to adding engine.diag io.Writer (defaulting to os.Stderr) so tests inject a bytes.Buffer. This mirrors the scorecard package's own injection pattern (EmitOpts.Diag) and the existing handlers_test.go direct-engine construction style. The production change is ~3 lines and is narrower than the deferred e.log shim.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go
- internal/mcp/handlers_test.go
- internal/scorecard/scorecard_test.go
