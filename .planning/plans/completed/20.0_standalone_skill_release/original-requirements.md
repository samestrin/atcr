# Original Requirements

**Date:** July 11, 2026 12:35:17PM
**Arguments:** `@.planning/epics/active/20.0_standalone_skill_release.md`
**Target:** `.planning/epics/active/20.0_standalone_skill_release.md`

## Content

# Feature Request: Standalone ATCR Skill Distribution

- **Estimated time**: 1 week
- **Tasks/Components**: TBD
- **Execution**: init-plan

## Context

The `atcr` engine has proven to be significantly more robust than the legacy code review system it was based on. Currently, we have tightly coupled, project-specific private skills (`execute-code-review`, `reconcile-code-review`) that rely on our internal `.planning/` directory structure. However, we have also developed a standalone, repository-agnostic skill (`skill/SKILL.md`) that operates entirely within a local `.atcr/` directory.

## Problem Statement

We want to release `atcr` as a public engine alongside its standalone skill, allowing anyone to run multi-agent code reviews on any repository without needing our private `.planning/` sprint workflow. At the same time, we must ensure that `atcr` continues to seamlessly support our internal, private skill workflows so that we get the best of both worlds.

## Proposed Solution

1. Document the existing `go install github.com/samestrin/atcr/cmd/atcr@latest` path as the install method for a clean repository. Real binary packaging/release automation is tracked separately in Epic 21.0 and is not this epic's job.
2. Publish and document the standalone skill (`skill/SKILL.md`) as a **single dispatcher skill** using a `/atcr <command> <flags>` UX pattern. Rather than distributing many separate skills, `skill/SKILL.md` will act as a lightweight router that invokes the underlying `atcr` CLI commands.
3. Migrate the private `claude-prompts` skills to this same monolithic dispatcher pattern. By unifying both public and private workflows under a single, clean `/atcr` namespace, we prioritize the quality and discoverability of the OSS product over the legacy 9+ separate skills.
4. Add a repo-local end-to-end test/check proving the `--output-dir` + reconcile contract (documented in `docs/code-review-backend.md`) that private-skill consumers depend on remains stable. This does not touch the private skills themselves — Epic 12.0 already validated that integration end-to-end from the external (`claude-prompts`) side.
5. Write `install.sh`, the one confirmed gap (no install script exists anywhere in the repo today). Document that `atcr doctor` (self-test) and `atcr quickstart` (onboarding wizard) already satisfy the self-test / quick-start requirement without depending on private APIs — do not build new commands.

## Acceptance Criteria

- [ ] AC1: The `atcr` binary can be installed and run on a clean repository without a `.planning` folder (via `go install`).
- [ ] AC2: The standalone skill (`skill/SKILL.md`) executes successfully, storing all artifacts under `.atcr/reviews/<id>/` — already satisfied by `docs/skill-usage.md`; verify it stays accurate.
- [ ] AC3: The engine remains fully backward-compatible with our private `.planning/` skills (`execute-code-review`, `reconcile-code-review`), verified via a repo-local test against the `docs/code-review-backend.md` contract (not by re-touching the external skill repo).
- [ ] AC4: An `install.sh` script is provided for external developers; the quick-start guide requirement is satisfied by the existing `docs/skill-usage.md` + `atcr quickstart` wizard.

## Out of Scope

- Modifying the existing private `.planning/` skills to use `.atcr/` (they should remain integrated with our sprint workflow).
- Binary packaging, versioning/tagging, goreleaser, or any release automation — tracked in Epic 21.0 (Release & Packaging Automation).
- Building a new `atcr self-test` command — `atcr doctor` already fills this role (naming decision recorded in epic 1.2).
- Re-validating `execute-code-review`/`reconcile-code-review` themselves — already validated end-to-end by Epic 12.0.

## References

