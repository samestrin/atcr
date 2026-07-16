# Acceptance Criteria: Panic-Safe, Fail-Open Behavior

**Related User Story:** [01: Anonymous Usage Telemetry Ping](../user-stories/01-anonymous-usage-telemetry-ping.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/telemetry`) | `defer recover()` inside the goroutine body per implementation-standards.md "Panic Safety" |
| Test Framework | `go test` (standard `testing`) | Test file: `internal/telemetry/client_test.go` |
| Key Dependencies | Go stdlib (`recover`, `log`/`internal/log`) | Reuses the project's existing `internal/log` logger for debug/trace-level failure logging |

### Related Files (from codebase-discovery.json)
- `internal/telemetry/client.go` - create: `Send`'s goroutine body opens with `defer func() { if r := recover(); r != nil { ... log and swallow ... } }()` per implementation-standards.md's Panic Safety / Defer Cleanup guidance (`.planning/specifications/implementation-standards.md:66-67`).
- `internal/telemetry/client_test.go` - create: a test that forces an internal panic (e.g. via an injectable hook or a deliberately malformed internal step) and asserts the parent test/process does not crash and the calling command still returns normally.
- `cmd/atcr/review.go` - modify: `runReview`'s completion-point telemetry call (`cmd/atcr/review.go:170`) must never propagate a panic or error that changes `runReview`'s return value.
- `cmd/atcr/reconcile.go` - modify: `runReconcile`'s completion-point telemetry call (`cmd/atcr/reconcile.go:71`) must never propagate a panic or error that changes `runReconcile`'s return value.

## Happy Path Scenarios
**Scenario 1: Network failure is logged and swallowed**
- **Given** the telemetry endpoint is unreachable
- **When** the goroutine's HTTP request fails
- **Then** the failure is logged at debug/trace level via `internal/log` (not `Warn`/`Error`, to avoid alarming end users about an opt-in background feature) and the goroutine returns cleanly with no error surfaced to `runReview`/`runReconcile`

**Scenario 2: Internal panic is recovered**
- **Given** an internal error occurs inside the telemetry goroutine (e.g. a forced nil-pointer dereference in a test double)
- **When** the panic is raised
- **Then** the `defer recover()` catches it, logs the recovered value at debug/trace level, and the goroutine exits without terminating the process or the parent command

## Edge Cases
**Edge Case 1: Panic during JSON marshaling of a malformed Event**
- **Given** an `Event` value that cannot be marshaled cleanly (defensive/injected test case)
- **When** `Send`'s internal marshal step is reached
- **Then** any resulting panic is recovered by the same `defer recover()` and does not propagate

**Edge Case 2: Multiple concurrent telemetry goroutines, one panics**
- **Given** both `runReview` and `runReconcile` fire telemetry pings in the same test run (e.g. sequential CLI invocations within a test binary)
- **When** one goroutine panics internally
- **Then** the panic is isolated to that goroutine (Go's per-goroutine recover semantics) and does not affect the other in-flight telemetry goroutine or either parent command

## Error Conditions
**Error Scenario 1: Recovered panic never crashes the CLI process**
- **Given** a forced internal panic inside the telemetry goroutine
- **When** the test asserts on the parent command's behavior
- **Then** the parent command (`runReview`/`runReconcile`) completes and returns its normal result; the test process itself does not exit or report an unrecovered panic
- Error message: recovered value is logged with a message such as `"telemetry: recovered from panic: %v"` at debug/trace level

**Error Scenario 2: No error value ever returned to the caller from telemetry**
- **Given** any internal telemetry failure mode (network, marshal, panic)
- **When** `Send` is called from `runReview`/`runReconcile`
- **Then** `Send` returns no error (its signature has no error return) — confirmed by the call sites in `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` treating telemetry purely as a side effect, consistent with `scorecard.EmitForReconcile`'s existing pattern

## Performance Requirements
- **Response Time:** `recover()` overhead is negligible (nanoseconds); no measurable impact on goroutine execution time.
- **Throughput:** N/A — panic recovery is a correctness/safety property, not a throughput concern.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** Recovered panic values are logged as opaque `%v` output only — never re-serialized into the telemetry payload itself, so a panic cannot leak internal state through the telemetry channel.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** An injectable seam (e.g. a package-level function variable for the HTTP send step, swappable in tests) that can be made to panic on demand.
**Mock/Stub Requirements:** Use a test double or function variable to force a panic inside the code path `Send`'s goroutine executes, then assert via `recover()` in the test harness (or simply that the test does not fail with an unrecovered panic) that the parent test function completes normally.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Goroutine body wraps its work in `defer recover()` per implementation-standards.md guidance
- [x] A test forcing an internal panic proves the parent command/process does not crash
- [x] All telemetry failure modes (network, marshal, panic) are logged at debug/trace level, never at a level that alarms the end user
- [x] No telemetry failure mode changes `runReview`/`runReconcile`'s return value or exit code

**Manual Review:**
- [ ] Code reviewed and approved
