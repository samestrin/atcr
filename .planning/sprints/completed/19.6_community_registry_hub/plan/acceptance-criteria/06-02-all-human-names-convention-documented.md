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
- `.planning/epics/active/23.0_human_persona_renaming.md` — reference: the epic this AC (with AC 05-01/05-02/05-03/05-04) absorbs. Per its own 2026-07-07 reconciliation note (option "a" — fold the rename into 19.6 and close 23.0 as absorbed), 23.0's AC1–AC5 are satisfied inside 19.6. This AC formally records that absorption for AC5 (the docs rule) so 23.0 is not executed as a second, competing renamer.

> **Epic 23.0 absorption (LOCKED).** Epic 23.0 is folded into and SUPERSEDED by Epic 19.6. The straggler rename (23.0 AC1–AC4) is performed by ACs 05-01/05-02/05-03/05-04; the human-names documentation rule (23.0 AC5) is performed by this AC. 23.0 must NOT be run standalone — it is closed as absorbed. Where 23.0 AC5 scopes the convention to "built-in personas," 19.6's broader AC8 wording (built-in AND community, per Edge Case 2) SUBSUMES that narrower scope — this is a superset, not a contradiction: everything 23.0 AC5 required is still documented, plus community personas.


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

**Edge Case 2: Rule applies uniformly to built-in and community personas (reconciled ONCE)**
- **Given** the story scope covers "new persona names" broadly (not just built-ins), and 23.0 AC5 scoped the identical rule to "built-in personas" only
- **When** the passage is written
- **Then** the shared `docs/personas-authoring.md` human-names rule is written EXACTLY ONCE, worded to cover both built-in and community personas — this broader scope subsumes 23.0 AC5's built-in-only phrasing (a superset, not a contradiction), so there is a single source of truth and no divergent second rule. The wording does not scope the rule to built-ins only — community contributions are held to the same all-human-names convention

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
- [ ] Wording is consistent with and cross-references Epic 23.0 AC5 rather than duplicating divergent phrasing; the rule is stated once, covering built-in AND community personas (superset of 23.0 AC5's built-in-only scope, no contradiction)
- [ ] Epic 23.0 is explicitly recorded as absorbed/superseded by 19.6 (its AC1–AC4 covered by ACs 05-01/05-02/05-03/05-04, its AC5 by this AC) and is not executed as a standalone renamer
- [ ] A Go test asserts the human-names phrase is present in `docs/personas-authoring.md`

**Manual Review:**
- [ ] Code reviewed and approved
