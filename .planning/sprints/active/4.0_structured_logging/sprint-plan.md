# Sprint 4.0: Structured Logging, Error Taxonomy, and Request Correlation

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 4.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What Is Being Built

ATCR currently has no consistent logging strategy — diagnostic output is emitted ad hoc, `LOG_LEVEL` is documented but not implemented, errors have no classification taxonomy, and there is no way to correlate log lines across agents. This sprint creates `internal/log` as the single diagnostic sink, introduces `internal/errors` with a classification taxonomy, and wires both through the CLI, MCP server, and engine layer with correlation IDs (`review_id`, `agent_name`) on every log line.

### Why This Matters

Without a shared logging package, every new error site must be audited independently for secret leakage, operators cannot enable debug output for a failing review, and log lines from concurrent agents cannot be correlated. This sprint closes all five problems identified in Epic 4.0 and provides the foundation for Epic 4.3 (Metrics) and Epic 4.5 (Circuit Breaker).

### Key Deliverables

- `internal/log` package with level parsing, format selection (text/JSON), redaction helpers, and correlation ID attachment
- `internal/errors` package with `ClassifiedError`, four constructors, and `IsRetryable`
- CLI wiring: `LOG_LEVEL` env var and `--log-format` flag with `PersistentPreRunE` construction
- Engine wiring: shared logger threaded through MCP server, fanout engine, payload builder, and llmclient
- 100% test coverage on `internal/log` and `internal/errors`; integration test for end-to-end correlation

### Success Criteria

- `LOG_LEVEL=debug` enables debug output; `LOG_LEVEL=error` suppresses info/warn (AC1)
- `--log-format=json` emits newline-delimited JSON logs; default emits readable text (AC2)
- MCP server reuses root logger — no local construction (AC3)
- `internal/payload/diff.go` no longer falls back to `slog.Default()` (AC4)
- Known API key values do not appear in log output at any level (AC5)
- Absolute repo paths rendered relative to review root (AC6)
- Tests capture log output without relying on `slog.Default()` (AC7)
- `go test ./internal/log/...` passes with 100% coverage (AC8)
- Every log line during a review includes the review ID (AC9)
- Every log line during an agent invocation includes the agent name (AC10)
- `internal/llmclient` wraps HTTP errors in `errors.ClassifiedError` (AC11)
- `errors.IsRetryable` returns correct values (AC12)
- `go test ./internal/errors/...` passes with 100% coverage (AC13)

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Plan Type:** Tech Debt (Task-Based — `internal/log` and `internal/errors` use RED→GREEN coverage discipline; wiring tasks use inject-test-verify)
**Complexity:** 7/12 (COMPLEX)
**Default Mode:** Moderate 🔄 — write tests before or alongside implementation for new packages; update tests for migrated packages
**Adversarial Reviews:** Enabled 🎯 — CRITICAL/HIGH findings fix inline before proceeding; MEDIUM/LOW deferred to `tech-debt-captured.md`
**Gated Execution:** Enabled 🚧 — `/execute-sprint` stops at each Phase Gate for integration-level review before proceeding to the next phase

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | Original request — source of truth |
| [tasks/](plan/tasks/) | Individual task files with success criteria and DoD |
| [documentation/README.md](plan/documentation/README.md) | Referenced docs — read before coding |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/log/... -run TestName` |
| T2: Module | After completing a task | `go test ./internal/log/... ./internal/errors/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` or `go test -race ./...` |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` — all passing
2. Coverage: `go test -cover ./internal/log/... ./internal/errors/...` — ≥100% on target packages
3. Lint: `golangci-lint run` — no errors
4. Build: `go build ./...` — succeeds
5. Vet: `go vet ./...` — clean

### DoD Report Template
```
Phase-{N} DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

### Implementation Standards
- Black box interfaces — every module is a black box with a clean, documented API
- Replaceable components — any module should be rewritable from scratch using only its interface
- Single responsibility — one module, one clear purpose
- Primitive-first design — core primitives are `slog.Logger`, `ClassifiedError`, `context.Context`, `io.Writer`
- Go & MCP specific: panic safety, defer cleanup, interface segregation, robust protocol handling

### Coding Standards
- Package names: lowercase, single-word (`log`, `errors`)
- Exported: PascalCase (`New`, `ClassifiedError`, `WithReviewID`, `AttrReviewID`)
- Unexported: camelCase (`bearerTokenPattern`, `contextKey`, `skKeyPattern`)
- Imports: stdlib → third-party → internal (use `goimports`)
- Error handling: return `error` as last param, never ignore, wrap with `fmt.Errorf("...: %w", err)`
- Context: accept `context.Context` as first param in long-running or I/O functions
- Constants: PascalCase (`const MaxRetries = 3`), not UPPER_SNAKE_CASE

### Git Strategy
- Branch: `feature/4.0_structured_logging`
- Commit format: `type(scope): description` (e.g., `feat(log): add level parsing`)
- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`
- Small, atomic commits — one logical change per commit

---

## External Resources

**[CRITICAL] Read before starting implementation:**
- [Core Logging Package](plan/documentation/core-logging-package.md) — `internal/log` API, level parsing, context helpers
- [Error Classification System](plan/documentation/error-classification-system.md) — `ClassifiedError`, retryability checks
- [Secret and Path Redaction](plan/documentation/secret-path-redaction.md) — redaction helpers for API keys, tokens, absolute paths

**[IMPORTANT] Review during development:**
- [Request Correlation](plan/documentation/request-correlation.md) — `WithReviewID`, `WithAgent`, threading context through fanout
- [CLI and MCP Integration](plan/documentation/cli-mcp-integration.md) — `LOG_LEVEL`/`--log-format`, cobra `PersistentPreRunE`, MCP logger injection

**[REFERENCE] Consult as needed:**
- [Testing Patterns](plan/documentation/testing-patterns.md) — deterministic log capture, discard logger pattern, 100% coverage strategies

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation (2 days)

**Goal:** Establish `internal/log` as the single diagnostic sink with level parsing, format selection, redaction, and correlation ID support. All subsequent phases depend on this package being stable and fully tested.

---

### 1.1 [x] 🔧 **Create Core Logging API**
   **Task:** Create `internal/log/log.go` with `New`, `LevelFromString`, `FromContext`, `NewContext`. This is the foundation API used by every other task in the sprint.
   **Priority:** P1 | **Effort:** M
   1. Understand: Review [core-logging-package.md](plan/documentation/core-logging-package.md) and `internal/mcp/handlers.go` nil-safe logger pattern before writing any code
   2. Write tests first in `internal/log/log_test.go`: `TestLevelFromString_ValidLevels` (table-driven: debug/info/warn/error), `TestLevelFromString_EmptyDefaultsToInfo`, `TestLevelFromString_InvalidReturnsError`, `TestNew_TextFormat`, `TestNew_JSONFormat`, `TestNew_LevelFiltering`, `TestNew_InvalidLevelReturnsError`, `TestNew_InvalidFormatReturnsError`, `TestFromContext_EmptyContext`, `TestNewContext_FromContext_RoundTrip`, `TestFromContext_DiscardLoggerNoOutput`
   3. Implement `internal/log/log.go`: `New(level, format string, w io.Writer) (*slog.Logger, error)`, `LevelFromString(s string) (slog.Level, error)`, `FromContext(ctx context.Context) *slog.Logger` (discard fallback), `NewContext(ctx context.Context, logger *slog.Logger) context.Context`; use unexported `contextKey` struct type for context key
   4. Verify: `go test ./internal/log/... -cover` passes; `grep -n 'slog\.Default' internal/log/` returns no matches (T1)
   5. Commit: `git add internal/log/log.go internal/log/log_test.go && git commit -m "feat(log): add core logging API"`
   **Success Criteria:** `New` constructs logger with correct level and format; `LevelFromString` parses all four levels; `FromContext` returns non-nil discard logger when none set (no panic); `NewContext`/`FromContext` round-trip preserves logger identity (AC7, AC8)
   **Files:** `internal/log/log.go` (create), `internal/log/log_test.go` (create) | **Duration:** 0.5 days

