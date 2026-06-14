# Acceptance Criteria: Payload-Modes Semantics & README Cost Guidance

**Related User Story:** [06: Persona Guidance & Documentation](../user-stories/06-persona-guidance-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown | `docs/payload-modes.md` and `README.md` |

### Related Files (from codebase-discovery.json)
- `docs/payload-modes.md` - modify: add "Tool agents" / "Payload as starting point" subsection
- `README.md` - modify: add cost guidance paragraph and link to `docs/registry.md`
- `docs/registry.md:54` - reference: budget fields (`max_turns`, `tool_budget_bytes`, `timeout_secs`) documented as active

## Happy Path Scenarios

**Scenario 1: Payload-as-starting-point semantics documented**
- **Given** `docs/payload-modes.md` is rendered
- **When** the tool-agents subsection is read
- **Then** it explains that the payload is the starting point of the review, not the universe
- **And** it states that a tool agent may read additional files through `read_file`, `grep`, and `list_files` within the path-jailed snapshot

**Scenario 2: Scope rule documented for tool agents**
- **Given** `docs/payload-modes.md` is rendered
- **Then** the tool-agents subsection restates that findings still target the changed range unless tagged `out-of-scope`

**Scenario 3: README cost guidance present**
- **Given** `README.md` is rendered
- **Then** it contains a statement that tool agents typically consume 3-10× the provider calls of a single-shot reviewer
- **And** it links to `docs/registry.md` for the budget fields (`max_turns`, `tool_budget_bytes`, `timeout_secs`)

## Edge Cases

**Edge Case 1: Cost guidance is a range, not a guarantee**
- **Given** the README cost paragraph
- **Then** it uses wording such as "typically 3-10×" rather than an exact multiplier

## Error Conditions

**Error Scenario 1: Broken links**
- **Error detection:** Markdown link check or manual review
- **Behavior:** Fix links to `docs/registry.md` and `docs/payload-modes.md` before acceptance

## Performance Requirements
- Documentation updates have no runtime impact.

## Security Considerations
- Document that tool agents are path-jailed and read-only; no write tools or network access.

## Test Implementation Guidance
**Test Type:** Documentation
**Test Data Requirements:** N/A
**Mock/Stub Requirements:** N/A

## Definition of Done
**Auto-Verified:**
- [ ] `docs/payload-modes.md` and `README.md` render without broken internal links

**Story-Specific:**
- [ ] `docs/payload-modes.md` contains tool-agent payload-as-starting-point semantics
- [ ] `docs/payload-modes.md` restates the scope rule for tool agents
- [ ] `README.md` contains the 3-10× cost guidance
- [ ] `README.md` links to `docs/registry.md` for budget configuration

**Manual Review:**
- [ ] Docs reviewed for clarity and alignment with Epic 2.0 behavior
