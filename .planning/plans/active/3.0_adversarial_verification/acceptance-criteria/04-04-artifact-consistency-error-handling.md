# Acceptance Criteria: Artifact Consistency, Error Handling & Integration Tests

**Related User Story:** [04: CLI Command & MCP Tool](../user-stories/04-cli-command-mcp-tool.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test Framework | `go test` + `testify/assert` | Integration tests with real fixtures |
| Fixture Management | `testdata/` directories | Golden-file artifacts for comparison |
| Key Dependencies | `internal/verify`, `cmd/atcr`, `internal/mcp` | Cross-package integration |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `cmd/atcr/main.go:97` - reference: CLI command registration
- `internal/mcp/server.go:57` - reference: MCP tool registration
- [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) — defines artifact guarantees and cross-interface consistency requirements

- `cmd/atcr/verify_test.go` - modify: integration tests for CLI invocation
- `cmd/atcr/review_test.go` - modify: integration tests for `--verify` chaining
- `internal/mcp/handlers_test.go` - modify: integration tests for MCP handler
- `internal/verify/verify.go` - reference: `Verify` and `Result.Emit` produce the artifacts
- `internal/reconcile/emit.go:145` - reference: `ReadReconciledFindings` input loader

## Happy Path Scenarios

**Scenario 1: CLI verify and MCP verify produce identical artifacts**
- **Given** a fixture review directory with reconciled findings
- **When** `atcr verify <dir>` is run via CLI, then `atcr_verify` is called via MCP handler on the same directory
- **Then** the emitted `verification.json`, `findings.json`, `manifest.json`, and `summary.json` are byte-identical (or semantically equivalent) between the two runs

**Scenario 2: `atcr review --verify` produces same artifacts as running stages separately**
- **Given** a fixture diff input
- **When** `atcr review --verify <diff>` is run, and separately `atcr review --reconcile <diff>` then `atcr verify <dir>` is run
- **Then** the final artifacts from both approaches are identical

**Scenario 3: All artifact types present after successful verify**
- **Given** a successful verify run via any entry point
- **When** the review directory is inspected after the run
- **Then** the directory contains: `verification.json`, re-emitted `reconciled/findings.json` with verification blocks, `manifest.json` with `"verify"` in stages array, `summary.json` with `verdictCounts`

## Edge Cases

**Edge Case 1: Idempotent re-runs via CLI**
- **Given** a review directory with artifacts from a previous `atcr verify` run
- **When** `atcr verify <dir>` is run again with the same input
- **Then** artifacts are overwritten; content is deterministic for the same input and options

**Edge Case 2: `--verify` and `--reconcile` together do not double-reconcile**
- **Given** `atcr review --verify --reconcile <diff>` is executed
- **When** the manifest is inspected after the run
- **Then** the `"reconcile"` stage appears exactly once in the manifest stages array

**Edge Case 3: Verify after reconcile with empty findings**
- **Given** reconciled findings with an empty array
- **When** verify is invoked via any entry point
- **Then** `verification.json` contains empty verdict counts; `manifest.json` still includes `"verify"` stage; no errors

## Error Conditions

**Error Scenario 1: Missing reconciled findings — all entry points**
- **Given** a review directory without `reconciled/findings.json`
- **When** verify is invoked via CLI (`atcr verify`), chained (`atcr review --verify`), or MCP (`atcr_verify`)
- **Then** all three entry points produce the same error message: `"no reconciled findings found in <path> — run 'atcr reconcile' first"`

**Error Scenario 2: Skeptic failure produces `unverifiable` — never crashes**
- **Given** a finding where the skeptic LLM returns an error
- **When** verify is invoked
- **Then** the finding receives verdict `unverifiable`; the run completes successfully; the error is logged but does not propagate

**Error Scenario 3: Gate failure does not prevent artifact emission**
- **Given** `--fail-on HIGH` is set and there are HIGH-severity confirmed findings
- **When** verify completes with a failing gate
- **Then** all artifacts are still emitted; the CLI exits with non-zero code; the MCP result includes gate failure status

## Performance Requirements
- **Response Time:** Integration tests complete in < 30 seconds per test case (mock LLM calls)
- **Throughput:** Artifact emission writes all four files in < 100ms

## Security Considerations
- **Authentication/Authorization:** N/A for test fixtures
- **Input Validation:** Integration tests cover invalid inputs (missing files, bad severity, empty findings)
- **Error Messages:** Verified to not contain sensitive information (API keys, internal paths beyond the review dir)

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:**
- Fixture review directories with `reconciled/findings.json` (golden files)
- Registry configs
- Expected output artifacts for comparison
- Fixture diffs for `atcr review --verify` tests

**Mock/Stub Requirements:**
- Mock LLM API calls in `verify.Verify` to return deterministic verdicts
- Use temp directories for artifact writes
- Mock registry loading if needed for error path tests

**Test Cases Matrix:**
| Test | Entry Point | Input | Expected |
|------|-------------|-------|----------|
| CLI basic | `atcr verify <dir>` | Valid dir + findings | Artifacts emitted, summary printed |
| CLI all flags | `atcr verify <dir> --fresh --thorough --min-severity HIGH` | Valid dir | Artifacts with filtered results |
| CLI missing input | `atcr verify <dir>` | No reconciled findings | Error: run reconcile first |
| CLI invalid severity | `atcr verify <dir> --min-severity X` | Bad value | Error: valid severity levels |
| Chain basic | `atcr review --verify <diff>` | Valid diff | All 3 stages run, artifacts emitted |
| Chain implies reconcile | `atcr review --verify <diff>` | Without `--reconcile` | Reconcile runs automatically |
| Chain both flags | `atcr review --verify --reconcile <diff>` | Valid diff | Reconcile runs once |
| Chain backward compat | `atcr review --reconcile <diff>` | Valid diff | No verify stage |
| MCP basic | `atcr_verify {path}` | Valid dir | VerifyResult returned |
| MCP all params | `atcr_verify {path, fresh, thorough, minSeverity, failOn}` | Valid dir | VerifyResult with gate status |
| MCP missing input | `atcr_verify {path}` | No reconciled findings | MCP error returned |
| CLI == MCP | CLI verify + MCP verify | Same input | Identical artifacts |
| Idempotent | `atcr verify <dir>` x2 | Same dir | Same artifacts both times |

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/... ./internal/mcp/...` passes (including new integration tests)
- [ ] `go vet ./...` clean
- [ ] `go build ./cmd/atcr/...` succeeds

**Story-Specific:**
- [ ] CLI verify and MCP verify produce identical artifacts for the same input
- [ ] `atcr review --verify` produces same artifacts as running stages separately
- [ ] Missing reconciled findings error is identical across all three entry points
- [ ] Skeptic failures produce `unverifiable` verdicts without crashing
- [ ] All flag combinations tested: `--fresh`, `--thorough`, `--min-severity`, `--verify`, `--reconcile`
- [ ] Idempotent re-runs produce deterministic output
- [ ] Gate failure still emits artifacts

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Test coverage >= 90% on new code paths (`verify.go`, `handleVerify`, chaining logic)
- [ ] Error messages reviewed for clarity and consistency
