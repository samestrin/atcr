# User Story 4: Documentation Accuracy Pass

**Plan:** [20.0: Standalone ATCR Skill Distribution](../plan.md)

## User Story

**As a** maintainer preparing atcr's standalone skill for public release
**I want** `docs/skill-usage.md`, `docs/code-review-backend.md`, and `README.md` verified against the rewritten `skill/SKILL.md` dispatcher and cross-checked for the new `install.sh` linkage
**So that** external OSS developers and internal `claude-prompts` maintainers who read these docs get command examples, installation instructions, and contract descriptions that match the shipped code exactly, instead of drifting stale the moment the dispatcher rewrite lands

## Story Context

- **Background:** User Story 1 rewrites `skill/SKILL.md` from a linear review-only script into a `/atcr <command> <flags>` dispatcher routing to the Cobra command tree at `cmd/atcr/main.go:185-208`. That rewrite changes the skill's user-facing command surface and shape. `docs/skill-usage.md` already documents installation and usage of `skill/SKILL.md` and explicitly states artifacts land under `.atcr/reviews/<id>/` — per the plan's AC2, this statement is already accurate and must remain true post-rewrite, not be rebuilt from scratch. `docs/code-review-backend.md` documents the `--output-dir` contract that private-skill consumers rely on (the same contract User Story 2's new backward-compatibility test validates), with an "Output tree" code block and a "Behavioral notes for callers" section that must stay accurate. `README.md` already documents the `go install github.com/samestrin/atcr/cmd/atcr@latest` Quickstart path, `atcr doctor`, and `atcr quickstart` (README.md:59-89) and a Documentation section (README.md:199-207) cross-linking `docs/skill-usage.md` and other per-concern doc pages. This story is the plan's designated verification checkpoint (per plan.md's Implementation Strategy) confirming those three files did not drift once Stories 1 and 3 land.
- **Assumptions:** User Story 1 (dispatcher rewrite) and User Story 3 (`install.sh`) are the only stories in this plan that change user-facing surface these docs describe; User Story 2 (backward-compat test) and User Story 5 (external migration descope note) do not require doc changes here beyond what they already own. The canonical command tree for validating dispatcher command examples is `cmd/atcr/main.go:185-208` — any doc example must match those exact names, not stale or invented aliases. `docs/skill-usage.md`'s `.atcr/reviews/<id>/` artifact-location statement and `docs/code-review-backend.md`'s "Output tree" contract are already correct as of plan authoring; the task is confirming they remain correct, not authoring new content for them.
- **Constraints:** Out of scope: rewriting `docs/skill-usage.md` or `docs/code-review-backend.md` from scratch; building any new documentation tooling; touching the external `claude-prompts` repo's docs (that migration is User Story 5's descope note, not a docs edit here). Any edits made must be corrections that close a verified drift (a command name, a path, an installation step), not speculative rewrites. New doc content, if any is needed for `install.sh`, must follow the plan's established doc-per-concern pattern: link from README.md's Documentation section (README.md:199) rather than duplicating installation instructions inline.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (Dispatcher Skill Rewrite), User Story 3 (Install Script) |

## Success Criteria (SMART Format)

- **Specific:** Every command name, flag, and installation step referenced in `docs/skill-usage.md`, `docs/code-review-backend.md`, and `README.md` is checked against the post-dispatcher-rewrite `skill/SKILL.md` and the live Cobra command tree (`cmd/atcr/main.go:185-208`), and any confirmed drift is corrected in place.
- **Measurable:** All three files pass a line-by-line cross-check against current code/skill state with zero unresolved discrepancies; `docs/skill-usage.md`'s `.atcr/reviews/<id>/` artifact-location statement and `docs/code-review-backend.md`'s "Output tree" / "Behavioral notes for callers" sections are confirmed still accurate (or corrected); README.md's Documentation section links the new `install.sh` alongside the existing `go install` path with no functional change to the Quickstart section itself.
- **Achievable:** This is a verification-and-targeted-correction pass over three existing files, not new-document authoring — scope is bounded to confirming accuracy and fixing only what has actually drifted.
- **Relevant:** Directly satisfies the plan's AC2 (skill artifact-location accuracy) and AC4's quick-start portion (docs/skill-usage.md + atcr quickstart already satisfy the requirement); prevents the public release from shipping documentation that misleads external adopters about the dispatcher's command surface.
- **Time-bound:** Completed within this plan's single sprint, sequenced after User Story 1 (whose dispatcher rewrite is the change being verified against) and after User Story 3 (whose `install.sh` needs a README cross-link).

## Acceptance Criteria Overview

1. `docs/skill-usage.md`'s installation steps, usage table, and `.atcr/reviews/<id>/` output description are verified against the rewritten `skill/SKILL.md` dispatcher, with any drift corrected.
2. `docs/code-review-backend.md`'s "Output tree" code block and "Behavioral notes for callers" section are verified against the current `--output-dir` contract and current CLI behavior, with any drift corrected.
3. `README.md`'s command table, Quickstart section, and Documentation section (README.md:199) are verified for accuracy against `cmd/atcr/main.go:185-208` and updated to link the new `install.sh` from User Story 3, with no functional change to the existing `go install` Quickstart path.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/20.0_standalone_skill_release/`_

## Technical Considerations

- **Implementation Notes:** Treat this as a diff-style verification: read the post-rewrite `skill/SKILL.md` and the current `cmd/atcr/main.go:185-208` command registration as ground truth, then walk each of the three doc files section by section comparing claims against that ground truth. Only touch lines that are demonstrably wrong (a renamed command, a moved artifact path, a missing install reference) — do not restructure or reword sections that are already accurate.
- **Integration Points:** Reads (not writes, unless drift is found) `skill/SKILL.md` (post User Story 1), `cmd/atcr/main.go:185-208`, and the net-new `install.sh` (post User Story 3). Writes only to `docs/skill-usage.md`, `docs/code-review-backend.md`, and `README.md` — no other files. Any `install.sh` cross-link follows the existing doc-per-concern pattern (README.md's Documentation section, README.md:199-207) rather than inlining new installation prose into the Quickstart section.
- **Data Requirements:** None — no schema or data-model changes; this is a documentation-text verification and correction pass only.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Dispatcher rewrite (User Story 1) introduces command names or flags not reflected in `docs/skill-usage.md`, misleading external adopters | Medium | Cross-check every command reference in `docs/skill-usage.md` against `cmd/atcr/main.go:185-208` and the final `skill/SKILL.md`, correcting any drift before this story is considered complete |
| `docs/code-review-backend.md`'s `--output-dir` contract description silently diverges from the behavior User Story 2's backward-compatibility test actually verifies | Medium | Compare the "Output tree" and "Behavioral notes for callers" sections directly against User Story 2's test assertions and current CLI `--output-dir` behavior, not against memory of prior doc content |
| Scope creep into rewriting `docs/skill-usage.md` or `docs/code-review-backend.md` from scratch, exceeding the plan's explicit "verification pass, not new-document authoring" boundary | Low | Limit edits strictly to correcting verified drift; do not restructure sections, retitle headers, or add new prose beyond what closes a confirmed inaccuracy |
| `install.sh` cross-link gets added inline to README's Quickstart section instead of the Documentation section, breaking the established doc-per-concern pattern | Low | Follow the plan's documented pattern explicitly: link `install.sh` from README.md's Documentation section (README.md:199-207) or as an alternate install option near the existing `go install` line, without altering the Quickstart section's existing functional steps |

---

**Created:** July 11, 2026 01:48:34PM
**Status:** Draft - Awaiting Acceptance Criteria
