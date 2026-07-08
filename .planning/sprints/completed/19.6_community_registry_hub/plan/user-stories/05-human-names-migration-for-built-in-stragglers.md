# User Story 5: Human-Names Migration for Built-in Stragglers

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** persona ecosystem maintainer rolling out the community-canonical, model-indexed registry
**I want** the three role-named built-in personas (`sentinel`, `tracer`, `idiomatic`) renamed to human first names (`sasha`, `penny`, `ingrid`) with `ingrid`'s clean-code lens generalized beyond Go, landing in the same release as the new library
**So that** no active persona — built-in or community — ever carries a role-based slug, and Epic 23.0's rename work is not duplicated in a separate, later effort

## Story Context

- **Background:** `personas/personas.go` defines the canonical built-in `names` slice as `{"bruce", "greta", "kai", "mira", "dax", "sentinel", "tracer", "idiomatic", "otto"}` and panics at `init()` if the embedded `.md` files under `personas/` don't exactly match that slice plus `_base.md`. `sentinel.md` carries the security/OWASP lens, `tracer.md` carries the performance/N+1/latency lens, and `idiomatic.md` carries a Go-specific clean-code lens. Each has a matching fixture under `personas/testdata/<name>_fixture.patch`. These three are the last role-based names in the active persona set; every other built-in (`bruce`, `greta`, `kai`, `mira`, `dax`, `otto`) already uses a human first name, and the new community library being introduced by this plan is human-named by convention from the start.
- **Assumptions:** The persona's underlying review lens (security-focused, performance-focused, idiomatic/clean-code-focused) does not change — only the slug, prompt template filename, fixture filename, and (for `ingrid`) the scope of the prompt's language guidance change. `docs/personas-authoring.md` and `docs/personas-install.md` are the two documentation files with worked examples that reference the old names and need updating. Per **Clarification C2** (original-requirements.md), the migrated stragglers remain the **embedded, model-agnostic subset** of the unified persona unit — they name no model, so they stay compiled-in as the offline base and resolve through the single `ResolvePersona` precedence chain, not as a divergent format. Updating `personas/personas.go`'s `names` slice and embedded file set is therefore the path; this is **no longer an open implementation-time decision**. `/design-sprint` sizes only whether the built-in `.md` files are reformatted into the unified unit now or as a bounded fast-follow.
- **Constraints:** This story must not create a mixed-naming state: `sentinel`/`tracer`/`idiomatic` and `sasha`/`penny`/`ingrid` must not coexist in the active set at the end of the story — the old slugs are fully retired, not aliased or deprecated-but-present. If the personas stay built-in, `personas/personas.go`'s init-time panic guard (embedded-file-count/name match) means a partial rename (e.g., template renamed but `names` slice not updated) fails fast at binary startup rather than silently degrading, so the four changes (template, fixture, YAML/index entry, `names` slice) must land atomically. This story explicitly folds in Epic 23.0's scope so that epic does not need a separate implementation once this plan ships.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 2 (structured `provider`/`model` metadata schema) if the migrated personas are authored with structured metadata in `personas/community/index.json`; otherwise None |

## Success Criteria (SMART Format)

