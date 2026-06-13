# Acceptance Criteria: Tool-Enabled Persona Guidance Sections

**Related User Story:** [06: Persona Guidance & Documentation](../user-stories/06-persona-guidance-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Persona Templates | Go `text/template` | Conditional sections on `PayloadContext.ToolsEnabled` |
| Render Tests | `go test` | Existing `personas_render_test.go` pattern |
| Persona Files | Markdown | Embedded or default persona assets |

### Related Files (from codebase-discovery.json)
- `internal/payload/template.go:15` - read: `PayloadContext.ToolsEnabled` field
- `internal/payload/personas_render_test.go:11` - modify: add `ToolsEnabled: true`/`false` render cases
- `personas/_base.md` - modify: add shared `{{if .ToolsEnabled}}` guidance section
- `personas/bruce.md`, `personas/greta.md`, `personas/kai.md`, `personas/mira.md`, `personas/dax.md`, `personas/otto.md` - modify: add persona-specific `{{if .ToolsEnabled}}` guidance sections

## Happy Path Scenarios

**Scenario 1: Tool guidance renders when ToolsEnabled is true**
- **Given** a persona template containing `{{if .ToolsEnabled}}You may use read_file, grep, and list_files.{{end}}`
- **And** a `PayloadContext` with `ToolsEnabled: true`
- **When** the template is rendered
- **Then** the output contains "You may use read_file, grep, and list_files."

**Scenario 2: Tool guidance is omitted when ToolsEnabled is false**
- **Given** the same persona template
- **And** a `PayloadContext` with `ToolsEnabled: false`
- **When** the template is rendered
- **Then** the output does not contain "You may use read_file, grep, and list_files."

**Scenario 3: Existing non-tool persona content is unchanged**
- **Given** a persona rendered with `ToolsEnabled: false`
- **When** compared to the pre-Epic-2.0 rendered output
- **Then** the text is identical (no accidental scope widening or new instructions for single-shot agents)

## Edge Cases

**Edge Case 1: Missing `ToolsEnabled` in context defaults to false**
- **Given** a `PayloadContext` created without setting `ToolsEnabled`
- **When** the persona is rendered
- **Then** tool-aware guidance is omitted (Go zero value `false`)

**Edge Case 2: Tool guidance in `_base.md` applies to all personas**
- **Given** `_base.md` contains shared `{{if .ToolsEnabled}}` guidance
- **When** any persona is rendered with `ToolsEnabled: true`
- **Then** the shared guidance appears in the final prompt

## Error Conditions

**Error Scenario 1: Template syntax error in new conditional**
- **Error detection:** `personas_render_test.go` fails with template parse error
- **Behavior:** Fix before merge; all persona templates must parse successfully

## Performance Requirements
- Persona render latency is unchanged; `{{if .ToolsEnabled}}` adds O(1) evaluation overhead.

## Security Considerations
- No user input flows into `ToolsEnabled`; it is set from `AgentConfig.Tools` by the engine.

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** Sample `PayloadContext` values with `ToolsEnabled: true` and `ToolsEnabled: false`; fixture persona templates.
**Mock/Stub Requirements:** None — render tests use real template files.

## Definition of Done
**Auto-Verified:**
- [ ] All persona render tests pass (`go test ./internal/payload/...`)
- [ ] No linting errors

**Story-Specific:**
- [ ] At least one shipped persona contains a tool-aware `{{if .ToolsEnabled}}` section
- [ ] Tool guidance is absent when `ToolsEnabled` is false
- [ ] A render test asserts both states

**Manual Review:**
- [ ] Tool guidance reviewed for clarity and conciseness
