# User Story 3: Model-Aware Search and Discovery via `--model`/`--provider`

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** new atcr user who already holds an API key for a specific model (e.g. DeepSeek)
**I want** to run `atcr personas search deepseek` or `atcr personas search --model deepseek` and get back the persona(s) bound to that model
**So that** I can go from "I have model X" to "installed persona tuned for X" without reading free-text descriptions or guessing persona names

## Story Context

- **Background:** Epic 9.0 already built the full community-persona distribution mechanism — `cmd/atcr/personas.go`'s `newPersonasCmd` registers six subcommands (install/list/search/remove/test/upgrade), each a thin `RunE` delegating to `internal/personas`. `newPersonasSearchCmd` (personas.go:218) currently calls `commpersonas.Search(personasClient, commpersonas.BaseURL(), keyword)`, and `internal/personas/search.go`'s `Search()` does case-insensitive substring matching only against `PersonaIndexEntry.Name` and `PersonaIndexEntry.Description` — there is no `Provider`/`Model` field on the struct today, so any current "model match" is an accident of free text appearing in a description, not a structured guarantee. This story depends on Theme 2 (Story 2) having added `Provider`/`Model` fields to `PersonaIndexEntry` and to the generated `index.json`; this story is the CLI-facing consumer of that schema — it must match against the structured fields, never fall back to scanning free text for a model name.
- **Assumptions:** `PersonaIndexEntry` (or the type `Search()` returns) carries populated `Provider` and `Model` fields by the time this story's code runs, per Story 2's Theme 2 output. The existing positional-keyword form of `atcr personas search <keyword>` must keep working unchanged (backward compatibility for existing users/scripts/docs). `renderPersonaSearch` is free to grow columns since it is presentation-only and has no on-disk/serialized format to stay compatible with.
- **Constraints:** Must follow the exact Cobra flag-registration pattern already used in this codebase — `cmd.Flags().String(...)`/`StringVar(...)` declared inside the `newPersonasSearchCmd` constructor and read inside `RunE`, matching the `--scores` boolean flag pattern already on `newPersonasListCmd` (per `documentation/cli-search-flags.md`). Matching must be against structured `Provider`/`Model` fields only — a model name appearing incidentally inside a `Description` string must not count as a match when `--model` is used. No new third-party dependency; stdlib + existing `spf13/cobra` only.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 2 (Structured Model Metadata Schema — `Provider`/`Model` fields on `PersonaIndexEntry` and in generated `index.json`) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas search --model <name>` and `atcr personas search --provider <name>` filter results using only the structured `Provider`/`Model` fields on each `PersonaIndexEntry`, and `atcr personas search <keyword>` continues to work exactly as before for existing callers.
- **Measurable:** A persona whose `Model` field equals (case-insensitively, substring-tolerant) the `--model` value is returned; a persona whose `Model` differs but whose `Description` happens to mention the same model string is NOT returned when `--model` is used — the distinction is directly assertable in a table-driven test against a mock `index.json`.
- **Achievable:** Confined to `internal/personas/search.go` (new filter function/parameters) and `cmd/atcr/personas.go` (`newPersonasSearchCmd` flag registration + `renderPersonaSearch` column additions) — no changes to fetch/install/upgrade code paths.
- **Relevant:** This is the literal mechanism behind the plan's headline flow — "I have DeepSeek → find the DeepSeek persona → install it" — and is explicitly named in AC2 and AC6.
- **Time-bound:** Implementable and independently testable within a single sprint phase alongside Story 2, since both touch the same `PersonaIndexEntry`/`Search()` surface.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-structured-model-provider-filtering.md) | Structured `--model`/`--provider` Filtering (No Free-Text Fallback) | Unit + Integration |
| [03-02](../acceptance-criteria/03-02-keyword-search-backward-compatibility.md) | Positional Keyword Search Remains Unchanged (Backward Compatibility) | Unit + Integration |
| [03-03](../acceptance-criteria/03-03-flag-registration-and-arg-validation.md) | `--model`/`--provider` Flag Registration and Argument Validation Guard | Integration |

## Original Criteria Overview

1. `atcr personas search --model <name>` and `atcr personas search --provider <name>` filter community persona results using only structured `Provider`/`Model` metadata (never a substring match inside free-text `Description`), and can be combined with each other and with the existing positional `<keyword>` argument.
2. The existing `atcr personas search <keyword>` behavior (substring match on Name/Description) is unchanged for callers that pass no new flags — full backward compatibility.
3. `renderPersonaSearch`'s table output includes the persona's `Provider`/`Model` so a user can visually confirm which model a returned persona targets before installing it.

## Technical Considerations

- **Implementation Notes:** Extend `internal/personas/search.go`'s `Search()` (or add a sibling function/options struct, e.g. `SearchOptions{Keyword, Model, Provider string}`) so structured-field filtering is applied against the `Provider`/`Model` fields Story 2 adds to `PersonaIndexEntry` — keeping the existing keyword/description substring path intact as an independent, combinable filter rather than replacing it. Add `--model`/`--provider` as `cmd.Flags().String(...)` on `newPersonasSearchCmd` (personas.go:218), following the same registration style as `--scores` on `newPersonasListCmd`; decide in `RunE` whether the positional keyword arg becomes optional when `--model`/`--provider` are supplied (`cobra.ExactArgs(1)` currently requires it — this likely needs to relax to `cobra.MaximumNArgs(1)` or similar, with a usage error if neither a keyword nor a model/provider flag is present).
- **Integration Points:** `internal/personas/search.go` (`Search`/`PersonaIndexEntry`), `cmd/atcr/personas.go` (`newPersonasSearchCmd`, `renderPersonaSearch`, lines ~218-239 and ~417). Consumes the schema Story 2 produces; is itself consumed by Story 4/6 (persona library authoring + end-to-end mock-registry flow in AC6).
- **Data Requirements:** Relies on `index.json` entries carrying populated `provider`/`model` JSON fields (Story 2's responsibility). No new persistent data — this story is read/filter-only against data already fetched via `FetchIndex`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Relaxing `Args: cobra.ExactArgs(1)` to allow flag-only invocation could silently accept a no-argument, no-flag call and return an unhelpful empty result | Medium | Add an explicit `RunE` guard returning a `usageError` when keyword is empty AND both `--model`/`--provider` are unset, mirroring the existing empty-keyword check at personas.go:225-227 |
| Story 2 lands late or with a different field/type shape than assumed here, breaking this story's filter implementation | Medium | Confirm `PersonaIndexEntry.Provider`/`Model` field names and types with Story 2 before implementation; both stories are in the same sprint phase per plan.md's Implementation Strategy, so sequencing/interface can be settled during `/design-sprint` |
| A model name substring accidentally also matches an unrelated persona's structured `Model` field (e.g. "gpt-4" matching "gpt-4o") produces a false-positive install recommendation | Low | Table-driven tests exercise near-miss model strings explicitly; document the matching semantics (substring vs. exact) precisely in code comments and AC so behavior is deliberate, not incidental |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
