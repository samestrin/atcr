# Acceptance Criteria: Existing Leaderboard Export Path Byte-for-Byte Regression

**Related User Story:** [03: Persona ID Hashing for the Persona Leaderboard](../user-stories/03-persona-id-hashing-for-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go regression test (`internal/scorecard`) | Proves Epic 10.0's `--export` path is untouched by this story |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Extends `internal/scorecard/export_test.go` patterns |
| Key Dependencies | None new — reuses `scorecard.Export`, `FilterOpts`, fixed-clock helpers already in `export_test.go` | |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/export_test.go` - modify: add a regression test that captures `scorecard.Export`'s output for a fixed, representative input set and asserts it is byte-for-byte identical to a pinned golden value/checksum, both before and after this story's `telemetry.go` changes land.
- `internal/scorecard/export.go` - reference only (must remain unmodified by this story): `Export` (`internal/scorecard/export.go:212`), `PublicRecord` (`internal/scorecard/export.go:35`), `scrubField` (`internal/scorecard/export.go:321`), `AnonymizeRecord`/`ScrubPublicRecord` (`internal/scorecard/export.go:143-160`).
- `cmd/atcr/leaderboard.go` - reference only (must remain unmodified by this story): `runLeaderboardExport` (`cmd/atcr/leaderboard.go:156`) calls `scorecard.Export` unchanged.

## Happy Path Scenarios
**Scenario 1: Export output is byte-for-byte unchanged**
- **Given** the same fixed input record set and fixed `exportedAt` timestamp used by `TestExport_EnvelopeMatchesSpec` (or an equivalent deterministic fixture)
- **When** `scorecard.Export(records, filters, fixedExportNow)` is called after this story's `telemetry.go` and `HashPersonaID`/`TelemetryPersonaRecord` additions land
- **Then** the resulting `[]byte` JSON output is byte-for-byte identical (via `assert.Equal` on the raw bytes, or a SHA-256 checksum comparison against a pinned golden hash) to the output captured before this story's changes

**Scenario 2: PublicRecord allowlist keys are unchanged**
- **Given** the same export output
- **When** the JSON keys present in each `reviewers[]` entry are enumerated
- **Then** they are exactly `model`, `persona`, `runs`, `findings_raised_avg`, `corroboration_rate`, `survived_skeptic_rate` (when present), `cost_per_corroborated_finding_usd` (when present), `latency_p50_ms` — no new key (e.g. `persona_id_hash`) has leaked onto this schema

## Edge Cases
**Edge Case 1: Verification-block present record**
- **Given** an input record carrying `FindingsVerified`/`FindingsRefuted`/`SurvivedSkepticRate` pointers (exercising `reviewerAcc.finalize`'s verification branch)
- **When** exported before and after this story's changes
- **Then** the `survived_skeptic_rate` value and its presence/omission behavior are identical in both runs

**Edge Case 2: Multiple reviewers/models aggregated and sorted**
- **Given** a multi-record, multi-(persona,model) input set (exercising `Export`'s grouping and `sort.Slice` ordering)
- **When** exported before and after this story's changes
- **Then** row order and aggregated values are identical in both runs — confirming this story introduced no ordering or aggregation side effects

## Error Conditions
**Error Scenario 1: Empty record set still returns the same error**
- **Given** an empty `[]Record{}` input
- **When** `scorecard.Export` is called
- **Then** it still returns `scorecard.ErrNoExportRecords` with the same error identity (`errors.Is`) as before this story's changes — the error path is unaffected
- Error message: unchanged — `"no records match the export filters"` (via `ErrNoExportRecords`)

## Performance Requirements
- **Response Time:** No performance regression: `Export` call time for the fixture set is unaffected since `telemetry.go` is not invoked from `Export`, `AnonymizeRecord`, `ScrubPublicRecord`, or `runLeaderboardExport`.
- **Throughput:** N/A — this AC is a correctness regression test, not a load test.

## Security Considerations
- **Authentication/Authorization:** N/A — local test, no network/auth surface.
- **Input Validation:** N/A — the test reuses already-validated `Record` fixtures from `export_test.go`.
- **Privacy guarantee integrity:** This is the story's primary safety net (per the plan's Risk Mitigation #1): it is the concrete, automated proof that the Epic 10.0 public leaderboard export's documented allowlist and scrubbing behavior were not weakened, bypassed, or extended by this story's new hashing path.

## Test Implementation Guidance
**Test Type:** INTEGRATION (exercises the full `Export` pipeline end-to-end against a fixed fixture)
**Test Data Requirements:** Reuse or extend `export_test.go`'s `exportRec` helper and `fixedExportNow` constant so the regression fixture stays in lockstep with existing export tests; capture a golden byte sequence (or its SHA-256 checksum) as a test constant/fixture committed alongside the test.
**Mock/Stub Requirements:** None — `scorecard.Export` is a pure function over in-memory records plus a fixed timestamp; no I/O to mock.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A regression test in `internal/scorecard/export_test.go` pins `Export`'s output (byte-for-byte or checksum) for a fixed fixture set
- [ ] Test confirms no new key (e.g. `persona_id_hash`) appears in the `PublicRecord`/`ExportEnvelope` JSON output
- [ ] Test confirms `ErrNoExportRecords` behavior on empty input is unchanged
- [ ] `scrubField`, `PublicRecord`, `AnonymizeRecord`, `ScrubPublicRecord`, and `runLeaderboardExport` source remain textually unmodified (verified in code review, not just by the test)

**Manual Review:**
- [ ] Code reviewed and approved
