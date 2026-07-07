# Acceptance Criteria: Vendor-Grounded Prompt Phrasing and Canonical Structure Compliance

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go `text/template` prompt files (Markdown) | `personas/community/<slug>.md`, rendered via the existing `payload.PayloadContext` pattern used by `personas/personas.go` |
| Test Framework | Go `testing` package, template-render assertions | Extends the `personas_test.go`/`grounding_test.go` render pattern to the community set |
| Key Dependencies | `text/template` (stdlib); `internal/payload.PayloadContext` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `personas/community/*.md` — create: one prompt template per authored persona (10+ total), mirroring the canonical section structure and required variables.
- `personas/community_test.go` — create: table-driven test rendering every `personas/community/*.md` template against a fixture `payload.PayloadContext` and asserting no leftover `{{ }}` actions and mandatory sections present.
- `docs/personas-authoring.md` — reference: canonical structure and required-variable contract (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`).
- `personas/_base.md` — reference: shared prompt-template scaffold.


## Happy Path Scenarios
**Scenario 1: Every persona template renders with no leftover template actions**
- **Given** a community persona Markdown template and a fully-populated `payload.PayloadContext`
- **When** the template is executed via `text/template`
- **Then** the rendered output contains no unrendered `{{` or `}}` substrings

**Scenario 2: Every persona template contains the mandatory sections**
- **Given** a community persona Markdown template
- **When** its section headings are extracted
- **Then** `## Role` and `## Output Format` are both present, and the `## Output Format` block contains the exact 7-column pipe-delimited contract (`SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`) byte-for-byte

**Scenario 3: Prompt phrasing style differs per vendor, reflecting that vendor's own prompting guidance**
- **Given** the Anthropic, OpenAI, and Google frontier persona prompts
- **When** their instruction style is compared (e.g. explicit step-by-step framing vs. terse directive framing)
- **Then** each prompt's structure/emphasis is authored per that vendor's documented prompting guidance rather than all three sharing identical phrasing patterns

## Edge Cases
**Edge Case 1: Optional `{{if .ToolsEnabled}}…{{end}}` block renders without error and leaves no unrendered template actions in both states**
- **Given** a persona template that includes the optional tool-assisted-review block
- **When** the template is rendered once with `ToolsEnabled: true` and once with `ToolsEnabled: false`
- **Then** both renders complete with no leftover `{{ }}` actions and no template execution error

**Edge Case 2: A persona template references a variable not in the required set**
- **Given** a persona template using an undeclared field on `payload.PayloadContext`
- **When** the template is executed
- **Then** `text/template` returns an execution error, which the community render test surfaces as a test failure rather than a silent empty substitution

## Error Conditions
**Error Scenario 1: Missing required template variable**
- **Given** a persona template missing one of the required variables (e.g. no `{{.Payload}}` reference)
- **When** the story's manual/automated structure check runs
- **Then** the persona fails the Definition of Done's structure checklist and is not considered complete, even if it technically renders

**Error Scenario 2: `## Output Format` contract drift**
- **Given** a persona template whose `## Output Format` section uses a different column order or delimiter than the canonical 7-column contract
- **When** the community render test compares the section text against the canonical contract string
- **Then** the test fails, since the reconciler downstream parses this format byte-for-byte

## Performance Requirements
- **Response Time:** Template render of 10 persona templates against a fixture context completes in well under 1 second in the test suite; no measurable regression versus baseline (≤1% wall-time difference in `go test ./...`) to `go test ./...` runtime.
- **Throughput:** N/A (test-time only, not a runtime request path).

## Security Considerations
- **Authentication/Authorization:** N/A — template rendering is local, no network or auth surface.
- **Input Validation:** Rendered output is asserted to contain no leftover `{{ }}` actions, preventing a malformed template from silently leaking template syntax into a review prompt sent to a model.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A single reusable `payload.PayloadContext` fixture (mirroring the existing `renderContext` helper in `personas_test.go`) with all required fields populated, plus a canonical `## Output Format` contract string constant to diff against
**Mock/Stub Requirements:** None — pure `text/template` execution, no HTTP or LLM calls

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] All 10+ community persona templates render with zero leftover `{{ }}` actions
- [ ] `## Role` and `## Output Format` (exact 7-column contract) are present in every template
- [ ] Frontier persona phrasing style differs per vendor per that vendor's own prompting guidance
- [ ] `## Output Format` text matches the canonical contract byte-for-byte across all personas

**Manual Review:**
- [ ] Code reviewed and approved
