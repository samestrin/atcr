# Acceptance Criteria: `atcr verify` CLI Subcommand

**Related User Story:** [04: CLI Command & MCP Tool](../user-stories/04-cli-command-mcp-tool.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Framework | Cobra (`github.com/spf13/cobra`) | Matches existing `atcr reconcile` pattern |
| Package | `cmd/atcr` | New file `verify.go`, registration in `main.go` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests matching existing CLI patterns |
| Key Dependencies | `internal/verify`, `internal/registry`, `internal/reconcile` | Backend pipeline from Stories 1-3 |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:145` - reference: `ReadReconciledFindings` (input loader)
- `cmd/atcr/main.go:97` - reference: `AddCommand` registration point
- [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) — defines `atcr verify` CLI conventions, flags, exit codes, and gate semantics

- `cmd/atcr/verify.go` - create: `verifyCmd` Cobra command with flags `--fresh`, `--thorough`, `--min-severity`
- `cmd/atcr/main.go` - modify: add `rootCmd.AddCommand(verifyCmd)` at line 97
- `cmd/atcr/verify_test.go` - create: unit tests for verify command flag parsing and execution
- `internal/verify/verify.go` - reference: `Verify(findings, reg, opts)` called by the command

## Happy Path Scenarios

**Scenario 1: Run verify on a review directory with reconciled findings**
- **Given** a review directory containing `reconciled/findings.json` and a valid registry file
- **When** `atcr verify <review-dir>` is executed
- **Then** the command loads the registry, loads reconciled findings, calls `verify.Verify`, emits all artifacts (`verification.json`, re-emitted `findings.json`, updated `manifest.json` with `"verify"` in stages, updated `summary.json` with `verdictCounts`), and prints a human-readable summary to stdout

**Scenario 2: Run verify with `--fresh` flag**
- **Given** a review directory with reconciled findings
- **When** `atcr verify <review-dir> --fresh` is executed
- **Then** `verify.Options{Fresh: true}` is passed to `verify.Verify`, producing fresh-only verification results

**Scenario 3: Run verify with `--thorough` flag**
- **Given** a review directory with reconciled findings
- **When** `atcr verify <review-dir> --thorough` is executed
- **Then** `verify.Options{Thorough: true}` is passed, producing thorough verification results

**Scenario 4: Run verify with `--min-severity` flag**
- **Given** a review directory with reconciled findings of mixed severities
- **When** `atcr verify <review-dir> --min-severity HIGH` is executed
- **Then** only findings at or above HIGH severity are verified; lower-severity findings are excluded

**Scenario 5: Verify help output**
- **Given** the `atcr` binary is built
- **When** `atcr verify --help` is executed
- **Then** usage text is printed listing the command, all three flags (`--fresh`, `--thorough`, `--min-severity`), and their defaults

## Edge Cases

**Edge Case 1: Verify on a review directory with zero findings**
- **Given** a review directory with `reconciled/findings.json` containing an empty findings array
- **When** `atcr verify <review-dir>` is executed
- **Then** the command completes successfully with verdict counts all at zero, emits artifacts, and prints a summary indicating no findings were processed

**Edge Case 2: Verify on a review with all findings already verified (idempotent re-run)**
- **Given** a review directory that already contains `verification.json` from a previous run
- **When** `atcr verify <review-dir>` is executed again
- **Then** the command overwrites artifacts with fresh results; the output is deterministic for the same input

**Edge Case 3: Verify with review ID instead of path**
- **Given** a valid review ID registered in the atcr data directory
- **When** `atcr verify <review-id>` is executed
- **Then** the command resolves the ID to a review directory and proceeds normally

## Error Conditions

**Error Scenario 1: No reconciled findings found**
- **Given** a review directory that does not contain `reconciled/findings.json`
- **When** `atcr verify <review-dir>` is executed
- **Then** the command exits with a non-zero exit code and prints: `"no reconciled findings found in <path> — run 'atcr reconcile' first"`

**Error Scenario 2: Invalid `--min-severity` value**
- **Given** the `atcr verify` command
- **When** `atcr verify <review-dir> --min-severity INVALID` is executed
- **Then** the command exits with a non-zero exit code and prints an error indicating valid severity levels (LOW, MEDIUM, HIGH, CRITICAL)

**Error Scenario 3: Registry load failure**
- **Given** a missing or malformed registry file
- **When** `atcr verify <review-dir>` is executed
- **Then** the command exits with a non-zero exit code and propagates the registry load error

## Performance Requirements
- **Response Time:** Command startup (flag parsing + registry load + findings load) completes in < 500ms for registries with up to 100 agents
- **Throughput:** Verification duration is dominated by LLM API calls (Story 2/3); command overhead adds < 100ms

## Security Considerations
- **Authentication/Authorization:** N/A — CLI runs with user's local permissions; LLM API keys sourced from environment/config
- **Input Validation:** Review directory path validated for existence; `--min-severity` validated against known constants; registry path validated for readability

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Fixture review directories with `reconciled/findings.json`
- Valid registry config files
- Empty findings fixtures

**Mock/Stub Requirements:**
- Mock `verify.Verify` to avoid real LLM calls in unit tests
- Use in-memory registry fixtures
- Stub `result.Emit` to verify artifact writes without filesystem side effects (or use temp dirs)

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go vet ./cmd/atcr/...` clean
- [ ] `go build ./cmd/atcr/...` succeeds
- [ ] `atcr verify --help` prints usage with all flags

**Story-Specific:**
- [ ] `verifyCmd` registered in `main.go` via `AddCommand`
- [ ] `--fresh`, `--thorough`, `--min-severity` flags defined with correct defaults
- [ ] Missing reconciled findings produces clear error suggesting `atcr reconcile`
- [ ] Command calls `verify.Verify` and `result.Emit` with correct arguments
- [ ] Stdout summary includes verdict counts and gate status

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Cobra command follows same pattern as `atcr reconcile` (`cmd/atcr/reconcile.go`)
