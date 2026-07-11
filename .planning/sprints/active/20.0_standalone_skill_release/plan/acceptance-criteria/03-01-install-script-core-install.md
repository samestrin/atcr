# Acceptance Criteria: Install Script Core Installation Flow

**Related User Story:** [3: Install Script](../user-stories/03-install-script.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | POSIX/bash shell script | `install.sh` at repo root, executable (`chmod +x`) |
| Test Framework | bats-core (or manual bash test harness) | Matches existing repo convention of testing shell scripts by direct invocation; no existing bats suite in repo, so manual/CI-driven invocation tests are acceptable |
| Key Dependencies | `go` toolchain (Go 1.24+), `go install` | No third-party shell libraries; wraps a single stdlib Go tooling command |

### Related Files (from codebase-discovery.json)

- `install.sh` — create: thin wrapper script that checks for `go`, runs `go install github.com/samestrin/atcr/cmd/atcr@latest`, and reports success
- `examples/ci-gate.sh` — reference only: style precedent for shebang, `set -euo pipefail`, comment density, and stdout/stderr message conventions (not modified by this AC)
- `.planning/plans/active/20.0_standalone_skill_release/documentation/install-script-conventions.md` — reference only: documents the Installation Targets table and Quick Reference this AC implements
- `README.md` — reference only: existing Quickstart section that documents the same `go install` path

## Design References

- [Install Script Conventions](../documentation/install-script-conventions.md) — requirements, installation targets, and scope guard for `install.sh`

## Happy Path Scenarios
**Scenario 1: Fresh install with Go toolchain present**
- **Given** a machine with a working Go 1.24+ toolchain on `PATH` and no `atcr` binary previously installed
- **When** the user runs `./install.sh` from the repo root (or via `curl ... | bash`)
- **Then** the script runs `go install github.com/samestrin/atcr/cmd/atcr@latest`, the `atcr` binary is placed at `$(go env GOPATH)/bin/atcr`, the script exits 0, and prints a success message to stdout suggesting `atcr doctor` or `atcr version` as the next step

**Scenario 2: Re-install over an existing binary**
- **Given** a machine where `atcr` is already installed at `$(go env GOPATH)/bin/atcr` from a prior run
- **When** the user runs `./install.sh` again
- **Then** `go install` overwrites the existing binary with the latest version, the script exits 0, and the same success message is printed (idempotent behavior, no special-casing required)

## Edge Cases
**Edge Case 1: Script invoked from a directory other than the repo root**
- **Given** the user has copied or downloaded `install.sh` and runs it from an arbitrary working directory
- **When** `./install.sh` executes
- **Then** the script behaves identically regardless of invocation directory, because it only shells out to `go install` with a fully qualified module path (`github.com/samestrin/atcr/cmd/atcr@latest`) and never depends on relative paths or the current working directory

**Edge Case 2: `atcr` already on `PATH` via a different install method**
- **Given** an `atcr` binary already exists somewhere else on `PATH` (e.g., installed via a package manager, out of scope for this epic)
- **When** the user runs `./install.sh`
- **Then** the script still installs to the standard `go install` destination `$(go env GOPATH)/bin/atcr` without attempting to detect, warn about, or resolve the conflict — shadowing behavior is left to the user's own `PATH` ordering, consistent with the story's constraint against OS/arch or environment detection beyond what `go install` already handles

## Error Conditions
**Error Scenario 1: `go install` command fails (e.g., network failure, module not found, build error)**
- **Given** a working Go toolchain but `go install github.com/samestrin/atcr/cmd/atcr@latest` fails for any reason (network unreachable, module resolution error, compile error)
- **When** `./install.sh` executes
- **Then** the script exits non-zero (propagating `go install`'s own exit code, enabled by `set -euo pipefail`), and the underlying `go install` error output is surfaced to stderr rather than being suppressed
- Error message: underlying `go install` stderr output is passed through unmodified (no wrapper text required beyond letting `set -euo pipefail` halt the script)
- HTTP status / error code: script exit code matches `go install`'s non-zero exit code (typically `1`)

## Performance Requirements
- **Response Time:** Script overhead (excluding the `go install` network/compile time) must be under 1 second — it performs only a `command -v go` check and a single `go install` invocation, no polling or retries
- **Throughput:** N/A — single-invocation script, not a service

## Security Considerations
- **Authentication/Authorization:** None — `install.sh` performs no authentication; it relies on the Go toolchain's own module-proxy verification (GOPROXY/GOSUMDB) for the fetched module, which is unmodified default Go behavior
- **Input Validation:** Script takes no user-supplied arguments in scope for this AC; the installed module path (`github.com/samestrin/atcr/cmd/atcr@latest`) is a hardcoded literal, not user input, eliminating injection risk

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** A CI runner or local machine with a real Go 1.24+ toolchain installed; no mocked Go environment, since the whole point is verifying the real `go install` invocation succeeds
**Mock/Stub Requirements:** None for the happy path (run against real `go install`); for the failure-path test, temporarily shadow `go` in `PATH` with a stub script that exits non-zero to simulate a `go install` failure without depending on network conditions

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors (`shellcheck install.sh` passes with no warnings)
- [ ] Build succeeds (script is syntactically valid bash: `bash -n install.sh`)

**Story-Specific:**
- [ ] `install.sh` exists at repo root and is executable
- [ ] `./install.sh` installs `atcr` to `$(go env GOPATH)/bin/atcr` on a machine with Go present
- [ ] Script exits 0 and prints a stdout success message referencing `atcr doctor`/`atcr version` on success
- [ ] Script is under ~40 lines, matching `examples/ci-gate.sh` scope

**Manual Review:**
- [ ] Code reviewed and approved
