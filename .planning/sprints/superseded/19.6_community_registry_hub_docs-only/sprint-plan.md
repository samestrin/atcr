# Sprint 19.6: Community Registry Hub

---
executor: /execute-sprint
execution_mode: continuous
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.6 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

This sprint delivers the single in-repo task of Plan 19.6: updating `docs/personas-install.md` and `README.md` to recommend installing a curated pack of 3 default, model-tuned reviewer personas (Anthropic Claude, OpenAI GPT, Google Gemini) via the existing `atcr personas install` community channel. The persona content itself (YAML + prompt templates + fixtures — Stories 1-2) is authored and published separately in the external `atcr/personas` repo and is tracked here only as an externally-verified dependency; this sprint's own TDD/execution loop does not implement or verify Stories 1-2 directly.

### Why This Matters

New atcr users currently have no ready-to-install personas phrased per a frontier provider's own official prompting guide, so they either hand-write prompts or reuse generic domain personas. Pointing first-time-setup documentation at the new pack turns that gap into a single install command.

### Key Deliverables

- `docs/personas-install.md`'s "Quick walkthrough" section recommends the default persona pack with a runnable `atcr personas install bundle/<name>` example (AC 03-01)
- `README.md`'s "## Quickstart" section adds a step recommending the default pack alongside `atcr init`/provider setup (AC 03-02)

### Success Criteria

- Both files are diffable via `git diff`, each showing only an additive insertion — no reordering or rewriting of existing steps
- The recommendation appears as a distinct, callable step in each section, reusing the already-documented `atcr personas install bundle/<name>` syntax (or a clearly-marked placeholder if Stories 1-2 haven't published real names yet, per the story's documented risk mitigation)

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Pragmatic ⚡ (complexity 2/12, SIMPLE band)

This sprint's only in-repo deliverable (Story 3) is a documentation-only edit — per `test-planning-matrix.md`, all 9 ACs across the whole plan are MANUAL and 0 require a new Go unit/integration/E2E test. There is no failing-test/passing-test cycle to run against markdown prose, so the usual RED/GREEN/REFACTOR cadence is adapted below to DRAFT (identify insertion point + draft step text) → EDIT (apply the additive insertion) → VALIDATE (confirm `git diff` is additive-only and the AC's Definition of Done checklist is satisfied). Stories 1-2 are external, tracked-only, and have no tasks in this sprint.

**Adversarial Review:** Disabled (sprint-design.md's Recommended Flags: adversarial=false, gated=false — complexity and phase count both clear the thresholds for skipping it).

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |

---

## Sprint Conventions

### Testing Tiers

Not applicable to this sprint's deliverable — no automated test framework runs against a markdown-only change. Verification tiers are replaced by:
- **Manual diff review:** `git diff docs/personas-install.md README.md` after each edit
- **DoD checklist review:** cross-check against each AC's Definition of Done (AC 03-01, AC 03-02)

### DoD Verification Checklist

1. Docs: Both files updated additively, existing steps preserved in order and command text
2. Diff: `git diff` scoped to exactly the 2 target files, no unrelated restructuring
3. Consistency: New step in both files reuses the existing `atcr personas install bundle/<name>` syntax (or documented placeholder)
4. Manual Review: AC 03-01 and AC 03-02 Definition of Done checklists both satisfied

### DoD Report Template

```
Story-3 DoD Complete
Docs: 2/2 files updated | Story-Specific: {Y}/{Z}
Manual Review: [ ] Both AC checklists satisfied
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

**Implementation Standards** ([implementation-standards.md](../../../specifications/implementation-standards.md)):
- Black Box Design: modules replaceable via interface only — n/a for this sprint (no code).
- DRY & KISS: keep the inserted doc step additive and minimal, no restructuring beyond what's needed.

**Coding Standards** ([coding-standards.md](../../../specifications/coding-standards.md)): n/a — this sprint introduces no Go code.

**Git Strategy** ([git-strategy.md](../../../specifications/git-strategy.md)):
- Branch: `feature/19.6_community_registry_hub`
- Commit format: `type(scope): description` (Conventional Commits) — e.g. `docs(personas): recommend default pack in quick walkthrough`
- Squash and merge to `main` via PR; at least one approval required; CI (Go CI: format/vet/lint/tests) must pass — unaffected by this sprint since no Go code changes, but the workflow still runs.

---

## External Resources

No specifications in `.planning/specifications/` met the relevance threshold for this plan (per `plan/documentation/source.md`) — this sprint's only in-repo work is editing `docs/personas-install.md` and `README.md` directly, both already read and cited in `plan/codebase-discovery.json`.

Key references already gathered during `/design-sprint`:
- `docs/personas-install.md:51-61` — existing `atcr personas install bundle/<name>` syntax to reuse verbatim in the new step.
- `docs/personas-install.md:156-176` — existing 6-step "Quick walkthrough" section (search → install → list → test → upgrade → remove) to extend additively.
- `README.md:36-57` — existing 6-step "## Quickstart" section (install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`) to extend additively.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Setup & Draft

