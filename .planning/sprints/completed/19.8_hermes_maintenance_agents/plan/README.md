## Overview
Plan 19.8 configures the already-live external hermes agents (nucleus.lan:`~/docker/hermes/`) to consume Epic 19.7's `atcr models check --json` primitive across three separated roles — mechanical (no-LLM slug-bump PR), judgment (classify drift), and drafting (LLM-drafted persona re-tune PR). Every change lands as a PR, is fixture-gated in CI by Epic 19.6's reused C3 guardrails, and prompt changes require explicit human approval. The atcr-repo footprint is thin: CI workflow wiring, a configuration-surface doc, and a hermes-host provisioning runbook — the agent framework itself is external and out of scope.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/19.8_hermes_maintenance_agents/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/19.8_hermes_maintenance_agents/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.8_hermes_maintenance_agents/`
- [ ] **Execute Sprint** - `/execute-sprint`

## Timeline & Milestones
| Phase | Deliverables |
|-------|--------------|
| Phase 1: CI Fixture-Gate Confirmation | Verified/extended `.github/workflows/ci.yml` required check reachable by agent-authored PRs |
| Phase 2: Auto-Merge Policy | Opt-in, narrowly-scoped auto-merge workflow for mechanical slug-bump PRs only |
| Phase 3: Configuration Surface Docs | `docs/hermes-maintenance-agents.md` — role→agent, drafting model, dry-run mode |
| Phase 4: Provisioning Runbook | One-time `atcr` binary + repo checkout + `gh` auth setup steps for nucleus.lan |
| Phase 5: Judgment + Drafting Contract Spec | Documented classification rule and vendor-guidance edit contract for hermes skill authoring |

## Resource Requirements
- **Personnel**: 1 maintainer (Sam Estrin)
- **Tools**: GitHub Actions, existing `atcr` CLI (`atcr models check --json`, already shipped), hermes agent framework (external, already live)
- **External Dependencies**: None new — no new Go package required
- **Testing**: `go test ./internal/personas/... ./internal/registry/...` (existing fixture/schema gate, reused unmodified)

## Expected Outcomes
1. **Mechanical toil eliminated**: slug bumps, expiration handling, and missing-slug fixes are opened as PRs automatically, no LLM involved.
2. **Consistent triage**: every drift condition is classified minor (routine bump) vs. major/deprecation (re-tune task with vendor guide URL) without manual review of `atcr models check` output.
3. **Faster, safer re-tunes**: the maintainer reviews a drafted prompt re-tune instead of writing one from scratch, while the guardrail chain (fixture gate + length cap + schema validation) still blocks any bad draft from merging.
4. **No new trust surface**: agents are contributors, never committers — every change is a PR, and the reused 19.6 C3 guardrails treat agent-authored prompts exactly like any other untrusted input.

## Risk Summary
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Auto-merge-on-green misconfigured merges an unintended mechanical change | Low | High | Narrow path/label filter, default off, identical fixture-gate requirement |
| A prompt-edit PR bypasses human review via the mechanical auto-merge filter | Low | Critical | Structurally distinct paths/branches for mechanical vs. prompt PRs; filter never keys on "opened by a bot" |
| Hermes host provisioning drifts, causing false drift reports | Medium | Medium | Pull-before-run step documented in the provisioning runbook |
| Vendor guide fetch fails or is stale, producing a weak draft | Medium | Low | Draft always requires human approval; cost is review time, not correctness |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)

## Related Epics
- **Epic 19.7 (Live Model Resolution)**: shipped; provides `atcr models check --json`, the lock, and the drift signals this plan's agents act on.
- **Epic 19.6 (Community Registry Hub)**: shipped; provides the C3 guardrail chain (fixture gate + length cap + schema validation) reused unmodified.
- **Epic 7.3 (GitHub Action / PR Integration)**: structural doc precedent (`docs/github-action.md`) for this plan's own configuration-surface documentation.
