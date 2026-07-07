# Acceptance Criteria: Flat-Rate Open Model Persona Coverage

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Persona content (YAML + Markdown), community layout | `personas/community/<slug>.yaml`, `personas/community/<slug>.md` |
| Test Framework | Go `testing` package (community fixture-test loop from AC 04-04) | Schema validation exercised via the registry loader |
| Key Dependencies | `docs/personas-authoring.md` authoring contract; each open-model provider's published model card/API docs | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `personas/community/<deepseek-slug>.yaml` — create: DeepSeek-bound persona YAML with non-empty `provider`/`model`.
- `personas/community/<qwen-slug>.yaml` — create: Qwen-bound persona YAML.
- `personas/community/<kimi-slug>.yaml` — create: Kimi (Moonshot)-bound persona YAML.
- `personas/community/<glm-slug>.yaml` — create: GLM (Zhipu)-bound persona YAML.
- `personas/community/<slug>.md` — create: prompt template per flat-rate persona, grounded in that model's documented strengths.
- `personas/community/index.json` — modify: one entry per flat-rate persona with `provider`/`model` matching each YAML.
- `docs/personas-authoring.md` — reference: authoring contract and provider-key conventions.
- `personas/_base.md` — reference: shared prompt-template scaffold.


## Happy Path Scenarios
**Scenario 1: Every flat-rate open model in scope has at least one bound persona**
- **Given** the completed `personas/community/` directory
- **When** the persona YAML files are enumerated and grouped by `provider`
- **Then** `deepseek`, `qwen`, `kimi`, and `glm` (or their registry-accepted provider keys) each resolve to at least one persona YAML with a non-empty `model` field

**Scenario 2: Each flat-rate persona is task-scoped to a lens suited to that model**
- **Given** a flat-rate persona's Markdown prompt (e.g. the DeepSeek persona)
- **When** its `## Role`/`## Focus` sections are read
- **Then** the review lens named is grounded in that model's documented strengths (e.g. cost/throughput profile, reasoning depth, context window) rather than a generic all-purpose reviewer restated with the model name swapped in

**Scenario 3: Every flat-rate persona YAML passes strict registry-schema validation**
- **Given** a flat-rate persona YAML (e.g. the Qwen persona)
- **When** the persona loader parses and validates it against the registry agent schema
- **Then** validation succeeds with `provider` and `model` both non-empty and no unknown agent field present

**Scenario 4: Empty or missing `model` field is rejected before the persona is considered complete**
- **Given** a flat-rate persona YAML with `provider: "deepseek"` and `model` omitted or set to `""`
- **When** the registry schema validates it
- **Then** validation fails with a required-field error identifying the persona file and the missing `model` key, and the AC's Definition of Done cannot be marked complete until the field is populated

## Edge Cases
**Edge Case 1: Provider key for an open model matches the registry's OpenAI-compatible routing expectations**
- **Given** the `provider` value authored in each flat-rate YAML (e.g. `deepseek`, `openrouter` with a vendor-prefixed model id, or another accepted key)
- **When** it is compared against the provider keys accepted elsewhere in the registry
- **Then** the value uses the exact accepted casing/spelling, so the persona is installable rather than rejected at load

**Edge Case 2: Thin or unofficial vendor guidance for an open model**
- **Given** an open-model provider (e.g. Kimi/GLM) with less standardized public prompting guidance than a frontier provider
- **When** the persona's task scope is authored
- **Then** the prompt is grounded in that provider's official model card/API docs/documented strengths rather than an invented or unsupported vendor claim, per the story's risk mitigation

## Error Conditions
**Error Scenario 1: Missing `model` field on a flat-rate persona**
- **Given** a flat-rate persona YAML with `provider` set but `model` omitted or empty
- **When** the persona loader validates it
- **Then** validation fails with a required-field error and the persona is not written to the index

**Error Scenario 2: A flat-rate persona is a copy of a frontier persona's generic prompt**
- **Given** a flat-rate persona's `## Focus` section
- **When** it is compared against a frontier persona's `## Focus` section for content-review purposes
- **Then** the two are not near-identical text with only the model name swapped — this fails the manual review step of Definition of Done

## Performance Requirements
- **Response Time:** N/A — static content authored at commit time; no runtime performance surface introduced by this AC.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — persona YAML/Markdown are static repository content with no auth surface.
- **Input Validation:** Every flat-rate persona YAML validates against the strict registry agent schema; no persona embeds credentials, secrets, or network-call instructions in its prompt template.

## Test Implementation Guidance
**Test Type:** UNIT (schema validation via the existing registry loader test pattern) + manual content review (task-scope distinctness, vendor-guidance grounding)
**Test Data Requirements:** The 4+ flat-rate persona YAML files themselves, loaded through the same validation path `internal/registry` already exercises for agent configs
**Mock/Stub Requirements:** None — pure static-file validation, no network or LLM call required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] DeepSeek, Qwen, Kimi, and GLM each have at least one persona YAML with non-empty `provider`+`model`
- [ ] Every flat-rate persona YAML passes strict schema validation
- [ ] Each flat-rate persona's task scope is grounded in that model's documented strengths, not a generic restatement
- [ ] No flat-rate persona duplicates a frontier persona's prompt text with only the model name changed

**Manual Review:**
- [ ] Code reviewed and approved
