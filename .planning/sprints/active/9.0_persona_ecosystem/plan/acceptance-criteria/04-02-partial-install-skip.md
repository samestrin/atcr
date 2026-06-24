# Acceptance Criteria: Partial Bundle Install Skip Behavior

**Related User Story:** [04: Domain Bundle Installation](../user-stories/04-domain-bundles.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Bundle resolver | Go package (`internal/personas/bundles.go`) | Returns full persona list; install loop skips present entries |
| Install loop | `cmd/atcr/personas.go` + `internal/personas/install.go` | Checks existence before fetch for each persona in the resolved list |
| Existence check | `os.Stat` on `~/.config/atcr/personas/<namespace>/<name>` | Returns `(installed, already-present)` per-persona outcome |
| Test framework | `go test` / `testify` | Table-driven unit tests |

## Related Files
- `cmd/atcr/personas.go` - modify: per-persona outcome reporting (installed vs. already present) in bundle install loop
- `internal/personas/install.go` - modify: bundle-aware install path calls `bundles.Resolve` then loops single-persona install
- `internal/personas/bundles.go` - create: `Resolve` returns full list; caller loop handles skip logic
- `internal/personas/bundles_test.go` - create: partial-install skip test with pre-populated temp config dir

### Related Files (from codebase-discovery.json)

- `cmd/atcr/personas.go` — modify: install subcommand skip logic
- `internal/personas/bundles.go` — create: `Resolve` returns full persona list
- `internal/personas/bundles_test.go` — create: partial-install skip tests
- `internal/personas/install.go` — related: single-persona install path
- `internal/personas/paths.go` — create: `PersonasDir()` path helpers

## Happy Path Scenarios

**Scenario 1: Two of four bundle members already installed**
- **Given** `django-orm` and `python-types` are already present in `~/.config/atcr/personas/`
- **When** the user runs `atcr personas install bundle/django`
- **Then** the command installs `security/owasp` and `security/secrets`, prints "already present" for `django-orm` and `python-types`, prints "installed" for the two new ones, and exits 0

**Scenario 2: One of four bundle members already installed**
- **Given** only `security/owasp` is already installed
- **When** `atcr personas install bundle/django` is run
- **Then** the command installs the remaining three personas, reports "already present" for `security/owasp`, and exits 0

## Edge Cases

**Edge Case 1: All bundle members already installed**
- **Given** all four members of `bundle/django` are present on disk
- **When** `atcr personas install bundle/django` is run
- **Then** the command prints "already present" for all four, performs no writes, and exits 0

**Edge Case 2: Config dir exists but is empty**
- **Given** `~/.config/atcr/personas/` exists with no contents
- **When** `atcr personas install bundle/django` is run
- **Then** all four personas are installed; no skip messages appear

## Error Conditions

**Error Scenario 1: Existence check fails due to permission error**
- **Given** `~/.config/atcr/personas/` is not readable (permission denied)
- **When** `atcr personas install bundle/django` is run
- **Then** the command exits non-zero with a clear error message; no partial writes are attempted
- Error message: `"cannot access personas directory: permission denied"`
- Error code: non-zero exit status

## Performance Requirements
- **Response Time:** Per-persona existence check via `os.Stat` must complete in < 5 ms per entry; the four-persona loop must complete the skip decision phase in < 20 ms total (no network calls during the check phase)
- **Throughput:** N/A — sequential install loop is correct behavior; no concurrency required for the initial bundles

## Security Considerations
- **Authentication/Authorization:** Skip decision is based solely on local filesystem state; no remote calls are made for already-present personas
- **Input Validation:** Persona paths derived from bundle manifest are validated against the canonical namespace/name format (`[a-z][a-z0-9-]*/[a-z][a-z0-9-]*`) before constructing filesystem paths to prevent path traversal

## Test Implementation Guidance
**Test Type:** UNIT (skip logic in install loop) + INTEGRATION (temp config dir with pre-populated entries)
**Test Data Requirements:** A temp `~/.config/atcr/personas/` directory with a subset of bundle members pre-created as empty marker files or full persona YAML files
**Mock/Stub Requirements:** `httptest.NewServer` for the personas that actually need fetching; `os.Stat` can operate on real temp dir without mocking

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./internal/personas/... ./cmd/atcr/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `internal/personas/bundles_test.go` covers partial-install skip: some members pre-installed, remainder fetched, all outcomes reported
- [x] CLI output distinguishes "installed" from "already present" for each bundle member
- [x] Full re-run on a fully-installed bundle exits 0 and makes no filesystem writes
- [x] Per-persona outcome is reported even when all members are skipped

**Manual Review:**
- [ ] Code reviewed and approved
