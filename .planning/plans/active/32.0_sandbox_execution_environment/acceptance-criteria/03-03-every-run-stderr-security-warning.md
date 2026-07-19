# Acceptance Criteria: Every-Run (Non-Memoized) stderr Security Warning

**Related User Story:** [03: `--no-sandbox` Opt-Out Flag with CLI Security Warnings](../user-stories/03-no-sandbox-opt-out-flag.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Dedicated helper function (e.g. `warnNoSandbox(out io.Writer)`) called unconditionally on every `--no-sandbox` code path — explicitly NOT modeled on `telemetryEnabledFromEnv`'s read-once memoization (`cmd/atcr/main.go:337-352`, warning at line 348) | The story's Implementation Notes name this divergence explicitly: that precedent is a one-time warning by design; this one must not be |
| Test Framework | `go test`, stderr capture via an injected `io.Writer` (e.g. `bytes.Buffer` in place of `os.Stderr`) | Matches the existing pattern in `cmd/atcr/main.go` where `axiMaxLinesFromEnv(w io.Writer)` takes a writer for testability (line 378) |
| Key Dependencies | `fmt.Fprintf`/`fmt.Fprintln` against `cmd.ErrOrStderr()` or an equivalent injected writer; no new package | No memoization primitive (no `sync.Once`, no package-level "seen" bool, no env/state file) may be introduced for this warning |

## Related Files
- `cmd/atcr/autofix.go` - modify: add a small dedicated helper (e.g. `warnNoSandbox(out io.Writer)`) that writes a clearly-marked security warning to stderr; call it unconditionally at the top of every `--no-sandbox` code path — both inside `validateAutoFixBackend` (so it fires as soon as the bypass is chosen) and/or immediately before `runAutoFix`'s validation call, whichever call site is reachable on every `--no-sandbox`-set invocation without being skipped by an early return elsewhere
- `cmd/atcr/autofix_test.go` - modify/create: a test that invokes the `--no-sandbox` path twice in the same process (two consecutive calls, not two separate process invocations) and asserts the warning string appears in captured stderr output both times, proving no memoization
- `cmd/atcr/main.go` - reference only: `telemetryEnabledFromEnv` (line 337, warning at line 348) is the explicit anti-pattern this AC must NOT replicate — cited so a reviewer can confirm the divergence is intentional and documented in a code comment, not accidental

## Happy Path Scenarios
**Scenario 1: Warning prints once per single `--no-sandbox` invocation**
- **Given** `atcr review --auto-fix --no-sandbox` runs once against a captured stderr buffer
- **When** the run reaches the `--no-sandbox` bypass path
- **Then** stderr contains a clearly-marked warning (e.g. prefixed `WARNING:` or similar) stating that validation is running WITHOUT container isolation, printed before the validation command executes, and printed to stderr — never stdout

**Scenario 2: Warning prints on EVERY consecutive invocation in the same process (the non-negotiable behavior)**
- **Given** the same test process calls the `--no-sandbox` code path (e.g. `validateAutoFixBackend` + `runAutoFix`, or the warning helper's call site) twice in sequence, each against its own fresh stderr buffer
- **When** both calls complete
- **Then** BOTH captured buffers contain the warning string — proving the warning is not gated behind a package-level "already warned" flag, a `sync.Once`, an env var check, or a state file; a THIRD consecutive call in the same test is added for extra confidence and must also show the warning, ruling out any off-by-one memoization bug

**Scenario 3: Warning text names the specific risk**
- **Given** the warning helper's output
- **When** the test inspects the warning string
- **Then** it explicitly states that validation is running "WITHOUT container isolation" (or materially equivalent wording) against untrusted/LLM-generated code — matching the story's Constraint that the warning must be substantively more explicit than a generic "using --no-sandbox" notice

## Edge Cases
**Edge Case 1: Warning fires even when validation subsequently passes**
- **Given** `--no-sandbox` is set and the direct `os/exec` validation command exits 0 (success)
- **When** the run completes and opens a pull request
- **Then** the warning still appears in stderr — it is unconditional on the bypass being taken, not conditional on the validation outcome

**Edge Case 2: Warning fires even when validation subsequently fails**
- **Given** `--no-sandbox` is set and the validation command exits non-zero
- **When** `runAutoFix` reverts the patch and returns its failure error
- **Then** the warning still appeared in stderr before the failed validation ran — the warning is not suppressed or retroactively hidden by a later failure

**Edge Case 3: `--no-sandbox` absent — no warning at all**
- **Given** `atcr review --auto-fix` without `--no-sandbox` (Story 1/2's default sandboxed path)
- **When** the run completes
- **Then** stderr contains NO occurrence of the `--no-sandbox` warning string — the warning is strictly conditional on the flag being true, never printed on the default path (regression guard against an inverted condition)

**Edge Case 4: Two back-to-back CI-style runs in separate process invocations**
- **Given** two entirely separate `atcr` process invocations (not just two in-process calls), each with `--no-sandbox` set, simulating consecutive CI job runs
- **When** both processes' stderr logs are inspected
- **Then** both show the warning — since there is no memoization mechanism, cross-process persistence is a non-issue by construction, but this scenario documents that the guarantee holds at the process boundary too (an E2E-level sanity check, not just the in-process unit test)

## Error Conditions
**Error Scenario 1: A memoization regression is introduced (mutation-style negative test)**
- **Given** a hypothetical/regression build where the warning helper is wrapped in `sync.Once` or a package-level bool (the explicit anti-pattern being guarded against)
- **When** the two-consecutive-calls test (Scenario 2) runs
- **Then** the test FAILS on the second call's missing warning — this is the load-bearing regression test for the story's single hard constraint; it must be written to actually catch this class of bug, not merely assert the happy path once
- Error message: N/A (this is a test-suite guard, not a runtime error condition)

## Performance Requirements
- **Response Time:** Warning print is a single buffered `Fprintf`/`Fprintln` call; negligible (<1ms), no measurable effect on run time
- **Throughput:** N/A — one warning write per `--no-sandbox` invocation, unbounded/no rate-limiting by design (the whole point is that it is NOT throttled or deduplicated)

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** N/A — the warning is static/templated text, not derived from user input, so no injection surface; must route to stderr specifically (not stdout) so it is not mistaken for structured/piped output (e.g. does not corrupt `--axi` TOON payloads on stdout, per `cmd/atcr/review.go:90`'s stdout/stderr separation convention)

## Test Implementation Guidance
**Test Type:** UNIT (in-process, two/three consecutive calls against fresh buffers) plus one lightweight E2E-flavored subprocess test for Edge Case 4 if the existing test suite has a subprocess-invocation harness already (check `cmd/atcr` for existing `exec.Command`-based CLI tests before adding a new harness)
**Test Data Requirements:** An `io.Writer` (`bytes.Buffer`) substituted for `os.Stderr`/`cmd.ErrOrStderr()`; no filesystem or network fixtures needed beyond what Scenario 1/2 minimally require to reach the `--no-sandbox` code path (can stub the validation command as `["true"]`)
**Mock/Stub Requirements:** None beyond the writer substitution — this AC specifically must NOT mock away the memoization check by testing the helper function in isolation only; at least one test must exercise it through two full consecutive calls to the actual call site(s) in `validateAutoFixBackend`/`runAutoFix`, not just two calls to the bare `warnNoSandbox` helper (a bare-helper-only test would pass even if a caller wrongly added memoization around the call site)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Warning prints to stderr (never stdout) before validation executes, on every `--no-sandbox`-set invocation
- [ ] Warning prints identically on a second and third consecutive in-process invocation — no `sync.Once`, no package-level "seen" flag, no env/state-file gate
- [ ] Warning text explicitly names "WITHOUT container isolation" (or materially equivalent) run against untrusted/LLM-generated code
- [ ] No warning appears at all when `--no-sandbox` is absent or explicitly `false`
- [ ] A regression-style test proves a `sync.Once`/memoized implementation would fail the suite (i.e., the test genuinely exercises repetition, not just single-call presence)

**Manual Review:**
- [ ] Code reviewed and approved
