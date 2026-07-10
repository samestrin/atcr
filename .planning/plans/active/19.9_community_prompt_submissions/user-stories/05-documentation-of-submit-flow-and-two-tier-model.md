# User Story 5: Documentation of the Submit Flow and Two-Tier Model

**Plan:** [19.9: Community Prompt Submissions (Intake & Curation)](../plan.md)

## User Story

**As an** atcr user deciding whether and how to contribute a tuned persona back to the project (and the maintainer who later curates it)
**I want** `docs/personas-install.md` and `docs/personas-authoring.md` to fully document the `atcr personas submit <name>` subcommand and the `submitted` → graduated two-tier curation model
**So that** I can learn the submit flow, its preconditions, and error cases from the docs alone (without reading source), and understand that a submission being fixture-passing is not the same as it being vetted

## Story Context

- **Background:** Themes 1–4 implement `submit`'s fixture gate, fork+PR automation, `submitted` status, and maintainer graduation path. This story is the documentation capstone required by AC4: it does not add behavior, it makes the already-implemented flow discoverable and the curation model explicit so users and maintainers share the same mental model of what "submitted" does and does not mean.
- **Assumptions:**
  - `docs/personas-install.md` currently documents six subcommands (`install`/`list`/`search`/`remove`/`test`/`upgrade`) under the heading "## The six subcommands" (line 40); `submit` becomes the seventh, inserted after `test` and before `upgrade` to keep the fixture-related commands (`test`, then `submit`, which depends on the same gate) adjacent.
  - `docs/personas-authoring.md`'s "Contribution checklist" (`## 4. Contribution checklist`, line 162) already lists "Fixture test passes" as a manual checklist item (line 172); this story cross-references that item rather than rewriting the checklist, since `submit` automates verification of that one item, not the whole checklist.
  - Both docs files already exist and follow an established per-command / per-section format; this story must match that format exactly rather than inventing a new structure.
  - Terminology is fixed by the epic's own terminology-collision resolution (recorded in `original-requirements.md`): "community-contributed" describes provenance, "submitted" describes the unvetted status tier — the docs must not use these interchangeably or introduce synonyms.
- **Constraints:**
  - Must not describe `submitted` as a new `Source` value — the docs must state explicitly that `Source` stays `community` and `submitted` is an orthogonal status, mirroring the codebase's actual separation (Theme 3).
  - Must not imply any marketplace, website, or hosted-registry surface exists (AC3) — all documented interaction happens through `atcr personas submit`, `gh`, and a GitHub PR.
  - Persona directory path references in the new/edited docs must match `commpersonas.PersonasDir()` (`~/.config/atcr/personas/`) exactly — no stale or invented paths.
  - Must not alter any of the six existing subcommand sections' content beyond the heading text change and the insertion point for `submit`.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Theme 1 (Local Fixture-Gate Reuse and Submission Blocking), Theme 2 (Fork + PR Automation via `gh`), Theme 3 (`submitted` Status Distinct from `Source`/Provenance), Theme 4 (Maintainer Graduation into the Vetted Library) — this story documents behavior those stories implement and must not describe capabilities that don't yet exist |

## Success Criteria (SMART Format)

- **Specific:** `docs/personas-install.md`'s heading is updated from "## The six subcommands" to "## The seven subcommands," a new `### atcr personas submit <name>` section is added between the existing `test` and `upgrade` sections (one-line description, example invocation/output, error cases, matching the format of the surrounding subcommand sections), and `docs/personas-authoring.md`'s contribution checklist gains a cross-reference to `submit` plus a new section explaining the `submitted` → graduated model.
- **Measurable:** Both files render correctly as markdown (no broken headers or links); a reader following only `docs/personas-install.md` and `docs/personas-authoring.md` can determine (a) the exact command to run, (b) what happens on fixture failure vs. success, (c) that `Source` remains `community` while `submitted` is a separate status, and (d) how a submission becomes graduated — without consulting source code.
- **Achievable:** Purely additive documentation edits to two existing markdown files whose per-command format is already established by the six current subcommand sections; no new tooling or code required.
- **Relevant:** Directly satisfies AC4's documentation requirement and is the only remaining uncovered piece of AC4 once `go test ./...` passes; without it, the two-tier curation model implemented by Themes 3–4 would exist in code but be undiscoverable by users or maintainers.
- **Time-bound:** Completed within this sprint's implementation phase, after Themes 1–4 land (so the documented behavior matches shipped behavior), and verified before the sprint's code-review gate alongside the `go test ./...` full-suite pass.

## Acceptance Criteria Overview

1. `docs/personas-install.md` documents `atcr personas submit <name>` as the seventh subcommand, in the existing per-subcommand format (description, example, error cases), positioned after `test` and before `upgrade`, with the section heading updated to "The seven subcommands."
2. `docs/personas-authoring.md`'s contribution checklist cross-references `atcr personas submit` as the automated equivalent of the "Fixture test passes" checklist item, noting that a failing fixture blocks submission.
3. `docs/personas-authoring.md` gains a new section describing the `submitted` → graduated two-tier model: `Source` stays `community`, `submitted` is an orthogonal status assigned on successful submission, and graduation is a maintainer PR-merge action that promotes the persona into `personas/community/`.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`_

## Technical Considerations

- **Implementation Notes:** Edit `docs/personas-install.md` (heading at line 40 → "## The seven subcommands"; new `### atcr personas submit <name>` subsection inserted between the existing `test` and `upgrade` subsections, following the established one-line-description → example invocation/output → error-cases structure used by every other subcommand). Edit `docs/personas-authoring.md` (cross-reference added near line 172's "Fixture test passes" checklist item in `## 4. Contribution checklist`; new section added — e.g., a "## 6. From submitted to graduated" or similarly numbered section following the existing numbered-section convention — explaining the two-tier model in plain language, without duplicating Theme 3/4's implementation detail).
- **Integration Points:** No code integration; this story's only integration point is textual accuracy against the shipped behavior of Themes 1–4 (command name, error messages, status terminology) and against `commpersonas.PersonasDir()` for any path references. Should verify final subcommand behavior (exact flags, output strings, error text) against the actual `cmd/atcr/personas.go` `submit` implementation and `cmd/atcr/personas_test.go` / `internal/personas/submit_test.go` before finalizing doc examples, so documented example output matches real output.
- **Data Requirements:** None — documentation-only change; no schema, config, or persisted data introduced.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Docs are written or finalized before Themes 1–4 land, causing documented command syntax, output, or error text to drift from actual shipped behavior | Medium | Sequence this story after Themes 1–4 in the sprint; verify example invocations/output against the actual implementation and its tests before finalizing |
| Docs blur the "submitted" status with the "community" provenance term, reintroducing the terminology collision the epic explicitly resolved | Medium | Use "community-contributed" only for provenance and "submitted" only for the unvetted status tier throughout both files, per `original-requirements.md`'s terminology-collision resolution; review both edits against that resolution before completion |
| New "seventh subcommand" section is inserted with inconsistent formatting relative to the other six, reducing doc quality/readability | Low | Mirror the exact structural pattern (heading level, description-then-example-then-errors ordering) already used by the `test` and `upgrade` sections immediately adjacent to the insertion point |
| Docs imply or fail to explicitly deny a marketplace/website/hosted-registry surface, contradicting AC3 | Low | Explicitly state the flow is GitHub-PR-native (fork, branch, PR, maintainer merge) with no other surface, matching language already used in `plan.md`'s objectives |

---

**Created:** July 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
