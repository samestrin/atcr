# Acceptance Criteria: Degrade Path and Fallback Inheritance

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Degrade Detection | Type assertion `completer.(ChatCompleter)` returns `ok=false` | `internal/fanout/engine.go` |
| Status Flag | `ToolsDegraded bool` field on `AgentStatus` | `internal/fanout/status.go` |
| Fallback Builder | `buildFallbackAgent` propagates tool fields from effective `AgentConfig` | `internal/fanout/review.go` |
| Test Framework | `go test` + `net/http/httptest` | Mock non-tool-capable completer; fallback scenarios |
| Key Dependencies | `encoding/json` (stdlib) | |

## Related Files
- `internal/fanout/engine.go` - modify: degrade detection when `ChatCompleter` assertion fails; fall back to single-shot
- `internal/fanout/status.go` - modify: add `ToolsDegraded bool` field with `json:"tools_degraded,omitempty"` to `AgentStatus`
- `internal/fanout/review.go` - modify: `buildFallbackAgent` (line ~460) propagates `Tools`/`MaxTurns`/`ToolBudgetBytes` from effective config
- `internal/fanout/engine_test.go` - create/modify: degrade path test, fallback-with-tools test, fallback-without-tools test
- `internal/fanout/artifacts.go` - modify: `statusFor()` propagates `ToolsDegraded` to output

## Happy Path Scenarios
**Scenario 1: Non-tool-capable model degrades to single-shot**
- **Given** an agent with `Tools: true` but the injected `Completer` does NOT implement `ChatCompleter`
- **When** `invokeAgent` runs
- **Then** the engine falls back to the single-shot `Complete` path; the response is returned normally; `AgentStatus.ToolsDegraded == true`

**Scenario 2: tools_degraded recorded in status.json**
- **Given** a degraded agent completed its review
- **When** `statusFor()` builds the `AgentStatus`
- **Then** `AgentStatus.ToolsDegraded == true` is serialized as `"tools_degraded": true` in `status.json`

**Scenario 3: Non-degraded agent does not emit tools_degraded field**
- **Given** an agent with `Tools: true` that completes a multi-turn loop successfully
- **When** `statusFor()` builds the `AgentStatus`
- **Then** `AgentStatus.ToolsDegraded == false`; the `tools_degraded` field is omitted from JSON (omitempty)

**Scenario 4: Fallback agent inherits tools setting from effective config**
- **Given** a primary agent with `Tools: true`, `MaxTurns: 5`, `ToolBudgetBytes: 4096` in its effective `AgentConfig`
- **When** the primary fails and `buildFallbackAgent` constructs the fallback
- **Then** the fallback `Agent` has `Tools: true`, `MaxTurns: 5`, `ToolBudgetBytes: 4096`

## Edge Cases
**Edge Case 1: Fallback agent is non-tool (degrade is per-agent)**
- **Given** a primary agent with `Tools: true` and a fallback agent whose own `AgentConfig` has `Tools: false`
- **When** the fallback is invoked
- **Then** the fallback runs single-shot (its own `Tools: false`); the fallback's `ToolsDegraded` is false (it never tried tools)

**Edge Case 2: Both primary and fallback degrade**
- **Given** primary has `Tools: true` but `Completer` lacks `ChatCompleter`; fallback also has `Tools: true` but same `Completer`
- **When** both run
- **Then** both degrade independently; both have `ToolsDegraded: true`

**Edge Case 3: Tools enabled but agent has no tool definitions**
- **Given** an agent with `Tools: true` but the tool list is empty (no tools registered)
- **When** `invokeAgent` runs
- **Then** the loop sends `Chat` with empty tools array (or degrades); behavior is defined and tested

## Error Conditions
**Error Scenario 1: ChatCompleter assertion returns false but agent has Tools: true**
- **Given** type assertion `completer.(ChatCompleter)` returns `ok=false`
- **When** the engine handles the failed assertion
- **Then** a warning is logged; `ToolsDegraded` is set to `true`; single-shot `Complete` is called instead; no error returned to caller

**Error Scenario 2: Fallback agent construction fails to read tool config fields**
- **Given** the primary agent's `AgentConfig` has valid tool fields
- **When** `buildFallbackAgent` reads them
- **Then** the fallback agent correctly receives all three fields (`Tools`, `MaxTurns`, `ToolBudgetBytes`)

## Performance Requirements
- **Response Time:** Type assertion is O(1); degrade path adds no latency beyond the single-shot call
- **Throughput:** Degrade detection is per-agent; no global lock or shared state

## Security Considerations
- **Authentication/Authorization:** Degrade path uses existing `Complete` method — same auth as pre-story behavior
- **Input Validation:** Fallback agent config validated by existing registry validation; tool fields are boolean/int (no injection risk)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** (a) A `Completer` mock that does NOT implement `ChatCompleter`; (b) `AgentConfig` fixtures with various tool settings for primary and fallback; (c) status.json output verification
**Mock/Stub Requirements:** Mock `Completer` (non-ChatCompleter); mock `ChatCompleter` for non-degrade path; registry config fixtures

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Non-tool-capable `Completer` triggers degrade to single-shot
- [ ] `AgentStatus.ToolsDegraded` is `true` after degrade, omitted from JSON when false
- [ ] `buildFallbackAgent` propagates `Tools`, `MaxTurns`, `ToolBudgetBytes` from effective config
- [ ] Degrade is per-agent (fallback with `Tools: false` does not degrade)

**Manual Review:**
- [ ] Code reviewed and approved
