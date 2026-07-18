# Acceptance Criteria: `ATCR_AXI_MAX_LINES` Environment Override with Fail-Open Parsing

**Related User Story:** [Story 3: AXI Pagination and Truncation Guarantees](../user-stories/03-axi-pagination-and-truncation-guarantees.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI (`cmd/atcr`) — env-var parsing function | New `axiMaxLinesFromEnv() int` mirroring `logLevelFromEnv`/`telemetryEnabledFromEnv` |
| Test Framework | `go test` with `t.Setenv` for isolated env-var manipulation | Captures `os.Stderr` output to assert exactly-one warning line |
| Key Dependencies | `strconv.Atoi`, `os.Getenv`, `strings.TrimSpace` (standard library only) | Same idiom as existing `ATCR_*` env parsers |

## Related Files
- `cmd/atcr/main.go` - modify: add `axiMaxLinesFromEnv() int` alongside `logLevelFromEnv` (line 288) and `telemetryEnabledFromEnv` (line 306), following their exact read-once-per-run, fail-open structure
- `cmd/atcr/github.go` - reference (line 66, `envOr`): flag-then-env fallback helper precedent for env-var resolution idiom
- `internal/report/pagination.go` - modify: accepts the resolved max-lines value from `axiMaxLinesFromEnv()` as the cap parameter (default 500) for the AC 03-01 truncation step

## Happy Path Scenarios
**Scenario 1: Unset env var uses the documented default**
- **Given** `ATCR_AXI_MAX_LINES` is not set in the environment
- **When** `atcr report --axi` runs against a payload larger than 500 lines
- **Then** the cap resolves to 500 (the documented default), no stderr warning is emitted, and truncation behaves per AC 03-01

**Scenario 2: Valid override changes the cap**
- **Given** `ATCR_AXI_MAX_LINES=50` is set in the process environment
- **When** `atcr report --axi` runs against a payload larger than 50 lines
- **Then** the cap resolves to 50, the payload is truncated at 50 lines, and no stderr warning is emitted

## Edge Cases
**Edge Case 1: Blank value falls open to default**
- **Given** `ATCR_AXI_MAX_LINES=""` (set but blank/whitespace-only)
- **When** the cap is resolved
- **Then** the cap falls open to 500 with exactly one `stderr` warning, matching `telemetryEnabledFromEnv`'s blank-value handling

**Edge Case 2: Non-numeric value falls open to default with a warning**
- **Given** `ATCR_AXI_MAX_LINES=notanumber`
- **When** the cap is resolved
- **Then** `strconv.Atoi` parsing fails, the cap falls open to 500, exactly one `stderr` warning line is written (e.g. `warning: unrecognized ATCR_AXI_MAX_LINES value "notanumber"; using default 500`), and the run exits 0 (no fatal error)

**Edge Case 3: Zero or negative value falls open to default with a warning**
- **Given** `ATCR_AXI_MAX_LINES=0` or `ATCR_AXI_MAX_LINES=-10`
- **When** the cap is resolved
- **Then** the value is treated as invalid (a non-positive cap is nonsensical), falls open to 500, and emits exactly one `stderr` warning — consistent with the "fail open, never fatal" posture

**Edge Case 4: Value read exactly once per run**
- **Given** `ATCR_AXI_MAX_LINES=50` is set for a run that renders AXI output multiple times within the same process (e.g. `atcr review --axi` with live output plus a final summary)
- **When** the run executes
- **Then** `axiMaxLinesFromEnv()` is called once and the resolved value (50) is reused consistently across every AXI-mode emission point in that run, never re-parsed mid-run

## Error Conditions
**Error Scenario 1: Invalid value must never produce a fatal error or non-zero exit**
- **Given** `ATCR_AXI_MAX_LINES=garbage`
- **When** `atcr report --axi` runs
- **Then** the command completes successfully (exit code 0, assuming no unrelated failures), the fallback default (500) is applied, and exactly one warning line appears on stderr — never a hard crash, panic, or usage error

**Error Scenario 2: Warning is emitted exactly once, not per truncation call**
- **Given** an invalid `ATCR_AXI_MAX_LINES` value and a run that invokes the AXI renderer/truncation step multiple times (e.g. multiple files or multiple live-output flushes)
- **When** the run executes
- **Then** the stderr warning appears exactly once for the entire run, not once per renderer invocation — confirming the env var is parsed once per run, not once per call site

## Performance Requirements
- **Response Time:** Env-var parsing is a single `os.Getenv`/`strconv.Atoi` call resolved once per process — negligible, sub-microsecond overhead.
- **Throughput:** No measurable impact; this is a one-time startup-path resolution, not a per-payload cost.

## Security Considerations
- **Authentication/Authorization:** N/A — local environment variable, no external trust boundary.
- **Input Validation:** Arbitrary/hostile env-var content (extremely large integers, non-ASCII, control characters) must not crash the parser or the run; `strconv.Atoi` bounds-checks integer overflow and returns an error for any malformed input, which is handled by the fail-open path in Edge Cases 2-3. An excessively large valid integer (e.g. `ATCR_AXI_MAX_LINES=999999999`) is accepted as a valid override — it is an operator's explicit choice to disable practical truncation, not a security boundary this AC enforces.

## Test Implementation Guidance
**Test Type:** UNIT (env-var parsing function) + INTEGRATION (end-to-end CLI run asserting stderr output and exit code)
**Test Data Requirements:** Env-var values: unset, blank, valid positive integer, non-numeric string, zero, negative integer, very large integer.
**Mock/Stub Requirements:** `t.Setenv` for isolated per-test env-var state (avoids cross-test pollution); capture `os.Stderr` via a redirected pipe or dependency-injected writer to count warning lines exactly.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `axiMaxLinesFromEnv()` follows the `logLevelFromEnv`/`telemetryEnabledFromEnv` fail-open structure exactly
- [ ] Unset/blank/invalid/non-positive values fall open to default 500 with exactly one stderr warning, never a fatal error or non-zero exit
- [ ] Valid override (e.g. `ATCR_AXI_MAX_LINES=50`) changes the cap and produces no warning
- [ ] Env var is read once per run and the resolved value is reused consistently across all AXI-mode emission points

**Manual Review:**
- [ ] Code reviewed and approved
