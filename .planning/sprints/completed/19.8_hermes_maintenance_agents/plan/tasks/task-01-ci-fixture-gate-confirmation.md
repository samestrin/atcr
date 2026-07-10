# Task 01: CI Fixture-Gate Confirmation for Agent-Authored PRs

**Source:** Plan 19.8 – Objective 1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
Persona-model maintenance guardrails (fixture gate, schema validation) already exist in CI but have never been verified as reachable/blocking for bot-authored PRs specifically. Epic 19.8 is about to introduce hermes agents that open PRs mechanically (catalog-diff slug bumps), by judgment (drift classification), and by drafting (LLM-authored persona `.md` edits). Before any of those agents is allowed to open a PR against `main`, it must be confirmed that the same CI gate a human contributor faces — fixture pass/fail on `internal/personas/...` and `internal/registry/...` — applies identically to agent-authored PRs, with no actor-based bypass and no missing required-check wiring that would let a broken persona merge silently.

## Solution Overview
Audited `.github/workflows/ci.yml`'s trigger and job configuration directly (read in full, no `if: github.actor` or bot-exclusion condition present anywhere in the file) and confirmed `go test ./...` in the `ci` job's "Run Tests" step covers `internal/personas/...` and `internal/registry/...` unconditionally on every `pull_request` event, regardless of PR author. No gap was found, so no workflow code changes are required. The one remaining item — GitHub branch-protection "required check" configuration — is a repo-setting, not code, and could not be verified via `gh api` in this environment (the org tier does not expose the branch-protection endpoint for private repos: `gh api repos/samestrin/atcr/branches/main/protection` returned `403 Upgrade to GitHub Pro or make this repository public`). This is documented below as a manual maintainer step, cross-referenced for Task 03's `docs/hermes-maintenance-agents.md`.

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC1** — ensures the CI fixture gate is reachable and blocking for agent-authored slug-bump PRs.
- **AC4** — verifies no actor-based bypass exists; agent PRs face the same required checks as human PRs.
- **AC5** — confirms the reused 19.6 C3 guardrails (fixture gate, schema validation, length cap) remain on the required CI path.

## Technical Implementation
### Steps
1. Read `.github/workflows/ci.yml` in full (137 lines, 3 jobs: `ci`, `reconcile-module`, `braceparser-module`).
   - Confirmed: `on:` triggers on `push` (branches: `[main]`) and `pull_request` (branches: `[main]`) with no `if:` conditions anywhere at the workflow, job, or step level that reference `github.actor`, `github.event.pull_request.user`, bot logins, or any other author-based filter (lines 1-137, full file scanned).
   - The only conditional logic present is event-type branching (`github.event_name == 'pull_request'` vs. else, line 60-64) to pick fast `go test ./...` vs. full `-race -coverprofile` — this branches on event type, not actor, so it applies identically to human and hermes-agent PRs.
2. Confirmed `go test ./...` (ci.yml:61, PR path) exercises `internal/personas/...` and `internal/registry/...`.
   - Verified `internal/personas` and `internal/registry` live under the repo-root `go.mod` (only `./reconcile` and `./internal/astgroup/parsers/src/braceparser` have their own nested `go.mod` boundaries requiring the two dedicated jobs at ci.yml:82-136) — so the root `go test ./...` reaches both packages with no extra job needed.
   - Confirmed the specific fixture gate exists and is reachable: `internal/personas/community_fixture_test.go:16` `TestTemplateFixtureRunner_CommunityPersonasPass`, and `internal/registry/validate.go:49` `ValidateCommunityPersonaYAML` is the strict schema check the fixture test and `internal/personas/community_schema_test.go` exercise.
3. Gap check result: **no gap found**. The workflow's `pull_request` trigger and `ci` job have no actor exclusion, so hermes-agent-authored PRs hit the identical `gofmt` + `golangci-lint` + `go test ./...` pipeline as human PRs. **No workflow edit was made** — this task's diff is intentionally empty for `.github/workflows/ci.yml`.
4. Documented the required-check manual step (this task) for hand-off to Task 03: the GitHub Settings → Branches → Branch protection rule for `main` must list the `Go Lint & Test` check (job name at ci.yml:18, surfaces in GitHub's PR UI as "Go CI / Go Lint & Test") as a **required status check**. This could not be confirmed programmatically in this session (`gh api repos/samestrin/atcr/branches/main/protection` → `403`, private repo without GitHub Pro). Task 03 must record this as a maintainer action item in `docs/hermes-maintenance-agents.md`.

## Files to Create/Modify
- `.github/workflows/ci.yml` – audited only; **no changes made** (no actor-exclusion gap found)

## Documentation Links
- [Documentation Source Index](../documentation/source.md)

## Related Files (from codebase-discovery.json)
- `.github/workflows/ci.yml`
- `internal/personas/community_fixture_test.go`
- `internal/personas/community_schema_test.go`
- `internal/registry/validate.go`

## Success Criteria
- [x] `.github/workflows/ci.yml`'s Go Lint & Test job is confirmed to run unconditionally on every `pull_request`, including hermes-agent-authored ones (no actor-based exclusion) — confirmed by full-file read, no `github.actor`/bot-exclusion conditions found
- [x] `go test ./internal/personas/... ./internal/registry/...` coverage within the CI job is confirmed — both packages sit under the root `go.mod`, reached by `go test ./...` at ci.yml:61
- [x] The GitHub branch-protection "required check" manual step is documented (cross-referenced to Task 03's doc) — could not be verified via `gh api` in this environment (403, private repo tier limit); recorded as an open maintainer action for Task 03
- [x] No gap was found, so no workflow extension was applied (per the "extend only if needed" scope)

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — this task audits/confirms existing CI wiring; no new Go code is introduced

**Integration Tests:**
- Full-file read of `.github/workflows/ci.yml` substituted for a live workflow-inspection dry-run (no `act`/local-runner available in this environment); confirmed via static analysis of triggers, conditions, and the "Run Tests" step that the check fires unconditionally on `pull_request`

**Test Files:**
- N/A (confirmation-only task; existing suite already covers persona/registry guardrails via `internal/personas/community_fixture_test.go` and `internal/personas/community_schema_test.go`)

## Risk Mitigation
- Risk: silently assuming the check is already required without confirming branch-protection settings → Mitigation: explicitly flagged that `gh api repos/samestrin/atcr/branches/main/protection` is inaccessible under the current GitHub plan/repo visibility, and handed the required-check verification off as an explicit manual maintainer step for Task 03's documentation rather than assuming it is already configured.

## Dependencies
- None (foundational task; other tasks reference its confirmation)

## Definition of Done
- [x] CI fixture-gate reachability confirmed for agent-authored PRs (no actor-based exclusion in `.github/workflows/ci.yml`)
- [x] No workflow extension was necessary (no gap found); `.github/workflows/ci.yml` left unmodified
- [x] Required-check manual step documented for the maintainer, including the `gh api` verification blocker encountered, for hand-off to Task 03's `docs/hermes-maintenance-agents.md`
