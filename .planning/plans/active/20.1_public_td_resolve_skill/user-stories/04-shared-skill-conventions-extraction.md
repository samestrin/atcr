# User Story 4: Shared Skill Conventions Extraction

**Plan:** [20.1: Public TD Resolve Skill](../plan.md)

## User Story

**As a** atcr maintainer
**I want** the Prerequisites boilerplate currently embedded in `skill/SKILL.md` (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules) extracted into a new shared `skill/CONVENTIONS.md` file
**So that** `skill/SKILL.md` and the new `skill/debt-resolve/SKILL.md` (Story 3) both reference a single source of truth for prerequisites instead of duplicating text that will drift out of sync as more public skills are added

## Story Context

- **Background:** Epic 20.0 established `skill/SKILL.md` as the single public dispatcher skill, with its Prerequisites section (binary-on-PATH check, git-worktree check) inlined directly in the file at `skill/SKILL.md`'s "## Prerequisites" heading. This epic (20.1) adds the second public skill to the repo, `skill/debt-resolve/SKILL.md` (Story 3), which needs the same prerequisite checks plus `.atcr/` path-safety rules for a `.planning/`-free resolution loop. Epic 20.0's plan explicitly deferred this extraction, stating "the shared-conventions extraction happens... as part of Epic 20.1's execution" — this story is that deferred work, assigned as Objective 5 (Extended Scope) during `/refine-epic`. It is not part of the original AC1-4 scope.
- **Assumptions:**
  - The extraction target is the existing "## Prerequisites" section in `skill/SKILL.md` (skill/SKILL.md:19-22): the `atcr` binary-on-PATH check, the git-worktree check, and the `gh` CLI note for PR resolution — plus `.atcr/` path-safety rules that Story 3's `debt-resolve` skill needs but that are not yet written down anywhere (this story writes them, sourced from the existing `.atcr/`-scoped conventions already implicit in `cmd/atcr/reconcile.go`'s `Root: "."` pattern and the rest of the public skill's directory-scoping behavior).
  - `skill/CONVENTIONS.md` follows the same on-demand-sibling-file pattern already used for `host-review.md`, `ambiguity-adjudication.md`, and `findings-format.md`: a separate Markdown file referenced from SKILL.md rather than inlined.
  - This is a pure refactor/extraction with no behavior change to the CLI or to how the dispatcher skill responds to a user — its purpose is DRY between the two public skill files, not new functionality.
  - Story 3 (`skill/debt-resolve/SKILL.md`) consumes `skill/CONVENTIONS.md` by reference; this story should land alongside or ahead of Story 3 so Story 3 can point at a finished file rather than a placeholder.
- **Constraints:**
  - Must not remove or weaken any existing prerequisite check currently enforced by `skill/SKILL.md` — the extraction relocates text, it does not drop coverage.
  - `skill/CONVENTIONS.md` must be embedded in the Go harness (`skill/skill.go`), following the exact `//go:embed` + exported-`string`-variable pattern already used for `HostReviewMD`, `AmbiguityAdjudicationMD`, and `FindingsFormatMD`.
  - `skill/skill_test.go`'s existing `dispatcherCommands` list (skill/skill_test.go:133) and `TestSkill_RequiredSections` / `TestSkill_NoAbsoluteOrClaudePaths` assertions must continue passing unmodified in spirit — this story adds new assertions for CONVENTIONS.md, it does not need a new `dispatcherCommands` entry since this is a shared doc file, not a CLI command.
  - `TestSkill_NoAbsoluteOrClaudePaths` iterates a fixed list of embedded-MD variables (skill/skill_test.go:117) — `ConventionsMD` must be added to that list so the new file is held to the same no-`.claude`/no-absolute-path bar as the existing three.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Should land alongside or before Story 3 (`/atcr debt resolve` skill route) so Story 3's SKILL.md can reference a finished `skill/CONVENTIONS.md` rather than a placeholder; technically independent of Stories 1-2 (local TD store, reconcile persistence hook). |

## Success Criteria (SMART Format)

