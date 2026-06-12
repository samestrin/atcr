# Acceptance Criteria: Configuration Precedence and Validation

**Related User Story:** [02: Agent Configuration](../user-stories/02-agent-configuration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Precedence Resolver | Go function chain | CLI > project > registry > embedded |
| Graph Validation | Custom Go (DFS with three-color marking) | Cycle and dangling ref detection |
| YAML Parsing | `yaml.v3` | `KnownFields(true)` strict mode |
| Test Framework | `testify` (assert, require) | Table-driven precedence and graph tests |

## Related Files
- `internal/registry/config.go` - modify: Add precedence resolver and fallback validation logic
- `internal/registry/config_test.go` - modify: Add tests for precedence chain and fallback validation
- `internal/registry/graph.go` - create: Fallback chain graph construction and cycle detection
- `internal/registry/graph_test.go` - create: Unit tests for graph validation (cycles, dangling refs)
- `cmd/atcr/review.go` - modify: Apply CLI flags to override config at runtime

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Configuration & Registry](../documentation/configuration-management.md) — Authoritative spec for the precedence chain, `KnownFields(true)` strict mode (`NewDecoder().KnownFields(true)`), and the fallback-chain validation invariants.
- [CLI Architecture](../documentation/cli-architecture.md) — `MarkFlagsRequiredTogether` / `MarkFlagsOneRequired` for flag-relationship declarations on cobra commands.

### Spec alignment notes

- **Precedence order is exact and unidirectional**: CLI flag > project config (`.atcr/config.yaml` only; `~/.config/atcr/` is the registry tier — registry.yaml plus user persona files) > registry > embedded default. No merge semantics; each level is a full override of the value defined at the next-priority level.
- **Fallback validation runs at config load time** (before any review or provider call). Two failure modes both produce hard errors: dangling reference (e.g., `bruce` falls back to `unknown-agent`) and cycle (e.g., `A → B → A`, including self-references `A → A`).
- **Diamond shapes are not cycles** (e.g., `A → C` and `B → C` is fine); the validator must distinguish shared-fallback-targets from cycles. The classic DFS-with-coloring algorithm handles this naturally.
- **Diamond break**: when a `fallback` chain has multiple roots sharing a single fallback, the cycle detector must not flag the shared target as a cycle. Use DFS with three colors (`white`/`gray`/`black`) — `gray → gray` edge triggers a cycle; `gray → black` does not.
- **Strict mode is non-negotiable**: a single unknown field in a registry or project config aborts the entire load. No partial loading. Per `configuration-management.md`.
- **Cycle error message includes the full path**: e.g., `fallback cycle detected: bruce -> greta -> kai -> bruce`. The path aids debugging.

## Happy Path Scenarios

**Scenario 1: Precedence — CLI flag overrides project config**
- **Given** `.atcr/config.yaml` has `timeout_secs: 600`
- **And** the developer runs `atcr review --timeout 300`
- **When** atcr resolves the timeout value
- **Then** the effective timeout is 300 (CLI flag wins)

**Scenario 2: Precedence — project config overrides registry**
- **Given** registry.yaml has agent "bruce" with `temperature: 0.5`
- **And** `.atcr/config.yaml` overrides bruce with `temperature: 0.9`
- **When** atcr resolves bruce's temperature
- **Then** the effective temperature is 0.9 (project config wins)

**Scenario 3: Precedence — registry overrides embedded default**
- **Given** the embedded default timeout is 600 seconds
- **And** registry.yaml has `timeout_secs: 1200`
- **When** atcr resolves the timeout (no project config or CLI flag)
- **Then** the effective timeout is 1200 (registry wins)

**Scenario 4: Precedence — full chain from embedded to CLI**
- **Given** embedded default: `payload_mode: blocks`
- **And** registry.yaml: `payload_mode: full`
- **And** `.atcr/config.yaml`: `payload_mode: summary`
- **And** CLI flag: `--payload-mode diff`
- **When** atcr resolves payload_mode
- **Then** the effective payload_mode is "diff" (CLI flag wins)

**Scenario 5: Fallback chain — valid linear chain**
- **Given** registry.yaml defines agents with fallbacks: `bruce -> greta -> kai`
- **When** atcr validates the fallback chain at load time
- **Then** validation passes without error
- **And** the chain is available for runtime failover

**Scenario 6: Fallback chain — agent with no fallback**
- **Given** agent "bruce" has no `fallback` field in registry.yaml
- **When** atcr validates the fallback chain
- **Then** validation passes (no fallback is valid — agent has no failover)

**Scenario 7: Strict parsing — all known fields accepted**
- **Given** a registry.yaml with all documented fields correctly spelled
- **When** atcr parses the file
- **Then** parsing succeeds without error

## Edge Cases

**Edge Case 1: Fallback chain forms a cycle (A -> B -> A)**
- **Given** registry.yaml defines: bruce fallback: greta, greta fallback: bruce
- **When** atcr validates the fallback chain at load time
- **Then** the tool returns an error: "fallback cycle detected: bruce -> greta -> bruce"
- **And** exits with non-zero exit code

