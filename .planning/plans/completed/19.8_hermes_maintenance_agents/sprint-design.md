# Sprint Design: Hermes Maintenance Agents

**Created:** July 09, 2026 08:36:06PM
**Plan:** [Hermes Maintenance Agents](/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.8_hermes_maintenance_agents/)
**Plan Type:** 🏗️ Infrastructure
**Status:** Design Complete

---

## Original User Request

> Configure scheduled hermes agents to consume Epic 19.7's `atcr models check` primitives and offload the recurring persona-model maintenance across three clearly-separated roles — mechanical (deterministic catalog-diff → slug-bump PR, no LLM), judgment (classify drift; route minor vs. major/deprecation), and drafting (an LLM reads the vendor's updated prompting guide and drafts persona `.md` edits). Every change lands as a pull request, is fixture-gated in CI, and prompt changes require human approval — the agent is a contributor, never a committer to `main`.

**Referenced Resources:** None — the original request targets the epic plan document itself (`.planning/epics/active/19.8_hermes_maintenance_agents.md`), not external files. Structural precedents cited throughout the plan (`docs/github-action.md`, `docs/personas-authoring.md`) are internal repo docs, addressed directly in Work Decomposition below.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Hermes Maintenance Wiring
**Complexity:** 5/12 (MODERATE)
**Timeline:** 5 days
**Phases:** 4
**Pattern:** Item 1: RGR → Item 2: RGR → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
GitHub Actions auto-merge workflow patterns
CI required status check branch protection
bot-authored PR guardrail design
path-based workflow trigger filters
documentation-first configuration surface pattern
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - One new workflow file (`hermes-auto-merge.yml`) plus a new doc file, both built on well-established GitHub Actions constructs (`dorny/paths-filter`, `gh pr merge`) and an explicit in-repo doc precedent (`docs/github-action.md`); no new architectural layer or primitive is introduced.
- **Integration:** 1/3 - Two real integration points: the existing `Go CI` required-check (consumed, not modified) and the GitHub PR-merge surface (`gh pr merge` / REST merge API) for the opt-in auto-merge workflow. The hermes host itself is documented, not integrated with, in this repo.
- **Story/Task & Test:** 2/3 - Six discrete tasks with a real dependency chain (01→02, 03→04, 03→05, 03+05→06), but per-item complexity is low: one CI audit (already completed, zero-diff), one ~150-line workflow file, and four documentation sections in a single shared file — no automated test suite is added since none of the deliverables are Go code.
- **Risk/Unknowns:** 1/3 - The one Critical-impact risk identified (auto-merge accidentally matching a prompt-edit PR) is already mitigated by design (structural path/label filter, never keyed on actor) rather than left open; the sole unresolved unknown is a GitHub branch-protection setting that could not be verified via `gh api` in this environment (403, private-repo tier limit) and is handled as a documented manual maintainer step, not a code risk.

