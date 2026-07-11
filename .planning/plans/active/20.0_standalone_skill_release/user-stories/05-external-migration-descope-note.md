# User Story 5: External Migration Descope Note

**Plan:** [20.0: Standalone ATCR Skill Distribution](../plan.md)

## User Story

**As an** operator who will later update the private `claude-prompts` repository (`~/Documents/GitHub/claude-prompts/.claude/skills/`)
**I want** a documented, actionable manual migration checklist for adopting the `/atcr <command>` dispatcher pattern in the private skills
**So that** I can complete Proposed Solution #3's unification goal by hand, on my own schedule, without this plan silently dropping the requirement or this workspace attempting writes it has no access to perform

## Story Context

- **Background:** The original epic's Proposed Solution #3 called for migrating the private `claude-prompts` skills (`execute-code-review`, `reconcile-code-review`) to the same dispatcher pattern as `skill/SKILL.md` (Story 1), "bringing both public and private architectures into alignment under a modern, unified CLI-style UX." Two prior `/refine-epic` passes determined this cannot be automated from the `atcr` workspace: the 2026-07-05 audit states plainly, "Because the agent only has access to the `/Users/samestrin/Documents/GitHub/atcr` workspace, we cannot directly edit files in `claude-prompts`." `original-requirements.md`'s Out of Scope section correspondingly excludes "Modifying the existing private `.planning/` skills to use `.atcr/` (they should remain integrated with our sprint workflow)." A documentation stub already exists at `documentation/external-migration-descope.md` in this plan folder, produced by `/create-documentation`, and gives this story its checklist content and rationale almost verbatim — this story's job is to promote/finalize that content into a durable, plan-independent reference (`docs/external-migration.md`) rather than leave it stranded inside planning-only scaffolding.
- **Assumptions:** Epic 12.0 (Skill Integration) already validated private-skill backward-compatibility end-to-end from the external side, and Story 2's repo-local contract test locks in the `docs/code-review-backend.md` surface those private skills depend on — this story does not re-validate either of those. The dispatcher template produced by Story 1 (`skill/SKILL.md`) is the concrete artifact the manual checklist tells the operator to copy or adapt; this story is written to reference that template, not to re-derive dispatcher design decisions itself.
- **Constraints:** This story produces documentation only — no code, no shell scripts, no changes outside this repository's `docs/` (and this plan's `user-stories/`/`documentation/`) tree. It must not attempt to write, stage, or reference commits in the external `claude-prompts` repository; the checklist is guidance for a human operator to execute manually in that other repo, later, on their own initiative. It must not re-litigate the AC3 decision (repo-local contract test satisfies backward-compatibility verification) — that is locked in by Story 2 and the 2026-07-03 addendum decisions D2/D3.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Low |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (dispatcher template `skill/SKILL.md` must exist to be referenced by the checklist) |

## Success Criteria (SMART Format)

- **Specific:** A new `docs/external-migration.md` file exists, documenting the private `claude-prompts` skill migration as a manual operator follow-up action, with an explicit numbered checklist and a citation of `skill/SKILL.md` as the dispatcher template to copy or adapt.
- **Measurable:** The checklist contains at minimum the four steps already established in `documentation/external-migration-descope.md` — (1) replace the fragmented skills with a single `atcr` skill, (2) copy or adapt the dispatcher template from `skill/SKILL.md`, (3) preserve any `.planning/` sprint workflow hooks the private skills still need, (4) validate against the `docs/code-review-backend.md` contract — and states plainly, in one sentence or less, that no code in this repository or the `claude-prompts` repository changes as a result of this story.
- **Achievable:** The content is a documentation promotion/consolidation task drawing on material already drafted in `documentation/external-migration-descope.md`, `original-requirements.md`'s Out of Scope section, and the 2026-07-05 addendum — no new investigation or design work is required.
- **Relevant:** Closes the loop on Proposed Solution #3 without violating the workspace's single-repo write boundary; without this story, the unification goal from the addendum override would go undocumented and the requirement would appear silently dropped rather than deliberately descoped.
- **Time-bound:** Completed within this sprint, sequenced after Story 1 (so the dispatcher template it references exists) but independent of Stories 2-4's implementation work.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-external-migration-doc-existence-and-rationale.md) | External Migration Doc Existence and Rationale | Manual |
| [05-02](../acceptance-criteria/05-02-manual-migration-checklist-and-discoverability.md) | Manual Migration Checklist and Discoverability | Manual |
| [05-03](../acceptance-criteria/05-03-scope-containment-no-external-repo-writes.md) | Scope Containment — No External-Repo Writes | Manual |

## Original Criteria Overview

1. `docs/external-migration.md` exists, states the workspace boundary reason the migration cannot be automated here, and cites Epic 12.0 as already having validated private-skill backward-compatibility end-to-end.
2. The file contains a concrete, numbered manual migration checklist (replace fragmented skills, copy/adapt the `skill/SKILL.md` dispatcher template, preserve `.planning/` sprint workflow hooks, validate against `docs/code-review-backend.md`) that a human operator can follow later in the external `claude-prompts` repo.
3. No files outside this repository's `docs/` tree (and this plan's own `user-stories/`/`documentation/` folders) are modified, and no attempt is made to stage, commit, or write to `~/Documents/GitHub/claude-prompts/`.

## Technical Considerations

- **Implementation Notes:** Promote the existing `documentation/external-migration-descope.md` content (Overview, Why It Is Out of Scope, Migration Checklist, Quick Reference, Related Documentation sections) into `docs/external-migration.md`, adjusting relative links so they resolve correctly from the `docs/` directory rather than the plan's `documentation/` directory (e.g., `../original-requirements.md` becomes a reference to the plan folder path or is rephrased as prose, since `original-requirements.md` is plan-scoped scaffolding that will not persist after archival). Cross-link `docs/external-migration.md` from `docs/skill-usage.md` or `README.md` if either already has a natural section for related/advanced docs, so the note remains discoverable after this plan archives.
- **Integration Points:** References Story 1's `skill/SKILL.md` (the dispatcher template to copy/adapt) and Story 2's `docs/code-review-backend.md` contract test (the validation target for the eventual manual migration) — read-only references, no code coupling. No integration with the external `claude-prompts` repository beyond documenting its future manual update path.
- **Data Requirements:** None — pure documentation, no schema, config, or code changes.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Checklist content drifts from the actual `skill/SKILL.md` dispatcher shape once Story 1 lands, leaving the operator with stale guidance | Medium | Write this story after Story 1 completes (per Dependencies), and reference the dispatcher template by file path rather than duplicating its routing details, so the checklist stays accurate even if `skill/SKILL.md`'s internals evolve later |
| Descope note reads as dropping the requirement rather than deliberately deferring it, causing confusion about whether Proposed Solution #3 was actually addressed | Low | State explicitly in `docs/external-migration.md` why the migration is manual (workspace write-access boundary, cited from the 2026-07-05 `/refine-epic` audit) rather than silently omitting the rationale |
| Documentation becomes orphaned/undiscoverable once this plan folder archives to `.planning/plans/completed/` | Low | Land the durable copy at `docs/external-migration.md` (not only inside the plan's `documentation/` folder) and cross-link it from `README.md` or `docs/skill-usage.md` so it survives independently of the plan's lifecycle |

---

**Created:** July 11, 2026 01:48:34PM
**Status:** Draft - Awaiting Acceptance Criteria
