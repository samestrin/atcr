# User Story 2: Fixture Authoring & Test-Gate Integration

**Plan:** [29.0: Anti-Slop Persona (Simon) & Content Marketing](../plan.md)

## User Story

**As a** community persona maintainer verifying the new `simon` anti-slop reviewer
**I want** a synthetic slop-bloat fixture plus `simon`'s registration in both the `communityPersonas` Go roster and `personas/community/index.json`
**So that** the project's existing fixture/differentiation/distinct-category/index-registration test suite actually exercises `simon` end to end, proving the persona detects AI-generated code bloat rather than merely existing on disk

## Story Context

- **Background:** The community persona library (`personas/community/`) is validated by a hand-maintained Go roster (`communityPersonas` in `personas/community_test.go:117`) plus an `index.json` manifest — both of which drive the fixture, differentiation, distinct-category, and distinct-task-scoping test gates. A persona YAML/prompt existing on disk is invisible to those gates until it is added to both. The epic's three stated tasks (author persona, write fixture, write blog outline) omit this fourth integration point; codebase discovery flagged it as mandatory because without it AC3 ("a passing fixture test proves the persona successfully identifies slop") cannot be satisfied — no fixture test would ever run against `simon`. The inverse holds at landing time: because `CommunityNames()` lists the embedded directory directly, Story 1's `simon.md` on disk already turns `TestCommunityAccessors`' `require.Len` (14 names vs 13 roster rows) and `TestTemplateFixtureRunner_CommunityPersonasPass` red — this story's roster row, `index.json` entry, and fixture are what bring the suite back to green. Stories 1 and 2 must merge to the default branch as a single unit; neither lands green on its own.
- **Assumptions:**
  - Story 1 has already produced `personas/community/simon.yaml` and `personas/community/simon.md` (the prompt template), including a `Category` word embedded verbatim in `simon.md`.
  - The roster + index.json registration is explicitly extended scope beyond the epic's three stated tasks, added here because codebase discovery identified it as the mandatory wiring that makes the fixture test meaningful; it is called out as such rather than silently folded into the epic narrative.
  - `simon`'s bound model id is already decided in Story 1's `simon.yaml`; this story only needs a case-insensitive substring of it for `VendorToken`.
- **Constraints:**
  - Fixture path is `personas/community/testdata/simon_fixture.patch` — the community location, not `personas/testdata/` (built-in-only).
  - `communityPersonas` roster entry must use a `Category` value not already claimed: coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant are taken; must also match the word embedded in `simon.md` from Story 1.
  - `index.json` entry's `tasks[0]` must be a fresh primary task tag not among the 13 already-claimed values (architecture-review, correctness-review, api-review, validation-review, concurrency-review, resource-review, performance-review, type-safety-review, dependency-review, observability-review, secrets-review, duplication-review, invariant-review).
  - `TestCommunityAccessors` (`personas/community_test.go:175`) uses `require.Len` on `CommunityNames()` vs `communityPersonas` — a missing or extra roster row fails the whole suite fatally, not just the per-persona case.
  - Two independent test layers must both pass: roster-driven gates in `personas/community_test.go`, and embedded-set gates that read the filesystem directly with no roster dependency (`internal/personas/community_fixture_test.go`, `test_test.go`, `community_schema_test.go`, `internal/registry/persona_test.go:305`, `internal/personas/search_test.go`'s `verifyCommunityIndex`).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (Simon persona YAML + prompt template + category word authored in `simon.md`) |

## Success Criteria (SMART Format)

- **Specific:** `personas/community/testdata/simon_fixture.patch` exists as a synthetic unified diff planting a known instance of AI-slop (e.g. a pointless single-implementation interface plus a tautological "apologetic" comment), and `simon` is registered as a `communityPersona{Slug: "simon", VendorToken: ..., Category: ...}` row in `personas/community_test.go:117` and as a matching `PersonaIndexEntry` in `personas/community/index.json`.
- **Measurable:** `go test ./personas/... ./internal/personas/... ./internal/registry/...` passes with zero failures, and `len(CommunityNames())` equals `len(communityPersonas)` (14 after this addition) via the existing `require.Len` assertion.
- **Achievable:** Mirrors the exact pattern of the 13 existing community personas (e.g. `anthony`/`anthony_fixture.patch`) with no new test infrastructure required — only new data rows and one new fixture file.
- **Relevant:** Without this registration, `simon` would be discoverable at runtime but structurally invisible to every automated test gate that proves community personas work, which would silently defeat the epic's own fixture-test acceptance criterion.
- **Time-bound:** Completed within this story's execution window as the direct dependent step following Story 1's persona authoring, before the blog post story (Story 3) requires no code dependency but should not ship claiming a "tested" persona until this lands.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-fixture-patch-authoring.md) | Fixture Patch Authoring | Unit |
| [02-02](../acceptance-criteria/02-02-community-roster-registration.md) | Community Roster Registration (`communityPersonas`) | Unit |
| [02-03](../acceptance-criteria/02-03-index-registration-and-test-gate.md) | `index.json` Registration and Full Test-Gate Pass | Integration |

