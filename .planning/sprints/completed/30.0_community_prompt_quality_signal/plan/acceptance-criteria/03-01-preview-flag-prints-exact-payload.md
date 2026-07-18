# Acceptance Criteria: `--preview` Flag Renders the Exact Outbound JSON Payload

**Related User Story:** [03: Local `--preview` Surface for the Outbound Quality-Signal Payload](../user-stories/03-local-preview-of-outbound-quality-signal-payload.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra CLI flag + command run-path branch | Follows `addSyncCloudFlags`/`addRangeFlags` chained-`PreRunE` registration pattern |
| Test Framework | Go `testing` + `testify/assert` | Matches existing `cmd/atcr` and `internal/telemetry` test conventions |
| Key Dependencies | `github.com/spf13/cobra`, stdlib `encoding/json` | No new third-party dependency (epic constraint) |

## Related Files
- `cmd/atcr/flags.go` - modify: add an `addQualitySignalFlags`-style helper that registers the `--preview` bool flag on the host command(s), chaining its own `PreRunE` prev-first (mirroring `addRangeFlags`/`addSyncCloudFlags`)
- `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` (the `--preview` flag is registered on both `atcr review` and `atcr reconcile`, matching Story 6's two Send call sites) - modify: build the `quality_signal.go` payload first, then branch on `--preview` before any gate check or transport construction
- `internal/telemetry/quality_signal.go` - existing (Story 1 dependency): the allowlisted payload struct that `--preview` marshals via `json.MarshalIndent`
- `cmd/atcr/qualitysignal_test.go` (new) - create: tests asserting `--preview` stdout output and exit behavior

### Related Files (from codebase-discovery.json)

- `cmd/atcr/flags.go` - update: register the `--preview` bool flag via an `addQualitySignalFlags`-style helper, following the existing `addSyncCloudFlags`/`addRangeFlags` chained-`PreRunE` pattern
- `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` - update: `--preview` branch in the run path (both host commands), before the Story 2 gate check and before any transport construction
- `cmd/atcr/qualitysignal_test.go` - create: `--preview` stdout/exit-behavior tests

## Happy Path Scenarios
**Scenario 1: `--preview` prints the allowlisted JSON payload and exits successfully**
- **Given** quality-signal aggregation (Story 1) has produced at least one `(persona, model)` counter row
- **When** the user runs the host command with `--preview` (e.g. `atcr review --preview`)
- **Then** stdout contains pretty-printed JSON of the quality-signal payload with exactly the allowlisted fields (persona identifier, model, dismissed count, confirmed count), and the command exits 0

**Scenario 2: `--preview` output includes an explicit "not sent" marker**
- **Given** `--preview` is set
- **When** the command prints the payload
- **Then** the output includes a human-readable line (alongside the JSON, not embedded in it) stating that nothing was transmitted, addressing the epic's "false sense of completion" risk

## Edge Cases
**Edge Case 1: No aggregated data yet**
- **Given** no local debt records exist (or none carry a resolvable model per Story 1's schema-v2 rule)
- **When** `--preview` runs
- **Then** it prints an empty/zero-row payload set (a valid empty JSON array or documented "no data yet" message) rather than erroring

**Edge Case 2: `--preview` combined with `--sync-cloud`**
- **Given** the user passes both `--preview` and `--sync-cloud` on the same invocation
- **When** the command runs
- **Then** `--preview` takes precedence — the payload is printed, no cloud push occurs, and `--sync-cloud` is silently ignored for the quality-signal path (its own scorecard push, if any, is unaffected and out of scope for this AC)

## Error Conditions
**Error Scenario 1: `--preview` passed on a command that does not host the quality-signal flag**
- Error message: `unknown flag: --preview`
- HTTP status / error code: cobra usage error, exit code 2 (matches existing `usageError` convention)

## Performance Requirements
- **Response Time:** `--preview` completes in the time it takes to read and aggregate local `.atcr/debt/` records — no network round-trip is on the critical path, so it must be at least as fast as the equivalent non-preview aggregation step alone.
- **Throughput:** N/A — single local invocation, no concurrency requirement beyond what the existing aggregation path already provides.

## Security Considerations
- **Authentication/Authorization:** `--preview` must not read or require `ATCR_API_KEY` or any cloud credential — it is designed to work for an undecided user who has not opted in.
- **Input Validation:** The printed payload must contain only the allowlisted fields defined in `internal/telemetry/quality_signal.go` — no code, file path, or finding-content field may ever appear in `--preview` output.

## Test Implementation Guidance
**Test Type:** INTEGRATION (cobra command execution with captured stdout)
**Test Data Requirements:** Fixture `.atcr/debt/` records covering: populated aggregation (1+ persona/model rows), zero-row aggregation, and a record set combining `--preview` with `--sync-cloud`.
**Mock/Stub Requirements:** No HTTP stubbing needed for this AC (covered by AC 03-02); use cobra's `SetOut`/`SetArgs` test harness to capture stdout and exit behavior.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `--preview` flag is registered via a chained `PreRunE` helper mirroring `addSyncCloudFlags` (`addQualitySignalFlags`)
- [x] Stdout contains pretty-printed JSON with exactly the allowlisted quality-signal fields (`TestPreview_PrintsAllowlistedJSONPayload`)
- [x] Output includes an explicit "not sent" marker distinct from the JSON payload (`TestPreview_IncludesNotSentMarker`)
- [x] Empty-aggregation and `--preview`+`--sync-cloud` combinations are covered by tests (`TestPreview_EmptyAggregationPrintsEmptyPayloadNotError`, `TestPreview_TakesPrecedenceOverSyncCloud`)

**Manual Review:**
- [ ] Code reviewed and approved
