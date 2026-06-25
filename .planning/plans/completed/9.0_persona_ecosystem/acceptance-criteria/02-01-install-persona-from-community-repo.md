# Acceptance Criteria: Install Persona from Community Repo

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `cmd/atcr/personas.go` → `newPersonasCmd()` → `install` sub-subcommand |
| HTTP fetch | `net/http` stdlib | Injected `http.Client` so tests substitute `httptest.NewServer` |
| File I/O | `os`, `os.MkdirAll` | Write YAML to `~/.config/atcr/personas/<name>.yaml`; create dirs if absent |
| Validation | existing `validateAgent` | Run against fetched YAML before writing to disk |
| Test Framework | `go test` / `testify` + `httptest.NewServer` | No live network calls in CI |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas`, `internal/registry` | |

## Related Files
- `cmd/atcr/personas.go` - create: defines `newPersonasCmd()` and registers `install` sub-subcommand
- `cmd/atcr/personas_test.go` - create: integration tests for `install` via `httptest.NewServer`
- `internal/personas/install.go` - create: `Install(client, baseURL, name, destDir)` function
- `internal/personas/client.go` - create: injectable `http.Client` wrapper and `RegistryBaseURL` constant
- `internal/personas/paths.go` - create: `PersonasDir()` and related path helpers
- `cmd/atcr/main.go` - modify: register `newPersonasCmd()` under root (line 128)
- `cmd/atcr/main_test.go` - modify: bump `TestRootCmd_HasExactlyFourteenSubcommands` to 15

### Related Files (from codebase-discovery.json)

- `cmd/atcr/main.go:128` — root command constructor `newRootCmd`
- `cmd/atcr/main.go:174-189` — `root.AddCommand()` registration block
- `cmd/atcr/main_test.go:46` — `TestRootCmd_HasExactlyFourteenSubcommands` to update to 15
- `cmd/atcr/personas.go` — create: `newPersonasCmd()` and `install` sub-subcommand
- `cmd/atcr/personas_test.go` — create: integration tests for `install`
- `internal/personas/install.go` — create: install logic
- `internal/personas/client.go` — create: HTTP client and `RegistryBaseURL`
- `internal/personas/paths.go` — create: `PersonasDir()` path helpers
- `internal/registry/persona.go:44` — persona resolution chain for installed files

## Happy Path Scenarios

**Scenario 1: Successful install of a named persona**
- **Given** a community repo `httptest.NewServer` serving `/security/owasp.yaml` with valid YAML content and an HTTP 200 response
- **When** the user runs `atcr personas install security/owasp`
- **Then** the file is written to `~/.config/atcr/personas/security/owasp.yaml`, the command exits 0, and stdout contains a success message including the persona name

**Scenario 2: Install creates missing directory**
- **Given** `~/.config/atcr/personas/` does not exist on a fresh install
- **When** the user runs `atcr personas install security/owasp`
- **Then** `os.MkdirAll` creates the directory tree, the file is written successfully, and the command exits 0

**Scenario 3: Persona available in registry without restart**
- **Given** `security/owasp.yaml` has been installed via `atcr personas install`
- **When** the registry performs its startup scan of `~/.config/atcr/personas/` in the same running process
- **Then** `security/owasp` appears as a resolvable persona without restarting the binary

## Edge Cases

**Edge Case 1: Install overwrites existing persona (re-install)**
- **Given** `~/.config/atcr/personas/security/owasp.yaml` already exists
- **When** the user runs `atcr personas install security/owasp` again
- **Then** the file is overwritten with the latest fetched content, the command exits 0, and stdout indicates the persona was updated

**Edge Case 2: Base URL overridden via environment variable**
- **Given** `ATCR_PERSONAS_URL` is set to a custom `httptest.NewServer` address
- **When** `atcr personas install security/owasp` is executed
- **Then** the fetch targets the override URL, not the default `https://raw.githubusercontent.com/atcr/personas/main`

**Edge Case 3: Fetched YAML fails registry validation**
- **Given** the community repo returns syntactically valid YAML that fails `validateAgent` (e.g., missing required fields)
- **When** `atcr personas install security/owasp` is executed
- **Then** the command exits non-zero, prints a descriptive validation error to stderr, and does NOT write any file to disk

## Error Conditions

**Error Scenario 1: Persona not found in community repo (HTTP 404)**
- Error message: `"persona \"security/owasp\" not found in community repo"`
- Exit code: 1

**Error Scenario 2: Network/fetch failure**
- Error message: `"failed to fetch persona \"security/owasp\": <underlying error>"`
- Exit code: 1

**Error Scenario 3: File write permission denied**
- Error message: `"failed to write persona to <path>: permission denied"`
- Exit code: 1

**Error Scenario 4: Fetched YAML validation failure**
- Error message: `"persona \"security/owasp\" failed validation: <validation error>"`
- Exit code: 1

## Performance Requirements
- **Response Time:** Install completes (fetch + write) in under 2 seconds on a local `httptest.NewServer`; live network latency is acceptable up to 10 seconds before timeout
- **Throughput:** Single install at a time; no concurrency requirement for this command

## Security Considerations
- **Authentication/Authorization:** No authentication required for public community repo; HTTPS enforced for the default base URL
- **Input Validation:** Persona name must match pattern `[a-zA-Z0-9_/-]+`; reject names containing `..` or absolute path components to prevent path traversal when constructing the destination file path
- **YAML content:** `validateAgent` must run before any write to prevent malformed or malicious configs from reaching the registry

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/install.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** Valid persona YAML fixture; invalid persona YAML fixture (missing required fields); 404 response fixture
**Mock/Stub Requirements:** `httptest.NewServer` serving `/security/owasp.yaml`; temp directory substituted for `~/.config/atcr/personas/` via `PersonasDir()` override in tests

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas install <name>` exits 0 and writes the file when the remote returns HTTP 200 with valid YAML
- [ ] `atcr personas install <name>` exits non-zero with a descriptive error on HTTP 404 or fetch failure
- [ ] `validateAgent` is called before any disk write; invalid YAML is rejected without writing
- [ ] `TestRootCmd_HasExactlyFourteenSubcommands` updated to 15 in the same commit that registers `newPersonasCmd()`

**Manual Review:**
- [ ] Code reviewed and approved