### 1.1.A [x] **1.1 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/log/log.go`, `internal/log/log_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.1 internal/log core API`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/log.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/log_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Does the `contextKey` type risk collision with keys from other packages? Does any API path expose `slog.Default()`?
       - EDGE CASES: Nil `io.Writer`, empty level string, concurrent context access, invalid format values, `FromContext` on nil context?
       - ERROR HANDLING: Does `LevelFromString` return a typed error or generic string error? Does `New` validate the format parameter or silently default?
       - PERFORMANCE: Unnecessary allocations in `FromContext` (new discard logger on every miss)? Is the discard logger cached?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2 LOW, 0 CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | internal/log/log.go:New | `New` does not validate `w` for nil; a nil writer panics on first write rather than failing fast at construction | Add nil-writer guard returning an error (deferred → TD-001) |
   | LOW | internal/log/log.go:LevelFromString/New | Errors are generic `fmt.Errorf` strings, not typed sentinels; callers cannot branch via `errors.Is` | Define `ErrInvalidLevel`/`ErrInvalidFormat` sentinels (deferred → TD-002) |

   **Action Taken:** No CRITICAL/HIGH — proceed. 2 LOW deferred to `tech-debt-captured.md` (TD-001, TD-002).

---

### 1.2 [x] 🔧 **Secret and Path Redaction Helpers**
   **Task:** Create `internal/log/redact.go` with `Redactor`, `NewRedactor`, `Redact`. Scrubs bearer tokens, `sk-` keys, and absolute paths from log records at the sink level before emission.
   **Priority:** P1 | **Effort:** S
   1. Understand: Review [secret-path-redaction.md](plan/documentation/secret-path-redaction.md) and `internal/llmclient/client.go:342-355` (`redactErrorSnippet`, `bearerTokenPattern`, `skKeyPattern`) — reuse or mirror these compiled regexes
   2. Write tests in `internal/log/redact_test.go`: exact secret match, URL-encoded secret match, bearer token (any value), `sk-` pattern, foreign bearer+sk combo (`TestRedact_ForeignBearerAndSKTokens` mirrors `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens`), absolute path relativized, path outside root unchanged, path redaction no root (no-op), multiple passes compose, empty message, concurrent safety
   3. Implement `internal/log/redact.go`: package-level compiled `bearerTokenPattern`, `skKeyPattern`; `Redactor` struct (review root + secret list); `NewRedactor(reviewRoot string, secrets ...string) *Redactor`; `func (r *Redactor) Redact(msg string) string` applying all passes; ensure no mutable state in `Redact` (concurrent safe)
   4. Verify: `go test ./internal/log/... -race -cover`; `TestRedact_ConcurrentSafety` passes; bearer/sk-/path cases all covered
   5. Commit: `git add internal/log/redact.go internal/log/redact_test.go && git commit -m "feat(log): add secret and path redaction helpers"`
   **Success Criteria:** Bearer → `Bearer [redacted]`; `sk-` → `[redacted]`; absolute paths rendered relative when root configured; concurrent safe; redaction at sink level means no log record at any level bypasses it (AC5, AC6)
   **Files:** `internal/log/redact.go` (create), `internal/log/redact_test.go` (create) | **Duration:** 0.25 days

### 1.2.A [x] **1.2 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/log/redact.go`, `internal/log/redact_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2 redaction helpers`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/redact.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/redact_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/llmclient/client.go` (read lines 342-355 for regex comparison)
     - Checklist (pass verbatim):
       - SECURITY: Can a token bypass redaction via casing differences, extra whitespace, or URL encoding variants not covered? Can path redaction be bypassed with a trailing slash or symlink? Does the "no secrets configured" path still apply bearer/sk- regex redaction?
       - EDGE CASES: Empty string input, nil review root, message containing only the secret, multiple secrets in one message, partial path overlap (root is prefix of another path)?
       - ERROR HANDLING: If regex compilation panics at init, does it crash the process? Does path redaction handle Windows-style paths or UNC paths gracefully?
       - PERFORMANCE: Are regexes compiled once at package level (not per `Redact` call)? Does `strings.ReplaceAll` on a message with no secrets avoid allocations?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (1 HIGH fixed inline, 2 MEDIUM + 2 LOW deferred):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | internal/log/redact.go:skKeyPattern | `sk-\S+` was case-sensitive (no `(?i)`) unlike bearer; `SK-`/`Sk-` bypassed redaction | FIXED: added `(?i)` flag + regression test `TestRedact_SKKeyCaseInsensitive` |
   | MEDIUM | internal/log/redact.go:relativizePaths | replace used literal `/` while guard used `filepath.Separator` (Windows inconsistency) | FIXED: replace now uses `string(filepath.Separator)` (no-op on darwin/linux, removes smell) |
   | MEDIUM | internal/log/redact.go:bearerTokenPattern | `Bearer%20<token>` (URL-encoded space) not scrubbed | Deferred → TD-004 |
   | LOW | internal/log/redact.go:Redact secrets | exact-secret scrub is case-sensitive `strings.ReplaceAll` | Deferred → TD-005 |
   | LOW | internal/llmclient/client.go:339 | same `sk-` case-sensitivity gap in the mirrored llmclient regex | Deferred → TD-003 (align in Phase 4.3) |

   **Action Taken:** HIGH fixed inline before 1.3 (committed). MEDIUM separator fixed inline. Remaining MEDIUM/LOW deferred to `tech-debt-captured.md` (TD-003, TD-004, TD-005).

---

### 1.3 [x] 🔧 **Request Correlation — WithReviewID and WithAgent**
   **Task:** Create `internal/log/correlation.go` with `WithReviewID` and `WithAgent`. Both attach structured attributes to every log line via `slog.Logger.With`, enabling grep-based correlation across concurrent agents.
   **Priority:** P1 | **Effort:** S
   1. Understand: Review [request-correlation.md](plan/documentation/request-correlation.md). Both functions use `slog.Logger.With` — immutable, returns a new logger, cheap.
   2. Write tests in `internal/log/correlation_test.go`: `WithReviewID` attaches `review_id` attribute and preserves existing attributes; `WithAgent` attaches `agent_name`; chaining produces both; nil logger returns nil (no panic); empty string still attaches attribute; original logger immutability
   3. Implement `internal/log/correlation.go`: export `AttrReviewID = "review_id"` and `AttrAgentName = "agent_name"` constants; `WithReviewID(logger *slog.Logger, reviewID string) *slog.Logger` → `logger.With(AttrReviewID, reviewID)` (return nil if logger is nil); `WithAgent(logger *slog.Logger, agentName string) *slog.Logger` → same pattern
   4. Verify: `go test ./internal/log/...` passes; nil logger returns nil; chaining test shows both attributes in captured output
   5. Commit: `git add internal/log/correlation.go internal/log/correlation_test.go && git commit -m "feat(log): add WithReviewID and WithAgent correlation helpers"`
   **Success Criteria:** `WithReviewID` attaches `review_id`; `WithAgent` attaches `agent_name`; nil-safe; original logger immutable; both constants exported for use by downstream packages (AC9, AC10)
   **Files:** `internal/log/correlation.go` (create), `internal/log/correlation_test.go` (create) | **Duration:** 0.25 days

