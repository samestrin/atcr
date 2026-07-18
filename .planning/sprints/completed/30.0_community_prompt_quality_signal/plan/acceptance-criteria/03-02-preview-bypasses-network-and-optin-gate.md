# Acceptance Criteria: `--preview` Never Sends — No Network Call, Independent of Opt-In Gate State

**Related User Story:** [03: Local `--preview` Surface for the Outbound Quality-Signal Payload](../user-stories/03-local-preview-of-outbound-quality-signal-payload.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Command run-path branch + HTTP seam assertion | The `--preview` branch must execute before `qualitySignalGate()` (Story 2) and before any `net/http` client construction |
| Test Framework | Go `testing` + atomic-counter `doRequest` seam | Mirrors `TestClient_Send_EmptyEndpointNoOps` / `SetDoRequestForTest` pattern in `internal/telemetry/client_test.go` |
| Key Dependencies | stdlib `net/http`, `sync/atomic` (test-only) | No new dependency |

## Related Files
- `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` (both host commands) - modify: place the `--preview` short-circuit before the `qualitySignalGate()` check and before any transport/client construction
- `internal/telemetry/client.go` - existing: the `doRequest` seam (`SetDoRequestForTest`) reused to assert zero HTTP calls
- `cmd/atcr/qualitysignal_test.go` - create/modify: tests asserting zero network calls under both gate-disabled and gate-enabled states
- `cmd/atcr/telemetry_gate_test.go` - reference: exhaustive truth-table test style to mirror for gate-state coverage

### Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` - update: `--preview` short-circuit placed before `qualitySignalGate()` and before any `net/http` client construction (both host commands)
- `cmd/atcr/qualitysignal_test.go` - create/update: zero-HTTP-call assertions via the `doRequest` seam (`internal/telemetry/client.go:60` `SetDoRequestForTest`)

## Happy Path Scenarios
**Scenario 1: `--preview` works with the opt-in gate disabled (default)**
- **Given** `quality_signal` is unset in `.atcr/config.yaml` and no override env var is set (gate resolves disabled, the default per epic AC1)
- **When** the user runs the host command with `--preview`
- **Then** the payload is printed and zero HTTP requests are attempted

**Scenario 2: `--preview` works with the opt-in gate enabled**
- **Given** `quality_signal` is enabled (via config or env, per Story 2's gate)
- **When** the user runs the host command with `--preview`
- **Then** the payload is still only printed — the gate being enabled does not cause `--preview` to also send; zero HTTP requests are attempted

## Edge Cases
**Edge Case 1: `--preview` with no `ATCR_API_KEY` configured**
- **Given** `ATCR_API_KEY` is unset
- **When** `--preview` runs
- **Then** the command still succeeds without any credential-related error, because `--preview` short-circuits before any credential or transport resolution

**Edge Case 2: `--preview` with a malformed persisted `quality_signal` config value**
- **Given** `.atcr/config.yaml` has a malformed `quality_signal` value (would normally fail safe to disabled per Story 2)
- **When** `--preview` runs
- **Then** behavior is identical regardless of the malformed value, because the gate is never consulted on the `--preview` path

## Error Conditions
**Error Scenario 1: Regression — transport constructed before the `--preview` branch**
- Error message (test failure): `"--preview attempted N request(s); want 0"`
- HTTP status / error code: N/A (caught by test assertion, not a runtime user-facing error)

## Performance Requirements
- **Response Time:** Zero network round-trips on the `--preview` path; latency bounded solely by local aggregation, independent of network availability or endpoint reachability.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** `--preview` must never read `ATCR_API_KEY` or construct an authenticated request — reducing the surface an undecided user is exposed to before opting in.
- **Input Validation:** Gate state (env var or persisted config) must have zero influence on whether `--preview` sends — the two mechanisms (gate resolution, `--preview` short-circuit) must remain provably independent, matching Story 2's "no shared boolean, no shared precedence" constraint.

## Test Implementation Guidance
**Test Type:** UNIT (with an httptest-free atomic-counter seam)
**Test Data Requirements:** Gate-disabled state (no env, no config), gate-enabled state (config `quality_signal: true`), and a malformed-config state.
**Mock/Stub Requirements:** Swap `doRequest` via `SetDoRequestForTest` (or equivalent seam) to count invocations; assert count is 0 across all three gate states when `--preview` is set.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `--preview` branch executes before any `qualitySignalGate()` check and before any HTTP client construction (short-circuit at top of `runReview`/`runReconcile`)
- [x] Zero-HTTP-call test passes with gate disabled (default) (`TestPreview_ZeroHTTPCalls_GateDisabled`)
- [x] Zero-HTTP-call test passes with gate enabled (`TestPreview_ZeroHTTPCalls_GateEnabled`)
- [x] `--preview` succeeds with no `ATCR_API_KEY` set (`TestPreview_WorksWithNoAPIKey`, `TestPreview_EndToEndThroughExecute`)

**Manual Review:**
- [ ] Code reviewed and approved