### 1.1 [ ] **Draft Quick Walkthrough Recommendation** [plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md](plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md)
   **Mode:** Pragmatic (adapted for docs) | **AC:** [03-01](plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md) | **Story:** [03](plan/user-stories/03-recommend-default-persona-pack-in-documentation.md)
   1. DRAFT: Re-confirm the exact insertion point in `docs/personas-install.md`'s "Quick walkthrough" section (currently 6 numbered steps: discover → install → list → test → upgrade → remove).
   2. DRAFT: Check whether Stories 1-2 have published concrete persona/bundle names in the external `atcr/personas` repo yet.
      - If yes: draft the new step using the real bundle name in an `atcr personas install bundle/<name>` example.
      - If no: draft the new step with clearly-generic placeholder language (e.g. "the recommended starter pack — see [releases] for the current bundle name"), per the story's documented risk mitigation. Do not fabricate a command that would 404 against the live registry.
   3. DRAFT: Confirm the drafted example reuses the exact syntax already documented at `docs/personas-install.md:51-61` — no new command form invented.
   **Files:** `docs/personas-install.md` (draft only, no edit yet) | **Duration:** 10-15 min

### 1.2 [ ] **Draft README Quickstart Recommendation** [plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md](plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md)
   **Mode:** Pragmatic (adapted for docs) | **AC:** [03-02](plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md) | **Story:** [03](plan/user-stories/03-recommend-default-persona-pack-in-documentation.md)
   1. DRAFT: Re-confirm the exact insertion point in `README.md`'s "## Quickstart" section (currently 6 numbered steps: install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`).
   2. DRAFT: Draft a new step recommending the default persona pack, positioned near `atcr init`/provider setup — same real-name-or-placeholder judgment call as Task 1.1, kept consistent between both files.
   3. DRAFT: Confirm the new step does not alter or renumber the `atcr review && atcr reconcile` "zero arguments" two-command pipeline framing (README.md:52-53, 63).
   **Files:** `README.md` (draft only, no edit yet) | **Duration:** 10-15 min

---

## Phase 2: Edit, Validate & Refactor

### 2.1 [ ] **Apply Quick Walkthrough Edit** [plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md](plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md)
   **Mode:** Pragmatic (adapted for docs) | **AC:** [03-01](plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md)
   1. EDIT: Insert the Task 1.1 drafted step into `docs/personas-install.md`'s "Quick walkthrough" section, near the top (before or alongside the existing "Discover a persona" / "Install it" steps).
   2. EDIT: Renumber only the shifted steps — preserve the existing 6 steps' relative order and command text unchanged.
   3. COMMIT: `git add docs/personas-install.md && git commit -m "docs(personas): recommend default pack in quick walkthrough"`
   **Files:** `docs/personas-install.md` | **Duration:** 10-15 min

### 2.2 [ ] **Apply README Quickstart Edit** [plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md](plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md)
   **Mode:** Pragmatic (adapted for docs) | **AC:** [03-02](plan/acceptance-criteria/03-02-readme-quickstart-recommends-default-pack.md)
   1. EDIT: Insert the Task 1.2 drafted step into `README.md`'s "## Quickstart" numbered list, positioned alongside `atcr init`/provider setup.
   2. EDIT: Renumber only the shifted steps — preserve the existing 6 steps' original command text and relative order.
   3. COMMIT: `git add README.md && git commit -m "docs(readme): recommend default persona pack in quickstart"`
   **Files:** `README.md` | **Duration:** 10-15 min

### 2.3 [ ] **VALIDATE: Diff Review & DoD** [plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md](plan/acceptance-criteria/03-01-quick-walkthrough-recommends-default-pack.md)
   1. Run `git diff main -- docs/personas-install.md README.md` (or `git log -p` on the 2 commits above) and confirm both diffs are purely additive — no unrelated restructuring or renumbering of existing command text.
   2. Cross-check AC 03-01's Definition of Done: new step present, existing 6 walkthrough steps preserved in order, diff additive-only.
   3. Cross-check AC 03-02's Definition of Done: new step present, existing 6 Quickstart steps preserved in order, diff additive-only, `atcr review && atcr reconcile` framing undisturbed.
   4. Mark Story-3 DoD complete using the report template above.
   **Duration:** 10 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] Both DoD checklists (AC 03-01, AC 03-02) satisfied
- [ ] `git diff` for `docs/personas-install.md` and `README.md` is additive-only
- [ ] No unrelated file changed by this sprint
- [ ] Build succeeds (`go build ./...`) — sanity check only, since no Go source changed
- [ ] Lint/format clean (`go fmt ./...`, `golangci-lint run`) — sanity check only, since no Go source changed

### Optional: Targeted Mutation Testing

Not applicable — mutation tooling (`stryker`/`mutmut`/`cargo-mutants`) was not detected in this environment, and this sprint changes no Go source, so mutation testing has no target regardless.

### Drift Analysis

Compare final state against `plan/original-requirements.md`:
- Confirm the shipped doc edits match the original request's Acceptance Criteria: "`docs/personas-install.md` and the README quickstart recommend installing these personas as part of first-time setup."
- Confirm no scope crept into Stories 1-2's external-repo territory — this sprint touches only `docs/personas-install.md` and `README.md`.
- Note in the completion report whether real persona/bundle names were available at execution time, or whether placeholder language was used pending Stories 1-2's external publication (flag for a follow-up tightening pass if placeholder).
