# Sprint 19.8: hermes maintenance agents

---
executor: /execute-sprint
execution_mode: continuous
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.8 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A maintainer-side automation surface that lets the already-live external hermes agents (nucleus.lan:`~/docker/hermes/`) consume Epic 19.7's `atcr models check --json` primitives and offload recurring persona-model maintenance across three separated roles — mechanical (deterministic slug-bump PR, no LLM), judgment (classify drift as minor vs. major/deprecation), and drafting (an LLM drafts persona `.md` prompt edits from a vendor guide). The in-repo footprint is one opt-in CI workflow (`.github/workflows/hermes-auto-merge.yml`) plus one documentation file (`docs/hermes-maintenance-agents.md`); the agent configuration itself lives on the hermes host, documented here.

### Why This Matters

The recurring slug/deprecation toil and prompt re-tuning are unavoidable maintenance costs; this sprint converts "write the re-tune from scratch" into "review a drafted re-tune" and eliminates the purely mechanical slug/deprecation toil — while ensuring every agent change lands as a reviewable, fixture-gated pull request and never as a direct commit to `main`.

### Key Deliverables

- `.github/workflows/hermes-auto-merge.yml` — opt-in, off-by-default auto-merge workflow whose structural path/label filter matches mechanical `*.yaml` slug bumps only (never prompt `*.md` edits) and never keys on actor/bot authorship (Task 02).
- `docs/hermes-maintenance-agents.md` — configuration surface: Role→Agent table, drafting model default/fallback, dry-run/preview mode, guardrail contract, plus Provisioning, Judgment Classification Rule, and Drafting Agent Contract sections (Tasks 03–06).
- Confirmed CI fixture gate reachability for agent-authored PRs, with the branch-protection required-check documented as a manual maintainer step (Task 01).

### Success Criteria

