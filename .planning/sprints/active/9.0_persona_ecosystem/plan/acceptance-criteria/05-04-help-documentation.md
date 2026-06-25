# Acceptance Criteria: Help Documentation

**Related User Story:** [05: Corroboration Feedback via Persona Scores](../user-stories/05-corroboration-feedback.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI help text | Go / Cobra | Flag description string on `--scores` flag registration |
| Flag registration | `cmd/atcr/personas.go` | `listCmd.Flags().BoolP("scores", "", false, "<description>")` |
| Test Framework | `go test` / `testify` | Assert `--help` output contains expected flag description |
| Key Dependencies | `github.com/spf13/cobra` | Cobra generates help from flag description automatically |

## Related Files
- `cmd/atcr/personas.go` - modify: register `--scores` flag with a human-readable description string explaining that it adds a corroboration-rate column sourced from past review runs
- `internal/personas/list_test.go` - modify: add `TestPersonasListHelpContainsScoresFlag` that runs `personas list --help` and asserts the output contains `--scores` and a non-empty description

### Related Files (from codebase-discovery.json)

- `cmd/atcr/personas.go` — modify: register `--scores` flag description
- `internal/personas/list_test.go` — modify: add help-text assertion test
- `cmd/atcr/main.go:128` — root command registration

## Happy Path Scenarios

**Scenario 1: --help output includes --scores flag description**
- **Given** the `atcr personas list` subcommand is registered with the `--scores` flag
- **When** the user runs `atcr personas list --help`
- **Then** the help output contains the string `--scores` and a description such as `"show corroboration rate for each persona from past review runs"`

**Scenario 2: Flag description communicates n/a behavior**
- **Given** the `--scores` flag description is set
- **When** the user reads `atcr personas list --help`
- **Then** the description indicates that personas without run history display `n/a` (either inline or via the extended help block)

## Edge Cases

**Edge Case 1: --help is invoked before any personas are installed**
- **Given** no personas are installed
- **When** the user runs `atcr personas list --help`
- **Then** the help text still displays the `--scores` flag; the command does not attempt to read the persona directory or scorecard file on `--help` invocation

**Edge Case 2: Cobra auto-generates -h shorthand**
- **Given** the flag is registered as `--scores` without a shorthand character
- **When** the user runs `atcr personas list -h`
- **Then** the same help output is shown (Cobra default behavior; no explicit shorthand registration needed)

## Error Conditions

**Error Scenario 1: Flag registration conflict**
- If a future flag named `--scores` is registered elsewhere in the `list` subcommand, Cobra panics at startup
- Mitigation: test suite catches this at `init()` time via `TestMain`; no runtime user-facing error message applies to this scenario

## Performance Requirements
- **Response Time:** `--help` output renders in under 50 ms (Cobra generates it synchronously without reading any files)
- **Throughput:** N/A — single interactive CLI invocation

## Security Considerations
- **Authentication/Authorization:** Help output contains no secrets; flag description is static string literal
- **Input Validation:** N/A — help flag is handled entirely by Cobra before any user input is processed

## Test Implementation Guidance
**Test Type:** UNIT  
**Test Data Requirements:** None beyond the compiled binary or Cobra command tree  
**Mock/Stub Requirements:** None; test executes `listCmd.UsageString()` or `listCmd.HelpFunc()(listCmd, nil)` and captures output with `bytes.Buffer` to assert string containment

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `TestPersonasListHelpContainsScoresFlag` passes: `--help` output contains `--scores` and a non-empty description string
- [x] Description string references `n/a` behavior for personas without run history

**Manual Review:**
- [x] Code reviewed and approved
