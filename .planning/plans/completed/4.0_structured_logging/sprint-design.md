# Sprint Design: Structured Logging, Error Taxonomy, and Request Correlation

**Created:** June 16, 2026
**Plan:** [Structured Logging, Error Taxonomy, and Request Correlation](.planning/plans/active/4.0_structured_logging/)
**Plan Type:** Tech Debt
**Status:** Design Complete

---

## Original User Request

> Epic 4.0: Structured Logging, Error Taxonomy, and Request Correlation
>
> ATCR currently has no consistent logging strategy. Diagnostic output is emitted ad hoc across the CLI and engine, creating five concrete problems: (1) no level control despite documented LOG_LEVEL, (2) inconsistent sinks (MCP uses slog, CLI uses os.Stderr), (3) no error classification taxonomy, (4) no request correlation across agents, (5) security risks from path/secret leakage. Introduce a small internal/log package that wraps log/slog and becomes the single way ATCR emits diagnostics. Add error classification and correlation IDs as first-class concepts.

**Referenced Resources:**
- [Core Logging Package](documentation/core-logging-package.md) — internal/log API, level parsing, context helpers
- [Error Classification System](documentation/error-classification-system.md) — internal/errors taxonomy, ClassifiedError, retryability
- [Secret and Path Redaction](documentation/secret-path-redaction.md) — redaction helpers for API keys, tokens, absolute paths
- [Request Correlation](documentation/request-correlation.md) — WithReviewID, WithAgent, threading context through fanout
- [CLI and MCP Integration](documentation/cli-mcp-integration.md) — LOG_LEVEL/--log-format flags, cobra PersistentPreRunE, MCP logger injection

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** structured-logging-error-taxonomy
**Complexity:** 7/12 (COMPLEX)
**Timeline:** 7 days
**Phases:** 5
**Pattern:** Foundation → Core Items → Advanced → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
structured logging slog context propagation
error classification retryable transient permanent
correlation ID review ID agent name
secret redaction bearer token sk- key
logger injection nil-safe discard fallback
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New patterns introduced (internal/log, internal/errors packages); logger injection via context is new for most packages; existing MCP pattern provides reference implementation
- **Integration:** 2/3 - 3+ integrations (cmd/atcr, internal/mcp, internal/fanout, internal/payload, internal/llmclient, internal/verify); logger must flow through context across package boundaries; MCP stdio discipline adds constraint
- **Story/Task & Test:** 2/3 - 15 tasks across 5 phases; 100% coverage targets on two new packages (internal/log, internal/errors); integration tests verify end-to-end correlation and redaction; existing test contracts must be preserved
- **Risk/Unknowns:** 1/3 - Well-understood patterns (slog is stdlib, error wrapping is standard Go); medium risks identified and mitigated (test breakage, redaction coverage); no significant unknowns

**Time Formula:** PHASE_COUNT × 1.4 days
**Calculation:** 5 × 1.4 = 7 days

---

## Recommended Flags

**Adversarial:** true (complexity 7/12 >= 6, phases 5 >= 3)
**Gated:** false (complexity 7/12 < 8, phases 5 >= 5 triggers gated)
**Recommendation strength:** false (complexity 7/12 < 10)
**Suggested command:** `/create-sprint @.planning/plans/active/4.0_structured_logging/ --gated`

---

## Phase Structure

### Phase 1: Foundation (2 days)

**Items:** Tasks 01-04
- Task 01: Create Core Logging API (internal/log/log.go)
- Task 02: Secret and Path Redaction Helpers (internal/log/redact.go)
- Task 03: Request Correlation (WithReviewID, WithAgent)
- Task 04: Validate internal/log Test Coverage

**Focus:** Establish internal/log as the single diagnostic sink. Implement level parsing (debug/info/warn/error), format selection (text/json), redaction helpers (bearer tokens, sk- keys, absolute paths), and correlation ID attachment (WithReviewID, WithAgent). Achieve 100% test coverage on level parsing, redaction, and sink wiring.

### Phase 2: Core Items (1 day)

**Items:** Task 05
- Task 05: Error Classification System (internal/errors)

**Focus:** Create internal/errors package with ClassifiedError type, classification constructors (NewTransient, NewPermanent, NewUserError, NewSystemError), and IsRetryable function. Preserve errors.As/Is reachability through Unwrap. Achieve 100% test coverage on classification and retryability logic.

### Phase 3: Advanced (1 day)

**Items:** Tasks 06-08, 14-15
- Task 06: CLI Flags for LOG_LEVEL and --log-format
- Task 07: Root Logger Construction and Context Storage
- Task 08: MCP Server Logger Reuse
- Task 14: Review Command Correlation (WithReviewID)
- Task 15: Reconcile Command Logger Wiring

