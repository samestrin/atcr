# Task 04: Validate `internal/log` Test Coverage (100% of Level Parsing, Redaction, Sink Wiring)

**Source:** Plan 4.0 – Debt Item #4 (AC7, AC8)
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement

`internal/log` ships with no guarantee that its test files cover the acceptance criteria. Tasks 01–03 each create focused test files (`log_test.go`, `redact_test.go`, `correlation_test.go`), but those tests must be verified to collectively cover level parsing, redaction, and sink wiring to 100% (AC8) without ever touching `slog.Default()` (AC7). This task is the coverage gate: it reviews the tests created in Tasks 01–03, fills any coverage gaps, and ensures the package-level test suite meets the hard targets.

## Solution Overview

Build on the tests created in Tasks 01–03 to reach deterministic, hermetic coverage of the `internal/log` package. Each test constructs its own `*slog.Logger` per test (via `log.New`), captures output into a `bytes.Buffer`, and asserts on structured content. Coverage is measured with `go test -cover ./internal/log/...`; the target is 100% on the three covered surfaces (level parsing, redaction, sink wiring). Context helpers and correlation functions are tested for contract correctness even when they fall outside the hard coverage metric.

## Technical Implementation
### Steps
1. **Review the tests created in Tasks 01–03.** Identify any missing branches in:
   - `log.go`: `New`, `LevelFromString`, `FromContext`, `NewContext`
   - `redact.go`: secret/token/path redaction paths
   - `correlation.go`: `WithReviewID`, `WithAgent`
2. **Fill coverage gaps** in the appropriate focused test file (`internal/log/log_test.go`, `internal/log/redact_test.go`, or `internal/log/correlation_test.go`) using table-driven subtests. Do not duplicate tests that already exist in Tasks 01–03; extend them.
3. **Ensure 100% coverage** of level parsing, redaction, and sink wiring lines by running `go test -cover ./internal/log/...` and iterating with `go tool cover -html=cover.out`.
4. **Verify no `slog.Default()` usage** in `internal/log/` tests — `grep -rn 'slog\.Default' internal/log/` must return no matches (AC7).

### Coverage Checklist
- **`TestNew_ValidLevels`** — table-driven subtests over `debug`, `info`, `warn`, `error`. For each level, construct a logger via `log.New(level, "text", &buf)`, emit a single record, and assert the output contains the expected level token (`msg="…"` preceded by `DEBUG`, `INFO`, `WARN`, or `ERROR`).
- **`TestNew_InvalidLevel`** — assert `log.New("trace", "text", io.Discard)` returns a non-nil error. Subtests: empty string, `"TRACE"`, `"fatal"`, `"information"`.
- **`TestLevelFromString`** — table-driven: `"debug"→slog.LevelDebug`, `"info"→slog.LevelInfo`, `"warn"→slog.LevelWarn`, `"error"→slog.LevelError`. Case-insensitivity subtest: `"DEBUG"`, `"Info"`, `"WARN"` resolve identically. Invalid input (`""`, `"verbose"`) returns an error.
- **`TestNew_TextFormat`** — construct `log.New("info", "text", &buf)`, emit `logger.Info("hello", "k", "v")`, assert output is a single human-readable line containing `level=INFO msg=hello k=v`.
- **`TestNew_JSONFormat`** — construct `log.New("info", "json", &buf)`, emit a record, parse the output line with `json.Unmarshal`, and assert the resulting map has `"level":"INFO"`, `"msg":"hello"`, `"k":"v"`.
- **`TestNew_MultilineJSON`** — emit two records; split `buf.String()` on `\n`, drop trailing empty line, assert exactly two parseable JSON objects.
- **`TestNew_Defaults`** — `log.New("", "", &buf)` must not error and must behave as `info`/`text` (verifies documented defaults).
- **`TestRedact_BearerToken`** — test via logger output: construct a logger whose handler/sink applies the redactor from Task 02, log a string containing `"Authorization: Bearer eyJhbG..."`, assert output contains `"Bearer [redacted]"` and does not contain the raw token. Mirror the `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` contract (`internal/llmclient/client_test.go:329`).
- **`TestRedact_SKKey`** — log a string containing `"sk-OTHER-leaked-99"`; assert output contains `"[redacted]"` and does not contain the raw key. Subtest for URL-encoded key variant mirroring `TestComplete_ErrorBodyRedactsURLEncodedKey` (`client_test.go:348`).
- **`TestRedact_AbsolutePath`** — configure the logger with the review root `/repo/root` (using the mechanism implemented in Task 02, e.g., a redacting handler or `WithRoot` helper), log `"/repo/root/internal/foo.go"`, assert output contains `"internal/foo.go"` and does not contain the absolute form. Subtest: path outside root is left untouched.
- **`TestRedact_NoOp`** — plain strings without secrets or paths pass through unchanged.
- **`TestWithReviewID`** — `log.WithReviewID(logger, "abc123")` returns a new logger; log a record, assert the output contains `review_id=abc123`. Original logger is unmodified (log a record through it, assert `review_id` absent).
- **`TestWithAgent`** — `log.WithAgent(logger, "primary")` returns a new logger; assert output contains `agent_name=primary`. Original logger unmodified.
- **`TestWithReviewID_ChainedWithAgent`** — chain both; assert output contains both `review_id=` and `agent_name=` attributes.
- **`TestFromContext_Missing`** — `log.FromContext(context.Background())` returns a non-nil logger; writing through it does not panic and produces no output on the test's captured sink (discard fallback per `internal/mcp/handlers.go:logger()`).
- **`TestNewContext_RoundTrip`** — store a logger via `log.NewContext(ctx, l)`, retrieve via `log.FromContext`, assert pointer equality and that emitted records flow to the original `bytes.Buffer`.
- **`TestSinkWriter`** — construct `log.New("info", "text", &buf)`, emit `logger.Error("boom")`, assert `buf.Len() > 0` and contents include `level=ERROR msg=boom`. Confirms the supplied `io.Writer` is the active sink.

