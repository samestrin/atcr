# Acceptance Criteria: Upgrade Installed Personas

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `upgrade` sub-subcommand under `newPersonasCmd()`; accepts `<name>` arg or `--all` flag; `--dry-run` flag |
| Version comparison | `golang.org/x/mod/semver` | Structured semver comparison; fall back to string equality for non-semver |
| HTTP fetch | `net/http` stdlib | Re-fetch persona YAML from community repo; injectable `http.Client` |
| File I/O | `os.WriteFile` | Overwrite local file only when remote version is newer |
| Test Framework | `go test` / `testify` + `httptest.NewServer` | Deterministic version fixtures; no live network calls |
| Key Dependencies | `github.com/spf13/cobra`, `golang.org/x/mod/semver`, `internal/personas/upgrade.go`, `internal/personas/client.go` | |

## Related Files
- `internal/personas/upgrade.go` - create: `Upgrade(client, baseURL, personasDir, name string, dryRun bool) (UpgradeResult, error)` — fetches remote YAML, compares versions, writes if newer
- `internal/personas/client.go` - modify: add `FetchPersonaYAML(client, baseURL, name string) ([]byte, error)` helper (shared with `install`)
- `cmd/atcr/personas.go` - modify: add `upgrade` sub-subcommand with `--all` and `--dry-run` flags
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasUpgrade_*` test cases using `httptest.NewServer` and temp directory

## Happy Path Scenarios

**Scenario 1: Upgrade a single persona when remote is newer**
- **Given** `~/.config/atcr/personas/security/owasp.yaml` is installed at version `1.0.0` and the community repo `httptest.NewServer` serves version `1.1.0`
- **When** the user runs `atcr personas upgrade security/owasp`
- **Then** the local file is overwritten with the remote content, stdout prints `"Upgraded security/owasp: 1.0.0 → 1.1.0"`, and the command exits 0

**Scenario 2: Upgrade all installed personas (`--all` flag)**
- **Given** two community personas are installed: `security/owasp` (upgradeable) and `go/idiomatic` (already current)
- **When** the user runs `atcr personas upgrade --all`
- **Then** `security/owasp` is upgraded and reported; `go/idiomatic` is skipped with a `"already up to date"` note; command exits 0

**Scenario 3: Dry-run prints what would change without writing**
- **Given** `security/owasp` v1.0.0 is installed and the remote has v1.1.0
- **When** the user runs `atcr personas upgrade --dry-run security/owasp`
- **Then** stdout prints `"Would upgrade security/owasp: 1.0.0 → 1.1.0"`, no file is written to disk, and the command exits 0

**Scenario 4: Persona is already at latest version**
- **Given** the local and remote versions of `security/owasp` are both `1.1.0`
- **When** `atcr personas upgrade security/owasp` is run
- **Then** stdout prints `"security/owasp is already up to date (1.1.0)"`, no file is written, and the command exits 0

## Edge Cases

**Edge Case 1: Non-semver version strings fall back to string equality**
- **Given** a persona uses `version: "2026-06-01"` (date-stamped) locally and the remote returns `version: "2026-06-24"`
- **When** `atcr personas upgrade <name>` is run
- **Then** the tool falls back to string comparison; since `"2026-06-24" != "2026-06-01"` the upgrade proceeds (treats any version change as newer when semver parse fails)

**Edge Case 2: Remote YAML fails validation after fetch**
- **Given** the remote version of a persona fails `validateAgent`
- **When** `atcr personas upgrade security/owasp` is run
- **Then** the local file is NOT overwritten; stderr prints a descriptive validation error; the command exits non-zero

**Edge Case 3: `--all` with no community personas installed**
- **Given** `~/.config/atcr/personas/` is empty
- **When** `atcr personas upgrade --all` is run
- **Then** stdout prints `"No community personas installed"` and the command exits 0

**Edge Case 4: Dry-run with `--all` and mixed states**
- **Given** two personas — one upgradeable, one current
- **When** `atcr personas upgrade --dry-run --all` is run
- **Then** both are reported (`"Would upgrade..."` and `"already up to date"`) with no files written; exit 0

## Error Conditions

**Error Scenario 1: Named persona not installed**
- Error message: `"persona \"security/owasp\" is not installed"`
- Exit code: 1

**Error Scenario 2: Fetch fails for one persona during `--all`**
- Error message: `"failed to fetch security/owasp: <error> (skipping)"` printed to stderr; upgrade continues for remaining personas
- Exit code: 1 at completion if any persona failed

**Error Scenario 3: Mutual exclusion — name arg and `--all` both supplied**
- Error message: `"cannot specify both a persona name and --all"`
- Exit code: 1

## Performance Requirements
- **Response Time:** Single persona upgrade completes in under 3 seconds on a local `httptest.NewServer`
- **Throughput:** `--all` upgrades run sequentially; no parallelism required (avoids rate-limit concerns on live repo)

## Security Considerations
- **Authentication/Authorization:** No authentication required for public community repo; HTTPS enforced for the default base URL
- **Input Validation:** Same path-traversal check as `install` and `remove` applied to the persona name before any filesystem operation; `validateAgent` run on fetched content before overwriting local file

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/upgrade.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** `httptest.NewServer` fixture returning newer-version YAML, same-version YAML, and invalid YAML; temp `personasDir` with pre-installed fixtures at known versions
**Mock/Stub Requirements:** Inject `httptest.NewServer` URL as `RegistryBaseURL`; inject temp directory for `PersonasDir()`; stub `validateAgent` to return error for the invalid-YAML test case

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas upgrade <name>` overwrites local file only when remote version is newer; exits 0
- [ ] `atcr personas upgrade --all` upgrades all outdated personas and skips current ones; exits 0
- [ ] `--dry-run` flag prints what would change without writing any files
- [ ] `validateAgent` is called on fetched content before overwriting; invalid remote content is rejected without touching the local file

**Manual Review:**
- [ ] Code reviewed and approved
