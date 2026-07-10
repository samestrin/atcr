# Task 02: Opt-In Auto-Merge Policy for Mechanical PRs Only

**Source:** Plan 19.8 – Objective 2
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
Mechanical slug-bump PRs (advancing a persona's model lock in `personas/community/*.yaml` when Epic 19.7's `atcr models check --json` detects a newer family member, an expiring slug, or a missing slug) are routine, deterministic, string/timestamp-comparison changes with no LLM judgment involved — they are safe to merge automatically once CI is green. Prompt re-tune PRs (editing a persona's `.md` body against a vendor's prompting guide) are the opposite: a flat-rate open model summarizing a *different* vendor's own guidance is a cross-vendor judgment call that must always get human review. Today nothing in the repo distinguishes these two PR shapes at the CI/workflow level. A naive rule such as "auto-merge anything opened by the hermes bot" would auto-merge both, letting an unreviewed prompt re-tune land on `main` — flagged as a Critical-impact risk in plan.md's Risk Mitigation table.

## Solution Overview
Create `.github/workflows/hermes-auto-merge.yml`: a new, opt-in (disabled/off-by-default), narrowly-scoped GitHub Actions workflow that auto-merges a PR only when both hold:
1. **Structural filter match** — the PR touches only mechanical-slug-bump paths (`personas/community/*.yaml` and/or a models lockfile), and/or carries the `hermes:mechanical` label. It must never match `personas/community/*.md` (prompt-edit) paths.
2. **Fixture-gate green** — the same required `Go CI` (`Go Lint & Test`, job `ci` in `.github/workflows/ci.yml`) check that gates every human PR has passed.

The filter is path/label-based only — it is never keyed on `github.actor`, PR author, or "opened by a bot," per plan.md's Risk Mitigation table ("gate the auto-merge workflow's trigger condition on that path filter, never on 'opened by a bot'"). This keeps a prompt-edit PR structurally incapable of matching the mechanical filter, regardless of who or what opened it.

> **Lock-file note:** In this repository the resolved slug lock is the `model:` field inside `personas/community/<slug>.yaml`; there is no separate lockfile. The structural filter therefore targets the YAML files themselves.

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC1** — enables automatic merging of mechanical slug-bump PRs once the fixture gate passes.
- **AC4** — preserves the PR-only, human-approval-for-prompts contract by structurally excluding prompt-edit PRs from auto-merge.

## Technical Implementation
### Steps
1. Create `.github/workflows/hermes-auto-merge.yml` triggered on `pull_request` (types: `opened`, `synchronize`, `labeled`) and/or `check_suite`/`check_run` `completed`, so the workflow can react both to PR changes and to the fixture-gate check finishing.
2. Gate the entire workflow behind an opt-in switch — e.g. a repository variable (`vars.HERMES_AUTO_MERGE_ENABLED == 'true'`) checked at the top of the job — so the workflow is inert until a maintainer explicitly enables it. Default: disabled.
3. Implement the structural filter as a job condition/step using `dorny/paths-filter` (or `git diff --name-only` against the base ref) to confirm every changed file matches `personas/community/*.yaml` or a models lockfile path, combined with (not substituted by) an `contains(github.event.pull_request.labels.*.name, 'hermes:mechanical')` check. Explicitly reject the run (no-op) if any changed file matches `personas/community/*.md` or any other path outside the allow-list.
4. After the filter passes, poll/require the `Go Lint & Test` check (from `.github/workflows/ci.yml`, job `ci`) to be `success` — either via `gh pr checks` or by triggering off that check's `check_run` completion event — before invoking merge (e.g. `gh pr merge --auto --squash` or the GitHub REST merge API).
5. Set workflow permissions to the minimum needed: `contents: write` (to merge) and `pull-requests: write` (to label/comment) — nothing broader (no `checks: write`, no `actions: write`).
6. Add an explicit inline comment/guard block in the YAML stating the filter must never be based on `github.actor`, `github.event.pull_request.user.login`, or any "is this a bot" check — structural path/label match is mandatory and is the only permitted gate.
7. Document this workflow's opt-in toggle (the repository variable name and how a maintainer flips it) and the filter design (paths allow-listed, label used, explicit prompt-path exclusion) in `docs/hermes-maintenance-agents.md` (coordinate with Task 03, which owns that file's overall structure).

## Files to Create/Modify
- `.github/workflows/hermes-auto-merge.yml` – new opt-in auto-merge workflow, off by default

## Documentation Links
- [Documentation Source Index](../documentation/source.md)
- `docs/github-action.md` — structural precedent for permissions documentation pattern (Overview → Usage/config table → required permissions)

## Related Files (from codebase-discovery.json)
- `.github/workflows/ci.yml` — source of the required `Go Lint & Test` (job `ci`) fixture-gate check name and the `[self-hosted, gauntlet]` runner group to reuse
- `.github/workflows/reconcile-module.yml`
- `.github/workflows/refresh-synthetic-manifest.yml`
- `personas/community/*.yaml` — mechanical-slug-bump path the filter must match
- `personas/community/*.md` — prompt-edit path the filter must never match

## Success Criteria
- [x] `.github/workflows/hermes-auto-merge.yml` exists, is disabled/opt-in by default
- [x] Filter matches only mechanical-slug-bump changes (lockfile/`*.yaml`, never `*.md`) and/or a `hermes:mechanical` label
- [x] Auto-merge only proceeds when the `Go CI` (`Go Lint & Test`) fixture-gate check is green
- [x] Workflow permissions scoped to `contents: write` + `pull-requests: write` only
- [x] No condition anywhere keys on bot authorship alone

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — this is a GitHub Actions YAML workflow, not Go code

**Integration Tests:**
- Open a mock PR touching only `personas/community/*.yaml` with the `hermes:mechanical` label and confirm the workflow would trigger auto-merge only after `Go CI` succeeds
- Open a mock PR touching a `personas/community/*.md` file and confirm the workflow does NOT match/trigger, even with a bot author
- Confirm the workflow does nothing when the opt-in repository variable is unset/false, even if a PR would otherwise match the filter

**Test Files:**
- N/A (workflow-level verification via `act` or a scratch PR, not a Go test file)

## Risk Mitigation
- Risk (Critical): a prompt-edit PR is mistakenly matched by the mechanical auto-merge filter, bypassing human review → Mitigation: filter is path/label-based only, structurally distinct from prompt-edit paths, never keyed on bot authorship; documented explicitly in the workflow.
- Risk (High): auto-merge-on-green misconfigured merges an unintended mechanical change → Mitigation: narrow path/label filter, default off, same fixture-gate requirement as any human PR.

## Dependencies
- Task 01 (CI fixture-gate confirmation) — this workflow depends on the `Go CI` check name/behavior confirmed there

## Definition of Done
- [x] Opt-in auto-merge workflow created and default-disabled
- [x] Path/label filter verified to exclude all prompt-edit PR shapes
- [x] Permissions minimally scoped
