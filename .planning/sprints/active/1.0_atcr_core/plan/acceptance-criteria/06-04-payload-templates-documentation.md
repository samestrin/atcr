# Acceptance Criteria: Payload Template Variables, Scope Rules, and Documentation

**Related User Story:** [06: Payload Mode Selection](../user-stories/06-payload-mode-selection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Template Engine | Go `text/template` | Renders persona prompts with payload vars |
| Template Vars | Go struct | `Payload`, `PayloadMode`, `FileCount`, `BaseRef`, `HeadRef`, `AgentName` |
| Scope Rules | Go string constants or embedded files | Per-mode scope instructions injected into persona prompts |
| Documentation | Markdown | `docs/payload-modes.md` — when to use each mode |
| Test Framework | `testify` (assert, require) | Template rendering tests with fixture prompts |

## Related Files
- `internal/payload/template.go` - create: `PayloadContext` struct, `RenderPrompt()` function
- `internal/payload/template_test.go` - create: Tests for template variable substitution and scope rules
- `internal/prompt/scope.go` - create: Per-payload scope rule constants/functions
- `docs/payload-modes.md` - create: User-facing documentation for diff, blocks, files modes

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Payload Engine](../documentation/payload-engine.md) — Authoritative spec for the `PayloadContext` struct, template variables, and per-payload-mode scope rules.
- [Configuration & Registry](../documentation/configuration-management.md) — How persona prompt files are resolved (registry dir > project dir > embedded); `text/template` rendering with `Option("missingkey=error")`.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — How the reconciler annotates out-of-scope findings; the `out-of-scope` category convention from `plan.md` clarifications (2026-06-10).

### Spec alignment notes

- **Template variables are exactly**: `{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.AgentName}}`. Per `original-requirements.md`. No additional variables are injected; persona authors must use only these.
- **Per-payload-mode scope rules** (per `plan.md` clarifications 2026-06-10):
  - `diff` mode: focus only on changed regions; findings outside the diff lines are out of scope.
  - `blocks` mode: same as diff — changed regions only; function-context expansion does not change the scope rule.
  - `files` mode: full file content is provided, pre-existing issues may be visible. Findings on unchanged regions use the `out-of-scope` category so the reconciler can annotate rather than promote.
- **Adversarial personality clause** must be injected into every persona prompt: "find problems the author would prefer you didn't", no-flattery rules, priority-ordered focus areas. Per `plan.md` clarification (2026-06-10) and AC 02-04.
- **Severity rubric in persona prompt**: `CRITICAL|HIGH|MEDIUM|LOW` directly, not blocking/significant/minor with implicit translation. The reconciler matches on the canonical values.
- **`docs/payload-modes.md` decision table** must include: (1) when to use diff (frontier models, large ranges, token-constrained environments), (2) when to use blocks (small MoE models, default for v1), (3) when to use files (audit-style review of small ranges, when you want out-of-scope findings to be reported but flagged). Per `plan.md` task 13.
- **`text/template` security**: persona files are developer-controlled and trusted; no untrusted input reaches the template context. Render with `Option("missingkey=error")` so unknown variables fail loudly rather than silently rendering as empty.

## Happy Path Scenarios

**Scenario 1: Template renders all payload variables**
- **Given** a persona prompt template containing `{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.AgentName}}`
- **And** a `PayloadContext` with: Payload="<diff content>", PayloadMode="diff", FileCount=5, BaseRef="main", HeadRef="feature", AgentName="bruce"
- **When** `RenderPrompt(template, ctx)` is called
- **Then** the rendered prompt contains the diff content, "diff", "5", "main", "feature", and "bruce" in the correct positions

**Scenario 2: files mode scope rule instructs agent about pre-existing issues**
- **Given** payload mode is "files"
- **When** the scope rule for files mode is injected into the persona prompt
- **Then** the prompt includes instruction that full file content is provided and pre-existing issues may be visible
- **And** the prompt instructs the agent to focus on changed regions but may note pre-existing issues separately

**Scenario 3: diff and blocks mode scope rules constrain to changed regions**
- **Given** payload mode is "diff" or "blocks"
- **When** the scope rule is injected into the persona prompt
- **Then** the prompt instructs the agent to focus only on changed regions
- **And** findings outside changed ranges are flagged during reconciliation