- `docs/skill-usage.md` — standalone skill install/usage guide (already satisfies AC2).
- `docs/code-review-backend.md` — `--output-dir` backend contract (AC3 surface).
- Epic 12.0 (Skill Integration) — already validated the private-skill backward-compatibility end-to-end.
- Epic 21.0 (Release & Packaging Automation) — owns binary packaging/release, descoped from this epic.

## Refinements (2026-07-03)

This section records findings from `/refine-epic` run on July 03, 2026 12:24:08PM. It is additive — original plan content above is preserved.

### Auto-applied corrections (0)

No mechanical corrections were needed — no typo'd paths, no structural gaps that qualify as SAFE_AUTOFIX.

### Items needing user confirmation (4)

⏸️ **T1 conflicts with a prior explicit scope decision:** Proposed Solution #1 ("Package and distribute the `atcr` engine for public release") proposes exactly the work that two prior epics' recorded clarifications explicitly declined to take on. `.planning/.knowledge/clarifications-16.0_quick_start-Q1.md` states: "Cutting a first release is not a cheap workflow-template fix: the repo has no release automation (no goreleaser config, no tag-triggered release workflow) anywhere under .github/workflows/, and `git tag`/`gh release list` are both empty. Establishing a first release is a repo-wide lifecycle decision, not something a single epic's task list should absorb." Epic 7.3's clarification (`clarifications-7.3_github_action_pr_integration-Q2.md`) made the same call, scoping "release artifacts/goreleaser" out. Suggested action: either explicitly override that prior decision here and say so, or descope T1 to "document the `go install`-based install path" (already how README.md and docs/skill-usage.md work today) and route actual release/packaging automation to its own dedicated epic.

⏸️ **Ambiguous T3 — no concrete success criterion, possible external-repo scope:** "Validate that the engine's core features... work flawlessly for both the public standalone skill and the internal private skills" names no specific test suite, scenario, or pass/fail threshold. It's also unclear whether validating "the internal private skills" requires touching `execute-code-review`/`reconcile-code-review`, which live outside this repo (`~/Documents/GitHub/claude-prompts/.claude/skills/`) — Epic 12.0's own `/refine-epic` run hit exactly this problem: "The `/execute-epic` skill operates on the current repo and cannot stage, commit, or merge changes in an external repo." Suggested rewrite: name a specific atcr-repo-local validation artifact (e.g., an end-to-end `atcr review --output-dir` + `atcr reconcile` run checked against the documented output tree) rather than a cross-repo claim.

⏸️ **T4 names a rejected command and restates already-shipped work:** "self-test mechanisms (e.g. `atcr self-test`)" cites a command name that does not exist and was explicitly rejected — `.planning/.knowledge/clarifications-1.2_model_endpoint_selftest-Q1.md` records the decision: "Use `atcr doctor`... `atcr selftest` is verbose with no precedent advantage." `atcr doctor` (`cmd/atcr/doctor.go:20`) already ships as this exact self-test mechanism (epic 1.2). Likewise, "a quick-start guide" already exists as the `atcr quickstart` CLI wizard (`internal/quickstart/`, shipped under epic 16.0) plus the written `docs/skill-usage.md` guide. Suggested action: rewrite T4 to name what's actually missing (see next item) rather than re-proposing artifacts that already ship.

⏸️ **AC2 is substantially already satisfied, uncited in the plan:** `docs/skill-usage.md` (not mentioned anywhere in the epic) already documents `skill/SKILL.md`'s installation and usage and explicitly states artifacts land under `.atcr/reviews/<id>/` — the exact AC2 requirement. `docs/code-review-backend.md` further documents the `--output-dir` contract that private skills rely on, directly relevant to AC3. Suggested action: cite these two existing docs in the plan and scope remaining work to the genuine gap (an install script — see Advisory observations below), rather than re-deriving them from scratch.

### Advisory observations (3)

