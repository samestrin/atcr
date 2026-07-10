# Task 04: Hermes Host Provisioning Runbook

**Source:** Plan 19.8 – Objective 4
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
The mechanical maintenance agent's `no_agent` cron job on nucleus.lan cannot run `atcr models check --json` or open PRs until the `atcr` binary, a working repo checkout, and GitHub authentication exist on that host. This one-time runtime provisioning is currently undocumented and unprovisioned, making it a hard prerequisite blocker for the mechanical agent (Task 01/02) regardless of how correct its cron job configuration is.

## Solution Overview
Write a `## Provisioning` section in `docs/hermes-maintenance-agents.md` (created by Task 03) documenting the one-time steps: build/install the `atcr` binary on nucleus.lan, clone an `atcr` repo checkout under `~/docker/hermes/`, configure `gh` auth for PR opening, and wire a pull-before-run step into the mechanical cron job so drift checks always run against current personas/lock state rather than a stale checkout. This task is documentation only — no `cmd/atcr` code changes.

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC1** — provides the one-time runtime prerequisite so the scheduled mechanical agent can run `atcr models check --json` and open PRs.

## Technical Implementation
### Steps
1. Append a `## Provisioning` section to `docs/hermes-maintenance-agents.md` (from Task 03).
2. Document installing the `atcr` binary on nucleus.lan (build from source via `go build ./cmd/atcr` or copy a release binary) and placing it on `PATH` for the hermes cron execution environment.
3. Document establishing a repo checkout under `~/docker/hermes/` (or an adjacent path) with `gh auth login` (or an equivalent token-based auth, e.g. `GH_TOKEN`/`GITHUB_TOKEN` env var scoped for PR creation) so the mechanical agent's cron script can open PRs via `gh pr create`.
4. Document the pull-before-run requirement: the `no_agent` cron script must `git pull` (or equivalent fetch + fast-forward) against the current default branch before invoking `atcr models check --json`, so drift is evaluated against current personas/lock state, not a stale checkout.
5. Reference the hermes cron job shape (`data/profiles/<agent>/cron/jobs.json`, `no_agent: true` + `script` field) and brian's `fleet-sweep` job (`7 3 * * *`, SSHes the fleet and writes a status file) as a structural precedent for the mechanical job's script entry.
6. Note explicitly that this runbook is documentation only — no `cmd/atcr` code changes are required or introduced by this task.

## Files to Create/Modify
- `docs/hermes-maintenance-agents.md` – append `## Provisioning` section (same file created by Task 03)

## Documentation Links
- [GitHub Action Docs](../../../../docs/github-action.md) — required-permissions callout precedent

## Related Files (from codebase-discovery.json)
- `cmd/atcr/models.go`

## Success Criteria
- [ ] `## Provisioning` section documents atcr binary install, repo checkout location, and `gh` auth setup on nucleus.lan
- [ ] Pull-before-run step is explicitly specified as a prerequisite of every mechanical cron invocation
- [ ] Section cross-references the hermes `no_agent` cron job shape (`cron/jobs.json`)
- [ ] No `cmd/atcr` code changes introduced by this task

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — documentation-only task

**Integration Tests:**
- Manual: follow the runbook once on nucleus.lan to confirm `atcr models check --json` runs successfully from the provisioned checkout (out-of-repo verification, tracked by the maintainer, not by CI)

**Test Files:**
- N/A

## Risk Mitigation
- Risk (Medium): hermes host provisioning drifts from the atcr repo (stale binary/checkout), causing false drift reports → Mitigation: pull-before-run step documented as mandatory in the runbook so every cron invocation checks out current `main` before running `atcr models check`.

## Dependencies
- Task 03 (Configuration Surface Documentation Skeleton) — this task extends the same `docs/hermes-maintenance-agents.md` file

## Definition of Done
- [ ] Provisioning runbook section written and appended to docs/hermes-maintenance-agents.md
- [ ] Pull-before-run requirement unambiguous
