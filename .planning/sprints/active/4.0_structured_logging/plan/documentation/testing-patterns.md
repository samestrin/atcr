# Testing Patterns for Structured Logging `Priority: REFERENCE`

## Overview

This reference captures the testing patterns, acceptance criteria, and risk mitigations that govern the `internal/log` and `internal/errors` packages introduced by the structured-logging epic. It draws on the Go standard-library testing toolkit (`testing`, `net/http/httptest`), the test requirements recorded in the epic plan, and concrete pre-existing test patterns in the ATCR codebase that the new packages must remain compatible with.

The two hard coverage targets are: 100% on level parsing, redaction, and sink wiring in `internal/log` (AC8), and 100% on classification and retryability logic in `internal/errors` (AC13). A cross-cutting constraint (AC7) requires that tests capture log output deterministically without ever touching `slog.Default()`. Existing patterns — table-driven subtests, `httptest.NewServer` provider fakes, nil-safe logger injection with a discard fallback, and regression tests for secret redaction and `HTTPStatusError` classification — form the template the new tests must follow.

## Key Concepts

### Framework and Location

- Framework: `go test`, Go 1.25+.
- Test files live alongside source: `*_test.go` in the same directory.

> Source: [codebase-discovery.json:TEST_PATTERNS]

### Deterministic Log Capture (AC7)

Tests must not rely on `slog.Default()`. Each test constructs its own `slog.Logger` wired to a captured sink so assertions are hermetic.

> Source: [plan.md:Acceptance Criteria — AC7]

### Coverage Requirements

- `go test ./internal/log/...` — 100% coverage of level parsing, redaction, and sink wiring (AC8).
- `go test ./internal/errors/...` — 100% coverage of classification and retryability logic (AC13).

> Source: [plan.md:Acceptance Criteria — AC8, AC13]

### Nil-Safe Logger Injection

`internal/mcp/handlers.go` and `internal/mcp/server.go` fall back to `slog.New(slog.NewTextHandler(io.Discard, nil))` when no logger is provided. This pattern prevents panics and lets tests omit the logger field when constructing structs.

> Source: [codebase-discovery.json:EXISTING_PATTERNS — Logger Injection with Nil-Safe Fallback]

### Existing Regression Contracts to Preserve

- `TestComplete_HTTPStatusErrorSurfacedForClassification` (`internal/llmclient/client_test.go:437`) asserts that `HTTPStatusError` is surfaced via `errors.As`. The classification wrapper in `internal/errors` must keep this contract.
- `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` (`internal/llmclient/client_test.go:329`) is the existing regression test for secret redaction; log-redaction tests in `internal/log` should mirror these cases.

> Source: [codebase-discovery.json:SEMANTIC_MATCHES]

### Provider Fakes with `httptest`

`httptest.NewServer(handler)` stands in for an OpenAI-compatible provider; the registry under test points `base_url` at `server.URL`, so the real client code path (auth, retry, decode) is exercised with zero network. Table-driven subtests (`t.Run`) cover the range decision tree, stream parsing, and reconciler merge rules.

> Source: [standard-library.md:testing + net/http/httptest — provider mocks]

### Integration Risk — `gitRunner` Logger Wiring

After `slog.Default()` is removed, every `gitRunner` test must inject a discard logger. Some tests currently construct `gitRunner{...}` without a logger field and will need updating.

> Source: [codebase-discovery.json:INTEGRATION_GAPS — Test wiring for payload logger]

## Code Examples

### Nil-Safe Discard Logger Fallback

Verbatim pattern from `internal/mcp/handlers.go` / `internal/mcp/server.go`:

```go
slog.New(slog.NewTextHandler(io.Discard, nil))
```

> Source: [codebase-discovery.json:EXISTING_PATTERNS]

### Test Name Convention

Existing example: `TestEngine_LoggerNilSafe` at `internal/mcp/handlers_test.go`.

> Source: [codebase-discovery.json:TEST_PATTERNS — example_test]

## Quick Reference

| Topic | Value | Source |
|---|---|---|
| Test framework | `go test`, Go 1.25+ | codebase-discovery.json:TEST_PATTERNS |
| Test file location | Same directory as source (`*_test.go`) | codebase-discovery.json:TEST_PATTERNS |
| AC7 — deterministic capture | No `slog.Default()`; inject per-test logger | plan.md:Acceptance Criteria |
| AC8 — `internal/log` coverage | 100% on level parsing, redaction, sink wiring | plan.md:Acceptance Criteria |
| AC13 — `internal/errors` coverage | 100% on classification and retryability | plan.md:Acceptance Criteria |
| Nil-safe fallback | `slog.New(slog.NewTextHandler(io.Discard, nil))` | codebase-discovery.json:EXISTING_PATTERNS |
| Classification contract test | `TestComplete_HTTPStatusErrorSurfacedForClassification` (`client_test.go:437`) | codebase-discovery.json:SEMANTIC_MATCHES |
| Redaction contract test | `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` (`client_test.go:329`) | codebase-discovery.json:SEMANTIC_MATCHES |
| Provider fake pattern | `httptest.NewServer(handler)` + `server.URL` | standard-library.md |
| `gitRunner` migration risk | Must inject discard logger in all tests | codebase-discovery.json:INTEGRATION_GAPS |
| Error-classification migration risk | Update tests to use `errors.Is` | codebase-discovery.json:RISKS |

## Related Documentation

- [../plan.md](../plan.md) — Epic plan, acceptance criteria (AC7, AC8, AC13).
- [../codebase-discovery.json](../codebase-discovery.json) — Source of test patterns, semantic matches, integration gaps, and risks.
- [Core Logging Package](core-logging-package.md) — `internal/log` package design.
- [Secret and Path Redaction](secret-path-redaction.md) — Redaction rules the 100%-coverage test suite must exercise.
- [Error Classification System](error-classification-system.md) — Classification taxonomy the retryability tests must cover.
- `internal/llmclient/client_test.go` — Existing regression tests (`:329` redaction, `:437` classification) the new packages must not break.
- `internal/mcp/handlers_test.go:TestEngine_LoggerNilSafe` — Reference example of nil-safe logger test naming.
