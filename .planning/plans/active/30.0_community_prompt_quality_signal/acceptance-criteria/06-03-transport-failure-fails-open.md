# Acceptance Criteria: Transport Failure Fails Open — Run Outcome and Exit Code Unchanged

**Related User Story:** [06: Gated Quality-Signal Transmission via the Epic 28.0 Transport](../user-stories/06-gated-quality-signal-transmission.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI run-path wiring + transport) | `cmd/atcr`, `internal/telemetry` (or `internal/scorecard` per the design-sprint fork) |
| Test Framework | `go test` (standard library `testing`), `testify` `assert`/`require` | Mirrors `internal/telemetry/client_test.go` fail-open tests |
| Key Dependencies | `net/http/httptest` (controllable failure server) | No new third-party dependency |

## Related Files
- `internal/telemetry/client.go` - reference: documented fail-open contract ("a non-2xx response, or an internal panic never blocks, crashes, or" alters the run — `:3-5`), detached send (`:100-106`).
- `cmd/atcr/cloudsync.go` - reference: `resolveSyncCloudOutcome` (`:86`) exit-code mapping that keeps a push failure from corrupting the run outcome (extend-payload fork).
- `cmd/atcr/review.go` / `cmd/atcr/reconcile.go` - modify: Story 6's call sites must inherit this fail-open behavior.
- `cmd/atcr/qualitysignal_send_test.go` - create: failure-matrix tests (non-2xx, DNS failure, timeout, panic) asserting run-outcome invariance.

### Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go` / `cmd/atcr/reconcile.go` - update: Story 6 call sites inherit the fail-open contract (`internal/telemetry/client.go:3-5`, `:106`; `cmd/atcr/cloudsync.go:86` `resolveSyncCloudOutcome` for the `Push` fork)
- `cmd/atcr/qualitysignal_send_test.go` - create: failure-matrix tests (500, DNS failure, timeout, panic)

## Happy Path Scenarios
**Scenario 1: endpoint returns HTTP 500 — run succeeds identically**
- **Given** the gate is enabled and the endpoint responds 500 to every request
- **When** a review run completes
- **Then** the run's exit code and stdout are byte-identical to the same run with the gate disabled

**Scenario 2: endpoint unreachable (DNS/connection refused) — run succeeds identically**
- **Given** the gate is enabled and the endpoint host does not resolve / refuses connections
- **When** a review or reconcile run completes
- **Then** the run's exit code and stdout match the gate-disabled baseline, and the run does not block on connection retries beyond the transport's existing behavior

**Scenario 3: slow endpoint exceeding the request timeout — run does not hang**
- **Given** the gate is enabled and the endpoint accepts connections but never responds
- **When** a run completes
- **Then** the run finishes within its normal time envelope — the send is bounded by the transport's existing `requestTimeout` and never gates run completion

## Edge Cases
**Edge Case 1: panic inside the send path**
- **Given** the gate is enabled and a fault is injected into the send/goroutine path
- **When** a run completes
- **Then** the panic is contained per `client.go`'s documented contract — the run's outcome is unaffected (mirroring the existing client panic-safety test pattern)

**Edge Case 2: auth rejection on the `Push` fork**
- **Given** the design-sprint fork resolved to the cloud-sync transport, the gate is enabled, and the endpoint answers 401/403
- **When** a run completes
- **Then** the rejection surfaces only through the existing `resolveSyncCloudOutcome` mapping (a visible sync error, never a corrupted run outcome) — no new error-mapping logic is introduced by this epic

**Edge Case 3: failure on one run does not suppress or duplicate the next run's send**
- **Given** a first run whose send fails (500) and a second run against a healthy endpoint
- **When** both complete
- **Then** the second run sends exactly once — there is no circuit-breaker or retry state carried across runs (the existing transports hold no such state; none is added)

## Error Conditions
**Error Scenario 1: all transport errors are absorbed by the fail-open path**
- Non-2xx, network unreachable, DNS failure, timeout, TLS failure, and send-path panic all resolve to the same outcome: the quality-signal send is dropped, at most a one-line diagnostic is emitted (matching the transport's existing logging posture), and the run result is never altered.
- HTTP status / error code: no quality-signal transport error may surface as `usageError` (exit 2) or a run failure (exit 1) — this mirrors the passive ping's contract, where telemetry can never fail a review.

## Performance Requirements
- **Response Time:** Run completion never awaits the send; the failure modes above add zero wall-clock time beyond the transport's existing bounded timeout on its detached path.
- **Throughput:** A failing endpoint does not cause retries, queue growth, or goroutine accumulation across runs.
- **Strictness requirement:** The failure-matrix tests assert wall-clock invariance within a generous bound (e.g. the timeout scenario completes in well under the transport timeout plus normal run time) to catch accidental synchronous sends.

## Security Considerations
- **Authentication/Authorization:** N/A — failure handling neither weakens nor exercises the transport's auth path beyond its existing behavior.
- **Input Validation:** N/A — failure handling introduces no new input surface.
- **Privacy Guarantee:** Fail-open must never become fail-closed-then-retry-with-different-payload: a failed send is dropped, not mutated and re-attempted. Error diagnostics on this path never include finding content (the payload carries none by construction) and never log the payload body at default verbosity.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** An `httptest` server configurable to return 500, hang past the timeout, or be shut down mid-test (connection refused); a DNS-unreachable endpoint constant; `t.TempDir()` + `t.Setenv` for the enabled-gate axis.
**Mock/Stub Requirements:** Controllable failure server and, for the panic case, a fault-injection seam on the send path (mirroring the existing client tests); the run path under test stays real.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] 500 / DNS failure / timeout / panic each leave the run's exit code and stdout identical to the gate-disabled baseline
- [ ] The timeout scenario completes without blocking run completion
- [ ] No retry, queue, or circuit-breaker state is carried across runs
- [ ] Failure diagnostics never include the payload body at default verbosity

**Manual Review:**
- [ ] Code reviewed and approved
