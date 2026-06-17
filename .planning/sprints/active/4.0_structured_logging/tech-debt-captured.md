# Tech Debt Captured — Sprint 4.0 Structured Logging

Items deferred during `/execute-sprint`. Read by `/execute-code-review` and pre-seeded into the adversarial TD stream.

## TD-001 — log.New does not validate nil writer (LOW)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-06-17
**File:** internal/log/log.go:New
**Issue:** `New` accepts a nil `io.Writer` without validation. A nil writer passes construction silently and panics only on the first log write, producing a deferred failure far from the call site.
**Why accepted:** In production the writer is always `os.Stderr` (set in cmd/atcr Phase 3); no current call site passes nil. Fail-fast is a hardening nicety, not a correctness bug.
**Fix in:** Phase 3 (CLI wiring) or a later hardening pass — add `if w == nil { return nil, fmt.Errorf("log: nil writer") }` before building the handler, with a covering test.

## TD-002 — log package errors are not typed sentinels (LOW)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-06-17
**File:** internal/log/log.go:LevelFromString/New
**Issue:** `LevelFromString` and `New` return generic `fmt.Errorf` string errors. Callers cannot distinguish "invalid level" from "invalid format" programmatically via `errors.Is`.
**Why accepted:** Current consumers (Phase 3 PersistentPreRunE) surface the error text directly to the user and do not branch on error identity. Sentinels add value only if a caller needs to branch.
**Fix in:** Future hardening — define exported `ErrInvalidLevel`/`ErrInvalidFormat` sentinels and wrap with `%w` if a caller ever needs to branch.

## TD-003 — llmclient sk- redaction regex is case-sensitive (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-17
**File:** internal/llmclient/client.go:339
**Issue:** `skKeyPattern` in llmclient is `sk-\S+` (case-sensitive), the same gap fixed in `internal/log/redact.go` during 1.2.A. A `SK-`/`Sk-` shaped foreign token echoed in a provider error body would bypass the llmclient scrub.
**Why accepted:** Real OpenAI keys are always lowercase `sk-`; the case-variant risk is defense-in-depth. llmclient is owned by Phase 4.3 (llmclient migration) — fixing it now would be scope creep into a file with its own test suite.
**Fix in:** Phase 4.3 (llmclient migration) — add `(?i)` to `skKeyPattern` in client.go to match the log package, with a regression test mirroring `TestRedact_SKKeyCaseInsensitive`.

## TD-004 — redaction misses URL-encoded Bearer%20 tokens (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-17
**File:** internal/log/redact.go:bearerTokenPattern
**Issue:** `bearerTokenPattern` requires literal whitespace (`Bearer\s+\S+`), so a URL-encoded `Bearer%20<token>` appearing in a logged URL or error body is not scrubbed.
**Why accepted:** Logged auth headers in this codebase use literal-space form; encoded variants are not produced by current log sites. The existing llmclient scrub has the same limitation, so no regression is introduced.
**Fix in:** Future hardening pass — extend the pattern to `(?i)Bearer(\s+/%20)\S+` or percent-decode messages before applying token regexes, with a covering test.

## TD-005 — exact-secret scrub is case-sensitive (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-17
**File:** internal/log/redact.go:Redact
**Issue:** Configured secrets are removed via case-sensitive `strings.ReplaceAll`; a secret echoed by an upstream provider with altered casing would not match the exact-secret pass (bearer/sk- regex only backstops those shapes, not arbitrary configured secrets).
**Why accepted:** Registry API keys are opaque values returned verbatim by providers; casing changes are not observed in practice. Case-folding every secret against every message adds cost for a hypothetical case.
**Fix in:** Future hardening — if a provider is found to alter secret casing, fold case for the exact-secret comparison or document verbatim-only matching.

## TD-006 — correlation helpers do not enforce single-key semantics (LOW)
**Origin:** Phase 1, task 1.3.A adversarial review, 2026-06-17
**File:** internal/log/correlation.go:WithReviewID/WithAgent
**Issue:** `slog.Logger.With` appends attributes rather than replacing them, so wrapping a logger that already carries `review_id`/`agent_name` emits the key twice (last-wins for most JSON parsers, but the raw line carries both). There is no handler-level dedup guard.
**Why accepted:** No external spoofing vector exists — agents are LLM subprocesses, not Go callers; only the engine invokes these helpers. The intended wiring attaches each key exactly once (review_id in review.go, agent_name in invokeAgent). Mitigated this sprint via a documented call-once contract and the `TestCorrelation_DoubleWrapAppends` regression test. The plan also mandates the simple `logger.With(...)` design, so handler-level dedup is out of spec scope.
**Fix in:** Future hardening — if double-wrap misuse appears, add a custom handler (or `ReplaceAttr` collapse) in `log.New` that enforces single-occurrence for correlation keys, with a test asserting one key after a double wrap.

