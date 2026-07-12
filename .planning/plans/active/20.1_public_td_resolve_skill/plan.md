## Metadata
**Last Modified:** 2026-07-12T03:54:11Z
**Plan Type:** feature

## Plan Overview
**Plan Type:** Feature Development ✨
**Plan Goal:** Give standalone/public atcr users a durable, `.atcr/`-scoped technical-debt store and an autonomous `/atcr debt resolve` skill route so findings from `atcr review`/`atcr reconcile` runs accumulate into a persistent backlog and get fixed without a `.planning/` sprint workflow.
**Target Users:** Standalone/public atcr users (OSS developers, no `.planning/` directory) driving atcr through an AI coding agent; secondarily the atcr maintainer validating the public skill's parity with the private pipeline.
**Framework/Technology:** Go (stdlib-only JSONL persistence, Cobra CLI), Markdown Agent Skills (Level 1/2 progressive disclosure)

## Objectives

1. Define and implement a local, `.atcr/`-scoped technical-debt store format that works without a `.planning/` directory and can accumulate findings across review runs.
2. Extend `atcr reconcile` (or an equivalent persistence step) to append reconciled findings into the local TD store, with an opt-out flag matching existing `--no-scorecard` conventions.
3. Extend the public `skill/SKILL.md` dispatcher with a new `/atcr debt resolve` route that reads the local TD store and autonomously resolves stored items, consuming `justification` and `SourceReport` fields when present.
4. Document the new public debt-resolve capability in `docs/skill-usage.md`.
5. *(Extended Scope)* Extract shared public-skill boilerplate from `skill/SKILL.md` into `skill/CONVENTIONS.md` per Epic 20.0's addendum, so both public skills reference a single prerequisites source.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/20.1_public_td_resolve_skill/`

