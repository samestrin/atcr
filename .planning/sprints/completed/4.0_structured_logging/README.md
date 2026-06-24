# Sprint 4.0: Structured Logging, Error Taxonomy, and Request Correlation

**Sprint:** 4.0 — structured-logging-error-taxonomy
**Type:** Tech Debt 🔧
**Status:** Active
**Branch:** `feature/4.0_structured_logging`
**Timeline:** 7 days
**Complexity:** 7/12 (COMPLEX)
**Execution Mode:** Gated 🚧 | Adversarial Reviews: Enabled 🎯

---

## Overview

ATCR currently has no consistent logging strategy — diagnostic output is emitted ad hoc across the CLI and engine, creating five concrete problems: (1) no level control despite documented `LOG_LEVEL`, (2) inconsistent sinks (MCP uses slog, CLI uses `os.Stderr`), (3) no error classification taxonomy, (4) no request correlation across agents, (5) security risks from path/secret leakage.

This sprint introduces a small `internal/log` package wrapping `log/slog` as the single way ATCR emits diagnostics, adds `internal/errors` with a classification taxonomy, and wires both through the CLI, MCP server, and engine layer with correlation IDs (`review_id`, `agent_name`) on every log line.

---

## Timeline

| Phase | Focus | Duration |
|-------|-------|----------|
| Phase 1: Foundation | `internal/log` — core API, redaction, correlation | 2 days |
| Phase 2: Core Items | `internal/errors` — error classification taxonomy | 1 day |
| Phase 3: Advanced | CLI and MCP wiring (`LOG_LEVEL`, `--log-format`, context propagation) | 1 day |
| Phase 4: Integration | Engine wiring (payload, fanout, llmclient) | 2 days |
| Phase 5: Validation | End-to-end integration test, documentation | 1 day |

---

## Expected Outcomes

- `LOG_LEVEL=debug` enables debug output; `LOG_LEVEL=error` suppresses info/warn
- `--log-format=json` emits newline-delimited JSON; default emits readable text
- Every log line during a review includes `review_id=<id>` and `agent_name=<name>` for agent-level lines
- No API key, bearer token, or `sk-` key value appears in log output at any level
- Absolute repo paths rendered relative to review root
- MCP stdout is protocol-only — no log lines on stdout in serve mode
- `go test ./internal/log/...` and `./internal/errors/...` pass with 100% coverage
- All HTTP errors in `internal/llmclient` classified as Transient or Permanent

---

## Top Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `slog.Default()` removal breaks existing tests (payload, fanout tests construct `gitRunner{}` without logger field) | Medium | Search `grep -n 'gitRunner{' internal/payload/` across test files; update all sites to inject discard logger |
| Redaction rules miss new secret shapes or URL-encoded variants | Medium | Mirror existing `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` patterns; confirm bearer/sk- coverage in adversarial review 1.2.A |
| Error classification breaks `TestComplete_HTTPStatusErrorSurfacedForClassification` or `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` | Low | Run these tests as explicit DoD checkpoints after llmclient migration in 4.3; verify `errors.As` reachability through `ClassifiedError.Unwrap()` |

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Main sprint plan — all phases, tasks, adversarial reviews, and gates |
| [metadata.md](metadata.md) | Sprint tracking, schedule, status |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge entries created and referenced during this sprint |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth — original Epic 4.0 requirements (13 ACs) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, risk analysis |
| [plan/tasks/](plan/tasks/) | 15 individual task files |
| [plan/documentation/](plan/documentation/) | 6 documentation files (core-logging-package, error-classification-system, secret-path-redaction, request-correlation, cli-mcp-integration, testing-patterns) |

---

## Acceptance Criteria Summary

| AC | Description |
|----|-------------|
| AC1 | `LOG_LEVEL=debug` enables debug; `LOG_LEVEL=error` suppresses info/warn |
| AC2 | `--log-format=json` emits newline-delimited JSON |
| AC3 | MCP server reuses root logger — no local construction |
| AC4 | `internal/payload/diff.go` no longer falls back to `slog.Default()` |
| AC5 | Known API key values do not appear in log output at any level |
| AC6 | Absolute repo paths rendered relative to review root |
| AC7 | Tests capture log output without relying on `slog.Default()` |
| AC8 | `go test ./internal/log/...` passes with 100% coverage |
| AC9 | Every log line during a review includes the review ID |
| AC10 | Every log line during an agent invocation includes the agent name |
| AC11 | `internal/llmclient` wraps HTTP errors in `errors.ClassifiedError` |
| AC12 | `errors.IsRetryable` returns correct values (true for Transient, false for others) |
| AC13 | `go test ./internal/errors/...` passes with 100% coverage |
