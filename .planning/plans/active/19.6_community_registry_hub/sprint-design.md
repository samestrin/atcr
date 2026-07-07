# Sprint Design: Default Model-Tuned Community Personas

**Created:** July 06, 2026
**Plan:** [Default Model-Tuned Community Personas](plan.md)
**Plan Type:** Feature
**Status:** Design Complete

---

## Original User Request

> Ship a curated set of default, model-tuned reviewer personas — prompt phrasing that follows each target model's official prompting guide (e.g. Anthropic's Claude guidelines, OpenAI's GPT-4 guidelines) — distributed through atcr's existing community-persona channel (`atcr personas install`), so a new user gets a well-tuned review panel with a single install command instead of hand-authoring prompts from scratch. Refined via Clarifications (2026-07-06) to 3 personas — Anthropic Claude, OpenAI GPT, Google Gemini — each with a flagship-primary + same-family-fallback model pair.

**Referenced Resources:**
- [Persona Authoring Contract](../../../../docs/personas-authoring.md)
  - **Summary:** Defines the registry-agent schema (`provider`/`model` required; `persona`/`role`/`language` optional), the canonical prompt-template section structure, and the community-repo contribution checklist (fixture naming, synthetic-only content).
  - **Key Points:** Required template variables (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`) must render with no leftovers; fixtures live at `personas/testdata/<slug>_fixture.patch`.
- [Persona Install Docs](../../../../docs/personas-install.md)
  - **Summary:** Documents the `atcr personas install/search/list/upgrade/remove/test` CLI surface, `ATCR_PERSONAS_URL` default, and a 6-step "Quick walkthrough" (lines 156-176) this plan's Story 3 extends.
  - **Key Points:** Existing `bundle/<name>` install syntax already documented at lines 51-61 — Story 3 reuses this syntax rather than inventing new command forms.
- [README Quickstart](../../../../README.md)
  - **Summary:** Top-level 6-step onboarding sequence (install → `atcr init` → provider setup → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report`), lines 36-57.
  - **Key Points:** Story 3 inserts one additive step here recommending the default persona pack, without disturbing the "zero arguments" two-command pipeline framing.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Default Persona Pack Docs
