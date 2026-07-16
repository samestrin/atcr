# Acceptance Criteria: Fire-and-Forget Telemetry Send

**Related User Story:** [01: Anonymous Usage Telemetry Ping](../user-stories/01-anonymous-usage-telemetry-ping.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/telemetry`, new) | Goroutine-based fire-and-forget HTTP client |
| Test Framework | `go test` (standard `testing`, `net/http/httptest`) | Test file: `internal/telemetry/client_test.go` |
| Key Dependencies | Go stdlib only: `net/http`, `encoding/json`, `context` | No new third-party dependency, per plan's "Recommended Packages" conclusion |

### Related Files (from codebase-discovery.json)
- `internal/telemetry/client.go` - create: defines the `Client` type, a `New(endpoint string) *Client` constructor, and a `Send(ctx context.Context, ev Event)` method that launches a goroutine to POST the JSON-marshaled `Event` payload to the configured HTTPS endpoint.
- `cmd/atcr/main.go` - modify: construct a package-level (or dependency-injected) `telemetry.Client` at `newRootCmd` time (`cmd/atcr/main.go:217`), following the existing `logLevelFromEnv`/`LOG_LEVEL` read pattern for any config needed at startup.
- `cmd/atcr/review.go` - modify: invoke `telemetry.Client.Send` from `runReview` (`cmd/atcr/review.go:170`, alongside `writeAuditRecord`) on command completion.
- `cmd/atcr/reconcile.go` - modify: invoke `telemetry.Client.Send` from `runReconcile` (`cmd/atcr/reconcile.go:71`, alongside `scorecard.EmitForReconcile`) on command completion.

## Happy Path Scenarios
**Scenario 1: Successful ping on review completion**
- **Given** `atcr review` completes a run (success or failure outcome) against a reachable telemetry endpoint
- **When** `runReview` reaches its completion point
- **Then** `internal/telemetry.Client.Send` is invoked with an `Event{Event: "review_run", Lang: <detected lang>, Lines: <line count>, Status: "success"|"failure"}` payload, launched in a goroutine, and the HTTP POST is sent as `application/json` over HTTPS to the configured endpoint

**Scenario 2: Successful ping on reconcile completion**
- **Given** `atcr reconcile` completes a run against a reachable telemetry endpoint
- **When** `runReconcile` reaches its completion point (alongside `scorecard.EmitForReconcile` at `cmd/atcr/reconcile.go:148`)
- **Then** `internal/telemetry.Client.Send` is invoked with the equivalent `Event` payload for the reconcile outcome

**Scenario 3: Client constructed once at startup**
- **Given** `atcr` starts and `newRootCmd` runs
- **When** the root command is constructed
- **Then** a single `telemetry.Client` instance is created (mirroring the `LOG_LEVEL`-read pattern at `cmd/atcr/main.go:217`) and made available to both `runReview` and `runReconcile` without re-constructing per invocation

## Edge Cases
**Edge Case 1: Send called with a zero-value or empty endpoint**
- **Given** the telemetry endpoint is unset or empty (e.g. not yet configured by an operator)
- **When** `Send` is invoked
- **Then** the client no-ops without error (does not panic, does not attempt a request to an invalid URL) — this is the seam Story 2's opt-out mode will reuse

**Edge Case 2: Concurrent invocations from review and reconcile in the same process**
- **Given** the CLI process invokes both `runReview`-style and `runReconcile`-style completions in rapid succession (e.g. scripted usage)
- **When** `Send` is called multiple times
- **Then** each call runs in its own independent goroutine with no shared mutable state that causes a data race (verified via `go test -race`)

## Error Conditions
**Error Scenario 1: Endpoint returns a non-2xx HTTP status**
- **Given** the telemetry endpoint responds with `500 Internal Server Error`
- **When** the goroutine's POST completes
- **Then** the client logs the failure at debug/trace level and returns without affecting `runReview`/`runReconcile`'s return value or exit code
- HTTP status / error code: any non-2xx response is treated as a logged, swallowed failure (no error propagated to the caller)

**Error Scenario 2: JSON marshal failure**
- **Given** an `Event` value that somehow fails to marshal (defensive case)
- **When** `Send` attempts `json.Marshal`
- **Then** the error is logged and the goroutine returns without sending a request or panicking

## Performance Requirements
- **Response Time:** `Send` itself (the call from `runReview`/`runReconcile`) returns in effectively constant time (goroutine dispatch only, no blocking I/O on the calling path).
- **Throughput:** Single ping per command completion; no batching or queuing required for this story.

## Security Considerations
- **Authentication/Authorization:** No credentials are required or transmitted; the endpoint is a public ingestion URL. HTTPS is mandatory (no plaintext HTTP fallback) to protect payload integrity in transit.
- **Input Validation:** The endpoint URL is validated as HTTPS before use; `lang`/`status` values come from a closed set already computed internally (not user-supplied free text) so no injection surface exists in the payload.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Sample `Event` values covering both `status: "success"` and `status: "failure"`; a variety of `lang` strings already used elsewhere in the codebase (e.g. "go", "python").
**Mock/Stub Requirements:** `net/http/httptest.NewServer` to stand in for the telemetry endpoint and assert the received request's method, content-type, and body; no external network calls in tests.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/telemetry.Client.Send` POSTs the `{event, lang, lines, status}` payload over HTTPS from a goroutine
- [ ] `runReview` and `runReconcile` both invoke `Send` on completion alongside their existing non-fatal side effects
- [ ] A single `Client` instance is constructed once at `newRootCmd` time
- [ ] An empty/unset endpoint no-ops safely

**Manual Review:**
- [ ] Code reviewed and approved
