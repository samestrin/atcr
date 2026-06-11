# Acceptance Criteria: Payload Mode Configuration and Per-Agent Override

**Related User Story:** [06: Payload Mode Selection](../user-stories/06-payload-mode-selection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Project Config | Go `yaml.v3` | `.atcr/config.yaml` — default payload mode |
| Registry Config | Go `yaml.v3` | `registry.yaml` — per-agent `payload` field |
| Config Struct | Go struct with validation | `PayloadMode` type with allowed values |
| Resolution Logic | Go function | Config default → per-agent override |
| Test Framework | `testify` (assert, require) | Table-driven tests with YAML fixtures |

## Related Files
- `internal/config/config.go` - modify: Add `PayloadMode` field with default `"blocks"`
- `internal/registry/config.go` - modify: Add `Payload` field to `AgentConfig` struct
- `internal/payload/resolve.go` - create: Resolution logic — effective mode per agent
- `internal/payload/resolve_test.go` - create: Tests for config resolution and override

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Payload Engine](../documentation/payload-engine.md) — Authoritative spec for the three modes (`diff`, `blocks`, `files`), default `blocks`, per-agent override, and resolution logic.
- [Configuration & Registry](../documentation/configuration-management.md) — Two-tier config (project-level default + registry-level per-agent override); `KnownFields(true)` strict mode; precedence chain.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — How `manifest.json`'s `per_agent_payload` map is consumed by downstream tools (the `atcr_status` and `report.md` panel table use it).

### Spec alignment notes

- **Resolution order is exact**: per-agent `payload` (registry) > project-level `payload_mode` (`.atcr/config.yaml`) > built-in default (`blocks`). This is the same precedence pattern as the rest of the config system.
- **`PayloadMode` is a typed enum** in Go (e.g., `type PayloadMode string` with constants `PayloadDiff`, `PayloadBlocks`, `PayloadFiles`); YAML decoder uses `KnownFields(true)` to reject unknown KEYS; payload-mode VALUES are validated against the enum {`diff`, `blocks`, `files`} by a separate load-time check.
- **Default is `blocks`** per `original-requirements.md` clarification (2026-06-10). Small MoE models produce better findings from real code than from unified diffs; `diff` is the more compact choice for frontier models on large ranges.
- **Per-agent override allows mixing modes in one run**: a single `atcr review` can have one agent on `diff` and another on `blocks`. The fan-out engine uses the effective mode for each agent; `manifest.json` records `payload_mode` (default) and `per_agent_payload` (map).
- **Empty string handling**: an empty `payload_mode` (project) or `payload` (agent) value falls back to the next-priority level (default `blocks`). This matches the rest of the config system (empty is treated as unset).

## Happy Path Scenarios

**Scenario 1: Default payload mode is blocks when not configured**
- **Given** `.atcr/config.yaml` does not specify `payload_mode`
- **When** atcr resolves the default payload mode
- **Then** the effective default is `"blocks"`

**Scenario 2: Developer sets default payload mode in project config**
- **Given** `.atcr/config.yaml` contains `payload_mode: "diff"`
- **When** atcr resolves the default payload mode
- **Then** the effective default is `"diff"`
- **And** all agents without a per-agent override use `"diff"`

**Scenario 3: Developer overrides payload mode for a specific agent in registry**
- **Given** `.atcr/config.yaml` has `payload_mode: "blocks"`
- **And** `registry.yaml` has agent "bruce" with `payload: "diff"`
- **When** atcr resolves the payload mode for agent "bruce"
- **Then** the effective mode for "bruce" is `"diff"` (per-agent override)
- **And** other agents without override still use `"blocks"`

**Scenario 4: Per-agent override works for each valid mode**
- **Given** registry.yaml has three agents with `payload: "diff"`, `payload: "blocks"`, and `payload: "files"`
- **When** atcr resolves payload modes
- **Then** each agent gets its configured mode
- **And** the mode is used by the fan-out engine when building payloads

