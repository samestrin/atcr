# Plan 19.6: Default Model-Tuned Community Personas

## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-07-06
**Plan Goal:** Ship 3 default, model-tuned reviewer personas — one for Anthropic Claude, one for OpenAI GPT, one for Google Gemini, each phrased per that provider's own official prompting guide with a flagship-primary + same-family-fallback model binding — distributed through atcr's existing `atcr personas install` community channel, so a new user gets a well-tuned, frontier-model-diverse review panel with a single install command instead of hand-authoring prompts from scratch.
**Target Users:** New atcr users onboarding a reviewer panel; community persona contributors publishing model-tuned content
**Framework/Technology:** Go (atcr CLI/MCP server); persona content is YAML + Markdown templates authored in the external `atcr/personas` repo

## Objectives

1. Author 3 model-tuned persona YAML + prompt templates in the external `atcr/personas` repo — one for Anthropic Claude, one for OpenAI GPT, one for Google Gemini — each phrased per that provider's own official prompting guide, bound to a flagship-primary + same-family-fallback model pair (mirroring `registry.yaml`'s existing primary/backup convention), with a passing fixture for each.
2. Publish the new personas via the existing community-persona channel by adding them to the `atcr/personas` repo's `index.json`, so they are discoverable via `atcr personas search` and installable via `atcr personas install <name>`.
3. Update this repo's documentation (`docs/personas-install.md` and the README quickstart) to recommend installing the new default persona pack as part of first-time setup.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 3 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`

## Feature Analysis Summary
This epic fills a content gap in Epic 9.0's already-shipped community-persona distribution mechanism (`atcr personas install/search/list/upgrade/remove/test`). No default personas tuned to a frontier provider's *own official prompting guide* exist yet, so new users either hand-write persona YAML from scratch or reuse generic domain personas (`bruce`, `security/owasp`) that don't take advantage of model-specific prompting techniques. This is distinct from the user's existing production-tuned panel (`~/.config/atcr/registry.yaml`, ported from `llm-tools`), which already tunes *which model* plays each of 11 review lenses (MiniMax, Qwen, Kimi, Deepseek, GLM, Gemma, Nemotron, poolside, OpenAI's open-weight `gpt-oss-20b`) — valuable prior art this epic preserves rather than replaces, since none of those personas are phrased per Anthropic's, OpenAI's, or Google's own official prompting guide. The fix is pure content: author 3 new persona YAML + prompt templates — one per top-3 frontier provider (Anthropic, OpenAI, Google), each phrased per that provider's own official prompting guide and bound to a flagship-primary + same-family-fallback model pair — in the external `atcr/personas` repo, publish them via that repo's `index.json`, and update this repo's docs to recommend installing them during first-time setup. No new schema, command, or hosting is introduced anywhere.

## Out of Scope

- An interactive registry "marketplace" website or UI — this is purely a static file distribution initially.
- Dynamic registry generation.
- Hosting on `atcr.dev` — superseded by the existing community-persona channel.
- Any new `registry.yaml` schema for mapping tasks to default models/personas — superseded by the existing named-agent + roster model.

## Dependencies

- **Epic 9.0 (Persona Ecosystem)** — provides the community persona install/search/distribution mechanism this epic authors content for.
- ~~Epic 19.2 (Shared Registry Remote Fetch)~~ — no longer a dependency; this plan does not touch `ATCR_REGISTRY_URL` or `registry.yaml` hosting.

## Technical Planning Notes
- The user's existing `~/.config/atcr/registry.yaml` panel (ported from `llm-tools`) already tunes *which model* plays each of 11 review lenses using MiniMax/Qwen/Kimi/Deepseek/GLM/Gemma/Nemotron/poolside/OpenAI's open-weight `gpt-oss-20b` — prior art to preserve, not replace. It does not cover Anthropic Claude, OpenAI GPT, or Google Gemini prompt phrasing tuned to those providers' own official guides — see Clarifications in `original-requirements.md`.
- Persona YAML is a superset of a registry agent — binds `provider` + `model` (required) plus optional `persona`/`role`/`language` fields, validated against the existing registry schema (`docs/personas-authoring.md`).
- Prompt templates must mirror the canonical section structure (Role/Focus/Scope/Severity Rubric/Output Format/Payload) and render every required template variable (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`) with no leftovers.
- Each new persona needs a passing fixture: a synthetic `.patch` in `personas/testdata/`, named `<slug>_fixture.patch`, whose target category word appears in the prompt template itself.
- Discovery/install already work end-to-end via `ATCR_PERSONAS_URL` (default `https://raw.githubusercontent.com/atcr/personas/main`) and `atcr personas search`/`install` — no code change needed in this repo.
- The only in-repo change is documentation: `docs/personas-install.md` and `README.md`'s Quickstart should recommend installing the new pack.

## Implementation Strategy
Because the persona content (Tasks 1-2) is authored and published in the separate `atcr/personas` git repository, this plan's own TDD/sprint execution is scoped to the single in-repo task: updating `docs/personas-install.md` and `README.md`'s Quickstart section to recommend the new default persona pack. The cross-repo authoring and publishing work is tracked as an external dependency this plan's Definition of Done references but cannot verify directly — it is confirmed complete when the personas are discoverable via `atcr personas search` and installable via `atcr personas install <name>` against the live community repo.

## Recommended Packages
No high-ROI packages identified — this plan's in-repo scope is documentation-only.

## User Story Themes

### Theme 1 — Author Model-Tuned Persona Content (external, tracked)
Write 3 persona YAML + prompt templates (one each for Anthropic, OpenAI, Google) in the `atcr/personas` repo, following `docs/personas-authoring.md`'s schema and template contract, each phrased per that provider's own official prompting guide with a flagship-primary + same-family-fallback model pair.

### Theme 2 — Publish via Community Persona Channel (external, tracked)
Add the new personas to `atcr/personas`'s `index.json` so `atcr personas search`/`install` can discover and install them — no new hosting or schema.

### Theme 3 — Recommend Default Persona Pack in Documentation (in-repo)
Update `docs/personas-install.md` and the README quickstart to recommend installing the new persona pack as part of first-time setup.

## Planning Success Criteria
- 3 new persona YAMLs exist in the `atcr/personas` community repo — one each for Anthropic, OpenAI, and Google — each bound to a flagship-primary + same-family-fallback model pair from that provider, with prompt phrasing tuned to that provider's official prompting guide.
- Each new persona has a passing fixture per the existing contribution checklist (`docs/personas-authoring.md`).
- The personas are discoverable via `atcr personas search` and installable via `atcr personas install <name>`.
- `docs/personas-install.md` and the README quickstart recommend installing these personas as part of first-time setup.

## Risk Mitigation
- **Risk:** Tasks 1-2 land in an external repo this plan's TDD loop cannot execute or verify. **Mitigation:** scope this plan's own Definition of Done strictly to the in-repo doc change (Theme 3); treat external publication as a tracked, externally-verified dependency.
- **Risk:** Prompt phrasing claims to follow a model's "official prompting guide" but drifts from it over time as providers update guidance. **Mitigation:** cite the specific guide version/date in each new persona's authoring notes in the external repo (not enforced by this plan).

## Next Steps
1. `/find-documentation @.planning/plans/active/19.6_community_registry_hub/`
2. `/create-documentation @.planning/plans/active/19.6_community_registry_hub/`
3. `/create-user-stories @.planning/plans/active/19.6_community_registry_hub/`
4. `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`
5. `/design-sprint @.planning/plans/active/19.6_community_registry_hub/`
6. `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`