- Mechanical slug-bump PRs can auto-merge only on green, only when structurally distinct from prompt-edit PRs, and only when the maintainer has opted in (AC1, AC4).
- Drift classification (minor→mechanical / major/deprecation→re-tune task) and the drafting agent contract are documented and grounded in `internal/personas/drift.go` constants and the persona `<!-- vendor-guidance: ... -->` preamble (AC2, AC3).
- No agent ever commits to `main`; prompt PRs always require human approval and pass the reused 19.6 C3 guardrails (AC4, AC5); configuration is documented with a safe opt-in default plus a dry-run/preview mode (AC6).

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** TASK-BASED (infrastructure plan — complexity 5/12 MODERATE). This sprint introduces no new Go code, so there is no RED→GREEN→REFACTOR cadence; deliverables are one CI-workflow YAML file and Markdown documentation. Verification is per-task manual/mock-PR validation against explicit, falsifiable comparison targets (see each task's Success Criteria), plus the existing `go test ./...` fixture gate this sprint's PR(s) ride unchanged.

**Adversarial Review:** ENABLED 🎯 — inline-fix severities **CRITICAL/HIGH**, deferred **MEDIUM/LOW**. A fresh subagent (no memory of the implementation) reviews the sprint's one code artifact — the auto-merge workflow (Task 02), this sprint's sole Critical-impact-risk surface — and a cumulative adversarial pass reviews the full documentation set in the final phase.

**Testing Tiers:**
- **T1 (Focused):** After each small change (e.g., re-read the edited doc section / workflow block).
- **T2 (Module):** After completing a task (whole-file read-through against the task's named comparison target).
- **T3 (Full):** DoD validation — `go test ./...` remains green (unaffected; no Go code changes), `gofmt`/`golangci-lint` clean on any touched files, markdown/YAML well-formed.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [tasks/](plan/tasks/) | Task specifications (Task 01–06) |
| [plan.md](plan/plan.md) | Plan overview and metadata |

---

## Sprint Conventions

### Testing Tiers
- **T1: Focused** — after each small change (`re-read edited section / relevant test path`).
- **T2: Module** — after completing a task (whole-file read-through vs. named target).
- **T3: Full** — DoD validation & pre-commit (`go test ./...`, `gofmt`, `golangci-lint`).

### DoD Verification Checklist
1. Tests (T3): `go test ./...` passing (unaffected — no Go code changed).
2. Coverage: ≥80% baseline unaffected (no new Go code).
3. Lint: `gofmt`/`golangci-lint` clean on any touched files; YAML/Markdown well-formed.
4. Build: `go build ./cmd/atcr` succeeds (unaffected).
5. Docs: `docs/hermes-maintenance-agents.md` sections complete, no unreplaced placeholders.

### DoD Report Template
```
Task-{N} DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

**Git Strategy:** GitHub Flow / trunk-based. Short-lived `feature/` branch (`feature/19.8_hermes_maintenance_agents`), atomic Conventional Commits (`type(scope): description`), squash-and-merge to `main`. PRs cannot merge if the GitHub Actions `Go CI` workflow fails.

**Commit Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`.

**Implementation & Coding Standards:** See `.planning/specifications/implementation-standards.md` and `.planning/specifications/coding-standards.md`. This sprint adds no Go code; standards apply to the YAML workflow (minimal, least-privilege permissions) and Markdown (mirror `docs/github-action.md` structure).

---

## External Resources

No external specifications cleared the relevance threshold for this plan (see [plan/documentation/source.md](plan/documentation/source.md)). In-repo structural precedents referenced by tasks:
- `docs/github-action.md` — doc structure precedent (Overview → config table → required permissions → manual verification).
- `docs/personas-authoring.md` — the 7-section persona contract the drafting agent must preserve.
- `.github/workflows/ci.yml` — source of the required `Go CI` (`Go Lint & Test`) check name and `[self-hosted, gauntlet]` runner group.
- `internal/personas/drift.go` — `DriftFinding` condition constants grounding the judgment classification rule.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — CI Gate Confirmation & Doc Skeleton

**Duration:** 1 day
**Focus:** Establish the two foundations everything else depends on — confirm the existing CI fixture gate is reachable/blocking for agent-authored PRs (Task 01, a re-verify + formal close of an already-completed zero-diff audit), and create `docs/hermes-maintenance-agents.md` with its full section skeleton (Task 03). No dependency between the two — they run independently.

### 1.1 [x] **🏗️ CI Fixture-Gate Confirmation for Agent-Authored PRs** ([task-01](plan/tasks/task-01-ci-fixture-gate-confirmation.md))
   **Task:** Re-verify and formally close that `.github/workflows/ci.yml` runs its `Go Lint & Test` job unconditionally on every `pull_request` (no `github.actor`/bot-exclusion condition anywhere), that `go test ./...` reaches `internal/personas/...` and `internal/registry/...` under the root `go.mod`, and that the fixture gate (`internal/personas/community_fixture_test.go`, `internal/registry/validate.go`'s `ValidateCommunityPersonaYAML`) is on the required CI path.
   **Priority:** P1 | **Effort:** S (0d — audit already performed, zero-diff)
   1. Read `.github/workflows/ci.yml` in full; confirm no actor/bot-based `if:` condition at workflow, job, or step level (event-type branching at ci.yml:60-64 is permitted — it keys on event type, not actor).
   2. Confirm `go test ./...` (ci.yml:61) reaches `internal/personas/...` and `internal/registry/...` (both under root `go.mod`; only `./reconcile` and `./internal/astgroup/parsers/src/braceparser` are nested modules).
   3. Record the branch-protection required-check verification as a **manual maintainer step** for Task 03 — `gh api .../branches/main/protection` returns 403 in this environment (private-repo tier limit), so it cannot be confirmed programmatically.
   4. No workflow edit expected (extend only if a gap is found).
   **Success Criteria:** No actor-based exclusion in `ci.yml`; persona/registry coverage confirmed; branch-protection manual step handed to Task 03; `.github/workflows/ci.yml` left unmodified unless a real gap is found.
   **Files:** `.github/workflows/ci.yml` (audit only) | **Duration:** 0d (verify) | **AC:** AC1, AC4, AC5

### 1.2 [x] **🏗️ Configuration Surface Documentation Skeleton** ([task-03](plan/tasks/task-03-configuration-surface-docs.md))
   **Task:** Create `docs/hermes-maintenance-agents.md` mirroring `docs/github-action.md`'s structure, with the configuration-surface content (Overview, Role→Agent Configuration table, Drafting Model Default & Fallback, Dry-Run/Preview Mode, Guardrail Contract) and three placeholder anchors (`## Provisioning`, `## Judgment Classification Rule`, `## Drafting Agent Contract`) for Tasks 04–06 to populate.
   **Priority:** P1 | **Effort:** M
   1. Read `docs/github-action.md` in full to match heading levels, table style, and the required-permissions/manual-verification callout pattern.
   2. Create `docs/hermes-maintenance-agents.md` with the sections above, in order, ending with the three placeholder anchors carrying exact placeholder text (`_To be completed by Task 0N._`).
   3. Populate the Role→Agent Configuration table with the exact SSH-confirmed rows (mechanical/`no_agent`/none; judgment/brian-or-cole/`glm-5.1`-or-`kimi-k2.7-code`; drafting/marcus-default-nolan-fallback/`openai/qwen-3.7-plus`-or-`glm-5.2`) — no invented agents/models.
   4. State the opt-in/off-by-default posture and the dry-run/preview contract (prints PR title, target branch, diff summary; no PR-creation call). Record the C3 guardrail chain (`internal/registry/validate.go`, `internal/tools/limits.go`, `internal/personas/community_fixture_test.go`, `internal/personas/community_schema_test.go`) as reused unmodified, and the branch-protection manual step from Task 01.
   5. COMMIT: `git add docs/hermes-maintenance-agents.md && git commit -m "docs(hermes): add configuration surface doc skeleton"`
   **Success Criteria:** File created with all five content sections + three placeholder anchors; Role→Agent table matches SSH-confirmed assignments exactly; opt-in posture and dry-run behavior unambiguous; structure mirrors `docs/github-action.md`.
   **Files:** `docs/hermes-maintenance-agents.md` (create) | **Duration:** 1d | **AC:** AC4, AC5, AC6

### 1.3 [x] **Phase 1 — Definition of Done**
   1. Task 01 audit closed (no actor exclusion; branch-protection manual step recorded).
   2. `docs/hermes-maintenance-agents.md` exists with all sections + three placeholder anchors intact.
   3. T3: `go test ./...` still green (unaffected); touched files `gofmt`/markdown clean.
   4. DoD Report: `Task 01/03 DoD Complete — Auto: X/5 | Task-Specific: Y/Z`.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

---

## Phase 2: Core Items — Auto-Merge Workflow & Provisioning Runbook

**Duration:** 1.5 days
**Focus:** Build the sprint's one code artifact — `.github/workflows/hermes-auto-merge.yml`, opt-in and off-by-default, whose structural path/label filter (never actor-based) is this sprint's Critical-risk surface — depending on Task 01's confirmed check name. In parallel, append the `## Provisioning` section to the Task 03 skeleton (depends only on the skeleton existing). Task 02 gets a mandatory fresh-subagent adversarial review.

### 2.1 [x] **🏗️ Opt-In Auto-Merge Policy for Mechanical PRs Only** ([task-02](plan/tasks/task-02-auto-merge-policy.md))
   **Task:** Create `.github/workflows/hermes-auto-merge.yml`: opt-in (gated on a repo variable, e.g. `vars.HERMES_AUTO_MERGE_ENABLED == 'true'`, default off), triggered on `pull_request` (`opened`, `synchronize`, `labeled`) and/or check-completion. Auto-merge proceeds only when a structural allow-list filter confirms **every** changed file matches `personas/community/*.yaml` (and/or the `hermes:mechanical` label) — rejecting the run if **any** file matches `personas/community/*.md` or falls outside the allow-list — and only after the required `Go CI` (`Go Lint & Test`) check reports success. Filter is path/label-only, **never** keyed on `github.actor`/PR author/"is a bot".
   **Priority:** P1 | **Effort:** M
   1. Understand the two PR shapes (mechanical `*.yaml` slug bump vs. prompt `*.md` re-tune) and the fail-closed allow-list requirement; identify `Go CI` check name from `ci.yml` (Task 01).
   2. Author the workflow: opt-in repo-variable gate at job top; structural path filter via `dorny/paths-filter` or `git diff --name-only` combined (not substituted) with the `hermes:mechanical` label check; explicit no-op (not error) on non-match or unset opt-in.
   3. Require `Go Lint & Test` = success before merge (`gh pr checks` / `check_run` completion), then `gh pr merge --auto --squash` or REST merge.
   4. Scope `permissions:` to `contents: write` + `pull-requests: write` **only** (no `checks: write`, no `actions: write`).
   5. Add an inline comment/guard block banning any `github.actor`/author/bot-login gating — path/label match is the only permitted gate.
   6. Verify (no `act`/local runner available — use scratch/mock PR or workflow dry-read) the 3 scenarios: mechanical-`.yaml`+label → would auto-merge only after green; prompt-`.md` path → never matches even with label/bot author; opt-in unset → no-op regardless of path match.
   7. COMMIT: `git add .github/workflows/hermes-auto-merge.yml && git commit -m "ci(hermes): add opt-in auto-merge for mechanical PRs only"`
   **Success Criteria:** Workflow exists, opt-in/off by default; filter matches only `*.yaml`/lockfile paths and/or the label, never `*.md`; merge gated on green `Go CI`; permissions scoped to `contents:write`+`pull-requests:write`; no bot-authorship condition anywhere.
   **Files:** `.github/workflows/hermes-auto-merge.yml` (create) | **Duration:** 1.5d | **AC:** AC1, AC4

### 2.1.A [x] **Auto-Merge Workflow — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `.github/workflows/hermes-auto-merge.yml`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.1 auto-merge workflow`
   - prompt: Self-contained brief including:
     - File to review (absolute path): `/Users/samestrin/Documents/GitHub/atcr/.github/workflows/hermes-auto-merge.yml`
     - Context: this workflow auto-merges only *mechanical* slug-bump PRs; it must be structurally incapable of auto-merging a prompt-edit (`personas/community/*.md`) PR, must be opt-in/off by default, and must never key on actor/bot authorship.
     - Checklist (pass verbatim):
       - SECURITY: Can the path/label filter be bypassed? Is it fail-closed allow-list (any out-of-allow-list file, including a single `.md`, disqualifies the whole PR) rather than deny-list? Any actor/bot-login/author condition present (must be none)? Permissions over-scoped beyond `contents:write`+`pull-requests:write`? Untrusted input into a run step (script injection via PR title/label/body)?
       - EDGE CASES: Mixed-path PR (`.yaml` + `.md` together) — rejected? `labeled` event with a forged `hermes:mechanical` label but a `.md` in the diff — rejected? Opt-in variable unset — full no-op regardless of match?
       - ERROR HANDLING: Does a non-matching PR no-op cleanly (not fail the job)? Merge only after `Go Lint & Test` = success (not merely "checks exist")?
       - PERFORMANCE: N/A (event-triggered workflow) — note only if a pathological trigger loop is possible.
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, no impl memory):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | hermes-auto-merge.yml (filter/gate/merge) | TOCTOU — no head-SHA pin between filter, gate, and merge; author could push a `.md` commit after the `.yaml`-only filter passed and `gh pr merge` would land the newer HEAD (Go CI trivially green on a `.md`), breaking the "structurally incapable of merging a prompt PR" guarantee. | Pin the triggering head SHA once; evaluate files + check-runs against it; `gh pr merge --match-head-commit "$SHA"` so merge aborts if HEAD moved. |
   | MEDIUM | hermes-auto-merge.yml (gate) | `select(.name=="Go Lint & Test").conclusion | head -n1` picks an arbitrary run; a re-run creates multiple runs on the same SHA so a stale `success` could win over a current `failure`. | Require ≥1 matching run AND every matching run == `success` (fail-closed). |
   | MEDIUM | hermes-auto-merge.yml (concurrency) | Group key differs by event (`pull_request.number` vs `check_suite.head_sha`), so two evaluations of the same PR run concurrently and `cancel-in-progress:false` never supersedes a stale one — widening the TOCTOU window. | Normalize the group to the PR number for both events; `cancel-in-progress: true`. |

   **Action Taken:** All three fixed inline in 2.1.R (the two MEDIUMs are coupled to the CRITICAL TOCTOU surface and cheap — fixed rather than deferred). Re-verified below.

### 2.1.R [x] **Auto-Merge Workflow — REFACTOR (address review)**
   1. Fix CRITICAL/HIGH issues from 2.1.A (if any).
   2. Re-verify the 3 filter scenarios still hold after fixes (T1).
   3. Confirm YAML well-formed and permissions unchanged/minimal (T2).
   4. COMMIT: `git add .github/workflows/hermes-auto-merge.yml && git commit -m "ci(hermes): address adversarial review of auto-merge filter"`
   **Duration:** 0.5d (only if findings)

### 2.2 [x] **🏗️ Hermes Host Provisioning Runbook** ([task-04](plan/tasks/task-04-provisioning-runbook.md))
   **Task:** Append a `## Provisioning` section to `docs/hermes-maintenance-agents.md` (replacing Task 03's placeholder) documenting the one-time nucleus.lan setup: install the `atcr` binary (`go build ./cmd/atcr` or release binary on PATH), clone a repo checkout under `~/docker/hermes/`, configure `gh auth` / token, and a **mandatory pull-before-run** step so drift is evaluated against current personas/lock. Cross-reference the `no_agent` cron shape (`data/profiles/<agent>/cron/jobs.json`) and brian's `fleet-sweep` job as precedent. Documentation only — no `cmd/atcr` changes.
   **Priority:** P1 | **Effort:** S
   1. Read the current `docs/hermes-maintenance-agents.md` (Task 03 state) to match heading level/style and confirm the `## Provisioning` placeholder text.
   2. Replace the placeholder with binary-install + checkout-location + `gh` auth + mandatory pull-before-run steps; cross-reference the `no_agent` cron job shape.
   3. State explicitly this is documentation only — no `cmd/atcr` code changes.
   4. COMMIT: `git add docs/hermes-maintenance-agents.md && git commit -m "docs(hermes): add host provisioning runbook"`
   **Success Criteria:** `## Provisioning` documents binary install, checkout location, `gh` auth, and mandatory pull-before-run; cross-references `cron/jobs.json` `no_agent` shape; no `cmd/atcr` changes.
   **Files:** `docs/hermes-maintenance-agents.md` (edit) | **Duration:** 0.5d | **AC:** AC1

### 2.3 [x] **Phase 2 — Definition of Done**
   1. `.github/workflows/hermes-auto-merge.yml` created, opt-in/off by default, filter fail-closed, permissions minimal, no actor gating.
   2. Adversarial review (2.1.A) run by a fresh subagent; CRITICAL/HIGH fixed in 2.1.R, MEDIUM/LOW deferred to `clarifications/tech-debt-captured.md`.
   3. `## Provisioning` placeholder replaced; pull-before-run mandatory; no `cmd/atcr` changes.
   4. T3: `go test ./...` green (unaffected); YAML well-formed; touched files clean.
   5. DoD Report: `Task 02/04 DoD Complete — Auto: X/5 | Task-Specific: Y/Z`.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

---

## Phase 3: Advanced — Judgment Classification Rule

**Duration:** 1 day
**Focus:** Replace the Task 03 `## Judgment Classification Rule` placeholder with the minor/major classification mapping grounded in `internal/personas/drift.go`'s actual `DriftFinding.Condition` constants, the `ConditionMissing` + co-occurring `ConditionDeprecation` escalation edge case, and the re-tune task payload spec that Phase 4's Task 06 consumes as its input contract. Isolated as its own phase because Task 06 has a hard dependency on this task's output shape.

### 3.1 [x] **🏗️ Judgment Classification Rule Documentation** ([task-05](plan/tasks/task-05-judgment-classification-rule.md))
   **Task:** Replace the `## Judgment Classification Rule` placeholder with a mapping table grounded in `internal/personas/drift.go:23-26` constants: `ConditionNewerMember`→minor (→mechanical slug-bump), `ConditionDeprecation`→major (→re-tune task), `ConditionMissing`→minor by default **unless** it co-occurs with a `ConditionDeprecation` for the same persona in the same `atcr models check --json` run → major. Document the "same persona, same run" grouping rule, the re-tune task payload (persona=`DriftFinding.Persona`, old=`CurrentSlug`, new=`SuggestedSlug` or literal `"none suggested — requires manual selection"`, vendor-guide URL from the persona's `<!-- vendor-guidance: <description>, <url> -->` preamble), and name the judgment agents (brian/`glm-5.1` or cole/`kimi-k2.7-code`). Cross-reference `renderDriftJSON` (`cmd/atcr/models.go:304`) and exit-code 1 as the trigger.
   **Priority:** P2 | **Effort:** S
   1. Read the current `docs/hermes-maintenance-agents.md` (Tasks 03/04 state) to confirm the placeholder heading and match style.
   2. Write the condition→classification mapping table (grounded in actual constants, not paraphrased) with the co-occurrence escalation rule stated explicitly.
   3. Specify the re-tune task payload fields and the exact `<!-- vendor-guidance: ... -->` marker format (test-enforced by `personas/community_test.go`'s `vendorGuidanceRe`).
   4. Name the judgment agents; state no atcr-repo code changes are required.
   5. COMMIT: `git add docs/hermes-maintenance-agents.md && git commit -m "docs(hermes): add judgment classification rule"`
   **Success Criteria:** Each `DriftFinding` condition mapped to minor/major; `ConditionMissing`+`ConditionDeprecation` co-occurrence escalation documented; re-tune payload (incl. vendor-guidance marker format and the "none suggested" literal) unambiguous; judgment agents named; implementable hermes-side with no atcr-repo changes.
   **Files:** `docs/hermes-maintenance-agents.md` (edit) | **Duration:** 0.5d | **AC:** AC2

### 3.2 [x] **Phase 3 — Definition of Done**
   1. `## Judgment Classification Rule` placeholder replaced; mapping grounded in `internal/personas/drift.go` constants.
   2. Co-occurrence escalation rule and re-tune payload spec (the input contract for Task 06) present and unambiguous.
   3. T3: `go test ./...` green (unaffected); markdown clean; no other placeholders disturbed.
   4. DoD Report: `Task 05 DoD Complete — Auto: X/5 | Task-Specific: Y/Z`.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

---

## Phase 4: Integration & Validation — Drafting Agent Contract

**Duration:** 1.5 days
**Focus:** Replace the final Task 03 placeholder with the drafting agent's full contract (input payload from Task 05, edit procedure, structural invariant preserving the persona's mandatory section format, model assignment cross-reference, hard output contract). Closes the last placeholder in `docs/hermes-maintenance-agents.md`, then runs the cumulative adversarial + Definition-of-Done validation pass over the whole sprint.

### 4.1 [x] **🏗️ Drafting Agent Contract Documentation** ([task-06](plan/tasks/task-06-drafting-agent-contract.md))
   **Task:** Replace the `## Drafting Agent Contract` placeholder with: **input** (Task 05's re-tune payload); **edit procedure** (read persona `<!-- vendor-guidance: ... -->` preamble → fetch guide → edit only the body below the preamble → conditionally update the preamble URL/description if the guide moved); **invariant** (the mandatory persona section structure from `docs/personas-authoring.md` — `## Role`/`## Focus`/`## Scope`/`## Severity Rubric`/`## Output Format` (7-column contract)/`## Payload` — preserved byte-for-byte, content-only edits; never touch the paired `.yaml` unless the payload carries a concrete `SuggestedSlug`); **model assignment** (marcus/`openai/qwen-3.7-plus` default, nolan/`glm-5.2` fallback — cross-reference Task 03, don't restate as a new decision); **hard output contract** (separate PR from mechanical, structurally excluded from Task 02's `*.md`-rejecting filter, always a draft, human approval required, never auto-merges, must pass the reused C3 gate before review).
   **Priority:** P2 | **Effort:** S
   1. Read the current `docs/hermes-maintenance-agents.md` (Tasks 03/04/05 state) to confirm the placeholder and match conventions.
   2. Write input, ordered edit procedure, structural invariant, `.yaml`-off-limits rule, model assignment cross-reference, and the hard output contract.
   3. State no atcr-repo code changes are required.
   4. COMMIT: `git add docs/hermes-maintenance-agents.md && git commit -m "docs(hermes): add drafting agent contract"`
   **Success Criteria:** Placeholder replaced with input payload, ordered edit procedure, byte-for-byte 7-section invariant, `.yaml`-off-limits-unless-model-change rule, model assignment cross-reference, and the separate-PR/draft/human-approval/never-auto-merge/must-pass-C3 output contract.
   **Files:** `docs/hermes-maintenance-agents.md` (edit) | **Duration:** 0.5d | **AC:** AC3, AC4, AC5

### 4.2 [x] **Sprint Documentation — CUMULATIVE ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `docs/hermes-maintenance-agents.md` (full file, Tasks 03–06), `.github/workflows/hermes-auto-merge.yml`

   **Spawn a fresh subagent** via the Agent tool to perform this cumulative review. The subagent has no memory of the implementation — intentional, to avoid bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Cumulative adversarial review: sprint 19.8 docs`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/docs/hermes-maintenance-agents.md`, `/Users/samestrin/Documents/GitHub/atcr/.github/workflows/hermes-auto-merge.yml`
     - Checklist (pass verbatim):
       - CONSISTENCY: Is the Role→Agent table internally consistent across the Configuration, Judgment (Task 05), and Drafting (Task 06) sections? Any contradicting model/agent assignment?
       - COMPLETENESS: Are all three placeholder anchors (`## Provisioning`, `## Judgment Classification Rule`, `## Drafting Agent Contract`) fully replaced (no `_To be completed_` text left)?
       - GROUNDING: Do the judgment condition names match `internal/personas/drift.go` constants exactly? Does the drafting invariant match `docs/personas-authoring.md`'s section contract? Is the `<!-- vendor-guidance: ... -->` marker format consistent between Tasks 05 and 06?
       - SAFETY CONTRACT: Does the doc's guardrail/opt-in posture agree with the actual `hermes-auto-merge.yml` behavior (off by default, `.md` excluded, C3 gate reused unmodified)? Any place a prompt PR could be described as auto-mergeable?
       - AC TRACE: Does every plan AC (AC1–AC6) trace to a completed task/section?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh cumulative review, no impl memory):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | docs/hermes-maintenance-agents.md (Guardrail/Provisioning) | Doc never named the `hermes:mechanical` label or the `HERMES_AUTO_MERGE_ENABLED` variable the workflow requires; a maintainer following only the doc would open mechanical PRs that never auto-merge — AC1/AC4 auto-merge capability documented-but-unreachable (fails safe, no wrong merge). | Named the repo variable + label + `.yaml`-only path discipline in Guardrail Contract and Provisioning (cron wrapper applies `--label hermes:mechanical`). |
   | MEDIUM | docs/hermes-maintenance-agents.md (Drafting output contract) | "only when step-5-authorized" referenced a nonexistent step 5 (edit procedure has steps 1–4). | Replaced with a pointer to the `.yaml` off-limits rule (concrete `SuggestedSlug`). |
   | LOW | docs/hermes-maintenance-agents.md (Guardrail branch-protection) | Called `Go CI / Go Lint & Test` the "job name"; `Go Lint & Test` is the job/check-run name, `Go CI` is the workflow. | Clarified which is the selectable required-check entry. |

   **Action Taken:** HIGH fixed inline + committed (required before Final Validation). MEDIUM/LOW were cheap same-file doc-accuracy corrections fixed in the same pass rather than deferred. No CRITICAL. Cumulative adversarial review resolved.

### 4.3 [x] **Phase 4 — Definition of Done (cumulative sprint validation)**
   1. `## Drafting Agent Contract` placeholder replaced; all three anchors now fully populated (no `_To be completed_` remaining).
   2. Cumulative adversarial review (4.2) run by a fresh subagent; CRITICAL/HIGH fixed + committed, MEDIUM/LOW deferred.
   3. Role→Agent table internally consistent across Tasks 03/05/06; every AC (AC1–AC6) traces to a completed task.
   4. T3: `go test ./...` green (unaffected); YAML + markdown well-formed; touched files `gofmt`/lint clean.
   5. DoD Report: `Task 06 DoD Complete — Auto: X/5 | Task-Specific: Y/Z`.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...` green (unaffected — no Go code changed).
- [ ] Coverage meets threshold (≥80% baseline unaffected).
- [ ] Lint/format clean: `gofmt`/`golangci-lint` on touched files; YAML + Markdown well-formed.
- [ ] Build succeeds: `go build ./cmd/atcr`.
- [ ] `docs/hermes-maintenance-agents.md` complete — no unreplaced placeholder anchors.
- [ ] `.github/workflows/hermes-auto-merge.yml` opt-in/off by default; filter fail-closed; no actor gating; permissions minimal.

### Optional: Targeted Mutation Testing
MUTATION_TOOL = UNAVAILABLE for this repo's toolchain, and this sprint introduces no new Go code — **no mutation testing applies**. (If ever run: target only changed files; never full-codebase mutation, which can take hours.)

### Drift Analysis
Compare delivered work against [plan/original-requirements.md](plan/original-requirements.md):
- **AC1** → Task 01 (CI gate reachable/blocking) + Task 02 (mechanical auto-merge on green) + Task 04 (host provisioning prerequisite).
- **AC2** → Task 05 (minor/major classification + re-tune payload with vendor-guide URL).
- **AC3** → Task 06 (drafting agent produces separate prompt-edit PRs from the vendor guide, configurable model).
- **AC4** → Task 01 (no actor bypass) + Task 02 (PR-only, mechanical-may-auto-merge) + Task 06 (prompt PRs human-approved, never auto-merge).
- **AC5** → Task 01/03/06 (reused 19.6 C3 fixture gate + length cap + schema validation as a hard CI gate).
- **AC6** → Task 03 (role→agent + drafting-model config documented, opt-in default, dry-run/preview mode).

Flag any task that drifted from the original request; do not accept scope beyond it.
