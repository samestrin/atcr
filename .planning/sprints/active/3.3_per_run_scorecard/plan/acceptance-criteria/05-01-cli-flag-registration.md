# Acceptance Criteria: CLI Flag Registration & Help Text

**Related User Story:** [05: Suppress Emission](../user-stories/05-suppress-emission.md)

## Acceptance Criteria Statement
The `--no-scorecard` boolean flag is registered on the `atcr reconcile` command, defaults to `false`, appears in `--help` output, and rejects non-boolean or misspelled flag names.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Framework | Cobra (`github.com/spf13/cobra`) | `--no-scorecard` boolean flag on `reconcileCmd` |
| Flag Binding | `cobra.Flags().BoolVarP` | Bind to a local `bool` variable in the command |
| Help Text | Cobra auto-generated | Flag description string appears in `--help` output |
| Test Framework | `go test` + `testify/assert` | Verify flag existence and help text content |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go` — modify: register `--no-scorecard` boolean flag on reconcile subcommand
- `cmd/atcr/reconcile_test.go` — create: verify flag is registered, help text contains expected description

## Happy Path Scenarios

**Scenario 1: Flag appears in `atcr reconcile --help` output**
- **Given** the `atcr` binary is built with the `--no-scorecard` flag
- **When** `atcr reconcile --help` is executed
- **Then** the output contains a line matching `--no-scorecard` followed by a description such as `Skip writing scorecard records to the local store`

**Scenario 2: Flag is a boolean (no value required)**
- **Given** the `atcr` binary is built
- **When** `atcr reconcile --no-scorecard` is executed (without `=true` or `=false`)
- **Then** the flag is parsed as `true`; reconcile runs normally with suppression enabled

**Scenario 3: Flag can be explicitly set to false**
- **Given** the `atcr` binary is built
- **When** `atcr reconcile --no-scorecard=false` is executed
- **Then** the flag is parsed as `false`; reconcile emits scorecard records normally (default behavior)

## Edge Cases

**Edge Case 1: Flag name is exact — no aliases accepted**
- **Given** the `atcr` binary is built
- **When** `atcr reconcile --no-scorecards` (typo, extra `s`) is executed
- **Then** the command exits with code 1 and prints `unknown flag: --no-scorecards`

**Edge Case 2: Misspelled flag `--skip-scorecard` rejected**
- **Given** the `atcr` binary is built
- **When** `atcr reconcile --skip-scorecard` is executed
- **Then** the command exits with code 1 and prints `unknown flag: --skip-scorecard`

## Error Conditions

**Error Scenario 1: Flag with non-boolean value rejected**
- **Given** the `atcr` binary is built
- **When** `atcr reconcile --no-scorecard=maybe` is executed
- **Then** the command exits with code 1 and prints an invalid value error from Cobra's flag parser

## Performance Requirements
- **Flag parsing:** Adding the boolean flag adds < 1ms to CLI startup time

## Security Considerations
- **No security implications:** The flag is a runtime-only boolean; it is not persisted, not written to config, and not included in any output artifacts

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- None — tests operate on the Cobra command definition directly

**Mock/Stub Requirements:**
- No mocks needed. Access `reconcileCmd.Flags().Lookup("no-scorecard")` and assert non-nil, correct usage string, and default value `false`
- For help text test: capture `reconcileCmd` help output via `cmd.Execute()` with `--help` argument and assert the flag description is present

## Definition of Done

### Automated Tests
- [ ] Unit test verifies `--no-scorecard` flag is registered on `reconcileCmd`
- [ ] Unit test verifies flag default value is `false`
- [ ] Unit test verifies help text contains `--no-scorecard` with a clear description
- [ ] Unit test verifies `--no-scorecards` (typo) is rejected with exit code 1

### Story-Specific
- [ ] Flag description is discoverable and unambiguous in `--help` output
- [ ] Flag is a pure boolean — no string value parsing required

### Manual Verification
- [ ] `atcr reconcile --help` output reviewed for clarity and consistency with other flags
