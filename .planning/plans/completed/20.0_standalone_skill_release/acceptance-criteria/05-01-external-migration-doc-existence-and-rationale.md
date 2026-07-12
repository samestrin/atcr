# Acceptance Criteria: External Migration Doc Existence and Rationale

**Related User Story:** [05: External Migration Descope Note](../user-stories/05-external-migration-descope-note.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation file | New file `docs/external-migration.md`, GitHub Flavored Markdown |
| Test Framework | Manual review / markdown link check | No code test framework applies; verified by direct read and link resolution |
| Key Dependencies | None (pure prose, no code, no scripts) | Content promoted from `documentation/external-migration-descope.md` |

### Related Files (from codebase-discovery.json)

- `docs/external-migration.md` — create: durable public doc stating the workspace-boundary rationale and citing Epic 12.0's prior end-to-end backward-compatibility validation
- `.planning/plans/active/20.0_standalone_skill_release/documentation/external-migration-descope.md` — read-only source: existing Overview / Why It Is Out of Scope content this AC promotes and rephrases for a `docs/` audience
- `.planning/plans/active/20.0_standalone_skill_release/original-requirements.md` — read-only source: Out of Scope section and 2026-07-05 `/refine-epic` audit language ("the agent only has access to the `/Users/samestrin/Documents/GitHub/atcr` workspace")

## Design References

- [External Private-Skill Migration Descope](../documentation/external-migration-descope.md) — source content for the rationale and workspace-boundary explanation

## Happy Path Scenarios
**Scenario 1: Doc exists and states the workspace-boundary reason**
- **Given** the plan's `documentation/external-migration-descope.md` "Why It Is Out of Scope" section as source content
- **When** `docs/external-migration.md` is created
- **Then** it contains a clear statement that the private `claude-prompts` skills at `~/Documents/GitHub/claude-prompts/.claude/skills/` live outside this workspace's write access (`/Users/samestrin/Documents/GitHub/atcr`), and that this is why the migration is manual rather than automated by this plan

**Scenario 2: Doc cites Epic 12.0's prior validation**
- **Given** the source content's "Epic 12.0 already validated the private-skill backward-compatibility end-to-end from the external side" line
- **When** `docs/external-migration.md` is written
- **Then** it explicitly references Epic 12.0 (Skill Integration) as having already validated private-skill backward-compatibility end-to-end, so a reader does not conclude that compatibility is unverified or that this story re-validates it

## Edge Cases
**Edge Case 1: Relative links from the promoted content do not resolve from `docs/`**
- **Given** `documentation/external-migration-descope.md` links to `../original-requirements.md`, `../plan.md`, and `codebase-discovery.json` (all plan-folder-relative paths)
- **When** the content is promoted into `docs/external-migration.md`
- **Then** those links are either rephrased as prose (no dangling relative link) or replaced with the plan's public-facing GitHub path, since `original-requirements.md` and `codebase-discovery.json` are plan-scoped scaffolding that will not persist once `.planning/plans/active/20.0_standalone_skill_release/` archives to `.planning/plans/completed/`

**Edge Case 2: Story 1's `skill/SKILL.md` has not yet landed when this AC is authored**
- **Given** this story's Dependencies field requires Story 1 to complete first
- **When** `docs/external-migration.md` is written
- **Then** it references `skill/SKILL.md` by file path only (no line numbers or internal routing details that could drift once Story 1's rewrite lands)

## Error Conditions
**Error Scenario 1: Doc omits the workspace-boundary rationale**
- Condition: `docs/external-migration.md` states the migration is manual without explaining why
- Detection: manual review against Success Criteria "Measurable" bullet in the user story
- Required fix: add the one-sentence-or-less workspace-boundary explanation before merge

**Error Scenario 2: Doc implies the private skills are currently broken or incompatible**
- Condition: prose omits or contradicts the Epic 12.0 validation citation, implying compatibility is an open question
- Detection: manual review comparing against `original-requirements.md`'s AC3 framing (repo-local contract test, not re-touching external skills)
- Required fix: restore the explicit Epic 12.0 citation

## Performance Requirements
- **Documentation Accuracy Bar:** Every factual claim (workspace boundary, Epic 12.0 validation, `skill/SKILL.md` as dispatcher template) must be traceable to an existing source document (`original-requirements.md`, `documentation/external-migration-descope.md`, or the 2026-07-05 `/refine-epic` audit) — no new unverified claims introduced
- **Completeness Bar:** The doc is self-contained — a reader with no access to `.planning/` scaffolding can understand the rationale without following a broken link

## Security Considerations
- **No External-Repo Leakage:** The doc must not embed secrets, credentials, or machine-specific absolute paths beyond the already-public `~/Documents/GitHub/claude-prompts/.claude/skills/` reference used elsewhere in this plan's artifacts
- **Input Validation:** N/A — static prose file, no user input processed

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** Side-by-side comparison of `docs/external-migration.md` against `documentation/external-migration-descope.md` and `original-requirements.md`'s Out of Scope section
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] No linting errors (markdown lint, if configured, passes on the new file)
- [ ] No broken relative links within `docs/external-migration.md` (verified via link check or manual click-through)
- [ ] Build succeeds (docs-only change; site/docs build, if any, completes without error)

**Story-Specific:**
- [ ] `docs/external-migration.md` exists at the repo root `docs/` path
- [ ] File states the workspace-boundary rationale citing the 2026-07-05 `/refine-epic` audit language
- [ ] File explicitly cites Epic 12.0 as having already validated private-skill backward-compatibility end-to-end
- [ ] No dangling relative links to plan-scoped scaffolding (`../original-requirements.md`, `codebase-discovery.json`)

**Manual Review:**
- [ ] Code reviewed and approved