### 1.3.A [x] **1.3 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/log/correlation.go`, `internal/log/correlation_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.3 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.3 correlation helpers`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/correlation.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/correlation_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Can attribute injection override existing `review_id` or `agent_name` from a parent logger (e.g., can an agent spoof another agent's name)? Can empty-string IDs be confused with absent IDs downstream?
       - EDGE CASES: Nil logger — does downstream caller nil-check consistently? Chaining `WithReviewID` twice — does the second value override the first? Unicode in review IDs or agent names?
       - ERROR HANDLING: Returning nil on nil logger — are all call sites in this repo nil-safe, or will Phase 3 wiring expose a nil panic path?
       - PERFORMANCE: `slog.Logger.With` allocates a new logger on every call — is this acceptable given `WithAgent` is called once per agent invocation in the fanout loop?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (1 HIGH assessed+mitigated, 1 MEDIUM rejected, 2 LOW handled):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | HIGH→assessed | correlation.go:WithReviewID/WithAgent | `slog.With` appends not replaces; double-wrap emits duplicate keys ("spoofing") | Reframed: no external spoofing vector (agents are LLM subprocesses, not Go callers); intended wiring never double-wraps. MITIGATED inline: documented call-once contract + regression test `TestCorrelation_DoubleWrapAppends`. Residual dedup-enforcement deferred → TD-006 |
   | MEDIUM | correlation.go | empty-string ID attaches present-but-empty attr (ambiguous vs absent) | REJECTED: plan task 1.3 explicitly requires "empty string still attaches attribute"; behavior is intentional and tested |
   | LOW | correlation.go nil-return | nil propagates to `.Info()` panic if caller passes nil | Accepted as spec: contract is "nil logger returns nil"; production callers use `FromContext` (never nil). No change |
   | LOW | correlation_test.go | `recs[0]` indexed without length guard | FIXED: `decodeLines` now fails cleanly when no lines |

   **Action Taken:** No genuine CRITICAL/HIGH (HIGH reframed as robustness, mitigated inline). Test-robustness LOW fixed. Residual deferred → `tech-debt-captured.md` (TD-006).

---

### 1.4 [x] 🔧 **Validate internal/log Test Coverage (100%)**
   **Task:** Verify tests from 1.1–1.3 collectively achieve 100% coverage on level parsing, redaction, and sink wiring. Fill any coverage gaps. Confirm no `slog.Default()` usage in test files.
   **Priority:** P1 | **Effort:** M
   1. Run `go test -coverprofile=coverage.out ./internal/log/... && go tool cover -func=coverage.out` — identify uncovered lines
   2. Add missing table-driven subtests to the appropriate focused test file: invalid format in `New`, case-insensitive level parsing, discard fallback sink verification, path-outside-root no-op, URL-encoded secret, multi-pass redaction composition
   3. Create `internal/log/export_test.go` only if unexported redaction helpers need direct invocation for branch coverage — keep it minimal
   4. Verify: `grep -rn 'slog\.Default' internal/log/` returns no matches (AC7); 100% coverage confirmed on target surfaces; all table-driven tests use per-subtest `bytes.Buffer` sinks (no shared state)
   5. Commit: `git add internal/log/*_test.go && git commit -m "test(log): achieve 100% coverage on level parsing, redaction, sink wiring"`
   **Success Criteria:** 100% on level parsing, redaction, and sink wiring (AC8); no `slog.Default()` anywhere in `internal/log/` (AC7)
   **Files:** `internal/log/log_test.go`, `internal/log/redact_test.go`, `internal/log/correlation_test.go` (extend as needed); `internal/log/export_test.go` (only if required) | **Duration:** 0.5 days

---

### 1.5 [x] Phase 1 — Definition of Done

Run the full DoD verification before proceeding to Phase 2:

- [x] `go test ./internal/log/...` — all passing
- [x] `go test -cover ./internal/log/...` — 100% coverage on level parsing, redaction, sink wiring (AC8)
- [x] `go test -race ./internal/log/...` — no data races
- [x] `go vet ./internal/log/...` — clean
- [x] `go build ./...` — succeeds (internal/log has no external dependencies)
- [x] `grep -rn 'slog\.Default' internal/log/` — returns no matches (AC7)
- [x] `internal/log/log.go`, `redact.go`, `correlation.go` all created
- [x] All adversarial reviews completed: 1.1.A, 1.2.A, 1.3.A

```
Phase-1 DoD Complete
Auto: 5/5 | Task-Specific: 8/8
Manual Review: [x] Code reviewed (3 adversarial subagent reviews + Phase 1 gate)
```

### 1.6 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of Phase 1 implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/log.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/redact.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/correlation.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/log_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/redact_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/correlation_test.go`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are `New`, `LevelFromString`, `FromContext`, `NewContext`, `WithReviewID`, `WithAgent`, `AttrReviewID`, `AttrAgentName` signatures stable and ready for downstream Phase 3 and Phase 4 consumers?
       - CONFIG SURFACE: No runtime config surface — is the intended `LOG_LEVEL` env var integration point clear for Phase 3 implementers from the package API?
       - INTEGRATION: Will `cmd/atcr` (Phase 3) be able to import `internal/log` without circular deps? Will `internal/fanout` (Phase 4) be able to call `log.WithAgent` as intended?
       - PHASE-EXIT CONTRACT: Can Phase 2 (`internal/errors`) be implemented with zero changes to `internal/log`?
       - REGRESSION: Does adding the `internal/log` package break any existing `go build ./...` or `go test ./...`?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (0 CRITICAL/HIGH — gate passed; 2 LOW, one root issue):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | internal/log/log.go:New | `Redact` is a standalone helper not wired into the slog sink; `New` does not enforce redaction, so a caller can log a secret directly via slog and bypass the `Redactor` | Deferred → TD-007. Cross-phase design decision: AC5/AC6 (Phase 5 integration test) require either a redacting `slog.Handler` wrapper or caller-side redaction at all log sites. Flagged at phase stop. |
   | LOW | internal/log/log.go:New | `New` signature has no `*Redactor` param/option; if Phase 3 chooses sink-enforced redaction, `New` needs a (non-breaking, variadic) option | Folded into TD-007 — decide redaction enforcement model before Phase 4 engine wiring |

   **Verified clean:** all 8 exported signatures stable; `internal/log` has zero internal-package deps (no cycle risk for cmd/atcr or fanout); `LevelFromString` is the clear `LOG_LEVEL` integration point; Phase 2 (`internal/errors`) needs zero changes here; `go build ./...` + `go test ./...` pass, vet clean.

   **Action Taken:** Phase gate PASSED (no CRITICAL/HIGH). 2 LOW (one root issue) deferred → `tech-debt-captured.md` (TD-007), surfaced at the gated phase stop because it shapes Phase 3/4 wiring.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Core Items (1 day)

**Goal:** Introduce the `internal/errors` error classification taxonomy. Standalone package with no dependencies on other internal packages — low risk, high foundation value.

**Dependency:** Phase 1 complete (internal/log stable).

---

### 2.1 [x] 🔧 **Error Classification System (internal/errors)**
   **Task:** Create `internal/errors/errors.go` with `ClassifiedError`, four constructors (`NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`), and `IsRetryable`. Enables the llmclient migration in Phase 4.
   **Priority:** P1 | **Effort:** S
   1. Understand: Review [error-classification-system.md](plan/documentation/error-classification-system.md) and `internal/llmclient/client.go:37-45` (`retryableStatus` map) to understand the existing informal classification that this formalizes
   2. Write tests in `internal/errors/errors_test.go`: each constructor sets correct `Classification` and `Retryable`; nil input returns nil; `Error()` delegates; `Unwrap()` returns inner error; `IsRetryable` returns true for Transient only; `IsRetryable` returns false for non-`ClassifiedError` errors; `errors.As` reaches through wrapper to a custom type; `errors.Is` reaches through wrapper to a sentinel; double-wrapped `ClassifiedError` (outer classification wins)
   3. Implement `internal/errors/errors.go`: `Classification` type with constants `Transient`, `Permanent`, `UserError`, `SystemError`; `ClassifiedError` struct with `Err`, `Classification`, `Retryable` fields; `Error()`, `Unwrap()` methods; four constructors with nil-safety (return nil for nil input); `IsRetryable(err error) bool` using `errors.As`
   4. Verify: `go test ./internal/errors/... -cover`; `errors.As` and `errors.Is` reachability confirmed; nil-input constructors return nil
   5. Commit: `git add internal/errors/ && git commit -m "feat(errors): add error classification taxonomy"`
   **Success Criteria:** All four constructors correct; `IsRetryable` returns true for Transient only; `errors.As`/`errors.Is` reach through wrapper; nil-safe constructors; zero dependencies on other internal packages (AC11, AC12, AC13)
   **Files:** `internal/errors/errors.go` (create), `internal/errors/errors_test.go` (create) | **Duration:** 0.75 days

