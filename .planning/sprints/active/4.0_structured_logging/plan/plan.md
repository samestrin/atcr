## Metadata
- **Plan Type:** tech-debt
- **Last Modified:** 2026-06-16
- **Original Requirements:** [original-requirements.md](original-requirements.md)

## Plan Overview
**Plan Goal:** Introduce a shared logging package (`internal/log`) and error classification system (`internal/errors`) to consolidate ATCR's diagnostic output, add request correlation IDs, and eliminate security risks from path/secret leakage.
**Target Users:** Operators debugging failing reviews, developers maintaining the codebase, CI/CD pipelines parsing structured logs
**Framework/Technology:** Go 1.25, log/slog (stdlib), cobra CLI

## Objectives
1. Establish `internal/log` as the single, shared diagnostic sink for the entire CLI, MCP server, fanout engine, payload runner, and LLM client.
2. Implement runtime level control via `LOG_LEVEL` and a `--log-format` CLI flag (text default, JSON optional).
3. Attach structured correlation context (`review_id`, `agent_name`) to every log line so a complete review run can be traced.
4. Build an error taxonomy in `internal/errors` that classifies failures as transient, permanent, user, or system errors and exposes retryability.
5. Classify HTTP errors returned by `internal/llmclient` and propagate classification through `errors.IsRetryable`.
6. Redact secrets (API keys, tokens) and render absolute repo paths relative to the review root in default log output.
7. Remove production use of `slog.Default()` and global stderr writes so tests can assert on injected log output deterministically.
8. Update sprint configuration and package documentation to reflect that `LOG_LEVEL` is implemented and error classification is available.

## Scope
### In Scope
- `internal/log` package with level parsing, format selection, redaction helpers, and correlation ID attachment.
- `internal/errors` package with error classification and retryability checks.
- `LOG_LEVEL` and `--log-format` support in the CLI root.
- Shared logger instance passed to MCP server, payload runner, and fanout engine.
- Review ID and agent name threaded through context and attached to every log line.
- Redaction of API keys, tokens, and absolute paths from log output.
- `internal/llmclient` wraps errors in `errors.ClassifiedError`.
- Unit tests for level parsing, format output, redaction rules, error classification, and correlation ID attachment.
- Update `.planning/.config/sprint-config.md` to remove the "optional" ambiguity once implemented.

### Out of Scope
- Metrics or telemetry (distinct concern; Epic 4.3).
- Log rotation or file-based log shipping.
- Audit trail (`audit.log.jsonl` in Epic 13.0) — that is an immutable run record, not operational logging.
- Rewriting every legacy `fmt.Fprintf` site in one pass; migration can be incremental per package.
- Circuit breaker logic (Epic 4.4) — this epic classifies errors; 4.4 acts on them.

## Dependencies and Context
- **Epic 1.7 (Real review run verification)**: established stdout/stderr ownership rules and the initial MCP logger pattern.
- **Epic 2.2 (Fanout hardening)**: retry and provider-error paths are high-value targets for structured logging and error classification.
- **Epic 4.3 (Metrics & Observability)**: builds on the logging foundation; metrics are a separate concern but share the correlation ID.
- **Epic 4.5 (Circuit Breaker / Provider Health)**: consumes `errors.ClassifiedError` to decide when a provider is failing.
- **Epic 13.0 (Team edition validation)**: owns the immutable `audit.log.jsonl`; this epic owns operational diagnostics. The two must stay separate.
- **Epic 5.0 (File path validation)**: path warnings and hallucination diagnostics should use the shared logger.
- **Epic 7.0 (Executor model fix generation)**: fix-generation debug output should flow through the shared logger.
- **TD item**: Surface a generic user-facing error line and keep path-bearing details at debug level only; this is satisfied by the redaction and level-control policy in `internal/log`.

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Generated `/create-tasks @.planning/plans/active/4.0_structured_logging/`
- **Estimated Count:** 15 tasks across 5 phases
- **Note:** The detailed task breakdown is generated as `tasks/*.md` and linked below.