## Feature Analysis Summary
This plan gives standalone atcr users the review-and-fix loop the private `.planning/technical-debt/` pipeline already has, without requiring a `.planning/` directory or sprint workflow. It introduces a new `.atcr/`-scoped, append-only local TD store (patterned on `internal/scorecard/store.go`'s atomic-append JSONL design, but rooted per-repo rather than per-user), a persistence hook so `atcr reconcile` appends reconciled findings into that store across runs, and a new `/atcr debt resolve` skill route that autonomously resolves stored items using the `justification`/back-reference fields already stamped onto reconciled findings by Epic 18.2. It also extracts shared skill boilerplate (binary-on-PATH check, `.atcr/` path-safety rules) into a new `skill/CONVENTIONS.md` per Epic 20.0's addendum, since this is the second public skill added to the repo *(Extended Scope — not in original AC1-4; added from Epic 20.0's addendum during refinement)*. Discovery found that `atcr debt` already exists as a live, `.planning/`-scoped CLI command family (list/add/dashboard) — this plan's new store and commands should extend that family's shape rather than invent a parallel one.

## Technical Planning Notes
- New local TD store package (name TBD in design) copies `internal/scorecard/store.go`'s Append/ReadRecords/ReadAll pattern (one `os.Write` per record under `O_APPEND` for atomic-append safety) but roots at `.atcr/` instead of `os.UserConfigDir()`.
- The store becomes atcr's 6th append-only ledger (joining audit, debate, scorecard, tools, history); the project has an accepted, documented won't-fix on cross-process O_APPEND locking (TD-004) — the new store should state the same tradeoff explicitly rather than silently diverging.
- `internal/reconcile/emit.go`'s `Justification`/`SourceReport` fields (Epic 18.2 in the repo; referred to as Epic 18.3 in the original requirements) are already live on main — AC3's stated dependency is satisfied today, no blocking wait needed.
- `atcr reconcile` (`cmd/atcr/reconcile.go`) already has a `--no-scorecard` opt-out flag pattern; a persistence hook + matching opt-out flag is the natural integration point for AC2.
- `skill/SKILL.md`'s command table already lists `atcr debt`; the new `/atcr debt resolve` route is an additive table row, not a new top-level command, per the existing routing-table-drift convention documented in SKILL.md.

## Documentation References
- **[CRITICAL]** [Agent Skills Format & Progressive Disclosure](documentation/agent-skills-format.md)
- **[CRITICAL]** [Append-Only JSONL Store Pattern](documentation/append-only-store-pattern.md)
- **[CRITICAL]** [Local TD Store Schema](documentation/local-td-store-schema.md)
- **[IMPORTANT]** [CLI Integration Points](documentation/cli-integration-points.md)
- **[IMPORTANT]** [Skill Dispatcher & CONVENTIONS.md Extraction](documentation/skill-dispatcher-conventions.md)

## Implementation Strategy
Work decomposes into five stories: (1) design and implement the local `.atcr/`-scoped TD store package, (2) wire persistence into `atcr reconcile` so findings accumulate across runs, (3) build the `/atcr debt resolve` skill route that reads the store and autonomously resolves items via a RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted to a `.planning/`-free context, (4) extract `skill/CONVENTIONS.md` from `skill/SKILL.md`'s Prerequisites section per Epic 20.0's addendum, and (5) document the new capability in `docs/skill-usage.md`. Given the four-component footprint (new store package, skill/atcr-resolve + CONVENTIONS.md, cmd/atcr reconcile hook, docs), this plan exceeds `/execute-epic`'s ≤2-component scope guard and correctly routes through the full sprint pipeline (`/design-sprint` → `/create-sprint` → `/execute-sprint`).

## Recommended Packages
No high-ROI packages identified — the codebase's existing append-only-ledger precedents (scorecard, history, audit, debate) are all pure stdlib (`encoding/json`, `os`, `bufio`), and this plan should follow the same minimal-dependency convention.

## User Story Themes
1. **Local TD store persistence** — as a standalone atcr user, I want reconciled findings to accumulate into a durable local store so they aren't lost after one review run.
2. **Reconcile-time persistence hook** — as a standalone atcr user, I want `atcr reconcile` to automatically append findings into the local store (with an opt-out flag), mirroring the existing `--no-scorecard` pattern.
3. **`/atcr debt resolve` skill route** — as a standalone user driving atcr through a coding agent, I want to say "/atcr debt resolve" and have stored TD items autonomously fixed.
4. **Shared skill conventions extraction** — as the atcr maintainer, I want `skill/CONVENTIONS.md` to hold boilerplate shared by both public skills so `skill/SKILL.md` and the new skill don't duplicate Prerequisites text.
5. **Documentation** — as a new standalone user, I want `docs/skill-usage.md` to explain how to install and use the new debt-resolve capability.

## Planning Success Criteria
- A local, `.atcr/`-scoped TD store format is implemented and documented (mirrors `docs/scorecard.md`'s documentation style).
- `atcr reconcile` persists reconciled findings into the local store across multiple runs (verified by running reconcile twice and confirming both runs' findings are queryable).
- `/atcr debt resolve` autonomously resolves at least one stored TD item end-to-end in a `.planning/`-free fixture repo, consuming `justification`/`SourceReport` when present.
- `docs/skill-usage.md` documents installation and usage of the new route.
- `skill/CONVENTIONS.md` exists and both `skill/SKILL.md` and the new skill's SKILL.md reference it instead of duplicating Prerequisites text.

## Out of Scope

- Any change to the private `.planning/technical-debt/` pipeline or `claude-prompts` skills.
- Binary packaging/release automation (Epic 21.0).
- Multi-repo/team-wide TD aggregation (Team Edition concerns).

## Dependencies

- Epic 18.2 (Technical Debt Metadata Pipeline Enhancement) — adds `justification`/`SourceReport` fields to `reconciled/findings.json`; referred to as Epic 18.3 in the original requirements. These fields are already live on main.
- Epic 20.0 (Standalone Skill Release) — establishes the `.atcr/` conventions, public skill install pattern, and single dispatcher skill architecture this plan builds upon.
- Epic 21.0 (Release & Packaging Automation) — binary distribution this skill will ride on; no packaging work is in scope here.

## Risk Mitigation
| Risk | Impact | Mitigation |
|------|--------|------------|
| New `.atcr/`-scoped store reintroduces the exact `.atcr/findings-history.jsonl` design Epic 19.4 moved away from, appearing to contradict that decision | Confusion during review; possible push to unify the two stores | Explicitly document in design why this store targets a different audience (standalone/public, zero `.planning/`) than internal/history's now-`.planning/`-scoped design |
| Concurrent `atcr reconcile` runs appending to the same local store shard could tear a JSONL line | Low-probability data loss identical to the accepted TD-004 tradeoff on the other 5 ledgers | Explicitly adopt the same accepted won't-fix stance (single `os.Write` per record, no cross-process lock) rather than leaving it undecided |
| `/atcr debt resolve`'s adapted RED→GREEN→ADVERSARIAL→REFACTOR cycle diverges from `/resolve-td`'s battle-tested private-pipeline version in an unreviewed way | Public users get a weaker resolution loop than the private pipeline offers | Ground the adaptation explicitly against `/resolve-td`'s documented behavior during `/design-sprint`, not from scratch |

## Next Steps
1. `/find-documentation @.planning/plans/active/20.1_public_td_resolve_skill/`
2. `/create-documentation @.planning/plans/active/20.1_public_td_resolve_skill/`
3. `/create-user-stories @.planning/plans/active/20.1_public_td_resolve_skill/`
4. `/create-acceptance-criteria @.planning/plans/active/20.1_public_td_resolve_skill/`
5. `/design-sprint @.planning/plans/active/20.1_public_td_resolve_skill/`
6. `/create-sprint @.planning/plans/active/20.1_public_td_resolve_skill/`
