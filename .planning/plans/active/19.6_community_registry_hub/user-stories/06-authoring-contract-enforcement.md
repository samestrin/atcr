# User Story 6: Authoring Contract Enforcement for Model Metadata and Human Names

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** persona contributor writing a new community persona against `docs/personas-authoring.md`
**I want** the authoring contract to spell out that my persona's bound model must appear in structured metadata and that my persona should carry an all-human name, with a fixture test that actually asserts the metadata rule
**So that** I know exactly what "done" looks like before I open a pull request, and a violation is caught by `go test ./...` instead of by a reviewer's eyeball in code review

## Story Context

- **Background:** `docs/personas-authoring.md` already requires `provider` and `model` as strictly-validated required fields on every persona YAML, and already documents a fixture-per-persona requirement ("a small diff with a known problem the persona must flag, run in CI with no network"). `internal/personas/test.go`'s `TemplateFixtureRunner.RunFixture` currently only resolves fixtures for built-in personas (`isBuiltin(name)`) — for any community persona it short-circuits to `FixtureOutcome{HasFixture: false}` without inspecting metadata at all. This story closes two documentation/enforcement gaps left open by the rest of the plan: (1) the fixture test does not yet assert that the bound model appears in a persona's structured metadata, and (2) the all-human-names convention (established elsewhere in this plan for the shipped library) is not yet written down as a rule for future contributors.
- **Assumptions:** The persona YAML schema (per `documentation/persona-yaml-schema.md`) already carries `provider` and `model` as required, strictly-decoded fields, so "bound model in structured metadata" means asserting `model` is present and non-empty on the persona's parsed metadata — no new schema field is being introduced. The all-human-names convention itself (which names are acceptable, how they're chosen) is defined by earlier stories in this plan (per `documentation/human-names-migration.md`); this story only documents it as a forward-looking rule and does not re-litigate naming choices already made. Community personas gain enough fixture support elsewhere in this plan (or already carry the metadata needed) that extending `TemplateFixtureRunner` to check community personas' bound-model metadata is feasible without a full LLM-backed fixture run.
- **Constraints:** Must not weaken or remove the existing fixture pass/fail semantics for built-in personas (`isBuiltin` path in `internal/personas/test.go` stays intact). The new assertion must be enforceable by `go test ./...` with no network access, consistent with the existing "runs in CI with no network" fixture principle. Documentation changes must be additive to `docs/personas-authoring.md`, not a rewrite of the existing required-fields section. This AC8 documentation requirement is shared with Epic 23.0's AC5 — the human-names section should be written so it satisfies both without contradiction or duplication of intent.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Depends on the community-persona-library and fetch-from-repo stories earlier in this plan (personas whose bound model and human name the fixture test will assert against must already exist) |

## Success Criteria (SMART Format)

- **Specific:** `docs/personas-authoring.md` gains a section documenting (a) that a persona's bound model must be discoverable in structured metadata and is asserted by the fixture test, and (b) the all-human-names convention as a forward-looking rule for new persona names; `internal/personas/test.go`'s fixture path asserts the bound model is present in a persona's parsed metadata for every persona the runner resolves.
- **Measurable:** `go test ./...` passes with the extended fixture assertion in place; a persona YAML missing or blanking its `model` field causes the fixture test to fail with a clear, attributable error.
- **Achievable:** Builds incrementally on the existing strictly-validated `provider`/`model` required fields and the existing `TemplateFixtureRunner` structure — no new schema fields, no new file formats.
- **Relevant:** Directly satisfies AC7 (fixture-enforced model-in-metadata convention) and AC8 (documented forward-looking human-names rule), making both conventions self-enforcing for contributors who never read this plan's design history.
- **Time-bound:** Completed within this sprint, as the final theme gating the plan's authoring contract before the community channel goes canonical.

## Acceptance Criteria Overview

1. `docs/personas-authoring.md` documents the model-in-structured-metadata convention and cross-references where the fixture test enforces it.
2. `docs/personas-authoring.md` documents the all-human-names convention as a forward-looking rule for new persona contributions (consistent with Epic 23.0 AC5's phrasing/intent).
3. The fixture test (`internal/personas/test.go` or its test suite) asserts the bound model appears in structured metadata for every persona under test, and `go test ./...` passes with this assertion active.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`_

## Technical Considerations

- **Implementation Notes:** Extend the fixture assertion path so it validates `model` non-empty on the persona's already-parsed struct (the strict decode already guarantees the field exists at load time; the fixture test's job is to assert it is asserted/visible, not to re-implement schema validation). Keep the built-in `isBuiltin(name)` branch in `TemplateFixtureRunner.RunFixture` untouched; add the model-metadata assertion either as a shared pre-check both branches call, or as a new lightweight check that runs for community personas where `HasFixture` currently short-circuits to `false`.
- **Integration Points:** `internal/personas/test.go` (`TemplateFixtureRunner`, `FixtureOutcome`, `TestPersona`), `docs/personas-authoring.md` (required-fields section and a new "Conventions" or "Contribution Checklist" section), `documentation/persona-yaml-schema.md` and `documentation/human-names-migration.md` as grounding references (do not restate their content verbatim — link/summarize).
- **Data Requirements:** No new data structures. Reuses the existing persona YAML struct's `provider`/`model` fields already enforced by strict decoding per `documentation/persona-yaml-schema.md`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Extending `TemplateFixtureRunner` for community personas accidentally weakens the existing built-in fixture pass/fail contract | High | Keep the `isBuiltin(name)` branch fully separate; add community-specific assertion as an additive path, and run existing built-in fixture tests unchanged to confirm no regression |
| AC8's human-names documentation drifts from or duplicates Epic 23.0 AC5's wording, creating two slightly different "sources of truth" | Medium | Write the section once, phrase it as the shared forward-looking rule, and cross-reference Epic 23.0 rather than restating divergent rationale |
| "Bound model in structured metadata" is interpreted as requiring a new schema field instead of asserting the existing `model` field, causing scope creep | Medium | Ground implementation strictly in the existing `provider`/`model` required fields already validated by strict decoding; no new YAML keys introduced |
| Fixture assertion has no network/LLM dependency today but a naive implementation could try to call out to verify the model id against a live provider | Low | Keep the check purely structural (field present/non-empty on parsed metadata), consistent with the existing no-network fixture principle |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
