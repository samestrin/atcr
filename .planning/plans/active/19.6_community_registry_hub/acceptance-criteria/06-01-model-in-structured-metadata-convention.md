# Acceptance Criteria: Model-in-Structured-Metadata Convention Documented

**Related User Story:** [06: Authoring Contract Enforcement for Model Metadata and Human Names](../user-stories/06-authoring-contract-enforcement.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/personas-authoring.md`) | Additive section, not a rewrite of the existing required-fields text |
| Test Framework | Go `testing` + `stretchr/testify` (`assert.Contains`) | Mirrors the existing doc-content assertion pattern in `internal/payload/template_test.go` (`TestDocs_PayloadModesExists`) and `internal/payload/tools_persona_test.go` |
| Key Dependencies | None (no new package) | Pure documentation change plus a grounding test |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` — modify: add a "Conventions" or contribution-checklist passage stating that a persona's bound `model` must be present in structured metadata (the existing required `model:` YAML key) and is enforced by the fixture test.
- `internal/personas/test.go` (`TemplateFixtureRunner.RunFixture`, `FixtureOutcome`) — reference: the enforcement point the new doc section must name accurately.
- `internal/personas/personas_test.go` — modify/create: a doc-content test asserting the new section's key phrases are present in `docs/personas-authoring.md`.


## Happy Path Scenarios
**Scenario 1: New section documents the model-in-metadata convention**
- **Given** `docs/personas-authoring.md` after this story lands
- **When** a contributor reads the document top to bottom
- **Then** a section (in "The persona YAML" or the "Contribution checklist") explicitly states that the bound `model` value must be present in the persona's structured metadata (the existing `model:` YAML key) and is not sufficient if only mentioned in free-text `description`

**Scenario 2: Section cross-references the fixture enforcement point**
- **Given** the new documentation passage
- **When** a contributor looks for where this rule is enforced
- **Then** the passage names the fixture test (e.g. "enforced by the persona fixture test in `internal/personas/test.go`") so the rule is traceable to executable code, not just prose

**Scenario 3: Existing required-fields text is preserved untouched**
- **Given** the pre-story "Required vs optional" paragraph and the `provider`/`model` REQUIRED template comments in section 1
- **When** the new section is added
- **Then** the original required-fields sentences remain byte-identical (additive change only, per the story's constraint against rewriting the required-fields section)

## Edge Cases
**Edge Case 1: Section placement does not duplicate the fixture requirements table**
- **Given** section 3 ("The fixture") already documents fixture location/format/naming rules
- **When** the new model-metadata passage is written
- **Then** it links to or briefly cross-references section 3 rather than re-stating the fixture requirements table, avoiding two divergent descriptions of fixture behavior

**Edge Case 2: Convention applies to community personas, not just built-ins**
- **Given** the story's scope explicitly targets community personas (the fixture path that previously short-circuited to `HasFixture: false`)
- **When** the documentation is written
- **Then** the wording does not imply the rule is built-in-only — it states the convention applies to every persona resolved by the fixture runner

## Error Conditions
**Error Scenario 1: Doc test fails if the section is missing or reworded away from required phrases**
- **Given** `internal/personas/personas_test.go`'s new doc-content test
- **When** `docs/personas-authoring.md` is missing the expected phrases (e.g. the section is deleted or the model-metadata sentence is removed in a future edit)
- Error message: test failure output states `docs/personas-authoring.md missing "<phrase>"` (mirroring the existing `TestDocs_PayloadModesExists` failure message format)
- HTTP status / error code: N/A (Go test failure, not an HTTP path)

## Performance Requirements
- **Response Time:** N/A — documentation-only change; the doc-content test reads one Markdown file (`os.ReadFile`) and performs in-memory substring checks, negligible overhead versus existing suite runtime.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation file, no auth surface.
- **Input Validation:** N/A — static Markdown content; no user input is parsed by this AC's changes.

## Test Implementation Guidance
**Test Type:** UNIT (doc-content assertion test, no LLM/network)
**Test Data Requirements:** The literal content of `docs/personas-authoring.md` after the edit; no fixtures or mocks needed
**Mock/Stub Requirements:** None — `os.ReadFile("../../docs/personas-authoring.md")` (relative path from `internal/personas/`) reads the real file, matching the existing pattern in `internal/payload/template_test.go`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` documents that the bound `model` must appear in structured metadata (not just free text)
- [ ] The new section names `internal/personas/test.go`'s fixture path as the enforcement mechanism
- [ ] A Go test asserts the new section's key phrases are present in `docs/personas-authoring.md`
- [ ] Pre-existing required-fields text is unchanged (additive-only diff)

**Manual Review:**
- [ ] Code reviewed and approved
