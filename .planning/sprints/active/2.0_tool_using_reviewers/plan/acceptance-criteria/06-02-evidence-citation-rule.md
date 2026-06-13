# Acceptance Criteria: Evidence-Citation Rule & Scope Guard

**Related User Story:** [06: Persona Guidance & Documentation](../user-stories/06-persona-guidance-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Persona Templates | Go `text/template` | Prose rules in tool-aware conditional section |
| Findings Parser | Existing reconciler | Validates or annotates citation presence |
| Tests | `go test` | Persona render + finding fixture tests |

### Related Files (from codebase-discovery.json)
- `personas/_base.md` - modify: add evidence-citation and scope-guard prose inside shared `{{if .ToolsEnabled}}`
- `personas/bruce.md`, `personas/greta.md`, `personas/kai.md`, `personas/mira.md`, `personas/dax.md`, `personas/otto.md` - modify: add persona-specific evidence-citation prose inside `{{if .ToolsEnabled}}`
- `internal/reconcile/reconcile.go` - read: findings format; no code change required unless adding a citation linter
- `internal/payload/personas_render_test.go:11` - modify: assert citation rule text is present

## Happy Path Scenarios

**Scenario 1: Persona states the evidence-citation rule**
- **Given** a tool-enabled persona rendered with `ToolsEnabled: true`
- **When** the rendered text is inspected
- **Then** it contains an explicit instruction that every finding citing tool-gathered evidence must include the file path and line numbers the agent actually read

**Scenario 2: Persona restates the scope rule**
- **Given** a tool-enabled persona rendered with `ToolsEnabled: true`
- **When** the rendered text is inspected
- **Then** it contains an instruction that tools widen evidence gathering, not review scope, and that pre-existing issues must be tagged `out-of-scope`

**Scenario 3: Single-shot persona is unchanged**
- **Given** a persona rendered with `ToolsEnabled: false`
- **When** the rendered text is inspected
- **Then** it does not mention tool evidence citation or out-of-scope tags beyond what 1.0 already required

## Edge Cases

**Edge Case 1: Finding cites evidence not actually read**
- **Given** an agent produces a finding that references a file/line the transcript shows was never read
- **Then** the persona instruction makes this a quality violation; the reconciler may annotate it low-confidence (implementation optional in v1; at minimum the rule is stated in the prompt)

**Edge Case 2: Out-of-scope tag included in finding category**
- **Given** a finding about an unchanged region
- **When** the finding includes `out-of-scope` in its category
- **Then** the reconciler annotates rather than promotes it (existing 1.0 behavior; persona rule aligns with it)

## Error Conditions

**Error Scenario 1: Scope rule is missing from tool guidance**
- **Error detection:** AC review or render test assertion fails
- **Behavior:** Add the scope-guard sentence before acceptance

## Performance Requirements
- No runtime performance impact; these are prompt-text requirements.

## Security Considerations
- The citation rule reduces hallucination risk by binding findings to actual read evidence.

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** Rendered persona text; sample findings with and without citations.
**Mock/Stub Requirements:** None.

## Definition of Done
**Auto-Verified:**
- [ ] Persona render tests pass
- [ ] String assertions for citation rule and scope guard pass

**Story-Specific:**
- [ ] Tool-enabled persona contains the evidence-citation rule
- [ ] Tool-enabled persona contains the scope rule (`out-of-scope` tag for pre-existing issues)
- [ ] Single-shot persona content is unchanged

**Manual Review:**
- [ ] Persona text reviewed for clarity and absence of scope widening
