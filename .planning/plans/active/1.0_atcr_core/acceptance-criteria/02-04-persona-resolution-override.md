# Acceptance Criteria: Persona Resolution and Override

**Related User Story:** [02: Agent Configuration](../user-stories/02-agent-configuration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Template Engine | `text/template` | Go stdlib, payload variable substitution |
| File Loader | `os`, `path/filepath` | Persona file discovery and loading |
| Embedded FS | `embed.FS` | Fallback to embedded defaults |
| CLI Flag | `cobra` flag | `--task-message` override |
| Test Framework | `testify` (assert, require) | Table-driven template rendering tests |

## Related Files
- `internal/registry/persona.go` - create: Persona resolution chain logic (file lookup, fallback, template render)
- `internal/registry/persona_test.go` - create: Tests for resolution chain and template rendering
- `internal/registry/config.go` - modify: Add persona field to AgentConfig, link to resolver
- `cmd/atcr/review.go` - modify: Apply `--task-message` flag to override persona
- `personas/_base.md` - create: Base persona template with shared instructions
- `personas/bruce.md` - create: Bruce-specific persona (and 5 others)

## Happy Path Scenarios

**Scenario 1: Agent uses explicit persona reference**
- **Given** registry.yaml has agent "bruce" with `persona: bruce-security`
- **And** `.atcr/personas/bruce-security.md` exists with a custom prompt
- **When** atcr resolves bruce's persona
- **Then** the content of `bruce-security.md` is used as the system prompt

**Scenario 2: Agent persona defaults to agent name**
- **Given** registry.yaml has agent "bruce" with no `persona` field
- **And** `.atcr/personas/bruce.md` exists
- **When** atcr resolves bruce's persona
- **Then** the content of `bruce.md` is used (agent name as persona name)

**Scenario 3: Persona file missing, falls back to _base.md**
- **Given** agent "bruce" has no persona file at `.atcr/personas/bruce.md`
- **And** `.atcr/personas/_base.md` exists
- **When** atcr resolves bruce's persona
- **Then** the content of `_base.md` is used as the fallback prompt

**Scenario 4: Both agent file and _base.md missing, falls back to embedded default**
- **Given** no `.atcr/personas/` directory exists (or is empty)
- **When** atcr resolves bruce's persona
- **Then** the embedded default persona from the Go binary is used

**Scenario 5: Persona template renders payload variables**
- **Given** a persona file contains: `Review this {{.PayloadMode}} with {{.FileCount}} files from {{.BaseRef}} to {{.HeadRef}}. Agent: {{.AgentName}}`
- **And** the review context has `PayloadMode: "blocks"`, `FileCount: 5`, `BaseRef: "main"`, `HeadRef: "feature"`, `AgentName: "bruce"`
- **When** atcr renders the persona template
- **Then** the output is: `Review this blocks with 5 files from main to feature. Agent: bruce`

**Scenario 6: `--task-message` overrides all persona resolution**
- **Given** a persona file exists for bruce
- **And** `_base.md` exists
- **And** the developer runs `atcr review --task-message "Focus on security vulnerabilities only"`
- **When** atcr resolves bruce's persona
- **Then** the system prompt is: "Focus on security vulnerabilities only"
- **And** all persona files are ignored for this invocation

**Scenario 7: Multiple agents each resolve their own persona**
- **Given** agents bruce, greta, kai are in the roster
- **And** each has a distinct persona file in `.atcr/personas/`
- **When** atcr resolves personas for all three agents
- **Then** each agent receives its own persona prompt
- **And** prompts are independent (no cross-contamination)

## Edge Cases

**Edge Case 1: Persona file exists but is empty**
- **Given** `.atcr/personas/bruce.md` exists but contains zero bytes
- **When** atcr resolves bruce's persona
- **Then** the tool falls through to `_base.md` (empty file treated as missing)
- **And** a warning is printed: "persona file .atcr/personas/bruce.md is empty, using fallback"

**Edge Case 2: Persona file references undefined template variable**
- **Given** a persona file contains `{{.UndefinedField}}`
- **When** atcr renders the template
- **Then** the tool returns an error: "persona template: undefined field 'UndefinedField'"
- **And** exits with non-zero exit code

**Edge Case 3: Persona file has valid Go template syntax error**
- **Given** a persona file contains `{{.Payload` (unclosed braces)
- **When** atcr parses the template
- **Then** the tool returns an error: "persona template parse error: <detail>"

**Edge Case 4: `--task-message` is empty string**
- **Given** the developer runs `atcr review --task-message ""`
- **When** atcr resolves the persona
- **Then** the empty string is used as the system prompt (explicit override to no instructions)
- **Note:** This is valid — developer may want bare API call with no system prompt

**Edge Case 5: Persona file with mixed content and template variables**
- **Given** a persona file with prose and multiple `{{.Payload}}`, `{{.AgentName}}` references
- **When** atcr renders the template
- **Then** all variables are substituted correctly
- **And** non-template content is preserved verbatim

**Edge Case 6: Agent references persona in registry dir, not project dir**
- **Given** agent "bruce" has `persona: custom`
- **And** `~/.config/atcr/personas/custom.md` exists (registry dir)
- **And** `.atcr/personas/custom.md` also exists (project dir)
- **When** atcr resolves the persona
- **Then** the project-level file `.atcr/personas/custom.md` takes precedence over registry dir

## Error Conditions

**Error Scenario 1: Template parse error in persona file**
- Error message: "persona template parse error in <file>: <detail>"
- Exit code: 1

**Error Scenario 2: Template execution error (undefined variable)**
- Error message: "persona template render error: undefined field '<name>' in <file>"
- Exit code: 1

**Error Scenario 3: All persona sources missing (should never happen)**
- Error message: "internal error: no persona found for agent '<name>' — embedded default missing"
- Exit code: 1
- Note: This indicates a build/deployment error

## Performance Requirements
- **Response Time:** Persona resolution and template rendering: < 5ms per agent
- **Throughput:** All six personas resolved in < 30ms total
- **Template Rendering:** No external dependencies — Go stdlib `text/template` only

## Security Considerations
- **Input Validation:** Template variables are restricted to a known allowlist (Payload, PayloadMode, FileCount, BaseRef, HeadRef, AgentName)
- **Template Injection:** Persona files are trusted (developer-controlled); no untrusted input reaches template context
- **File Path Validation:** Persona names are sanitized to prevent path traversal (no `../` in persona field)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Persona files at each resolution level (project, registry dir, embedded)
- Template fixtures with all supported variables
- Edge case fixtures (empty file, bad template syntax, undefined vars)

**Mock/Stub Requirements:**
- Filesystem: use `t.TempDir()` to create persona file hierarchies
- Embedded FS: override for testing with `testing/fstest.MapFS`
- Template context: construct test structs with known values

**Test Cases:**
1. `TestPersonaResolution_ExplicitRef` — agent with `persona: name`, file exists
2. `TestPersonaResolution_DefaultToAgentName` — no persona field, uses agent name
3. `TestPersonaResolution_FallbackToBase` — agent file missing, uses _base.md
4. `TestPersonaResolution_FallbackToEmbedded` — all files missing, uses embedded
5. `TestPersonaResolution_TemplateRendering` — all variables render correctly
6. `TestPersonaResolution_TaskMessageOverride` — `--task-message` bypasses all resolution
7. `TestPersonaResolution_TaskMessageEmpty` — empty string is valid override
8. `TestPersonaResolution_MultipleAgents` — each agent gets own persona
9. `TestPersonaResolution_EmptyFile` — empty file treated as missing, falls through
10. `TestPersonaResolution_BadTemplateSyntax` — parse error with clear message
11. `TestPersonaResolution_UndefinedVariable` — execution error with field name
12. `TestPersonaResolution_PathTraversal` — `persona: ../../../etc/passwd` rejected
13. `TestPersonaResolution_ProjectOverridesRegistry` — project file beats registry dir file

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] Persona resolution chain works through all four levels
- [ ] Template rendering substitutes all supported variables
- [ ] `--task-message` completely bypasses persona file resolution
- [ ] Empty persona files fall through to next level with warning

**Story-Specific:**
- [ ] Resolution order: agent's persona ref > `<agent>.md` in project > `_base.md` in project > registry dir > embedded default
- [ ] `--task-message` CLI flag overrides ALL persona resolution
- [ ] Supported template variables: `{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.AgentName}}`
- [ ] Persona names are sanitized (no path traversal)
- [ ] Template parse errors include file path and line number
- [ ] Template execution errors name the undefined field
- [ ] Multiple agents resolve personas independently (no shared state)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Persona templates in `personas/` are well-written and useful for code review
- [ ] Error messages are clear and guide developer to fix the persona file
