# Task 13: End-to-End Integration Test

**Source:** Plan 4.0 – Debt Item #13
**Priority:** P1 | **Effort:** M | **Type:** Fix

## Problem Statement
The structured logging and error classification changes touch multiple packages (`internal/log`, `internal/errors`, `internal/llmclient`, `internal/fanout`, `internal/payload`, `internal/mcp`, `internal/verify`, `cmd/atcr`). Individual package tests verify correctness in isolation, but integration between packages — especially the context propagation chain from CLI → review → fanout → agent → llmclient — must be verified end-to-end.

## Solution Overview
Run the full test suite and verify:
1. All packages compile and test cleanly.
2. A review run with `LOG_LEVEL=debug` produces structured debug output with review ID and agent name on every log line.
3. A review run with `--log-format=json` produces valid JSON log lines.
4. A known API key value does not appear in log output at any level.
5. Absolute repo paths in log output are rendered relative to the review root.
6. `errors.IsRetryable` returns correct values when called on errors from the llmclient.
7. MCP mode produces correct protocol output on stdout with no log pollution.

## Technical Implementation
### Steps
1. Run `go test -race ./...` — all tests must pass.
2. Run `go vet ./...` — must be clean.
3. Run a manual integration test (or add an integration test):
   - Execute `LOG_LEVEL=debug atcr review <path>` against a test fixture.
   - Capture stderr output.
   - Verify: every log line includes `review_id=<id>`.
   - Verify: agent log lines include `agent_name=<name>`.
   - Verify: no API key value appears in output.
   - Verify: no absolute paths appear in output (when root is configured).
4. Run `LOG_LEVEL=debug --log-format=json atcr review <path>` and validate JSON structure.
5. Run `atcr serve` in MCP mode and verify stdout contains only protocol messages.
6. Verify `errors.IsRetryable` on a real llmclient error (e.g., from a mock 503 response).

## Files to Create/Modify
- `internal/integration/logging_test.go` — create (end-to-end integration tests)

## Documentation Links
- [Testing Patterns](../documentation/testing-patterns.md)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` — CLI entry point
- `cmd/atcr/review.go` — review command with `WithReviewID`
- `internal/fanout/engine.go` — fanout engine with `WithAgent`
- `internal/llmclient/client.go` — classified errors
- `internal/mcp/server.go` — MCP server with logger injection

## Success Criteria
- [ ] `go test -race ./...` passes with 0 failures
- [ ] `go vet ./...` is clean
- [ ] `LOG_LEVEL=debug` enables debug output in a review run
- [ ] `LOG_LEVEL=error` suppresses info/warn output in a review run
- [ ] `--log-format=json` produces valid JSON log lines
- [ ] Every log line during a review includes the review ID
- [ ] Agent log lines include the agent name
- [ ] No API key value appears in log output at any level
- [ ] Absolute paths are rendered relative to review root
- [ ] MCP mode stdout is protocol-only (no log pollution)
- [ ] `errors.IsRetryable` returns true for a 503 llmclient error
- [ ] `errors.IsRetryable` returns false for a 404 llmclient error
- [ ] `internal/payload/diff.go` no longer falls back to `slog.Default()`

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Integration Tests:**
- `TestIntegration_ReviewRun_DebugOutputHasCorrelation` — run a review with debug logging, assert review_id and agent_name appear in output
- `TestIntegration_ReviewRun_JSONFormat` — run with `--log-format=json`, parse each line as JSON, verify structure
- `TestIntegration_ReviewRun_NoSecretLeak` — configure a known API key, run a review, assert the key does not appear in output
- `TestIntegration_ReviewRun_NoAbsolutePathLeak` — configure a review root, run a review, assert absolute paths are relativized
- `TestIntegration_MCPMode_StdoutClean` — run in serve mode, assert stdout contains only MCP protocol messages
- `TestIntegration_LLMClient_ErrorClassification` — mock a 503 provider, call Complete, assert `errors.IsRetryable(err)` is true
- `TestIntegration_LLMClient_PermanentError` — mock a 404 provider, call Complete, assert `errors.IsRetryable(err)` is false

**Test Files:**
- `internal/integration/logging_test.go` (create)

## Risk Mitigation
- **Test isolation**: Integration tests use `t.TempDir()` for fixtures and `httptest.NewServer` for provider mocks. No external dependencies.
- **Flaky test risk**: Integration tests that depend on timing or process execution can be flaky. Use `exec.Command` with timeouts and explicit error messages.
- **Scope**: This task verifies the plan's success criteria. If any criterion fails, the root cause is in a specific task — trace back and fix there.

## Dependencies
- Tasks 01–12, 14, and 15 must be complete before this integration test can pass.

## Definition of Done
- [ ] Full test suite passes with `-race`
- [ ] `go vet` is clean
- [ ] Integration tests verify all plan success criteria
- [ ] Manual verification of `LOG_LEVEL=debug` review run completed
- [ ] Manual verification of `--log-format=json` review run completed
- [ ] Manual verification of MCP mode stdout discipline completed
- [ ] No `slog.Default()` in production code
- [ ] Plan success criteria are met
