# Acceptance Criteria: Registry Documentation Activation

**Related User Story:** [06: Persona Guidance & Documentation](../user-stories/06-persona-guidance-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown | `docs/registry.md` |
| Validation | Go struct tags / manual loader | Field behavior implemented in Stories 2 and 4 |

### Related Files (from codebase-discovery.json)
- `docs/registry.md:54` - modify: update reserved-fields table to active status; add `supports_function_calling` documentation
- `internal/registry/config.go:54` - read: confirms field names and validation (Stories 2 and 4)
- `internal/registry/config.go:209` - read: confirms `max_turns=10` default application when `tools: true`

## Happy Path Scenarios

**Scenario 1: `tools` field documented as active**
- **Given** `docs/registry.md` is rendered
- **Then** it explains that `tools: true` enables the multi-turn tool loop for that agent
- **And** it notes the default is `false`

**Scenario 2: `max_turns` field documented as active**
- **Given** `docs/registry.md` is rendered
- **Then** it explains that `max_turns` caps the number of loop turns per agent
- **And** it documents the default `10` when `tools: true` and the validation rule `> 0`

**Scenario 3: `tool_budget_bytes` field documented as active**
- **Given** `docs/registry.md` is rendered
- **Then** it explains that `tool_budget_bytes` caps cumulative tool-result bytes per agent
- **And** it documents that `0` means unlimited and negative values are rejected

**Scenario 4: `supports_function_calling` documented**
- **Given** `docs/registry.md` is rendered
- **Then** it documents a per-model `supports_function_calling: bool` field
- **And** it states the default is `false` and that it is required for an agent with `tools: true` to run the loop

## Edge Cases

**Edge Case 1: Backward compatibility note**
- **Given** a reader with a 1.x registry using the reserved fields
- **When** they read the updated docs
- **Then** they see a note that these fields were reserved in 1.1/1.x and are now active in 2.0

## Error Conditions

**Error Scenario 1: Documentation contradicts validation**
- **Error detection:** Manual review or automated doc test
- **Behavior:** Align docs with `internal/registry/config.go` validation before acceptance

## Performance Requirements
- Documentation updates have no runtime impact.

## Security Considerations
- Document that `tools: true` should only be set for models with `supports_function_calling: true` to avoid silent degrade loops.

## Test Implementation Guidance
**Test Type:** Documentation
**Test Data Requirements:** N/A
**Mock/Stub Requirements:** N/A

## Definition of Done
**Auto-Verified:**
- [ ] `docs/registry.md` renders without broken internal links

**Story-Specific:**
- [ ] Reserved-fields table updated to active status
- [ ] `tools`, `max_turns`, `tool_budget_bytes` documented with defaults and validation
- [ ] `supports_function_calling` documented as per-model opt-in, default `false`
- [ ] Backward-compatibility note included

**Manual Review:**
- [ ] Docs reviewed for accuracy against implementation