ℹ️ **Scope-guard violation:** Derived TASK_COUNT=6, COMPONENT_COUNT=4 (release/packaging, skill/docs publishing, docs/quick-start + install script, engine-wide validation) — exceeds `/execute-epic`'s ≤6 tasks / ≤2 components limit. The plan's own metadata line already states `**Execution**: init-plan`, meaning the author had already flagged this epic for the full `/init-plan` pipeline rather than `/execute-epic`. Running `/execute-epic` against this plan as-is will hit the Phase 1 Step 4 HARD STOP.

ℹ️ **Genuinely missing piece:** Confirmed no `install.sh` (or any `install*.sh`) anywhere in the repo, and no goreleaser/release workflow. This is the one part of AC4 ("installation script") that is real, net-new work, distinct from the already-shipped `atcr quickstart` wizard and `docs/skill-usage.md` guide.

ℹ️ **Cross-system signal:** "Public release" implies a new external distribution channel (GitHub Releases / package manager), which the codebase has explicitly treated as a repo-wide lifecycle decision (see T1 finding above) rather than epic-scoped work — reinforces the `/init-plan` recommendation over `/execute-epic`.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 6 (limit: 6)
- Derived COMPONENT_COUNT: 4 (limit: 2)
- COMPONENTS_TOUCHED: release/packaging (root + .github/workflows/), skill/ (SKILL.md publishing), docs/ (quick-start guide + install script), internal/* engine validation (fanout, reconcile, report, etc.)
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: true (public distribution channel)
- Cited references checked: 4 (skill/SKILL.md, execute-code-review, reconcile-code-review, atcr self-test)
- Codebase search queries (spot-check): ["goreleaser|packaging|release binary|homebrew|distribut", "install.sh|installation script|quick-start guide|quickstart", "public release|external developers?|open[- ]source"]
- Deep discovery method: semantic
- Deep discovery queries: ["public release packaging distribution of atcr engine and standalone skill", "standalone skill documentation quick start guide external developers", "self-test doctor command clean repository no planning folder", "install script binary release goreleaser github actions", "private planning skill execute-code-review reconcile-code-review backward compatibility"]
- Deep discovery match count: 11
- Deep discovery snapshot: /Users/samestrin/Documents/GitHub/atcr/.planning/.temp/refine-epic/codebase-discovery.json (temp-only — not committed)

## Refinements (2026-07-03) — Follow-up: corrections & decisions

All 4 user-confirm items above were reviewed and accepted. The plan body (Proposed Solution, Acceptance Criteria, Out of Scope, References) was updated directly to reflect these decisions.

### Decisions (resolved)

- **D1 — Packaging/release automation is out of scope here (supersedes the original Proposed Solution #1):** Rather than overriding the prior 16.0/7.3 scope decision, a new standalone Epic 21.0 (Release & Packaging Automation) was created to own that work. This epic keeps AC1 satisfied via the existing `go install` path only.
- **D2 — T3 rewritten to a repo-local validation artifact:** "Validate... work flawlessly for both" is replaced with a repo-local end-to-end check against the documented `docs/code-review-backend.md` contract. Re-validating the external private skills themselves is explicitly out of scope — Epic 12.0 already covers that.
- **D3 — T4 rewritten, no new command:** The plan no longer proposes an `atcr self-test` command. It documents that `atcr doctor` and `atcr quickstart` already satisfy the requirement, and narrows the remaining work to `install.sh`.
- **D4 — AC2/AC3 documentation citations added:** `docs/skill-usage.md` and `docs/code-review-backend.md` are now cited directly in the plan (References section) instead of being re-derived.

**Net effect:** the plan's real remaining work is now approximately 2 tasks (a repo-local backward-compat test; `install.sh` + doc citations) across 1-2 components, which likely brings it back under `/execute-epic`'s ≤6 tasks / ≤2 components guard. Recommend re-running `/refine-epic` to confirm before choosing between `/execute-epic` and `/init-plan`.

## Refinements (2026-07-05) — Addendum Override: The Dispatcher Pattern

**OVERRIDE:** The July 03 decision to use separate, single-capability skills has been explicitly overturned. We are adopting a **single dispatcher skill** (`/atcr <command>`) for both the public OSS product and the private internal tools.

**Rationale for Override:**
- **Product Quality over Legacy Convenience**: The public OSS UX must be clean and discoverable. A single `/atcr` entrypoint is far superior to forcing users to install and manage a fragmented ecosystem of 9+ different skills.
- **Unification**: We will update the private `claude-prompts` repo to match this dispatcher pattern, bringing both public and private architectures into alignment under a modern, unified CLI-style UX.
- **Prompt Size Management**: To prevent the single `SKILL.md` from bloating past the 500-line context window limit, the router will simply map user intents to underlying `atcr` CLI commands or load deep instructions from secondary markdown files on the fly.

## Refinements (2026-07-05) — Unified Dispatcher Audit

This section records findings from the `/refine-epic --deep` run on July 05, 2026 01:24:12PM. It is additive — original plan content above is preserved.

### Auto-applied corrections (0)

No automatic corrections were applied.

### Items needing user confirmation (2)

⏸️ **Header Tasks/Components Mismatch:** The header currently lists `- **Tasks/Components**: TBD`. Our derived counts are `5 / 5` (which exceeds the `/execute-epic` component limit). Recommend updating the metadata line to `- **Tasks/Components**: 5/5` to accurately reflect the plan contents.

⏸️ **Cross-repository write limitation:** Proposed Solution #3 requires migrating skills in the external `claude-prompts` repository. Because the agent only has access to the `/Users/samestrin/Documents/GitHub/atcr` workspace, we cannot directly edit files in `claude-prompts`. Recommend clarifying that the agent will only write the local `skill/SKILL.md` dispatcher template in this workspace, and that the migration of the actual private skill files to the external repository is descoped/marked as a manual operator action.

### Advisory observations (2)

ℹ️ **Scope-guard violation:** Derived `TASK_COUNT` = 5, `COMPONENT_COUNT` = 5 — exceeds the `/execute-epic` limit of 2 components. It also targets files in an external repository (`claude-prompts`), which violates the single-repo execution boundary. Therefore, this plan cannot be run via `/execute-epic` and must run through the `/init-plan` pipeline.

ℹ️ **Verified path accuracy:** Checked that `skill/SKILL.md`, `docs/skill-usage.md`, and `docs/code-review-backend.md` exist. Also verified that `install.sh` does not exist (the net-new task target) and that the self-test/wizard commands (`doctor`, `quickstart`) exist in `cmd/atcr/`.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 5 (limit: 6)
- Derived COMPONENT_COUNT: 5 (limit: 2)
- COMPONENTS_TOUCHED: [skill/, install.sh, docs/, internal/verify, claude-prompts]
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: true (public release/distribution, external repository `claude-prompts` changes)
- Cited references checked: 6 (skill/SKILL.md, docs/skill-usage.md, docs/code-review-backend.md, install.sh, atcr doctor, atcr quickstart)
- Codebase search queries (spot-check): ["doctor.go", "quickstart.go"]
- Deep discovery method: semantic
- Deep discovery queries: ["unification of dispatcher skills monolithic skill.md router", "public standalone skill distribution router UX pattern atcr command", "private skill claude-prompts unified atcr dispatcher routing", "reconcile contract test output-dir verify cmd integration test"]
- Deep discovery match count: 11
- Deep discovery snapshot: /Users/samestrin/Documents/GitHub/atcr/.planning/.temp/refine-epic/codebase-discovery.json (temp-only — not committed)

## Purpose

This document is the source-of-truth capture of the original request for this plan. It is preserved verbatim (with all refinement history) to anchor downstream planning artifacts (plan.md, codebase-discovery.json, user-stories/, acceptance-criteria/) against the original intent.
