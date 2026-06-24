# Acceptance Criteria: Clean Bundle Installation

**Related User Story:** [04: Domain Bundle Installation](../user-stories/04-domain-bundles.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Bundle resolver | Go package (`internal/personas/bundles.go`) | Parses embedded YAML manifests |
| CLI subcommand | Cobra (`cmd/atcr/personas_install.go`) | Detects `bundle/` prefix, delegates to resolver |
| Manifest embedding | `go:embed bundles/*.yaml` | No global init; parsed at call time |
| YAML parsing | `gopkg.in/yaml.v3` | Struct-based decode |
| Test framework | `go test` / `testify` | Unit + integration |

## Related Files
- `internal/personas/bundles.go` - create: bundle resolver with `go:embed`, `Resolve(name string) ([]string, error)`, typed `ErrUnknownBundle`
- `internal/personas/bundles/django.yaml` - create: manifest declaring `django-orm`, `python-types`, `security/owasp`, `security/secrets`
- `internal/personas/bundles/go-production.yaml` - create: manifest declaring go-production persona set
- `cmd/atcr/personas_install.go` - modify: add `strings.HasPrefix(arg, "bundle/")` guard to delegate to `bundles.Resolve`

### Related Files (from codebase-discovery.json)

- `internal/personas/bundles.go` — create: bundle resolver and manifest parser
- `internal/personas/bundles/django.yaml` — create: django bundle manifest
- `internal/personas/bundles/go-production.yaml` — create: go-production bundle manifest
- `cmd/atcr/personas.go` — modify: detect `bundle/` prefix in install subcommand
- `internal/personas/install.go` — related: single-persona install path used by bundle expansion
- `internal/personas/paths.go` — create: `PersonasDir()` path helpers

## Happy Path Scenarios

**Scenario 1: Install bundle/django on a clean config directory**
- **Given** `~/.config/atcr/personas/` is empty (no personas installed)
- **When** the user runs `atcr personas install bundle/django`
- **Then** the command exits 0, installs `django-orm`, `python-types`, `security/owasp`, and `security/secrets` into `~/.config/atcr/personas/`, and prints a success message that names each installed persona

**Scenario 2: Install bundle/go-production on a clean config directory**
- **Given** `~/.config/atcr/personas/` is empty
- **When** the user runs `atcr personas install bundle/go-production`
- **Then** the command exits 0, installs all declared go-production personas, and prints each name installed

## Edge Cases

**Edge Case 1: Bundle with all members already installed**
- **Given** all four django bundle members are already present in `~/.config/atcr/personas/`
- **When** `atcr personas install bundle/django` is run
- **Then** the command exits 0, prints "already present" for each member, and installs nothing new (idempotent)

**Edge Case 2: Config directory does not yet exist**
- **Given** `~/.config/atcr/personas/` does not exist on disk
- **When** `atcr personas install bundle/django` is run
- **Then** the command creates the directory, installs all four personas, and exits 0

## Error Conditions

**Error Scenario 1: Network failure during one persona download mid-bundle**
- **Given** the community-repo HTTP endpoint returns an error for `security/owasp`
- **When** `atcr personas install bundle/django` runs
- **Then** the command reports the failure for that persona, continues with remaining bundle members, and exits non-zero; a subsequent re-run skips already-installed members and retries only the failed one
- Error message: `"failed to install security/owasp: <underlying error>"`
- Error code: non-zero exit status

## Performance Requirements
- **Response Time:** Bundle resolution (YAML parse + persona list expansion) must complete in < 50 ms (CPU-bound, no I/O); end-to-end install time is network-bound per persona fetch
- **Throughput:** Resolver must handle the full 4-persona django bundle expansion in a single synchronous call with no goroutine overhead

## Security Considerations
- **Authentication/Authorization:** No auth required for install from public community repo; configurable base URL must be validated as a well-formed HTTPS URL before fetch
- **Input Validation:** Bundle name after `bundle/` prefix is validated against the embedded manifest registry before any filesystem writes; path traversal characters in bundle name return `ErrUnknownBundle` rather than a filesystem path

## Test Implementation Guidance
**Test Type:** UNIT (resolver) + INTEGRATION (end-to-end install into temp dir)
**Test Data Requirements:** Two valid YAML manifests embedded in `internal/personas/bundles/`; a temp `~/.config/atcr/personas/` directory for integration tests
**Mock/Stub Requirements:** `httptest.NewServer` serving fake persona content for integration tests; no live network calls in CI

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/personas/... ./cmd/atcr/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas install bundle/django` on a clean config dir installs exactly the four declared personas and exits 0
- [ ] `atcr personas install bundle/go-production` on a clean config dir installs all declared personas and exits 0
- [ ] Bundle install is idempotent: re-running on a fully-installed bundle exits 0 and installs nothing new
- [ ] `internal/personas/bundles_test.go` unit test verifies resolver expands `bundle/django` to the exact four-persona list

**Manual Review:**
- [ ] Code reviewed and approved
