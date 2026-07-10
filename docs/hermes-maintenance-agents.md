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
  is **off by default** and must be explicitly enabled per repo by setting the
  repository variable `HERMES_AUTO_MERGE_ENABLED` to `'true'` (any other value,
  or unset, keeps it off). Auto-merge additionally requires each mechanical PR to
  (a) carry the `hermes:mechanical` label and (b) touch **only**
  `personas/community/*.yaml` paths — the mechanical cron script must apply that
  label and restrict its diff accordingly (see [Provisioning](#provisioning)). A
  PR missing the label, or touching any other path (including a single `.md`), is
  never auto-merged. Enabling `HERMES_AUTO_MERGE_ENABLED` does not retroactively
  re-evaluate already-open mechanical PRs whose required `Go Lint & Test` check
  already concluded before the flag was flipped; generate a fresh event by
  re-applying the `hermes:mechanical` label or pushing an update to the PR.
- **Judgment and drafting** automation is likewise **opt-in** — no role runs
  against a live PR target until the maintainer enables it.
- **Prompt PRs always require explicit human approval** and never auto-merge,
  regardless of any repo variable.

**Manual maintainer step (branch protection):** confirm that the GitHub
Settings → Branches → branch-protection rule for `main` lists the
`Go Lint & Test` check (the job / check-run name inside the `Go CI` workflow at
`.github/workflows/ci.yml` — `Go CI` is the workflow name, `Go Lint & Test` is
the selectable required-check entry) as a **required status check**. This could not be verified programmatically during
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

**Auto-merge eligibility (the wrapper's responsibility).** For a mechanical PR to
be eligible for auto-merge, the wrapper must open it so that it (a) applies the
`hermes:mechanical` label (`gh pr create --label hermes:mechanical`) and (b)
restricts its diff to `personas/community/*.yaml` slug-lock changes only — no
`.md` prompt edits, no other paths. These are the exact signals the
[auto-merge workflow](../.github/workflows/hermes-auto-merge.yml) checks (in
addition to the `HERMES_AUTO_MERGE_ENABLED='true'` opt-in and a green
`Go Lint & Test`). A PR that omits the label or touches any other path is never
auto-merged — it simply waits for a human, exactly like a prompt PR.

## Judgment Classification Rule

The judgment agent reads `atcr models check --json` output and classifies each
drift finding as **minor** (route to the mechanical slug-bump PR) or **major**
(open a re-tune task for the drafting agent). The classification is grounded in
the `DriftFinding.Condition` constants defined in `internal/personas/drift.go`
(the same stable `condition` strings emitted in the `--json` output) — switch on
these values, never on free text:

| `condition` (constant) | Value | Class | Route |
|------------------------|-------|-------|-------|
| `ConditionNewerMember` | `newer-member` | minor | Mechanical slug-bump PR (carries `SuggestedSlug`). |
| `ConditionDeprecation` | `deprecation` | major | Re-tune task (carries `ExpirationDate`; no `SuggestedSlug`). |
| `ConditionMissing` | `missing` | minor by default (see escalation) | Mechanical slug-bump PR — unless escalated below. |

**Co-occurrence escalation ("same persona, same run").** Group findings by
`DriftFinding.Persona` within a single `atcr models check --json` invocation. If a
persona has a `missing` finding **and** a `deprecation` finding in the same run,
escalate the `missing` to **major** (re-tune) rather than routing it as minor —
the missing slug and the deprecation are the same underlying event, not two
independent conditions, so the judgment agent must not slug-bump a slug that is
simultaneously deprecating. This is a conservative safeguard: `CheckDrift`
currently emits a `missing` finding only when the locked slug is absent from the
catalog (and then checks no deprecation for that persona in the same run), so the
default minor route for `missing` is taken only when no `deprecation` accompanies
it — the escalation rule keeps the judgment agent correct even if a future run
surfaces both for one persona.

**Re-tune task payload** (the input contract consumed by the [Drafting Agent
Contract](#drafting-agent-contract)):

| Field | Source |
|-------|--------|
| `persona` | `DriftFinding.Persona` |
| `old` (current model) | `DriftFinding.CurrentSlug` |
| `new` (target model) | `DriftFinding.SuggestedSlug` when present; otherwise the literal string `none suggested — requires manual selection` (deprecation/missing findings carry no `SuggestedSlug`, so a slug must never be fabricated). |
| `vendor_guide_url` | Parsed from the persona `.md`'s `<!-- vendor-guidance: <description>, <url> -->` HTML-comment preamble. The marker format is test-enforced by `personas/community_test.go`'s `vendorGuidanceRe` (`(?m)<!--\s*vendor-guidance:\s*(\S.*?)\s*-->`); the captured value is `<description>, <url>` and the agent extracts the URL from it. |

**Trigger.** The judgment agent fires off the `atcr models check --json` output
rendered by `renderDriftJSON` (`cmd/atcr/models.go`). A non-empty findings set
exits the command with code `1` (`driftFoundError.ExitCode()` → `exitFailure`),
which is the cron script's signal that drift was found and classification should
run; a clean check exits `0` and the agent does nothing.

**Judgment agents.** This classification runs on `brian` (`glm-5.1`) or `cole`
(`kimi-k2.7-code`) per the [Role → Agent Configuration](#role--agent-configuration)
table. It is light classification logic implementable entirely hermes-side — it
requires **no atcr-repo code changes**.

## Drafting Agent Contract

The drafting agent turns a **major**/re-tune task (from the [Judgment
Classification Rule](#judgment-classification-rule)) into a **separate**,
LLM-drafted persona prompt-edit PR. It never auto-merges and always requires
human approval.

**Input.** The re-tune task payload defined by the Judgment Classification Rule:
`persona` (`DriftFinding.Persona`), `old` (`DriftFinding.CurrentSlug`), `new`
(`DriftFinding.SuggestedSlug`, or the literal `none suggested — requires manual
selection` when absent), and `vendor_guide_url` (extracted from the persona's
`<!-- vendor-guidance: <description>, <url> -->` preamble).

**Edit procedure (ordered).**

1. Read the target persona's `.md` file and parse its `<!-- vendor-guidance:
   <description>, <url> -->` preamble comment — the same marker the Judgment rule
   documents, test-enforced by `personas/community_test.go`'s `vendorGuidanceRe`
   (`(?m)<!--\s*vendor-guidance:\s*(\S.*?)\s*-->`); the convention is a free-text vendor/guide
   description followed by a URL, as in `personas/community/anthony.md:1`,
   `gene.md:1`, `celeste.md:1`).
2. Fetch the current guide at the cited URL.
3. Edit **only the persona body below the preamble comment** — never the
   preamble's HTML-comment line itself, except as in step 4.
4. If (and only if) the guide's location or title changed, update the preamble's
   description/URL to match; otherwise leave the preamble untouched.

**Structural invariant.** The mandatory persona section structure from
[`docs/personas-authoring.md`](personas-authoring.md) —
`## Role` / `## Focus` / `## Scope` / `## Tool-Assisted Review`
(optional, only where present) / `## Severity Rubric` / `## Output Format`
(the exact 7-column pipe-delimited reviewer-finding contract) / `## Payload` —
must be preserved **byte-for-byte**: never restructured, reordered, renamed, and
no headings added or removed. The template tokens (`{{.AgentName}}`,
`{{.ScopeRule}}`, `{{.Payload}}`, etc.) and the 7-column `## Output Format`
contract must survive untouched (the reconciler parses that format). The drafting
agent may change only the **prose content within** these existing sections.

**`.yaml` off-limits.** The drafting agent must **never** touch the paired
`personas/community/<slug>.yaml` (provider/model binding) file unless the re-tune
payload explicitly calls for a model change — i.e. it carries a concrete
`SuggestedSlug`, not the `none suggested — requires manual selection` placeholder.
A pure prompt re-tune touches the `.md` only.

**Model assignment.** marcus (`openai/qwen-3.7-plus`) default, nolan
(`glm-5.2`) fallback — see [Role → Agent Configuration](#role--agent-configuration)
and [Drafting Model Default & Fallback](#drafting-model-default--fallback). This
is a cross-reference, not a new decision.

**Hard output contract.**

- Opens a PR **separate** from any mechanical slug-bump PR — never mixed into the
  same PR or commit.
- The PR touches `personas/community/*.md` only (and the paired `.yaml` only when
  the re-tune payload carries a concrete `SuggestedSlug`, per the **`.yaml`
  off-limits** rule above) — a path the [auto-merge workflow](../.github/workflows/hermes-auto-merge.yml)'s
  fail-closed allow-list explicitly **excludes**, so this PR is structurally
  incapable of matching the mechanical auto-merge path.
- The PR is explicitly a **draft** and requires explicit **human approval** before
  merge; it **never auto-merges** under any configuration.
- Before it is even reviewable it must pass the reused Epic 19.6 **C3 guardrail
  chain** unmodified — schema validation (`internal/registry/validate.go`), length
  caps (`internal/tools/limits.go`), and the fixture gate
  (`internal/personas/community_fixture_test.go`,
  `internal/personas/community_schema_test.go`) — the same gate any human PR faces.

This section records a contract for a hermes-side skill to implement; it requires
**no atcr-repo code changes**.
