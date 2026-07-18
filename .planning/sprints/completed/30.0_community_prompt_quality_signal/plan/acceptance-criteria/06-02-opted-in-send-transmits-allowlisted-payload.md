# Acceptance Criteria: Opted-In Run Sends Exactly One Allowlisted Payload, Byte-Identical to `--preview`

**Related User Story:** [06: Gated Quality-Signal Transmission via the Epic 28.0 Transport](../user-stories/06-gated-quality-signal-transmission.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI run-path wiring + transport) | `cmd/atcr`, `internal/telemetry` (sibling-payload transport over `telemetry.Client`, resolved in sprint-design) |
| Test Framework | `go test` (standard library `testing`), `testify` `assert`/`require` | Mirrors `internal/telemetry/client_test.go` conventions |
| Key Dependencies | `net/http/httptest` (capture server), `encoding/json` | No new third-party dependency |

## Related Files
- `cmd/atcr/review.go` / `cmd/atcr/reconcile.go` - modify: enabled-branch send invocation at the Story 6 call sites.
- `internal/telemetry/quality_signal.go` - reference: Story 1's allowlisted payload type and its locking regression test (AC 01-05).
- `internal/telemetry/client.go` - modify: add a sibling send method for the new payload type, preserving the detached-goroutine, HTTPS-only, nil/empty-endpoint-no-op contract. (Sprint-design resolved the transport as the sibling-payload path over `telemetry.Client` — never an extension of `internal/scorecard`'s `Push`/`CloudSyncRecord`.)
- `cmd/atcr/qualitysignal_send_test.go` - create: capture-server tests asserting exactly-one send, allowlisted body, and preview/send byte equality.

### Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go` / `cmd/atcr/reconcile.go` - update: enabled-branch send at the Story 6 call sites (`:462-467` / `:186-191`)
- `internal/telemetry/client.go` - update: sibling send method for the new payload type, preserving the `New` (`:81`) / `isHTTPS` (`:95`) / detached-`Send` (`:106`) contract
- `cmd/atcr/qualitysignal_send_test.go` - create: capture-server tests (exactly-one send, allowlisted body, preview/send byte equality)

## Happy Path Scenarios
**Scenario 1: gate enabled via env — one send with the exact aggregate counters**
- **Given** the quality-signal env var explicitly enables the gate
- **And** a fixture `.atcr/debt/` store whose hand-computed per-(persona, model) dismissed/confirmed counts are known (per Story 1's AC 01-01 fixtures)
- **And** an `httptest` capture server as the endpoint
- **When** a review run completes
- **Then** the capture server observes exactly one request
- **And** the request body unmarshals into Story 1's payload struct with field values equal to the hand-computed counts
- **And** the body contains no key outside Story 1's allowlisted field set (guarded doubly: here and by AC 01-05's locking test)

**Scenario 2: sent bytes equal `--preview` bytes for the same data**
- **Given** the same fixture store and gate enabled
- **When** the payload is rendered via Story 3's `--preview` path and, separately, transmitted by the send path
- **Then** both byte streams are produced by the same constructor/marshal path and are identical (field order, values, and field set) — the preview is the send, per Story 3's AC 03-03 asserted here from the send side

**Scenario 3: gate enabled via persisted config — same single-send behavior**
- **Given** no env var and `.atcr/config.yaml` containing `quality_signal: true`
- **When** a review or reconcile run completes
- **Then** exactly one send fires, identical in shape to Scenario 1 — config consent is as sufficient as env consent

## Edge Cases
**Edge Case 1: empty aggregation (no debt records, or none with model attribution)**
- **Given** the gate is enabled but Story 1's aggregation yields zero rows
- **When** a run completes
- **Then** no send fires — a zero-row aggregation is treated as "nothing to transmit," so the enabled branch short-circuits after aggregation and makes no network call (consistent with the epic's minimal-transmission posture); it must not error, and must not send an empty or partial/malformed body. This behavior is documented in `docs/telemetry.md` (Story 5) and locked by this test.

**Edge Case 2: plaintext or empty endpoint**
- **Given** the gate is enabled but the configured endpoint is empty or non-HTTPS
- **When** a run completes
- **Then** the transport no-ops per `isHTTPS`/`New`'s existing contract (`internal/telemetry/client.go:81,95`) — no plaintext transmission ever occurs

**Edge Case 3: review and reconcile in the same session**
- **Given** the gate is enabled and a session runs both commands
- **When** both complete
- **Then** send semantics are **per-run**: each `review` and each `reconcile` invocation independently sends once at its own completion (no cross-command deduplication within a session). Because every send is a deterministic aggregate of the same underlying `.atcr/debt/` records, the resulting duplicate is idempotent and detectable rather than corrupting.

## Error Conditions
**Error Scenario 1: capture server answers 500 — run still succeeds**
- **Given** the gate is enabled and the endpoint returns HTTP 500
- **When** a run completes
- **Then** the run's exit code and stdout match the gate-disabled baseline (fail-open; full matrix in AC 06-03)
- HTTP status / error code: transport errors never map to a usage error (exit 2) or a run failure (exit 1) on this path

## Performance Requirements
- **Response Time:** The send is fire-and-forget on the transport's detached path — the run does not await the HTTP round trip; added wall-clock latency to `review`/`reconcile` is bounded by payload construction only (<10ms for realistic `.atcr/debt/` sizes).
- **Throughput:** Exactly one send per run; no batching loops, no retries beyond the transport's existing behavior.
- **Strictness requirement:** Payload construction happens at most once per run and is shared with the `--preview` render path (single source of marshaling).

## Security Considerations
- **Authentication/Authorization:** Inherits the `telemetry.Client` transport's existing endpoint-configuration mechanism — this AC adds no new credential surface.
- **Input Validation:** The payload originates from Story 1's typed constructor — no user-supplied free text reaches the body.
- **Privacy Guarantee:** The transmitted field set is exactly Story 1's allowlist (persona identifier hashed via `scorecard.HashPersonaID`, model, dismissed count, confirmed count): zero code, zero finding content, zero file paths. Any future field addition fails AC 01-05's locking test before it can reach this send path.

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION (in-process CLI run against a capture server)
**Test Data Requirements:** Story 1's fixture debt-record set with hand-computed expected counts; `t.TempDir()` for `.atcr/`; `t.Setenv` for the env axis; an `httptest` capture server recording request count and raw body.
**Mock/Stub Requirements:** Capture server only — no mocking of the aggregation or gate; the point of this AC is that the real wiring produces exactly the real payload.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Enabled gate (env or config) → exactly one request per run, body unmarshals into Story 1's struct with correct counts
- [ ] Transmitted bytes are byte-identical to the `--preview` rendering for the same fixture data
- [ ] Body contains no field outside Story 1's allowlisted set
- [ ] Plaintext/empty endpoint → no transmission
- [ ] Empty aggregation (zero rows) → no send fires; behavior tested and matches the Story 5 documentation
- [ ] Send semantics are per-run (each `review`/`reconcile` sends once at its own completion)

**Manual Review:**
- [ ] Code reviewed and approved
