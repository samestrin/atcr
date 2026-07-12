# User Story 3: `/atcr debt resolve` Skill Route

**Plan:** [20.1: Public TD Resolve Skill](../plan.md)

## User Story

**As a** standalone/public atcr user driving atcr through an AI coding agent (no `.planning/` directory)
**I want** to invoke `/atcr debt resolve` and have items in my local `.atcr/`-scoped technical-debt store autonomously fixed
**So that** atcr closes the loop from "review-only" to "review-and-fix" without requiring the private sprint pipeline or a `.planning/` directory

## Story Context

- **Background:** The private pipeline already has `/resolve-td`, a battle-tested skill that reads `.planning/technical-debt/README.md`, selects items deterministically, and drives a per-item RED→GREEN→ADVERSARIAL→REFACTOR TDD cycle (with a final cumulative adversarial pass) to resolve them. Standalone/public atcr users have no equivalent — `skill/SKILL.md`'s dispatcher already lists `atcr debt` in its command table (line 79) as a top-level command with subcommands, but resolution is out of scope for the currently-shipped skill. This story adds the resolve route as an on-demand secondary skill file, `skill/debt-resolve/SKILL.md`, following the exact loading pattern `skill/host-review.md` already establishes for `atcr review`'s host-review pass.
- **Assumptions:**
  - Story 1 (local `.atcr/`-scoped TD store) has landed and defines a stable record schema (including `FindingID`, file/line/problem, severity, and the `justification`/`SourceReport` fields already stamped by `internal/reconcile/justification.go` when present).
  - Story 2 (reconcile-time persistence hook) is populating that store across `atcr reconcile` runs, so there is real, accumulating data for this route to act on.
  - Story 4 (`skill/CONVENTIONS.md` extraction) either lands before or alongside this story so `skill/debt-resolve/SKILL.md` can reference shared Prerequisites (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules) rather than duplicating them; if sequenced in parallel, this story stubs the reference and Story 4 fills it in.
  - The target repo has no `.planning/` directory and no sprint context — the resolution cycle must be fully repo-agnostic, driven only by the local store and the live codebase.
- **Constraints:**
  - `/atcr debt resolve` must be an additive subcommand of the existing `atcr debt` row in `skill/SKILL.md`'s command table, not a new top-level dispatcher row — per the routing-table-drift convention documented at `skill/SKILL.md:84-87` and the `dispatcherCommands` list in `skill/skill_test.go:133`.
  - Every command invocation must remain CLI-only (never a direct engine call), consistent with `skill/SKILL.md`'s stated dispatcher contract (`skill/SKILL.md:16,37`). The design sprint resolved the store-access question in favor of a new `atcr debt resolve` CLI subcommand in `cmd/atcr/debt.go`; the skill shells out to this subcommand and never reads `.atcr/debt/*.jsonl` directly (see AC 03-02 and `documentation/cli-integration-points.md`).
  - The resolution cycle must not depend on `.planning/`, sprint state, or any artifact private-pipeline `/resolve-td` assumes exists (e.g. `.planning/technical-debt/README.md`, sprint branches under the private convention).
  - New skill content must be embedded in `skill/skill.go`'s build-time constants and covered by `skill/skill_test.go` assertions, matching how `host-review.md`, `ambiguity-adjudication.md`, and `findings-format.md` are already wired in.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (local `.atcr/`-scoped TD store), Story 2 (reconcile-time persistence hook) |

## Success Criteria (SMART Format)

- **Specific:** `skill/debt-resolve/SKILL.md` exists, is embedded via `skill/skill.go`, is reachable from `skill/SKILL.md`'s `atcr debt` row as an on-demand load (mirroring `host-review.md`'s pattern), and implements a documented RED→GREEN→ADVERSARIAL→REFACTOR resolution cycle over items read from the local TD store.
- **Measurable:** Running `/atcr debt resolve` against a fixture repo containing at least one persisted local-store TD item results in that item being fixed, verified (tests pass, adversarial check clears), and marked resolved in the local store, with zero references to `.planning/` anywhere in the executed path.
- **Achievable:** The cycle is an adaptation of `/resolve-td`'s existing, proven per-item TDD loop — the story reuses its stage structure rather than designing resolution logic from scratch.
- **Relevant:** Directly satisfies AC3, the centerpiece capability that converts standalone atcr from review-only to review-and-fix.
- **Time-bound:** Completed within this plan's sprint, gated on Story 1 and Story 2 landing first so real store data exists to resolve against.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-skill-md-dispatcher-documentation.md) | SKILL.md Dispatcher Documentation for `/atcr debt resolve` | Unit |
| [03-02](../acceptance-criteria/03-02-debt-resolve-cli-subcommand.md) | `atcr debt resolve` CLI Subcommand | Unit |
| [03-03](../acceptance-criteria/03-03-item-selection-and-justification-consumption.md) | Local Store Item Selection and Justification/SourceReport Consumption | Integration |
| [03-04](../acceptance-criteria/03-04-red-green-adversarial-refactor-cycle.md) | RED→GREEN→ADVERSARIAL→REFACTOR Resolution Cycle | E2E |
| [03-05](../acceptance-criteria/03-05-resolution-outcome-persistence-and-branch-safety.md) | Resolution Outcome Persistence and Branch Safety | Integration |
| [03-06](../acceptance-criteria/03-06-go-embed-wiring-and-test-coverage.md) | Go Embed Wiring and Test Coverage for `skill/debt-resolve/SKILL.md` | Unit |

