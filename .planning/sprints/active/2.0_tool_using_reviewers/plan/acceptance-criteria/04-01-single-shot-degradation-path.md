# Acceptance Criteria: Single-Shot Degradation Path

**Related User Story:** [04: Graceful Degradation](../user-stories/04-graceful-degradation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Degrade Branch | Go `if` in `invokeAgent` | Check registry capability before tool loop |
| Registry Field | `SupportsFunctionCalling bool` on model/provider descriptor | Parsed from `registry.yaml`, default `false` |
| Status Field | `ToolsDegraded bool` on `AgentStatus` | `json:"tools_degraded,omitempty"` |
| Status Field | `ToolsRequested bool` on `AgentStatus` | Preserves original request intent |
| Completer | Existing `Completer.Complete()` single-shot path | Reused unchanged from 1.x |
| Test Framework | `go test` + table-driven | Fake completer asserts single-shot invocation |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:228` - modify: add degrade branch in `invokeAgent` before tool loop starts
- `internal/fanout/status.go:225` - modify: add `ToolsDegraded` and `ToolsRequested` fields to `AgentStatus`
- `internal/registry/config.go:54` - modify: add `SupportsFunctionCalling` field to model/provider descriptor or `AgentConfig`
- `internal/fanout/engine_test.go` - create: table-driven tests for degrade branch (tool-capable vs non-tool-capable)
- `internal/registry/config_test.go` - modify: verify `supports_function_calling` parsed from YAML

## Spec Alignment Notes

- **Registry is the sole source of truth** — no dynamic capability probing at runtime. The engine consults the registry before starting any tool loop.
- **Default is `false`** — a model is assumed non-tool-capable unless explicitly declared otherwise. This prevents silent failures from incorrect declarations.
- **Single-shot path is reused unchanged** — the existing `Completer.Complete()` call from 1.x is invoked without modification. The degrade branch only redirects flow, it does not alter the completion contract.
- **`tools_requested` is always preserved** — even when degradation occurs, the operator sees what was originally requested. This enables post-hoc audit of degrade events.

## Happy Path Scenarios

**Scenario 1: Non-tool-capable model with `tools: true` degrades to single-shot**
- **Given** an agent configured with `tools: true` in `registry.yaml`
- **And** the agent's model entry has `supports_function_calling: false` (or the field is absent, defaulting to false)
- **When** `invokeAgent` is called for this agent
- **Then** the engine skips the tool loop entirely
- **And** invokes the existing `Completer.Complete()` single-shot path
- **And** the resulting `AgentStatus` has `tools_degraded: true`
- **And** the `AgentStatus` preserves `tools_requested: true`

**Scenario 2: Agent configured with `tools: false` runs single-shot without degradation**
- **Given** an agent configured with `tools: false` (default)
- **When** `invokeAgent` is called
- **Then** the engine invokes the single-shot `Completer.Complete()` path
- **And** the `AgentStatus` has `tools_degraded: false` (or field absent)
- **And** no degrade logic is triggered

**Scenario 3: `tools_requested` reflects the agent's configuration**
- **Given** an agent configured with `tools: true`
- **When** the agent completes (either tool-loop or degraded single-shot)
- **Then** the `AgentStatus.tools_requested` field is `true`
- **And** when the agent is configured with `tools: false`, `tools_requested` is `false`

## Edge Cases

**Edge Case 1: Registry field absent (missing `supports_function_calling`)**
- **Given** a model entry in `registry.yaml` with no `supports_function_calling` field
- **When** the engine consults the registry for capability
- **Then** the field defaults to `false`
- **And** the agent degrades to single-shot if `tools: true` was requested

**Edge Case 2: Agent has `tools: true` but no tool loop was planned (1.x compatibility)**
- **Given** a 1.x agent config that has `tools: false` (or unset)
- **When** the engine invokes the agent
- **Then** `tools_degraded` is `false` (or absent)
- **And** `tools_requested` is `false`
- **And** no degrade logic fires

**Edge Case 3: `tools_degraded` omitted from 1.x status.json**
- **Given** a 1.x review (no tool loop code path exists)
- **When** `AgentStatus` is serialized
- **Then** `tools_degraded` field is absent from the JSON (omitempty)
- **And** `tools_requested` field is absent from the JSON (omitempty)
- **And** backward compatibility is maintained

## Error Conditions

**Error Scenario 1: Registry incorrectly declares `supports_function_calling: true` for a model that silently ignores `tools`**
- **Error detection:** The engine logs a warning when the first response in a tool loop contains no `tool_calls` but the agent is configured for tools
- **Error message (warning log):** "agent %s: model %s declared supports_function_calling=true but first response has no tool_calls — possible misconfiguration"
- **Behavior:** This is a warning only, not a hard failure. The agent continues in tool-loop mode.

**Error Scenario 2: Registry parse failure for `supports_function_calling`**
- **Error message:** "agent '%s': supports_function_calling must be a boolean, got %T"
- **Behavior:** Load-time validation error; the review fails to start

## Performance Requirements
- **Degrade Decision Latency:** The capability check against the registry adds < 1ms per agent (map lookup, no I/O)
- **No Additional API Calls:** Degraded agents make exactly one LLM API call (single-shot), same as 1.x
- **Status Write Latency:** `tools_degraded` and `tools_requested` fields add < 100 bytes to status.json; no measurable I/O impact

## Security Considerations
- **Input Validation:** `supports_function_calling` is validated at load time as a boolean; invalid values are rejected before any review starts
- **No Runtime Probing:** The engine does not probe the provider for capabilities at runtime, preventing information leakage about model internals
- **Default-Safe:** Default `false` means unknown models are never assumed tool-capable, preventing silent tool-loop failures

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- `AgentConfig` with `tools: true` and `supports_function_calling: false`
- `AgentConfig` with `tools: true` and `supports_function_calling: true`
- `AgentConfig` with `tools: false` (default)
- Fake `Completer` that records whether `Complete()` was called
- `AgentStatus` serialization/deserialization test fixtures

**Mock/Stub Requirements:**
- Fake `Completer` implementing `Complete(ctx, inv) (string, error)` to assert single-shot vs tool-loop path
- Registry loaded from test YAML with varying `supports_function_calling` values

**Test Cases:**
1. `TestInvokeAgent_DegradeToSingleShot` — non-tool-capable model with tools:true degrades
2. `TestInvokeAgent_ToolCapableRunsLoop` — tool-capable model with tools:true runs loop
3. `TestInvokeAgent_ToolsFalseNoDegradation` — tools:false never triggers degrade
4. `TestAgentStatus_ToolsDegradedField` — serialization omits `tools_degraded` when false and emits it when true
5. `TestRegistry_SupportsFunctionCallingDefault` — absent field defaults to false

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/fanout/... ./internal/registry/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] Non-tool-capable model with `tools: true` invokes single-shot `Completer.Complete()` (TestInvokeAgent_DegradeToSingleShot)
- [x] `AgentStatus.tools_degraded` is `true` after degrade event
- [x] `AgentStatus.tools_requested` preserves the original `tools` config value (TestInvokeAgent_DegradeStatusPreservesRequested)
- [x] `tools_degraded` field is absent from 1.x status.json (omitempty backward compat) (TestInvokeAgent_SingleShotStatusOmitsToolFields)

**Manual Review:**
- [x] Code reviewed and approved (4.2.A adversarial subagent)
- [x] Degrade branch in `invokeAgent` is a single `if` check, minimal code change
- [x] Warning log for misconfigured `supports_function_calling: true` is documented (loop.go first-turn warning)
