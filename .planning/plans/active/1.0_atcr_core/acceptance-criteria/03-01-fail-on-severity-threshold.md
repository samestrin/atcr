# Acceptance Criteria: Fail-on Severity Threshold

**Related User Story:** [03: CI Integration](../user-stories/03-ci-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Command | Go (cobra) | `atcr reconcile --fail-on <severity>` |
| Severity Logic | Go | Centralized threshold check in cmd/atcr/main.go |
| Test Framework | testify | assert/require for exit code and severity checks |

## Related Files
- `cmd/atcr/main.go` - create: centralized exit-code logic mapping severity threshold to exit code
- `cmd/atcr/reconcile.go` - create: `reconcile` cobra command with `--fail-on` flag
- `internal/reconcile/merger.go` - create: severity constants and threshold comparison logic
- `internal/reconcile/merger_test.go` - create: tests for severity threshold check

## Happy Path Scenarios

**Scenario 1: No findings at or above threshold**
- **Given** reconciled findings.json contains only LOW severity findings
- **When** `atcr reconcile --fail-on HIGH` is executed
- **Then** exit code is 0 and stdout reports pass

**Scenario 2: Findings at threshold present**
- **Given** reconciled findings.json contains one HIGH severity finding
- **When** `atcr reconcile --fail-on HIGH` is executed
- **Then** exit code is 1 and stdout reports fail with finding count

**Scenario 3: Findings above threshold present**
- **Given** reconciled findings.json contains one CRITICAL severity finding
- **When** `atcr reconcile --fail-on HIGH` is executed
- **Then** exit code is 1 (CRITICAL >= HIGH threshold)

**Scenario 4: No findings at all**
- **Given** reconciled findings.json is an empty array
- **When** `atcr reconcile --fail-on LOW` is executed
- **Then** exit code is 0

**Scenario 5: All four threshold levels**
- **Given** findings.json with severities [CRITICAL, HIGH, MEDIUM, LOW]
- **When** `atcr reconcile --fail-on <severity>` is run for each level
- **Then** `--fail-on CRITICAL` → exit 1, `--fail-on HIGH` → exit 1, `--fail-on MEDIUM` → exit 1, `--fail-on LOW` → exit 1

## Edge Cases

**Edge Case 1: Invalid severity value**
- **Given** `--fail-on INVALID` is passed
- **When** command executes
- **Then** exit code is 2 (error) with message: `invalid severity threshold: "INVALID" (must be CRITICAL, HIGH, MEDIUM, or LOW)`

**Edge Case 2: Case-insensitive severity**
- **Given** `--fail-on critical` (lowercase) is passed
- **When** command executes
- **Then** severity is normalized to uppercase, threshold is CRITICAL, command succeeds

**Edge Case 3: Missing findings.json**
- **Given** reconciled/findings.json does not exist
- **When** `atcr reconcile --fail-on HIGH` is executed
- **Then** exit code is 2 with message: `reconciled findings not found: run 'atcr review' first`

**Edge Case 4: Malformed findings.json**
- **Given** reconciled/findings.json contains invalid JSON
- **When** `atcr reconcile --fail-on HIGH` is executed
- **Then** exit code is 2 with message: `failed to parse findings: <parse error>`

## Error Conditions

**Error Scenario 1: Unknown flag value**
- Error message: `invalid severity threshold: "X" (must be CRITICAL, HIGH, MEDIUM, or LOW)`
- Exit code: 2

**Error Scenario 2: Findings file not found**
- Error message: `reconciled findings not found: run 'atcr review' first`
- Exit code: 2

## Performance Requirements
- **Response Time:** Exit-code check completes in <10ms after findings.json is loaded
- **Throughput:** Handles findings files up to 10MB without degradation

## Security Considerations
- **Input Validation:** Severity flag validated against enum before any file I/O
- **No external input:** Reads only local findings.json, no network calls

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture findings.json files for each severity combination (empty, LOW-only, MEDIUM+HIGH+CRITICAL, all levels)
**Mock/Stub Requirements:** None — pure logic on local file

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--fail-on CRITICAL` returns 1 only when CRITICAL findings exist
- [ ] `--fail-on HIGH` returns 1 when HIGH or CRITICAL findings exist
- [ ] `--fail-on MEDIUM` returns 1 when MEDIUM, HIGH, or CRITICAL findings exist
- [ ] `--fail-on LOW` returns 1 when any finding exists
- [ ] Exit code 0 when no findings at/above threshold
- [ ] Severity comparison is case-insensitive

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Exit-code logic centralized in main.go (single code path)