## Original Criteria Overview

1. `skill/SKILL.md`'s `atcr debt` command-table row documents `/atcr debt resolve` (or a dedicated subsection) without inventing subcommand names beyond what is implemented, and `skill/debt-resolve/SKILL.md` is loaded on demand per that documentation.
2. `/atcr debt resolve` reads items from the local `.atcr/`-scoped TD store (Story 1/2) — including `justification`/`SourceReport` fields when present on a record — and selects items to resolve using a deterministic, documented selection rule (e.g. severity/age or explicit filter), analogous to `/resolve-td`'s `llm_support_td_filter`-driven selection.
3. Each selected item is resolved via a RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted from `/resolve-td`, with zero `.planning/` dependency, and the local store is updated to reflect resolution outcome.
4. `skill/skill.go` embeds the new skill file(s), and `skill/skill_test.go` gains assertions verifying the debt-resolve route is documented and non-duplicated against `skill/CONVENTIONS.md`.

## Technical Considerations

- **Implementation Notes:** New on-demand secondary skill file at `skill/debt-resolve/SKILL.md`, structured like `skill/host-review.md` (loaded on demand, not inlined into `SKILL.md`'s ~500-line budget). Adapt `/resolve-td`'s per-item stages: (1) pre-fix evaluation (still-exists, clear-fix, safe-scope) against the live codebase using the item's stored file/line/problem, relocating findings whose cited location drifted since capture; (2) RED — reproduce/confirm the issue; (3) GREEN — apply the minimal fix; (4) ADVERSARIAL — an over-simplification/reward-hack check equivalent to `/resolve-td`'s `llm_support_diff_smell` gate (test-only changes, weakened assertions, lint/type suppressions, stubbed bodies flagged NEEDS_REVIEW); (5) REFACTOR — cleanup pass. Followed by a cumulative adversarial review across all items resolved in the run, mirroring `/resolve-td`'s final stage.
- **Integration Points:**
  - `skill/SKILL.md` (`skill/SKILL.md:79`, command table) — extend the `atcr debt` row's description; subcommand discovery convention already states subcommands are found via `atcr <command> --help` (`skill/SKILL.md:57`), so no new dispatcher row is added.
  - `skill/skill.go` — add the new file(s) to the embed set alongside `SKILL.md`, `host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`.
  - `skill/skill_test.go` — add assertions (the `dispatcherCommands` list at line 133 likely needs no new entry since `debt` is already present; new assertions target documentation presence and CONVENTIONS.md non-duplication).
  - `skill/CONVENTIONS.md` (Story 4) — referenced for Prerequisites (binary-on-PATH, git-worktree check, `.atcr/` path-safety) instead of duplicating boilerplate.
  - Local TD store (Story 1) and its reconcile-time persistence (Story 2) — the read/query surface this route consumes; access is via the new `atcr debt resolve` CLI subcommand in `cmd/atcr/debt.go`, not by direct file read from the skill.
  - `internal/reconcile/emit.go`'s `Justification`/`SourceReport` fields on `JSONFinding`, stamped by `internal/reconcile/justification.go:72`, consumed when present on a stored record to give the fix loop narrative context, exactly as `/resolve-td` benefits from equivalent private-pipeline metadata.
- **Data Requirements:** Depends on Story 1's local TD store record schema (must expose file, line, problem/description, severity, `FindingID`, resolution status, and optional `justification`/`SourceReport`) and Story 2's guarantee that records persist and are queryable across multiple `atcr reconcile` runs before this route has anything real to act on.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Direct-file-read vs. new-CLI-subcommand design question — resolved during `/design-sprint` in favor of a new `atcr debt resolve` CLI subcommand in `cmd/atcr/debt.go` for consistency with SKILL.md's CLI-invocation-only convention | Low | Already closed in AC 03-02; the skill shells out to `atcr debt resolve` and never reads `.atcr/debt/*.jsonl` directly |
| Adapted RED→GREEN→ADVERSARIAL→REFACTOR cycle diverges from `/resolve-td`'s proven behavior in an unreviewed way, giving public users a weaker resolution loop | High | Ground the adaptation explicitly against `/resolve-td`'s documented stage-by-stage behavior during `/design-sprint` rather than designing from scratch; reuse the same reward-hack/over-simplification adversarial gate concept |
| Autonomous fix loop operating with zero `.planning/` context and no sprint branch could land unreviewed changes directly on a user's working branch | High | Document a clear commit/branch convention in the skill file (e.g. dedicated branch or explicit user confirmation before applying fixes), analogous to `/resolve-td`'s branch-creation behavior when run outside a sprint context |
| Skill route consumes `justification`/`SourceReport` fields that may be absent on older or manually-added store records | Low | Treat these fields as optional throughout; the resolution cycle must function correctly (with less narrative context) when they are missing |
| New embed/test wiring in `skill/skill.go` and `skill/skill_test.go` drifts out of sync with Story 4's CONVENTIONS.md work if sequenced concurrently | Medium | Coordinate story sequencing so Story 4 lands its CONVENTIONS.md scaffold first, or stub the reference and reconcile in a follow-up commit within the same sprint |

---

**Created:** July 11, 2026
**Status:** Draft - Awaiting Acceptance Criteria
