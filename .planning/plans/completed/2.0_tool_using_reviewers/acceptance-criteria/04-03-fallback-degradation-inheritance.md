# Acceptance Criteria: Fallback Degradation Inheritance

**Related User Story:** [04: Graceful Degradation](../user-stories/04-graceful-degradation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Fallback Dispatch | Existing `invokeSlot` chain in `engine.go` | Fallbacks already iterate after primary failure |
| Degrade Check | Per-agent in `invokeAgent` | Each fallback re-evaluates against its own model's capability |
| Status Fields | `ToolsDegraded`, `ToolsRequested` | Per-agent `AgentStatus` records the degrade signal |
| Lane Tools Flag | Threaded through `Slot` construction | Lane's resolved `tools` setting passed to each agent in chain |
| Test Framework | `go test` + table-driven | Multi-agent chain with mixed capabilities |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:169` - modify: thread `tools` flag through fallback dispatch in `invokeSlot`; each fallback agent re-evaluates degrade in `invokeAgent`
- `internal/fanout/engine.go:228` - modify: per-agent degrade check inside `invokeAgent`
- `internal/fanout/review.go:460` - modify: `buildFallbackAgent` threads the lane's `tools` setting into the fallback's `Agent` struct
- `internal/fanout/status.go:225` - modify: `AgentStatus` carries `ToolsDegraded` and `ToolsRequested`
- `internal/fanout/engine_test.go` - modify: tests for per-agent degrade in fallback chains

## Spec Alignment Notes

- **Degradation is per-agent, not per-slot** — each agent in the fallback chain independently checks its own model's `supports_function_calling` against the lane's `tools` setting. A primary may run the tool loop while its fallback degrades to single-shot.
- **Fallbacks inherit the lane's effective `tools` setting** — not the primary agent's resolved mode. If the lane was invoked with `tools: true`, every fallback in the chain attempts `tools: true` and degrades independently if its model is incapable.
- **Each fallback records its own `tools_degraded` signal** — the `AgentStatus` for the fallback agent carries its own degrade status, distinct from the primary's.
- **No cascading failure from degrade** — a degraded fallback that produces a valid single-shot response is `status: ok` with `tools_degraded: true`. It is not a failure; it is a successful degradation.

## Happy Path Scenarios

**Scenario 1: Primary runs tool loop, fallback degrades to single-shot**
- **Given** a slot with a primary agent (tool-capable model) and a fallback agent (non-tool-capable model)
- **And** the lane is configured with `tools: true`
- **When** the primary agent fails and the fallback is invoked
- **Then** the fallback agent degrades to single-shot (its model lacks `supports_function_calling`)
- **And** the fallback's `AgentStatus` has `tools_degraded: true`

**Scenario 2: Both primary and fallback are non-tool-capable; both degrade**
- **Given** a slot where both primary and fallback models lack `supports_function_calling`
- **And** the lane is configured with `tools: true`
- **When** the primary fails and the fallback is invoked
- **Then** the primary's `AgentStatus` has `tools_degraded: true`
- **And** the fallback's `AgentStatus` also has `tools_degraded: true`
- **And** each agent independently recorded its own degrade event

**Scenario 3: Fallback is tool-capable; no degradation on fallback**
- **Given** a slot with a non-tool-capable primary and a tool-capable fallback
- **And** the lane is configured with `tools: true`
- **When** the primary degrades to single-shot and fails
- **And** the fallback is invoked
- **Then** the fallback runs the tool loop successfully
- **And** the fallback's `AgentStatus` has `tools_degraded: false`

**Scenario 4: Lane configured with `tools: false`; no fallback degradation**
- **Given** a lane with `tools: false` (single-shot mode)
- **When** any agent in the fallback chain runs
- **Then** no degrade logic fires for any agent
- **And** `tools_degraded` is `false` (or absent) for all agents

## Edge Cases

**Edge Case 1: Fallback chain of three agents with mixed capabilities**
- **Given** a primary (non-tool-capable), fallback1 (tool-capable), fallback2 (non-tool-capable)
- **And** the lane is configured with `tools: true`
- **When** primary fails, fallback1 runs the tool loop successfully
- **Then** fallback2 is never invoked (fallback1 succeeded)
- **And** primary's status has `tools_degraded: true`
- **And** fallback1's status has `tools_degraded: false`

**Edge Case 2: Fallback chain where every agent degrades and all fail**
- **Given** a chain of three agents, all non-tool-capable, all with `tools: true`
- **When** all three degrade to single-shot and all three fail
- **Then** each agent's `AgentStatus` has `tools_degraded: true`
- **And** the slot's final status is `failed`
- **And** the degrade signals are preserved on every agent's status for diagnosis

**Edge Case 3: Fallback agent's `tools` field is explicitly set in its own config**
- **Given** a fallback agent whose own `AgentConfig` has `tools: false` while the lane has `tools: true`
- **When** the fallback is invoked
- **Then** the lane's effective `tools: true` takes precedence (the lane setting governs)
- **And** the fallback degrades based on its model's capability, not its own `tools` field

## Error Conditions

**Error Scenario 1: Fallback degradation is silent (operator never notices)**
- **Detection:** `tools_degraded: true` is always emitted on degrade, including fallbacks
- **Mitigation:** Per-agent `status.json` entries carry the degrade signal; operators inspect individual agent status for diagnosis

**Error Scenario 2: Fallback chain references an agent not in the registry**
- **Error message:** "fallback agent %q not found in registry"
- **Behavior:** Build-time failure in `buildSlots`; the review fails to start. This is an existing validation path unchanged by this story.

## Performance Requirements
- **Per-Agent Degrade Check:** < 1ms per fallback agent (same registry map lookup as primary)
- **No Additional Fallback Latency:** The degrade branch adds zero overhead to the fallback dispatch path; it is a check inside `invokeAgent` which is already called per agent
- **Status Write:** Multiple agents with `tools_degraded` each add < 100 bytes to their respective status.json files

## Security Considerations
- **Per-Agent Isolation:** Each fallback evaluates degradation against its own model's capability; a misconfigured primary cannot force a fallback into a tool loop the fallback's model cannot support
- **No Cross-Agent Leakage:** The `tools` flag is threaded through the lane, not inherited from the primary's runtime state; a primary that degraded does not alter the fallback's degrade decision
- **Default-Safe:** Fallback agents with undeclared `supports_function_calling` default to `false`, ensuring safe degradation

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Multi-agent fallback chains with mixed `supports_function_calling` values
- Lane configurations with `tools: true` and `tools: false`
- Fake completers that can simulate tool-loop and single-shot responses per agent
- `AgentStatus` assertions for `tools_degraded` and `tools_requested`

**Mock/Stub Requirements:**
- Fake `Completer` per agent to control tool-loop vs single-shot behavior
- Registry with mixed tool-capability models
- Slot construction with multiple fallback agents

**Test Cases:**
1. `TestFallback_PrimaryToolCapable_FallbackDegraded` — primary runs loop, fallback degrades
2. `TestFallback_BothDegraded` — both primary and fallback degrade independently
3. `TestFallback_FallbackToolCapable` — primary degrades, fallback runs loop
4. `TestFallback_ToolsFalse_NoDegradation` — tools:false lane never triggers degrade
5. `TestFallback_ChainMixedCapabilities` — three-agent chain with mixed capabilities
6. `TestFallback_AllDegradedAndFailed` — all agents degrade and fail; signals preserved

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Each fallback agent independently evaluates degradation against its own model's capability
- [ ] `tools_degraded` is recorded per-agent in the fallback chain, not per-slot
- [ ] Lane's effective `tools` setting is threaded through to every fallback agent

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Per-agent degrade semantics are clearly documented in code comments

