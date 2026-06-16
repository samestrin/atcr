# Acceptance Criteria: --no-scorecard Flag Suppression

**Related User Story:** [01: Auto-emit Scorecard](../user-stories/01-auto-emit-scorecard.md)

## Acceptance Criteria Statement
When the `--no-scorecard` flag is passed to `atcr reconcile`, scorecard emission is skipped entirely and no records are written to the local store.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Flag | Go `flag` package or cobra/pflag | `--no-scorecard` bool flag |
| Emission Guard | Conditional check in reconcile hook | Early return when flag set |
| Test Framework | `go test` + `testify` | Integration test with flag |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go:32` — modify: register `--no-scorecard` flag; guard `scorecard.Emit()` call
- `internal/scorecard/store.go` — reference: `Emit` function checks enabled state

## Happy Path Scenarios
**Scenario 1: --no-scorecard suppresses all writes**
- **Given** `--no-scorecard` flag is passed to `atcr reconcile`
- **When** reconcile completes
- **Then** zero records are written to scorecard directory; no file created if absent; no append if exists

**Scenario 2: Without flag, emission proceeds normally**
- **Given** `--no-scorecard` is NOT passed
- **When** reconcile completes
- **Then** scorecard records are written as normal

**Scenario 3: Flag works with existing scorecard directory**
- **Given** `~/.config/atcr/scorecard/2026-06.jsonl` exists with prior records
- **When** `atcr reconcile --no-scorecard` runs
- **Then** existing file is untouched (no bytes added, no truncation)

## Edge Cases
**Edge Case 1: Flag combined with other flags**
- **Given** `--no-scorecard --verbose` passed together
- **When** reconcile completes
- **Then** scorecard suppressed; verbose output does not mention scorecard emission

**Edge Case 2: Flag with invalid value (--no-scorecard=maybe)**
- **Given** flag parser receives non-boolean value
- **When** CLI parses args
- **Then** Standard flag error: `invalid value "maybe" for flag -no-scorecard`

## Error Conditions
**Error Scenario 1: No error conditions — flag is a pure suppressor**
- No error state; flag silently suppresses emission

## Performance Requirements
- **Response Time:** Flag check is O(1) boolean; no measurable overhead
- **Throughput:** N/A — suppression means zero I/O

## Security Considerations
- **Input Validation:** Boolean flag only; standard CLI parser validation
- **Data Protection:** Suppression prevents any data write — privacy-preserving by default

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Run reconcile with and without flag; assert file state after each
**Mock/Stub Requirements:** Temp config dir; mock reconcile pipeline

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `--no-scorecard` flag registered on reconcile command
- [x] Zero records written when flag is set (verified by test)
- [x] Normal emission when flag is absent (verified by test)
- [x] Existing JSONL file unmodified when flag is set

**Manual Review:**
- [x] Code reviewed and approved
