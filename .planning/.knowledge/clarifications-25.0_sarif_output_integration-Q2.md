---
id: mem-2026-07-14-a7c639
question: "Test-only fix asserting an error message contains a new format name is not a reward-hack when the message is built from a shared Formats() call"
created: 2026-07-14
last_retrieved: ""
sprints: [25.0_sarif_output_integration]
files: [internal/mcp/handlers.go, internal/mcp/handlers_test.go, internal/report/render.go]
tags: [clarifications, sprint-25.0_sarif_output_integration, testing, reward-hack, diff-smell, mcp, go]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode)
---

# Test-only fix asserting an error message contains a new form

## Decision

The test-only assertion is correct and sufficient — no production-code change is needed. `handleReport` (internal/mcp/handlers.go:379) builds its error message by delegating to `report.Formats()`, which already includes `FormatSarif` at the single source of truth (internal/report/render.go:27, 44-47), so the new format name propagates automatically. Asserting `err.Error()` contains "sarif" at handlers_test.go:648 is a legitimate parity/regression test, not a reward-hack.

General pattern (companion to the Q1 entry): before treating a diff-smell/reward-hack gate's "test-only" flag as a mandate for a production-code change, check whether the tested behavior is owned by a shared/delegated function elsewhere in the codebase. If it is, the test-only fix is the right-sized resolution — the risk it guards against is a FUTURE regression where the handler stops delegating and starts duplicating the list.

Justification:
- internal/mcp/handlers.go:379 — fmt.Errorf("invalid format: %s; must be one of: %s", format, report.Formats()) delegates the enumerated list to report.Formats() rather than hardcoding it.
- internal/report/render.go:27,44-47 — FormatSarif = "sarif" and Formats() concatenates all four format constants; this is the single source of truth for the error-message wording.
- AC 01-04's Related Files note for handlers.go:370-420 explicitly says "reference only (no code change expected)" — confirming the plan itself anticipated MCP-layer parity without new production logic.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go
- internal/mcp/handlers_test.go
- internal/report/render.go
