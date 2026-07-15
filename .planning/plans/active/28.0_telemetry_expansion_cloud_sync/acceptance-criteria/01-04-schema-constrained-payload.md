# Acceptance Criteria: Schema-Constrained Payload (No Source Code or File Paths)

**Related User Story:** [01: Anonymous Usage Telemetry Ping](../user-stories/01-anonymous-usage-telemetry-ping.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/telemetry`) | Narrow, dedicated `Event` struct — not a reuse/extension of scorecard structs |
| Test Framework | `go test` (standard `testing`, `encoding/json`) | Test file: `internal/telemetry/event_test.go` (or within `client_test.go`) |
| Key Dependencies | `encoding/json` (stdlib) | JSON struct tags define the exact wire schema |

## Related Files
- `internal/telemetry/event.go` - create: defines `type Event struct { Event string \`json:"event"\`; Lang string \`json:"lang"\`; Lines int \`json:"lines"\`; Status string \`json:"status"\` }` — a dedicated, narrow struct with exactly these four fields, deliberately not embedding or extending any `scorecard` struct.
- `internal/telemetry/event_test.go` - create: marshals a populated `Event` and asserts (via `json.Unmarshal` into a `map[string]interface{}`) that the result has exactly the keys `event`, `lang`, `lines`, `status` — no more, no fewer.
- `cmd/atcr/review.go` - modify: the `Event` passed from `runReview` is built from already-computed values (detected `lang`, line count used for the scorecard, `status` from the run outcome) — never from raw diff content, file paths, or findings text.
- `cmd/atcr/reconcile.go` - modify: the `Event` passed from `runReconcile` is built the same way, from already-computed values, never from `reconcile.Result` findings/file data directly.

## Happy Path Scenarios
**Scenario 1: Marshaled payload contains exactly four allowlisted keys**
- **Given** a populated `Event{Event: "review_run", Lang: "go", Lines: 450, Status: "success"}`
- **When** the struct is marshaled via `json.Marshal`
- **Then** the resulting JSON is exactly `{"event":"review_run","lang":"go","lines":450,"status":"success"}` with no additional fields

**Scenario 2: review.go builds the Event from safe, already-computed values**
- **Given** `runReview` has completed and already has a detected language and line count (the same values used for the scorecard)
- **When** the `Event` is constructed for the telemetry send
- **Then** `Lang` and `Lines` are copied from those existing computed values (not derived by re-scanning file contents or paths at the telemetry call site)

## Edge Cases
**Edge Case 1: Struct field reordering or renaming does not silently break the schema**
- **Given** a future code change to `internal/telemetry.Event`
- **When** the marshal test runs
- **Then** the test explicitly asserts the key set (not just presence of a `lang` field, but absence of any unexpected key), so an accidental new field (e.g. a debug field, a file path) fails the test immediately

**Edge Case 2: Zero-value Event fields**
- **Given** an `Event` with `Lines: 0` and `Status: ""` (e.g. a degenerate run)
- **When** marshaled
- **Then** the four keys are still present with their zero values (`"lines":0`, `"status":""`) — no `omitempty` tags that could make the schema shape ambiguous across payloads

## Error Conditions
**Error Scenario 1: A field is accidentally added that carries file-path or code content**
- **Given** a hypothetical future contribution adds a `File string` field to `Event` for debugging
- **When** the schema-shape test (`event_test.go`) runs
- **Then** the test fails, asserting only `event`, `lang`, `lines`, `status` are present — enforcing the "no source code, no file paths" constraint as a regression gate
- Error message: test failure message such as `"unexpected key %q in telemetry payload"` for any key beyond the four allowlisted ones

**Error Scenario 2: Call site accidentally passes findings/diff data into Lang or Status**
- **Given** a call site bug that assigns something other than a closed-set language string to `Lang`, or free-form text to `Status`
- **When** reviewed against this AC's test coverage
- **Then** a unit test on the call-site helper (if introduced, e.g. `run_helpers.go` in `cmd/atcr`) asserts `Lang` comes from the same detected-language value already used for the scorecard and `Status` is constrained to `"success"`/`"failure"` only

## Performance Requirements
- **Response Time:** N/A (schema validation is a compile-time/struct-shape concern, not a runtime performance concern).
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** `Event`, `Lang`, and `Status` are constrained to closed, internally-defined value sets (e.g. `"review_run"`, detected language identifiers, `"success"`/`"failure"`) rather than accepting arbitrary strings from user input, findings text, or file content — this is the primary privacy control for this story and must be enforced at the call sites in `cmd/atcr/review.go` and `cmd/atcr/reconcile.go`, not just at the struct level.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Representative `Event` values across supported languages (e.g. "go", "python", "javascript") and both status outcomes; a zero-value `Event` to confirm no `omitempty` ambiguity.
**Mock/Stub Requirements:** None — this is pure struct/marshal testing with `encoding/json`, no HTTP or network involved.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/telemetry.Event` is a dedicated struct with exactly `event`, `lang`, `lines`, `status` JSON keys
- [ ] A test asserts the marshaled payload's key set exactly matches the four allowlisted fields (no more, no fewer)
- [ ] `runReview`/`runReconcile` construct `Event` values from already-computed scorecard-adjacent values, never from raw file/diff content
- [ ] No `File`, path, or free-text field is ever added to the `Event` struct

**Manual Review:**
- [ ] Code reviewed and approved
