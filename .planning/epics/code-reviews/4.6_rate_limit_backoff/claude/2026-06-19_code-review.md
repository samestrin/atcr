# Code Review Report: 4.6_rate_limit_backoff

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 19, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Merge Commit:** e040b80858800a78a599a096f5a3fa6cdb99a550

## 2. Acceptance Criteria Verified

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | All LLM calls route through the retry middleware | ✅ VERIFIED | `internal/llmclient/client.go:408` (`dispatch`), `:303` (`send`) |
| 2 | 429 errors trigger exponential backoff with jitter | ✅ VERIFIED | `client.go:46` (429 retryable), `:432` (jitter), `:464` (exponential) |
| 3 | The system respects Retry-After headers | ✅ VERIFIED | `client.go:469` (`retryAfter > 0`), `:471` (`honorExact`) |
| 4 | Max retries and base delays are configurable | ✅ VERIFIED | `registry/precedence.go:44,52` (defaults 5/500), `registry/config.go` (fields+validation+`Effective*`), `fanout/review.go:594,665`, `fanout/engine.go:458-459` |
| 5 | A chunk is only marked failed after max_retries exhausted | ✅ VERIFIED | `client.go:478` (terminal only after budget), `:354` (exhausted 429 never trips breaker) |

## 3. Evidence Map

- **Configurability (the epic's deliverable):** `max_retries`/`initial_backoff_ms` added at the registry-global and per-agent tiers, mirroring `timeout_secs`; seeded to defaults 5/500 ms in `ResolveSettings`; resolved per agent via `Effective*` and threaded into each call through a context-carried override (`WithRetryOverride`). Production review path receives the default budget of 5 end-to-end (independently confirmed).
- **Engine reuse:** No new `RetryMiddleware` type — the existing `dispatch()` engine (exponential backoff, jitter, Retry-After, terminal-after-exhaustion) is exposed through config, per the recorded clarifications.
- **Breaker boundary unchanged:** An exhausted 429 returns `Transient` wrapping `HTTPStatusError{429}`; `isBreakerFailure` returns false, so `send()` records a healthy reply and the Epic 4.5 breaker never trips on sustained rate-limiting.

## 4. Remaining Unchecked Items

No remaining unchecked items — all 5 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria implemented with cited evidence; full test suite green; coverage 89.4%; all quality gates pass. Adversarial review found no Critical/High defects; the one Medium and six Low items are maintainability/consistency follow-ups routed to technical debt, none blocking.

## 6. Coverage Analysis
- **Coverage:** 89.4%
- **Baseline:** 80%
- **Delta:** ↑9.4%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l |

## 8. Adversarial Analysis
- **Files Reviewed:** 7 (source files changed by the epic)
- **Issues Found:** 7 (Critical: 0, High: 0, Medium: 1, Low: 6)

### Issues by Severity
- **MEDIUM** — `internal/verify/invoke.go:118` — skeptic lane forwards raw per-agent retry config instead of resolving through `Effective*`; an unconfigured skeptic uses the bare client default (2) rather than the resolved global (5). Resilience inconsistency between review and verify paths (verify never receives resolved `Settings`).
- **LOW** — `internal/fanout/engine.go:458` — `InitialBackoffMs > 0` override sentinel overloads one knob; a bare `Agent` setting only `max_retries=0` is ignored (config-loaded agents unaffected). Comment clarity / explicit flag.
- **LOW** — `internal/registry/precedence.go:172` — retry-bound range checks duplicated across three sites with inconsistent error-string formats; extract a shared validator.
- **LOW** — `internal/registry/precedence.go:51` — `MaxInitialBackoffMs`/`DefaultInitialBackoffMs` are cross-package magic numbers coupled to llmclient's `maxBackoff`/`defaultInitialBackoff` only by comment; add a drift guard test.
- **LOW** — `internal/llmclient/retry_override.go:26` — `WithRetryOverride` clamps negative `maxRetries` but not a negative `initialBackoff`, despite advertising the guard.
- **LOW** — `internal/llmclient/client.go:424` — the pre-loop `clampBackoff(delay)` comment overstates coverage (a no-op for all config values); tighten comment + add a direct-override cap test.
- **LOW** — `internal/verify/invoke_test.go:50` — skeptic retry test covers only explicit values, not the unset→inherit case; add after the Medium fix.

### Confirmed clean (hostile reviewers checked)
Retry-After/clamp interaction correct; `maxRetries`/`delay` refactor complete; no breaker desync; unexported context key (no collision, no secrets); no over-engineering; validation complete across all three tiers.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/4.6_rate_limit_backoff.md` to merge these 7 findings into the technical-debt README (the MEDIUM skeptic item and the LOW observability item overlap with TD already captured during epic execution — reconcile will dedup by file:line).

---
*Generated by /execute-code-review on June 19, 2026 06:42:44PM*
