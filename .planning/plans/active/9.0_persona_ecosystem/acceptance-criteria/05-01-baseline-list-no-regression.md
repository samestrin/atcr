# Acceptance Criteria: Baseline List No Regression

**Related User Story:** [05: Corroboration Feedback via Persona Scores](../user-stories/05-corroboration-feedback.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `atcr personas list` without `--scores` |
| List function | `internal/personas` package | `List()` returns `[]PersonaMeta` |
| Test Framework | `go test` / `testify` | Table-driven unit + cobra integration tests |
| Key Dependencies | `github.com/spf13/cobra`, `text/tabwriter` | Cobra flag handling, column alignment |

## Related Files
- `internal/personas/list.go` - modify: add `--scores` boolean flag (default `false`); gate all score-related logic behind the flag so the zero-value path is unchanged
- `cmd/atcr/personas.go` - modify: bind the `--scores` flag on the `list` subcommand; verify no positional args or existing flags are altered
- `internal/personas/list_test.go` - modify: add `TestPersonasListBaseline` asserting output column headers and row format are identical to pre-story output when `--scores` is absent

## Happy Path Scenarios

**Scenario 1: list without --scores returns unchanged output**
- **Given** one or more personas are installed and the scorecard JSONL has existing data
- **When** the user runs `atcr personas list` (no flags)
- **Then** the output table contains exactly the columns present before this change (e.g., `NAME`, `SOURCE`, `DESCRIPTION`) and contains no `CORROBORATION` column

**Scenario 2: explicit false flag is identical to omitting the flag**
- **Given** any set of installed personas
- **When** the user runs `atcr personas list --scores=false`
- **Then** the output is byte-for-byte identical to `atcr personas list`

## Edge Cases

**Edge Case 1: No personas installed**
- **Given** the personas directory is empty or no personas are registered
- **When** the user runs `atcr personas list`
- **Then** the command exits 0 with an empty table or a "no personas installed" message and no `CORROBORATION` column

**Edge Case 2: Scorecard JSONL exists but --scores is not set**
- **Given** a populated scorecard JSONL file is present at `~/.config/atcr/scorecard.jsonl`
- **When** the user runs `atcr personas list`
- **Then** `scorecard.Aggregate()` is never called and the scorecard file is not read

## Error Conditions

**Error Scenario 1: personas list subcommand receives unknown flag**
- Error message: `"unknown flag: --<flag>"`
- Error code: cobra exits 1 with usage printed to stderr; this behavior must be unchanged

## Performance Requirements
- **Response Time:** `atcr personas list` (no flag) completes in under 200 ms regardless of scorecard file size, because `scorecard.Aggregate()` must not be called on this path
- **Throughput:** N/A — single interactive CLI invocation

## Security Considerations
- **Authentication/Authorization:** None required; read-only display of locally installed persona metadata
- **Input Validation:** No user-supplied input other than the boolean flag; Cobra handles flag parsing

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION  
**Test Data Requirements:** A fixture `[]PersonaMeta` slice with 2–3 entries; a populated scorecard fixture JSONL file (to confirm it is not read when flag is absent)  
**Mock/Stub Requirements:** Mock `scorecard.Aggregate()` (or inject a fake reader) to assert it is never invoked when `--scores` is false; use dependency injection or a package-level `aggregateFn` variable

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/personas/... ./cmd/atcr/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas list` output is byte-for-byte identical to its pre-story baseline (verified by snapshot test or golden file)
- [ ] `scorecard.Aggregate()` call count is zero when `--scores` flag is absent (verified by mock assertion)
- [ ] `TestPersonasListBaseline` passes in CI

**Manual Review:**
- [ ] Code reviewed and approved
