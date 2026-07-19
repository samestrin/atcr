# Tech Debt Captured — Sprint 32.0 (Sandboxed Auto-Fix Validation)

Items deferred during `/execute-sprint`. Read by `/execute-code-review` and pre-seeded into its adversarial TD stream.

## TD-001 — TimeoutSecs override lacks direct test coverage (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-19
**File:** internal/verify/autofix_exec_test.go
**Issue:** `ResolveAutoFixSandbox` applies `sc.TimeoutSecs` to `cfg.Timeout`, but that value never appears in the `docker run` argv (it is a context deadline) and the resolver signature is pinned to `(sandbox.Backend, error)` by AC 02-02, so no test directly asserts the TimeoutSecs override reached the backend. A dropped `if sc.TimeoutSecs != nil` block would go unnoticed.
**Why accepted:** Testing it properly requires either exposing DockerBackend's internal `cfg.Timeout` or changing the resolver signature to return the timeout — both out of scope for this sprint (AC 02-02 fixes the signature). The auto-fix per-run budget is carried by `RunSpec.Timeout` at the dispatch site regardless, so a TimeoutSecs regression cannot silently shrink the validation budget.
**Fix in:** a future sprint — add an exported test accessor or a returned resolved-timeout value, then assert `120*time.Second` per AC 02-01 Edge Case 2.