**Edge Case 2: Fallback chain forms a longer cycle (A -> B -> C -> A)**
- **Given** registry.yaml defines: bruce fallback: greta, greta fallback: kai, kai fallback: bruce
- **When** atcr validates the fallback chain at load time
- **Then** the tool returns an error: "fallback cycle detected: bruce -> greta -> kai -> bruce"

**Edge Case 3: Fallback chain has dangling reference**
- **Given** registry.yaml defines: bruce fallback: unknown-agent (but unknown-agent is not defined)
- **When** atcr validates the fallback chain at load time
- **Then** the tool returns an error: "agent 'bruce' fallback references unknown agent 'unknown-agent'"

**Edge Case 4: Self-referencing fallback**
- **Given** registry.yaml defines: bruce fallback: bruce
- **When** atcr validates the fallback chain
- **Then** the tool returns an error: "fallback cycle detected: bruce -> bruce"

**Edge Case 5: CLI flag for non-overridable field**
- **Given** a field that has no corresponding CLI flag (e.g., `fail_on`)
- **When** the developer runs atcr
- **Then** the value resolves from project config > registry > embedded (no CLI override available)

**Edge Case 6: Registry YAML with both known and unknown fields**
- **Given** registry.yaml contains `temperature: 0.7` (known) and `temprature: 0.5` (typo)
- **When** atcr parses the file with `KnownFields(true)`
- **Then** parsing fails with error: "registry.yaml agents.<name>: unknown field 'temprature'"
- **And** the known fields are NOT partially loaded

## Error Conditions

**Error Scenario 1: Fallback cycle detected**
- Error message: "fallback cycle detected: <agent1> -> <agent2> -> ... -> <agent1>"
- Exit code: 1
- Timing: At config load time, before any review begins

**Error Scenario 2: Dangling fallback reference**
- Error message: "agent '<name>' fallback references unknown agent '<ref>'"
- Exit code: 1

**Error Scenario 3: Unknown YAML field (strict mode)**
- Error message: `<file> <path>: unknown field "<field>"` — e.g. `registry.yaml agents.bruce: unknown field "typo_field"`
- Exit code: 1

**Error Scenario 4: Invalid value type (e.g., string where int expected)**
- Error message: "registry.yaml agents.<name>.timeout_secs: cannot unmarshal string into int"
- Exit code: 1

## Performance Requirements
- **Response Time:** Precedence resolution and fallback validation complete in < 10ms
- **Throughput:** N/A (single validation per invocation)
- **Complexity:** Graph traversal is O(V + E) where V = agents, E = fallback edges

## Security Considerations
- **Input Validation:** All YAML parsed with `KnownFields(true)` — no silent field ignoring
- **Cycle Detection:** Prevents infinite loops that could hang the process
- **Dangling Ref Detection:** Prevents runtime nil pointer dereferences

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Precedence fixtures: multiple YAML configs at different levels (embedded, registry, project, CLI)
- Graph fixtures: linear chains, cycles (2-node, 3-node), self-refs, dangling refs, diamond shapes

**Mock/Stub Requirements:**
- No mocks needed — pure logic functions operating on in-memory structs
- Use `t.TempDir()` for any filesystem-dependent tests

**Test Cases:**
1. `TestPrecedence_CLIOverridesProject` — CLI flag wins over project config
2. `TestPrecedence_ProjectOverridesRegistry` — project config wins over registry
3. `TestPrecedence_RegistryOverridesEmbedded` — registry wins over embedded
4. `TestPrecedence_FullChain` — all four levels, CLI wins
5. `TestPrecedence_NoOverride` — embedded default used when no override
6. `TestFallbackChain_ValidLinear` — A -> B -> C, no error
7. `TestFallbackChain_NoFallback` — agent with no fallback, no error
8. `TestFallbackChain_TwoNodeCycle` — A -> B -> A, error
9. `TestFallbackChain_ThreeNodeCycle` — A -> B -> C -> A, error
10. `TestFallbackChain_SelfRef` — A -> A, error
11. `TestFallbackChain_DanglingRef` — A -> nonexistent, error
12. `TestStrictMode_UnknownFieldProvider` — typo in provider field
13. `TestStrictMode_UnknownFieldAgent` — typo in agent field
14. `TestStrictMode_AllKnownFields` — valid config passes strict mode
15. `TestFallbackChain_DiamondNoCycle` — A -> B, A -> C (no cycle, just shared target)

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds
- [x] Precedence chain correctly resolves for all four levels
- [x] Cycle detection catches 2-node, 3-node, and self-ref cycles
- [x] Dangling reference detection catches unknown agent refs
- [x] Strict mode rejects unknown fields with descriptive errors

**Story-Specific:**
- [x] Precedence order documented: CLI flag > project config > registry > embedded default
- [x] Fallback validation runs at config load time, before any review
- [x] Cycle error message includes the full cycle path (e.g., "bruce -> greta -> bruce")
- [x] Dangling ref error message names both the agent and the missing reference
- [x] `KnownFields(true)` is set on all YAML decoders
- [x] Diamond-shaped fallback graphs (multiple agents pointing to same fallback) do NOT trigger false cycle detection

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Cycle detection algorithm verified (DFS with three-color marking)
- [ ] Error messages name the file, the offending field or agent, and the expected valid values