## TD-007 — redaction is not enforced at the log sink (MEDIUM)
**Origin:** Phase 1, task 1.6 phase gate review, 2026-06-17
**File:** internal/log/log.go:New
**Issue:** `Redactor.Redact` is a standalone helper; `log.New` builds a plain text/JSON `slog.Handler` with no redaction in the pipeline. A caller can log a secret or absolute path directly through slog and bypass the `Redactor` entirely. The package's "single diagnostic sink" intent implies enforcement that does not yet exist.
**Why accepted:** Phase 1's task scope (1.2) was explicitly "create the redaction helpers", not sink wiring. The `New(level, format, w)` signature is fixed by the plan and carries no `*Redactor`. This is a deliberate cross-phase boundary, not an oversight.
**Fix in:** Phase 3/4 — decide the enforcement model BEFORE the Phase 5 integration test (5.2): AC5 (no secret leak) and AC6 (paths relativized) require either (a) a redacting `slog.Handler` that wraps the base handler and runs `Redact` on every record/attr at the sink, or (b) caller-side `Redact` at every log site. Option (a) is strongly preferred (enforced, not opt-in). If (a), add a variadic redactor option to `New` (non-breaking) or a `NewWithRedactor` constructor. This decision gates whether AC5/AC6 are met by construction.

## TD-008 — IsRetryable lets an outer Transient wrapper override an inner Permanent (LOW)
**Origin:** Phase 2, task 2.1.A adversarial review, 2026-06-17
**File:** internal/errors/errors.go:IsRetryable
**Issue:** `IsRetryable` resolves the outermost `*ClassifiedError` via `errors.As`. If a permanent failure (e.g. a 401) were re-wrapped as `NewTransient(NewPermanent(err))`, `IsRetryable` would report true, forging retryability and risking repeated calls against a permanent auth error.
**Why accepted:** The plan (task 2.1) explicitly specifies "outer classification wins"; the outermost-wins contract is intentional so the most recent, most-informed classifier decides. No double-wrap path exists in the intended wiring — each error is classified exactly once (Phase 4 llmclient maps each HTTP status to a single constructor). Re-wrapping is a Go programming error, not external/attacker input. Mitigated this sprint via the documented single-classification contract on `ClassifiedError` and `IsRetryable`.
**Fix in:** Future hardening — if a double-classification path is ever introduced (e.g. a generic retry wrapper), add a "Permanent poisons the chain" guard: have constructors detect an inner non-retryable `*ClassifiedError` and refuse to escalate it to Transient, with a regression test asserting an inner Permanent stays non-retryable.

## TD-009 — invalid LOG_LEVEL echoes the raw env value to stderr (LOW)
**Origin:** Phase 3, task 3.5.A adversarial review, 2026-06-17
**File:** cmd/atcr/main.go:setupLogger
**Issue:** `setupLogger` passes `LOG_LEVEL` to `log.New`; an invalid value yields `log: invalid level %q` (internal/log/log.go:43) which `main()` echoes verbatim to stderr. `LOG_LEVEL` is externally influenceable in some CI contexts, and the verbatim echo is unnecessary defense-in-depth exposure.
**Why accepted:** `LOG_LEVEL` is a constrained enum (debug/info/warn/error), not free-form secret content, so blast radius is negligible — it only reflects back the level string the operator themselves set. Not a correctness or leak bug.
**Fix in:** Future hardening — emit a fixed message listing the valid level values without echoing the raw input (or cap echoed length), with a covering test.

## TD-010 — help/version paths rely on the FromContext discard fallback (LOW)
**Origin:** Phase 3, task 3.5.A adversarial review, 2026-06-17
**File:** cmd/atcr/main.go:newRootCmd
**Issue:** cobra's `--help`/`--version`/`-h` short-circuit before `PersistentPreRunE`, so no logger is stored in context for those paths. It is correct today only because every consumer uses `log.FromContext`, which returns the shared discard logger on a miss — a future consumer that asserts a logger is always present would break on these paths.
**Why accepted:** No current defect — the discard fallback (internal/log/log.go:84-91) makes all bypass paths nil-safe and silent. This is a robustness/documentation note, not actionable debt; the reviewer flagged it as "no fix required."
**Fix in:** Future hardening — document (package or PersistentPreRunE comment) that bypass paths rely on the discard fallback so future code keeps using `FromContext` rather than asserting logger presence.