## Original Criteria Overview

1. `personas/community/testdata/simon_fixture.patch` is a valid unified diff sized to trigger `simon`'s slop-detection prompt without resembling legitimate business logic, following the format/naming convention of an existing fixture such as `anthony_fixture.patch`.
2. `personas/community_test.go`'s `communityPersonas` roster contains a `simon` row with a `VendorToken` that is a case-insensitive substring of `simon.yaml`'s bound model id, and a `Category` value that is both unclaimed by any other roster entry and verbatim-present in `simon.md`.
3. `personas/community/index.json` contains a `simon` entry whose name/path/provider/model/description exactly match `simon.yaml`, with non-empty `tasks`/`tags` and `tasks[0]` set to a fresh, unclaimed primary task tag (e.g. `bloat-review` or `slop-review`).
4. `go test ./personas/... ./internal/personas/... ./internal/registry/...` passes green, and `atcr personas test simon` succeeds as a manual no-LLM structural proof.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`_

## Technical Considerations

- **Implementation Notes:** Add the fixture patch file first, then the two registration entries together in a single change so `go test ./personas/...` cannot be run in a half-registered state (the `require.Len` fatal failure means partial registration is never a passable intermediate state). Model the fixture's diff shape directly on `anthony_fixture.patch` (`personas/community/testdata/anthony_fixture.patch`) but swap the planted violation from a layering/coupling issue to an anti-slop instance (unnecessary interface abstraction over a single implementation, or a tautological/apologetic AI comment such as `// This function handles the logic for processing`).
- **Integration Points:** `personas/community_test.go:117` (`communityPersonas` slice literal); `personas/community/index.json` (JSON array of `PersonaIndexEntry`); `personas/community/testdata/simon_fixture.patch` (new file). Downstream consumers that must also pass without direct edits: `internal/personas/community_fixture_test.go`, `internal/personas/test_test.go`, `internal/personas/community_schema_test.go`, `internal/registry/persona_test.go:305`, `internal/personas/search_test.go` (`verifyCommunityIndex`).
- **Data Requirements:** No runtime/database schema involved — this is static test-fixture and manifest data. The only "schema" constraint is structural: `PersonaIndexEntry` field parity with `simon.yaml`, and the `communityPersona` struct's three fields (`Slug`, `VendorToken`, `Category`).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Chosen `Category` word collides with an already-claimed value (coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant) or doesn't match what Story 1 embedded in `simon.md` | High | Cross-check against the full claimed-word list and `simon.md`'s literal text before writing the roster row; a mismatch fails the fixture/category contract test silently pointing at the wrong file. |
| `index.json` `tasks[0]` reuses one of the 13 claimed primary task tags, breaking distinct-task-scoping gates | Medium | Explicitly diff the new tag against the 13 listed values (architecture-review through invariant-review) before committing; prefer an unambiguous name like `bloat-review`. |
| Fixture diff is too subtle (persona doesn't flag it) or too on-the-nose (also trips on legitimate business logic in other fixtures), causing false pass/fail in `TestCommunityPersonas_FixtureAndPromptCategory` | Medium | Base the fixture directly on the `anthony_fixture.patch` structural pattern (small, single-purpose diff with one obvious planted violation and a comment naming the issue) rather than authoring from scratch. |
| Partial registration (roster entry added without matching index.json entry, or vice versa) leaves the test suite in a fatally broken state (`require.Len` failure) for other concurrent work on the shared branch | Medium | Land the fixture file and both registration entries as one atomic change set, and run `go test ./personas/... ./internal/personas/... ./internal/registry/...` locally before considering the story done. |

---

**Created:** July 16, 2026 09:15:34PM
**Status:** Acceptance Criteria Defined