**Focus:** Wire the logger through cobra CLI. Add LOG_LEVEL environment variable and --log-format flag. Construct root logger in PersistentPreRunE and store in command context. Update cmd/atcr/serve.go to reuse root logger. Update cmd/atcr/review.go to attach review ID after PrepareReview. Update cmd/atcr/reconcile.go to use context logger for diagnostics.

### Phase 4: Integration (2 days)

**Items:** Tasks 09-11
- Task 09: Remove Payload slog.Default() Fallback
- Task 10: Fanout Engine Logger Wiring
- Task 11: llmclient Error Classification Migration

**Focus:** Migrate engine packages to use injected logger. Remove slog.Default() fallback in internal/payload/diff.go. Add WithLogger option to fanout.Engine and call WithAgent before each agent invocation. Migrate internal/llmclient/client.go to wrap HTTP errors in ClassifiedError with correct classification (transient for 429/5xx, permanent for 4xx). Update all affected tests to inject discard logger.

### Phase 5: Validation (1 day)

**Items:** Tasks 12-13
- Task 12: Documentation and Configuration Updates
- Task 13: End-to-End Integration Test

**Focus:** Update CLI help text and user documentation to describe LOG_LEVEL and --log-format usage. Update .planning/.config/sprint-config.md to reflect LOG_LEVEL as implemented. Create internal/errors/README.md. Run full test suite with -race flag. Verify integration: LOG_LEVEL=debug enables debug output with review ID and agent name on every log line; --log-format=json produces valid JSON; no secrets or absolute paths leak in log output; MCP mode stdout is protocol-only.

---

## Work Decomposition

### Task Decomposition with Success Criteria

**Task 01: Core Logging API**
- Success: internal/log.New constructs logger with correct level and format; LevelFromString parses debug/info/warn/error (defaults to info); FromContext/NewContext propagate logger through context
- Testable: Unit tests for level parsing (table-driven), format selection (text vs JSON output), context propagation

**Task 02: Secret and Path Redaction**
- Success: Bearer tokens and sk- keys are redacted in log output; absolute paths are rendered relative to review root when root is configured
- Testable: Unit tests mirror existing TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens patterns; path redaction tests with various root configurations

**Task 03: Request Correlation**
- Success: WithReviewID attaches review_id attribute to every log line; WithAgent attaches agent_name attribute
- Testable: Unit tests capture log output and assert presence of review_id/agent_name attributes

**Task 04: internal/log Test Coverage**
- Success: go test ./internal/log/... passes with 100% coverage of level parsing, redaction, and sink wiring
- Testable: Coverage report shows 100% on internal/log package

**Task 05: Error Classification System**
- Success: ClassifiedError wraps errors with classification and Retryable flag; NewTransient/NewPermanent/NewUserError/NewSystemError constructors work correctly; IsRetryable returns true for transient, false for others; Unwrap preserves errors.As/Is reachability
- Testable: Unit tests for each constructor, IsRetryable logic, errors.As preservation

**Task 06: CLI Flags**
- Success: LOG_LEVEL environment variable is read; --log-format flag accepts text/json values; flags are defined in newRootCmd
- Testable: Manual verification with LOG_LEVEL=debug and --log-format=json

**Task 07: Root Logger Construction**
- Success: Root logger is constructed in PersistentPreRunE with configured level and format; logger is stored in command context via log.NewContext
- Testable: Unit tests verify logger is retrievable from cmd.Context()

**Task 08: MCP Server Logger Reuse**
- Success: cmd/atcr/serve.go passes root logger to mcp.Serve instead of constructing its own; internal/mcp/server.go uses injected logger consistently
- Testable: Unit tests verify logger injection; MCP tests verify stdout is protocol-only

**Task 09: Payload slog.Default() Removal**
- Success: internal/payload/diff.go no longer falls back to slog.Default(); gitRunner.log() returns discard logger when no logger is injected; BuildEntries and ChangedFileCount inject context logger
- Testable: Static check (grep -r 'slog.Default()' internal/); unit tests inject discard logger

**Task 10: Fanout Engine Logger Wiring**
- Success: Engine struct has *slog.Logger field and WithLogger option; invokeAgent calls log.WithAgent before each agent invocation; ExecuteReview passes context logger; invokeSkeptic passes context logger; direct fmt.Fprintf(os.Stderr, ...) calls migrated to logger
- Testable: Unit tests verify WithAgent attachment; integration tests verify agent_name in log output

