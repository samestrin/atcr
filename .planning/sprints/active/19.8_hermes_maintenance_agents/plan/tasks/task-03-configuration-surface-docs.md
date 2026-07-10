# Task 03: Configuration Surface Documentation Skeleton

**Source:** Plan 19.8 – Objective 3
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
There is currently no maintainer-facing documentation of how to assign hermes agents to the mechanical/judgment/drafting roles, which model to use for drafting (and its fallback), or how to preview a change before any PR is opened. Hermes is already live at nucleus.lan:`~/docker/hermes/` with real agent profiles (brian, nolan, marcus, cole) that can be wired into `atcr models check --json` today, but nothing records the configuration contract a maintainer would need to safely turn any of this on. Without this doc, enabling the mechanical/judgment/drafting automation would be an undocumented, tribal-knowledge operation with no recorded off-by-default posture and no specified way to preview a PR before it is opened.

## Solution Overview
Create `docs/hermes-maintenance-agents.md`, mirroring the doc-first structure already established by `docs/github-action.md` (Overview → Usage/config table → required permissions → manual verification). This task establishes the skeleton and the configuration-surface content only: the role→agent table, the drafting-model default/fallback, the dry-run/preview mode, and a guardrail-contract section that cross-references the reused C3 chain. It also stakes out the three section anchors that Tasks 04–06 will populate in the same file (`## Provisioning`, `## Judgment Classification Rule`, `## Drafting Agent Contract`), so later tasks append rather than restructure.

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC4** — documents the PR-only discipline and the human-approval requirement for prompt changes.
- **AC5** — records the reused C3 guardrail contract agent-authored prompt PRs must satisfy.
- **AC6** — captures the role→agent assignment, drafting-model default/fallback, opt-in posture, and dry-run/preview mode.

## Technical Implementation
### Steps
1. Read `docs/github-action.md` in full to confirm the exact structural convention (heading levels, table style, callout phrasing for required permissions, and the "Manual smoke test" pattern) before drafting the new file.
2. Create `docs/hermes-maintenance-agents.md` with the following top-level sections, in order:
   - `## Overview` — one paragraph: hermes is a live, external agent host (nucleus.lan:`~/docker/hermes/`) already scheduling cron-based agent profiles; this doc is the maintainer-facing configuration surface for wiring those agents to `atcr models check --json` (Epic 19.7) across three roles.
   - `## Role → Agent Configuration` — a table with columns `Role | Hermes Profile | Model | Notes`.
   - `## Drafting Model Default & Fallback`
   - `## Dry-Run / Preview Mode`
   - `## Guardrail Contract`
   - `## Provisioning` — placeholder section body: `_To be completed by Task 04 (runtime-provisioning runbook)._`
   - `## Judgment Classification Rule` — placeholder body: `_To be completed by Task 05._`
   - `## Drafting Agent Contract` — placeholder body: `_To be completed by Task 06._`
3. Populate `## Role → Agent Configuration` with exactly these rows (grounded in the SSH-confirmed Hermes Integration Surface, not speculative):
   | Role | Hermes Profile | Model | Notes |
   |------|-----------------|-------|-------|
   | Mechanical | `no_agent` (cron script job) | none (no LLM) | Runs `atcr models check --json`; deterministic slug-bump only, no model attached. |
   | Judgment | `brian` or `cole` | `glm-5.1` (brian) / `kimi-k2.7-code` (cole) | Classifies drift severity per the Judgment Classification Rule (Task 05). |
   | Drafting | `marcus` (default), `nolan` (fallback) | `openai/qwen-3.7-plus` (marcus) / `glm-5.2` (nolan) | Drafts persona prompt re-tune PRs per the Drafting Agent Contract (Task 06); never auto-merges. |
