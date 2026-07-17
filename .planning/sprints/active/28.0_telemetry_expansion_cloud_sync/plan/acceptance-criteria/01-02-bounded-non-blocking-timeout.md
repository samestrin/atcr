# Acceptance Criteria: Bounded, Non-Blocking Timeout

**Related User Story:** [01: Anonymous Usage Telemetry Ping](../user-stories/01-anonymous-usage-telemetry-ping.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/telemetry`) | `context.WithTimeout` bounding an `http.Client` request |
| Test Framework | `go test` (standard `testing`, `net/http/httptest`) | Test file: `internal/telemetry/client_test.go` |
| Key Dependencies | `context`, `net/http`, `time` (stdlib only) | 2-3 second bounded timeout per implementation-standards.md guidance |

### Related Files (from codebase-discovery.json)
- `internal/telemetry/client.go` - create: `Send` derives a bounded `context.Context` (e.g. `context.WithTimeout(ctx, 3*time.Second)`) for the outbound request; the goroutine itself is fire-and-forget so the timeout only bounds the background request's own lifetime, never the caller.
- `cmd/atcr/review.go` - modify: `runReview`'s call to the telemetry send at its completion point (`cmd/atcr/review.go:170`) must return immediately, not await the goroutine.
- `cmd/atcr/reconcile.go` - modify: `runReconcile`'s call to the telemetry send at its completion point (`cmd/atcr/reconcile.go:71`) must return immediately, not await the goroutine.
- `internal/telemetry/client_test.go` - create: tests simulating a hung/unreachable endpoint and asserting bounded goroutine lifetime plus prompt caller return.

## Happy Path Scenarios
**Scenario 1: Fast endpoint response does not delay completion**
- **Given** a telemetry endpoint that responds within milliseconds
- **When** `runReview` or `runReconcile` invokes the telemetry send at command completion
- **Then** the command function returns to its caller without waiting on the telemetry goroutine, and the eventual HTTP response (success or failure) has no effect on the command's return value

**Scenario 2: Timeout bounds the background request**
- **Given** the telemetry goroutine's HTTP request is in flight
- **When** the configured timeout (2-3 seconds) elapses without a response
- **Then** the request context is canceled, the underlying `http.Client` call returns a context-deadline error, and the goroutine logs and exits cleanly

## Edge Cases
**Edge Case 1: Endpoint accepts the TCP connection but never responds (hung connection)**
- **Given** a test server that accepts the connection and then never writes a response
- **When** `Send` is invoked and `runReview`/`runReconcile` proceeds to return
- **Then** the calling test asserts the command-level function call returns within a small bound (e.g. under 100ms, well under the telemetry timeout) — proving the hang is fully isolated to the background goroutine

**Edge Case 2: DNS resolution failure or connection refused**
- **Given** the configured endpoint is unreachable (connection refused or unresolvable host)
- **When** `Send` is invoked
- **Then** the goroutine's HTTP call fails immediately (or within the timeout), is logged, and does not block or affect the caller

## Error Conditions
**Error Scenario 1: Command exit code unaffected by a hung/failed telemetry call**
- **Given** a telemetry endpoint that hangs indefinitely
- **When** `runReview` or `runReconcile` completes its own work (independent of telemetry) and returns
- **Then** the command's exit code reflects only the review/reconcile outcome (`exitFailure=1`, `exitUsage=2`, or `nil`/success per `cmd/atcr/main.go`'s `codedError` convention) — never a telemetry-induced code or delay
- HTTP status / error code: N/A at the CLI level — telemetry failures never surface as CLI errors

## Performance Requirements
- **Response Time:** The telemetry send call adds no measurable latency (target: <5ms overhead) to `runReview`/`runReconcile`'s own completion path, verified via a test with an artificially slow/hung endpoint.
- **Throughput:** Telemetry request itself is bounded to a single attempt within a 2-3 second window (per implementation-standards.md guidance); no retries within this story's scope.

## Security Considerations
- **Authentication/Authorization:** N/A (no credentials transmitted).
- **Input Validation:** The timeout duration is a fixed internal constant, not user-configurable input, eliminating a DoS-via-config vector for this story.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** An `httptest.Server` handler that sleeps longer than the configured timeout to simulate a hung endpoint; a second handler using `http.Hijacker` or a raw `net.Listener` that accepts and never responds, to simulate a true network hang distinct from a slow-but-alive server.
**Mock/Stub Requirements:** Use `time.Since`/`context.WithTimeout` assertions in tests (e.g. wrap the call to `runReview`/`Send` with a wall-clock measurement and assert it completes well under the telemetry timeout); no real network access.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `Send` derives a bounded context (2-3s) for its outbound request
- [ ] A test with a hung/unreachable endpoint proves `runReview`/`runReconcile` returns promptly (not blocked)
- [ ] A test proves the command's exit code is unaffected by telemetry timeout or failure
- [ ] Timeout elapsing cancels the in-flight request and the goroutine exits cleanly

**Manual Review:**
- [ ] Code reviewed and approved
