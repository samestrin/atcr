# Acceptance Criteria: List Installed Personas

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `list` sub-subcommand under `newPersonasCmd()` |
| Built-in persona enumeration | `internal/registry` | Enumerate the 6 generalist + 3 bonus built-ins |
| Community persona enumeration | `os.ReadDir` on `~/.config/atcr/personas/` | Walk directory; treat missing dir as empty set |
| Output formatting | `text/tabwriter` | Aligned columns: Name, Version, Source, Language |
| Test Framework | `go test` / `testify` | Temp directory with fixture YAML files |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas/list.go` | |

## Related Files
- `internal/personas/list.go` - create: `List(personasDir string) ([]PersonaMeta, error)` enumerating built-ins + installed community personas
- `cmd/atcr/personas.go` - modify: add `list` sub-subcommand with `--scores` flag (deferred/no-op for Story 2; wired in Story 5)
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasList_*` test cases using temp directories

### Related Files (from codebase-discovery.json)

- `internal/personas/list.go` — create: list installed personas
- `cmd/atcr/personas.go` — modify: add `list` sub-subcommand
- `cmd/atcr/personas_test.go` — modify: add list test cases
- `internal/registry/persona.go:44` — persona resolution chain for installed personas
- `personas/personas.go:16` — built-in persona `names` slice

## Happy Path Scenarios

**Scenario 1: List with both built-in and community personas installed**
- **Given** the registry has 9 built-in personas (6 generalist + 3 bonus) and 2 community personas installed in `~/.config/atcr/personas/`
- **When** the user runs `atcr personas list`
- **Then** stdout prints a table with Name, Version, Source, and Language columns; all 11 personas appear; built-ins show source `built-in` and community personas show source `community`; personas with a non-empty `language` field display the canonical language list (e.g., `go`); personas with no `language` field display `-`; command exits 0

**Scenario 2: List with no community personas installed**
- **Given** `~/.config/atcr/personas/` is empty or does not exist
- **When** the user runs `atcr personas list`
- **Then** stdout prints the table with only the 9 built-in personas; no error is printed; command exits 0

**Scenario 3: Version column populated from YAML metadata**
- **Given** an installed community persona YAML contains a `version: "1.2.0"` field
- **When** `atcr personas list` is run
- **Then** the Version column shows `1.2.0` for that persona; built-ins show their embedded version string or `built-in` if unversioned

## Edge Cases

**Edge Case 1: `--scores` flag is accepted but deferred (no-op in Story 2)**
- **Given** the user runs `atcr personas list --scores`
- **When** the command executes
- **Then** it exits 0 and prints the standard table without a Scores column; a notice `"(--scores available in a future release)"` may appear; no error

**Edge Case 2: Community persona YAML missing version field**
- **Given** an installed YAML has no `version` field
- **When** `atcr personas list` is run
- **Then** the Version column shows `-` (dash) for that persona; command still exits 0

**Edge Case 3: Community personas directory contains non-YAML files**
- **Given** `~/.config/atcr/personas/` contains a `.DS_Store` or `.gitkeep` file alongside valid YAMLs
- **When** `atcr personas list` is run
- **Then** non-YAML files are silently skipped; only `.yaml` and `.yml` files appear in the output

**Edge Case 4: Persona with multiple language scopes**
- **Given** an installed community persona YAML contains `language: ["go", "ts"]`
- **When** `atcr personas list` is run
- **Then** the Language column shows `go, ts` (canonical, comma-separated) for that persona

**Edge Case 5: Persona with no language field**
- **Given** a built-in persona or an installed YAML omits the `language` field
- **When** `atcr personas list` is run
- **Then** the Language column shows `-` for that persona

## Error Conditions

**Error Scenario 1: Community personas directory is unreadable (permission denied)**
- Error message: `"warning: could not read personas directory <path>: permission denied"` (printed to stderr)
- Exit code: 0 (graceful degradation — built-ins still listed)

## Performance Requirements
- **Response Time:** `list` output renders in under 100ms for up to 100 installed community personas
- **Throughput:** Single synchronous directory scan; no concurrency requirement

## Security Considerations
- **Authentication/Authorization:** Read-only operation; no credentials required
- **Input Validation:** No user-supplied path input; `PersonasDir()` is constructed from a fixed config base path, not from CLI arguments

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/list.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** Temp directory with 2–3 fixture YAML files (with and without `version` field); scenario with missing directory
**Mock/Stub Requirements:** Override `PersonasDir()` to return a temp directory in tests; no HTTP mocking needed for `list`

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas list` prints Name, Version, Source, and Language columns and exits 0 regardless of whether `~/.config/atcr/personas/` exists
- [ ] Built-in personas appear with source `built-in`; community personas appear with source `community`
- [ ] Personas with a `language` field display the canonical language list; personas without one display `-`
- [ ] `--scores` flag is accepted without error (no-op output acceptable for Story 2)

**Manual Review:**
- [ ] Code reviewed and approved