- **Specific:** A new `skill/CONVENTIONS.md` file exists containing the binary-on-PATH check, git-worktree check, and `.atcr/` path-safety rules; `skill/SKILL.md`'s "## Prerequisites" section is rewritten to reference `skill/CONVENTIONS.md` instead of restating the text; `skill/skill.go` embeds the new file as `ConventionsMD`.
- **Measurable:** `skill/skill_test.go` gains passing assertions that (a) `ConventionsMD` is non-empty, (b) `skill/SKILL.md` references `CONVENTIONS.md` rather than containing the full duplicated Prerequisites text, (c) `ConventionsMD` is included in the existing no-`.claude`/no-absolute-path check alongside the other three embedded files; `go build ./skill/...` and `go test ./skill/...` both pass.
- **Achievable:** The extraction is a mechanical move of existing, already-written text (Prerequisites section) plus a small addition (`.atcr/` path-safety rules, grounded in the existing `Root: "."` convention already used elsewhere in the codebase) — no new subsystem or CLI change involved.
- **Relevant:** Directly satisfies Epic 20.0's addendum and this epic's Objective 5 (Extended Scope): prevents the two public skill files from duplicating and drifting on shared boilerplate as the public skill surface grows beyond one file.
- **Time-bound:** Deliverable within this sprint, sequenced alongside or ahead of Story 3 so Story 3's new SKILL.md can reference the finished file on first write rather than needing a follow-up edit.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-conventions-md-creation.md) | skill/CONVENTIONS.md Creation | Unit |
| [04-02](../acceptance-criteria/04-02-skill-md-prerequisites-pointer.md) | skill/SKILL.md Prerequisites Section Rewritten to Point to CONVENTIONS.md | Unit |
| [04-03](../acceptance-criteria/04-03-go-embed-and-test-coverage.md) | skill/CONVENTIONS.md Embedded in skill.go as ConventionsMD with Test Coverage | Unit |

## Original Criteria Overview

1. `skill/CONVENTIONS.md` is created containing the binary-on-PATH check, git-worktree check, and `.atcr/` path-safety rules, written once as shared text.
2. `skill/SKILL.md`'s "## Prerequisites" section is rewritten to point to `skill/CONVENTIONS.md` instead of duplicating the checks inline, with no loss of enforced coverage.
3. `skill/CONVENTIONS.md` is embedded in `skill/skill.go` (as `ConventionsMD`, mirroring the existing `HostReviewMD`/`AmbiguityAdjudicationMD`/`FindingsFormatMD` pattern) and covered by new `skill/skill_test.go` assertions (non-empty, referenced-not-duplicated, included in the no-`.claude`/no-absolute-path check).

## Technical Considerations

- **Implementation Notes:**
  - Source text to relocate: `skill/SKILL.md`'s existing "## Prerequisites" section (binary-on-PATH halt message, git-worktree halt message, `gh` CLI note for PR resolution).
  - New text to add: `.atcr/` path-safety rules — the convention (already implicit in `cmd/atcr/reconcile.go`'s `Root: "."` and the rest of the public skill's scoping) that all public-skill file operations stay rooted at the repo's `.atcr/` directory and never read/write outside it or under `.planning/`.
  - `skill/SKILL.md`'s "## Prerequisites" heading remains (so the required-sections test in `skill/skill_test.go` continues to find it) but its body becomes a short pointer sentence plus a load-on-demand reference to `CONVENTIONS.md`, mirroring how "## Host Review Instructions" points to `host-review.md` rather than inlining its content.
  - `skill/skill.go`: add a `//go:embed CONVENTIONS.md` directive and `var ConventionsMD string`, alongside the existing three embedded secondary files, and update the package doc comment's list of secondary files.
- **Integration Points:**
  - `skill/debt-resolve/SKILL.md` (Story 3, built in parallel/after) references `skill/CONVENTIONS.md` for its own Prerequisites section instead of restating the checks — this story's file must exist and be stable before or alongside Story 3's authoring.
  - `skill/skill_test.go`'s `TestSkill_RequiredSections`, `TestSkill_NoAbsoluteOrClaudePaths`, and the `dispatcherCommands`-driven `TestSkill_DispatcherRoutingTable` are the existing tests this story must not break; new tests are additive.
- **Data Requirements:** None — this is a Markdown-file and Go-embed-constant change only, no runtime data structures.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Extraction accidentally drops or weakens a prerequisite check (e.g. the `gh` CLI note) while relocating text | Medium | Diff the relocated text against the original `skill/SKILL.md` Prerequisites section line-for-line before removing the original; new tests assert the pointer is present, and existing tests continue to assert the required sections and orchestration order are intact |
| `.atcr/` path-safety rules are invented ad hoc rather than grounded in the codebase's actual scoping convention, producing text that contradicts how `cmd/atcr/reconcile.go` or the local TD store (Story 1) actually behave | Medium | Ground the rules explicitly in `cmd/atcr/reconcile.go`'s `Root: "."` convention and Story 1's `.atcr/debt/` scoping decision rather than writing generic/speculative rules |
| Story 3 is authored before this story lands, causing `skill/debt-resolve/SKILL.md` to either duplicate the Prerequisites text anyway or reference a file that does not yet exist | Low | Dependencies note states this story should land alongside or before Story 3; sprint sequencing should schedule this story first or in the same phase |

---

**Created:** July 11, 2026
**Status:** Draft - Awaiting Acceptance Criteria
