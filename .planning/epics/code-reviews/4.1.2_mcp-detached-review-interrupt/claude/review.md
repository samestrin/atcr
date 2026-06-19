
### Criterion: AC1 — detached review in flight at server shutdown marked `interrupted` (not `in_progress`)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/server.go:49` (Serve calls `e.shutdownReviews(ctx.Err() != nil, shutdownDrain)`), `internal/mcp/server.go:60-66` (`shutdownReviews` fires `e.shutdownCancel()` before `e.drain`), `internal/mcp/handlers.go:110-117` (`withShutdownCancel` ties rctx cancel to `shutdownCtx` via `context.AfterFunc`), `internal/fanout/review.go:361` (`interrupted := errors.Is(ctx.Err(), context.Canceled)`), `internal/fanout/status.go:216` (`m.Interrupted` → `RunInterrupted`). Test: `internal/mcp/shutdown_test.go:69` `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`.
- **Notes:** Shutdown cancels the WithCancel child (context.Canceled), the fan-out flushes partial + interrupted manifest within the 5s drain. Cancel fired BEFORE drain so the flush write is awaited.

### Criterion: AC2 — handler returning normally does NOT mark its detached review interrupted (detachment preserved)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/handlers.go:97` (`reviewContext` uses `context.WithoutCancel(ctx)`), `internal/mcp/handlers.go:110-117` (sole cancellation source is `e.shutdownCtx`, never the request ctx), `internal/mcp/handlers.go:229-233` (detached goroutine wires `withShutdownCancel(reviewContext(...))`). Tests: `internal/mcp/shutdown_test.go:183` `TestWithShutdownCancel_ParentReturnDoesNotCancel`, `:104` `TestServeShutdown_NormalCompletionNotInterrupted`.
- **Notes:** Detached base is WithoutCancel, so request cancellation cannot propagate; only server-lifecycle ctx cancels the child.

### Criterion: AC3 — existing serve-mode drain behavior unchanged
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/server.go:60-66` (`shutdownReviews` still calls `e.drain(timeout)`; on clean disconnect `serverShutdown=false` so reviews are left running per the original drain contract), `internal/mcp/server.go:49` (gate `ctx.Err() != nil` distinguishes SIGINT from clean stdio disconnect). Test: `internal/mcp/shutdown_test.go:85` `TestServeShutdown_ClientDisconnectDoesNotInterrupt` (stays `in_progress`, never interrupted).
- **Notes:** The independent-review refinement (commit in PR #39) correctly prevents a client disconnect from force-interrupting near-complete reviews — drain semantics preserved.

### Criterion: AC4 — no false `completed` status ever produced for an interrupted run
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/status.go:212-218` (`m.Interrupted` applied last, overrides both `RunCompleted` and `RunFailed` branches), `internal/fanout/review.go:361,389` (manifest.Interrupted stamped from parent-ctx Canceled check). Test: `internal/mcp/shutdown_test.go:104-128` `TestServeShutdown_NormalCompletionNotInterrupted` (a finished review stays `completed`; a later shutdown does not retroactively flip it).
- **Notes:** Interrupted overrides last → an interrupted partial cannot masquerade as completed; a genuinely-finished review is not retroactively interrupted.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Discovery (no sprint-design.md — epic mode)
**Files Reviewed:** 3 (internal/mcp/handlers.go, server.go, shutdown_test.go)
**Issues Found:** 3 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 3

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 2

### Notes
The adversarial pass surfaced 8 raw candidates; 5 were discarded after correctness review (nil-ctx guard is stdlib-idiomatic; `reviewContext`-in-goroutine relies on `WithoutCancel` preserving the value chain and is pre-existing; diffuse cancel ownership needs no change per Go idempotency; call-site coupling already documented at `withShutdownCancel`). The epic also self-captured 4 TD items pre-merge (observability parity rows 41–42, double-cancel note row 43, inverse-AC4 false-interrupted microscopic window row 44) — not re-reported here. The 3 recorded items are net-new: the missing `Serve`-level test of the SIGINT-vs-disconnect discrimination (strongest), the drain-timeout-vs-flush persistence window, and a brittle staleness coupling in the disconnect test. The core AC mechanism (context.Canceled propagation → manifest.Interrupted → status override) is sound and race-clean.
