# User Story 4: Model-Indexed Persona Library Authoring

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** persona contributor and maintainer of the `samestrin/atcr` community-persona library
**I want** a curated set of human-named personas, each bound to a specific frontier or flat-rate model and prompt-phrased per that model's own vendor prompting guidance, authored per `docs/personas-authoring.md` and listed in `personas/community/index.json`
**So that** a user who already holds an API key for a given model — Claude, GPT, Gemini, or a flat-rate open model like DeepSeek/Qwen/Kimi/GLM — can search by that model and install a persona genuinely tuned to get the best review out of it, not a generic prompt with the model name swapped in

## Story Context

- **Background:** Theme 2 (structured `provider`/`model` metadata on `PersonaIndexEntry`) and Theme 3 (model-aware `search`/`--model`/`--provider`) build the *machinery* for discover-by-model; this story is the *content* that machinery has nothing to search over without. Per the clarification on record, the library covers 3 frontier providers (Anthropic, OpenAI, Google), each shipping a flagship+fallback pair (e.g., Claude Opus primary / Claude Sonnet fallback), plus flat-rate open models (DeepSeek, Qwen, Kimi, GLM). Every persona is a YAML + Markdown prompt + `.patch` fixture triple living in the in-repo community layout (`personas/community/`), authored to the exact contract in `docs/personas-authoring.md`: `provider`+`model` REQUIRED and strictly validated, prompt template with mandatory `## Role`/`## Output Format` sections and the required template variables (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`), and a `<slug>_fixture.patch` in `personas/testdata/`-equivalent for the community layout whose target-class category word appears in the prompt template itself.
- **Assumptions:** Each persona is scoped to the review task that model's vendor guidance and observed strengths make it best suited for (e.g., a reasoning-heavy model tuned toward architecture/logic review, a fast/cheap model tuned toward a narrower or higher-volume lens) rather than every persona being a generic all-purpose reviewer restated per model. Vendor prompting guidance (Anthropic's, OpenAI's, Google's, and each open-model provider's own documented best practices) is the grounding source for how each prompt is phrased — structure, emphasis, and instruction style differ per vendor (e.g., explicit step-by-step framing vs. terse directive framing) even though the mandatory section/variable contract stays constant. Theme 2's schema extension (`Provider`/`Model`/`Tasks`/`Tags` on `PersonaIndexEntry`) is assumed available or landing in parallel so each persona's `index.json` entry can carry its bound model; this story does not re-implement that struct.
- **Constraints:** Every persona YAML must pass strict registry-schema validation (`provider`+`model` required, no unknown agent fields) exactly as `docs/personas-authoring.md` specifies — no persona ships with a placeholder or unvalidated binding. Every persona's fixture must pass the existing fixture-test pattern (template renders fully, no leftover `{{ }}`, category word present in the prompt) with no network access. No persona prompt may embed credentials, secrets, or instructions to make external network calls. Naming follows the all-human-names convention (Theme 5/Epic 23.0) — no role-based persona names in the new library.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | XL |
| **Dependencies** | Theme 2 (structured `provider`/`model` metadata on `PersonaIndexEntry` and `index.json` schema) should land first or in parallel so each authored persona has a schema to register into; no code dependency on Theme 1 or Theme 3 to author the content itself |

## Success Criteria (SMART Format)

- **Specific:** `personas/community/` contains one human-named persona (YAML + Markdown prompt + `.patch` fixture) per covered model — 3 frontier providers (Anthropic, OpenAI, Google) each with a flagship+fallback pair, plus flat-rate open models (DeepSeek, Qwen, Kimi, GLM) — every persona's YAML carries a validated `provider`+`model` binding, every prompt is phrased per that model's own vendor/official prompting guidance and scoped to a specific review task suited to that model, and every persona is listed in `personas/community/index.json` with its bound model discoverable.
- **Measurable:** `go test ./...` (including the fixture test) passes with zero failures across every newly authored persona; each persona YAML validates against the strict registry agent schema (rejects on an unknown/out-of-range agent field); each persona's fixture is committed under the testdata convention, named `<slug>_fixture.patch`, and its target-class category word is present in its own prompt template; `index.json` contains one entry per authored persona with `provider`/`model` populated and matching the YAML.
- **Achievable:** The authoring contract, template structure, and fixture-test pattern already exist and are proven by the built-in personas (`bruce.md`, `sentinel.md`, `tracer.md`, etc.); this story applies that same proven pattern across a bounded, enumerated model list rather than inventing new mechanics.
- **Relevant:** This is the content deliverable that makes AC3's model-indexed library and AC6's discover-by-model flow meaningful — without genuinely model-tuned personas to find, the search/index machinery has nothing worth discovering.
- **Time-bound:** Deliverable within this sprint, authored as its own phase given the plan's noted risk that content authoring is judgment-heavy and should not block the schema/network code's merge cadence (per plan.md's risk mitigation).

## Acceptance Criteria Overview

1. Every one of the 3 frontier providers (Anthropic, OpenAI, Google) has a flagship+fallback persona pair, each bound to its specific model via a validated `provider`+`model` YAML field.
2. Each flat-rate open model in scope (DeepSeek, Qwen, Kimi, GLM) has at least one persona bound to its specific model, task-scoped to a review lens suited to that model.
3. Every persona's prompt template is phrased per that model's own vendor/official prompting guidance (not a generic template with the model name substituted in), mirrors the canonical section structure (`## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, `## Output Format`, `## Payload`), and renders with no leftover `{{ }}` actions.
4. Every persona has a passing `.patch` fixture per `docs/personas-authoring.md` (correct location/naming, category word present in its own prompt template, fixture test green with no network access).
5. `personas/community/index.json` lists every authored persona with its `provider`/`model` populated, matching the persona's own YAML.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`_

## Technical Considerations

- **Implementation Notes:** For each persona: (1) write the YAML per `docs/personas-authoring.md`'s template with `provider`+`model` REQUIRED fields set to the exact model binding, `name`/`version`/`description` catalog metadata, and an optional `language`/task scope where relevant; (2) write the Markdown prompt mirroring `bruce.md`'s/`sentinel.md`'s canonical structure, researched and phrased against that model's own vendor prompting documentation (e.g., Anthropic's prompt-engineering guidance for the Claude personas, OpenAI's for the GPT personas, Google's for the Gemini personas, and each open-model provider's published guidance where available), with the target category word named directly in `## Focus`/`## Output Format`; (3) write a synthetic `.patch` fixture in the testdata location containing a known instance of that category; (4) add the persona's entry to `personas/community/index.json` with `provider`/`model` matching the YAML.
- **Integration Points:** `personas/community/` (new persona YAML + Markdown files), `personas/community/testdata/` (or equivalent fixture location per the in-repo community layout), `personas/community/index.json` (discovery index Theme 2/3 read from), the existing fixture-test harness (`personas/personas_test.go`/`grounding_test.go` pattern) extended to iterate the community persona set.
- **Data Requirements:** Each persona YAML is a superset of a registry agent (strict validation on `provider`/`model`/`persona`/`role`/`language`, non-strict on catalog-only `name`/`version`/`description`); `index.json` entries carry the Theme 2 schema extension (`Provider`/`Model`, plus `Tasks`/`Tags` as warranted) so search can match on structured data rather than free text.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Authoring 8+ genuinely model-tuned personas (real vendor-guidance research per model, not templated substitution) is a large, judgment-heavy content workload that could stall a linear TDD sprint | High | Per plan.md's own risk mitigation, isolate this content authoring into its own sprint phase, separate from the schema/network code, so content review cadence doesn't block code merge cadence; author and land personas incrementally (e.g., frontier pairs first, then flat-rate models) rather than as one atomic batch |
| A persona's prompt reads as a generic template with the model name swapped in, rather than genuinely reflecting that model's vendor prompting guidance, undermining the "discover by model" value proposition | Medium | Cite the specific vendor guidance consulted for each persona's phrasing choices during authoring/review; task-scope each persona to a lens that plausibly differs per model (not seven copies of the same generalist prompt) |
| Fixture category word accidentally leaks in only from the injected diff rather than being authored into the prompt template itself, silently passing the fixture test without proving intent | Medium | Follow `docs/personas-authoring.md`'s explicit test behavior: the fixture test asserts the category word in the template text, independent of the diff payload — verify this manually per persona during authoring, not just via the automated pass |
| Open-model vendor prompting guidance (DeepSeek/Qwen/Kimi/GLM) is less standardized or less publicly documented than frontier-provider guidance, risking thin or speculative grounding | Medium | Use each provider's own official model card / API docs / prompting guide where published; where guidance is thin, ground the persona in the model's documented strengths (e.g., cost/throughput profile, context window, benchmark focus) rather than inventing unsupported vendor claims |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
