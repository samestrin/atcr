# User Story 3: Recommend Default Persona Pack in Documentation

**Plan:** [19.6: Default Model-Tuned Community Personas](../plan.md)

## User Story

**As a** new atcr user setting up the tool for the first time
**I want** the documentation to point me at a curated, model-tuned default persona pack during initial setup
**So that** I get a well-tuned review panel with a single install command instead of hand-authoring reviewer prompts from scratch

## Story Context

- **Background:** Stories 1 and 2 of this plan author a set of model-tuned reviewer personas (phrased per each target model's official prompting guide) and publish them to the external `atcr/personas` community registry, reachable through the already-shipped `atcr personas install <name>` / `atcr personas search <keyword>` commands documented in `docs/personas-install.md`. Those personas are invisible to new users unless the documentation tells them the pack exists and when to install it. This story closes that gap by editing the two documents a first-time user actually reads: `docs/personas-install.md` (the full command reference, which already has a "Quick walkthrough" section) and `README.md` (the top-level "Quickstart" section that walks through `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`).
- **Assumptions:** Stories 1-2 will have published real, named personas/bundles to the `atcr/personas` repo before this doc update ships, so the recommendation can cite concrete install commands (e.g. `atcr personas install bundle/<name>`) rather than vague placeholders. The existing `atcr personas install` / `atcr personas search` command surface and registry URL behavior are unchanged by this story.
- **Constraints:** Markdown-only change — no Go code, command, or flag is introduced or modified by this story; the underlying `personas install`/`search` commands already exist and are exercised as-is. Edits must be additive to the existing docs structure (preserve the "Quick walkthrough" section in `docs/personas-install.md` and the numbered step format in `README.md`'s Quickstart) rather than a rewrite.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Best sequenced AFTER Stories 1-2 land in the external `atcr/personas` repo, so this story's doc edits can reference real, live persona/bundle names instead of placeholders. If sequencing requires this story to ship first, the doc edit can be written with generic/placeholder language (e.g. "the recommended starter pack") and tightened once Stories 1-2 publish concrete names. |

## Success Criteria (SMART Format)

- **Specific:** `docs/personas-install.md`'s "Quick walkthrough" section and `README.md`'s "## Quickstart" section each contain an explicit recommendation to install the default model-tuned persona pack, with a runnable `atcr personas install <name>` (or `bundle/<name>`) example.
- **Measurable:** Both files are updated in this repo (diffable via `git diff`); the recommendation appears as a distinct, callable step in each section (not buried in prose).
- **Achievable:** Pure markdown edit reusing existing, already-shipped commands — no new code path, so it can be completed in a single pass.
- **Relevant:** Directly satisfies this plan's stated goal — "a new user gets a well-tuned review panel with a single install command instead of hand-authoring prompts from scratch" — by making that install command discoverable at the two points a first-time user is most likely to read.
- **Time-bound:** Completed within this plan's single implementation session, gated only on Stories 1-2 having published real persona names to reference.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md) | Quick Walkthrough Recommends Default Persona Pack | Manual |
| [03-02](../acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md) | README Quickstart Recommends Default Persona Pack | Manual |

## Original Criteria Overview

1. `docs/personas-install.md`'s "Quick walkthrough" section recommends installing the default model-tuned persona pack early in the walkthrough, with a concrete `atcr personas install <name>` example.
2. `README.md`'s "## Quickstart" section adds a step recommending the default persona pack as part of first-time setup, positioned alongside the existing `atcr init` / provider-setup steps.
3. Neither file's existing content (commands, structure, numbering) is removed or restructured beyond what's needed to insert the new recommendation.

## Technical Considerations

- **Implementation Notes:** Insert the recommendation into `docs/personas-install.md`'s existing "Quick walkthrough" section (search → install → list → test → upgrade → remove) as an early step, and add a step to `README.md`'s numbered Quickstart list (currently: go install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`). No other files require changes.
- **Integration Points:** References the `atcr personas install` / `atcr personas search` commands and the community registry (`ATCR_PERSONAS_URL` / default `atcr/personas` repo) exactly as already documented — this story only adds pointers to specific persona/bundle names published by Stories 1-2, it does not alter the registry mechanism.
- **Data Requirements:** None — no schema, config, or registry.yaml changes. The only "data" involved is the literal persona/bundle name(s) to cite in the doc text, sourced from Stories 1-2's published output.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Stories 1-2 haven't published final persona/bundle names by the time this story is implemented | Doc would cite a nonexistent or placeholder persona name, misleading users | Sequence this story after Stories 1-2 per the Dependencies note; if it must ship first, use clearly generic placeholder language and flag it for a follow-up tightening pass once real names exist |
| Edit disrupts the existing "Quick walkthrough" or numbered Quickstart structure, confusing readers who already reference those sections | Reduced doc usability, support burden | Treat the edit as additive — insert one new step/paragraph rather than reordering or rewriting existing steps |

---

**Created:** July 06, 2026
**Status:** Draft - Awaiting Acceptance Criteria
