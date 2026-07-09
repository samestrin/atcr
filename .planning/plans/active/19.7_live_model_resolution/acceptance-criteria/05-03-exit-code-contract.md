# Acceptance Criteria: Exit-Code Contract (0 / 1 / 2)

**Related User Story:** [05: `atcr models check` Drift Report](../user-stories/05-atcr-models-check-drift-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra `RunE` return convention + explicit `os.Exit`/`SilenceUsage` handling at the command boundary | Distinguishes "conditions found" from "command/usage failure" per the user story's exit-code contract |
| Test Framework | Go `testing` + `testify`, asserting process/command exit codes via Cobra's `Execute()` error return and a thin `main`-level exit-code mapping test | Mirrors existing exit-code conventions elsewhere in `cmd/atcr` (e.g. `doctor`) |
| Key Dependencies | `github.com/spf13/cobra`, stdlib `os` | No new external dependency |

## Related Files
- `cmd/atcr/models.go` - create: `check`'s `RunE` returns a distinguishable sentinel/typed error (or sets a result on the command) when one or more conditions are found, separate from a genuine usage/execution error, so the exit-code mapping layer (`main.go` or a shared helper) can tell the two apart.
- `cmd/atcr/main.go` - modify: reuse or extend the existing top-level exit-code mapping (wherever `root.Execute()`'s error is translated to `os.Exit(code)`) so a "conditions found" result from `models check` maps to exit code `1` and a genuine usage/failure error maps to exit code `2`, while a clean run (no conditions) maps to `0`.
- `cmd/atcr/models_test.go` - create: tests invoking the command and asserting the exact exit code for each of the three states, run as a subprocess (`exec.Command` on the built binary, or Cobra's `Execute()` return value inspected against the exit-code mapping function directly) so the assertion covers the real process boundary, not just an internal return value.

## Happy Path Scenarios
**Scenario 1: No conditions found exits 0**
- **Given** every installed persona's resolved lock is up to date, non-deprecated, and present in the catalog
- **When** `atcr models check` runs (with or without `--json`)
- **Then** the process exits with code `0`

**Scenario 2: One or more conditions found exits 1**
- **Given** at least one installed persona triggers a newer-member, deprecation, or missing-slug condition
- **When** `atcr models check` runs (with or without `--json`)
- **Then** the process exits with code `1`, distinctly from both the clean (`0`) and failure (`2`) states

**Scenario 3: Usage error exits 2**
- **Given** the command is invoked with an invalid flag or unrecognized argument (e.g. `atcr models check --not-a-real-flag`)
- **When** the command is executed
- **Then** the process exits with code `2` and does not attempt to compute or print a drift report

## Edge Cases
**Edge Case 1: Combination of found conditions and a per-persona read failure**
- **Given** one persona triggers a valid drift condition while a different persona's lock cannot be read (an internal per-persona failure, not a usage error)
- **When** `atcr models check` runs
- **Then** the process still exits `1` (conditions were found) rather than `2`, since a per-persona internal read failure reported alongside valid findings is not a usage error — the per-persona failure is surfaced via `cmd.ErrOrStderr()` (per AC 05-01's Error Scenario 1) without escalating the exit code past `1`

**Edge Case 2: `--json` mode preserves the same exit-code contract as human-readable mode**
- **Given** the same three states from the Happy Path scenarios
- **When** each is run with `--json` instead of the default output
- **Then** the exit codes are identical to the non-JSON runs (`0`/`1`/`2` respectively) — the output format never changes the exit-code semantics

## Error Conditions
**Error Scenario 1: Catalog snapshot file is missing or unreadable (not a usage error, but a command failure)**
- Error message: `"failed to load catalog snapshot: %w"`
- HTTP status / error code: exit code `2` — this is a command/environment failure distinct from "conditions found," since no drift report could be computed at all

**Error Scenario 2: Invalid flag combination or unrecognized subcommand**
- Error message: Cobra's standard "unknown flag" / "unknown command" error text
- HTTP status / error code: exit code `2`

## Performance Requirements
- **Response Time:** Exit-code determination is O(1) after the drift-report computation completes; no additional pass over the findings beyond checking `len(findings) > 0`.
- **Throughput:** Not applicable — single boolean-like decision per invocation.

## Security Considerations
- **Authentication/Authorization:** Not applicable.
- **Input Validation:** Cobra's own flag-parsing validation handles malformed flags before `RunE` executes; this AC only maps that existing validation failure to the documented exit code `2`.

## Test Implementation Guidance
**Test Type:** INTEGRATION — invoking the actual command boundary (either via a built-binary subprocess test or Cobra's `Execute()` error inspected through the same exit-code mapping function used by `main()`), so the test proves the real process-exit-code contract rather than only an internal function's return value
**Test Data Requirements:** Same fixture set as AC 05-01/05-02 for the 0/1 states; an invalid-flag invocation and a missing/corrupt catalog-snapshot fixture for the 2 state
**Mock/Stub Requirements:** A temp-directory catalog snapshot fixture that can be deliberately deleted/corrupted to trigger Error Scenario 1 without touching the real checked-in `testdata/catalog_snapshot.json`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Clean run (no conditions) exits `0`
- [ ] One or more conditions found exits `1`, including when combined with a non-fatal per-persona read failure
- [ ] Usage errors and unrecoverable command failures (e.g. unreadable catalog snapshot) exit `2`, never conflated with the "conditions found" `1` state
- [ ] Exit-code contract is identical between default and `--json` output modes

**Manual Review:**
- [ ] Code reviewed and approved
