# Acceptance Criteria: Result Accounting and Backward Compatibility

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Result Fields | Go struct fields on `Result` (`Turns int`, `ToolCalls int`, `ToolBytes int64`) | `internal/fanout/engine.go` |
| Status Propagation | `statusFor()` reads `Result` fields and populates `AgentStatus` | `internal/fanout/artifacts.go` |
| PayloadContext | `ToolsEnabled bool` field on `PayloadContext` | `internal/payload/template.go` |
| Test Framework | `go test` | Existing + new tests |
| Key Dependencies | `encoding/json` (stdlib) | |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:56` - modify: `Result` struct gains `Turns int`, `ToolCalls int`, `ToolBytes int64` fields
- `internal/fanout/artifacts.go:176` - modify: `statusFor()` propagates `Result.Turns`/`ToolCalls`/`ToolBytes` to `AgentStatus`
- `internal/payload/template.go:15` - modify: `PayloadContext` gains `ToolsEnabled bool` field
- `internal/payload/personas_render_test.go:11` - modify: sample context includes `ToolsEnabled: false` so existing tests pass
- `internal/fanout/review.go:411` - modify: `buildAgent` sets `PayloadContext.ToolsEnabled` from `AgentConfig.Tools`

## Happy Path Scenarios
**Scenario 1: Result fields populated during multi-turn loop**
- **Given** an agent completes a 3-turn loop with 4 total tool calls and 2048 cumulative tool-result bytes
- **When** the loop exits normally
- **Then** `Result.Turns == 3`, `Result.ToolCalls == 4`, `Result.ToolBytes == 2048`

**Scenario 2: Result fields propagated to status.json**
- **Given** a completed agent with `Result.Turns: 3`, `Result.ToolCalls: 4`, `Result.ToolBytes: 2048`
- **When** `statusFor()` builds `AgentStatus`
- **Then** `AgentStatus.Turns` points to `3`, `AgentStatus.ToolCalls` points to `4`, `AgentStatus.ToolBytes` points to `2048`

**Scenario 3: PayloadContext.ToolsEnabled set from AgentConfig**
- **Given** an `AgentConfig` with `Tools: true`
- **When** `buildAgent` constructs the payload context
- **Then** `PayloadContext.ToolsEnabled == true`; persona template can use `{{if .ToolsEnabled}}` to conditionally render tool-aware sections

**Scenario 4: All existing tests remain green (single-shot path unchanged)**
- **Given** agents with `Tools: false` (or without `Tools` field set)
- **When** all existing engine tests run
- **Then** every pre-story test passes without modification; the single-shot code path is identical

## Edge Cases
**Edge Case 1: Result fields are zero for single-shot (non-tool) agents**
- **Given** an agent with `Tools: false` completes via single-shot
- **When** `Result` is returned
- **Then** `Result.Turns == 0`, `Result.ToolCalls == 0`, `Result.ToolBytes == 0` (zero values; no misleading data)

**Edge Case 2: AgentStatus pointer fields are nil for non-tool agents**
- **Given** a non-tool agent's `Result` has zero Turns/ToolCalls/ToolBytes
- **When** `statusFor()` builds `AgentStatus`
- **Then** the `*int Turns`, `*int ToolCalls`, `*int64 ToolBytes` fields in `AgentStatus` remain nil (not serialized to JSON)

**Edge Case 3: PayloadContext.ToolsEnabled defaults to false**
- **Given** a `PayloadContext` created without explicitly setting `ToolsEnabled`
- **When** the struct is initialized
- **Then** `ToolsEnabled == false` (Go zero value for bool); persona templates render non-tool sections

## Error Conditions
**Error Scenario 1: statusFor() receives Result with negative byte count**
- **Given** a bug causes `Result.ToolBytes` to go negative
- **When** `statusFor()` propagates it
- **Then** this scenario is prevented by construction (bytes are only added, never subtracted); a test verifies the invariant `Result.ToolBytes >= 0`

## Performance Requirements
- **Response Time:** `statusFor()` propagation adds O(1) overhead (field copy / pointer assignment)
- **Throughput:** `Result` struct size increase is negligible (16 bytes for int+int+int64)

## Security Considerations
- **Authentication/Authorization:** N/A — accounting fields are read-only output, not input vectors
- **Input Validation:** `PayloadContext.ToolsEnabled` is a bool set from config; no user input flows into it

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** (a) Result fixtures with various Turns/ToolCalls/ToolBytes values; (b) existing engine test suite run with `Tools: false` agents; (c) persona template render tests with `ToolsEnabled: true` and `ToolsEnabled: false`
**Mock/Stub Requirements:** Existing mock infrastructure; no new mocks required for backward compatibility tests

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/... ./internal/payload/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)
- [ ] All pre-story tests pass without modification

**Story-Specific:**
- [ ] `Result` struct has `Turns`, `ToolCalls`, `ToolBytes` fields
- [ ] `statusFor()` propagates all three fields to `AgentStatus` pointer fields
- [ ] `PayloadContext.ToolsEnabled` is set from `AgentConfig.Tools` in `buildAgent`
- [ ] Existing engine tests remain green with no modifications

**Manual Review:**
- [ ] Code reviewed and approved
