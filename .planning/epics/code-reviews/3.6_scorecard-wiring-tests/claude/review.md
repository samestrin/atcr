
### Criterion: AC1 — CLI test drives a scorecard command against a malformed store and asserts the read diagnostic lands in the command's ErrOrStderr buffer
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/scorecard_wiring_test.go:43-55` (TestLeaderboardCmd), `cmd/atcr/scorecard_wiring_test.go:24-39` (seedMalformedStore helper)
- **Notes:** seedMalformedStore appends a malformed JSONL line; test asserts `Contains(stderr, "skipping malformed record")` AND `NotContains(stdout, ...)`. Uses execCmdSplit (separate out/err buffers) to prove routing to ErrOrStderr specifically.

### Criterion: AC2 — The three scorecard-touching CLI entry points are each covered by an ErrOrStderr-wiring assertion
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/scorecard_wiring_test.go:43` (leaderboard/ReadAll), `:65` (scorecard/FindByRunID), `:87` (reconcile/EmitForReconcile). Production wiring confirmed: `leaderboard.go:65`, `scorecard.go:55`, `reconcile.go:91` all pass `cmd.ErrOrStderr()`.
- **Notes:** All three confirmed entry points (read path ×2, emit path ×1) covered. reconcile test forces ENOTDIR write-failure and asserts "scorecard: write failed" reaches stderr while exit code stays 0 (best-effort).

### Criterion: AC3 — MCP handler-level test asserts handleReconcile passes a non-default EmitOpts.Diag
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/handlers_test.go:224-238` (TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag); production seam `internal/mcp/handlers.go:38` (engine.diag), `:42-47` (diagWriter), `:234` (EmitOpts{Diag: e.diagWriter()}).
- **Notes:** Option B implemented per Clarifications — minimal injectable `diag io.Writer` field with default-to-os.Stderr resolver. Test injects `&buf`, asserts diagnostic lands there, reconcile does not fail (best-effort).

### Criterion: AC4 — A deliberate regression (swapping the wired writer back to a default) makes at least one new test fail
- **Verdict:** VERIFIED ✅
- **Evidence:** Structural: CLI tests assert `Contains(errBuf, ...)` — a regression to `ReadOpts{}`/`EmitOpts{}` (nil→os.Stderr) routes to process stderr, leaving errBuf empty → fails. MCP test asserts `Contains(buf, ...)` with injected buffer — a regression to `os.Stderr`/nil fails. Confirmed by runtime regression check (see Phase 5).
- **Notes:** Regression-detection is the core design of all four new tests; each assertion is keyed on the injected/command-scoped writer receiving the diagnostic.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md risk profile — epic)
**Files Reviewed:** 3
**Issues Found:** 3 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 3

### Runtime AC4 Regression Proof
- Swapped `EmitOpts{Diag: e.diagWriter()}` → `EmitOpts{Diag: os.Stderr}` in handlers.go:234.
- `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` FAILED: `"" does not contain "scorecard: write failed"`.
- Reverted clean; test passes again. AC4 ("deliberate regression makes a new test fail") confirmed at runtime.

### Notes
All 3 findings are LOW maintainability/testing nits (month-stem helper duplication, over-specified ENOTDIR comment, un-anchored diagnostic string literals). No correctness/security/perf defects. Production seam (engine.diag + diagWriter) is minimal and preserves the os.Stderr default for the real MCP server (server.go:61 builds engine{} with nil diag).
