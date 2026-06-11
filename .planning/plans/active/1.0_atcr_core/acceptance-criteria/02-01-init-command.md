# Acceptance Criteria: atcr init Command

**Related User Story:** [02: Agent Configuration](../user-stories/02-agent-configuration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Command | Go `cobra` subcommand | `atcr init` |
| YAML Serialization | `gopkg.in/yaml.v3` | Strict mode with `KnownFields(true)` |
| Filesystem | `os`, `path/filepath` | Create `.atcr/` directory and files |
| Embedded Defaults | `embed` package | Default persona files and config template |
| Test Framework | `testify` (assert, require) | Table-driven tests |

## Related Files
- `cmd/atcr/init.go` - create: `atcr init` cobra subcommand implementation
- `internal/registry/config.go` - create: Config struct definitions and default values
- `internal/registry/config_test.go` - create: Unit tests for init behavior
- `cmd/atcr/init_test.go` - create: Integration tests for init command (creates files, validates output)
- `personas/bruce.md` - create: Embedded default persona file (and 5 others)
- `personas/_base.md` - create: Base persona template used by all agents

## Happy Path Scenarios

**Scenario 1: Fresh initialization in a new project directory**
- **Given** a directory that does not contain `.atcr/` and the developer has write permissions
- **When** the developer runs `atcr init`
- **Then** the tool creates `.atcr/config.yaml` with default roster containing all six personas (bruce, greta, kai, mira, dax, otto), `payload_mode: blocks`, `timeout_secs: 600`, and `fail_on: HIGH`
- **And** the tool creates six persona files at `.atcr/personas/{bruce,greta,kai,mira,dax,otto}.md`
- **And** the tool creates `.atcr/personas/_base.md` as a shared base template
- **And** the tool prints a success message listing all created files

**Scenario 2: Init generates valid, parseable config**
- **Given** `atcr init` has completed successfully
- **When** the developer reads `.atcr/config.yaml`
- **Then** the file is valid YAML that parses without errors under strict mode (`KnownFields(true)`)
- **And** the config contains all required top-level keys: `agents`, `serial_agents`, `payload_mode`, `timeout_secs`, `fail_on`

**Scenario 3: Each persona file is editable and valid**
- **Given** `atcr init` has completed successfully
- **When** the developer opens `.atcr/personas/bruce.md`
- **Then** the file contains a valid persona prompt template with `{{.Payload}}` and `{{.AgentName}}` placeholders
- **And** the file is structured with clear frontmatter or section headers

## Edge Cases

**Edge Case 1: `.atcr/` directory already exists with config**
- **Given** `.atcr/config.yaml` already exists in the project
- **When** the developer runs `atcr init`
- **Then** the tool returns an error: "config already exists at .atcr/config.yaml — use --force to overwrite"
- **And** no existing files are modified

**Edge Case 2: Init with --force flag overwrites existing config**
- **Given** `.atcr/config.yaml` already exists
- **When** the developer runs `atcr init --force`
- **Then** the tool overwrites all files with defaults
- **And** prints a warning: "Overwriting existing configuration and persona files"

**Edge Case 3: Parent directory is read-only**
- **Given** the current directory is not writable by the developer
- **When** the developer runs `atcr init`
- **Then** the tool returns an error: "cannot create .atcr/: permission denied"
- **And** exits with non-zero exit code

## Error Conditions

**Error Scenario 1: Cannot create `.atcr/` directory**
- Error message: "cannot create .atcr/: <os error>"
- Exit code: 1

**Error Scenario 2: Cannot write persona file**
- Error message: "failed to write .atcr/personas/<name>.md: <os error>"
- Exit code: 1
- Partial cleanup: already-created files remain; developer can fix permissions and retry

**Error Scenario 3: Embedded defaults missing (build error)**
- Error message: "internal error: embedded persona <name> not found"
- This should never happen in a correct build; panics during development

## Performance Requirements
- **Response Time:** `atcr init` completes in < 100ms on local filesystem
- **Throughput:** N/A (single invocation)

## Security Considerations
- **Input Validation:** None required — `atcr init` writes only from embedded defaults, no user input
- **File Permissions:** Created files use 0644 (rw-r--r--); directories use 0755 (rwxr-xr-x)
- **No secrets written:** `atcr init` does not write API keys; keys are referenced by env var name only

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** None (uses embedded defaults)
**Mock/Stub Requirements:**
- Filesystem: use `t.TempDir()` to create isolated test directories
- Override working directory with `os.Chdir()` or pass path as parameter to enable testing without polluting project

**Test Cases:**
1. `TestInit_FreshDirectory` — verify all files created with correct content
2. `TestInit_AlreadyExists` — verify error when config present
3. `TestInit_Force` — verify overwrite with --force
4. `TestInit_ReadOnlyDir` — verify error on permission denied
5. `TestInit_ConfigParsesStrict` — verify generated config passes strict YAML parsing

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] `atcr init` creates `.atcr/config.yaml` with all six personas in roster
- [ ] `atcr init` creates six persona `.md` files + `_base.md`
- [ ] Generated config passes strict YAML parsing round-trip

**Story-Specific:**
- [ ] Default roster includes: bruce, greta, kai, mira, dax, otto
- [ ] `payload_mode` defaults to `blocks`
- [ ] `timeout_secs` defaults to 600
- [ ] `fail_on` defaults to `HIGH`
- [ ] Re-running without `--force` produces a clear error (not silent overwrite)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Persona file templates are clear, editable, and contain expected placeholders