**Task 11: llmclient Error Classification Migration**
- Success: Retryable status errors (429, 5xx) are wrapped in errors.NewTransient; non-retryable status errors (400, 401, 403, 404) are wrapped in errors.NewPermanent; errors.As reaches *HTTPStatusError through wrapper; existing tests pass unchanged
- Testable: Unit tests verify IsRetryable returns correct values; existing tests TestComplete_HTTPStatusErrorSurfacedForClassification and TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries pass

**Task 12: Documentation Updates**
- Success: atcr --help mentions LOG_LEVEL and --log-format; user-facing docs describe usage; .planning/.config/sprint-config.md reflects LOG_LEVEL as implemented; internal/errors/README.md documents classification API
- Testable: Manual verification of help text and documentation files

**Task 13: End-to-End Integration Test**
- Success: go test -race ./... passes; LOG_LEVEL=debug enables debug output with review ID and agent name; --log-format=json produces valid JSON; no API key value appears in log output; absolute paths are rendered relative; MCP mode stdout is protocol-only; errors.IsRetryable returns correct values for llmclient errors
- Testable: Integration tests in internal/integration/logging_test.go verify all success criteria

**Task 14: Review Command Correlation**
- Success: cmd/atcr/review.go:runReview retrieves root logger from context; after PrepareReview, logger carries review_id=<prep.ID>; correlated logger is stored in context for downstream calls; fanout.ExecuteReview, reconcile.RunReconcile, and verify.Verify receive correlated context
- Testable: Unit tests verify review_id attachment and context propagation

**Task 15: Reconcile Command Logger Wiring**
- Success: cmd/atcr/reconcile.go:runReconcile retrieves logger from command context; --require-verified warning is emitted via logger.Warn; scorecard diagnostics route through context logger; path-bearing details are at Debug level; user-facing summary remains on stdout
- Testable: Unit tests verify logger-based output; static check verifies no slog.Default or slog.New in reconcile.go

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Same directory as source (*_test.go in internal/log/, internal/errors/, internal/llmclient/, internal/fanout/, internal/payload/, internal/mcp/, internal/verify/, cmd/atcr/)

**Test File Placement Examples:**
- internal/log/log_test.go — level parsing, format selection, context helpers
- internal/log/redact_test.go — secret and path redaction
- internal/log/correlation_test.go — WithReviewID, WithAgent
- internal/errors/errors_test.go — classification, IsRetryable, errors.As preservation
- internal/llmclient/client_test.go — classification migration (modify existing)
- internal/fanout/engine_test.go — WithLogger option, WithAgent attachment (modify existing)
- internal/payload/diff_test.go — discard logger injection (modify existing)
- cmd/atcr/review_test.go — review_id attachment (modify existing)
- cmd/atcr/reconcile_test.go — context logger usage (modify existing)
- internal/integration/logging_test.go — end-to-end integration tests (create new)

**Unit/Integration/E2E:**
- Unit: Table-driven subtests for level parsing, format output, redaction rules, classification, retryability
- Integration: End-to-end review run with LOG_LEVEL=debug, --log-format=json, secret/path redaction verification
- E2E: Not applicable (no browser/UI)

**Test Environment Status:**
- Framework: go test (stdlib testing package + testify for assertions)
- Execution: go test ./... (unit), go test -race ./... (integration with race detector)
- Coverage Tools: go test -coverprofile=coverage.out ./... (coverage baseline: 80%)

---

## Architecture

**Primitives:**
- slog.Logger — structured logging primitive (stdlib)
- ClassifiedError — error classification primitive (internal/errors)
- context.Context — logger propagation primitive (stdlib)
- io.Writer — sink abstraction primitive (stdlib)

**Module Boundaries:**
- internal/log — Logger construction, level parsing, format selection, redaction, correlation; hides slog implementation details
- internal/errors — Error classification, retryability checks; hides taxonomy implementation
- cmd/atcr — CLI flag parsing, logger construction; hides cobra details from internal packages
- internal/mcp — MCP server with injected logger; hides protocol details from engine
- internal/fanout — Review execution with logger injection; hides agent orchestration details
- internal/llmclient — LLM API client with error classification; hides HTTP transport details

**External Dependencies:**
- log/slog (stdlib) — wrapped by internal/log
- context (stdlib) — used for logger propagation
- io (stdlib) — used for sink abstraction
- strings/regexp (stdlib) — used for redaction patterns
- cobra — CLI framework (wrapped by cmd/atcr)
- mcp-go-sdk — MCP server (wrapped by internal/mcp)
- testify — test assertions (used in tests only)

