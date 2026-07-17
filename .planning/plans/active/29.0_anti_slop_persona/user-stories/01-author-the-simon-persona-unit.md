# User Story 1: Author the `simon` Persona Unit

**Plan:** [29.0: Anti-Slop Persona (Simon) & Content Marketing](../plan.md)

## User Story

**As a** community-registry maintainer packaging a new reviewer lens
**I want** a `simon` persona YAML and prompt template authored to the registry's strict schema and template contract
**So that** engineering teams can install a one-command persona that hunts down AI-generated code bloat without waiting on the fixture or blog-post work to land first

## Story Context

- **Background:** ATCR's community registry (`personas/community/`) ships human-named persona units, each a `<name>.yaml` binding (provider/model/persona/role) paired with a `<name>.md` Go-template prompt. `sonny.yaml`/`sonny.md` is the closest structural analog: an openrouter-bound reviewer persona with the canonical `## Role` / `## Scope` / `{{if .ToolsEnabled}}...{{end}}` / `## Severity Rubric` / `## Output Format` / `## Payload` section order and a single leading `<!-- vendor-guidance: ... -->` citation comment. This story produces only the persona unit itself — `simon.yaml` and `simon.md` — not the fixture test or the `index.json`/registry wiring, which land in later stories of this plan.
- **Assumptions:** `docs/personas-authoring.md` is the current, authoritative authoring contract (fill-in-the-blank YAML template, prompt template rules, category-word registry) and has not drifted from what `internal/registry/persona_test.go` actually enforces. The slug `simon` is available (not already claimed in `personas/community/` or the built-in retired-role denylist) and needs no disambiguation.
- **Constraints:** `simon.yaml` must decode under yaml.v3 strict `KnownFields(true)` — no unrecognized keys — and must bind a concrete, non-placeholder `provider`/`model` pair (openrouter preferred over local, to avoid the additional ollama-pull-tag documentation gate that `provider: local` triggers). `simon.md` is rendered with Go `text/template`, restricted to bare references to the eight allowed template tokens (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`, `{{.ToolsEnabled}}`) plus a single `{{if .ToolsEnabled}}...{{end}}` block — no range/with/template/define, pipelines, or field chains — and must stay under `MaxPersonaPromptLen`. The Focus section's category word must be a new lowercase word distinct from the 13 already claimed (coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant) and must appear verbatim, case-insensitively, in the prompt body.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `personas/community/simon.yaml` and `personas/community/simon.md` exist, modeled on `sonny.yaml`/`sonny.md`, with `simon.md`'s `## Focus` section hyper-focused on tautological/apologetic AI comments, unnecessary design patterns applied to simple logic, defensive-programming overkill, and dead/hallucinated code paths.
- **Measurable:** `simon.yaml` contains exactly the allowed key set (name, version, description, provider, model, persona, role, optional language) with a concrete provider/model pair; `simon.md` contains all six mandatory section headings (`## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, `## Output Format`, `## Payload`) plus exactly one `<!-- vendor-guidance: ... -->` citation line and one net-new category word.
- **Achievable:** The `sonny.yaml`/`sonny.md` pair is a direct structural template already in the repository, so this is a content-authoring task within an established, machine-checkable contract, not new architecture.
- **Relevant:** This is the foundational artifact for the whole plan — the fixture test (Story 2) and the blog post's "install this persona" narrative (Story 3) both depend on `simon` existing and being schema-valid first.
- **Time-bound:** Completed within this sprint's first work session, ahead of Story 2's fixture authoring.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-simon-yaml-schema-binding.md) | `simon.yaml` Strict Schema and Concrete Provider/Model Binding | Unit |
| [01-02](../acceptance-criteria/01-02-simon-md-template-structure-focus.md) | `simon.md` Canonical Section Order, Template-Token Contract, and Anti-Slop Focus | Unit |
| [01-03](../acceptance-criteria/01-03-simon-authoring-contract-consistency.md) | `simon` Unit is Self-Consistent with the Authoring Contract and Auto-Discovered by the Registry Test Suite | Integration |

## Original Criteria Overview

1. `personas/community/simon.yaml` decodes cleanly under strict schema validation, with a concrete non-placeholder `provider`/`model` binding and slug `simon` matching `^[a-z]+$`.
2. `personas/community/simon.md` follows the canonical section order and template-token contract, includes the required vendor-guidance citation, and its `## Focus` section is hyper-focused on the four named anti-slop detection targets using a new, non-colliding category word.
3. The persona unit is self-consistent with `docs/personas-authoring.md`'s authoring contract such that the existing registry test suite (`TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames`, `TestCommunityPersonas_PromptStructure`, `TestCommunityPersonas_RendersInBothToolStates`, `TestCommunityPersonas_RequiredValuesRender`, `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass`) can pick it up without a persona-specific carve-out.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`_

## Technical Considerations

- **Implementation Notes:** Copy `personas/community/sonny.yaml` and `personas/community/sonny.md` as structural starting points. In `simon.yaml`, set `name: simon`, a fresh `description`, `provider: openrouter`, a concrete `model` (e.g. an existing catalog-approved openrouter model id already used elsewhere in `personas/community/`), `persona: simon`, `role: reviewer`. In `simon.md`, replace the vendor-guidance citation with one relevant to anti-bloat/conciseness prompting guidance, rewrite `## Role` to frame Simon as the panel's anti-slop lens, and write `## Focus` as a numbered list covering: (1) tautological/apologetic AI comments, (2) unnecessary design patterns (factories, interfaces) over simple logic, (3) defensive-programming overkill (redundant null/nil checks where type safety already guarantees non-nil), (4) dead or hallucinated code paths — using a new category word such as "bloat" verbatim in the prose.
- **Integration Points:** `internal/registry` loader (schema validation, strict YAML decode), `internal/registry/persona_test.go` (existing table-driven tests iterate all files in `personas/community/` — no test-file edits needed for this story to be picked up), `docs/personas-authoring.md` (contract reference only, not modified by this story).
- **Data Requirements:** None beyond the two new files; no `index.json` entry, catalog registration, or fixture is created in this story — those belong to later stories per the plan's task breakdown.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Category word collides with one of the 13 already-claimed words, or is chosen without checking `personas/community/*.md` for prior use | Medium | Grep all existing community persona prompts for the claimed-word list before finalizing `simon.md`'s Focus section wording; pick an unambiguous synonym like "bloat" and confirm no other file uses it as a CATEGORY value. |
| Provider/model pairing uses a model id not already validated elsewhere in the registry, causing `TestCommunityPersonas_NoPlaceholderModel` or catalog checks to fail | Medium | Reuse a `provider`/`model` combination already present in another `personas/community/*.yaml` file (e.g. matching `sonny.yaml`'s pattern with a different concrete model) rather than inventing an unverified model id. |
| Template drifts from the allowed-token whitelist (e.g. adds a pipeline or field chain) and fails the fetched-prompt guardrail | High | Author `simon.md` by editing a direct copy of `sonny.md`'s structural skeleton rather than writing template syntax from scratch; keep every token bare and the single `{{if .ToolsEnabled}}...{{end}}` block untouched in position. |
| Focus section reads as generic "code quality" advice rather than being hyper-focused on the four named anti-slop targets, weakening the persona's differentiation from existing reviewers like `sonny` | Medium | Ground each Focus bullet in a concrete, recognizable AI-slop pattern (e.g. "// This function checks if the value is valid" tautological comments; a `FooFactory` wrapping a single struct) so the prompt is unambiguously distinct from correctness-focused personas. |

---

**Created:** July 16, 2026 09:15:34PM
**Status:** Draft - Awaiting Acceptance Criteria