### 2.1.A [x] **2.1 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/errors/errors.go`, `internal/errors/errors_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.1 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.1 error classification system`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Can a `ClassifiedError` wrapper be used to forge retryability — e.g., wrapping a permanent 401 error in `NewTransient` — causing indefinite retries and potential API key lockout? Is `IsRetryable` checking the outermost or innermost wrapper on double-wrapping?
       - EDGE CASES: `NewTransient(nil)` — does it return nil or a non-nil interface wrapping nil concrete value (the classic Go nil interface trap)? `ClassifiedError.Unwrap()` when `Err` is nil? `errors.As` on a chain with 10+ levels of wrapping?
       - ERROR HANDLING: Does `Error()` panic if `Err` is nil (possible if nil-safety in constructors is bypassed)? Does `IsRetryable` correctly return false for a plain `errors.New("...")` error (no classification)?
       - PERFORMANCE: `errors.As` walks the error chain — acceptable for the error frequency in llmclient (not a hot path)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (1 HIGH reframed+mitigated, 1 MEDIUM fixed inline, 1 LOW handled):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | HIGH→reframed | errors.go:IsRetryable | `errors.As` finds the outermost `*ClassifiedError`; `NewTransient(NewPermanent(err))` would forge retryability on a permanent failure | REFRAMED as robustness: plan task 2.1 explicitly mandates "outer classification wins"; no double-wrap path exists in intended wiring (each error classified once; Phase 4 maps each HTTP status to one constructor); re-wrap is a Go programming error, not external input. MITIGATED inline: documented single-classification/outermost-wins contract on `ClassifiedError` + `IsRetryable`. Residual hardening deferred → TD-008 |
   | MEDIUM | errors.go:Error() | `Error()` dereferenced `e.Err` with no nil guard; a directly-constructed `&ClassifiedError{}` (exported fields) panics | FIXED inline: `Error()` falls back to the classification label when `Err` is nil; regression test `TestError_NilErrDoesNotPanic` added |
   | LOW | errors.go:Unwrap() | `Unwrap()` returns `e.Err` unguarded | Accepted as-is (nil simply terminates the chain); documented nil-tolerance; covered by `TestError_NilErrDoesNotPanic` |

   **Action Taken:** No genuine CRITICAL/HIGH (HIGH reframed as robustness vs. an explicit, tested spec decision; mitigated inline). MEDIUM nil-panic fixed inline before 2.2 (committed). Residual deferred → `tech-debt-captured.md` (TD-008). Proceeding to Phase 2 DoD.

---

### 2.2 [x] Phase 2 — Definition of Done

- [x] `go test ./internal/errors/...` — all passing
- [x] `go test -cover ./internal/errors/...` — 100% on classification and retryability logic (AC13)
- [x] `go vet ./internal/errors/...` — clean
- [x] `internal/errors/errors.go` has zero imports from other `internal/` packages
- [x] `NewTransient(nil)` returns nil (verified by test — not a non-nil interface)
- [x] `errors.As` and `errors.Is` reachability through `ClassifiedError.Unwrap()` verified
- [x] Adversarial review 2.1.A completed

```
Phase-2 DoD Complete
Auto: 5/5 | Task-Specific: 6/6
Manual Review: [x] Code reviewed (adversarial 2.1.A + Phase 2 gate)
```

