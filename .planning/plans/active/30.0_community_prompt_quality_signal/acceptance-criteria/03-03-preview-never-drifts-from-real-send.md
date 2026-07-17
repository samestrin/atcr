# Acceptance Criteria: Regression Test Locks `--preview` Output to the Real Send's Marshal Path

**Related User Story:** [03: Local `--preview` Surface for the Outbound Quality-Signal Payload](../user-stories/03-local-preview-of-outbound-quality-signal-payload.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Shared payload-construction helper + regression test | Both the `--preview` branch and the real send call site must call one shared function, not independently reconstruct the struct |
| Test Framework | Go `testing` (table-driven) | Mirrors `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` allowlist-locking style |
| Key Dependencies | stdlib `encoding/json`, `reflect` (for `DeepEqual` round-trip assertion) | No new dependency |

## Related Files
- `internal/telemetry/quality_signal.go` - existing (Story 1): the allowlisted payload struct and its single `json.Marshal`/`json.MarshalIndent` call path
- `cmd/atcr/review.go` (or the command wiring the Send call site) - modify: ensure a single shared helper builds the payload struct instance, consumed identically by both the `--preview` branch and the real `Send` call
- `cmd/atcr/qualitysignal_test.go` - create: round-trip/equivalence test proving `--preview` JSON matches the real-send marshal output byte-for-byte
- `internal/telemetry/client_test.go` - reference: `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` as the sibling allowlist-regression pattern (guards field-set drift; this AC guards marshal-path drift)

## Happy Path Scenarios
**Scenario 1: Preview and real send marshal the identical struct instance**
- **Given** a constructed quality-signal payload value (from the shared helper)
- **When** the `--preview` branch marshals it via `json.MarshalIndent`
- **And** the real send path (exercised with a stubbed endpoint, gate enabled) marshals the same constructed value via its own `json.Marshal` call
- **Then** the two resulting JSON byte outputs are identical (modulo indentation whitespace)

**Scenario 2: Golden round-trip of the preview output**
- **Given** the pretty-printed `--preview` JSON output
- **When** it is unmarshaled back into the quality-signal payload struct type
- **Then** the resulting struct is `reflect.DeepEqual` to the original struct instance that was passed into the preview branch

## Edge Cases
**Edge Case 1: A future field is added to the quality-signal struct**
- **Given** a hypothetical future field addition to the payload struct
- **When** this AC's equivalence test runs
- **Then** it still passes (both paths marshal the same struct value) — this AC guards marshal-path drift only; the separate allowlist regression test (mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`, owned by Story 1) is responsible for catching an unauthorized new field

**Edge Case 2: The preview render is refactored to hand-copy fields instead of reusing the shared constructor**
- **Given** a code change that has the `--preview` branch build its own struct literal field-by-field instead of calling the shared payload-construction helper
- **When** the equivalence test runs with a fixture where the hand-copy would omit or mis-order a field
- **Then** the test fails, catching the divergence before it reaches a release (this directly covers the epic's flagged "High" risk of preview/send drift)

## Error Conditions
**Error Scenario 1: Marshal-path divergence detected**
- Error message: `"preview payload JSON does not match real-send payload JSON for identical input: got %s, want %s"`
- HTTP status / error code: N/A (test-assertion failure, not a runtime error)

## Performance Requirements
- **Response Time:** Test-only concern; the table-driven equivalence test must run as a fast unit test (well under 1s for 3+ fixture rows), no network I/O involved (real-send side uses a stubbed/no-op transport).
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — this AC is a structural/regression guarantee, not an auth boundary.
- **Input Validation:** This AC is the concrete mechanism that makes the epic's "no-code, no-finding-content" privacy guarantee verifiable rather than merely asserted — a maintainer who inspects `--preview` output is guaranteed (by this test) to be looking at exactly what would be sent, not a stale or hand-copied approximation.

## Test Implementation Guidance
**Test Type:** UNIT (table-driven)
**Test Data Requirements:** 3+ fixture payload values: zero-value/empty aggregation, a single `(persona, model)` row, and multiple rows with distinct personas and models.
**Mock/Stub Requirements:** Stub the real-send transport (reuse the `doRequest` seam from AC 03-02) only to the extent needed to capture the exact bytes handed to `json.Marshal`/the HTTP request body, without an actual network call.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A single shared helper constructs the payload struct, consumed identically by `--preview` and the real send call site
- [ ] Byte-for-byte JSON equivalence test passes across 3+ fixture payloads
- [ ] Golden round-trip test (`--preview` output unmarshal → `DeepEqual` original) passes
- [ ] A fixture proving a hand-copied reconstruction would be caught is present and documented

**Manual Review:**
- [ ] Code reviewed and approved
