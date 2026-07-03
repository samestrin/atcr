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

**Scenario 2: Apply the single Go convenience default only when a `go.mod` is present at the repo root**
- **Given** a post-patch working tree with a `go.mod` file at the repository root and no `auto_fix.validate_command` configured
- **When** the validation entry point is invoked
- **Then** it uses the one built-in convenience default `["go", "build", "./..."]` — which applies ONLY because the Go project signal (`go.mod` at repo root) was detected — without requiring explicit configuration
- **And** there is NO hardcoded multi-language default table; detection is limited to the single `go.mod`-at-repo-root signal, and every other project type requires an operator-configured command (see Error Scenario 2)

**Scenario 3: Run against a scoped package/module directory**
- **Given** a working directory scoped to the affected package rather than the repository root
- **When** the validation entry point is invoked with that directory
- **Then** the command executes with that directory as its working directory (`exec.Cmd.Dir`), not the repository root

**Scenario 4: Validation command is sourced only from operator configuration, never from PR/diff/model content**
- **Given** a config with `auto_fix.validate_command` set by the operator (via `internal/registry` / project config) and a PR body, diff hunk, and model/LLM-generated summary that all contain attacker-controlled strings resembling shell commands
- **When** the validation entry point is invoked
- **Then** the argv passed to `exec.CommandContext` is derived ONLY from the operator-supplied config value (or the Go convenience default of Scenario 2), and NO value originating from the PR body, diff content, or model/LLM output can reach `exec.CommandContext`
- **And** the command is passed as an argv list, never a shell string, so no configured or injected value is interpreted by a shell

## Edge Cases
**Edge Case 1: Empty configured command**
- **Given** `auto_fix.validate_command` is present in config but is an empty list
- **When** the validation entry point is invoked
- **Then** it falls back to the Go convenience default IF a `go.mod` is present at the repo root; otherwise it hits the hard refusal of Error Scenario 2 rather than attempting to execute an empty argv

**Edge Case 4: No configured command and no `go.mod` at repo root**
- **Given** no `auto_fix.validate_command` configured and NO `go.mod` file at the repository root (e.g. a non-Go project)
- **When** the validation entry point is invoked
- **Then** no convenience default applies (the `go.mod` signal is absent) and the entry point hits the hard refusal of Error Scenario 2, testable in isolation via the `go.mod`-presence check alone

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

**Error Scenario 2: No configured command and no Go convenience default applies (no `go.mod` at repo root)**
- Error message: `"auto-fix requires a configured validate_command: no default validation command for this project type"`
- HTTP status / error code: N/A (Go `error`); `--auto-fix` must refuse to proceed rather than skip validation silently. The validation command is REQUIRED (operator-supplied); the ONLY exception is the single Go convenience default, which applies only when a `go.mod` exists at the repository root.

## Performance Requirements
- **Response Time:** Validation command execution (or timeout) completes within the configured timeout, default 2 minutes per invocation
- **Throughput:** Single validation run per `--auto-fix` fix cycle; no concurrent validation runs required by this AC

## Security Considerations
- **Authentication/Authorization:** N/A — local process execution only, no network or credential access introduced by this AC
- **Input Validation:** The configured command is an argv list (`[]string`), never a shell string, so no shell metacharacter injection is possible via config; working directory is resolved and validated to be within the repository before use
- **Command Source (Trust Boundary):** The validation command's argv is sourced ONLY from operator configuration (`internal/registry` / project config) or the single Go convenience default. It is NEVER derived from the PR body, diff/patch content, commit messages, or model/LLM-generated output. No untrusted, PR/diff-derived, or model-generated value may flow into `exec.CommandContext` (arguments or working directory)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven cases covering: configured command success, configured command failure, missing command with `go.mod` present at repo root (Go convenience default applies), missing command with NO `go.mod` at repo root (hard refusal), empty configured command, timeout expiry, command-not-found. Include a fixture repo root WITH a `go.mod` and one WITHOUT so the `go.mod`-presence detection signal is exercised in isolation. Include a case asserting that PR-body / diff / model-generated strings supplied alongside the config never reach the constructed argv (only the operator config value or Go default does)
**Mock/Stub Requirements:** Use real short-lived shell commands (e.g. `true`, `false`, `sleep`) in tests rather than mocking `exec.Cmd`, consistent with existing `internal/verify/exec_test.go` patterns; no network or filesystem mocks needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `runConfiguredValidation` (or equivalent) exists as a sibling function to `validateGoFixSyntax`, and `validateGoFixSyntax` itself is unmodified
- [ ] A missing/empty configured command falls back to the single Go convenience default (`go build ./...`) ONLY when a `go.mod` exists at the repo root; there is NO hardcoded multi-language default table; any other project with no configured command causes a hard refusal (Error Scenario 2)
- [ ] The validation argv is sourced only from operator config or the Go convenience default; a test asserts no PR-body/diff/model-derived value can reach `exec.CommandContext` (arguments or working directory), and the command is always an argv list, never a shell string
- [ ] Timeout is enforced via `context.WithTimeout` and produces a distinct timeout result

**Manual Review:**
- [ ] Code reviewed and approved
