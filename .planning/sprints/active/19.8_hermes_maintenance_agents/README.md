# Sprint 19.8: Hermes Maintenance Agents

**Type:** 🏗️ Infrastructure (CI/CD Automation)
**Complexity:** 5/12 (MODERATE)
**Timeline:** 5 days · 4 phases
**Branch:** `feature/19.8_hermes_maintenance_agents`
**Execution Mode:** Continuous · Adversarial Review: ENABLED 🎯 (inline CRITICAL/HIGH, defer MEDIUM/LOW)
**Status:** Active — Ready for Refinement

---

## Overview

Configure the already-live external hermes agents (nucleus.lan:`~/docker/hermes/`) to consume Epic 19.7's `atcr models check --json` primitives and offload recurring persona-model maintenance across three separated roles — **mechanical** (deterministic slug-bump PR, no LLM), **judgment** (classify drift minor vs. major/deprecation), and **drafting** (an LLM drafts persona `.md` prompt edits from a vendor guide). Every agent change lands as a reviewable, fixture-gated pull request; prompt changes require human approval; no agent ever commits to `main`.

The in-repo footprint is intentionally thin: one opt-in CI workflow plus one documentation file. The agent configuration itself lives on the hermes host and is documented, not built, here.

## Timeline

| Phase | Focus | Duration | Items |
|-------|-------|----------|-------|
| 1. Foundation | CI gate confirmation + doc skeleton | 1d | Task 01, Task 03 |
| 2. Core Items | Auto-merge workflow (+ adversarial review) + provisioning runbook | 1.5d | Task 02, Task 04 |
| 3. Advanced | Judgment classification rule | 1d | Task 05 |
| 4. Integration & Validation | Drafting agent contract + cumulative adversarial + DoD | 1.5d | Task 06 |

## Expected Outcomes

- `.github/workflows/hermes-auto-merge.yml` — opt-in, off-by-default, structural path/label filter (`*.yaml` only, never `*.md`, never actor-based), minimal permissions, green-`Go CI`-gated.
- `docs/hermes-maintenance-agents.md` — Role→Agent config, drafting model default/fallback, dry-run/preview mode, guardrail contract, provisioning runbook, judgment classification rule, and drafting agent contract.
- Confirmed CI fixture-gate reachability for agent-authored PRs; branch-protection required-check documented as a manual maintainer step.
- All six acceptance criteria (AC1–AC6) traced to completed tasks.

## Risk Summary (top 3)

1. **Critical — prompt-edit PR slips past the mechanical auto-merge filter** → mitigated by a fail-closed allow-list filter keyed on structurally distinct paths (`.yaml` vs `.md`), never on bot authorship; any out-of-allow-list file disqualifies the whole PR. Verified by the Phase 2 adversarial subagent review.
2. **High — auto-merge misconfiguration merges an unintended mechanical change** → mitigated by narrow path/label filter, default off (opt-in repo variable), and the identical fixture-gate requirement every human PR faces.
3. **Medium — hermes host provisioning drifts from the repo, causing false drift reports** → mitigated by a mandatory pull-before-run step documented in the provisioning runbook (Task 04).

Unresolved (documented, not code): the `main` branch-protection required-check setting could not be verified via `gh api` in this environment (403, private-repo tier limit) — handled as an explicit manual maintainer step in the docs.

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase/task plan (source for `/execute-sprint`) |
| [metadata.md](metadata.md) | Sprint tracking + complexity/schedule |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced entries) |
| [plan/](plan/) | Archived source plan (requirements, design, tasks) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | User's request — source of truth |
| [plan/tasks/](plan/tasks/) | Task specifications (Task 01–06) |

---

**Next:** `/refine-sprint @.planning/sprints/active/19.8_hermes_maintenance_agents/`