**Replaceability:**
- internal/log can be replaced with any structured logging library (zerolog, zap) by implementing the same interface
- internal/errors can be replaced with any error classification system by implementing ClassifiedError interface
- cmd/atcr can be replaced with any CLI framework by implementing the same command structure
- internal/mcp can be replaced with any MCP implementation by implementing the same server interface

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Secret redaction | API keys, bearer tokens, sk- keys in log output | Log file disclosure, CI log exposure, stderr capture | Enforce redaction at sink level; match known key patterns (bearerTokenPattern, skKeyPattern); log provider error bodies at debug level only |
| Path redaction | Absolute repo paths in log output | Information disclosure via log files | Render paths relative to review root; require root to be configured via WithRoot or context value |
| Provider error bodies | Full provider response bodies | Accidental secret leakage in error responses | Log provider error bodies at debug level only; redact known patterns before logging |
| MCP stdio discipline | stdout in serve mode | Protocol corruption, log leakage to MCP clients | Route all diagnostic output to stderr; stdout = transport only |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Logger construction | One-time at CLI startup | < 1ms | Construct once in PersistentPreRunE; pass through context |
| Redaction | Every emitted log record | < 0.1ms per record | Cache compiled regex patterns; run only on emitted records (not filtered by level) |
| Correlation ID attachment | Per-agent invocation | < 0.01ms per call | WithAgent called before each LLM call; slog.Logger.With is cheap |
| Context propagation | FromContext/NewContext called frequently | < 0.001ms per call | context.Value is O(1); no allocations |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Nil logger injection | Logger field is nil in struct | Return discard logger (slog.New(slog.NewTextHandler(io.Discard, nil))); no panic |
| Empty correlation IDs | WithReviewID(logger, "") or WithAgent(logger, "") | Attach empty string attribute; log output includes attribute with empty value |
| Invalid log levels | LevelFromString("invalid") | Default to info level; no error returned |
| Wrapped errors | ClassifiedError wrapping ClassifiedError | Unwrap returns inner error; errors.As/Is reach through all layers |
| Concurrent logger access | Multiple goroutines logging simultaneously | slog.Logger is thread-safe; redaction must not introduce race conditions |
| Path with special characters | Unicode, spaces, symlinks in paths | Path redaction handles all valid file paths; relativization works with symlinks |
| Nil error wrapping | NewTransient(nil), NewPermanent(nil) | Return nil; no ClassifiedError created |
| Context cancellation | Logger used after context cancelled | Logger continues to work; context cancellation does not affect logging |

### Defensive Measures Required

- **Input Validation:** LevelFromString validates against known levels (debug/info/warn/error); redaction patterns validated against existing test contracts (TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens)
- **Error Handling:** All error constructors handle nil input (NewTransient(nil) returns nil); logger accessors handle nil (return discard logger); errors.As/Is reachability preserved through Unwrap
- **Logging/Audit:** All diagnostic output flows through internal/log; no direct os.Stderr writes in production; redaction failures log warning and continue
- **Rate Limiting:** Not applicable (logging is not rate-limited; log levels control verbosity)
- **Graceful Degradation:** Nil-safe logger fallback to discard prevents panics; redaction failures log warning and continue; missing correlation IDs do not break logging

---

## Risks

**Technical:**
- slog.Default() removal breaks existing tests → Mitigation: Update tests to inject discard logger (slog.New(slog.NewTextHandler(io.Discard, nil))); search for gitRunner{ across test files to identify all sites
- Redaction rules miss new secret shapes → Mitigation: Add CI check that scans test logs for API-key-shaped strings; review at code-review time; mirror existing test patterns
- Performance cost of redaction → Mitigation: Redaction runs only on emitted records, not hot paths; benchmark if concerned; cache compiled regex patterns
- stdout/stderr ownership regressions in MCP mode → Mitigation: MCP tests already verify protocol output; run them after wiring; route all logs to stderr in serve mode
- Error classification breaks existing error-matching tests → Mitigation: Run full test suite after llmclient migration; update tests to use errors.Is; preserve errors.As reachability through Unwrap

**TDD-Specific:**
- 100% coverage targets are ambitious → Mitigation: Focus on level parsing, redaction, sink wiring, classification, retryability; use table-driven tests to cover edge cases
- Integration tests can be flaky → Mitigation: Use t.TempDir() for fixtures; use httptest.NewServer for provider mocks; use exec.Command with timeouts
- Existing test contracts must be preserved → Mitigation: Run TestComplete_HTTPStatusErrorSurfacedForClassification and TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens after migration; verify errors.As reachability
- Logger injection increases test boilerplate → Mitigation: Use nil-safe fallback to discard logger; tests can omit logger field when not testing logging behavior

---

**Next:** `/create-sprint @.planning/plans/active/4.0_structured_logging/`
