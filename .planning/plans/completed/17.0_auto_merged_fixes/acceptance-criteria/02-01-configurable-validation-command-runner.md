# Acceptance Criteria: Configurable Validation Command Runner

**Related User Story:** [02: Configurable Local Validation](../user-stories/02-configurable-local-validation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function (`internal/verify`) | Sibling to `validateGoFixSyntax` in `syntaxguard.go`, not a modification of it |
| Test Framework | Go `testing` package, table-driven tests | Follows existing `internal/verify/*_test.go` conventions |
| Key Dependencies | `os/exec`, `context` (`context.WithTimeout`), `internal/registry` (config struct) | No new third-party dependencies |

### Related Files (from codebase-discovery.json)
- `internal/verify/localvalidate.go` - create: new `runConfiguredValidation` (or equivalently named) entry point that builds an `exec.CommandContext` from a configured command, working directory, and timeout, and runs it against the post-patch working tree
- `internal/verify/syntaxguard.go` - reference only (not modified): documents the conservative-failure philosophy this new function extends into a language-independent, post-apply gate
- `internal/registry/autofix.go` - create (or extend an existing config file): defines the `[auto_fix]` (or similarly named) config block holding the validation command and timeout, following the `SandboxConfig` pattern in `internal/registry/sandbox.go`
- `internal/verify/localvalidate_test.go` - create: unit tests covering command construction, default command selection, and timeout wiring

## Happy Path Scenarios
**Scenario 1: Run a user-supplied validation command successfully**
- **Given** a post-patch working tree and a config with `auto_fix.validate_command: ["go", "build", "./..."]`
- **When** the validation entry point is invoked with the repository root as the working directory
- **Then** it runs `go build ./...` via `exec.CommandContext`, waits for completion, and returns a result with exit code `0`

**Scenario 2: Fall back to a sane per-language default when no command is configured**
- **Given** a post-patch working tree in a Go module and no `auto_fix.validate_command` configured
- **When** the validation entry point is invoked
- **Then** it uses a built-in default command (e.g. `go build ./...`) appropriate to the detected project type, without requiring any explicit configuration

**Scenario 3: Run against a scoped package/module directory**
- **Given** a working directory scoped to the affected package rather than the repository root
- **When** the validation entry point is invoked with that directory
- **Then** the command executes with that directory as its working directory (`exec.Cmd.Dir`), not the repository root

## Edge Cases
**Edge Case 1: Empty configured command**
- **Given** `auto_fix.validate_command` is present in config but is an empty list
- **When** the validation entry point is invoked
- **Then** it falls back to the built-in default command rather than attempting to execute an empty argv

**Edge Case 2: Command exceeds the configured timeout**
- **Given** a configured timeout of 2 minutes and a validation command that runs longer
- **When** the validation entry point is invoked
- **Then** the command's process is killed via `context.WithTimeout` cancellation and the result reports a timeout failure distinct from a normal non-zero exit

**Edge Case 3: Very large stdout/stderr output**
- **Given** a validation command that produces megabytes of output (e.g. verbose build logs)
- **When** the command runs to completion
- **Then** captured output is bounded (e.g. truncated past a fixed cap) so a pathological command cannot exhaust memory, with a marker indicating truncation occurred

## Error Conditions
**Error Scenario 1: Configured command binary not found / not executable**
- Error message: `"auto-fix validation command not found or not executable: <command>: <underlying exec error>"`
- HTTP status / error code: N/A (Go `error`); caller treats this as a distinct "cannot start" error class, separate from a completed run's non-zero exit

**Error Scenario 2: No sane default exists for the detected project type and no command is configured**
- Error message: `"auto-fix requires a configured validate_command: no default validation command for this project type"`
- HTTP status / error code: N/A (Go `error`); `--auto-fix` must refuse to proceed rather than skip validation silently

## Performance Requirements
- **Response Time:** Validation command execution (or timeout) completes within the configured timeout, default 2 minutes per invocation
- **Throughput:** Single validation run per `--auto-fix` fix cycle; no concurrent validation runs required by this AC

## Security Considerations
- **Authentication/Authorization:** N/A — local process execution only, no network or credential access introduced by this AC
- **Input Validation:** The configured command is an argv list (`[]string`), never a shell string, so no shell metacharacter injection is possible via config; working directory is resolved and validated to be within the repository before use

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven cases covering: configured command success, configured command failure, missing command (default fallback), empty configured command, timeout expiry, command-not-found
**Mock/Stub Requirements:** Use real short-lived shell commands (e.g. `true`, `false`, `sleep`) in tests rather than mocking `exec.Cmd`, consistent with existing `internal/verify/exec_test.go` patterns; no network or filesystem mocks needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `runConfiguredValidation` (or equivalent) exists as a sibling function to `validateGoFixSyntax`, and `validateGoFixSyntax` itself is unmodified
- [ ] A missing/empty configured command falls back to a per-language default; a missing default with no config causes a hard refusal
- [ ] Timeout is enforced via `context.WithTimeout` and produces a distinct timeout result

**Manual Review:**
- [ ] Code reviewed and approved
