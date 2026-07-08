# Acceptance Criteria: All-Human-Names Convention Documented as Forward-Looking Rule

**Related User Story:** [06: Authoring Contract Enforcement for Model Metadata and Human Names](../user-stories/06-authoring-contract-enforcement.md)
**Design References:** [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/personas-authoring.md`) | Additive section shared in intent with Epic 23.0's AC5 |
| Test Framework | Go `testing` + `stretchr/testify` (`assert.Contains`) | Same doc-content-assertion pattern as AC 06-01 |
| Key Dependencies | None (no new package) | Pure documentation change plus a grounding test |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` — modify: add a passage documenting that every new persona name must be an all-human first name, phrased as a forward-looking rule for contributions.
- `internal/personas/personas_test.go` — modify/create: a doc-content test asserting the human-names phrase/section is present in `docs/personas-authoring.md`.
- `.planning/plans/active/19.6_community_registry_hub/documentation/human-names-migration.md` — reference: grounding for the "all-human-names convention" concept and the straggler mapping.


## Happy Path Scenarios
**Scenario 1: New passage states the all-human-names rule**
- **Given** `docs/personas-authoring.md` after this story lands
- **When** a contributor reads the authoring contract before naming a new persona
- **Then** a section explicitly states new personas must use an all-human first name (not a role-based slug like `sentinel`/`tracer`/`idiomatic`), consistent with the built-in set's naming pattern

**Scenario 2: Passage is phrased as forward-looking, not retroactive**
- **Given** the wording of the new passage
- **When** it is compared against the story's constraint ("forward-looking rule... does not re-litigate naming choices already made")
- **Then** the passage describes what *new* contributions must do going forward and does not claim to audit or require renaming of any already-shipped persona

**Scenario 3: Wording is consistent with Epic 23.0 AC5**
- **Given** Epic 23.0's AC5 ("`docs/personas-authoring.md` documents the human-first-name convention for built-in personas as a forward-looking rule, so future additions follow it instead of reintroducing role-based names")
- **When** this story's passage is written
- **Then** it satisfies the same intent — phrased once, cross-referencing Epic 23.0 rather than authoring a second, differently-worded rule — so there is a single source of truth for the convention

## Edge Cases
**Edge Case 1: Passage does not restate naming choices from human-names-migration.md**
- **Given** `documentation/human-names-migration.md` documents the specific `sentinel`→`sasha` etc. mapping
- **When** the new authoring-contract passage is written
- **Then** it states the general rule (all-human-names for new personas) without re-deriving or restating the specific historical mapping — a link/summary reference is sufficient per the story's constraint

**Edge Case 2: Rule applies uniformly to built-in and community personas**
- **Given** the story scope covers "new persona names" broadly (not just built-ins)
- **When** the passage is written
- **Then** the wording does not scope the rule to built-ins only — community contributions are held to the same all-human-names convention

## Error Conditions
**Error Scenario 1: Doc test fails if the human-names passage is missing or removed**
- **Given** the doc-content test added alongside AC 06-01's test
- **When** `docs/personas-authoring.md` no longer contains the expected human-names phrase
- Error message: test failure output states `docs/personas-authoring.md missing "<phrase>"`
- HTTP status / error code: N/A (Go test failure, not an HTTP path)

## Performance Requirements
- **Response Time:** N/A — documentation-only change; test overhead is a single file read plus substring checks.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation file, no auth surface.
- **Input Validation:** N/A — static Markdown content, no user input parsed.

## Test Implementation Guidance
**Test Type:** UNIT (doc-content assertion test, no LLM/network)
**Test Data Requirements:** The literal content of `docs/personas-authoring.md` after the edit
**Mock/Stub Requirements:** None — direct `os.ReadFile` of the real doc file, same pattern as AC 06-01

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` documents the all-human-names convention as a rule for new persona contributions
- [ ] Wording is forward-looking (does not require renaming already-shipped personas)
- [ ] Wording is consistent with and cross-references Epic 23.0 AC5 rather than duplicating divergent phrasing
- [ ] A Go test asserts the human-names phrase is present in `docs/personas-authoring.md`

**Manual Review:**
- [ ] Code reviewed and approved
