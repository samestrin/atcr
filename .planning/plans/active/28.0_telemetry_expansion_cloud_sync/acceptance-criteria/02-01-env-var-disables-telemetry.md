# Acceptance Criteria: `ATCR_TELEMETRY=0` Disables Telemetry Process-Wide

**Related User Story:** [02: Telemetry Opt-Out](../user-stories/02-telemetry-opt-out.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI root command) | `cmd/atcr` |
| Test Framework | `go test` (standard library `testing`), `testify` `assert`/`require` | Mirrors existing `main_test.go` / `docs_audit_test.go` conventions |
| Key Dependencies | `strconv` (`ParseBool`), `os` (`Getenv`), `github.com/spf13/cobra` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` - modify: add `telemetryEnabledFromEnv() bool` beside `logLevelFromEnv` (`cmd/atcr/main.go:216`), read once at `newRootCmd` construction time (`cmd/atcr/main.go:128-210`), and thread the resolved bool into the telemetry client constructor used by `review`/`reconcile`.
- `internal/telemetry/client.go` - modify (Story 1 client): add a construction-time `enabled bool` (or equivalent) seam so a disabled client is a true no-op — no goroutine spawned, no HTTP client allocated.
- `cmd/atcr/main_test.go` - modify: add `TestTelemetryEnabledFromEnv` table test covering unset/true/false/invalid values.
- `cmd/atcr/review_test.go`, `cmd/atcr/reconcile_test.go` - modify: add a test asserting zero HTTP requests to a mock telemetry endpoint when `ATCR_TELEMETRY=0` is set, for both `review` (`cmd/atcr/review.go:170`) and `reconcile` (`cmd/atcr/reconcile.go:71`).

## Happy Path Scenarios
**Scenario 1: env var set to `0` disables telemetry for `review`**
- **Given** `ATCR_TELEMETRY=0` is set in the process environment
- **When** `atcr review` runs to completion against a mock telemetry endpoint
- **Then** zero HTTP requests are observed on the mock endpoint, and no telemetry goroutine is scheduled

**Scenario 2: env var set to `0` disables telemetry for `reconcile`**
- **Given** `ATCR_TELEMETRY=0` is set in the process environment
- **When** `atcr reconcile` runs to completion against a mock telemetry endpoint
- **Then** zero HTTP requests are observed on the mock endpoint

**Scenario 3: env var unset leaves telemetry enabled**
- **Given** `ATCR_TELEMETRY` is not set in the process environment
- **When** `atcr review` runs
- **Then** `telemetryEnabledFromEnv()` returns `true` and the Story 1 client is constructed in its normal (enabled) mode

## Edge Cases
**Edge Case 1: recognized falsy equivalents**
- **Given** `ATCR_TELEMETRY` is set to any of `false`, `f`, `F`, `False`, `FALSE` (the `strconv.ParseBool` falsy set)
- **When** `telemetryEnabledFromEnv()` is called
- **Then** it returns `false` for every listed value

**Edge Case 2: unparseable value defaults to enabled**
- **Given** `ATCR_TELEMETRY` is set to a non-boolean string (e.g. `maybe`, `disabled`, empty string after whitespace trim)
- **When** `telemetryEnabledFromEnv()` is called
- **Then** it returns `true` (fails open toward the documented default), matching the "unset or unparseable defaults to enabled" contract in the story's Implementation Notes

**Edge Case 3: read once at root-command construction, not per-subcommand**
- **Given** `ATCR_TELEMETRY=0` is set before `newRootCmd()` is called
- **When** any subcommand (`review`, `reconcile`, or any other) executes under that root command instance
- **Then** the resolved disabled state is consistent across all subcommands in that process — no subcommand re-reads the env var and reaches a different answer

## Error Conditions
**Error Scenario 1: N/A — env var parsing never errors**
- `telemetryEnabledFromEnv()` has no error return; an invalid value is treated as the documented "unparseable defaults to enabled" case, not a usage error. This intentionally differs from flag validation (e.g. `--log-format`), which does return a `usageError`.
- HTTP status / error code: not applicable (no user-facing error surface for this AC)

## Performance Requirements
- **Response Time:** The env var read and bool parse add no measurable latency (single `os.Getenv` + `strconv.ParseBool` call, O(1)); must not introduce a per-command retry or blocking I/O.
- **Throughput:** N/A — this AC only gates whether a telemetry goroutine is scheduled; it does not affect review/reconcile throughput.
- **Strictness requirement:** When disabled, no goroutine is spawned and no HTTP payload is allocated — verified by a test asserting absence of allocation/goroutine scheduling, not merely absence of an observed network call (per the story's first Potential Risk).

## Security Considerations
- **Authentication/Authorization:** N/A — this is a local, unauthenticated boolean gate; no credentials are involved.
- **Input Validation:** `ATCR_TELEMETRY` is parsed strictly via `strconv.ParseBool`; any value outside its accepted set is treated as the safe default (enabled) rather than causing undefined behavior or a panic. No injection surface — the value is never passed to a shell or interpolated into a command.

## Test Implementation Guidance
**Test Type:** UNIT (env var parsing) + INTEGRATION (zero-HTTP-call assertion against a mock telemetry endpoint)
**Test Data Requirements:** Table of `ATCR_TELEMETRY` values (`""`, `"0"`, `"1"`, `"true"`, `"false"`, `"f"`, `"F"`, `"maybe"`, unset) mapped to expected boolean outcomes; a `httptest.Server` acting as the mock telemetry endpoint with a request counter.
**Mock/Stub Requirements:** `httptest.NewServer` for the telemetry endpoint; `t.Setenv("ATCR_TELEMETRY", ...)` for isolated, parallel-safe env manipulation (no global env mutation leaking between tests).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `telemetryEnabledFromEnv()` exists in `cmd/atcr/main.go`, read once in `newRootCmd`
- [ ] `ATCR_TELEMETRY=0` (and `false`/`f`/`F`/`False`/`FALSE`) results in zero HTTP requests during `review` and `reconcile`, verified via mock endpoint
- [ ] Unset or unparseable values default to enabled (`true`)
- [ ] No goroutine is spawned and no HTTP payload allocated when disabled (construction-time short-circuit, not post-hoc suppression)

**Manual Review:**
- [ ] Code reviewed and approved
