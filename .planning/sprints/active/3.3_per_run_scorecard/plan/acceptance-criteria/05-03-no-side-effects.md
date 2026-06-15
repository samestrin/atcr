# Acceptance Criteria: Default Behavior Preserved & No Side Effects

**Related User Story:** [05: Suppress Emission](../user-stories/05-suppress-emission.md)

## Acceptance Criteria Statement
Passing `--no-scorecard` does not alter reconcile analysis, corroboration, summary output, `summary.json` content, or exit code; it only suppresses scorecard emission.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Framework | Cobra (`github.com/spf13/cobra`) | Flag does not alter any non-scorecard reconcile behavior |
| Reconcile Engine | `internal/reconcile` | No changes to analysis, corroboration, or summary logic |
| Summary Output | `summary.json` | Content identical regardless of `--no-scorecard` |
| Test Framework | `go test` + `testify/assert` | Byte-level comparison of outputs with/without flag |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go` — modify: ensure `--no-scorecard` only affects scorecard emission path
- `internal/reconcile/reconcile.go` — reference: confirm no changes to reconcile logic
- `cmd/atcr/reconcile_test.go` — create: verify reconcile output and exit code are identical with/without flag

## Happy Path Scenarios

**Scenario 1: Reconcile summary output is identical with `--no-scorecard`**
- **Given** a test fixture repo processed by `atcr reconcile`
- **When** the run is executed with and without `--no-scorecard`
- **Then** the stdout output (summary table, finding counts, corroboration results) is byte-identical between the two runs

**Scenario 2: `summary.json` content is unchanged**
- **Given** reconcile produces a `summary.json` as part of its normal output
- **When** the run is executed with `--no-scorecard`
- **Then** `summary.json` contains no scorecard-related fields; its content is identical to a run without the flag

**Scenario 3: Reconcile exit code is unaffected**
- **Given** a test fixture that reconcile processes successfully (exit code 0)
- **When** the run is executed with `--no-scorecard`
- **Then** exit code is 0 (same as without the flag)

**Scenario 4: Reconcile failure exit code is unaffected**
- **Given** a test fixture that causes reconcile to fail (non-zero exit code)
- **When** the run is executed with `--no-scorecard`
- **Then** exit code is identical to a run without the flag; the failure reason is unchanged

**Scenario 5: Analysis, corroboration, and all non-scorecard behavior proceeds normally**
- **Given** a test fixture with findings that require corroboration
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** all analysis steps run; corroboration logic executes; findings are reported; the only difference is that no scorecard records are written

## Edge Cases

**Edge Case 1: Flag does not appear in `summary.json` or scorecard records**
- **Given** reconcile produces both stdout output and a `summary.json`
- **When** `--no-scorecard` is passed
- **Then** neither the stdout output nor `summary.json` contains any reference to scorecard suppression; the flag leaves no trace in any output artifact

**Edge Case 2: Concurrent reconcile runs — one with flag, one without**
- **Given** two reconcile runs execute concurrently against different fixtures
- **When** one run uses `--no-scorecard` and the other does not
- **Then** the suppressed run writes zero records; the non-suppressed run writes its records independently; no race conditions or cross-contamination occur (each run has its own `suppress` boolean)

**Edge Case 3: Flag combined with other reconcile flags**
- **Given** `atcr reconcile` supports other flags (e.g., `--verbose`, `--model`)
- **When** `--no-scorecard` is combined with other flags
- **Then** all other flags function normally; `--no-scorecard` only suppresses scorecard emission

## Error Conditions

**Error Scenario 1: Reconcile fails mid-execution with `--no-scorecard`**
- **Given** reconcile encounters an error during analysis (before the post-completion hook)
- **When** the run exits with a non-zero exit code
- **Then** no scorecard records are written (same as without the flag — emission is a post-completion step); the error output does not mention scorecard

## Performance Requirements
- **No overhead on reconcile logic:** The flag check occurs only at the scorecard emission hook; zero impact on analysis, corroboration, or summary generation performance

## Security Considerations
- **No information leakage:** The flag does not cause any additional logging, error messages, or debug output that could reveal internal state
- **No side channels:** The flag is a pure suppression toggle with no persistence, config writes, or filesystem side effects

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:**
- A test fixture repo that reconcile processes successfully
- Capture full stdout, stderr, and `summary.json` for comparison

**Mock/Stub Requirements:**
- Run reconcile twice against the same fixture: once with `--no-scorecard`, once without
- Capture stdout/stderr as byte slices and assert `bytes.Equal(runWithFlag.stdout, runWithoutFlag.stdout)`
- Capture `summary.json` content and assert identical
- Assert exit codes are identical

## Definition of Done

### Automated Tests
- [ ] Integration test: stdout output is byte-identical with/without `--no-scorecard`
- [ ] Integration test: `summary.json` content is identical with/without the flag
- [ ] Integration test: exit code is identical with/without the flag (both success and failure cases)
- [ ] Integration test: no scorecard-related text appears in stdout/stderr when `--no-scorecard` is passed

### Story-Specific
- [ ] `--no-scorecard` has no effect on any reconcile logic outside the scorecard emission hook
- [ ] The flag is not persisted in any form (config, summary.json, scorecard records)
- [ ] Error paths do not reference scorecard status

### Manual Verification
- [ ] Run `atcr reconcile` and `atcr reconcile --no-scorecard` side-by-side; diff the output to confirm only the JSONL store differs
