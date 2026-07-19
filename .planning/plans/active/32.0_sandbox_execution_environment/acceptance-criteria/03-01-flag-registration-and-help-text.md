# Acceptance Criteria: `--no-sandbox` Flag Registration and Help Text

**Related User Story:** [03: `--no-sandbox` Opt-Out Flag with CLI Security Warnings](../user-stories/03-no-sandbox-opt-out-flag.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra CLI flag (`*cobra.Command.Flags().Bool`) | Registered inside `addAutoFixFlags` alongside the existing `--auto-fix` bool |
| Test Framework | `go test` + `cobra`'s `Flags()` introspection (no testify dependency elsewhere in this package) | Assert flag existence, type, and default via `cmd.Flags().Lookup("no-sandbox")` |
| Key Dependencies | `github.com/spf13/cobra` | No new package; `addAutoFixFlags` already imports cobra |

## Related Files
- `cmd/atcr/autofix.go` - modify: add `cmd.Flags().Bool("no-sandbox", false, ...)` inside `addAutoFixFlags` (currently lines 43-55), immediately after the existing `--auto-fix` flag registration
- `cmd/atcr/autofix_test.go` - modify/create: add a test asserting the flag is registered, defaults to `false`, and is reachable via `cmd.Flags().GetBool("no-sandbox")`
- `cmd/atcr/review.go` - reference only: confirms `addAutoFixFlags(cmd)` is called from `newReviewCmd` (line 92), so `--no-sandbox` becomes available on `atcr review` without further wiring in this file

## Happy Path Scenarios
**Scenario 1: Flag is registered with the correct name, type, and default**
- **Given** `addAutoFixFlags(cmd)` has been called on a fresh `*cobra.Command`
- **When** the test looks up `cmd.Flags().Lookup("no-sandbox")`
- **Then** the flag exists, its `Value.Type()` is `"bool"`, and its `DefValue` is `"false"`

**Scenario 2: Flag is readable via `GetBool` before and after parsing**
- **Given** a command with `addAutoFixFlags` applied and no CLI arguments parsed
- **When** `cmd.Flags().GetBool("no-sandbox")` is called
- **Then** it returns `(false, nil)` — the flag is present and unset, not merely absent

**Scenario 3: Passing `--no-sandbox` on the command line sets the flag to true**
- **Given** `atcr review --auto-fix --no-sandbox` is parsed by cobra
- **When** `cmd.Flags().GetBool("no-sandbox")` is read after `cmd.Execute()`/`ParseFlags()`
- **Then** it returns `(true, nil)`

## Edge Cases
**Edge Case 1: `--no-sandbox` passed without `--auto-fix`**
- **Given** `atcr review --no-sandbox` (no `--auto-fix`) is parsed
- **When** the review command runs
- **Then** the flag parses successfully (cobra never rejects an unused bool flag) and has no behavioral effect — the story's constraint that `--no-sandbox` is "meaningless without `--auto-fix`" is enforced by call-site logic in `validateAutoFixBackend`/`runAutoFix` (AC 03-02), not by flag registration itself; this AC only proves the flag parses cleanly in isolation

**Edge Case 2: Flag help text is present and non-empty**
- **Given** the registered flag
- **When** the test reads `cmd.Flags().Lookup("no-sandbox").Usage`
- **Then** the string is non-empty and contains language stating plainly that passing it disables container isolation and runs LLM-generated validation commands directly on the host (matching the story's Specific criterion)

## Error Conditions
**Error Scenario 1: Malformed boolean value on the CLI**
- **Given** `atcr review --auto-fix --no-sandbox=notabool`
- **When** cobra parses flags
- **Then** cobra itself returns its standard `invalid argument "notabool" for "--no-sandbox" flag: strconv.ParseBool` parse error before `RunE` executes — this is stock `pflag` behavior, not custom code, and requires no additional handling in `addAutoFixFlags`

## Performance Requirements
- **Response Time:** Flag registration is a single in-memory `Flags().Bool(...)` call with no I/O; negligible (<1ms), same cost class as the adjacent `--auto-fix` flag registration
- **Throughput:** N/A — registered once per command construction, not per-run

## Security Considerations
- **Authentication/Authorization:** N/A — flag registration carries no credential or authorization surface
- **Input Validation:** The flag help text itself is a security control: it must not understate the risk (e.g. must not read merely "skip sandbox" but must name that validation runs unsandboxed against untrusted/LLM-generated code); wording is asserted by substring match in the test, not just non-emptiness

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A bare `*cobra.Command{}` constructed in-test (mirroring existing patterns in `cmd/atcr/autofix_test.go` if present, else a minimal `&cobra.Command{Use: "review"}`); no filesystem or network fixtures needed
**Mock/Stub Requirements:** None — `addAutoFixFlags` has no external dependencies to stub; this is pure cobra flag-registration testing

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `cmd.Flags().Lookup("no-sandbox")` returns a non-nil flag of type `bool` with `DefValue == "false"`
- [ ] Help text substring-matches language about disabling container isolation and running validation directly on the host
- [ ] `--no-sandbox` parses successfully both with and without `--auto-fix` present on the same invocation
- [ ] Existing `--auto-fix`-only invocations (no `--no-sandbox`) remain byte-identical in behavior (regression check against Story 1's default-sandboxed path)

**Manual Review:**
- [ ] Code reviewed and approved
