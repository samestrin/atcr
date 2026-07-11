# User Story 1: Dispatcher Skill Rewrite

**Plan:** [20.0: Standalone ATCR Skill Distribution](../plan.md)

## User Story

**As an** external OSS developer (or the internal maintainer) invoking atcr through an AI coding agent
**I want** `skill/SKILL.md` to expose a single `/atcr <command> <flags>` dispatcher instead of one linear review-only script
**So that** I can drive any atcr capability (review, reconcile, verify, debate, report, status, doctor, and the rest of the CLI surface) through one discoverable skill entry point, without installing or managing a fragmented set of single-purpose skills

## Story Context

- **Background:** The current `skill/SKILL.md` (126 lines) is a linear orchestration script hard-coded to a single "review a git range" flow: pre-flight range → start review → poll status → host review → reconcile → report. The 2026-07-05 addendum (`original-requirements.md`, "Addendum Override: The Dispatcher Pattern") overturns the earlier plan to ship many separate single-capability skills, and instead mandates one dispatcher skill under a unified `/atcr <command>` UX, for both the public OSS release and (as a later manual follow-up, out of scope here) the private `claude-prompts` skills.
- **Assumptions:** The Cobra command tree registered in `newRootCmd` (`cmd/atcr/main.go:185-208`) is the single source of truth for command names the dispatcher may route to — no invented aliases. The existing orchestration logic (range resolution, background review + polling, host review, reconcile, report) is correct and must be preserved, not redesigned; this story changes the *entry surface*, not the underlying flow. Claude Code's three-level Agent Skill progressive-disclosure model applies: Level 1 (YAML frontmatter) is always loaded, Level 2 (SKILL.md body) loads only when triggered and should stay lean, Level 3 (secondary files) loads on demand from the filesystem with effectively no context cost until referenced.
- **Constraints:** `skill/SKILL.md` must stay under the plan's ~500-line budget even after gaining command-routing logic for ~20 top-level commands. Frontmatter `name` must stay lowercase/numbers/hyphens only, ≤64 chars, and must not contain "anthropic" or "claude"; `description` must be ≤1024 chars. Detailed Host Review Instructions, Ambiguity Adjudication, and the Findings Format Reference currently inline in SKILL.md must move to secondary markdown files (e.g. `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`) and be preserved **verbatim** to avoid regressing existing orchestration behavior for current adopters. Out of scope: modifying the private `claude-prompts` skills themselves, binary packaging/release automation (Epic 21.0), building any new `atcr self-test` command.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `skill/SKILL.md` is rewritten so its frontmatter description and body present a `/atcr <command> <flags>` dispatcher pattern that routes to the exact Cobra command names registered in `cmd/atcr/main.go:185-208` (`review`, `reconcile`, `verify`, `debate`, `report`, `github`, `range`, `status`, `init`, `quickstart`, `serve`, `doctor`, `trust`, `scorecard`, `leaderboard`, `benchmark`, `personas`, `models`, `debt`, `history`, `audit-report`, `version`), with the current review→reconcile→report orchestration flow preserved as one routable command path rather than the skill's only behavior.
- **Measurable:** `skill/SKILL.md` body is under ~500 lines; the Host Review Instructions, Ambiguity Adjudication, and Findings Format Reference sections are relocated to secondary markdown files under `skill/` and preserved verbatim (byte-for-byte content, only location changes); every command name referenced in the dispatcher routing table matches an entry in the `newRootCmd` registration with zero drift.
- **Achievable:** The rewrite only restructures existing, already-working orchestration instructions into a router plus on-demand secondary files — no new engine behavior, no new CLI commands, and no cross-repo edits are required.
- **Relevant:** This is the headline deliverable of the plan and the addendum's explicit rationale (product-quality OSS UX, architectural unification, prompt-size management via progressive disclosure) — without this story the plan has no public-facing dispatcher to release.
- **Time-bound:** Completed within this plan's single sprint, as the first and highest-risk user story sequenced before the documentation-accuracy pass (Story 4) that depends on it.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-dispatcher-command-routing-table.md) | Dispatcher Command Routing Table | Unit |
| [01-02](../acceptance-criteria/01-02-review-flow-preserved-through-dispatcher.md) | Review Orchestration Flow Preserved Through the Dispatcher | Unit |
| [01-03](../acceptance-criteria/01-03-secondary-files-verbatim-split.md) | Secondary Files Verbatim Content Split | Unit |
| [01-04](../acceptance-criteria/01-04-frontmatter-and-line-budget-constraints.md) | Frontmatter Validity and SKILL.md Line-Budget Constraints | Unit |
| [01-05](../acceptance-criteria/01-05-skill-usage-docs-consistency.md) | `docs/skill-usage.md` Consistency With the Dispatcher Rewrite | Manual |

