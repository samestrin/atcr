# Tech Debt Captured — Sprint 32.0 (Sandboxed Auto-Fix Validation)

Items deferred during `/execute-sprint`. Read by `/execute-code-review` and pre-seeded into its adversarial TD stream.

## TD-001 — TimeoutSecs override lacks direct test coverage (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-19
**File:** internal/verify/autofix_exec_test.go
**Issue:** `ResolveAutoFixSandbox` applies `sc.TimeoutSecs` to `cfg.Timeout`, but that value never appears in the `docker run` argv (it is a context deadline) and the resolver signature is pinned to `(sandbox.Backend, error)` by AC 02-02, so no test directly asserts the TimeoutSecs override reached the backend. A dropped `if sc.TimeoutSecs != nil` block would go unnoticed.
**Why accepted:** Testing it properly requires either exposing DockerBackend's internal `cfg.Timeout` or changing the resolver signature to return the timeout — both out of scope for this sprint (AC 02-02 fixes the signature). The auto-fix per-run budget is carried by `RunSpec.Timeout` at the dispatch site regardless, so a TimeoutSecs regression cannot silently shrink the validation budget.
**Fix in:** a future sprint — add an exported test accessor or a returned resolved-timeout value, then assert `120*time.Second` per AC 02-01 Edge Case 2.

## TD-002 — Zero-timeout effective-budget parity gap between host and sandbox paths (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-19
**File:** internal/verify/sandboxvalidate.go:57
**Issue:** The host path (`RunConfiguredValidation`) substitutes `defaultValidationTimeout` (2 min) when `timeout <= 0`; the sandbox path forwards `0` into `RunSpec.Timeout`, which defers to the backend's own default. So an unset `auto_fix.validate_timeout` yields a 2-min bound on the host path but a backend-defined bound on the sandbox path — an operator-visible default that differs by execution path.
**Why accepted:** AC 01-01 Scenario 3 explicitly mandates the adapter NOT default the timeout (RunConfiguredValidation stays the sole defaulter); both paths remain bounded (never unbounded). In production the sole call site passes `be.validateTimeout`, so the gap only manifests if that resolves to zero.
**Fix in:** Phase 4 — confirm `be.validateTimeout` is non-zero at the `cmd/atcr/autofix.go:252` call site (or Phase 5 docs the path-dependent default explicitly in `docs/auto-fix.md`).

## TD-003 — No real-backend fail-closed test for empty/relative dir delegation (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-19
**File:** internal/verify/sandboxvalidate_test.go
**Issue:** The adapter delegates empty/relative `dir` rejection to the backend's unexported `RunSpec.validate()`. Phase 2's fake `sandbox.Backend` does not replicate `validate()`, so no unit test proves an empty or relative `dir` actually fails closed through a real backend — only prose and the absolute-nonexistent-dir case are covered.
**Why accepted:** `RunSpec.validate()` is unexported, so a package-`verify` test cannot invoke it directly; a faithful test needs the real `DockerBackend`, which belongs in Phase 4's integration testing (against a fake docker shim), not Phase 2's pure unit layer.
**Fix in:** Phase 4 — add an integration test driving `dir=""` and a relative dir through a real `DockerBackend` (fake-docker shim) asserting the `RunSpec.validate()` StartError / `!Passed()` fail-closed outcome.

## TD-004 — Sandbox path has no ctx-level deadline backstop (defense-in-depth divergence) (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-19
**File:** internal/verify/sandboxvalidate.go:62
**Issue:** The host path enforces its timeout belt-and-suspenders (`context.WithTimeout` + process-group SIGKILL + `cmd.WaitDelay`) so an LLM-authored command can never stall `--auto-fix` indefinitely. The sandbox path passes `ctx` through unwrapped and relies solely on the backend honoring `RunSpec.Timeout`; there is no ctx-level backstop.
**Why accepted:** Deliberate — the container IS the enforcement boundary, and Preflight guarantees a working backend before dispatch. A naive `context.WithTimeout` backstop would misroute a genuine timeout (surfacing as a ctx-cancellation Go error from `Backend.Run`) into the StartError "cannot validate" branch instead of the `TimedOut` "validation failed" branch, regressing AC 01-02's routing. Documented at the call site.
**Fix in:** a future hardening sprint IF a backend proves to mishandle `RunSpec.Timeout` — would require a backstop that distinguishes ctx-timeout from a spawn fault before mapping to StartError vs TimedOut.
