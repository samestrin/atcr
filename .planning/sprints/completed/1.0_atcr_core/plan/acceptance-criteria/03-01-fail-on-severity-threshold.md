# Acceptance Criteria: Fail-on Severity Threshold

**Related User Story:** [03: CI Integration](../user-stories/03-ci-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Command | Go (cobra) | `atcr reconcile --fail-on <severity>` |
| Severity Logic | Go | internal/reconcile exposes a pure helper `CountAtOrAbove(findings, severity)`; cmd/atcr/main.go owns the exit-code mapping (0 pass / 1 threshold breached / 2 usage-config error) |
| Test Framework | testify | assert/require for exit code and severity checks |

## Related Files
- `cmd/atcr/main.go` - modify (created by the Phase 1 scaffold): centralized exit-code logic mapping severity threshold to exit code
- `cmd/atcr/reconcile.go` - create: `reconcile` cobra command with `--fail-on` flag
- `cmd/atcr/review.go` - modify: one-shot mode `atcr review --fail-on <severity>` runs review + reconcile + exit-code check
- `internal/reconcile/merger.go` - create: severity constants and threshold comparison logic
- `internal/reconcile/merger_test.go` - create: tests for severity threshold check

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [CLI Architecture](../documentation/cli-architecture.md) — Authoritative spec for centralized exit-code logic in `main()` (handlers return errors from `RunE`; the root maps them to exit codes). No `os.Exit` calls scattered across handlers.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — `--fail-on` exit-code gate section: "exits nonzero if any finding at or above the threshold survives reconciliation". Severity ordering CRITICAL > HIGH > MEDIUM > LOW; severity values from the closed enum.

### Spec alignment notes

- **Exit codes are exact and centralized**: `0` = pass (no findings at/above threshold), `1` = fail (finding at/above threshold), `2` = error (usage, config, missing file, malformed findings). Per `plan.md` Filesystem Discipline and CLI Architecture.
- **Severity threshold comparison is case-insensitive** at the input boundary; internally normalized to uppercase. Per `user-stories/03-ci-integration.md` original criterion #5.
- **Threshold includes the named severity**: `--fail-on HIGH` triggers on HIGH **or CRITICAL** findings (≥); `--fail-on MEDIUM` triggers on MEDIUM, HIGH, or CRITICAL.
- **Centralization invariant**: internal/reconcile exposes a pure helper `CountAtOrAbove(findings, severity)`; cmd/atcr/main.go owns the exit-code mapping (0 pass / 1 threshold breached / 2 usage-config error). `--fail-on` exit-code logic lives in `main()` after `Execute()` returns — not in any handler. The `RunE` pattern is the only way handlers signal outcome.
- **One-shot mode** (`atcr review --fail-on <severity>`): `cmd/atcr/review.go` invokes fan-out, then `atcr reconcile`, then the same threshold check. Total exit-code logic stays centralized in `main()` regardless of which command was invoked.

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

**Edge Case 5: Unknown severity value in findings.json**
- **Given** findings.json contains a finding with an unknown severity value (e.g. "BLOCKER")
- **When** the threshold check loads it
- **Then** loading fails with exit code 2 ("invalid severity in findings.json") — the enum is closed

## Error Conditions

**Error Scenario 1: Unknown flag value**
- Error message: `invalid severity threshold: "X" (must be CRITICAL, HIGH, MEDIUM, or LOW)`
- Exit code: 2

**Error Scenario 2: Findings file not found**
- Error message: `reconciled findings not found: run 'atcr review' first`
- Exit code: 2

## Performance Requirements
- **Response Time:** Exit-code check completes in <10ms after findings.json is loaded
- **Throughput:** Parses a 10MB findings.json in under 1 second

## Security Considerations
- **Input Validation:** Severity flag validated against enum before any file I/O
- **No external input:** Reads only local findings.json, no network calls

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture findings.json files for each severity combination (empty, LOW-only, MEDIUM+HIGH+CRITICAL, all levels)
**Mock/Stub Requirements:** None — pure logic on local file

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `--fail-on CRITICAL` returns 1 only when CRITICAL findings exist
- [x] `--fail-on HIGH` returns 1 when HIGH or CRITICAL findings exist
- [x] `--fail-on MEDIUM` returns 1 when MEDIUM, HIGH, or CRITICAL findings exist
- [x] `--fail-on LOW` returns 1 when any finding exists
- [x] Exit code 0 when no findings at/above threshold
- [x] Severity comparison is case-insensitive

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Exit-code logic centralized in main.go (single code path)
