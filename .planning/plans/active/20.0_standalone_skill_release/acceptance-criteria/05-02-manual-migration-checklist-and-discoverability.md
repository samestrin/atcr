# Acceptance Criteria: Manual Migration Checklist and Discoverability

**Related User Story:** [05: External Migration Descope Note](../user-stories/05-external-migration-descope-note.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation file (numbered list + cross-links) | `docs/external-migration.md`, GitHub Flavored Markdown |
| Test Framework | Manual review / markdown link check | No code test framework applies |
| Key Dependencies | Story 1's `skill/SKILL.md` (dispatcher template, referenced by path); `docs/code-review-backend.md` (validation-target contract, referenced by path) | Both are read-only citations, no code coupling |

### Related Files (from codebase-discovery.json)

- `docs/external-migration.md` — create: contains the four-step numbered manual migration checklist and a "Related Documentation" / cross-reference section
- `docs/skill-usage.md` — modify: add a discoverability cross-link (e.g. in a "See also" or related-docs note) pointing to `docs/external-migration.md`, OR `docs/README.md` — modify: add `docs/external-migration.md` to the documentation index, whichever already has a natural section for related/advanced docs
- `skill/SKILL.md` — read-only reference: the dispatcher template the checklist instructs the operator to copy or adapt (produced by Story 1)
- `docs/code-review-backend.md` — read-only reference: the contract the checklist instructs the operator to validate the eventual migration against (produced/locked in by Story 2)

## Design References

- [External Private-Skill Migration Descope](../documentation/external-migration-descope.md) — source checklist and discoverability guidance
- [CLI Dispatcher Conventions](../documentation/cli-dispatcher-conventions.md) — command surface the migrated private skill must mirror
- [Backward-Compatibility Contract Test Patterns](../documentation/backward-compat-test-patterns.md) — validation target for the checklist's `docs/code-review-backend.md` contract step

## Happy Path Scenarios
**Scenario 1: Checklist contains all four required steps**
- **Given** the source checklist in `documentation/external-migration-descope.md` ("Migration Checklist (Manual Operator Action)")
- **When** `docs/external-migration.md` is authored
- **Then** it contains a numbered list with at minimum: (1) replace the fragmented private skills with a single `atcr` skill, (2) copy or adapt the dispatcher template from `skill/SKILL.md`, (3) preserve any `.planning/` sprint workflow hooks the private skills still need, (4) validate against the `docs/code-review-backend.md` contract

**Scenario 2: Doc is discoverable after the plan archives**
- **Given** `docs/README.md` is "the single source of truth the website build consumes" and links every doc in `docs/`
- **When** `docs/external-migration.md` is created
- **Then** either `docs/README.md` gains a new entry linking to it, or `docs/skill-usage.md` gains a cross-link to it in a related/advanced-docs section, so the note remains reachable independent of `.planning/plans/active/20.0_standalone_skill_release/`'s lifecycle

**Scenario 3: Checklist references the dispatcher template by path, not by internal detail**
- **Given** the Potential Risk in the story that checklist content could drift from `skill/SKILL.md`'s actual shape
- **When** step 2 of the checklist is written
- **Then** it points to `skill/SKILL.md` by file path/reference only, without duplicating routing table contents or line-specific internals that could go stale as Story 1's dispatcher evolves

## Edge Cases
**Edge Case 1: Neither `docs/README.md` nor `docs/skill-usage.md` has an existing "related docs" section**
- **Given** the story's Implementation Notes say to cross-link "if either already has a natural section"
- **When** authoring the cross-link
- **Then** a new, minimally-scoped entry is added to `docs/README.md`'s existing category structure (e.g. under "Integration" alongside `code-review-backend.md`) rather than inventing an unrelated new top-level section

**Edge Case 2: Checklist step wording diverges from the plan's canonical checklist**
- **Given** `documentation/external-migration-descope.md` is the locked-in source for checklist wording
- **When** `docs/external-migration.md`'s checklist is drafted
- **Then** the four steps are reproduced with equivalent meaning (paraphrase allowed) but no step is dropped, reordered into a different meaning, or merged in a way that loses one of the four distinct actions

## Error Conditions
**Error Scenario 1: Checklist is missing one or more of the four required steps**
- Condition: `docs/external-migration.md`'s checklist has fewer than 4 numbered items, or omits the `docs/code-review-backend.md` validation step
- Detection: manual review against the user story's Success Criteria "Measurable" bullet
- Required fix: add the missing step(s) before merge

**Error Scenario 2: Doc is created but never linked from any discoverable index**
- Condition: `docs/external-migration.md` exists but neither `docs/README.md` nor `docs/skill-usage.md` references it
- Detection: `grep -r "external-migration" docs/README.md docs/skill-usage.md` returns no match
- Required fix: add the cross-link per Scenario 2 above

## Performance Requirements
- **Documentation Accuracy Bar:** Checklist wording must not contradict `original-requirements.md`'s Out of Scope section or the 2026-07-03 addendum decisions D2/D3 referenced by the story's Constraints
- **Discoverability Bar:** The doc must be reachable within one hop from `docs/README.md` (the canonical index) after this AC is complete

## Security Considerations
- **No External-Repo Coupling:** The checklist must remain read-only guidance — it must not embed executable shell commands that write into `~/Documents/GitHub/claude-prompts/`, consistent with the story's Constraints ("must not attempt to write, stage, or reference commits in the external `claude-prompts` repository")
- **Input Validation:** N/A — static prose file

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** `grep -c` for the four checklist step keywords in `docs/external-migration.md`; `grep` for `external-migration` in `docs/README.md` and `docs/skill-usage.md` to confirm the cross-link exists
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] No linting errors (markdown lint, if configured, passes on modified/new files)
- [ ] No broken relative links introduced in `docs/README.md` or `docs/skill-usage.md`
- [ ] Build succeeds (docs-only change)

**Story-Specific:**
- [ ] `docs/external-migration.md` contains all four checklist steps from `documentation/external-migration-descope.md`
- [ ] `skill/SKILL.md` and `docs/code-review-backend.md` are referenced by path in the checklist
- [ ] `docs/README.md` or `docs/skill-usage.md` cross-links to `docs/external-migration.md`

**Manual Review:**
- [ ] Code reviewed and approved
