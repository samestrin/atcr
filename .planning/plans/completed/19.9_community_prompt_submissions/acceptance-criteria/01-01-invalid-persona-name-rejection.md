# Acceptance Criteria: Invalid Persona Name Rejection

**Related User Story:** [01: Local Fixture-Gate Reuse and Submission Blocking](../user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra CLI subcommand (`RunE`) | `newPersonasSubmitCmd()` in cmd/atcr/personas.go |
| Test Framework | Go `testing` package | Table-driven, mirrors `TestPersonasRemove_*` traversal cases; testify is not used in this codebase |
| Key Dependencies | `internal/personas.validatePersonaName` (via `personaPath`), `github.com/spf13/cobra` | No new dependency introduced |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/personas.go` (modify) — add `newPersonasSubmitCmd()` alongside `newPersonasTestCmd` (line 298) and register it in `newPersonasCmd()`'s `cmd.AddCommand(...)` block (lines 106-113)
- `internal/personas/paths.go` (reference only) — `validatePersonaName` (line 42) and `personaPath` (line 72) are called verbatim, mirroring the validation order already used by `newPersonasRemoveCmd()` (personas.go:279-296)
- `cmd/atcr/personas_test.go` (modify) — add `TestPersonasSubmit_InvalidName` (or table-driven equivalent) using the `executeSplit` helper (line 35)

## Design References
- [Local Fixture-Gate Reuse (TestPersona)](../documentation/fixture-gate-reuse.md) — name/path validation conventions `submit` must reuse
- [Cobra Subcommand & Injectable-Seam Conventions](../documentation/cobra-subcommand-patterns.md) — where `newPersonasSubmitCmd()` registers and how CLI output is formatted

## Happy Path Scenarios
**Scenario 1: Well-formed, resolvable name proceeds past name validation**
- **Given** the persona name `security/owasp` matches `personaNameRe` and contains no path-traversal segments
- **When** `atcr personas submit security/owasp` is invoked
- **Then** `validatePersonaName`/`personaPath` return no error and control proceeds to the fixture gate (AC 01-03), producing no name-validation error on stderr

## Edge Cases
**Edge Case 1: Empty persona name**
- **Given** the command is invoked with an empty string argument (bypassing Cobra's `ExactArgs(1)` only if an empty string is technically supplied as the single arg)
- **When** `atcr personas submit ""` is invoked
- **Then** `validatePersonaName` returns `invalid persona name "": must not be empty`, `RunE` writes this to `cmd.ErrOrStderr()` and returns a non-nil error; no fork/PR/`gh` call occurs

**Edge Case 2: Path-traversal segment in name**
- **Given** the name `../../etc/passwd` or `foo/../bar` contains a `.` or `..` segment
- **When** `atcr personas submit "../../etc/passwd"` is invoked
- **Then** `validatePersonaName` returns an "invalid path segment" error, `RunE` writes it to stderr and returns non-nil; no filesystem path outside the personas directory is touched and no fork/PR/`gh` call occurs

**Edge Case 3: Absolute path name**
- **Given** the name is `/etc/passwd` (an absolute path)
- **When** `atcr personas submit /etc/passwd` is invoked
- **Then** `validatePersonaName` returns "must not be an absolute path", `RunE` writes it to stderr and returns non-nil; no fork/PR/`gh` call occurs

**Edge Case 4: Disallowed character in name**
- **Given** the name contains a character outside `[a-zA-Z0-9_/-]` (e.g. `bad name!` or `bad;rm`)
- **When** `atcr personas submit "bad;rm"` is invoked
- **Then** `validatePersonaName` returns "only letters, digits, '_', '-', and '/' are allowed", `RunE` writes it to stderr and returns non-nil; no shell/`gh` invocation occurs (defense against command-injection-shaped names)

## Error Conditions
**Error Scenario 1: Empty name**
- Error message: `invalid persona name "": must not be empty`
- Exit code: non-zero (`exitFailure`, matching the existing `personas test`/`remove` failure convention); written to stderr only, never stdout

**Error Scenario 2: Path traversal**
- Error message: `invalid persona name "../../etc/passwd": contains an invalid path segment`
- Exit code: non-zero; stderr only

**Error Scenario 3: Absolute path**
- Error message: `invalid persona name "/etc/passwd": must not be an absolute path`
- Exit code: non-zero; stderr only

**Error Scenario 4: Disallowed characters**
- Error message: `invalid persona name "bad;rm": only letters, digits, '_', '-', and '/' are allowed`
- Exit code: non-zero; stderr only

## Performance Requirements
- **Response Time:** Name validation must fail synchronously in-process (no I/O, no network) — under 10ms in practice, well before any filesystem or network call is attempted.
- **Throughput:** N/A (single-invocation CLI command; no concurrency requirement).

## Security Considerations
- **Authentication/Authorization:** None required for local validation; no credentials or `gh` auth are consulted at this stage (validation runs strictly before any GitHub interaction, per the story's Constraints).
- **Input Validation:** Reuse `validatePersonaName` (internal/personas/paths.go:42) and `personaPath` (internal/personas/paths.go:72) verbatim — do not reimplement or loosen the regex, the absolute-path check, or the `.`/`..` segment check. This closes the same path-traversal class of bug already fixed for `install`/`remove`.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A table of invalid names covering: empty string, `..` traversal (`../../etc/passwd`, `foo/../bar`), absolute path (`/etc/passwd`), and disallowed characters (`bad;rm`, `bad name`). Reuse the same traversal fixtures `newPersonasRemoveCmd()`'s existing tests already exercise where applicable.
**Mock/Stub Requirements:** None needed for this AC — validation fails before `personasFixtureRunner` or `personasClient` is touched; assert (via a stub/spy fixture runner and a spy `gh`-invocation flag, or simply the absence of any HTTP/exec calls in the test harness) that neither is invoked for any case in this AC.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `newPersonasSubmitCmd()` calls `validatePersonaName`/`personaPath` before any other logic, matching the validation-then-resolve order used by `newPersonasRemoveCmd()`
- [ ] Table-driven test covers empty name, path traversal, absolute path, and disallowed characters, asserting stderr content and non-zero exit for each
- [ ] Test asserts zero fork/PR/`gh`-related side effects occur when name validation fails

**Manual Review:**
- [ ] Code reviewed and approved
