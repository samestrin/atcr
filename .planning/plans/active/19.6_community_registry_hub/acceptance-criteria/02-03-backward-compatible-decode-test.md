# Acceptance Criteria: Backward-Compatible Old-Shape Decode Test

**Related User Story:** [02: Structured Model Metadata Schema](../user-stories/02-structured-model-metadata-schema.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md), [testing-mock-registry.md](../documentation/testing-mock-registry.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go unit test | `internal/personas/search_test.go` (new file) |
| Test Framework | Go `testing` + `testify/assert`/`require` (existing project convention) | Matches style used in `internal/personas/personas_test.go` |
| Key Dependencies | `encoding/json` (stdlib) | No new dependencies |

### Related Files (from codebase-discovery.json)
- `internal/personas/search_test.go` — create: table-driven test decoding old-shape (four-field) `index.json` fixtures into the extended `PersonaIndexEntry` struct and asserting zero-value new fields plus no decode error.
- `internal/personas/personas_test.go` (`fakeIndexJSON` line 34) — reference: existing old-shape fixture pattern to mirror/reuse.
- `internal/personas/search.go` (`PersonaIndexEntry`) — reference: the extended struct under test.
- `internal/personas/client.go` (`FetchIndex`) — reference: the full fetch-and-decode call site exercised against an old-shape fixture.
- `personas/community/index.json` — reference: new entries will use the extended shape while old entries remain decodable.


## Happy Path Scenarios
**Scenario 1: Old-shape index.json decodes with new fields at zero value**
- **Given** an old-shape `index.json` fixture identical to `fakeIndexJSON` (entries with only `name`/`version`/`description`/`path`, no `provider`/`model`/`tasks`/`tags` keys)
- **When** the fixture is unmarshaled into `[]PersonaIndexEntry`
- **Then** decoding succeeds with no error, every entry's `Name`/`Version`/`Description`/`Path` populate as before, and `Provider`/`Model` are empty strings while `Tasks`/`Tags` are `nil` slices

**Scenario 2: Old-shape fixture decodes via the full FetchIndex path**
- **Given** a `testServer` (per the existing helper in `personas_test.go`) serving the old-shape fixture at `/index.json`
- **When** `FetchIndex(client, baseURL)` is called against that server
- **Then** it returns the expected entries with no error, proving the entire fetch-and-decode pipeline (not just raw `json.Unmarshal`) tolerates pre-change payloads

## Edge Cases
**Edge Case 1: Mixed-shape payload (some entries old-shape, some new-shape)**
- **Given** an `index.json` array containing one entry with only the original four fields and one entry with all eight fields populated
- **When** the array is unmarshaled into `[]PersonaIndexEntry`
- **Then** both entries decode without error — the old-shape entry has zero-value `Provider`/`Model`/`Tasks`/`Tags`, and the new-shape entry has its full values, with no cross-entry interference

**Edge Case 2: New-shape payload decoded by pre-change consumer semantics**
- **Given** a new-shape entry with `provider`/`model` populated
- **When** it is decoded into a hypothetical four-field-only struct shape (simulated by decoding into a struct literal with only the original fields, or asserted via `json.Unmarshal` with a restricted target type in the test)
- **Then** decoding still succeeds with no error — unknown fields (`provider`/`model`/`tasks`/`tags`) are silently ignored, confirming forward compatibility for any caller that has not yet adopted the extended struct

## Error Conditions
**Error Scenario 1: Fixture is malformed JSON**
- **Given** a deliberately malformed old-shape fixture (e.g., missing closing bracket)
- **When** the test attempts to unmarshal it
- **Then** `json.Unmarshal` returns a non-nil error, and the test asserts this error is returned (not swallowed) — confirming the backward-compat guarantee applies to old-shape *valid* JSON only, not to malformed payloads

## Performance Requirements
- **Response Time:** Test execution completes in well under 100ms (in-memory JSON decode, no real network I/O — `testServer` is an in-process `httptest.Server`).
- **Throughput:** Not applicable — single-fixture unit test, not a load/throughput scenario.

## Security Considerations
- **Authentication/Authorization:** Not applicable — test-only scope, no auth surface.
- **Input Validation:** Confirms the decoder remains in permissive (non-`KnownFields(true)`) mode for the index struct, per the story's explicit constraint that strict-field enforcement belongs to the persona-loading path, not the index decode path — the test should not introduce or assert strict-mode behavior here.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Old-shape fixture (reuse/mirror `fakeIndexJSON`), a new-shape fixture with all eight fields, and a mixed-shape fixture combining both entry types within one array
**Mock/Stub Requirements:** `httptest.Server` via the existing `testServer` helper in `personas_test.go` for the `FetchIndex`-path scenario; no external network or filesystem mocking needed for direct `json.Unmarshal` scenarios

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/personas/search_test.go` exists and contains a test decoding an old-shape fixture into `PersonaIndexEntry` with zero-value new fields and no error
- [ ] A mixed-shape (old + new entries in one payload) case is covered
- [ ] The full `FetchIndex` fetch-and-decode path is exercised against an old-shape fixture, not just raw `json.Unmarshal`

**Manual Review:**
- [ ] Code reviewed and approved