**Complexity:** 2/12 (SIMPLE)
**Timeline:** 1 day
**Phases:** 2
**Pattern:** Setup & Draft → Edit, Validate & Refactor

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
documentation quickstart onboarding recommendation patterns
additive markdown doc edit conventions
community persona install discoverability docs
cross-repo epic in-repo scope boundary
external dependency tracked not implemented
```

---

## Complexity Breakdown

- **Architecture:** 0/3 - Existing patterns; pure documentation edit reusing the already-documented `atcr personas install bundle/<name>` syntax, no new code path or command introduced.
- **Integration:** 0/3 - Self-contained; touches exactly 2 markdown files in this repo (`docs/personas-install.md`, `README.md`). Stories 1-2 (persona content + index.json publication) land entirely in the external `atcr/personas` repo and are out of this sprint's execution scope — tracked as an external dependency, not implemented here.
- **Story/Task & Test:** 1/3 - 1 in-repo story (Story 3) with 2 manual ACs (03-01, 03-02); per `test-planning-matrix.md`, 9/9 ACs across the whole plan are MANUAL — no automated test framework applies to this sprint's actual deliverable.
- **Risk/Unknowns:** 1/3 - Minor unknown: Story 3 is best sequenced after Stories 1-2 publish real persona/bundle names in the external repo, so the doc edit can cite a concrete install command instead of a placeholder; the story's own documented mitigation (generic placeholder language, tightened later) resolves this without blocking work.

**Time Formula:** SIMPLE complexity (0-3) baseline is 1-3 days.
**Calculation:** Score 2/12 sits at the low end of the SIMPLE band — the only in-repo deliverable is a 2-file additive markdown insertion with no automated test surface, so the estimate holds at the 1-day floor rather than the 3-day ceiling.

---

## Recommended Flags

**Adversarial:** false
**Gated:** false
**Recommendation strength:** n/a
**Suggested command:** `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12. This sprint clears none of these — phase count is deliberately kept at 2 (rather than the SIMPLE band's alternative of 3) since the actual in-repo work is a trivial, low-risk doc insertion.

---

## Phase Structure

### Phase 1: Setup & Draft (~0.5 day)
- Confirm exact insertion points in both target files (already verified: `docs/personas-install.md` "Quick walkthrough" section lines 156-176; `README.md` "## Quickstart" section lines 36-57).
- Draft the new recommendation step for each file, using generic placeholder language (e.g. "the recommended starter pack") if Stories 1-2 have not yet published concrete persona/bundle names at execution time — per Story 3's documented risk mitigation.
- Confirm the drafted command examples reuse the existing `atcr personas install bundle/<name>` syntax already documented at `docs/personas-install.md:51-61`.

### Phase 2: Edit, Validate & Refactor (~0.5 day)
- Apply the additive edit to `docs/personas-install.md`: insert a new early step into the "Quick walkthrough" section recommending the default persona pack, preserving the existing 6 steps' order and command text.
- Apply the additive edit to `README.md`: insert one new step into the "## Quickstart" numbered list, positioned alongside `atcr init`/provider setup, without disturbing the `atcr review && atcr reconcile` "zero arguments" pipeline framing.
- Validate: `git diff docs/personas-install.md README.md` shows only additive insertions (no unrelated restructuring or renumbering of existing command text); confirm both DoD checklists (AC 03-01, AC 03-02) are satisfied.

---

## Work Decomposition

### Story 1: Author Model-Tuned Persona Content (external, tracked — not implemented by this sprint)
Entire implementation happens in the external `atcr/personas` repo (persona YAML + prompt templates + fixtures for Anthropic Claude, OpenAI GPT, Google Gemini). This sprint's TDD/execution loop has no branch, commit, or CI surface to run against for this story. ACs 01-01 through 01-05 are tracked as externally-verified dependencies (confirmed later via live `atcr personas search`/`install`/`test`), not executed by `/execute-sprint`.

### Story 2: Publish Personas to Community Registry Index (external, tracked — not implemented by this sprint)
Content addition to `index.json` in the external `atcr/personas` repo. Depends on Story 1's YAML/fixtures existing and passing validation first. ACs 02-01 and 02-02 are tracked as externally-verified dependencies (live `atcr personas search`/`install`/`list` against the published repo), not executed by `/execute-sprint`.

### Story 3: Recommend Default Persona Pack in Documentation (in-repo — this sprint's actual deliverable)
- **Testable element 1 (AC 03-01):** `docs/personas-install.md`'s "Quick walkthrough" section gains a new step recommending the default 3-persona pack with a runnable `atcr personas install bundle/<name>` example (or clearly-marked placeholder); existing 6 steps preserved in order.
- **Testable element 2 (AC 03-02):** `README.md`'s "## Quickstart" section gains one new numbered step recommending the default pack, positioned near `atcr init`/provider setup; existing 6 steps preserved in order and command text.
- **Verification:** `git diff` scoped to both files confirms additive-only changes; manual review against each AC's Definition of Done checklist.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** N/A — no automated test framework applies to this sprint's deliverable (docs-only edit).
**Test File Placement Examples:** N/A — no test files created or modified.

**Unit/Integration/E2E:** None required. Per `test-planning-matrix.md`, all 9 plan ACs are MANUAL; Story 3's 2 ACs (03-01, 03-02) verify via `git diff` and rendered-markdown review, not via `go test`.

**Test Environment Status:**
- Framework: N/A for this sprint's scope — no Go test framework applies to a markdown-only change.
- Execution: Manual — `git diff docs/personas-install.md README.md` reviewed against each AC's Definition of Done checklist; optionally render both files locally/via GitHub preview to confirm the new step reads naturally.
- Coverage Tools: N/A — this sprint introduces no code, so `go test -coverprofile` and the project's 80% coverage baseline are unaffected.

---

## Architecture

**Primitives:** N/A — no new data types. The only "data" is literal markdown text (a recommendation step + example command) and, for Stories 1-2 (external, not this sprint's scope), persona YAML/prompt-template content in the `atcr/personas` repo.
**Module Boundaries:** This sprint touches exactly 2 files in this repo: `docs/personas-install.md` ("Quick walkthrough" section) and `README.md` ("## Quickstart" section). No Go package, interface, or CLI command is added or modified.
**External Dependencies:** The `atcr/personas` community repo (Stories 1-2, external — this sprint neither implements nor verifies it directly); the existing, unmodified `internal/personas/client.go` fetch mechanism that `atcr personas install/search` already uses.
**Replaceability:** N/A — no code component is introduced; the doc edit is plain markdown, trivially revertable via `git revert` if the inserted recommendation needs to change.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| None in-repo | This sprint's deliverable is a documentation-only edit to 2 markdown files; no code, credentials, or network calls are introduced | N/A | N/A |
| (Informational) External repo content | Stories 1-2's persona YAML/fixtures (external `atcr/personas` repo, not this sprint's scope) must contain no secrets/credentials per the existing contribution checklist | Fixture or template accidentally embeds a real API key/token instead of a synthetic placeholder | Already covered by the existing external contribution checklist (`docs/personas-authoring.md`); not new attack surface introduced by this sprint |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| N/A | This sprint introduces no runtime code path | N/A | N/A — static documentation change only |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Placeholder-name sequencing | Story 3 implemented before Stories 1-2 publish real persona/bundle names | New doc step uses clearly-generic placeholder language (e.g. "the recommended starter pack") rather than a fabricated command that would 404 against the live registry; tightened later once real names exist |
| Structural preservation | Insertion of the new step into either file's existing numbered sequence | Existing steps retain their original relative order and command text; only step numbers shift to accommodate the insertion — no reordering or rewriting of unrelated content |
| Pipeline-framing preservation (README only) | New step inserted near `atcr review && atcr reconcile`'s "zero arguments" two-command framing | New step is positioned as an optional/recommended enhancement to first-time setup (near `atcr init`/provider setup), not altering or renumbering the core review/reconcile pipeline steps |

### Defensive Measures Required

- **Input Validation:** N/A — no user input surface; this is static markdown.
- **Error Handling:** N/A — no runtime code path.
- **Logging/Audit:** N/A.
- **Rate Limiting:** N/A.
- **Graceful Degradation:** The inserted doc language must degrade gracefully to generic placeholder text if Stories 1-2 haven't published real persona/bundle names by the time Story 3 is implemented, per the story's own documented risk mitigation — avoiding a runnable-looking command that would fail against the live registry.

---

## Risks

**Technical:**
- Risk: Story 3 doc edit ships before Stories 1-2 publish real persona/bundle names, forcing placeholder language → Mitigation: use generic placeholder phrasing per the story's documented fallback; flag for a follow-up tightening pass once real names exist.
- Risk: Edit disrupts the existing "Quick walkthrough" or numbered Quickstart structure → Mitigation: treat the edit as strictly additive (insert one step, do not reorder or rewrite existing steps); verify via `git diff` before considering the sprint done.

**TDD-Specific:**
- Risk: This sprint has no automated test to gate correctness (docs-only, 0/9 ACs require a new Go test) → Mitigation: Definition of Done is verified via `git diff` scoped to the 2 files plus manual checklist review against AC 03-01/03-02, not `go test`.
- Risk: Stories 1-2's externally-tracked completion could be mistaken for something this sprint's own test/build gates verify → Mitigation: `plan.md`/`sprint-design.md` explicitly scope this sprint's Definition of Done to Story 3 only; Stories 1-2 remain externally-verified dependencies.

---

**Next:** `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`