**Scenario 5: Agent without payload field inherits default**
- **Given** `.atcr/config.yaml` has `payload_mode: "files"`
- **And** registry.yaml has agent "greta" with no `payload` field
- **When** atcr resolves the payload mode for "greta"
- **Then** the effective mode is `"files"` (inherited from default)

## Edge Cases

**Edge Case 1: Invalid payload_mode in project config**
- **Given** `.atcr/config.yaml` contains `payload_mode: "invalid"`
- **When** atcr loads the project config
- **Then** the tool returns an error: "invalid payload_mode 'invalid': must be one of diff, blocks, files"

**Edge Case 2: Invalid payload in registry agent config**
- **Given** registry.yaml has agent "bruce" with `payload: "wrong"`
- **When** atcr loads the registry
- **Then** the tool returns an error: "agent 'bruce': invalid payload 'wrong': must be one of diff, blocks, files"

**Edge Case 3: Empty string payload_mode falls back to default**
- **Given** `.atcr/config.yaml` has `payload_mode: ""` (empty string)
- **When** atcr loads the config
- **Then** the effective default is `"blocks"` (empty treated as unset)

**Edge Case 4: Empty string payload in agent config falls back to default**
- **Given** registry.yaml has agent "bruce" with `payload: ""`
- **When** atcr resolves the payload mode for "bruce"
- **Then** the effective mode is the default from project config

**Edge Case 5: Uppercase or mixed-case payload mode values are rejected**
- **Given** `.atcr/config.yaml` contains `payload_mode: "DIFF"` or registry.yaml has an agent with `payload: "Blocks"`
- **When** atcr loads the config or registry
- **Then** the enum check rejects the value with the same invalid-mode error (payload mode values are lowercase; no case normalization is performed)

## Error Conditions

**Error Scenario 1: Unknown payload mode in project config**
- Error message: "invalid payload_mode '<mode>': must be one of diff, blocks, files"
- Exit code: 1

**Error Scenario 2: Unknown payload in agent registry**
- Error message: "agent '<name>': invalid payload '<mode>': must be one of diff, blocks, files"
- Exit code: 1

## Performance Requirements
- **Response Time:** Config resolution < 1ms (in-memory struct field access)
- **Throughput:** N/A

## Security Considerations
- **Input Validation:** Payload mode validated against allowed enum at config load time
- **Fail-Safe:** Invalid values rejected early with clear error messages

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- YAML fixtures: config with payload_mode set, unset, empty, invalid
- YAML fixtures: registry with agent payload set, unset, empty, invalid
- Combination fixtures: default + override scenarios

**Mock/Stub Requirements:**
- Filesystem: use `t.TempDir()` with fixture files
- No external dependencies needed

**Test Cases:**
1. `TestDefaultPayloadMode_Unset` — verify default is "blocks"
2. `TestDefaultPayloadMode_Set` — verify configured value used
3. `TestDefaultPayloadMode_Invalid` — verify error on invalid value
4. `TestDefaultPayloadMode_Empty` — verify empty falls back to "blocks"
5. `TestPerAgentPayload_Override` — verify agent override takes precedence
6. `TestPerAgentPayload_InheritDefault` — verify agent inherits default when unset
7. `TestPerAgentPayload_Invalid` — verify error on invalid agent payload
8. `TestPerAgentPayload_Empty` — verify empty agent payload inherits default
9. `TestResolvePayload_MultipleAgents` — verify each agent gets correct mode

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] Config validation rejects invalid payload modes

**Story-Specific:**
- [ ] `PayloadMode` type defined with allowed values: `"diff"`, `"blocks"`, `"files"`
- [ ] Default payload mode is `"blocks"` when not configured
- [ ] Project config `.atcr/config.yaml` supports `payload_mode` field
- [ ] Registry agent config supports `payload` field as per-agent override
- [ ] Resolution logic: per-agent override > project config default > built-in default (`"blocks"`)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Resolution precedence documented in code comments
