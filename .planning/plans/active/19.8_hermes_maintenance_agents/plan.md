## Metadata
- **Plan Type:** infrastructure
- **Last Modified:** 2026-07-09
- **Original Requirements:** [original-requirements.md](original-requirements.md)

## Plan Overview
**Plan Goal:** Configure the already-live external hermes agents (nucleus.lan:`~/docker/hermes/`) to consume Epic 19.7's `atcr models check --json` primitive across three separated roles — mechanical (no-LLM slug-bump PR), judgment (classify drift, route minor vs. major/deprecation), and drafting (LLM-drafted persona prompt re-tune PR) — with every change landing as a PR, fixture-gated in CI, and human-approved on prompt changes.
**Target Users:** The maintainer (Sam Estrin), who currently performs persona-model maintenance (slug bumps, deprecation handling, prompt re-tunes) by hand.
**Framework/Technology:** Go 1.25 (existing `atcr` CLI), GitHub Actions, hermes agent framework (external, nucleus.lan) with cron-scheduled profile agents (brian/nolan/marcus/cole) fronting a LiteLLM proxy.

## Objectives
1. Confirm/extend the existing CI fixture gate (`.github/workflows/ci.yml` + `go test ./internal/personas/... ./internal/registry/...`) so it is a required, blocking check reachable by hermes-agent-authored PRs exactly as it is by human PRs.
2. Add an opt-in, narrowly-scoped auto-merge-on-green policy for mechanical slug-bump PRs only (never for prompt-edit PRs), keyed off a path/label filter distinct from prompt PRs.
3. Write `docs/hermes-maintenance-agents.md` documenting the maintainer-facing configuration surface: role→agent assignment, drafting-model default/fallback, dry-run/preview mode, and the guardrail contract agent-authored PRs must satisfy.
4. Write a one-time runtime-provisioning runbook for installing the `atcr` binary and a pull-before-run repo checkout (with `gh` auth) on the hermes host, as the prerequisite for the mechanical agent's `no_agent` cron job.
5. Record the judgment classification rule (minor bump → mechanical; major bump or deprecation → re-tune task carrying the vendor prompting-guide URL) as part of the documented configuration surface — the actual classification logic runs hermes-side, not in this repo.
6. Record the drafting-agent contract (fetch vendor guide → edit the persona `.md`'s existing `<!-- vendor-guidance: ... -->`-anchored body → open a separate PR → never auto-merge) so hermes-side skill authoring has an unambiguous, repo-anchored spec to build against.

## Scope
### In Scope
- `.github/workflows/` changes: confirming/extending the fixture-gate required check and adding the opt-in mechanical-PR auto-merge policy.
- `docs/hermes-maintenance-agents.md`: the configuration surface (role→agent, drafting model, dry-run mode) and guardrail contract.
- A hermes-host provisioning runbook (documented in `docs/` or the plan's own tasks; no code changes to `cmd/atcr` required since `atcr models check --json` already ships from Epic 19.7).
- Verifying the separate-PR discipline (mechanical vs. prompt) is expressible via CI path/label filters, so a routine bump can merge on green while a prompt change always requires human review.

### Out of Scope
- The resolution/lockfile/`models check` primitives themselves — already shipped in Epic 19.7.
- Community prompt submissions / crowd-sourced battle-tested prompts — a later, separate epic.
- Building or modifying the hermes agent framework itself — hermes is already installed, live, and scheduled (confirmed 2026-07-08 via SSH probe); this plan only configures existing agents against atcr's primitives.
- Auto-merging prompt changes — always human-reviewed, no exceptions.
- Any new `cmd/atcr` command or flag — `atcr models check --json` already satisfies every primitive this plan's agents need.

## Dependencies and Context
- **Epic 19.7 (Live Model Resolution)** — shipped (`f6cdc3d4`); provides `atcr models check [--json]`, the lock, and the drift/expiration/major-bump signals. Confirmed live at `cmd/atcr/models.go:153` (`newModelsCheckCmd`/`runModelsCheck`), whose exit codes (0=clean, 1=conditions found, 2=usage failure) and `--json` output were explicitly authored with this plan's mechanical agent as the documented consumer.
- **Epic 19.6 (Community Registry Hub)** — shipped; provides the C3 untrusted-input guardrail chain (schema validation in `internal/registry/validate.go`, length caps in `internal/tools/limits.go`, fixture gate in `internal/personas/community_fixture_test.go` + `community_schema_test.go`) this plan reuses unmodified.
- **Epic 7.3 (GitHub Action / PR Integration)** — the existing composite Action (`action.yml`, `docs/github-action.md`) is a structural doc/config-surface precedent this plan's own docs mirror; it is not itself modified.
- **Hermes host (nucleus.lan:`~/docker/hermes/`)** — external, already live. Agent-role → hermes-profile mapping confirmed via SSH probe 2026-07-08: mechanical = `no_agent` cron script (no model); judgment = brian (`glm-5.1`) or cole (`kimi-k2.7-code`); drafting = marcus (`openai/qwen-3.7-plus`) default, nolan (`glm-5.2`) fallback.

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Generated
- **Estimated Count:** 6 tasks

## Tasks

The plan is decomposed into the following executable task files. Each task contains its problem statement, technical implementation steps, and success criteria.

- [Task 01: CI Fixture-Gate Confirmation for Agent-Authored PRs](tasks/task-01-ci-fixture-gate-confirmation.md) — Objective 1
- [Task 02: Opt-In Auto-Merge Policy for Mechanical PRs Only](tasks/task-02-auto-merge-policy.md) — Objective 2
- [Task 03: Configuration Surface Documentation Skeleton](tasks/task-03-configuration-surface-docs.md) — Objective 3
- [Task 04: Hermes Host Provisioning Runbook](tasks/task-04-provisioning-runbook.md) — Objective 4
- [Task 05: Judgment Classification Rule Documentation](tasks/task-05-judgment-classification-rule.md) — Objective 5
- [Task 06: Drafting Agent Contract Documentation](tasks/task-06-drafting-agent-contract.md) — Objective 6

## Technical Debt / Infrastructure Analysis Summary
Persona-model maintenance (catching a newer family member, an expiring slug, or a missing slug, then advancing the lock; and re-tuning a prompt when a vendor updates their prompting guide) is currently entirely manual. Epic 19.7 already built the deterministic detection primitive (`atcr models check --json`); this plan is the automation layer on top of it, split strictly by determinism: mechanical work (string/timestamp comparison) needs no LLM and should never touch one, while prompt re-tuning genuinely benefits from an LLM's summarization of a vendor guide but must never auto-merge, since a flat-rate open model re-tuning a prompt meant to embody a *different* vendor's own prompting guidance is a cross-vendor judgment call requiring maintainer review.

## Technical Planning Notes
- **Reused, not rebuilt**: `atcr models check --json` (cmd/atcr/models.go), the C3 guardrail chain (internal/registry/validate.go, internal/tools/limits.go, internal/personas/community_fixture_test.go, internal/personas/community_schema_test.go). None of these are modified by this plan.
- **Drafting agent's edit anchor**: every community persona `.md` already carries a `<!-- vendor-guidance: <Vendor> — "<Guide Title>", <URL> -->` HTML-comment preamble (see `personas/community/anthony.md`, `gene.md`, `celeste.md`). The drafting agent's contract is to read that URL, fetch the current guide, and edit the body below it — never restructuring the mandatory `## Role`/`## Output Format` 7-column contract documented in `docs/personas-authoring.md`.
- **Separate-PR discipline is a hermes-side convention, not an atcr-repo constraint**: nothing in this repo currently conflates mechanical and prompt-edit changes into one PR, so there is no existing anti-pattern to unwind — the discipline is enforced by giving the mechanical and drafting agents distinct hermes cron jobs/skills that each open their own PR.
- **Existing CI inventory**: `.github/workflows/ci.yml`, `reconcile-module.yml`, `refresh-synthetic-manifest.yml`. No workflow yet exists for agent-authored-PR auto-merge policy.
- **Provisioning is additive, not code**: installing the `atcr` binary + a pull-before-run repo checkout + `gh` auth on nucleus.lan is host configuration, documented as a runbook — it does not touch `cmd/atcr`.

## Documentation References
See [documentation/README.md](documentation/README.md) once generated by `/find-documentation`. Anticipated key references:
- `docs/github-action.md` — structural precedent for `docs/hermes-maintenance-agents.md` (Overview → Usage/config table → required permissions → manual verification).
- `docs/personas-authoring.md` — the authoring contract the drafting agent's PRs must satisfy (canonical prompt structure, fixture rules, vendor-guidance convention).
- `.planning/epics/active/19.8_hermes_maintenance_agents.md` — the "Hermes Integration Surface" section (agent profiles, cron `jobs.json` shape, model/role assignment rationale).

## Implementation Strategy
1. **CI fixture-gate confirmation** — verify `.github/workflows/ci.yml` already runs the fixture-gate `go test` suite as a required check on every PR (including bot-authored ones); extend only if a gap is found.
2. **Auto-merge policy (mechanical PRs only)** — add an opt-in workflow, off by default, that auto-merges a PR only when it matches a mechanical-slug-bump path/label filter AND the fixture-gate check is green.
3. **Configuration surface docs** — write `docs/hermes-maintenance-agents.md` covering role→agent assignment, the drafting-model default (marcus/qwen-3.7-plus) and fallback (nolan/glm-5.2), dry-run/preview mode, and the separate-PR + human-approval guardrails.
4. **Provisioning runbook** — document the one-time steps to install `atcr` + a pull-before-run checkout + `gh` auth on nucleus.lan, as the prerequisite for the mechanical agent's cron job.
5. **Judgment + drafting contract spec** — record (in the same docs) the minor/major classification rule and the drafting agent's vendor-guidance edit contract, so hermes-side skill authoring (out of this repo) has an unambiguous spec.

## Recommended Packages
No high-ROI packages identified. This plan is CI/docs wiring over an already-shipped Go CLI primitive (`atcr models check --json`, Epic 19.7) and an already-live external agent framework (hermes) — no new Go dependency is needed.

## Success Criteria
- A scheduled mechanical agent (hermes-side, external to this repo) can run `atcr models check --json` and open a slug-bump PR that the CI fixture gate blocks on failure and allows on pass.
- The judgment classification rule (minor → mechanical; major/deprecation → re-tune task with vendor guide URL) is documented unambiguously enough for a hermes skill to implement without further atcr-repo changes.
- The drafting agent's contract (vendor-guidance-anchored edit, separate PR, never auto-merge) is documented and traceable to the existing `<!-- vendor-guidance: ... -->` convention already present in every community persona `.md`.
- No agent can commit directly to `main`; every agent-authored change lands as a PR; prompt PRs require explicit human approval; mechanical PRs may opt into auto-merge-on-green.
- Agent-authored persona prompts are blocked by the same reused C3 guardrails (fixture gate + length cap + schema validation) as any human-authored change — verified by confirming these tests remain in the required CI check path.
- The role→agent and drafting-model configuration is documented with a safe (opt-in, off-by-default) default, and a dry-run/preview mode is specified before any PR is opened.

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Auto-merge-on-green misconfigured lets a mechanical PR merge with an unintended side effect | Low | High | Scope the auto-merge trigger to a narrow path/label filter matching only lock/slug files; default the policy off; require the identical fixture-gate check as any other PR |
| A prompt-edit PR is mistakenly matched by the mechanical auto-merge filter, bypassing human review | Low | Critical | Keep mechanical and prompt PRs on structurally distinct paths/branches by convention (separate hermes cron jobs/skills); gate the auto-merge workflow's trigger condition on that path filter, never on "opened by a bot" |
| Hermes host provisioning drifts from the atcr repo (stale binary/checkout), causing false drift reports | Medium | Medium | Document (and where feasible script) a pull-before-run step in the provisioning runbook so every cron invocation checks out current `main` before running `atcr models check` |
| Vendor prompting-guide fetch fails or returns stale content, producing a low-quality draft | Medium | Low | Drafting-agent output is explicitly a draft requiring human approval before merge; a bad draft costs review time, not correctness |

## Next Steps
1. `/create-tasks @.planning/plans/active/19.8_hermes_maintenance_agents/`
2. `/design-sprint @.planning/plans/active/19.8_hermes_maintenance_agents/`
3. `/create-sprint @.planning/plans/active/19.8_hermes_maintenance_agents/`
4. `/execute-sprint`