- **Specific:** `sentinel` → `sasha`, `tracer` → `penny`, and `idiomatic` → `ingrid` are each fully renamed — prompt template, fixture, YAML metadata, and registration (either `personas/personas.go`'s `names` slice or `personas/community/index.json`) — with `ingrid`'s prompt rewritten to review idiomatic/clean-code style generally rather than Go-specifically, and no reference to `sentinel`, `tracer`, or `idiomatic` as an active persona slug remains anywhere in the repository (code, docs, fixtures, or index).
- **Measurable:** `atcr personas test sasha`, `atcr personas test penny`, and `atcr personas test ingrid` each pass against their renamed fixtures; a repo-wide search for the three retired slugs used as persona identifiers (excluding Go's unrelated `sentinel`-error-value idiom found elsewhere in the codebase) returns zero matches in active persona registration paths, `docs/personas-authoring.md`, and `docs/personas-install.md`.
- **Achievable:** Confined to three personas' worth of file renames plus one prompt content rewrite (`ingrid`'s Go-specific language generalized) and the associated registration/doc updates — no new runtime mechanism, no changes to the resolution chain, CLI commands, or fixture-testing infrastructure itself.
- **Relevant:** Directly satisfies AC4 (no role-based names remain in the active set) and prevents the alternative outcome of Epic 23.0 re-implementing this same rename later against a codebase that has already introduced human-named community personas, which would otherwise produce a window of mixed naming.
- **Time-bound:** Completed within this plan's implementation phase, landing in the same release as the new model-indexed community library so there is no intermediate release with mixed role-based and human-named active personas.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-atomic-rename-sentinel-tracer-idiomatic.md) | Atomic Rename of `sentinel`/`tracer`/`idiomatic` to `sasha`/`penny`/`ingrid` | Unit |
| [05-02](../acceptance-criteria/05-02-ingrid-generalized-idiomatic-lens.md) | `ingrid` — Generalizing `idiomatic`'s Go-Specific Lens | Unit |
| [05-03](../acceptance-criteria/05-03-retired-slug-verification.md) | Retired-Slug Verification (No Remaining `sentinel`/`tracer`/`idiomatic` References) | Integration |
| [05-04](../acceptance-criteria/05-04-documentation-updates.md) | Documentation Updates for the Renamed Personas | Manual |

## Original Criteria Overview

1. `sentinel`, `tracer`, and `idiomatic` are each renamed to `sasha`, `penny`, and `ingrid` respectively across prompt template, fixture, YAML metadata, and registration, with the old slugs fully retired (not aliased).
2. `ingrid`'s prompt content is rewritten so its clean-code/idiomatic-style lens applies generally across languages rather than being Go-specific, while preserving the persona's original review intent.
3. `atcr personas test <new-name>` passes for all three renamed personas, and no code, fixture, index, or documentation reference to the retired role-based slugs remains in the active persona set.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`_

## Technical Considerations

- **Implementation Notes:** For each straggler: rename `personas/<old>.md` → `personas/<new>.md` (or move to `personas/community/<new>.md` if adopting the community-only path), updating any `{{.AgentName}}`-style self-references inside the template; rename `personas/testdata/<old>_fixture.patch` → `personas/testdata/<new>_fixture.patch` (or to `personas/community/testdata/<new>_fixture.patch` if moving to community-only), verifying the fixture's target category still triggers the persona's lens; author/update the persona's YAML metadata with the new slug plus `provider`/`model` structured fields per Story 2's schema if publishing through the community index; update `personas/personas.go`'s `names` slice (if staying built-in) so the init-time embedded-file guard matches, or remove the slug from it and add the persona to `personas/community/index.json` (if moving to community-only). `ingrid`'s prompt rewrite should generalize phrasing like "idiomatic Go" to "idiomatic style for the language under review" without losing the specificity that makes the lens useful. If the community-only path is chosen, coordinate with Story 1/Story 4 so the Markdown prompt is delivered to a directory on `ResolvePersona`'s chain (`~/.config/atcr/personas/` or `.atcr/personas/`).
- **Integration Points:** `personas/personas.go` init-time panic guard (embedded `.md` file count/name match against `names`); `personas/community/index.json` if the community-only path is chosen; `docs/personas-authoring.md` and `docs/personas-install.md` worked examples; the `atcr personas test <name>` fixture-verification command used to confirm each rename.
- **Data Requirements:** No new data shapes — this is a rename plus one content rewrite. If personas move to the community index, their YAML entries should carry `provider`/`model` per Story 2's `PersonaIndexEntry` schema rather than being added with only `name`/`version`/`description`/`path`, so the migration doesn't immediately create index entries that are already missing the new plan's structured metadata.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Partial rename (e.g., template and fixture renamed but `personas/personas.go`'s `names` slice left stale) trips the init-time panic and breaks every `atcr` invocation, not just the affected persona | High | Land each persona's four-part rename (template, fixture, metadata, registration) as a single atomic change; run `atcr personas test <new-name>` and a full build/init smoke check before considering a persona's migration complete |
| A repo-wide search for the retired slugs produces false positives from Go's unrelated `sentinel`-error-value naming idiom used elsewhere in the codebase (e.g., `internal/verify/skeptic.go`, `internal/registry/attribution.go`), causing the verification step to either miss real persona references or waste effort chasing unrelated code comments | Low | Scope the "no remaining references" check to persona-specific paths (`personas/`, `docs/personas-*.md`, `personas/community/index.json`) rather than a blind repo-wide grep for the bare word "sentinel" |
| Generalizing `ingrid`'s prompt beyond Go accidentally dilutes the lens into generic advice that no longer catches the specific idiom violations the original `idiomatic` persona was tuned for | Medium | Preserve the fixture's target category and verify `atcr personas test ingrid` still passes after the rewrite; keep concrete, language-adaptable examples in the prompt rather than removing specificity outright |
| This story's rename lands out of step with Epic 23.0's own tracked scope, causing duplicate or conflicting work if Epic 23.0 is executed independently before or after this plan | Medium | Explicitly document (per plan.md Theme 5 and documentation/human-names-migration.md) that this story folds in and satisfies Epic 23.0's rename scope, so Epic 23.0 is closed as superseded rather than re-implemented |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