**Time Formula:** `TOTAL_DAYS = sum(per-task effort in days) + 1 day validation buffer`
**Calculation:** Task01 0d (already audited, zero-diff) + Task02 1.5d (M) + Task03 1d (M) + Task04 0.5d (S) + Task05 0.5d (S) + Task06 0.5d (S) + 1d validation/adversarial pass = 5 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** false
**Recommendation strength:** false
**Suggested command:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.8_hermes_maintenance_agents/ --adversarial`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12. This sprint crosses the adversarial threshold on phase count (4 >= 3) despite a below-threshold complexity score — appropriate given the auto-merge workflow (Task 02) is the sprint's one Critical-impact-risk surface and benefits from an adversarial pass verifying the path/label filter cannot be bypassed.

---

## Phase Structure

### Phase 1: Foundation — CI Gate Confirmation & Doc Skeleton
**Duration:** 1 day
**Items:** Task 01 (CI Fixture-Gate Confirmation), Task 03 (Configuration Surface Documentation Skeleton)
**Focus:** Establish the two foundations everything else depends on: confirm the existing CI fixture gate is already reachable/blocking for agent-authored PRs (Task 01 — audit already performed at task-creation time with a documented zero-diff result; this phase re-verifies and formally closes it), and create `docs/hermes-maintenance-agents.md` with its full section skeleton (Overview, Role→Agent Configuration, Drafting Model Default & Fallback, Dry-Run/Preview Mode, Guardrail Contract, plus placeholder anchors for Provisioning/Judgment Classification Rule/Drafting Agent Contract). No dependency between the two tasks — they run independently.

### Phase 2: Core Items — Auto-Merge Workflow & Provisioning Runbook
**Duration:** 1.5 days
**Items:** Task 02 (Opt-In Auto-Merge Policy), Task 04 (Hermes Host Provisioning Runbook)
**Focus:** Build the sprint's one code artifact — `.github/workflows/hermes-auto-merge.yml`, an opt-in, off-by-default workflow whose structural path/label filter (never actor-based) is this sprint's Critical-risk surface — depending on Task 01's confirmed check name. In parallel, append the `## Provisioning` section to the Task 03 skeleton documenting one-time `atcr` binary + repo checkout + `gh` auth setup on nucleus.lan, depending only on Task 03's skeleton existing.

### Phase 3: Advanced — Judgment Classification Rule
**Duration:** 1 day
**Items:** Task 05 (Judgment Classification Rule Documentation)
**Focus:** Replace the Task 03 placeholder with the minor/major classification mapping grounded in `internal/personas/drift.go`'s actual `DriftFinding.Condition` constants, the `ConditionMissing` + co-occurring `ConditionDeprecation` escalation edge case, and the re-tune task payload spec that Phase 4's Task 06 consumes as its input contract. Isolated as its own phase because Task 06 has a hard dependency on this task's output shape, not just its existence.

### Phase 4: Integration & Validation — Drafting Agent Contract
**Duration:** 1.5 days
**Items:** Task 06 (Drafting Agent Contract Documentation), cumulative Definition-of-Done validation
**Focus:** Replace the final Task 03 placeholder with the drafting agent's full contract (input payload from Task 05, edit procedure, structural invariant preserving the persona's 7-section format, model assignment cross-reference, hard output contract). Closes the last placeholder in `docs/hermes-maintenance-agents.md`, so this phase also runs the cumulative validation pass: confirm all three placeholder anchors are replaced, the Role→Agent table is internally consistent across Tasks 03/05/06, and every plan AC (AC1–AC6) traces to a completed task.

---

## Work Decomposition

Grounded in the plan's existing `tasks/` files (WORK_ITEM_SOURCE = tasks) — no new decomposition performed, existing task scope preserved as-is.

