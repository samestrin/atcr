# Acceptance Criteria: `atcr_verify` MCP Tool

**Related User Story:** [04: CLI Command & MCP Tool](../user-stories/04-cli-command-mcp-tool.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP SDK | `internal/mcp` (mcpsdk) | Follows `handleReconcile` pattern at line 159 |
| Package | `internal/mcp` | Handler in `handlers.go`, registration in `server.go` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests matching existing MCP handler patterns |
| Key Dependencies | `internal/verify`, `internal/registry`, `internal/reconcile` | Backend pipeline from Stories 1-3 |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/mcp/server.go:57` - reference: `buildServer()` tool registration
- `internal/mcp/handlers.go:159` - reference: `handleReconcile` pattern to follow
- `internal/mcp/handlers.go:339` - reference: `failingFindings` gate helper
- [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) — defines `atcr_verify` MCP tool input/output schema and gate-status output

- `internal/mcp/handlers.go` - modify: add `handleVerify` function, `VerifyArgs` and `VerifyResult` structs, `ToolVerify` constant
- `internal/mcp/server.go` - modify: register `atcr_verify` in `buildServer()` at line 57 via `registerTool`
- `internal/mcp/handlers_test.go` - modify: add tests for `handleVerify` with various inputs
- `internal/mcp/server.go:57` - reference: `buildServer()` tool registration pattern

## Happy Path Scenarios

**Scenario 1: MCP verify with valid input**
- **Given** a review directory with `reconciled/findings.json` and a valid registry
- **When** the `atcr_verify` MCP tool is called with `{"path": "<review-dir>"}`
- **Then** the handler loads the registry, loads reconciled findings, calls `verify.Verify`, emits all artifacts, and returns a `VerifyResult` with `verdictCounts` (confirmed/refuted/unverifiable), `findingsProcessed`, `durationMs`, and `gateStatus`

**Scenario 2: MCP verify with all optional parameters**
- **Given** a review directory with reconciled findings
- **When** `atcr_verify` is called with `{"path": "<dir>", "fresh": true, "thorough": true, "minSeverity": "HIGH", "failOn": "HIGH", "requireVerified": true}`
- **Then** the handler passes all parameters through to `verify.Verify` options and gate counter; result reflects the filtered/gated output

**Scenario 3: MCP verify returns gate status**
- **Given** a review directory with findings and `failOn: "HIGH"` in the request
- **When** `atcr_verify` is called
- **Then** the result includes `gateStatus` with `pass`/`fail` and `failingCount` based on `CountAtOrAbove`

## Edge Cases

**Edge Case 1: MCP verify with empty findings**
- **Given** a review directory with reconciled findings containing an empty array
- **When** `atcr_verify` is called with `{"path": "<dir>"}`
- **Then** the handler returns `VerifyResult` with all verdict counts at zero, `findingsProcessed: 0`, and gate status pass (if applicable)

**Edge Case 2: MCP verify without optional parameters (defaults)**
- **Given** a review directory with reconciled findings
- **When** `atcr_verify` is called with only `{"path": "<dir>"}`
- **Then** defaults are applied: `fresh: false`, `thorough: false`, `minSeverity: "MEDIUM"`; result is correct for those defaults

**Edge Case 3: MCP verify with `requireVerified: true`**
- **Given** findings where some are unverifiable
- **When** `atcr_verify` is called with `{"path": "<dir>", "requireVerified": true}`
- **Then** the gate status accounts for unverifiable findings according to the `--require-verified` gate logic from Story 3

## Error Conditions

**Error Scenario 1: No reconciled findings**
- **Given** a review directory without `reconciled/findings.json`
- **When** `atcr_verify` is called with `{"path": "<dir>"}`
- **Then** the handler returns an MCP error with message: `"no reconciled findings found in <path> — run 'atcr reconcile' first"`

**Error Scenario 2: Registry load failure**
- **Given** a missing or malformed registry
- **When** `atcr_verify` is called
- **Then** the handler returns an MCP error wrapping the registry load error

**Error Scenario 3: Individual skeptic invocation failure**
- **Given** reconciled findings and a valid registry, but one skeptic agent fails during verification
- **When** `atcr_verify` is called
- **Then** the failed finding receives an `unverifiable` verdict; the handler completes successfully with remaining findings verified; the result reflects the partial verification

## Performance Requirements
- **Response Time:** Handler overhead (parsing, registry load, findings load) < 500ms; verification duration dominated by LLM API calls
- **Throughput:** Handler processes findings sequentially per skeptic (parallelism managed by `verify.Verify`)

## Security Considerations
- **Authentication/Authorization:** MCP tool runs within the MCP server's permission context; no additional auth needed
- **Input Validation:** `path` validated for existence; `minSeverity` validated against known constants; `registryPath` validated if provided
- **Error Messages:** Do not leak internal paths, API keys, or stack traces to MCP clients

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:**
- Fixture review directories with `reconciled/findings.json`
- Valid registry configs
- MCP request/response fixtures matching JSON-RPC format

**Mock/Stub Requirements:**
- Mock `verify.Verify` to avoid real LLM calls
- Use temp directories for artifact emission
- Mock MCP request context to simulate tool call

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/mcp/...` passes
- [ ] `go vet ./internal/mcp/...` clean
- [ ] `go build ./...` succeeds

**Story-Specific:**
- [ ] `ToolVerify` constant defined alongside existing tool constants
- [ ] `handleVerify` registered in `buildServer()` via `registerTool`
- [ ] `VerifyArgs` struct has all fields: `Path`, `Fresh`, `Thorough`, `MinSeverity`, `RegistryPath`, `FailOn`, `RequireVerified`
- [ ] `VerifyResult` struct has: `VerdictCounts`, `FindingsProcessed`, `DurationMs`, `GateStatus`
- [ ] Handler calls same `verify.Verify` function as CLI entry points
- [ ] Error messages follow established MCP error patterns

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Handler follows same pattern as `handleReconcile` (`internal/mcp/handlers.go:159`)
- [ ] `VerifyResult` JSON output is well-documented for MCP clients
