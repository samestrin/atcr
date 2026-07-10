# Acceptance Criteria: Contribution Checklist Cross-References `atcr personas submit`

**Related User Story:** [05: Documentation of the Submit Flow and Two-Tier Model](../user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no code) | Edits `docs/personas-authoring.md`'s existing `## 4. Contribution checklist` section only |
| Test Framework | None (docs-only) | No `go test` is added or run by this AC |
| Key Dependencies | None — pure content edit; the checklist item being cross-referenced already exists (docs/personas-authoring.md:172) | |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` (modify) — add a cross-reference sentence/note immediately adjacent to the "Fixture test passes" checklist item (`## 4. Contribution checklist`, line 172) stating that `atcr personas submit <name>` automates verification of that one item and blocks submission on a failing fixture; do not rewrite or reorder the existing checklist items (lines 166-174)
- `docs/personas-install.md` (reference only, no change beyond AC 05-01's edits) — the target of the cross-reference link, i.e. the new `### atcr personas submit <name>` subsection added by AC 05-01
- `.planning/plans/active/19.9_community_prompt_submissions/user-stories/05-documentation-of-submit-flow-and-two-tier-model.md` (reference only) — source of the Assumption that this story cross-references the existing checklist item "rather than rewriting the checklist, since `submit` automates verification of that one item, not the whole checklist"

## Design References
- [Personas Install & Authoring Doc Updates (AC4)](../documentation/personas-docs-updates.md) — the contribution-checklist cross-reference wording and the scope claim (fixture verification only)

## Happy Path Scenarios
**Scenario 1: A contributor preparing to submit finds the automation note**
- **Given** a contributor is reading `## 4. Contribution checklist` before running `atcr personas submit`
- **When** they reach the "Fixture test passes" item
- **Then** an adjacent note/link tells them `atcr personas submit <name>` re-runs this same fixture check automatically and will refuse to submit if it fails, with a link to the new subsection in `docs/personas-install.md`

**Scenario 2: Cross-reference does not overstate what `submit` automates**
- **Given** a contributor reads the new cross-reference note
- **When** they compare it against the full nine-item checklist
- **Then** the note scopes its claim to the single "Fixture test passes" item only — it does not claim `submit` verifies the YAML schema, `language` canonical form, prompt structure, category wording, or index-entry consistency, all of which remain manual checklist items

## Edge Cases
**Edge Case 1: Checklist item wording is not altered, only annotated**
- **Given** the existing checklist item at line 172 already reads "**Fixture test passes** locally with no network access."
- **When** the cross-reference is added
- **Then** the original bullet text is preserved verbatim and the cross-reference is appended as a trailing clause, footnote-style aside, or an immediately-following sentence — not a rewrite of the bullet itself

**Edge Case 2: Contributor has not yet read `docs/personas-install.md`**
- **Given** a contributor arrives at `docs/personas-authoring.md` first, without prior context on `submit`
- **When** they follow the cross-reference link
- **Then** the link resolves to the `#atcr-personas-submit-name` heading anchor in `docs/personas-install.md`, which is the exact subsection added by AC 05-01, giving them the full command syntax and error cases without needing to search the file manually

## Error Conditions
**Error Scenario 1: Cross-reference link is broken or targets the wrong anchor**
- Error message: N/A — documentation completeness defect, not a runtime error; caught by manually resolving the relative link (`personas-install.md#atcr-personas-submit-name` or equivalent generated anchor) against the actual heading AC 05-01 adds
- HTTP status / error code: N/A

**Error Scenario 2: Cross-reference implies `submit` verifies more of the checklist than it does**
- Error message: N/A — content-accuracy defect; caught by manual review comparing the note's scope claim against Theme 1's actual behavior (fixture gate only, not YAML schema/language-form/prompt-structure/index-entry checks)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials or auth flow described in this specific edit (covered instead by AC 05-01's `gh` precondition text).
- **Input Validation:** N/A — no user input parsed; the edit is prose only.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** The rendered `docs/personas-authoring.md`, reviewed to confirm the cross-reference appears adjacent to the correct checklist item, the link resolves, and the claim's scope matches Theme 1's actual fixture-gate behavior.
**Mock/Stub Requirements:** None — no code or automated test harness; verification is a documentation-accuracy read-through.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no test suite changes; pre-existing suite remains green)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A cross-reference to `atcr personas submit` is added immediately adjacent to the "Fixture test passes" checklist item (docs/personas-authoring.md:172) without altering the item's original wording
- [ ] The cross-reference links to the new `### atcr personas submit <name>` subsection in `docs/personas-install.md` (added by AC 05-01) and the link resolves correctly
- [ ] The cross-reference's claim is scoped to fixture verification only — it does not claim `submit` automates any other checklist item
- [ ] No other checklist item (lines 166-174) is reordered or reworded

**Manual Review:**
- [ ] Code reviewed and approved
