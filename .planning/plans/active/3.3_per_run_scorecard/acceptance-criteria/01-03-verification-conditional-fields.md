# Acceptance Criteria: Verification-Conditional Fields

**Related User Story:** [01: Auto-emit Scorecard](../user-stories/01-auto-emit-scorecard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Verification Detection | `os.Stat` on `verification.json` | Presence check |
| Conditional Fields | Go `omitempty` JSON tags | Omit when nil |
| Test Framework | `go test` + `testify` | Table-driven with/without verification |

## Related Files
- `internal/scorecard/scorecard.go` - modify: conditional fields with `omitempty`
- `internal/reconcile/emit.go` - reference: `ReadReconciledFindings` and Verification struct
- `internal/scorecard/store.go` - modify: populate verification fields when data available

## Happy Path Scenarios
**Scenario 1: verification.json present — fields populated**
- **Given** reconcile run produced `verification.json` with 8 verified, 2 refuted findings for reviewer-A
- **When** scorecard record is built
- **Then** record contains `findings_verified: 8`, `findings_refuted: 2`, `survived_skeptic_rate: <computed>`

**Scenario 2: verification.json absent — fields omitted**
- **Given** reconcile run did not produce `verification.json`
- **When** scorecard record is built
- **Then** JSON output does NOT contain `findings_verified`, `findings_refuted`, or `survived_skeptic_rate` keys (omitempty)

**Scenario 3: Partial verification data**
- **Given** `verification.json` exists but has 0 verified and 0 refuted for a reviewer
- **When** record is built
- **Then** `findings_verified: 0`, `findings_refuted: 0`, `survived_skeptic_rate: 0.0` (present, not omitted — zero is a valid value)

## Edge Cases
**Edge Case 1: verification.json malformed**
- **Given** `verification.json` exists but contains invalid JSON
- **When** scorecard attempts to read verification data
- **Then** verification fields are omitted (treated as absent); warning logged

**Edge Case 2: survived_skeptic_rate with zero skeptics**
- **Given** verification data exists but no skeptic findings were tracked
- **When** rate is computed
- **Then** `survived_skeptic_rate: 0.0` (not NaN)

## Error Conditions
**Error Scenario 1: verification.json unreadable (permissions)**
- Error message: logged warning `scorecard: verification read failed: <err>`; verification fields omitted; reconcile continues

## Performance Requirements
- **Response Time:** verification.json read adds < 5ms
- **Throughput:** Single read per run, shared across all reviewer records

## Security Considerations
- **Input Validation:** verification.json is parsed with strict JSON decoding; unknown fields ignored
- **Data Protection:** Verification data is local operational data; no external transmission

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture verification.json files (valid, empty, malformed, missing); reconcile Results with known verification counts
**Mock/Stub Requirements:** Filesystem mock for verification.json presence/absence

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Fields omitted from JSON when verification.json absent
- [ ] Fields populated when verification.json present
- [ ] Malformed verification.json handled gracefully (fields omitted, warning logged)
- [ ] survived_skeptic_rate handles zero-division

**Manual Review:**
- [ ] Code reviewed and approved
