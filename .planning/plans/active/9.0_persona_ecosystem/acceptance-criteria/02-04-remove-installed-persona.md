# Acceptance Criteria: Remove Installed Persona

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `remove` sub-subcommand under `newPersonasCmd()` |
| File deletion | `os.Remove` | Delete `~/.config/atcr/personas/<name>.yaml` |
| Existence check | `os.Stat` | Confirm file exists before deletion; surface descriptive error if not found |
| Test Framework | `go test` / `testify` | Temp directory with fixture YAML files; no HTTP mocking needed |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas/remove.go`, `internal/personas/paths.go` | |

## Related Files
- `internal/personas/remove.go` - create: `Remove(personasDir, name string) error` — resolves path, checks existence, deletes file
- `cmd/atcr/personas.go` - modify: add `remove` sub-subcommand wired to `personas.Remove()`
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasRemove_*` test cases using temp directory

## Happy Path Scenarios

**Scenario 1: Successfully remove an installed community persona**
- **Given** `~/.config/atcr/personas/security/owasp.yaml` exists (installed previously)
- **When** the user runs `atcr personas remove security/owasp`
- **Then** the file is deleted from disk, the command exits 0, and stdout prints `"Removed persona \"security/owasp\""`

**Scenario 2: Registry no longer resolves removed persona**
- **Given** `security/owasp` was installed and then removed via `atcr personas remove security/owasp`
- **When** the registry performs its next startup scan of `~/.config/atcr/personas/`
- **Then** `security/owasp` is no longer resolvable as an installed persona

## Edge Cases

**Edge Case 1: Name uses forward slash (subdirectory) notation**
- **Given** the user runs `atcr personas remove security/owasp`
- **When** `Remove()` resolves the path
- **Then** it correctly maps to `<personasDir>/security/owasp.yaml` (forward slash becomes path separator); no path traversal is possible

**Edge Case 2: Empty parent directory left after removal**
- **Given** `~/.config/atcr/personas/security/` contains only `owasp.yaml`, which is then removed
- **When** the removal completes
- **Then** the empty `security/` subdirectory may remain (not required to clean up); the tool does not error on empty dirs

## Error Conditions

**Error Scenario 1: Persona is not installed (file does not exist)**
- Error message: `"persona \"security/owasp\" is not installed"`
- Exit code: 1

**Error Scenario 2: Attempting to remove a built-in persona name**
- Error message: `"cannot remove built-in persona \"critic\" — only community-installed personas can be removed"`
- Exit code: 1

**Error Scenario 3: File deletion fails (permission denied)**
- Error message: `"failed to remove persona \"security/owasp\": permission denied"`
- Exit code: 1

**Error Scenario 4: Name contains path traversal components**
- Error message: `"invalid persona name \"../etc/passwd\""`
- Exit code: 1

## Performance Requirements
- **Response Time:** Remove completes in under 50ms (local file deletion only; no network calls)
- **Throughput:** Single file deletion; no concurrency requirement

## Security Considerations
- **Authentication/Authorization:** Operates only on the user's own `~/.config/atcr/personas/` directory; no elevated privileges required
- **Input Validation:** Persona name must match pattern `[a-zA-Z0-9_/-]+`; names containing `..` or absolute path components are rejected before any filesystem operation to prevent path traversal
- **Scope restriction:** Command refuses to delete files outside `PersonasDir()`; the resolved absolute path is verified to be a child of `PersonasDir()` before `os.Remove` is called

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/remove.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** Temp directory with a fixture `security/owasp.yaml`; scenario with missing file; scenario with path traversal attempt
**Mock/Stub Requirements:** Override `PersonasDir()` to return a temp directory in tests; no HTTP mocking needed

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas remove <name>` deletes the file and exits 0 when the persona is installed
- [ ] `atcr personas remove <name>` exits non-zero with a descriptive error when the persona is not installed
- [ ] Path traversal names are rejected before any filesystem operation
- [ ] Built-in persona names are rejected with a clear error message

**Manual Review:**
- [ ] Code reviewed and approved
