# Acceptance Criteria: `docs/personas-authoring.md` Cross-References the Discover-by-Model Flow

**Related User Story:** [07: Onboarding-Hierarchy Documentation Rewrite](../user-stories/07-onboarding-hierarchy-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation edit (`docs/personas-authoring.md`) | Content-only change; no code |
| Test Framework | Manual review + `grep`-based acceptance checks | No markdown lint configured in this repo |
| Key Dependencies | None beyond `docs/personas-install.md` existing (AC 07-02) as the cross-reference target | No new dependency introduced |

## Related Files
- `docs/personas-authoring.md` - modify: add a short cross-reference (in the "4. Contribution checklist" section, docs/personas-authoring.md:150, or immediately preceding it) pointing contributors to the discover-and-install-by-model flow documented in `docs/personas-install.md`
- `docs/personas-install.md` - reference only: target of the cross-reference link, carries the full discover-by-model flow per AC 07-02; not modified by this AC

## Happy Path Scenarios
**Scenario 1: Cross-reference added without duplicating the full hierarchy explanation**
- **Given** `docs/personas-authoring.md`'s existing four sections (persona YAML, prompt template, fixture, contribution checklist)
- **When** the cross-reference is added
- **Then** it is a short pointer (a sentence plus a Markdown link to `docs/personas-install.md`) explaining that a correctly authored persona's structured `provider`/`model` metadata is what makes it discoverable via `atcr personas search --model`/`--provider`, without restating the 5-tier onboarding hierarchy or the full bash flow

**Scenario 2: Cross-reference connects authoring metadata to discoverability**
- **Given** the persona YAML's "Agent binding" fields (docs/personas-authoring.md:22-27, provider/model) that this epic's Theme 2/4 stories bind into `index.json`'s structured `Provider`/`Model` fields
- **When** the cross-reference sentence is written
- **Then** it explicitly names the connection: the `provider`/`model` values a contributor sets in their persona YAML become the fields a user searches by in `atcr personas search --model`/`--provider`, per the discover-by-model flow in `docs/personas-install.md`

## Edge Cases
**Edge Case 1: Cross-reference does not duplicate content owned by the Theme 6 story**
- **Given** the story's Integration Points note that the human-names/structured-metadata convention updates to this file are owned by a separate Theme 6 story
- **When** this AC's cross-reference is added
- **Then** it only adds the discover-by-model pointer and does not attempt to rewrite the human-naming convention language or structured-metadata authoring rules (those are out of scope for this AC)

**Edge Case 2: Link target section exists at cross-reference time**
- **Given** this story's Dependency ordering (Theme 4 AC3, then this story after Theme 3/4 merge)
- **When** the cross-reference link is added to point at the discover-by-model flow in `docs/personas-install.md`
- **Then** the target section/anchor already exists in `docs/personas-install.md` (added by AC 07-02) so the link is not dangling

## Error Conditions
**Error Scenario 1: Cross-reference duplicates the full hierarchy explanation**
- **Given** the story's explicit instruction that this file gets the cross-reference "without duplicating the full hierarchy explanation"
- **When** the added content in `docs/personas-authoring.md` is reviewed
- **Then** any restatement of the 5-tier hierarchy (Synthetic/DashScope/Chutes-Featherless/LiteLLM/frontier) or the full 4-step bash sequence inside this file fails this AC — the file must link out, not duplicate
- HTTP status / error code: N/A (documentation-only; failure is a content-review rejection)

**Error Scenario 2: Broken relative link**
- **Given** the cross-reference uses a relative Markdown link to `docs/personas-install.md` (or an anchor within it)
- **When** the link is resolved from `docs/personas-authoring.md`'s location
- **Then** the link path resolves correctly (`personas-install.md` in the same `docs/` directory, or a matching heading anchor) — a broken or misspelled path fails this AC

## Performance Requirements
- **Response Time:** N/A — static Markdown content, no runtime execution.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — documentation-only change with no auth surface.
- **Input Validation:** N/A — no user input processed.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation content review)
**Test Data Requirements:** The rewritten `docs/personas-authoring.md` and the finalized `docs/personas-install.md` (from AC 07-02) to confirm the link target exists
**Mock/Stub Requirements:** None — link resolution can be checked with a simple relative-path existence check; no code under test

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Cross-reference sentence + link to `docs/personas-install.md`'s discover-by-model flow is added
- [ ] Cross-reference names the connection between authored `provider`/`model` YAML fields and search discoverability
- [ ] No restatement of the full 5-tier hierarchy or the 4-step bash sequence appears in `docs/personas-authoring.md`
- [ ] Link target exists and resolves correctly

**Manual Review:**
- [ ] Code reviewed and approved
