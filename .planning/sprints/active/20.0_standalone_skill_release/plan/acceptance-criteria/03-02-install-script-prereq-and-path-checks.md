# Acceptance Criteria: Install Script Prerequisite and PATH Checks

**Related User Story:** [3: Install Script](../user-stories/03-install-script.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | POSIX/bash shell script | Same `install.sh` file as AC 03-01; this AC covers its guard-clause and PATH-warning logic |
| Test Framework | bats-core (or manual bash test harness) | Verifies exit codes and stderr/stdout message content under controlled `PATH`/`command -v go` conditions |
| Key Dependencies | `command -v go`, `go env GOPATH`, `$PATH` string matching | All POSIX/bash builtins plus standard `go env` subcommand; no external libraries |

### Related Files (from codebase-discovery.json)

- `install.sh` — modify: add the `command -v go` prerequisite guard and the `$(go env GOPATH)/bin` vs `$PATH` warning check, per the four-step structure in the story's Implementation Notes
- `.planning/plans/active/20.0_standalone_skill_release/documentation/install-script-conventions.md` — reference only: defines the Installation Targets table (`$(go env GOPATH)/bin/atcr` primary target, PATH check requirement) this AC implements
- `examples/ci-gate.sh` — reference only: style precedent for `set -euo pipefail` and explicit stderr failure messages

## Design References

- [Install Script Conventions](../documentation/install-script-conventions.md) — prerequisite, PATH-check, and installation-target requirements

## Happy Path Scenarios
**Scenario 1: Go present and GOPATH/bin already on PATH**
- **Given** a machine with `go` on `PATH` and `$(go env GOPATH)/bin` already included in `$PATH`
- **When** `./install.sh` runs
- **Then** the prerequisite check passes silently, installation proceeds, the PATH check confirms `$(go env GOPATH)/bin` is present in `$PATH`, no warning is printed, and the script exits 0

**Scenario 2: Go present but GOPATH/bin missing from PATH**
- **Given** a machine with `go` on `PATH` but `$(go env GOPATH)/bin` NOT present in `$PATH` (a common fresh-install scenario)
- **When** `./install.sh` runs
- **Then** the script still installs `atcr` successfully to `$(go env GOPATH)/bin/atcr`, then prints a clear warning to stdout or stderr stating that `$(go env GOPATH)/bin` is not on `$PATH` and instructing the user how to add it (e.g., `export PATH="$(go env GOPATH)/bin:$PATH"`), and still exits 0 (a PATH warning is not a failure)

## Edge Cases
**Edge Case 1: `go env GOPATH` returns an empty string**
- **Given** an unusual Go environment where `go env GOPATH` resolves to empty (misconfigured `GOPATH`/`GOENV`)
- **When** `./install.sh` runs the PATH check
- **Then** the script does not crash or produce a malformed path check; it treats an empty GOPATH result defensively (e.g., skips the PATH substring check safely rather than matching against an empty string, which could produce false negatives/positives)

**Edge Case 2: `$PATH` contains `$(go env GOPATH)/bin` as a substring of a different, longer path**
- **Given** `$PATH` contains a directory that has `$(go env GOPATH)/bin` as a substring but is not an exact colon-delimited entry (e.g., `/some/other/$(go env GOPATH)/bin-extra`)
- **When** the script performs the PATH containment check
- **Then** the check must match `$(go env GOPATH)/bin` as an exact colon-delimited `$PATH` segment (not a naive substring `grep`), avoiding false "already on PATH" positives

## Error Conditions
**Error Scenario 1: Go toolchain not installed / `go` not on PATH**
- **Given** a machine without `go` available (`command -v go` fails)
- **When** `./install.sh` runs
- **Then** the script exits non-zero immediately, before attempting any install, and prints a clear, actionable message to stderr
- Error message: `"error: go toolchain not found. Install Go 1.24+ from https://go.dev/dl/ and re-run this script."` (exact wording may vary; must name the missing prerequisite and give a remediation path, per story constraint that the message be "clear and actionable")
- HTTP status / error code: script exit code `1`

## Performance Requirements
- **Response Time:** Prerequisite check (`command -v go`) and PATH check (string match against `$PATH`) must each complete in well under 100ms — both are shell builtins with no network or subprocess overhead beyond the single `go env GOPATH` call
- **Throughput:** N/A — single-invocation script

## Security Considerations
- **Authentication/Authorization:** None
- **Input Validation:** The PATH check reads `$PATH` and `$(go env GOPATH)` from the environment only for comparison purposes; it does not execute or `eval` any value derived from `$PATH`, avoiding shell injection via a maliciously crafted environment variable

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Two controlled environments: (1) `PATH` with `go` present and GOPATH/bin included, (2) `PATH` with `go` present but GOPATH/bin excluded, (3) `PATH` with `go` absent entirely (achieved by manipulating `PATH` in the test harness, e.g., `PATH=/usr/bin:/bin ./install.sh` to exclude `go`)
**Mock/Stub Requirements:** For the "go missing" error scenario, run the script with a `PATH` value that excludes any directory containing a `go` binary rather than mocking `command -v` itself, to exercise the real guard clause

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors (`shellcheck install.sh` passes with no warnings)
- [ ] Build succeeds (`bash -n install.sh`)

**Story-Specific:**
- [ ] Script exits non-zero with a clear stderr message when `go` is not on `PATH`, before attempting install
- [ ] Script warns (does not fail) when `$(go env GOPATH)/bin` is absent from `$PATH`, using an exact colon-delimited segment match
- [ ] Warning message suggests adding the directory to `$PATH` and mentions `atcr doctor`/`atcr version` as the post-install verification step
- [ ] No warning is printed when `$(go env GOPATH)/bin` is already present in `$PATH`

**Manual Review:**
- [ ] Code reviewed and approved
