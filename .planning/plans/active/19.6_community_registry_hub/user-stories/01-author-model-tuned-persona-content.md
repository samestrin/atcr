# User Story 1: Author Model-Tuned Persona Content

**Plan:** [19.6: Default Model-Tuned Community Personas](../plan.md)

## User Story

**As a** community persona contributor maintaining the `atcr/personas` repo
**I want** to author 1-2 new persona YAML files with prompt templates phrased per each target model's official prompting guide (e.g. a Claude-tuned persona following Anthropic's prompting guidelines, a GPT-4-tuned persona following OpenAI's prompting guidelines)
**So that** new atcr users get access to well-tuned, model-specific reviewer personas instead of generic domain personas that ignore model-specific prompting techniques

## Story Context

- **Background:** Epic 9.0 (Persona Ecosystem, completed) already built the full community-persona distribution mechanism in this codebase (`atcr personas install/search/list/upgrade/remove`), which fetches persona YAML and an `index.json` from a configurable URL (`ATCR_PERSONAS_URL`, default a public GitHub repo). That mechanism has no default, model-tuned content to distribute yet — this story fills that content gap by authoring the persona source files themselves.
- **Assumptions:** The `atcr/personas` external repo already exists, is writable by the contributor, and follows the schema and contribution checklist documented in this repo's `docs/personas-authoring.md`. The contributor has access to (or working knowledge of) the target models' official prompting guides (Anthropic's Claude prompting guidelines, OpenAI's GPT-4 prompting guidelines) at authoring time.
- **Constraints:** **This story's entire implementation happens in the external `atcr/personas` GitHub repository, not in this codebase (atcr).** This plan's own TDD/sprint execution loop does not run against, build, or test the `atcr/personas` repo — it has no branch, commit, or CI surface in this codebase to execute against. This plan's Definition of Done cannot directly verify this story's YAML/prompt content or fixture; it can only confirm the work landed by checking the resulting artifacts are discoverable from this codebase's tooling once published (Story 2/Theme 2), not by running this story's own tests.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Epic 9.0 (Persona Ecosystem) — provides the persona YAML schema and install mechanism this content targets; external write access to the `atcr/personas` repo |

## Success Criteria (SMART Format)

- **Specific:** At least 1-2 new persona YAML files exist in the `atcr/personas` repo (e.g. `personas/claude-reviewer.yaml`, `personas/gpt4-reviewer.yaml`), each with `provider` and `model` set to a specific target, and a prompt template phrased per that model's official prompting guide.
- **Measurable:** Each new persona YAML validates against the existing registry agent schema; each has exactly one corresponding fixture file in `personas/testdata/<slug>_fixture.patch` (mode 0644) that passes the existing persona test harness (`atcr personas test` or equivalent, per `docs/personas-authoring.md`).
- **Achievable:** No new schema, command, or distribution mechanism is required — this is pure content authored against an already-shipped contract.
- **Relevant:** Directly closes the content gap blocking Epic 19.6's goal of a single-command, model-tuned reviewer panel for new users.
- **Time-bound:** Content authored and fixture passing before Story 2 (publishing to `index.json`) begins, since Story 2 depends on these files existing.

## Acceptance Criteria Overview

1. Each new persona YAML sets `provider` and `model` to a specific, real target (not a placeholder), and its prompt template's phrasing follows that model's official prompting guide's documented structure/techniques.
2. Each prompt template mirrors the canonical section structure (`## Role`, `## Focus`, `## Scope` with `{{.ScopeRule}}`, `## Severity Rubric`, `## Output Format` with the exact 7-column pipe-delimited contract, `## Payload`) and renders every required template variable (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`) with no leftover unrendered variables, and the persona's target category word appears in the prompt template itself.
3. Each new persona has a passing fixture: a synthetic (never real) `.patch`/`.diff` file in `personas/testdata/`, named `<slug>_fixture.patch`, containing a synthetic instance of the target category, verified per the existing contribution checklist in `docs/personas-authoring.md`.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`_

## Technical Considerations

- **Implementation Notes:** Author each persona YAML as a superset of a registry agent per `docs/personas-authoring.md` — `provider` and `model` are required fields; `persona`, `role`, `language` are optional. Base the prompt template on the canonical structure already used by existing personas in `atcr/personas`, adapting phrasing (not structure) to match each target model's official prompting guide (e.g. XML-tag-oriented structuring and explicit role framing for Claude per Anthropic's guidance; system/developer-message conventions and explicit step-by-step instruction framing for GPT-4 per OpenAI's guidance).
- **Integration Points:** None in this codebase — the only integration point is the external `atcr/personas` repo's existing schema/fixture contract (`docs/personas-authoring.md`, cited in `codebase-discovery.json`). Story 2 (publishing to `index.json`) and Story 3 (in-repo docs) both depend on the files this story produces existing in that repo.
- **Data Requirements:** No data model changes. Persona YAML must conform to the existing registry agent schema (provider/model/persona/role/language fields) already validated by the shipped install/search mechanism.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| This story's work cannot be executed, tested, or verified by this plan's own sprint/TDD loop, since it lives entirely in an external repo | High | Scope this plan's Definition of Done to confirm completion only via external observation (e.g. `atcr personas search` against the live repo once Story 2 publishes it) rather than in-repo test execution; treat this story as externally tracked, not internally gated |
| Prompt phrasing claims to follow a model's "official prompting guide" but drifts from actual guidance or misinterprets it | Medium | Cite the specific guide version/date used in each persona's authoring notes in the external repo (not enforced by this plan's tooling) |
| Fixture is accidentally authored with a real (non-synthetic) instance of the target category, creating a content/compliance issue in the public community repo | Medium | Follow the existing contribution checklist in `docs/personas-authoring.md` requiring synthetic fixtures; have the fixture reviewed before merge in the external repo |
| Persona YAML fails schema validation or leaves template variables unrendered, breaking `atcr personas install` for end users | Medium | Run the existing persona test harness against each new YAML/fixture pair before publishing, per the authoring checklist |

---

**Created:** July 06, 2026
**Status:** Draft - Awaiting Acceptance Criteria
