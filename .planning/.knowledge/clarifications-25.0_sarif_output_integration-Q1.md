---
id: mem-2026-07-14-4b6826
question: "Test-only fix pinning a pre-existing, delegated MCP error message is not a reward-hack (TestReportHandler_InvalidFormatRejected err!=nil branch)"
created: 2026-07-14
last_retrieved: ""
sprints: [25.0_sarif_output_integration]
files: [internal/mcp/handlers.go, internal/mcp/handlers_test.go, internal/report/render.go]
tags: [clarifications, sprint-25.0_sarif_output_integration, testing, reward-hack, diff-smell, mcp, go]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode)
---

# Test-only fix pinning a pre-existing, delegated MCP error me

## Decision

The test-only assertion is the correct resolution — no production-code change is needed. `handleReport` (internal/mcp/handlers.go:378-379) constructs its error message via `fmt.Errorf("invalid format: %s; must be one of: %s", format, report.Formats())`, a pre-existing "defense in depth" validation path (comment at handlers.go:367-369) that predates the change being reviewed. The `err != nil` branch of the test was simply never exercised before; pinning it with `assert.Contains(t, err.Error(), "xml")` is a legitimate regression test on already-correct, already-delegated code — not a reward-hack.

General pattern: when a handler delegates its error text to a shared function (here `report.Formats()`) rather than constructing/duplicating its own message, a test-only assertion that pins the propagated string is legitimate coverage, not a reward-hack — there is no production behavior left to change because the single source of truth already owns it. A deterministic diff-smell/reward-hack gate flagging "test-only change" should be read as a prompt to verify delegation, not an automatic signal that production code must change.

Justification:
- internal/mcp/handlers.go:378-379 — handleReport builds its own error text but delegates the enumerated-formats content to report.Formats(), it does not hardcode or duplicate the list.
- internal/mcp/handlers.go:367-369 — comment documents dual validation: JSON Schema enum (before dispatch) + defense-in-depth in-process check.
- internal/report/render.go:44-47 — Formats() is the single source of truth for the supported-format list.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go
- internal/mcp/handlers_test.go
- internal/report/render.go
