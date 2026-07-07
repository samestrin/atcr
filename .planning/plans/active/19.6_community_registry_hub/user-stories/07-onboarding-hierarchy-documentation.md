# User Story 7: Onboarding-Hierarchy Documentation Rewrite

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** first-time atcr user choosing how to pay for and configure model access
**I want** README.md and the persona docs to lead me straight to `atcr quickstart` with Synthetic, then clearly rank every other provider path (DashScope, Chutes/Featherless, LiteLLM, frontier/majors) by cost and effort
**So that** I land on the cheapest, one-command working setup first and only reach for pricier or more complex options when I deliberately choose to

## Story Context

- **Background:** `atcr quickstart` already ships and works today — it runs an interactive wizard, writes a `synthetic` provider registry, sets `LLM_SYNTHETIC_API_KEY`, prints the `https://synthetic.new/` signup link, and is referenced at README.md:59. This story does not change that behavior; it rewrites the documentation that surrounds it so the monetizing flat-rate path is what new users see first, while every other viable path (DashScope, Chutes, Featherless, LiteLLM, frontier providers) remains fully documented but visibly secondary or opt-in. `documentation/onboarding-hierarchy.md` (already generated for this plan) specifies the exact 5-tier ranking and the files to touch.
- **Assumptions:**
  - No CLI code changes are required for this story except documenting existing flags/commands (`atcr quickstart`, `atcr personas search/install/list/test`); any new `--model`/`--provider` search flags land via Theme 3 (Story covering AC2/AC6) before this story's discover-by-model examples can be verified end to end.
  - The discover-and-install-by-model flow example in the docs must match the actual CLI output/behavior once AC2's structured `provider`/`model` search lands — this story documents the flow as designed in `documentation/onboarding-hierarchy.md`.
  - README.md's existing `## Quickstart` section (README.md:59) is the anchor point for the rewrite; it is edited in place, not replaced with a new section.
- **Constraints:**
  - Must not remove or weaken documentation for any existing provider path (DashScope, Chutes, Featherless, LiteLLM, frontier) — the hierarchy re-ranks and re-frames, it does not delete.
  - DashScope gets docs-only treatment this epic — no `quickstart` wiring, per explicit out-of-scope note in `documentation/onboarding-hierarchy.md`.
  - Frontier/majors personas must never appear inside the default `quickstart` funnel narrative; they are documented as a deliberate "bring your own key" action taken via `atcr personas search`/`install`.
  - Tone and ranking language ("explore, not default", "Advanced", "opt-in bring your own key") must be applied consistently across all three touched files so a reader gets the same mental model regardless of entry point.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | M |
| **Dependencies** | Theme 3 (Story: Model-Aware Search and Discovery, AC2/AC6) for the discover-by-model flow example to reflect real `--model`/`--provider` flag behavior; Theme 4 (Story: Model-Indexed Persona Library Authoring, AC3) so `docs/personas-install.md` can cite real installed persona names in examples |

## Success Criteria (SMART Format)

- **Specific:** README.md's `## Quickstart` section and `docs/personas-install.md` are rewritten to present the 5-tier onboarding hierarchy (Synthetic primary → DashScope secondary → Chutes/Featherless explore-only → LiteLLM advanced → frontier/majors opt-in) exactly as specified in `documentation/onboarding-hierarchy.md`, and `docs/personas-authoring.md` gets the discover-by-model flow cross-reference.
- **Measurable:** All 5 hierarchy tiers are present and correctly ordered in both README.md and `docs/personas-install.md`; the discover-and-install-by-model bash example (search → install → list → test) appears verbatim-equivalent to the sequence in `documentation/onboarding-hierarchy.md`; zero mentions of frontier/majors personas inside the Quickstart narrative itself.
- **Achievable:** Pure documentation edit to 3 existing Markdown files; no new code, no schema changes — the only dependency is that Themes 3/4 land first so cited commands and persona names are accurate.
- **Relevant:** Directly satisfies AC5 and the plan's stated goal of pairing the persona/model work with onboarding docs that lead with the monetizing Synthetic path.
- **Time-bound:** Completed within the sprint phase allocated to Theme 7, after Themes 3 and 4 merge.

## Acceptance Criteria Overview

1. README.md's Quickstart section leads with `atcr quickstart` (Synthetic) as the one-command default, then summarizes the remaining hierarchy tiers with their caveats in order (DashScope, Chutes→Featherless, LiteLLM, frontier/majors).
2. `docs/personas-install.md` documents DashScope as a secondary flat-rate option (manual registry snippet + docs link, no quickstart wiring), Chutes then Featherless as explore-only with performance/context/concurrency caveats, LiteLLM as an Advanced aggregation-proxy note, and frontier/majors personas as opt-in "bring your own key" — plus the full discover-and-install-by-model flow (`personas search` → `install` → `list` → `test`).
3. `docs/personas-authoring.md` cross-references the discover-by-model flow so contributors understand how their authored persona becomes discoverable, without duplicating the full hierarchy explanation.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`_

## Technical Considerations

- **Implementation Notes:** This is a content/documentation edit only. Follow the exact tier order and language cues already drafted in `documentation/onboarding-hierarchy.md` (Sections "Onboarding Hierarchy" and "Discover-and-Install-by-Model Flow") — do not re-derive the ranking or invent new wording for the caveats (e.g., "explore, not default" for Chutes/Featherless, "Advanced" for LiteLLM, "bring your own key" for frontier/majors).
- **Integration Points:** README.md:59 (`## Quickstart` section, existing anchor); `docs/personas-install.md` (persona installation reference doc); `docs/personas-authoring.md` (contributor-facing authoring doc, touched here only for the discover-by-model cross-reference, with the human-names/structured-metadata convention updates owned by the Theme 6 story).
- **Data Requirements:** None — no schema, index, or persona-file changes. The bash flow example depends on the `--model`/`--provider` search flags and `atcr personas test` existing and behaving as described (owned by Themes 2/3/4).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Discover-by-model bash example is written before Theme 3's `--model`/`--provider` flags land, and drifts from actual CLI behavior | Medium | Sequence this story after Theme 3 merges; verify the example against a real `atcr personas search --model ...` invocation before finalizing the doc |
| Hierarchy language is applied inconsistently across README.md vs. `docs/personas-install.md` (e.g., different caveat wording for Chutes/Featherless), confusing readers who cross-reference both | Low | Reuse the exact phrasing from `documentation/onboarding-hierarchy.md` verbatim in both files rather than paraphrasing independently |
| Frontier/majors persona mentions leak into the Quickstart narrative, undermining the "opt-in only" positioning the plan requires | Medium | Explicit acceptance check: grep README.md's Quickstart section for frontier provider/model names (Claude, GPT, Gemini) after the rewrite and confirm none appear outside a clearly separated "opt-in" callout |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