## Files to Create/Modify
- `internal/log/log_test.go` – review/extend (created in Task 01)
- `internal/log/redact_test.go` – review/extend (created in Task 02)
- `internal/log/correlation_test.go` – review/extend (created in Task 03)
- `internal/log/export_test.go` – create (optional, only if redaction helpers are
  unexported and need direct invocation for 100% branch coverage)

## Documentation Links
- [Testing Patterns](../documentation/testing-patterns.md) — AC7 deterministic
  capture, nil-safe fallback, discard logger pattern.
- [Core Logging Package](../documentation/core-logging-package.md) — `log.New`,
  `LevelFromString`, `FromContext`, `NewContext` API.
- [Secret and Path Redaction](../documentation/secret-path-redaction.md) —
  bearer, `sk-`, absolute-path redaction rules.
- [Request Correlation](../documentation/request-correlation.md) — `WithReviewID`,
  `WithAgent` contracts.

## Related Files (from codebase-discovery.json)
- `internal/mcp/handlers_test.go:526` (`TestEngine_LoggerNilSafe`) — reference
  naming and nil-safe logger test pattern.
- `internal/mcp/handlers.go:logger()` — nil-safe discard logger accessor to
  replicate in `FromContext` fallback.
- `internal/llmclient/client_test.go:329`
  (`TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens`) — redaction contract
  test to mirror for bearer + sk- patterns.
- `internal/llmclient/client_test.go:348`
  (`TestComplete_ErrorBodyRedactsURLEncodedKey`) — URL-encoded redaction edge
  case to mirror.
- `internal/llmclient/client.go:342-355` (`redactErrorSnippet`) — existing
  redaction implementation whose regexes (`bearerTokenPattern`, `skKeyPattern`)
  the `internal/log` redactor reuses.

## Success Criteria
- [ ] `go test ./internal/log/...` passes with 100% coverage of level parsing,
      redaction, and sink wiring (`go test -cover ./internal/log/...`).
- [ ] Tests do not rely on `slog.Default()` — `grep -rn 'slog\.Default' internal/log/`
      returns no matches (AC7).
- [ ] All tests use table-driven subtests (`t.Run`) and per-test `bytes.Buffer`
      sinks for hermetic capture.
- [ ] Bearer token, `sk-` key, URL-encoded key, absolute path, and path-outside-root
      redaction cases are all covered.
