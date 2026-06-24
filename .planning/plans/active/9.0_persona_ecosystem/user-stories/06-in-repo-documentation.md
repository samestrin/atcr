# User Story 6: In-Repo Documentation for Persona Installation and Authoring

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** developer or contributor working with ATCR personas
**I want** clear in-repo documentation covering how to install, list, upgrade, and remove personas (`docs/personas-install.md`), how to author and contribute new personas (`docs/personas-authoring.md`), and how the `language` field works in `docs/registry.md`
**So that** I can adopt, configure, and extend personas without reading source code, and the barrier to both vertical-market adoption and community contribution is removed

## Story Context

- **Background:** The bonus personas (T1), `atcr personas` CLI (T2), domain bundles (T5), corroboration scores (T6), and language-aware skeptic routing (T8) are all new surface area. Without documentation, a new user must reverse-engineer CLI flags from `--help` output and infer the `language` field semantics from registry YAML examples. Existing docs do not cover persona lifecycle or the new `AgentConfig.Language` field.
- **Assumptions:** `docs/personas-install.md` and `docs/personas-authoring.md` are new files. `docs/registry.md` already exists and documents the registry schema; it needs an addendum for the `language` field and routing behavior. `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` are the current canonical example files (the deprecated `docs/examples/registry.yaml` path no longer exists and must not be referenced).
- **Constraints:** Documentation must match the implemented behavior exactly — no speculative flags or fields. The authoring guide must specify the canonical `language` format (no leading dot, lowercased, e.g. `["go", "ts"]`), fixture requirements, and the prompt structure contract. The install guide must cover the `~/.config/atcr/personas/` install path and the configurable registry base URL. Community-repo contribution guide and CI workflow are explicitly out of scope for this story.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (T1 bonus personas), User Story 2 (T2 atcr personas CLI), User Story 3 (T8 language-aware routing), User Story 4 (T5 domain bundles) |

## Success Criteria (SMART Format)

- **Specific:** `docs/personas-install.md` documents all six `atcr personas` subcommands (`install`, `remove`, `list`, `search`, `test`, `upgrade`) and bundle installation (`atcr personas install bundle/<name>`); `docs/personas-authoring.md` provides a complete persona authoring template covering prompt structure, fixture requirements, and the `language` scope field; `docs/registry.md` is updated with the `language` field definition and language-aware skeptic routing behavior; both example registry files include at least one `language` field example.
- **Measurable:** A developer unfamiliar with the codebase can install a community persona, list installed personas, and run its fixture test using only `docs/personas-install.md` — verified by a walkthrough with no source-code lookups. An author can produce a valid, CI-passing persona YAML + fixture using only `docs/personas-authoring.md` as a reference.
- **Achievable:** All documented behavior is implemented by dependent stories (T1, T2, T5, T8) before this story's documentation is finalized; no speculative content is written.
- **Relevant:** Vertical-market adoption hinges on teams being able to install and configure personas without engineering support; contributor growth requires a clear authoring contract.
- **Time-bound:** Documentation is complete and merged in the same Sprint B that delivers T2, T5, T6, and T7-in-repo, before the 9.0 release cut.

## Acceptance Criteria Overview

1. `docs/personas-install.md` covers install, list, search, test, upgrade, remove, and bundle installation with working command examples and the `~/.config/atcr/personas/` install path.
2. `docs/personas-authoring.md` provides a fill-in-the-blank persona template with all required fields, canonical `language` format rules, fixture file requirements, and a step-by-step contribution checklist.
3. `docs/registry.md` gains a `language` field reference entry (type, canonical format, nil semantics, routing behavior) and at least one example showing how language-aware skeptic routing prefers matched skeptics with silent fallback.
4. `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` each include an optional `language` field example on at least one agent definition.
5. No documentation references the deprecated `docs/examples/registry.yaml` path.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Documentation is authored after T1, T2, T5, and T8 are implemented and green so that command flags, field names, and paths are confirmed correct. The `language` canonical format (no leading dot, lowercased) must match `applyDefaults` canonicalization logic exactly. Fixture file format (`.patch`/`.diff` in `personas/testdata/`) must be described accurately in the authoring guide.
- **Integration Points:** `docs/registry.md` update must cross-reference the `AgentConfig.Language` field as defined in the YAML schema; example files are consumed by integration tests and must remain valid YAML after the `language` field addition. The install guide's subcommand descriptions must stay in sync with Cobra `--help` output.
- **Data Requirements:** No new data models. The authoring template is a Markdown file with YAML front matter examples. Registry example files are standard YAML and must validate against the schema after edits.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Docs written before implementation is finalized, causing flag/path drift | Medium | Write documentation as the last task in Sprint B, after T2/T5/T8 are merged and green |
| Example registry YAML files become invalid after adding `language` examples | Medium | Run `go test ./...` (which exercises registry loading) after editing example files to confirm validity |
| Authoring guide omits a required fixture field, causing contributor CI failures | Medium | Cross-reference `TestPersonaFixture` test logic when drafting fixture requirements in the authoring guide |
| `docs/registry.md` routing behavior description diverges from `SelectEligibleSkeptics` implementation | Low | Copy the two-partition algorithm description directly from the plan's Technical Planning Notes and verify against the actual implementation diff |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