4. In `## Drafting Model Default & Fallback`, state explicitly: default drafting model is marcus/`openai/qwen-3.7-plus`; if marcus is unavailable/erroring, fall back to nolan/`glm-5.2`; fallback selection is a hermes-side cron/skill concern, not an atcr-repo concern — this doc records the contract the hermes profile config must satisfy.
5. In `## Dry-Run / Preview Mode`, specify the contract: before any PR is opened by any agent role (mechanical, judgment-routed, or drafting), the responsible hermes cron job/skill must support a preview mode that prints what it would do — PR title, target branch name, and a diff summary (files touched + line-count delta) — without calling the GitHub PR-creation skill (`data/skills/github/github-pr-workflow`). State that this mode is the default verification step a maintainer runs before flipping an agent from preview to live.
6. In `## Guardrail Contract`, state explicitly and link back to Epic 19.6/this plan's Dependencies: every agent-authored persona-prompt PR is blocked by the same reused C3 guardrail chain as a human PR — schema validation (`internal/registry/validate.go`), length caps (`internal/tools/limits.go`), and the fixture gate (`internal/personas/community_fixture_test.go`, `internal/personas/community_schema_test.go`) — reused unmodified, not re-implemented hermes-side. Also state the off/opt-in posture: mechanical auto-merge-on-green (Task 02) is off by default and must be explicitly enabled per repo; judgment/drafting automation is likewise opt-in — no role runs against a live PR target until the maintainer enables it.
7. Add a short `## Overview` cross-reference note (one sentence) pointing to `docs/github-action.md` as the structural precedent this doc mirrors, and to the plan's Dependencies section (Epic 19.7, Epic 19.6, Epic 7.3) for primitive provenance.

## Files to Create/Modify
- `docs/hermes-maintenance-agents.md` – new file: doc skeleton plus Overview, Role→Agent Configuration, Drafting Model Default & Fallback, Dry-Run/Preview Mode, and Guardrail Contract sections, plus placeholder anchors for Provisioning / Judgment Classification Rule / Drafting Agent Contract (populated by Tasks 04–06 in the same file).

## Documentation Links
- [GitHub Action Docs](../../../../docs/github-action.md) — structural precedent (Overview → Usage/config table → required permissions → manual verification)

## Related Files (from codebase-discovery.json)
- `docs/github-action.md`
- `action.yml`

## Success Criteria
- [x] `docs/hermes-maintenance-agents.md` created with Overview, Role→Agent Configuration table, Drafting Model Default & Fallback, Dry-Run/Preview Mode, and Guardrail Contract sections
- [x] Section anchors present (with clear placeholder text) for `## Provisioning`, `## Judgment Classification Rule`, `## Drafting Agent Contract` — to be filled by Tasks 04–06 without needing to restructure the file
- [x] Opt-in, off-by-default posture explicitly stated for mechanical auto-merge and for judgment/drafting automation
- [x] Dry-run/preview mode behavior (PR title, branch, diff summary, no PR-creation call) specified in enough detail for a hermes skill to implement it
- [x] Role→Agent table matches the SSH-confirmed profile/model assignments exactly (no invented agents or models)

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — documentation-only task

**Integration Tests:**
- N/A — documentation-only task; verified by manual read-through comparing section structure against `docs/github-action.md`

**Test Files:**
- N/A

## Risk Mitigation
- Risk: doc drifts from actual hermes-host configuration → Mitigation: role→agent table is grounded in the SSH-confirmed Hermes Integration Surface section of the epic plan (2026-07-08 probe), not speculative.
- Risk: Tasks 04–06 restructure the file instead of extending it → Mitigation: placeholder section anchors are created in this task with exact heading text (`## Provisioning`, `## Judgment Classification Rule`, `## Drafting Agent Contract`) for later tasks to append into.

## Dependencies
- None (this task creates the doc skeleton that Tasks 04, 05, and 06 extend)

## Definition of Done
- [x] `docs/hermes-maintenance-agents.md` exists with all required sections and section anchors
- [x] Role→agent, model default/fallback, and dry-run mode are unambiguously documented
- [x] Off-by-default/opt-in posture is stated without ambiguity