| Task | AC Coverage | Testable Element | Test Type |
|------|-------------|-------------------|-----------|
| Task 01: CI Fixture-Gate Confirmation | AC1, AC4, AC5 | `.github/workflows/ci.yml` has no `github.actor`/bot-exclusion condition on the `pull_request` trigger or `ci` job (`Go Lint & Test`, ci.yml:18, `runs-on: [self-hosted, gauntlet]`); `go test ./...` (ci.yml:61) reaches `internal/personas/...` and `internal/registry/...` under the root `go.mod` | Static audit (full-file read) — already performed, zero-diff result documented |
| Task 02: Opt-In Auto-Merge Policy | AC1, AC4 | New `.github/workflows/hermes-auto-merge.yml`: default-disabled via a repo variable gate; structural path filter matches only `personas/community/*.yaml` and/or `hermes:mechanical` label, never `personas/community/*.md`; merge only proceeds after `Go Lint & Test` reports success; permissions scoped to `contents: write` + `pull-requests: write` only | Manual/mock-PR verification (3 scenarios: mechanical-path match, prompt-path non-match, opt-in-disabled no-op) — no `act`/local-runner available, so verified via scratch PR or workflow dry-read |
| Task 03: Configuration Surface Documentation Skeleton | AC4, AC5, AC6 | `docs/hermes-maintenance-agents.md` created with Overview, Role→Agent Configuration table (exact SSH-confirmed rows: mechanical/`no_agent`/none, judgment/brian-or-cole/`glm-5.1`-or-`kimi-k2.7-code`, drafting/marcus-default-nolan-fallback/`qwen-3.7-plus`-or-`glm-5.2`), Drafting Model Default & Fallback, Dry-Run/Preview Mode, Guardrail Contract, plus 3 placeholder anchors | Manual read-through comparing structure against `docs/github-action.md` precedent |
| Task 04: Hermes Host Provisioning Runbook | AC1 | `## Provisioning` section appended: `atcr` binary install, repo checkout under `~/docker/hermes/`, `gh auth`/token setup, mandatory pull-before-run step, cross-reference to `data/profiles/<agent>/cron/jobs.json` shape | Manual (out-of-repo, tracked by maintainer on nucleus.lan, not CI) |
| Task 05: Judgment Classification Rule Documentation | AC2 | `## Judgment Classification Rule` section replaces placeholder: `ConditionNewerMember`→minor, `ConditionDeprecation`→major, `ConditionMissing`→minor unless co-occurring with `ConditionDeprecation` for the same persona in the same run→major; re-tune task payload spec (persona, old/new model, vendor-guidance URL parsed from `<!-- vendor-guidance: ... -->`) | Manual read-through against `internal/personas/drift.go` condition constants and `personas/community_test.go`'s `vendorGuidanceRe` marker format |
| Task 06: Drafting Agent Contract Documentation | AC3, AC4, AC5 | `## Drafting Agent Contract` section replaces placeholder: input (Task 05's payload), edit procedure (read preamble → fetch guide → edit body only → conditionally update preamble), invariant (7-section structure preserved byte-for-byte), model assignment cross-reference, hard output contract (separate PR, draft, human approval required, never auto-merge, must pass C3 gate) | Manual read-through against `docs/personas-authoring.md`'s section contract and an actual persona `.md` (`personas/community/anthony.md:1`) |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files alongside source (standard Go convention; 325 existing test files in the repo). This sprint introduces no new Go code, so no new `_test.go` files are added — Tasks 01–06 are CI-workflow YAML and Markdown documentation only.

**Test File Placement Examples:**
- N/A for this sprint's own deliverables (workflow YAML + docs). Existing fixture-gate tests this sprint depends on but does not modify: `internal/personas/community_fixture_test.go`, `internal/personas/community_schema_test.go`, `internal/registry/validate.go`'s `ValidateCommunityPersonaYAML`.

**Unit/Integration/E2E:**
- **Unit:** None required — no Go code changes.
- **Integration:** Task 02's auto-merge workflow is verified via 3 manual scenarios against a scratch/mock PR (mechanical-path-only match with label → triggers; prompt-`.md`-path match → never triggers, even with a matching label or bot author; opt-in variable unset → no-op regardless of path match). No `act`/local GitHub Actions runner is available in this environment, so verification is scratch-PR-based, not automated.
- **E2E:** Out of repo scope — full mechanical-agent-to-merge flow requires the hermes host (nucleus.lan) provisioned per Task 04's runbook; tracked by the maintainer post-sprint, not by this sprint's CI.

**Test Environment Status:**
- Framework: `go test` (standard library `testing`, some `testify` usage) — configured and green; unaffected by this sprint.
- Execution: `go test ./...` runs via `.github/workflows/ci.yml`'s `Go Lint & Test` job on every `pull_request`/`push` to `main`; this sprint's PR(s) ride that same existing gate.
- Coverage Tools: `go test -coverprofile=coverage.out ./...` configured in `config.yaml`; coverage baseline 80% — not affected, since no new Go code is introduced.

---

## Architecture

**Primitives:** `DriftFinding` (from `internal/personas/drift.go`: `Persona`, `Condition` ∈ {`ConditionNewerMember`, `ConditionDeprecation`, `ConditionMissing`}, `CurrentSlug`, `SuggestedSlug`, `ExpirationDate`) is the sole data primitive this sprint's documentation is built around — consumed read-only via `atcr models check --json` (`renderDriftJSON`, `cmd/atcr/models.go:304`). The `<!-- vendor-guidance: <description>, <url> -->` HTML-comment preamble on every community persona `.md` is the second primitive: the drafting agent's sole edit anchor.

**Module Boundaries:** This sprint adds no new atcr-repo module. It documents a boundary that already exists structurally: mechanical PRs touch `personas/community/*.yaml` only; prompt PRs touch `personas/community/*.md` only (and conditionally the paired `.yaml`, per Task 06's explicit exception). The auto-merge workflow (Task 02) is the boundary's sole enforcement point in-repo — a path/label filter, never an actor check.

**External Dependencies:** GitHub Actions runner (`[self-hosted, gauntlet]`, existing), GitHub PR-merge surface (`gh pr merge` / REST API, new usage in Task 02), `dorny/paths-filter` or equivalent `git diff --name-only` path-matching (new usage in Task 02). No new Go module dependency — confirmed in plan.md ("No high-ROI packages identified").

**Replaceability:** The auto-merge workflow is a single, self-contained YAML file, off by default via a repository variable — it can be deleted or disabled without affecting the CI fixture gate it depends on (Task 01) or any other workflow. The documentation (`docs/hermes-maintenance-agents.md`) is a single file with independently-addressable sections (one per task), so any section can be revised without restructuring the others — mirrored by Task 03's explicit placeholder-anchor design for Tasks 04–06.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| Auto-merge trigger filter (Task 02, `.github/workflows/hermes-auto-merge.yml`) | Any PR opened against `main` while the opt-in workflow is enabled | A crafted PR mimics the mechanical-path shape (e.g., renames a prompt-edit `.md` change to also touch a `.yaml` file, or forges the `hermes:mechanical` label) to slip an unreviewed prompt change past auto-merge | Structural allow-list filter (`personas/community/*.yaml` and/or lockfile paths only) that explicitly rejects the run if **any** changed file falls outside the allow-list — a single `.md` file in the diff disqualifies the whole PR from auto-merge regardless of label |
| Auto-merge authorization scope | GitHub Actions workflow permissions block | An overly broad `permissions:` grant (e.g. `actions: write`, `checks: write`) could let a compromised or malicious dependency in the merge step escalate beyond "merge this one PR" | Task 02 mandates minimum permissions: `contents: write` + `pull-requests: write` only, explicitly documented as the ceiling |
| Actor-based bypass in either CI or auto-merge | `.github/workflows/ci.yml` (Task 01) and the new auto-merge workflow (Task 02) | A condition keyed on `github.actor`, PR-author login, or "is this a bot" would let anything impersonating a trusted actor skip the fixture gate or qualify for auto-merge | Task 01 confirms zero actor-based conditions exist in `ci.yml`; Task 02 requires an explicit inline comment banning actor/bot-login-based gating, mandating path/label-only filters |
| Branch-protection required-check configuration | GitHub repo settings (`main` branch protection rule) | If `Go Lint & Test` is not marked as a **required** status check at the repo-settings level, a PR could merge despite a failing/absent check, independent of any workflow-level correctness | Could not be verified programmatically in this environment (`gh api .../branches/main/protection` → 403, private-repo tier limit); documented as an explicit manual maintainer verification step in Task 03's doc, not silently assumed |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| N/A | This sprint introduces no runtime code path, service, or hot loop — its deliverables are a CI workflow (fires only on `pull_request` events) and static documentation. No performance-critical path exists to analyze. | — | — |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| `ConditionMissing` co-occurrence | A `ConditionMissing` finding for persona X appears in the same `atcr models check --json` run as a `ConditionDeprecation` finding for the same persona X | Judgment rule (Task 05) must escalate to major/re-tune, not route as minor — since the missing slug and the deprecation are the same underlying event, not two independent conditions |
| Mixed-path PR diff | A PR (however opened) touches both a `personas/community/*.yaml` file and a `personas/community/*.md` file in the same diff | Task 02's filter must reject auto-merge entirely — presence of any disallowed path disqualifies the whole PR, it does not partially match |
| Vendor guide unreachable at draft time | The drafting agent's fetch of the vendor-guidance URL (from the persona's preamble) fails or returns stale/changed content | Task 06's contract: draft is still opened but is explicitly a draft requiring human approval — a bad fetch degrades draft quality, never bypasses the review gate |
| Auto-merge opt-in variable unset | `.github/workflows/hermes-auto-merge.yml` runs on a repo where `vars.HERMES_AUTO_MERGE_ENABLED` was never set | Workflow must no-op entirely regardless of whether the path/label filter would otherwise match — default-disabled is the safe default |
| Re-tune task with no suggested model | `DriftFinding.SuggestedSlug` is empty (pure deprecation or a missing slug with no catalog baseline) | Task 05/06 payload spec must carry the literal `"none suggested — requires manual selection"` marker rather than a fabricated slug, and Task 06's contract forbids touching the paired `.yaml` unless a concrete `SuggestedSlug` is present |

### Defensive Measures Required

- **Input Validation:** Task 02's path/label filter is the sprint's sole input-validation surface — it must be allow-list-based (reject-by-default outside `personas/community/*.yaml`/lockfile paths), never deny-list-based, so an unanticipated path shape fails closed.
- **Error Handling:** N/A for documentation tasks. For Task 02, the workflow must no-op (not error/crash the job) on any non-matching PR or unset opt-in variable — a benign no-op, not a failed run, is the correct outcome for the common case.
- **Logging/Audit:** GitHub Actions' own run log is sufficient audit trail for the auto-merge workflow (every trigger, filter evaluation, and merge action is already logged by the platform) — no additional logging mechanism is introduced or required.
- **Rate Limiting:** N/A — this sprint does not introduce a network-facing service; the drafting agent's vendor-guide fetch (hermes-side, documented not built) is out of this repo's runtime.
- **Graceful Degradation:** Task 06's contract already specifies the correct degradation path for a failed/stale vendor-guide fetch (draft still opens, human review is the safety net, cost is review time not correctness) — no additional fallback logic needed in-repo.

---

## Risks

**Technical:**
- Risk: Auto-merge-on-green misconfigured merges an unintended mechanical change → Mitigation: narrow path/label filter (Task 02), default off, identical fixture-gate requirement as any human PR.
- Risk: A prompt-edit PR is mistakenly matched by the mechanical auto-merge filter, bypassing human review → Mitigation: structurally distinct paths (`.yaml` vs `.md`) enforced by convention across separate hermes cron jobs/skills (documented, not code-enforced in this repo) plus the allow-list filter's fail-closed design.
- Risk: Hermes host provisioning (Task 04) drifts from the atcr repo, causing false drift reports → Mitigation: pull-before-run step documented as mandatory in the runbook.
- Risk: Vendor prompting-guide fetch fails or returns stale content → Mitigation: drafting-agent output is explicitly a draft requiring human approval; cost is review time, not correctness.

**TDD-Specific:**
- Risk: Task 02's workflow YAML has no automated test coverage (GitHub Actions workflows are not directly unit-testable with `go test`) → Mitigation: 3 explicit manual/mock-PR verification scenarios specified in Test Strategy above, to be executed as part of Phase 2/4 validation before the sprint's Definition of Done is marked complete.
- Risk: Documentation-only tasks (03–06) have no executable verification at all → Mitigation: each task's Success Criteria specifies an exact manual read-through comparison target (`docs/github-action.md` structure, `internal/personas/drift.go` constants, `personas/community_test.go`'s `vendorGuidanceRe`, `docs/personas-authoring.md`'s section contract) rather than "looks right," making the manual check falsifiable.

---

**Next:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.8_hermes_maintenance_agents/`