| Phase | Task | File |
|-------|------|------|
| Phase 1: Core Logging Package | Task 01 — Core Logging API | [tasks/task-01-core-logging-api.md](tasks/task-01-core-logging-api.md) |
| Phase 1: Core Logging Package | Task 02 — Secret and Path Redaction Helpers | [tasks/task-02-secret-path-redaction.md](tasks/task-02-secret-path-redaction.md) |
| Phase 1: Core Logging Package | Task 03 — Request Correlation (`WithReviewID`, `WithAgent`) | [tasks/task-03-request-correlation.md](tasks/task-03-request-correlation.md) |
| Phase 1: Core Logging Package | Task 04 — Validate `internal/log` Test Coverage | [tasks/task-04-internal-log-tests.md](tasks/task-04-internal-log-tests.md) |
| Phase 2: Error Taxonomy | Task 05 — Error Classification System (`internal/errors`) | [tasks/task-05-error-classification.md](tasks/task-05-error-classification.md) |
| Phase 3: CLI Wiring | Task 06 — CLI Flags for `LOG_LEVEL` and `--log-format` | [tasks/task-06-cli-flags.md](tasks/task-06-cli-flags.md) |
| Phase 3: CLI Wiring | Task 07 — Root Logger Construction and Context Storage | [tasks/task-07-cli-logger-construction.md](tasks/task-07-cli-logger-construction.md) |
| Phase 3: CLI Wiring | Task 08 — MCP Server Logger Reuse | [tasks/task-08-mcp-logger.md](tasks/task-08-mcp-logger.md) |
| Phase 3: CLI Wiring | Task 14 — Review Command Correlation (`WithReviewID`) | [tasks/task-14-review-correlation.md](tasks/task-14-review-correlation.md) |
| Phase 3: CLI Wiring | Task 15 — Reconcile Command Logger Wiring | [tasks/task-15-reconcile-logger.md](tasks/task-15-reconcile-logger.md) |
| Phase 4: Engine Wiring | Task 09 — Remove Payload `slog.Default()` Fallback | [tasks/task-09-payload-stderr.md](tasks/task-09-payload-stderr.md) |
| Phase 4: Engine Wiring | Task 10 — Fanout Engine Logger Wiring | [tasks/task-10-fanout-logger.md](tasks/task-10-fanout-logger.md) |
| Phase 4: Engine Wiring | Task 11 — llmclient Error Classification Migration | [tasks/task-11-llmclient-migration.md](tasks/task-11-llmclient-migration.md) |
| Phase 5: Documentation | Task 12 — Documentation and Configuration Updates | [tasks/task-12-documentation.md](tasks/task-12-documentation.md) |
| Phase 5: Verification | Task 13 — End-to-End Integration Test | [tasks/task-13-integration-test.md](tasks/task-13-integration-test.md) |

## Technical Debt Analysis Summary
ATCR currently has no consistent logging strategy. Diagnostic output is emitted ad hoc across the CLI and engine, creating five concrete problems: (1) no level control despite documented LOG_LEVEL, (2) inconsistent sinks (MCP uses slog, CLI uses os.Stderr), (3) no error classification taxonomy, (4) no request correlation across agents, (5) security risks from path/secret leakage. Each component chooses its own output mechanism and error format, making it impossible to capture, filter, or test log output uniformly.

## Technical Planning Notes
- **Existing Pattern**: internal/mcp/handlers.go demonstrates nil-safe logger injection with discard fallback
- **Problematic Pattern**: internal/payload/diff.go falls back to slog.Default() (global state, AC4 violation)
- **Error Classification**: internal/llmclient/client.go already classifies HTTP errors (429/5xx = retryable) but doesn't expose this as a reusable type
- **Integration Points**: cmd/atcr/main.go (flag parsing), internal/mcp/server.go (logger injection), internal/fanout/engine.go (agent invocation), internal/payload/diff.go (remove fallback), internal/llmclient/client.go (wrap errors)
- **Security**: Redact API keys/tokens by matching known key names, render absolute paths relative to review root, log provider error bodies at debug level only

## Documentation References
See [documentation/README.md](documentation/README.md) for the full index. Key files by priority:

**[CRITICAL]**
- [Core Logging Package](documentation/core-logging-package.md) — `internal/log` API, level parsing, format selection, `FromContext`/`NewContext`
- [Error Classification System](documentation/error-classification-system.md) — `ClassifiedError`, `NewTransient`/`NewPermanent`/`NewUserError`/`NewSystemError`, `IsRetryable`
- [Secret and Path Redaction](documentation/secret-path-redaction.md) — sink-level redaction, `bearerTokenPattern`/`skKeyPattern`, `WithRoot`

