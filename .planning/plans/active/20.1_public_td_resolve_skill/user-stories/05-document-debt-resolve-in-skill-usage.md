# User Story 5: Document Debt-Resolve in skill-usage.md

**Plan:** [20.1: Public TD Resolve Skill](../plan.md)

## User Story

**As a** new standalone/public atcr user installing the skill for the first time
**I want** `docs/skill-usage.md` to document the `/atcr debt resolve` route, the local `.atcr/`-scoped technical-debt store it reads from, and how both differ from the private `.planning/`-scoped `atcr debt` command family
**So that** I can discover, install, and use the review-and-fix loop without reverse-engineering it from source, and without confusing it with the unrelated private-pipeline `atcr debt list/add/dashboard` commands

## Story Context

- **Background:** `docs/skill-usage.md` currently documents only the review-orchestration skill (`/atcr <command>` dispatching `atcr range` → `atcr review` → host review → `atcr reconcile` → report). It has no mention of a debt backlog, a local store, or an autonomous resolve loop — those are all new capabilities this plan introduces (Stories 1-3). Once Stories 1-3 land, the capability exists but is undiscoverable to a standalone user reading the installation guide, since nothing in the doc tells them the store exists, what `/atcr debt resolve` does, or how it differs from the confusingly-similar-sounding private `atcr debt` commands documented in `docs/technical-debt.md`. This story closes that gap by extending the existing guide, following `docs/scorecard.md`'s established pattern for documenting a new local append-only store's format, CLI usage, and privacy model.
- **Assumptions:**
  - Stories 1-3 are functionally complete by the time this story's final AC pass runs — the store schema (`documentation/local-td-store-schema.md`, `.atcr/debt/` root, v1 schema), the `atcr reconcile` persistence hook (`--no-local-debt` flag), and the `/atcr debt resolve` skill route are all real, working behavior this doc describes, not aspirational. Drafting can start in parallel against the Story 1-3 designs, but the final content must be verified against their actual landed behavior before this story is considered done.
  - `docs/skill-usage.md`'s existing structure (Prerequisites → Installation → Usage → Output → cross-links) is the template to extend, not replace — new content is added as new sections/rows within that structure, consistent with how `docs/scorecard.md` documents a sibling local store (Storage, CLI Usage, Privacy Model sections).
  - If Story 4's `skill/CONVENTIONS.md` extraction changes the public installation steps (e.g., a shared file now installed alongside `SKILL.md`), this story's Installation section reflects that final state — this story does not duplicate CONVENTIONS.md's content, only points to it where install steps changed.
