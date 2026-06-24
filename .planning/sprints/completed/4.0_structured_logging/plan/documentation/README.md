# Plan Documentation References

**Created:** June 16, 2026
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical Priority

1. **[CRITICAL] [Core Logging Package](core-logging-package.md)** — The shared `internal/log` package that becomes the single diagnostic sink for ATCR. Covers level parsing, format selection, and context helpers (`FromContext`/`NewContext`).

2. **[CRITICAL] [Error Classification System](error-classification-system.md)** — Error taxonomy with `ClassifiedError`, classification constructors (`NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`), and retryability checks. Wraps existing llmclient HTTP error patterns.

3. **[CRITICAL] [Secret and Path Redaction](secret-path-redaction.md)** — Redaction helpers for API keys, tokens, and absolute paths. Security-critical for preventing leakage in logs. Adapts existing `redactErrorSnippet` regexes and adds path-redaction relative to the review root.

### Important Priority

4. **[IMPORTANT] [Request Correlation](request-correlation.md)** — `WithReviewID` and `WithAgent` functions for attaching structured context to log lines. Enables tracing complete review runs and correlating agent activity.

5. **[IMPORTANT] [CLI and MCP Integration](cli-mcp-integration.md)** — Wiring the logger through cobra CLI flags (`LOG_LEVEL`, `--log-format`), command context, and MCP server injection. Covers `PersistentPreRunE` setup and `ExecuteContext` propagation.

### Reference Priority

6. **[REFERENCE] [Testing Patterns](testing-patterns.md)** — How to test log output deterministically without relying on `slog.Default()`. Covers logger injection in tests, discard logger patterns, and assertion strategies for 100% coverage targets.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/standard-library.md` — Go stdlib patterns (log/slog, context, io, strings/regexp, testing)
  - `.planning/specifications/packages/mcp-go-sdk.md` — MCP server creation and context propagation
  - `.planning/specifications/packages/cobra.md` — CLI flag patterns and command lifecycle
  - `.planning/specifications/packages/openai.md` — Error handling and retry patterns (reference only)
- **Codebase Discovery:** `.planning/plans/active/4.0_structured_logging/codebase-discovery.json`
- **Plan:** `.planning/plans/active/4.0_structured_logging/plan.md`
- **Original Requirements:** `.planning/plans/active/4.0_structured_logging/original-requirements.md`

---

## How to Use

1. Start with **Critical** documentation before coding — these cover the core APIs and security requirements
2. Review **Important** docs during development — these cover integration points and wiring
3. Consult **Reference** docs for specific questions — these cover testing strategies and edge cases

---

**Navigation:** [← Back to Plan](../README.md)
