# Acceptance Criteria: Validation Result Capture and Reporting

**Related User Story:** [02: Configurable Local Validation](../user-stories/02-configurable-local-validation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct + function return value (`internal/verify`) | In-memory result type, no persistence layer |
| Test Framework | Go `testing` package, table-driven tests | Follows existing `internal/verify/*_test.go` conventions |
| Key Dependencies | `bytes.Buffer` or `strings.Builder` for stdout/stderr capture, `time` for duration | No new third-party dependencies |

### Related Files (from codebase-discovery.json)
- `internal/verify/localvalidate.go` - modify: define a `ValidationResult` struct (exit code, stdout, stderr, duration, pass/fail bool, error class) returned by the command runner from AC 02-01
- `internal/verify/localvalidate_test.go` - modify: unit tests asserting exit code, stdout, stderr, and duration are captured correctly for passing and failing commands
- `internal/autofix/` (new package, produced by Story 1/consumed across Stories 3-5) - reference: this result struct is the shape passed along the `internal/autofix` orchestration call chain per the story's Data Requirements
- `internal/verify/exec.go` - reference only: existing `ResolveExecBackend` result-handling conventions used as a pattern for a consistent result-object shape across `internal/verify`

## Happy Path Scenarios
**Scenario 1: Passing command captures full result**
- **Given** a validation command that exits `0` and writes to both stdout and stderr
- **When** the command completes
- **Then** the returned `ValidationResult` has `ExitCode == 0`, `Pass == true`, `Stdout` and `Stderr` populated with the exact captured output (exact up to the truncation cap defined in [AC 02-01](./02-01-configurable-validation-command-runner.md); the 02-01 truncation marker is what distinguishes a capped capture from a complete one), and a non-zero `Duration`

**Scenario 2: Failing command captures full result**
- **Given** a validation command that exits `1` with a compiler error on stderr
- **When** the command completes
- **Then** the returned `ValidationResult` has `ExitCode == 1`, `Pass == false`, and `Stderr` contains the captured compiler error text for diagnostics

**Scenario 3: Result is loggable/diagnosable without re-running the command**
- **Given** a completed validation run (pass or fail)
- **When** the caller logs or reports the `--auto-fix` outcome
- **Then** all diagnostic information (exit code, stdout, stderr, duration) is available directly from the returned struct with no need to re-invoke the command

## Edge Cases
**Edge Case 1: Command produces no output**
- **Given** a validation command that exits `0` with empty stdout and stderr
- **When** the command completes
- **Then** `ValidationResult.Stdout` and `Stderr` are empty strings (not nil-panic-prone types), and `Pass == true`

**Edge Case 2: Command killed by timeout mid-output**
- **Given** a command that is streaming output when the timeout cancels it
- **When** the context deadline is exceeded
- **Then** the partial stdout/stderr captured before cancellation is still included in the result, alongside a distinct `TimedOut == true` flag

**Edge Case 3: Non-UTF8 or binary output**
- **Given** a validation command that emits non-UTF8 bytes on stdout/stderr
- **When** output is captured into the result struct
- **Then** capture does not panic or corrupt the result; the raw captured bytes are preserved as-is in the `ValidationResult` struct (`Stdout`/`Stderr`), and sanitization for safe display happens ONLY at a display/reporting boundary — never mutating the stored bytes — with the runner never panicking on non-UTF8 and never crashing the `--auto-fix` flow

## Error Conditions
**Error Scenario 1: Result struct requested before command execution starts**
- Error message: N/A — API design: `ValidationResult` is only ever returned as the function's return value after execution attempt completes, never left in a partially-initialized state accessible to callers
- HTTP status / error code: N/A (Go struct, not an HTTP boundary)

**Error Scenario 2: Command-not-found error surfaces as a distinct field, not a generic exit code**
- Error message: `"validation command could not start: <underlying error>"` recorded in a dedicated `StartError error` field on the result (or a separate error return), not encoded as a fabricated exit code
- HTTP status / error code: N/A (Go `error`)

## Performance Requirements
- **Response Time:** Result construction adds negligible overhead (<10ms) beyond the underlying command's own execution/timeout time
- **Throughput:** N/A — single result per validation invocation

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** Captured stdout/stderr from the validation command is untrusted output (could echo file contents, secrets, etc.); callers that log or persist the result must treat it as potentially sensitive and not send it to remote services beyond local logs without existing ATCR redaction conventions

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven cases: passing command with output, failing command with output, empty-output command, timed-out command with partial output, command-not-found
**Mock/Stub Requirements:** Real short-lived shell commands (`printf`, `false`, `sleep`) as in AC 02-01; no mocking of the result struct itself since it is pure data

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `ValidationResult` captures exit code, stdout, stderr, and duration for both passing and failing runs
- [x] Timeout and command-not-found are represented as distinct fields/error classes, not folded into a generic non-zero exit code
- [x] Result struct is safe to construct and inspect even when output is empty, huge, or non-UTF8

**Manual Review:**
- [x] Code reviewed and approved
