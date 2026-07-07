# Acceptance Criteria: README Quickstart Recommends Default Persona Pack

**Related User Story:** [03: Recommend Default Persona Pack in Documentation](../user-stories/03-recommend-default-persona-pack-in-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `README.md` |
| Test Framework | Manual review / `git diff` | No automated test runner for docs-only changes |
| Key Dependencies | Existing "## Quickstart" numbered section (`README.md:36-57`: install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`) | Real bundle name sourced from Stories 1-2's published output once available; a clearly-marked placeholder is acceptable if this story ships first |

## Related Files
- `README.md` - modify: add one new step to the "## Quickstart" numbered list recommending installation of the default persona pack as part of first-time setup
- `docs/personas-install.md` - reference only: the step added here should point readers to this file for full `atcr personas` documentation (no edits to that file from this AC; see AC 03-01)
- [AC 03-01: Quick Walkthrough Recommends Default Persona Pack](03-01-quick-walkthrough-recommends-default-pack.md) - sibling AC covering the matching `docs/personas-install.md` Quick walkthrough update

### Related Files (from codebase-discovery.json)

- `README.md` — modify: add one new step to the "## Quickstart" numbered list
- `README.md:36-57` — existing "## Quickstart" numbered section to extend
- `docs/personas-install.md` — reference only: points readers to full `atcr personas` documentation

## Happy Path Scenarios
**Scenario 1: First-time user follows Quickstart and is pointed at the default persona pack**
- **Given** a new user reads `README.md`'s "## Quickstart" section
- **When** they follow the numbered steps (currently: install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`)
- **Then** one step recommends installing the default 3-persona pack (e.g. `atcr personas install bundle/<name>`) positioned alongside the existing `atcr init` / provider-setup steps, before or after `atcr doctor`

**Scenario 2: Existing 6 Quickstart steps remain intact**
- **Given** the current Quickstart numbering (steps 1-6)
- **When** the new persona-pack recommendation step is inserted
- **Then** all pre-existing steps retain their original command text and relative order (only numbering shifts to accommodate the insertion), satisfying the story's constraint that the numbered step format be preserved

## Edge Cases
**Edge Case 1: Stories 1-2 have not yet published a concrete bundle name at doc-edit time**
- **Given** this story is implemented before Stories 1-2 publish real persona/bundle names
- **When** the new Quickstart step is written
- **Then** the step uses generic placeholder language rather than a fabricated command, per the story's documented risk mitigation, and can be tightened once real names exist

**Edge Case 2: New step does not disrupt the two-command "zero arguments" pipeline framing**
- **Given** README.md's Quickstart currently frames `atcr review && atcr reconcile` as the "zero arguments" two-command pipeline (README.md:52-53, 63)
- **When** the persona-pack step is inserted
- **Then** it is positioned as an optional/recommended enhancement to first-time setup (e.g. near `atcr init`/provider setup) rather than altering or renumbering the core review/reconcile pipeline steps

## Error Conditions
**Error Scenario 1: N/A — documentation-only change**
- Error message: N/A for docs-only change
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — documentation only.
- **Input Validation:** N/A.

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** Rendered `README.md` (GitHub markdown preview) to visually confirm the new step reads naturally within the existing numbered Quickstart and doesn't disrupt the surrounding steps
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] "## Quickstart" section in `README.md` contains a new step recommending the default 3-persona pack
- [ ] The new step includes a concrete `atcr personas install bundle/<name>` example (or clearly-marked placeholder if Stories 1-2 haven't published names yet)
- [ ] The existing numbered steps retain their original order and command text
- [ ] `git diff README.md` shows only an additive insertion, no unrelated restructuring

**Manual Review:**
- [ ] Code reviewed and approved
