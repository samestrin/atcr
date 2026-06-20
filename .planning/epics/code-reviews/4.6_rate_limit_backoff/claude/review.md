# Code Review Stream - 4.6_rate_limit_backoff (Epic)

**Started:** June 19, 2026 06:42:44PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: All LLM calls route through the retry middleware
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:408` (`dispatch` retry loop), `internal/llmclient/client.go:303` (`send` wraps it)
- **Notes:** All provider calls go `Complete`/`Chat` → `send` → `dispatch`; the per-call override (`client.go:416`) layers config onto this single path. No parallel mechanism added (matches Clarifications Q1).

### Criterion: 429 errors trigger exponential backoff with jitter
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:46` (429 in `retryableStatus`), `:432` (`jitter(delay)`), `:464` retryable branch with `delay = clampBackoff(delay * factor)`
- **Notes:** Exponential schedule (×1.5, capped at 30s) with jitter; first sleep now also clamped (`:424`).

### Criterion: The system respects Retry-After headers from providers
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:469` (`retryAfter > 0`), `:471` (`honorExact = true` → slept verbatim, not jittered/clamped)
- **Notes:** Server-advertised cooldown overrides the computed backoff for that attempt.

### Criterion: Max retries and base delays are configurable
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/precedence.go:44,52` (defaults 5/500), `internal/registry/config.go` (`MaxRetries`/`InitialBackoffMs` on Registry+AgentConfig + validation + `Effective*`), `internal/fanout/review.go:594,665` (resolve per agent), `internal/fanout/engine.go:458-459` (apply via `WithRetryOverride`)
- **Notes:** This epic's core deliverable. Registry-global + per-agent tiers mirror `timeout_secs`; production review path receives default 5 end-to-end.

### Criterion: A chunk is only marked as failed after max_retries is exhausted
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:478` (retryable branch returns `Transient` only after `attempt < maxRetries` fails), `:457`/`:490` (transport + defensive exhaustion paths)
- **Notes:** A terminal error surfaces only once the budget is consumed; an exhausted 429 stays `Transient` wrapping `HTTPStatusError{429}` and never trips the Epic 4.5 breaker (`client.go:354`).

## Adversarial Analysis (Discovery Mode)

**Mode:** Full hostile review (no sprint-design.md risk profile — epic)
**Files Reviewed:** 7
**Issues Found:** 7 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 6

### Confirmed-clean (hostile reviewers checked, found no defect)
- Retry-After vs `clampBackoff(delay)` interaction: correct — Retry-After is set inside the loop with `honorExact` and slept verbatim, never passed through the pre-loop clamp.
- `maxRetries`/`delay` local-variable refactor: complete — all three former `c.maxRetries` reads in `dispatch` updated; no stray references.
- Circuit-breaker accounting: no desync — breaker records one verdict per `dispatch` regardless of retry count; an exhausted 429 still never trips it.
- Security: unexported `retryOverrideKey struct{}` — no collision; no secrets in the override.
- Over-engineering: compliant — no separate `RetryMiddleware` type; `backoff_factor`/`max_backoff` stay constants.
- Validation: complete — `max_retries` 0..10 and `initial_backoff_ms` 1..30000 enforced at registry tier, agent tier, AND post-resolution.

### Top finding
- **MEDIUM** `internal/verify/invoke.go:118` — skeptic lane forwards raw per-agent retry config instead of resolving through `Effective*`, so an unconfigured skeptic uses the bare client default (2) rather than the resolved global (5). Resilience inconsistency between review and verify paths. (Already a deferred TD item from epic execution; reconfirmed here.)
