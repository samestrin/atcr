# Acceptance Criteria: `atcr config set telemetry <bool>` Persists Opt-Out to `.atcr/config.yaml`

**Related User Story:** [02: Telemetry Opt-Out](../user-stories/02-telemetry-opt-out.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (new CLI subcommand group) | `cmd/atcr`, following `cmd/atcr/debt.go`'s `newDebtCmd` pattern |
| Test Framework | `go test`, `testify` `assert`/`require` | Test location: `cmd/atcr/config_test.go` |
| Key Dependencies | `github.com/spf13/cobra`, `gopkg.in/yaml.v3` (existing project YAML lib), `internal/registry` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/config.go` - create: `newConfigCmd()` (`Use: "config"`, `RunE: cmd.Help`, `Args: usageArgs(cobra.NoArgs)`) and `newConfigSetCmd()` (`Use: "set"`, `Args: usageArgs(cobra.ExactArgs(2))`), validating key `== "telemetry"` (else `usageError`) and value parses via `strconv.ParseBool` (else `usageError`), then loading/mutating/rewriting `.atcr/config.yaml`.
- `cmd/atcr/main.go` - modify: register `newConfigCmd()` in `newRootCmd`'s `AddCommand` list (`cmd/atcr/main.go:185-208`).
- `internal/registry/project.go` - modify: add `Telemetry *bool` field to `ProjectConfig` struct (`internal/registry/project.go:56-90`), `yaml:"telemetry,omitempty"`, matching the `Sandbox`/`AutoFix`/`MaxParallel` pointer idiom so an explicit `false` survives default application.
- `cmd/atcr/config_test.go` - create: unit tests for `atcr config set telemetry false`/`true`, invalid key, invalid value, and round-trip persistence.

## Happy Path Scenarios
**Scenario 1: `atcr config set telemetry false` persists and disables**
- **Given** a project with an existing `.atcr/config.yaml` and no `ATCR_TELEMETRY` env var set
- **When** `atcr config set telemetry false` runs, then a subsequent `atcr review` invocation runs (in a separate process/invocation) against a mock telemetry endpoint
- **Then** `.atcr/config.yaml` contains `telemetry: false`, and the subsequent `review` makes zero HTTP requests to the mock endpoint

**Scenario 2: `atcr config set telemetry true` re-enables**
- **Given** `.atcr/config.yaml` currently has `telemetry: false`
- **When** `atcr config set telemetry true` runs
- **Then** `.atcr/config.yaml` is rewritten with `telemetry: true`, and a subsequent `atcr review` (with no env override) sends its telemetry ping normally

**Scenario 3: `atcr config` with no subcommand prints help**
- **Given** the `config` command group is registered
- **When** `atcr config` runs with no subcommand
- **Then** it prints help output and exits 0 (matching `newDebtCmd`'s `RunE: cmd.Help` behavior)

## Edge Cases
**Edge Case 1: unset config field leaves telemetry enabled by default**
- **Given** `.atcr/config.yaml` has no `telemetry` key at all (a project that predates this story, or never ran `config set`)
- **When** the config is loaded and telemetry state is resolved
- **Then** the effective state is enabled (`Telemetry == nil` is treated as `true`), matching the epic's default-on posture from Story 1

**Edge Case 2: value accepts the same boolean vocabulary as `strconv.ParseBool`**
- **Given** `atcr config set telemetry 0` or `atcr config set telemetry True` is run
- **When** the value is parsed
- **Then** `0` is accepted as `false` and `True` is accepted as `true`, consistent with Go's `strconv.ParseBool` accepted set

**Edge Case 3: repeated `set` calls are idempotent**
- **Given** `.atcr/config.yaml` already has `telemetry: false`
- **When** `atcr config set telemetry false` runs again
- **Then** the file is rewritten with the same value and no error occurs (no "already set" failure)

## Error Conditions
**Error Scenario 1: unknown config key**
- **Given** `atcr config set foo bar` is run (any key other than `telemetry`)
- **When** the command validates its first positional argument
- **Then** it returns a `usageError` (exit code 2)
- Error message: `unsupported config key "foo": only "telemetry" is supported`
- HTTP status / error code: exit 2 (usage error)

**Error Scenario 2: non-boolean value**
- **Given** `atcr config set telemetry maybe` is run
- **When** the command attempts `strconv.ParseBool("maybe")`
- **Then** it returns a `usageError` (exit code 2)
- Error message: `invalid value "maybe" for telemetry: must be a boolean (true/false/1/0)`
- HTTP status / error code: exit 2 (usage error)

**Error Scenario 3: wrong argument count**
- **Given** `atcr config set telemetry` (missing value) or `atcr config set telemetry false extra` (too many args) is run
- **When** cobra's `Args: usageArgs(cobra.ExactArgs(2))` validates positional args
- **Then** it returns a `usageError` (exit code 2) before `RunE` executes
- Error message: cobra's standard `accepts 2 arg(s), received N` wrapped by `usageError`
- HTTP status / error code: exit 2 (usage error)

**Error Scenario 4: `.atcr/config.yaml` missing or unwritable**
- **Given** no `.atcr/` directory exists in the working tree, or the file is not writable (permissions)
- **When** `atcr config set telemetry false` attempts to load/write the file
- **Then** it returns a wrapped I/O error (not a `usageError` — this is an environment failure, not a usage mistake), non-zero exit
- Error message: `read/write .atcr/config.yaml: <underlying os error>`
- HTTP status / error code: exit 1 (general failure)

## Performance Requirements
- **Response Time:** `config set` completes in well under 100ms — a single file read, YAML unmarshal, field mutation, YAML marshal, file write; no network calls.
- **Throughput:** N/A — single-invocation, single-file operation, not a hot path.

## Security Considerations
- **Authentication/Authorization:** N/A — local file mutation under the user's own project directory; no elevated privileges required or used.
- **Input Validation:** The config key is restricted to an allowlist of exactly `"telemetry"` (rejecting anything else, scoping the surface per the plan's decision); the value is strictly parsed via `strconv.ParseBool` with no fallback interpretation. File writes go through `internal/registry`'s existing YAML marshal path (no raw string interpolation into the file), avoiding YAML injection from the CLI argument.

## Test Implementation Guidance
**Test Type:** UNIT (`config set` argument validation, `ProjectConfig.Telemetry` marshal/unmarshal round-trip) + INTEGRATION (persisted config actually gates a subsequent `review`/`reconcile` invocation's telemetry client)
**Test Data Requirements:** A temp directory fixture with a minimal `.atcr/config.yaml` (via `t.TempDir()` + `registry.DefaultProjectConfigYAML`); YAML fixtures with `telemetry` present-true, present-false, and absent.
**Mock/Stub Requirements:** No network mocks needed for the pure `config set` unit tests; the cross-invocation integration test reuses the `httptest.Server` mock telemetry endpoint from AC 02-01.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `cmd/atcr/config.go` created with `newConfigCmd()`/`newConfigSetCmd()` following the `debt.go` subcommand-group pattern, registered in `newRootCmd`
- [ ] `ProjectConfig.Telemetry *bool` field added to `internal/registry/project.go` with `omitempty` YAML tag
- [ ] `atcr config set telemetry false` persists to `.atcr/config.yaml` and a subsequent invocation with no env var makes zero telemetry HTTP requests
- [ ] `atcr config set telemetry true` re-enables; invalid key and invalid value both return `usageError` (exit 2)

**Manual Review:**
- [ ] Code reviewed and approved
