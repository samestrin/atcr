# Acceptance Criteria: Gate-Disabled Short-Circuit — No Payload Built, No Network Call

**Related User Story:** [06: Gated Quality-Signal Transmission via the Epic 28.0 Transport](../user-stories/06-gated-quality-signal-transmission.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI run-path wiring) | `cmd/atcr` |
| Test Framework | `go test` (standard library `testing`), `testify` `assert`/`require` | Mirrors `cmd/atcr/telemetry_gate_test.go` and `internal/telemetry/client_test.go` conventions |
| Key Dependencies | `net/http/httptest` (request-counting test server), Story 2's `qualitySignalGate()` | No new third-party dependency |

## Related Files
- `cmd/atcr/review.go` - modify: add the quality-signal call site adjacent to the passive-ping emission (`:462-467`), gated by `qualitySignalGate()` before any payload construction.
- `cmd/atcr/reconcile.go` - modify: same call-site pattern adjacent to the passive-ping emission (`:186-191`).
- `cmd/atcr/qualitysignal.go` - reference: Story 2's gate and (if colocated) Story 6's send-path helper.
- `cmd/atcr/qualitysignal_send_test.go` - create: tests asserting the disabled gate yields zero HTTP requests and no payload construction on both the review and reconcile run paths.

### Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go` - update: quality-signal call site adjacent to the passive-ping emission (`:462-467`), gated by `qualitySignalGate()` before any payload construction
- `cmd/atcr/reconcile.go` - update: same call-site pattern adjacent to `:186-191`
- `cmd/atcr/qualitysignal_send_test.go` - create: zero-request / no-payload-construction assertions on both run paths

## Happy Path Scenarios
**Scenario 1: gate disabled (no env var, no persisted config) — review run attempts nothing**
- **Given** no quality-signal env var is set and no `.atcr/config.yaml` `quality_signal` key exists (gate resolves disabled per AC 02-01)
- **And** a request-counting `httptest` server stands in for the telemetry endpoint
- **When** a review run completes
- **Then** the test server observes exactly zero requests
- **And** no quality-signal payload struct is constructed (observable via the payload-constructor seam, not inferred from the absence of a request alone)

**Scenario 2: gate explicitly disabled (`quality_signal: false` persisted) — reconcile run attempts nothing**
- **Given** `.atcr/config.yaml` contains `quality_signal: false` and no quality-signal env var is set
- **And** a request-counting `httptest` server stands in for the endpoint
- **When** a reconcile run completes
- **Then** the test server observes exactly zero requests

**Scenario 3: unrelated telemetry surfaces enabled — quality-signal path still dark**
- **Given** `ATCR_TELEMETRY` is unset (passive ping enabled by its own opt-out default) and/or `--sync-cloud` preconditions are met
- **And** the quality-signal gate resolves disabled
- **When** a review/reconcile run completes
- **Then** the passive ping / cloud-sync behavior is unchanged, and the quality-signal payload is neither built nor sent — independence from `telemetryGate()`/`resolveSyncCloud()` per Story 2

## Edge Cases
**Edge Case 1: endpoint configured but gate disabled**
- **Given** a valid telemetry/cloud endpoint is configured and reachable
- **And** the quality-signal gate resolves disabled
- **When** a run completes
- **Then** still zero quality-signal requests — endpoint availability must not bypass consent

**Edge Case 2: `--preview` set while gate disabled**
- **Given** the gate resolves disabled and `--preview` is passed
- **When** the command runs
- **Then** Story 3's preview branch renders the payload locally and returns — no send is attempted (preview works precisely so an undecided user can inspect before opting in)

**Edge Case 3: gate resolved fresh per run**
- **Given** one test invocation resolves the gate disabled with no config, then the test writes `quality_signal: true` into `.atcr/config.yaml`
- **When** the gate is re-resolved in the same process
- **Then** the second resolution reflects the new config — no stale in-process cache (mirroring `telemetryGate`'s per-run resolution)

## Error Conditions
**Error Scenario 1: N/A — the disabled path has no error surface**
- A disabled gate produces no user-facing error and no warning; it is the silent, safe default. Malformed persisted config is covered by AC 02-03 (fails safe to disabled), which composes with this AC: the corrupt-config run also attempts zero sends.
- HTTP status / error code: not applicable (no request is ever made)

## Performance Requirements
- **Response Time:** The disabled path is one env read plus at most one config read (short-circuiting before payload construction); negligible (<1ms), no measurable added latency to `review`/`reconcile`.
- **Throughput:** N/A — no work is scheduled on the disabled path.
- **Strictness requirement:** No goroutine, no HTTP client construction, and no payload allocation may occur when the gate resolves disabled — the short-circuit happens at the call site, per `telemetryGate`'s documented "BEFORE any goroutine spawns or payload is built" contract.

## Security Considerations
- **Authentication/Authorization:** N/A — no request is made; no credentials are read on the disabled path.
- **Input Validation:** N/A beyond Story 2's gate inputs.
- **Privacy Guarantee:** This AC is the enforcement half of the epic's absolute privacy line for the send path: without an explicit opt-in, the counters never leave the machine — structurally asserted at the network seam, not merely documented. A regression here is a privacy-critical release blocker.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A hermetic temp dir (`t.TempDir()`) with no `.atcr/config.yaml` (and variants with `quality_signal: false`); `t.Setenv`/`os.Unsetenv` for the env axis; an `httptest.NewServer` request counter as the endpoint.
**Mock/Stub Requirements:** Request-counting HTTP test server; payload-constructor seam (function variable or interface) to assert non-construction; no other mocking — the run path itself stays real.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] With the gate disabled, zero HTTP requests are observed on both the review and reconcile run paths
- [ ] With the gate disabled, the payload constructor is never invoked (asserted via seam, not just absence of requests)
- [ ] Passive-ping and `--sync-cloud` behavior is byte-identical with the new call site present
- [ ] Gate is re-resolved on every run (no cross-run caching)

**Manual Review:**
- [ ] Code reviewed and approved