**Scenario 4: Reconcile flags findings outside changed ranges**
- **Given** a finding references line numbers outside the changed ranges
- **When** the reconciler processes the finding against the payload scope
- **Then** the finding is annotated as "outside changed range"
- **And** the reconciler annotates the finding with category `out-of-scope` and lists it in a separate report section; severity and confidence are computed by the standard rules (no adjustment)
- **Note:** out-of-scope annotation is reconciler behavior implemented in `internal/reconcile` (see AC 01-05); this scenario validates the cross-package integration

**Scenario 5: Documentation explains when to use each mode**
- **Given** `docs/payload-modes.md` exists
- **When** a developer reads the documentation
- **Then** it explains diff mode (most compact, good for frontier models)
- **And** it explains blocks mode (function-context, good for small MoE models, default)
- **And** it explains files mode (full content, thorough but high token cost)
- **And** it includes a decision table or guide for choosing a mode

## Edge Cases

**Edge Case 1: Template with missing variable**
- **Given** a persona prompt template containing `{{.UnknownVar}}`
- **When** `RenderPrompt` is called
- **Then** the tool returns a hard error via `Option("missingkey=error")`: "template references unknown variable 'UnknownVar'" (matches the spec note and `TestRenderPrompt_UnknownVar`)

**Edge Case 2: Template with no payload variables**
- **Given** a persona prompt template with no `{{.Payload*}}` variables
- **When** `RenderPrompt` is called
- **Then** the prompt renders without error (payload simply not included)

**Edge Case 3: Very large payload in template**
- **Given** a payload of 500KB
- **When** the template renders with `{{.Payload}}`
- **Then** the full payload is included in the rendered prompt
- **And** no truncation occurs at the template level (byte budget handles truncation earlier)

**Edge Case 4: Empty payload (no changes)**
- **Given** an empty payload (no files changed)
- **When** the template renders
- **Then** `{{.Payload}}` renders as empty string
- **And** `{{.FileCount}}` renders as "0"

**Edge Case 5: Payload content contains template syntax**
- **Given** the payload content itself contains `{{` template syntax
- **When** the persona prompt renders
- **Then** the payload is injected as data (never parsed as a template)
- **And** the payload appears verbatim in the rendered prompt

## Error Conditions

**Error Scenario 1: Template syntax error**
- Error message: "failed to parse persona prompt template: <detail>"
- Exit code: 1

**Error Scenario 2: Missing required template variable in context**
- Error message: "payload context missing required field '<field>'"
- Exit code: 1

## Performance Requirements
- **Response Time:** Template rendering < 5ms for payloads up to 1MB
- **Throughput:** N/A (one render per agent per invocation)

## Security Considerations
- **Input Validation:** Template parsed with `text/template` — no arbitrary code execution
- **Payload Injection:** Payload content is data, not template code (use `{{.Payload}}` not raw concatenation)
- **Scope Enforcement:** Scope rules are static strings, not user-controllable at runtime

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Persona prompt templates with all variable combinations
- PayloadContext fixtures with various modes and file counts
- Expected rendered output for comparison
- Scope rule text fixtures for each mode

**Mock/Stub Requirements:**
- No external dependencies
- Pure string processing tests

**Test Cases:**
1. `TestRenderPrompt_AllVars` — verify all template variables render correctly
2. `TestRenderPrompt_DiffMode` — verify diff mode scope rule injection
3. `TestRenderPrompt_BlocksMode` — verify blocks mode scope rule injection
4. `TestRenderPrompt_FilesMode` — verify files mode scope rule (pre-existing issues noted)
5. `TestRenderPrompt_EmptyPayload` — verify empty payload renders cleanly
6. `TestRenderPrompt_UnknownVar` — verify error on unknown template variable
7. `TestRenderPrompt_NoPayloadVars` — verify template without payload vars works
8. `TestScopeRule_FilesModeMentionsPreExisting` — verify files scope text content
9. `TestScopeRule_DiffBlocksConstrainToChanges` — verify diff/blocks scope text content
10. `TestDocs_PayloadModesExists` — verify docs file exists and contains key sections

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] Template rendering produces expected output for all modes
- [ ] docs/payload-modes.md exists and contains mode descriptions

**Story-Specific:**
- [ ] `PayloadContext` struct has fields: `Payload`, `PayloadMode`, `FileCount`, `BaseRef`, `HeadRef`, `AgentName`
- [ ] All template variables renderable in persona prompts
- [ ] files mode scope rule mentions pre-existing issues visibility
- [ ] diff/blocks scope rules constrain findings to changed ranges
- [ ] Reconciler annotates findings outside changed ranges
- [ ] `docs/payload-modes.md` documents all three modes with usage guidance

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Documentation reviewed for accuracy and clarity
- [ ] Scope rules align with reconciliation logic