**[IMPORTANT]**
- [Request Correlation](documentation/request-correlation.md) — `WithReviewID`, `WithAgent`, context threading through review and agent invocation
- [CLI and MCP Integration](documentation/cli-mcp-integration.md) — cobra `PersistentPreRunE`, `ExecuteContext`, `StdioTransport` stdout discipline

**[REFERENCE]**
- [Testing Patterns](documentation/testing-patterns.md) — discard logger injection, `t.TempDir` fixtures, coverage targets for `./internal/log/...` and `./internal/errors/...`

## Implementation Strategy
The implementation follows a five-phase approach:

1. **Core Logging Package** — Create `internal/log` with `New`, `LevelFromString`, format selection, redaction helpers, and correlation ID attachment (`WithReviewID`, `WithAgent`). Unit tests target 100% coverage of level parsing, redaction, sink wiring, and correlation.

2. **Error Taxonomy** — Create `internal/errors` with `ClassifiedError`, classification constructors (`NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`), and `IsRetryable`. Unit tests target 100% coverage of classification and retryability logic.

3. **CLI Wiring** — Add `LOG_LEVEL` and `--log-format` flags to `cmd/atcr/main.go:newRootCmd`, construct the root logger, and store it in the command context. Update `cmd/atcr/serve.go` to reuse the root logger and `cmd/atcr/review.go` to attach the review ID via `log.WithReviewID` after review ID resolution.

4. **Engine Wiring** — Update `internal/mcp/server.go` to use the passed logger consistently; remove the `slog.Default()` fallback in `internal/payload/diff.go`; call `log.WithAgent` in `internal/fanout/engine.go` before each agent invocation; migrate the highest-risk direct-stderr writes (fanout errors, provider retries). Update `internal/llmclient/client.go` to wrap HTTP errors in `errors.ClassifiedError`.

5. **Documentation** — Update CLI help and `docs/` to document `LOG_LEVEL` and `--log-format`; update `.planning/.config/sprint-config.md` to reflect that `LOG_LEVEL` is implemented; document error classification in `internal/errors/README.md`.

## Recommended Packages
No high-ROI packages identified. The implementation uses Go standard library only: log/slog (structured logging), io/os (I/O operations), strings/regexp (redaction), errors (error wrapping). Existing dependencies (cobra, testify, go-sdk) are sufficient.

## Success Criteria
- `LOG_LEVEL=debug` enables debug output; `LOG_LEVEL=error` suppresses info/warn output.
- `--log-format=json` emits newline-delimited JSON logs; default emits human-readable text.
- The MCP server reuses the root logger instead of constructing its own.
- `internal/payload/diff.go` no longer falls back to `slog.Default()` in production.
- A known API key value does not appear in log output at any level.
- Absolute repo paths in log output are rendered relative to the review root.
- Tests capture log output deterministically without relying on `slog.Default()`.
- `go test ./internal/log/...` passes with 100% coverage of level parsing, redaction, and sink wiring.
- Every log line emitted during a review includes the review ID (when available).
- Every log line emitted during an agent invocation includes the agent name.
- `internal/llmclient` wraps HTTP errors in `errors.ClassifiedError` with correct classification.
- `errors.IsRetryable(err)` returns true for transient errors, false for permanent/user/system errors.
- `go test ./internal/errors/...` passes with 100% coverage of classification and retryability logic.
- All production diagnostics flow through `internal/log`.
- No secrets or absolute paths leak in default-level logs.
- Operators can debug a failing review by setting `LOG_LEVEL=debug`.
- A reviewer can `grep` logs by review ID and see all agent activity for that run.

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| slog.Default() fallback removal breaks tests | Medium | Low | Update tests to inject slog.New(slog.NewTextHandler(io.Discard, nil)) |
| Redaction rules miss a new secret shape | Medium | High | Add CI check that scans test logs for API-key-shaped strings; review at code-review time |
| Performance cost of redaction | Low | Low | Redaction runs only on emitted records, not hot paths; benchmark if concerned |
| stdout/stderr ownership regressions in MCP mode | Low | High | MCP tests already verify protocol output; run them after wiring |
| Error classification breaks existing error-matching tests | Medium | Medium | Run full test suite after llmclient migration; update tests to use errors.Is |

## Next Steps
1. `/create-tasks @.planning/plans/active/4.0_structured_logging/`
2. `/design-sprint @.planning/plans/active/4.0_structured_logging/`
3. `/create-sprint @.planning/plans/active/4.0_structured_logging/`
4. `/execute-sprint`
