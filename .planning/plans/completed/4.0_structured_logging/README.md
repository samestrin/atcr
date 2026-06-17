## Overview
Epic 4.0 introduces structured logging, error taxonomy, and request correlation to ATCR. The plan consolidates diagnostic output into a shared internal/log package, adds error classification via internal/errors, and threads correlation IDs (review ID, agent name) through all log lines. This eliminates security risks from path/secret leakage, enables operators to debug failing reviews via LOG_LEVEL=debug, and provides a foundation for future observability work (metrics, circuit breakers).

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/4.0_structured_logging/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/4.0_structured_logging/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/4.0_structured_logging/`
- [ ] **Execute Sprint** - `/execute-sprint`

## Timeline & Milestones
| Phase | Duration | Deliverables |
|-------|----------|--------------|
| Phase 1: Core Logging Package | 2 days | internal/log package (log.go, redact.go, correlation.go), unit tests |
| Phase 2: Error Taxonomy | 1 day | internal/errors package, llmclient integration, unit tests |
| Phase 3: CLI Wiring | 1 day | LOG_LEVEL and --log-format flags, root logger construction, context storage |
| Phase 4: Engine Wiring | 2 days | MCP server, payload, fanout, llmclient migration |
| Phase 5: Documentation | 1 day | CLI help, sprint-config update, errors README |
| **Total** | **7 days** | Full implementation with 100% test coverage for new packages |

## Resource Requirements
- **Personnel**: 1 backend developer
- **Tools**: Go 1.25+, standard library (log/slog, io, os, strings, regexp)
- **External Dependencies**: None (all stdlib)
- **Testing**: go test, testify for assertions

## Expected Outcomes
1. **Unified Logging**: All production diagnostics flow through internal/log with consistent level filtering and format selection
2. **Security**: No secrets or absolute paths leak in default-level logs; redaction happens at the sink level
3. **Debuggability**: Operators can set LOG_LEVEL=debug to see detailed diagnostic output for failing reviews
4. **Testability**: Tests can capture log output deterministically without relying on slog.Default() global state
5. **Error Classification**: Every error returned by internal/llmclient is classified (transient/permanent/user/system) with retryability metadata
6. **Correlation**: Every log line includes review ID (when available) and agent name (during agent invocation), enabling grep-based debugging

## Risk Summary
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| slog.Default() fallback removal breaks tests | Medium | Low | Update tests to inject discard logger |
| Redaction rules miss new secret shapes | Medium | High | Add CI check for API-key-shaped strings |
| Error classification breaks existing tests | Medium | Medium | Run full test suite, update to use errors.Is |
| MCP stdout/stderr ownership regression | Low | High | Run MCP tests after wiring |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/) (pending)
- [Sprint Design](sprint-design.md) — complexity analysis, phase structure, risk assessment
- [Documentation](documentation/) — organized reference docs by priority

## Documentation References
See [documentation/README.md](documentation/README.md) for the full index. Key files by priority:

**[CRITICAL]**
- [Core Logging Package](documentation/core-logging-package.md) — `internal/log` API, level parsing, context helpers
- [Error Classification System](documentation/error-classification-system.md) — `internal/errors` taxonomy, `ClassifiedError`, retryability
- [Secret and Path Redaction](documentation/secret-path-redaction.md) — redaction helpers for API keys, tokens, absolute paths

**[IMPORTANT]**
- [Request Correlation](documentation/request-correlation.md) — `WithReviewID`, `WithAgent`, threading context through fanout
- [CLI and MCP Integration](documentation/cli-mcp-integration.md) — `LOG_LEVEL`/`--log-format` flags, cobra `PersistentPreRunE`, MCP logger injection

**[REFERENCE]**
- [Testing Patterns](documentation/testing-patterns.md) — deterministic log capture, discard logger injection, coverage targets

## Acceptance Criteria
- [ ] AC1: LOG_LEVEL=debug enables debug output; LOG_LEVEL=error suppresses info/warn output
- [ ] AC2: --log-format=json emits newline-delimited JSON logs; default emits human-readable text
- [ ] AC3: The MCP server reuses the root logger instead of constructing its own
- [ ] AC4: internal/payload/diff.go no longer falls back to slog.Default() in production
- [ ] AC5: A known API key value does not appear in log output at any level
- [ ] AC6: Absolute repo paths in log output are rendered relative to the review root
- [ ] AC7: Tests capture log output deterministically without relying on slog.Default()
- [ ] AC8: go test ./internal/log/... passes with 100% coverage of level parsing, redaction, and sink wiring
- [ ] AC9: Every log line emitted during a review includes the review ID (when available)
- [ ] AC10: Every log line emitted during an agent invocation includes the agent name
- [ ] AC11: internal/llmclient wraps HTTP errors in errors.ClassifiedError with correct classification
- [ ] AC12: errors.IsRetryable(err) returns true for transient errors, false for permanent/user/system errors
- [ ] AC13: go test ./internal/errors/... passes with 100% coverage of classification and retryability logic

## Related Epics
- **Epic 1.7**: Established stdout/stderr ownership rules and initial MCP logger pattern
- **Epic 2.2**: Fanout hardening; retry and provider-error paths are high-value targets for this work
- **Epic 4.3**: Metrics & Observability; builds on the logging foundation
- **Epic 4.5**: Circuit Breaker / Provider Health; consumes errors.ClassifiedError
- **Epic 13.0**: Team edition validation; owns audit.log.jsonl (separate from operational logging)
