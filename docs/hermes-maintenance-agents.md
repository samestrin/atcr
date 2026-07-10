# Hermes Maintenance Agents — Configuration Surface

Hermes is a live, external agent host (`nucleus.lan:~/docker/hermes/`) that
already schedules cron-based agent profiles (brian, nolan, marcus, cole). This
document is the maintainer-facing configuration surface for wiring those existing
agents to Epic 19.7's `atcr models check --json` primitive across three
clearly-separated roles — **mechanical** (deterministic slug-bump PR, no LLM),
**judgment** (classify drift as minor vs. major/deprecation), and **drafting**
(an LLM drafts persona `.md` prompt edits from a vendor guide). Every agent
change lands as a reviewable, fixture-gated pull request; no agent ever commits
to `main`.

This doc mirrors the structural precedent of
[`docs/github-action.md`](github-action.md) (Overview → configuration table →
guardrail/permissions contract → manual verification). Primitive provenance:
**Epic 19.7 (Live Model Resolution)** supplies `atcr models check` and the
drift/expiration signals these agents act on; **Epic 19.6 (Community Registry
Hub)** supplies the C3 guardrails reused unmodified to contain agent-authored
prompts.

## Role → Agent Configuration

The hermes-side agent configuration itself (cron `jobs.json`, role assignment,
skills) lives on the hermes host — **not** in the atcr repo. This table records
the SSH-confirmed (2026-07-08 probe of nucleus.lan) assignment contract a
maintainer wires into the hermes profiles.

| Role | Hermes Profile | Model | Notes |
|------|-----------------|-------|-------|
| Mechanical | `no_agent` (cron script job) | none (no LLM) | Runs `atcr models check --json`; deterministic slug-bump only, no model attached. |
| Judgment | `brian` or `cole` | `glm-5.1` (brian) / `kimi-k2.7-code` (cole) | Classifies drift severity per the Judgment Classification Rule (see below). |
| Drafting | `marcus` (default), `nolan` (fallback) | `openai/qwen-3.7-plus` (marcus) / `glm-5.2` (nolan) | Drafts persona prompt re-tune PRs per the Drafting Agent Contract (see below); never auto-merges. |

## Drafting Model Default & Fallback

- **Default:** marcus / `openai/qwen-3.7-plus`. Prompt re-tuning is prose and
  instruction work (not code); marcus is the prose-tuned "senior creative"
  profile, and its large context ingests a full vendor guide + persona +
  fixtures in one pass. Drafting fires only on major bumps/deprecations, so
  throughput is irrelevant.
- **Fallback:** nolan / `glm-5.2`, selected if marcus is unavailable/erroring or
  if strict schema/template precision matters more than prose quality in
  practice.
- Fallback selection is a **hermes-side cron/skill concern**, not an atcr-repo
  concern. This document records only the contract the hermes profile config
  must satisfy; the model stays configurable per profile (`model.default`).

## Dry-Run / Preview Mode

Before any PR is opened by **any** agent role (mechanical, judgment-routed, or
drafting), the responsible hermes cron job/skill must support a **preview mode**
that prints what it would do without opening a PR:

- The proposed **PR title**.
- The **target branch name**.
- A **diff summary** — files touched plus a line-count delta.

Preview mode must **not** call the GitHub PR-creation skill
(`data/skills/github/github-pr-workflow`). It is the default verification step a
maintainer runs before flipping an agent from preview to live — the safe posture
is preview-first, live-only-after-confirmation.

## Guardrail Contract

Every agent-authored persona-prompt PR is blocked by the **same reused Epic 19.6
C3 guardrail chain** as a human PR — reused **unmodified**, not re-implemented
hermes-side:

- Schema validation — `internal/registry/validate.go`
  (`ValidateCommunityPersonaYAML`).
- Length caps — `internal/tools/limits.go`.
- Fixture gate — `internal/personas/community_fixture_test.go`,
  `internal/personas/community_schema_test.go`.

These run inside the existing `Go CI` (`Go Lint & Test`) job on every
`pull_request` (see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)),
which carries **no actor/bot-based exclusion** — an agent-authored PR faces the
identical gate a human contributor does.

**Off/opt-in posture:**

- **Mechanical auto-merge-on-green** (`.github/workflows/hermes-auto-merge.yml`)
  is **off by default** and must be explicitly enabled per repo.
- **Judgment and drafting** automation is likewise **opt-in** — no role runs
  against a live PR target until the maintainer enables it.
- **Prompt PRs always require explicit human approval** and never auto-merge,
  regardless of any repo variable.

**Manual maintainer step (branch protection):** confirm that the GitHub
Settings → Branches → branch-protection rule for `main` lists the
`Go CI / Go Lint & Test` check (job name at `.github/workflows/ci.yml`) as a
**required status check**. This could not be verified programmatically during
sprint execution (`gh api repos/samestrin/atcr/branches/main/protection` returns
`403` for this private-repo tier), so it is handed off as an explicit manual
verification a maintainer must perform before enabling any agent role.

## Provisioning

One-time runtime setup on the hermes host (`nucleus.lan`) so the mechanical
`no_agent` cron job can run `atcr models check --json` and open PRs. This section
is **documentation only** — it introduces no `cmd/atcr` code changes; it records
the host-side steps a maintainer performs on nucleus.lan.

1. **Install the `atcr` binary.** Build from source (`go build -o bin/atcr
   ./cmd/atcr`) or copy a release binary onto the host, and place it on the
   `PATH` of the hermes cron execution environment (e.g. `~/docker/hermes/bin/`
   or `/usr/local/bin/atcr`). Confirm with `atcr --version`.

2. **Establish a repo checkout.** Clone the atcr repository under
   `~/docker/hermes/` (or an adjacent path the cron script references), e.g.
   `~/docker/hermes/atcr/`. This checkout is what `atcr models check --json`
   reads the current `personas/community/*` state and model locks from.

3. **Configure GitHub auth for PR opening.** Run `gh auth login` on the host, or
   provide a token-based credential (`GH_TOKEN` / `GITHUB_TOKEN` env var scoped
   for PR creation) in the cron job's environment, so the mechanical script can
   open PRs via `gh pr create`. Use the shared hermes PR skill
   (`data/skills/github/github-pr-workflow`) as the opening mechanism.

4. **Pull before every run (mandatory).** The `no_agent` cron script MUST fetch
   and fast-forward the checkout to the current default branch
   (`git pull --ff-only` against `main`) **before** invoking `atcr models check
   --json`. Without this, drift is evaluated against a stale checkout and the
   agent can emit false drift findings or bump a slug that was already changed.
   Pull-before-run is a hard prerequisite of every mechanical invocation, not an
   optimization.

**Cron job shape.** The mechanical agent is a `no_agent` (no-LLM) job in
`data/profiles/<agent>/cron/jobs.json` — an entry with `no_agent: true` and a
`script` field pointing at the pull-then-`atcr models check --json`-then-open-PR
wrapper. Use brian's `fleet-sweep` job (`7 3 * * *`, which SSHes the fleet and
writes a status file) as the structural precedent for the script-based entry.
The job configuration itself lives on the hermes host, not in the atcr repo.

## Judgment Classification Rule

_To be completed by Task 05._

## Drafting Agent Contract

_To be completed by Task 06._
