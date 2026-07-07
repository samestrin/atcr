# Acceptance Criteria: Schema & Template-Structure Validation Across All 3 Personas

**Related User Story:** [01: Author Model-Tuned Persona Content](../user-stories/01-author-model-tuned-persona-content.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cross-cutting schema/template-structure conformance check | Applies to all 3 new persona YAML + prompt template pairs at once |
| Test Framework | Existing registry agent schema validator + persona fixture test harness (per `docs/personas-authoring.md`) | Manual/external execution — no in-repo CI surface |
| Key Dependencies | `docs/personas-authoring.md` (schema required/optional fields, canonical prompt section structure, required template variables); `personas/_base.md` (canonical structure precedent) |

## Related Files
- `atcr/personas/claude-reviewer.yaml` / `atcr/personas/claude-reviewer.md` - validate: schema + canonical section structure
- `atcr/personas/gpt-reviewer.yaml` / `atcr/personas/gpt-reviewer.md` - validate: schema + canonical section structure
- `atcr/personas/gemini-reviewer.yaml` / `atcr/personas/gemini-reviewer.md` - validate: schema + canonical section structure
- `docs/personas-authoring.md` - reference only: the schema/structure contract this AC checks against (no change)

### Related Files (from codebase-discovery.json)

- `docs/personas-authoring.md` — registry agent schema, canonical prompt section structure, required template variables, and contribution checklist
- `personas/_base.md` — canonical built-in prompt template structure the community-persona template mirrors
- `internal/personas/client.go` — fetches and parses persona YAML; validates against registry schema
- `internal/personas/install.go` — validates and writes installed persona YAML to the local config directory
- `cmd/atcr/personas.go` — registers the `atcr personas install/search/list/upgrade/remove/test` CLI surface

## Happy Path Scenarios
**Scenario 1: All 3 YAMLs validate against the registry agent schema**
- **Given** the 3 new persona YAML files (`claude-reviewer.yaml`, `gpt-reviewer.yaml`, `gemini-reviewer.yaml`)
- **When** each is loaded by the existing registry schema validator
- **Then** each sets both required fields (`provider`, `model`) with valid, in-range values, and any optional fields present (`persona`, `role`, `language`, `version`, `description`) are well-formed per `docs/personas-authoring.md` — none carries an unknown or out-of-range agent field

**Scenario 2: All 3 prompt templates mirror the canonical section structure with no unrendered variables**
- **Given** the 3 new prompt templates (`claude-reviewer.md`, `gpt-reviewer.md`, `gemini-reviewer.md`)
- **When** each is checked against the canonical structure (`## Role`, `## Focus`, `## Scope` with `{{.ScopeRule}}`, `## Severity Rubric`, `## Output Format` with the exact 7-column pipe-delimited contract `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`, `## Payload`) and rendered with all required template variables (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`)
- **Then** every section is present in all 3 templates, the Output Format contract is byte-for-byte correct in all 3, and rendering leaves no leftover `{{ }}` actions in any of them

## Edge Cases
**Edge Case 1: One persona's `language` scope field is malformed**
- **Given** any of the 3 personas declares a `language` field
- **When** an entry is empty, whitespace-only, just dots, contains control characters, or contains embedded interior whitespace
- **Then** that persona fails schema validation (empty/whitespace/dots) or silently never matches (interior whitespace/compound extension) — per `docs/personas-authoring.md`'s canonicalization rules; the AC requires each declared `language` entry (if any) to be in canonical form (`["go"]`, not `[".go"]` or `["g o"]`)

**Edge Case 2: Output Format contract diverges between the 3 personas**
- **Given** all 3 templates must share the exact same 7-column contract
- **When** any one of the 3 templates alters the column order, count, or delimiter of `## Output Format`
- **Then** that persona fails this cross-cutting check — the reconciler parses this format byte-for-byte, so all 3 must match the canonical contract identically, not merely "close enough"

## Error Conditions
**Error Scenario 1: Unknown or out-of-range agent field in any of the 3 YAMLs**
- Error message: registry load error surfaced by the existing schema validator (per `docs/personas-authoring.md`'s "an unknown *agent* field or an out-of-range value is rejected before the persona is ever written to disk")
- HTTP status / error code: N/A (CLI/load-time validation error, not an HTTP path)

**Error Scenario 2: Missing required template variable causes render failure**
- Error message: "the renderer fails if a referenced variable is missing" (per `docs/personas-authoring.md` section 2)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static content validation, no runtime performance surface
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — schema validation requires no credentials
- **Input Validation:** All 3 YAMLs must pass strict agent-field validation; all 3 templates must contain no secrets, credentials, or network-call instructions (per `docs/personas-authoring.md`'s security note), since a persona prompt executes verbatim inside the review pipeline

## Test Implementation Guidance
**Test Type:** MANUAL (external-observation-based; this codebase's CI does not execute the `atcr/personas` repo's own schema/fixture test suite)
**Test Data Requirements:** N/A beyond the 3 persona YAML/template pairs themselves — this AC is a structural conformance check, not new data
**Mock/Stub Requirements:** N/A — verification is running `go test ./...` in the `atcr/personas` repo (or `atcr personas install <slug>` against each) manually, per the contribution checklist in `docs/personas-authoring.md`, not a mocked in-repo test

## Definition of Done
**Auto-Verified:**
- [ ] N/A in this codebase — no automated test target exists here for external-repo content (documented per story's Constraints)
- [ ] No linting errors (N/A — external repo)
- [ ] Build succeeds (N/A — external repo)

**Story-Specific:**
- [ ] All 3 YAMLs validate against the registry agent schema (required `provider`/`model`; any optional fields well-formed)
- [ ] All 3 prompt templates contain every canonical section and every required template variable, rendering with zero leftover `{{ }}` actions
- [ ] All 3 templates' `## Output Format` sections match the exact 7-column pipe-delimited contract byte-for-byte

**Manual Review:**
- [ ] Code reviewed and approved (external repo maintainer confirms all 3 personas pass the full contribution checklist in `docs/personas-authoring.md` before merge)