- [ ] `WithReviewID` and `WithAgent` each produce the expected attribute; original
      logger is not mutated.
- [ ] `FromContext(context.Background())` returns a non-nil discard logger and
      does not panic.
- [ ] `NewContext`/`FromContext` round-trip preserves logger identity and sink.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- Level parsing: valid levels (debug, info, warn, error), invalid input (empty,
  "trace", "fatal"), case insensitivity (uppercase, mixed case).
- Format output: text format emits human-readable `level=… msg=… k=v` lines;
  json format emits newline-delimited JSON parseable by `encoding/json`.
- Redaction: bearer tokens → `"Bearer [redacted]"`, sk- keys → `"[redacted]"`,
  URL-encoded keys → `"[redacted]"`, absolute paths rendered relative to the
  configured root, paths outside the root untouched, plain strings unchanged.
- Correlation: `WithReviewID` attaches `review_id` attribute, `WithAgent`
  attaches `agent_name` attribute, chained calls carry both attributes, original
  logger is unmodified by either.
- Context helpers: `FromContext` returns discard logger when none set;
  `NewContext` stores and `FromContext` retrieves the same logger; emitted
  records flow to the original sink.
- Sink wiring: logger constructed with a `*bytes.Buffer` writes all emitted
  records to that buffer.

**Test Files:**
- `internal/log/log_test.go` (created/extended in Task 01, validated here)
- `internal/log/redact_test.go` (created/extended in Task 02, validated here)
- `internal/log/correlation_test.go` (created/extended in Task 03, validated here)
- `internal/log/export_test.go` (only if unexported redaction helpers require
  direct invocation for branch coverage)

## Risk Mitigation
- **Redaction API is unexported.** If `redact.go` exposes only internal helpers,
  create `export_test.go` with thin `var` aliases so tests can exercise them
  directly without promoting them to the public surface.
- **Redaction semantics drift from `llmclient.redactErrorSnippet`.** Mirror the
  exact regex names (`bearerTokenPattern`, `skKeyPattern`) and reuse the same
  compiled `*regexp.Regexp` values if they are exported; otherwise duplicate the
  patterns and add a comment cross-referencing `client.go:342`.
- **`slog.Handler` attribute key names are unspecified.** The test must assert
  against the attribute keys `internal/log` actually uses (`review_id`,
  `agent_name`). If the implementation chooses different keys, update the tests
  to match rather than changing the implementation.
- **Coverage target is scoped, not global.** AC8 requires 100% on level parsing,
  redaction, and sink wiring only. Context helpers and correlation are tested
  for contract correctness but are not held to the 100% bar. If a branch in
  those areas proves unreachable, document it with a `// unreachable:` comment
  rather than forcing artificial coverage.
- **Table-driven tests must not share a buffer.** Each subtest gets its own
  `bytes.Buffer`; sharing state across `t.Run` closures causes flaky ordering
  dependencies.

## Dependencies
- Task 01 (core-logging-api) — `log.New`, `LevelFromString`, `FromContext`,
  `NewContext` must be implemented before tests can target them.
- Task 02 (secret-path-redaction) — `redact.go` helpers and the redacting
  handler must be implemented before redaction tests can run.
- Task 03 (request-correlation) — `WithReviewID`, `WithAgent` must be
  implemented before correlation tests can run.

## Definition of Done
- [ ] All test files from Tasks 01–03 compile together with `go build ./internal/log/...`.
- [ ] `go test ./internal/log/...` passes with zero failures.
- [ ] `go test -cover ./internal/log/...` reports 100% coverage on level parsing,
      redaction, and sink wiring lines.
- [ ] No test in `internal/log/` calls `slog.Default()`.
- [ ] Redaction tests cover bearer, sk-, URL-encoded, absolute path, and no-op
      cases.
- [ ] Correlation tests confirm attribute attachment and logger immutability.
- [ ] Context tests confirm discard fallback and round-trip identity.
- [ ] Tests follow the table-driven subtest pattern used elsewhere in the
      codebase (`internal/mcp/handlers_test.go`, `internal/llmclient/client_test.go`).
- [ ] `export_test.go` exists only if required; no unnecessary shims.
- [ ] Code reviewed manually; no speculative or unused test helpers committed.