## Original Criteria Overview

1. `skill/SKILL.md` frontmatter and body describe a `/atcr <command> <flags>` dispatcher that enumerates and routes to the live Cobra command inventory (`cmd/atcr/main.go:185-208`), with no invented or drifted command names.
2. The existing review-flow orchestration (range resolution, background review + status polling, host review, reconcile, report, output path) remains fully intact and reachable through the dispatcher, with Host Review Instructions, Ambiguity Adjudication, and Findings Format Reference content moved verbatim into secondary markdown files loaded on demand.
3. `skill/SKILL.md` stays within the ~500-line budget, and `docs/skill-usage.md` is verified (and updated if needed) to still accurately describe the dispatcher's installation, usage, and `.atcr/reviews/<id>/` artifact output per AC2.

## Technical Considerations

- **Implementation Notes:** Keep Level 2 content (SKILL.md body) to routing logic, command-selection guidance, and a compact per-command summary; push the verbose Host Review Instructions, Ambiguity Adjudication, and Findings Format Reference sections into separate files under `skill/` (e.g. `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`), referenced from SKILL.md so they load only when that code path is actually exercised. Treat `cmd/atcr/main.go:185-208` as ground truth over any stale spec snapshot (the existing `cobra.md` package doc is known to reference a non-existent `anchor` subcommand and omit newer commands — do not propagate that drift).
- **Integration Points:** No engine code changes — this is purely a Markdown/skill-surface rewrite. The dispatcher must continue to invoke the `atcr` binary exactly as today (never reach into the engine directly), per the existing "Orchestration Steps" convention already established in SKILL.md.
- **Data Requirements:** None — no schema or data-model changes. Artifact layout under `.atcr/reviews/<id>/` (payload/, sources/pool/, sources/host/, reconciled/) is unchanged and must continue to match `docs/skill-usage.md`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Dispatcher rewrite regresses the existing review→reconcile→report orchestration relied on by current adopters | High | Preserve all existing orchestration steps and Host Review/Ambiguity Adjudication instructions verbatim in secondary files; the dispatcher changes only the entry surface, not the underlying flow |
| Command-routing table drifts from the actual Cobra command tree (invented aliases or missing commands) | Medium | Treat `cmd/atcr/main.go:185-208` as the single source of truth; cross-check every routed command name against that registration, not against the stale `cobra.md` snapshot |
| Moving detailed instructions to secondary files breaks on-demand loading (e.g., broken file references) causing the agent to silently skip host-review or adjudication behavior | Medium | Verify each secondary-file reference resolves to an actual file under `skill/` and manually trace the dispatcher for at least one full review-flow invocation before considering the rewrite complete |
| SKILL.md exceeds the ~500-line budget once command-routing logic for ~20 commands is added | Low | Keep per-command entries terse (one line each) in Level 2 and defer any command-specific deep-dive content to Level 3 secondary files, following the same pattern used for host-review/adjudication content |

---

**Created:** July 11, 2026 01:48:34PM
**Status:** Acceptance Criteria Generated