### 2.3 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of Phase 2 implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors_test.go`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are `NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`, `IsRetryable` signatures stable and ready for Phase 4 (llmclient migration)? Are classification string values (`"transient"`, etc.) finalized?
       - CONFIG SURFACE: No runtime config surface — is the nil-safety guarantee (constructors return nil for nil input) documented clearly for Phase 4 implementers?
       - INTEGRATION: Can `internal/llmclient` import `internal/errors` without circular deps? Can `internal/fanout` use `errors.IsRetryable` for retry decisions in the future?
       - PHASE-EXIT CONTRACT: Does `ClassifiedError.Unwrap()` correctly preserve the `*HTTPStatusError` chain that `TestComplete_HTTPStatusErrorSurfacedForClassification` (in llmclient) depends on? (The test doesn't run yet — verify the design contract.)
       - REGRESSION: Does `go test ./...` (excluding new packages) still pass cleanly?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (0 CRITICAL/HIGH — gate passed; 2 LOW):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | internal/errors/errors.go:package doc | nil-safety contract documented per-constructor but not in the package header (the Phase 4 integration touchpoint) | FIXED inline: package doc now states all `New*` constructors return a true nil interface for nil input |
   | LOW | internal/errors/errors.go:IsRetryable | outermost-wins is an honor-system contract with no runtime guard against double-wrap re-classification | Duplicate of TD-008 (already captured at 2.1.A); doc warning kept prominent. No new entry |

   **Verified clean:** all five exported signatures (`NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`, `IsRetryable`) stable; classification string values finalized (`"transient"`/`"permanent"`/`"user_error"`/`"system_error"`); `errors` imports stdlib only → no circular dep with llmclient/fanout; `errors.As(NewTransient(httpStatusErr), &se)` reaches `*HTTPStatusError` through `ClassifiedError.Unwrap()` (Phase 4 contract holds); `go build ./...` + full `go test ./...` pass, vet clean.

   **Action Taken:** Phase gate PASSED (no CRITICAL/HIGH). 1 LOW fixed inline (committed); 1 LOW is a duplicate of TD-008. Proceeding to phase stop.
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Advanced — CLI and MCP Wiring (1 day)

**Goal:** Wire the shared logger through cobra flags and command context. All subcommands receive the logger via `log.FromContext`. MCP server and reconcile command stop writing diagnostics outside the shared logger.

**Dependencies:** Phase 1 (`internal/log`) complete.

---

### 3.1 [ ] 🔧 **CLI Flags — LOG_LEVEL and --log-format**
   **Task:** Add `--log-format` persistent flag and read `LOG_LEVEL` env var in `cmd/atcr/main.go:newRootCmd`. These values are consumed by `PersistentPreRunE` in task 3.2.
   **Priority:** P1 | **Effort:** S
   1. Understand: Review [cli-mcp-integration.md](plan/documentation/cli-mcp-integration.md) and current `cmd/atcr/main.go:newRootCmd` structure before any changes
   2. Write tests in `cmd/atcr/main_test.go`: `TestRootCmd_LogFormatDefault` (default is `"text"`), `TestRootCmd_LogLevelFromEnv` (`LOG_LEVEL=debug` is read), `TestRootCmd_LogLevelEnvEmptyDefaultsToInfo` (unset → `"info"`)
   3. Add `--log-format` as a persistent string flag (default: `"text"`) to root command; read `os.Getenv("LOG_LEVEL")` with `"info"` default; store both for `PersistentPreRunE` consumption
   4. Verify: `go build ./cmd/atcr/...` succeeds; existing tests pass; no behavioral change yet (logger construction is 3.2)
   5. No separate commit — batched with 3.2 and 3.3

   **Success Criteria:** `--log-format` persistent flag declared; `LOG_LEVEL` env var read with `"info"` default; both available to `PersistentPreRunE`; all subcommands inherit `--log-format` (AC1, AC2)
   **Files:** `cmd/atcr/main.go` (modify), `cmd/atcr/main_test.go` (create/modify) | **Duration:** 0.2 days

### 3.2 [ ] 🔧 **Root Logger Construction and Context Storage**
   **Task:** Add `PersistentPreRunE` to root command that constructs `*slog.Logger` from flags and stores it in command context. Single construction point — no subcommand constructs its own logger after this.
   **Priority:** P1 | **Effort:** S
   1. Understand: `PersistentPreRunE` runs before every subcommand handler; `cmd.ExecuteContext(context.Background())` is required for context propagation through cobra
   2. Write tests in `cmd/atcr/main_test.go`: `TestPersistentPreRunE_ValidLevelAndFormat` (logger in context), `TestPersistentPreRunE_InvalidLevel` (returns error), `TestPersistentPreRunE_InvalidFormat` (returns error)
   3. Implement `PersistentPreRunE` in `cmd/atcr/main.go`: call `log.New(level, format, os.Stderr)`, on error return descriptive error before any subcommand runs; store logger via `log.NewContext(cmd.Context(), logger)` and update command context; use `cmd.ExecuteContext(context.Background())` in `main()`
   4. Update `cmd/atcr/review.go` to retrieve logger via `log.FromContext(cmd.Context())` (baseline for 3.4); update `cmd/atcr/serve.go` (see 3.3)
   5. Verify: `go test ./cmd/atcr/...`; `LOG_LEVEL=bogus atcr review` exits with error before running

   **Success Criteria:** Root logger constructed once; accessible via `log.FromContext` in all subcommands; invalid `LOG_LEVEL` produces error before any subcommand runs (AC1, AC2)
   **Files:** `cmd/atcr/main.go` (modify), `cmd/atcr/review.go` (modify baseline), `cmd/atcr/main_test.go` (modify) | **Duration:** 0.2 days

### 3.3 [ ] 🔧 **MCP Server Logger Reuse (AC3)**
   **Task:** Remove local `slog.Logger` construction from `cmd/atcr/serve.go`. Use root logger from context via `log.FromContext`. MCP stdio discipline (stdout = transport only) must be preserved.
   **Priority:** P1 | **Effort:** S
   1. Understand: `cmd/atcr/serve.go:newServeCmd` currently builds a local `slog.New(...)`. `internal/mcp/server.go` already accepts `*slog.Logger`. MCP stdout must remain protocol-only.
   2. Write test `TestServeCmd_UsesContextLogger` in `cmd/atcr/serve_test.go`; verify existing MCP protocol tests pass unchanged
   3. Replace local `slog.New(...)` in `serve.go:newServeCmd` with `log.FromContext(cmd.Context())`; pass retrieved logger to `mcp.Serve`
   4. Verify: `go test ./cmd/atcr/... ./internal/mcp/...`; stdout protocol-only confirmed by MCP tests (AC3)
   5. Commit (batching 3.1–3.3): `git add cmd/atcr/main.go cmd/atcr/serve.go cmd/atcr/review.go cmd/atcr/main_test.go cmd/atcr/serve_test.go && git commit -m "feat(cli): wire shared logger through PersistentPreRunE, context, and MCP server"`

   **Success Criteria:** `cmd/atcr/serve.go` has no local logger construction; root logger from context passed to MCP server; MCP stdout is protocol-only (AC3)
   **Files:** `cmd/atcr/serve.go` (modify), `cmd/atcr/serve_test.go` (create/modify) | **Duration:** 0.2 days

### 3.4 [ ] 🔧 **Review Command Correlation — WithReviewID (AC9)**
   **Task:** After `fanout.PrepareReview` returns `prep.ID`, attach `review_id` to the context logger and propagate the correlated context to all downstream calls (ExecuteReview, RunReconcile, Verify).
   **Priority:** P1 | **Effort:** S
   1. Understand: Review [request-correlation.md](plan/documentation/request-correlation.md). The review ID is only available after `PrepareReview` returns — this is the earliest correlation attachment point.
   2. Write tests in `cmd/atcr/review_test.go`: `TestRunReview_AttachesReviewID`, `TestRunReview_ContextLoggerFlowsToExecuteReview`, `TestRunReview_NoLocalLogger`
   3. In `cmd/atcr/review.go:runReview`: after `PrepareReview` returns `prep`, call `logger = log.WithReviewID(logger, prep.ID)` and `ctx = log.NewContext(ctx, logger)`; use updated `ctx` for all subsequent calls (`ExecuteReview`, `RunReconcile`, `Verify`); do NOT reuse `cmd.Context()` after this point
   4. Verify: `go test ./cmd/atcr/...`; no local logger construction in `review.go`; `review_id` flows to downstream calls (AC9)

   **Success Criteria:** `review_id` attached after `PrepareReview`; updated context used for all downstream calls; no local `slog.New` or `slog.Default` in `review.go` (AC9)
   **Files:** `cmd/atcr/review.go` (modify), `cmd/atcr/review_test.go` (create/modify) | **Duration:** 0.2 days

### 3.5 [ ] 🔧 **Reconcile Command Logger Wiring**
   **Task:** Route `cmd/atcr/reconcile.go` diagnostics through the context logger. Replace direct `cmd.ErrOrStderr()` writes with `logger.Warn`/`logger.Debug`. Path-bearing details go to Debug level. User-facing stdout summary unchanged.
   **Priority:** P2 | **Effort:** S
   1. Understand: `runReconcile` writes `--require-verified` warning and scorecard diagnostics to stderr directly, bypassing `LOG_LEVEL`, redaction, and correlation IDs. Stdout summary output must remain unchanged.
   2. Write tests in `cmd/atcr/reconcile_test.go`: `TestRunReconcile_UsesContextLogger`, `TestRunReconcile_RequireVerifiedWarning`, `TestRunReconcile_NoSlogDefault`
   3. Retrieve `logger := log.FromContext(cmd.Context())` at start of `runReconcile`; replace `--require-verified` warning with `logger.Warn(...)`; route scorecard diagnostics through `logger.Info`/`logger.Warn`; put path-bearing details behind `logger.Debug(...)`
   4. Verify: `go test ./cmd/atcr/...`; `--require-verified` warning visible at default `info` level; user-facing stdout unchanged
   5. Commit (batching 3.4–3.5): `git add cmd/atcr/review.go cmd/atcr/reconcile.go cmd/atcr/review_test.go cmd/atcr/reconcile_test.go && git commit -m "feat(cli): wire review_id correlation and reconcile diagnostic routing"`

   **Success Criteria:** Diagnostics route through context logger; path details at debug level; `--require-verified` warning visible at default level; stdout summary unchanged (no breaking change for CLI users)
   **Files:** `cmd/atcr/reconcile.go` (modify), `cmd/atcr/reconcile_test.go` (create/modify) | **Duration:** 0.2 days

### 3.5.A [ ] **Phase 3 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `cmd/atcr/main.go`, `cmd/atcr/serve.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, and related test files

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the Phase 3 implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Phase 3 CLI and MCP wiring`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/serve.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/reconcile.go`
     - Checklist (pass verbatim):
       - SECURITY: Does `PersistentPreRunE` log the invalid level/format error in a way that could echo back sensitive env var content? Does MCP stdout-only discipline hold in all code paths — can any logger write reach stdout in serve mode?
       - EDGE CASES: What happens if cobra's `--help` or `--version` flag bypasses `PersistentPreRunE`? What if `PrepareReview` fails before `prep.ID` is set — does `runReview` nil-panic on `WithReviewID`? What if `log.NewContext` is called but the updated `ctx` is not propagated to one of the downstream calls?
       - ERROR HANDLING: Does `PersistentPreRunE` returning an error actually prevent subcommand execution in all cobra configurations? Does `runReconcile` fall back gracefully if no logger is in context (discard fallback from `FromContext`)?
       - PERFORMANCE: `PersistentPreRunE` runs before every subcommand invocation — is logger construction (`log.New`) acceptably cheap for `atcr --help` and short commands?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before 3.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

---

### 3.6 [ ] Phase 3 — Definition of Done

- [ ] `go test ./cmd/atcr/... ./internal/mcp/...` — all passing
- [ ] `go build ./cmd/atcr/...` — succeeds
- [ ] `go vet ./cmd/atcr/...` — clean
- [ ] `cmd/atcr/serve.go` has no local `slog.New(...)` construction
- [ ] `cmd/atcr/review.go` attaches `review_id` after `PrepareReview`; updated context used for all downstream calls
- [ ] `cmd/atcr/reconcile.go` uses context logger for diagnostics; no direct `cmd.ErrOrStderr()` for warnings
- [ ] `LOG_LEVEL=bogus atcr version` exits with error before subcommand runs
- [ ] MCP tests verify stdout is protocol-only (AC3)
- [ ] Adversarial review 3.5.A completed

```
Phase-3 DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/9
Manual Review: [ ] Code reviewed
```

### 3.7 [ ] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of Phase 3 implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/serve.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/reconcile.go`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the context logger propagation contract clear for Phase 4 consumers (`internal/fanout`, `internal/payload`)? Will `log.FromContext(ctx)` return the correlated logger (with `review_id`) inside `ExecuteReview` when called from `runReview`?
       - CONFIG SURFACE: Are `LOG_LEVEL` and `--log-format` now documented in cobra help text for Phase 5 documentation task?
       - INTEGRATION: Does the `review_id` set in `runReview` flow through the context to `ExecuteReview` so that Phase 4's `log.WithAgent` call will produce lines with BOTH `review_id` and `agent_name`?
       - PHASE-EXIT CONTRACT: Can Phase 4 (`internal/fanout`, `internal/payload`, `internal/llmclient`) retrieve the logger from context without additional CLI layer changes?
       - REGRESSION: Do MCP stdio protocol tests pass (stdout is transport-only)? Does `go test ./...` pass cleanly?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Integration (2 days)

**Goal:** Migrate engine packages to the injected logger. Remove `slog.Default()` from payload. Wire `log.WithAgent` into fanout. Migrate llmclient to use `ClassifiedError`.

**Dependencies:** Phase 1 (`internal/log`), Phase 2 (`internal/errors`), Phase 3 (context logger available in all subcommand contexts).

---

### 4.1 [ ] 🔧 **Remove Payload slog.Default() Fallback (AC4)**
   **Task:** Replace `slog.Default()` fallback in `internal/payload/diff.go:gitRunner.log()` with nil-safe discard logger. Inject context logger in `BuildEntries` and `ChangedFileCount`.
   **Priority:** P1 | **Effort:** S
   1. Understand: Run `grep -n 'slog\.Default' internal/payload/` to find all sites; review `internal/mcp/handlers.go:logger()` nil-safe pattern; run `grep -n 'gitRunner{' internal/payload/` to find all test construction sites that need updating
   2. Update tests: add discard logger (`slog.New(slog.NewTextHandler(io.Discard, nil))`) to all `gitRunner{}` constructions in `diff_test.go`, `builder_test.go`, `pipeline_test.go`; add `TestGitRunner_NilLogger_NoPanic`
   3. Modify `internal/payload/diff.go:gitRunner.log()`: return `slog.New(slog.NewTextHandler(io.Discard, nil))` when `r.log == nil` (same as `internal/mcp/handlers.go` pattern); modify `builder.go:BuildEntries` and `ChangedFileCount` to call `log.FromContext(ctx)` and inject into `gitRunner`
   4. Verify: `go test ./internal/payload/... -race`; `grep -r 'slog\.Default()' internal/` returns no matches (AC4)
   5. Commit: `git add internal/payload/ && git commit -m "refactor(payload): remove slog.Default() fallback, inject context logger"`

   **Success Criteria:** `slog.Default()` removed; `gitRunner.log()` returns discard when nil (no panic); `BuildEntries`/`ChangedFileCount` inject context logger; all payload tests pass (AC4)
   **Files:** `internal/payload/diff.go`, `internal/payload/builder.go` (modify); `internal/payload/diff_test.go`, `internal/payload/builder_test.go`, `internal/payload/pipeline_test.go` (modify) | **Duration:** 0.5 days

### 4.1.A [ ] **4.1 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/payload/diff.go`, `internal/payload/builder.go`, `internal/payload/diff_test.go`, `internal/payload/builder_test.go`, `internal/payload/pipeline_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.1 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.1 payload slog.Default removal`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/diff.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/builder.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/diff_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/builder_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Does the injected logger flow through the redactor (so absolute paths in git output don't leak at debug level)? Is there a path through `BuildEntries` or `ChangedFileCount` where git output reaches the logger without redaction?
       - EDGE CASES: Context with no logger — does the discard fallback in `gitRunner.log()` activate correctly? Does `gitRunner` being embedded in another struct break the nil-safe logger check?
       - ERROR HANDLING: Does `BuildEntries` handle a cancelled context (logger may still be valid even when ctx is done — no confusion)?
       - PERFORMANCE: `log.FromContext(ctx)` called once per `BuildEntries` invocation — acceptable. Is it called more than once per invocation (redundant calls)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before 4.2, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

---

### 4.2 [ ] 🔧 **Fanout Engine Logger Wiring (AC10)**
   **Task:** Add `*slog.Logger` field and `WithLogger` option to `internal/fanout/engine.go`. Call `log.WithAgent(e.logger(), a.Name)` before each agent invocation. Wire logger in `ExecuteReview` and `invokeSkeptic`. Migrate highest-risk direct stderr writes.
   **Priority:** P1 | **Effort:** M
   1. Understand: Review `internal/fanout/engine.go:Agent` struct (has `Name` field used for `WithAgent`), `invokeAgent`, `ExecuteReview`, `internal/verify/invoke.go:invokeSkeptic`, `logSkepticFailure`, and the existing `WithDispatcher` option pattern to model `WithLogger` after
   2. Write tests: `TestEngine_WithLogger`, `TestEngine_NilLogger_ReturnsDiscard`, `TestInvokeAgent_AttachesAgentName`, `TestInvokeSkeptic_UsesLogger`; update existing engine and verify tests to inject discard logger
   3. Add to `engine.go`: `log *slog.Logger` field; `WithLogger(l *slog.Logger) EngineOption`; `func (e *Engine) logger() *slog.Logger` with discard fallback; in `invokeAgent`: `agentLogger := log.WithAgent(e.logger(), a.Name)` at start of each invocation; in `review.go:ExecuteReview`: `logger := log.FromContext(ctx)` + `WithLogger(logger)` on `NewEngine`; in `verify/invoke.go:invokeSkeptic`: same; replace `logSkepticFailure`'s `fmt.Fprintf(os.Stderr, ...)` with `logger.Warn("skeptic failed", "error", err)`; put path-bearing details behind `logger.Debug(...)`
   4. Verify: `go test ./internal/fanout/... ./internal/verify/... -race`; captured log output shows `agent_name` attribute (AC10)
   5. Commit: `git add internal/fanout/ internal/verify/ && git commit -m "feat(fanout): wire logger injection and WithAgent per-agent correlation"`

   **Success Criteria:** `Engine` has logger field and `WithLogger` option; nil-safe `logger()` method; `WithAgent` called before each agent invocation; `logSkepticFailure` routes through logger; path details at debug level; all fanout and verify tests pass (AC10)
   **Files:** `internal/fanout/engine.go`, `internal/fanout/review.go`, `internal/verify/invoke.go` (modify); `internal/fanout/engine_test.go`, `internal/fanout/review_test.go` (modify) | **Duration:** 1 day

### 4.2.A [ ] **4.2 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/fanout/engine.go`, `internal/fanout/review.go`, `internal/verify/invoke.go`, `internal/fanout/engine_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2 fanout engine logger wiring`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/engine.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/invoke.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/engine_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Does `log.WithAgent` scope the logger to a single agent invocation (not leaked to other goroutines in the pool)? Are any path-bearing skeptic failure details still written to `os.Stderr` directly after the migration?
       - EDGE CASES: Empty `a.Name` string in `WithAgent` — does it produce a useless `agent_name=""` attribute? `ExecuteReview` called with a context that has no logger (discard fallback activates)? `WithLogger` called twice on the same engine — does last value win cleanly?
       - ERROR HANDLING: After migrating `logSkepticFailure` from `fmt.Fprintf` to `logger.Warn`, does the skeptic failure still propagate as an error to the caller, or does it get swallowed by the logger call?
       - PERFORMANCE: `log.WithAgent` called once per `invokeAgent` invocation allocates a new `slog.Logger` — acceptable for N-agent fanout where N is typically 3-10?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

---

### 4.3 [ ] 🔧 **llmclient Error Classification Migration (AC11, AC12)**
   **Task:** Wrap HTTP and transport errors in `internal/llmclient/client.go:send` with `internal/errors.ClassifiedError`. Preserve `errors.As` reachability to `*HTTPStatusError` through the wrapper.
   **Priority:** P1 | **Effort:** S
   1. Understand: Read `internal/llmclient/client.go:253-305` (`send` function with retry loop), `retryableStatus` map (line 37, 429/500/502/503/504), and `TestComplete_HTTPStatusErrorSurfacedForClassification` (line 437) to understand the contract that must not break
   2. Add tests to `client_test.go`: `TestComplete_TransientError_IsRetryable` (503 exhausted → `IsRetryable` returns true), `TestComplete_PermanentError_NotRetryable` (404 → `IsRetryable` returns false), `TestComplete_TransientError_ErrorsAsHTTPStatusError`, `TestComplete_PermanentError_ErrorsAsHTTPStatusError`, `TestComplete_TransportError_IsRetryable`, `TestComplete_ContextDeadline_NotWrapped`
   3. In `send`: wrap retryable status exhaustion with `errors.NewTransient(err)`; wrap non-retryable status with `errors.NewPermanent(err)`; wrap transport exhaustion with `errors.NewTransient(err)`; do NOT wrap `ctx.Err()` returns (context cancellation is its own sentinel)
   4. Verify: `go test ./internal/llmclient/... -race`; `TestComplete_HTTPStatusErrorSurfacedForClassification` and `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` both still pass unchanged (AC11, AC12)
   5. Commit: `git add internal/llmclient/ && git commit -m "feat(llmclient): wrap HTTP errors in ClassifiedError taxonomy"`

   **Success Criteria:** 429/5xx/transport → `NewTransient`; non-retryable 4xx → `NewPermanent`; `errors.As` reaches `*HTTPStatusError` through wrapper; `errors.Is` still works for `context.DeadlineExceeded`; existing tests pass unchanged (AC11, AC12)
   **Files:** `internal/llmclient/client.go` (modify), `internal/llmclient/client_test.go` (modify) | **Duration:** 0.5 days

### 4.3.A [ ] **4.3 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/llmclient/client.go`, `internal/llmclient/client_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.3 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.3 llmclient error classification`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/llmclient/client.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/llmclient/client_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors.go`
     - Checklist (pass verbatim):
       - SECURITY: Can a 401 (bad/expired API key) be incorrectly classified as transient, causing retries that hammer the provider with a known-bad key and risk rate limiting or lockout? Is the classification for 429 read as "transient-stop-now" or does it cause additional retry attempts beyond the intended limit?
       - EDGE CASES: Are ALL return paths in `send` updated — is there any path where a retryable status error returns without the `NewTransient` wrapper? What happens with a 408 (Request Timeout) — transient or permanent? Is `context.DeadlineExceeded` correctly left unwrapped on ALL return paths, or only some?
       - ERROR HANDLING: Does double-wrapping occur if the retry loop calls `send` recursively or if the error is wrapped both inside and outside the loop? Does `ClassifiedError.Unwrap()` correctly reach `*HTTPStatusError` through the wrapper so `TestComplete_HTTPStatusErrorSurfacedForClassification` still passes?
       - PERFORMANCE: `errors.As` walk on every error check after `send` returns — is this on the hot path given the retry loop frequency?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before 4.4, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

---

### 4.4 [ ] Phase 4 — Definition of Done

- [ ] `go test ./internal/payload/... ./internal/fanout/... ./internal/verify/... ./internal/llmclient/...` — all passing
- [ ] `go test -race ./internal/...` — no data races
- [ ] `go vet ./internal/...` — clean
- [ ] `grep -r 'slog\.Default()' internal/` — returns no matches (AC4)
- [ ] `grep -r 'fmt\.Fprintf(os\.Stderr' internal/fanout/ internal/verify/` — highest-risk sites migrated
- [ ] `errors.IsRetryable` returns true for 429/5xx/transport errors, false for 4xx permanent (AC11, AC12)
- [ ] `TestComplete_HTTPStatusErrorSurfacedForClassification` and `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` pass unchanged
- [ ] All adversarial reviews completed: 4.1.A, 4.2.A, 4.3.A

```
Phase-4 DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/8
Manual Review: [ ] Code reviewed
```

### 4.5 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of Phase 4 implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/diff.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/builder.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/engine.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/invoke.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/llmclient/client.go`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does the full correlation chain work end-to-end: `cmd/atcr/review.go` sets `review_id` → context flows to `ExecuteReview` → `invokeAgent` calls `WithAgent` → every agent log line has BOTH `review_id` and `agent_name`? Trace the chain explicitly.
       - CONFIG SURFACE: Are there any remaining `fmt.Fprintf(os.Stderr, ...)` calls in the reviewed files that should have been migrated but were not?
       - INTEGRATION: Does `internal/llmclient` now import `internal/errors` without circular deps? Does `internal/payload` import `internal/log` without circular deps? Does `internal/fanout` import both `internal/log` and use `log.WithAgent` correctly?
       - PHASE-EXIT CONTRACT: Can Phase 5 (documentation + integration test) verify all 13 acceptance criteria without any further code changes to phases 1-4?
       - REGRESSION: Do the existing `TestComplete_HTTPStatusErrorSurfacedForClassification`, `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries`, and `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` all pass with `-race`?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Final Phase: Validation (1 day)

**Goal:** Verify all 13 acceptance criteria are met. Write and run the end-to-end integration test. Update documentation. Confirm no scope creep against original requirements.

**Dependencies:** All preceding phases complete.

---

### 5.1 [ ] 🔧 **Documentation and Configuration Updates**
   **Task:** Update CLI help text, create/update user-facing docs, update sprint-config.md, create `internal/errors/README.md`.
   **Priority:** P2 | **Effort:** S
   1. Update `cmd/atcr/main.go:newRootCmd` `Long` description to mention `LOG_LEVEL` env var and `--log-format` flag with accepted values and defaults
   2. Create/update `docs/logging.md` with: `LOG_LEVEL` values (debug/info/warn/error, default info), `--log-format` values (text/json, default text), debug recipe (`LOG_LEVEL=debug atcr review ...`), JSON CI recipe (`LOG_LEVEL=debug --log-format=json atcr review ...`)
   3. Update `.planning/.config/sprint-config.md`: find `LOG_LEVEL` entry and change status from "optional" or "documented" to "implemented"
   4. Create `internal/errors/README.md`: `Classification` constants and meanings, constructor functions with nil-safety note, `IsRetryable` usage, code example wrapping and checking an error
   5. Verify: `atcr --help` mentions both `LOG_LEVEL` and `--log-format`; `atcr review --help` shows inherited `--log-format`; no broken links
   6. Commit: `git add cmd/atcr/main.go docs/ .planning/.config/sprint-config.md internal/errors/README.md && git commit -m "docs: document LOG_LEVEL and --log-format, update sprint-config, add errors API reference"`

   **Success Criteria:** `atcr --help` mentions both config options; sprint-config.md shows `LOG_LEVEL` as implemented; `internal/errors/README.md` created with API reference
   **Files:** `cmd/atcr/main.go`, `docs/logging.md`, `.planning/.config/sprint-config.md`, `internal/errors/README.md` | **Duration:** 0.25 days

### 5.2 [ ] 🔧 **End-to-End Integration Test**
   **Task:** Create `internal/integration/logging_test.go` verifying all plan success criteria. Run full test suite with race detector. Manual verification of `LOG_LEVEL=debug` review run.
   **Priority:** P1 | **Effort:** M
   1. Run `go test -race ./...` — must pass with 0 failures; fix any race conditions before proceeding
   2. Run `go vet ./...` — must be clean
   3. Create `internal/integration/logging_test.go` with tests using `t.TempDir()` fixtures and `httptest.NewServer` for provider mocks:
      - `TestIntegration_ReviewRun_DebugOutputHasCorrelation` — run with `LOG_LEVEL=debug`, assert every log line includes `review_id` (AC9) and agent log lines include `agent_name` (AC10)
      - `TestIntegration_ReviewRun_JSONFormat` — run with `--log-format=json`, parse each line with `encoding/json`, assert `"level"` and `"msg"` keys present (AC2)
      - `TestIntegration_ReviewRun_NoSecretLeak` — configure known API key, run review, assert key does not appear anywhere in log output (AC5)
      - `TestIntegration_ReviewRun_NoAbsolutePathLeak` — configure review root, run review, assert no absolute paths appear in log output (AC6)
      - `TestIntegration_MCPMode_StdoutClean` — run in serve mode, assert stdout contains only MCP protocol JSON (AC3)
      - `TestIntegration_LLMClient_ErrorClassification` — mock 503 provider, call Complete, assert `errors.IsRetryable(err)` is true (AC12)
      - `TestIntegration_LLMClient_PermanentError` — mock 404 provider, assert `errors.IsRetryable(err)` is false (AC12)
   4. Manual verification: run `LOG_LEVEL=debug atcr review <fixture>` against a real test fixture; confirm review_id and agent_name on log lines, no secrets, no absolute paths
   5. Commit: `git add internal/integration/logging_test.go && git commit -m "test(integration): add end-to-end logging and error classification integration tests"`

   **Success Criteria:** `go test -race ./...` — 0 failures; all 7 integration tests pass; manual review run confirms AC1, AC5, AC6, AC9, AC10 visually
   **Files:** `internal/integration/logging_test.go` (create) | **Duration:** 0.75 days

---

### Validation Checklist

- [ ] `go test -race ./...` — 0 failures
- [ ] `go vet ./...` — clean
- [ ] `go build ./...` — succeeds
- [ ] `golangci-lint run` — no new errors
- [ ] `go test -cover ./internal/log/...` — 100% on level parsing, redaction, sink wiring (AC8)
- [ ] `go test -cover ./internal/errors/...` — 100% on classification and retryability logic (AC13)
- [ ] `grep -rn 'slog\.Default' internal/ cmd/` — no matches in production code (AC7)
- [ ] Manual: `LOG_LEVEL=debug atcr review <fixture>` — debug lines appear; every line includes `review_id=<id>` (AC1, AC9)
- [ ] Manual: agent log lines include `agent_name=<name>` (AC10)
- [ ] Manual: `LOG_LEVEL=debug --log-format=json atcr review <fixture>` — output is valid newline-delimited JSON (AC2)
- [ ] Manual: API key value does not appear in log output at any level (AC5)
- [ ] Manual: absolute paths rendered relative to review root in output (AC6)
- [ ] Manual: `atcr serve` in MCP mode — stdout contains only protocol messages, no log output (AC3)

### Optional: Targeted Mutation Testing
Mutation testing is UNAVAILABLE in this environment. Skip this step.

### Drift Analysis

Compare deliverables against [original-requirements.md](plan/original-requirements.md):

- [ ] `internal/log` package with `New`, `LevelFromString`, `FromContext`, `NewContext`, redaction helpers (`Redactor`, `NewRedactor`), `WithReviewID`, `WithAgent`, `AttrReviewID`, `AttrAgentName`
- [ ] `internal/errors` package with `ClassifiedError`, `NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`, `IsRetryable`
- [ ] `LOG_LEVEL` and `--log-format` implemented and wired (not just documented)
- [ ] MCP server reuses root logger — no local construction
- [ ] `internal/payload/diff.go` has no `slog.Default()` fallback
- [ ] `internal/llmclient` wraps HTTP errors in `ClassifiedError`
- [ ] All out-of-scope items remain unimplemented: metrics/telemetry (Epic 4.3), log rotation, audit trail (Epic 13.0), full `fmt.Fprintf` migration in one pass, circuit breaker (Epic 4.4)
- [ ] No scope added beyond original request

---

### 5.3 [ ] Phase 5 — Definition of Done

- [ ] All validation checklist items above complete
- [ ] All 13 acceptance criteria (AC1–AC13) verified
- [ ] Sprint success criteria met: all production diagnostics flow through `internal/log`; no secrets or absolute paths leak in default-level logs; operators can debug via `LOG_LEVEL=debug`; tests can assert on log output without global state; every llmclient error is classified; a reviewer can grep logs by review ID
- [ ] `git log --oneline feature/4.0_structured_logging` shows atomic commits for each task
- [ ] Ready for `/finalize-sprint`

```
Phase-5 (Final) DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/13
Manual Review: [ ] All 13 ACs verified | [ ] Drift analysis complete | [ ] No scope creep
```

### 5.4 [ ] **Phase 5 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All sprint deliverables — final gate before `/finalize-sprint`

   **Spawn a fresh subagent** via the Agent tool to perform this final integration review. The subagent has no memory of the sprint implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 final gate review`
   - prompt: Self-contained brief including:
     - All new/modified files across all phases (absolute paths):
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/log.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/redact.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/log/correlation.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/errors.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/serve.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/reconcile.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/diff.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/payload/builder.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/engine.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/fanout/review.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/invoke.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/llmclient/client.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/integration/logging_test.go`
       - `/Users/samestrin/Documents/GitHub/atcr/internal/errors/README.md`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are all 13 acceptance criteria (AC1–AC13) demonstrably met? Which ones have only manual verification — are those verification steps documented?
       - CONFIG SURFACE: Is `LOG_LEVEL` behavior documented consistently across CLI help text, `docs/logging.md`, and sprint-config.md? Are there any "optional" or "TBD" qualifiers still remaining?
       - INTEGRATION: Does the complete correlation chain work end-to-end: CLI flag → `PersistentPreRunE` → `review_id` attached after `PrepareReview` → `ExecuteReview` context → `invokeAgent` → `WithAgent` → every agent log line has both `review_id` and `agent_name`?
       - PHASE-EXIT CONTRACT: Are all "out of scope" items from the original requirements (metrics, log rotation, audit trail, full fmt.Fprintf migration, circuit breaker) still unimplemented? Is there any scope creep?
       - REGRESSION: Does `go test -race ./...` pass clean? Are there any remaining direct `os.Stderr` writes or `slog.Default()` calls in production code?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" — sprint is complete. Run `/finalize-sprint`
   **Duration:** 15-30 min
