# Acceptance Criteria: Verify Minimum Severity Registry Config

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Registry Config | Go struct in `internal/registry` | Optional `verify` block with `min_severity` string field |
| Pipeline Filter | Go logic in `internal/verify` | Skip findings whose severity is below configured floor |
| Test Framework | `go test` + `testify` | Table-driven tests |
| Key Dependencies | `internal/registry` (Registry, VerifyConfig), `internal/reconcile` (JSONFinding severity) | Existing severity ordering reused |

### Related Files

- `internal/registry/config.go` — `Registry` struct and registry YAML loading
- `internal/verify/select.go` / `internal/verify/pipeline.go` — orchestration that applies the severity floor
- `internal/reconcile/merge.go` — severity constants and ordering

## Happy Path Scenarios

**Scenario 1: Default `min_severity` is MEDIUM**
- **Given** a registry without an explicit `verify.min_severity` value
- **When** the verification pipeline evaluates findings
- **Then** findings with severity `LOW` skip verification and retain their v1 confidence; `MEDIUM` and `HIGH` findings are verified

**Scenario 2: Explicit `min_severity: HIGH` skips MEDIUM findings**
- **Given** a registry with `verify.min_severity: HIGH`
- **When** the verification pipeline evaluates findings
- **Then** only `HIGH` (and `CRITICAL` if present) findings are verified; `MEDIUM` and `LOW` findings are skipped

**Scenario 3: CLI `--min-severity` overrides registry config**
- **Given** a registry with `verify.min_severity: MEDIUM` and a CLI invocation `atcr verify --min-severity HIGH`
- **When** the verify command constructs `verify.Options`
- **Then** the CLI flag value `HIGH` takes precedence over the registry config

**Scenario 4: Skipped findings are recorded in verification.json**
- **Given** a review with findings below the configured `min_severity`
- **When** verification completes
- **Then** `verification.json` includes the configured `minSeverity` field, and skipped findings are not present in the `findings` array (they retain v1 confidence in `findings.json`)

## Edge Cases

**Edge Case 1: `min_severity` is empty or omitted in registry**
- **Given** a registry with `verify:` block present but no `min_severity` key
- **When** the config is loaded
- **Then** the effective value defaults to `"MEDIUM"`

**Edge Case 2: `min_severity` set to an invalid value**
- **Given** a registry with `verify.min_severity: BLOCKER`
- **When** the config is loaded or the verify command runs
- **Then** registry load fails with a clear validation error listing valid severity levels (`LOW`, `MEDIUM`, `HIGH`, `CRITICAL`)

**Edge Case 3: Finding severity is empty or unknown**
- **Given** a finding with empty or unrecognized severity
- **When** the severity floor is applied
- **Then** the finding is treated as below the floor and skipped (defensive — never crash on unknown severity)

## Error Conditions

**Error Scenario 1: Invalid `min_severity` in registry**
- Error message: `invalid verify.min_severity "BLOCKER": must be LOW, MEDIUM, HIGH, or CRITICAL`
- Behavior: Registry load fails; verification cannot run until config is fixed

## Performance Requirements
- **Response Time:** Severity comparison is O(1) per finding; negligible overhead
- **Throughput:** Filter applied once per finding before skeptic selection

## Security Considerations
- **Input Validation:** `min_severity` validated at registry load time; unknown severities skipped defensively
- **No external input:** Config value is operator-controlled, not user-provided at runtime

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Registry fixtures with `verify.min_severity` set to `LOW`, `MEDIUM`, `HIGH`
- Findings at each severity level

```go
func TestMinSeverityFloor(t *testing.T) {
    tests := []struct {
        name       string
        minSev     string
        findingSev string
        wantVerify bool
    }{
        {"default skips low", "MEDIUM", "LOW", false},
        {"default verifies medium", "MEDIUM", "MEDIUM", true},
        {"high skips medium", "HIGH", "MEDIUM", false},
        {"high verifies high", "HIGH", "HIGH", true},
    }
    // ...
}
```

## Definition of Done
**Auto-Verified:**
- [x] `go test ./internal/verify/... ./internal/registry/...` passes
- [x] `go vet ./...` clean
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] Registry schema supports optional `verify.min_severity` (default `MEDIUM`)
- [x] Findings below the floor skip verification and retain v1 confidence
- [x] CLI `--min-severity` overrides registry config
- [x] Invalid config values fail at load time with a clear error
- [x] Skipped findings are omitted from `verification.json` findings array

**Manual Review:**
- [x] Code reviewed and approved