- **Constraints:**
  - Must not remove or restructure existing content in `docs/skill-usage.md` beyond what's needed to accommodate the new section(s) — this is an additive documentation change.
  - Must explicitly and clearly distinguish the new public/local `.atcr/`-scoped debt-resolve capability from the pre-existing private `.planning/`-scoped `atcr debt list/add/dashboard` family documented in `docs/technical-debt.md`, since both are named `atcr debt` and a user could otherwise reasonably conflate them.
  - Tone, structure, and level of technical detail should match `docs/scorecard.md` (the closest existing precedent for documenting a new local store's format/CLI/privacy model) rather than introducing a new documentation style.
  - No code changes — this story is documentation-only.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (Local TD store), User Story 2 (Reconcile-time persistence hook), User Story 3 (`/atcr debt resolve` skill route) |

## Success Criteria (SMART Format)

- **Specific:** `docs/skill-usage.md` gains a new section (or set of sections) documenting the `/atcr debt resolve` route: what it does, how the local `.atcr/`-scoped TD store accumulates findings across `atcr reconcile` runs, how to invoke the route, and an explicit callout contrasting it with the private `.planning/`-scoped `atcr debt list/add/dashboard` commands.
- **Measurable:** A reader of `docs/skill-usage.md` alone (no source-code access) can correctly answer: (1) what `/atcr debt resolve` does, (2) where the local store lives and how it's populated, (3) how to opt out of persistence, (4) how this differs from the private `atcr debt` family — verifiable via a documentation walkthrough/checklist during acceptance-criteria review.
- **Achievable:** The doc extension follows an established template (`docs/scorecard.md`'s Storage/CLI Usage/Privacy Model structure) and documents behavior that Stories 1-3 have already implemented — no new design decisions are needed at this stage, only accurate description of landed behavior.
- **Relevant:** Directly satisfies AC4; without this, the review-and-fix capability the rest of the plan builds is technically present but practically undiscoverable to the standalone users it targets.
- **Time-bound:** Draft can start in parallel with Stories 1-3; final content is completed and verified against landed behavior by the end of the current sprint, as the last story to close.

## Acceptance Criteria Overview

1. `docs/skill-usage.md` documents the `/atcr debt resolve` route's purpose, invocation, and behavior (what it reads, what it does, what it produces), consistent with the doc's existing Usage/Output section style.
2. `docs/skill-usage.md` documents the local `.atcr/`-scoped TD store (location, how it's populated by `atcr reconcile`, the `--no-local-debt` opt-out flag), in a style consistent with `docs/scorecard.md`'s Storage section.
3. `docs/skill-usage.md` includes an explicit contrast/disambiguation between the new public/local debt-resolve capability and the pre-existing private `.planning/`-scoped `atcr debt list/add/dashboard` family (cross-linking `docs/technical-debt.md`), so the two are not confused.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/20.1_public_td_resolve_skill/`_

## Technical Considerations

- **Implementation Notes:** Extend `docs/skill-usage.md` in place — likely a new `## Technical Debt Resolution` (or similarly named) section after the existing `## Output` section, plus an update to the file's opening summary paragraph if the skill's scope statement ("resolve range → fan out → host review → reconcile → report") should now mention the debt-resolve capability. Mirror `docs/scorecard.md`'s section shape (short intro paragraph, Storage subsection with path/rotation/permissions, CLI Usage subsection with command + flags, cross-link at the end) rather than inventing new structure. Add a "Related" or inline cross-link to `docs/technical-debt.md` for the disambiguation requirement (AC3).
- **Integration Points:** `docs/skill-usage.md` (file modified); `docs/scorecard.md` (structural/tone precedent, read-only reference); `docs/technical-debt.md` (cross-link target for the private/public contrast); `skill/CONVENTIONS.md` (Story 4 — reference only if it changed the Installation section's file list); `documentation/local-td-store-schema.md` (source of truth for store field/path claims, to keep documentation accurate without restating the full schema).
- **Data Requirements:** None (documentation-only story, no schema or code changes). Content must accurately reflect the final `.atcr/debt/` path, the `--no-local-debt` flag name, and the `/atcr debt resolve` invocation syntax as implemented by Stories 1-3 — verify against landed code/tests rather than the story drafts if either drifts during implementation.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Documentation is drafted against Story 1-3's planned design and drifts from what actually lands (e.g., flag name, store path, or command syntax changes during implementation) | Medium | Treat this story's final AC verification pass as a check against the actual merged Story 1-3 code/tests, not just their story files; update the draft before marking done |
| Readers conflate the new public `/atcr debt resolve` + `.atcr/`-scoped store with the pre-existing private `.planning/`-scoped `atcr debt list/add/dashboard` family, since both share the `debt` name | Medium | Include an explicit, unmissable disambiguation callout (not just a passing mention) contrasting scope (`.atcr/` vs `.planning/`), audience (standalone vs. private-pipeline), and command surface, cross-linked to `docs/technical-debt.md` |
| Doc update lands before Story 3's skill route is fully stable, describing behavior that later changes underneath it | Low | Sequence this story's final verification after Stories 1-3 are functionally complete, per the plan's stated dependency; draft-stage work can proceed in parallel but is not the definition of done |

---

**Created:** July 11, 2026
**Status:** Draft - Awaiting Acceptance Criteria
