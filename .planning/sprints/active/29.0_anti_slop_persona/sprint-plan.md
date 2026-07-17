# Sprint 29.0: Anti-Slop Persona Simon

---
executor: /execute-sprint
execution_mode: continuous
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 29.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A new community-registry persona, `simon`, hyper-focused on hunting down and flagging AI-generated code bloat — tautological comments, unnecessary abstractions, defensive-programming overkill, and dead/hallucinated code paths. The persona ships as a `simon.yaml`/`simon.md` unit fully wired into the existing fixture, roster, and index test gates, paired with a refresh of the already-drafted marketing outline that pitches it.

### Why This Matters

Engineering teams using AI coding assistants accumulate costly "slop" that today requires manual cleanup (or paid services charging $10k/week). ATCR's persona architecture can ship a free, automated, one-command alternative — but only if `simon` is fully registered and its marketing outline points at commands that actually exist.

### Key Deliverables

- `personas/community/simon.yaml` + `personas/community/simon.md` — the anti-slop persona unit, modeled on `sonny.yaml`/`sonny.md`
- `personas/community/testdata/simon_fixture.patch` — synthetic slop fixture, plus `simon`'s registration in the `communityPersonas` Go roster and `personas/community/index.json`
- Verified/refreshed `.planning/product/content/blog/slopfix-ai-code-bloat.md` — invalid CTA replaced, category-word framing reconciled

### Success Criteria

- `simon.yaml` strict-schema decodes; `simon.md` passes `ValidateFetchedPersonaPrompt` with a hyper-focused, non-colliding anti-slop `## Focus` section
- `go test ./personas/... ./internal/personas/... ./internal/registry/...` is fully green with `simon` included (roster, index, differentiation, category, fixture gates all pass)
- `atcr personas test simon` succeeds as a manual no-LLM structural smoke check
- Blog outline contains zero `--persona simon` references and at least one `atcr personas install/test simon` reference, with category-word framing matching the shipped `simon.md`

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (complexity 4/12 — MODERATE) for Stories 1-2 (RED → GREEN → REFACTOR, 2-task cadence).
**Adversarial:** ENABLED 🎯 — a fresh subagent reviews each story's GREEN diff before REFACTOR. Inline-fix bar: **CRITICAL/HIGH** (fixed in REFACTOR); **MEDIUM/LOW** deferred to `clarifications/tech-debt-captured.md`.
**Story 3** is content-only (Markdown outline review/refresh, no Go code) — no automated test coverage exists per `plan/test-planning-matrix.md`, so it uses a task-based verify-and-edit cadence instead of RED/GREEN, but still receives the adversarial review pass per `--adversarial`.
**Gated:** Disabled — sprint runs continuously through all phases without stopping at boundaries.

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

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./personas/... -run TestCommunityPersonas` |
| T2: Module | After completing element | `go test ./personas/...` |
| T3: Full | DoD validation, pre-commit | `go test ./personas/... ./internal/personas/... ./internal/registry/...` |

### DoD Verification Checklist
1. Tests (T3): All passing
2. Coverage: ≥80%
3. Lint: No errors
4. Build: Succeeds
5. Docs: Updated

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

### Implementation Standards

**Core Philosophy:** "It's faster to write five lines of code today than to write one line today and then have to edit it in the future." Black-box interfaces, replaceable components, single-responsibility modules, primitive-first design. Avoid leaky abstractions, hard-coded dependencies, god objects, premature optimization, hidden side effects.

### Coding Standards (Go)

- **Naming:** Packages lowercase/single-word; exported `PascalCase`; unexported `camelCase`; files snake_case or lowercase.
- **Imports:** stdlib → third-party → internal (`github.com/samestrin/atcr/...`), arranged via `goimports`.
- **Error Handling:** Return `error` as the last param; never ignore errors; wrap with `fmt.Errorf("doing action: %w", err)`.
- **Testing:** Table-driven tests; `*_test.go` co-located with source; `testify/assert`/`require` for assertions.
- **Quality Gates:** `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...` before commit.

### Git Strategy

- Trunk-based: `feature/short-description` branches from `main`, squash-merged.
- Conventional Commits: `type(scope): description` (`feat`, `fix`, `docs`, `refactor`, `test`, `chore`).
- CI gating: PRs cannot merge if the `Go CI` workflow (format, vet, lint, unit tests) fails.

---

## External Resources

**Critical (read before starting):**
- [persona-yaml-and-prompt-authoring.md](plan/documentation/persona-yaml-and-prompt-authoring.md) — `simon.yaml`/`simon.md` authoring contract: yaml.v3 strict-schema decode (`KnownFields(true)`, fixed recognized-key set) and the `text/template` prompt-rendering pattern (8 allow-listed bare tokens + one `{{if .ToolsEnabled}}...{{end}}` block; single leading vendor-guidance citation comment).
- [test-gate-and-fixture-verification.md](plan/documentation/test-gate-and-fixture-verification.md) — how `personas/community_test.go`'s `communityPersonas` roster and embedded-set gates use testify `assert`/`require` to verify fixture, differentiation (0.85 Jaccard ceiling via `TestCommunityPersonas_Differentiation`), category uniqueness, and index registration.

**Package references:** `.planning/specifications/packages/yaml-v3.md`, `.planning/specifications/packages/standard-library.md`, `.planning/specifications/packages/testify.md`

---

## Sprint Phases

(TDD mode: Moderate 🔄 for Stories 1-2; task-based for Story 3. Adversarial review runs in a fresh subagent per `--adversarial`. Inline-fix bar: CRITICAL/HIGH. Defer: MEDIUM/LOW.)

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Story 1 — Author the `simon` Persona Unit (RGR)

**Duration:** 1 day | **Covers:** AC 01-01, AC 01-02, AC 01-03

> RED state is implicit: `simon` does not yet exist, so no `CommunityNames()` entry, no roster row. GREEN is reached when `simon.yaml` strict-decodes and `simon.md` passes `ValidateFetchedPersonaPrompt`, with `## Focus` hyper-focused on the four anti-slop targets and a new, unclaimed category word (e.g. `bloat`) embedded verbatim. **Expected, documented gap:** `TestCommunityAccessors` and `TestTemplateFixtureRunner_CommunityPersonasPass` go red the moment `simon.md` lands (auto-discovered via `go:embed`) until Phase 2's registration closes the loop — this is intentional per AC 01-03, not a regression. Stories 1 and 2 merge to `main` as a single unit.

### 1.1 [ ] **[Author the `simon` Persona Unit - RED](plan/user-stories/01-author-the-simon-persona-unit.md)**
   **Mode:** Moderate | **AC:** [01-01](plan/acceptance-criteria/01-01-simon-yaml-schema-binding.md), [01-02](plan/acceptance-criteria/01-02-simon-md-template-structure-focus.md), [01-03](plan/acceptance-criteria/01-03-simon-authoring-contract-consistency.md)
   1. Confirm the RED baseline: `simon` slug not present anywhere in `personas/community/`; grep the 13 claimed category words (coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant) to confirm `bloat` (or chosen alternative) is unclaimed.
   2. No new `*_test.go` files are authored (existing table-driven suites in `internal/personas/`, `internal/registry/` auto-iterate `personas.CommunityNames()` once `simon.md` exists) — verify this auto-discovery mechanism by reading `personas/community.go` and `internal/registry/persona_test.go`.
   3. Run `go test ./internal/personas/... ./internal/registry/...` and confirm it is green pre-change (baseline for later comparison).
   **Files:** `personas/community.go` (read-only), `internal/registry/persona_test.go` (read-only) | **Duration:** ~1 hr

### 1.2 [ ] **[Author the `simon` Persona Unit - GREEN](plan/user-stories/01-author-the-simon-persona-unit.md)**
   Copy `personas/community/sonny.yaml` → `personas/community/simon.yaml`: set `name: simon`, fresh `description`, `provider: openrouter`, a concrete existing-catalog `model`, `persona: simon`, `role: reviewer`. Copy `personas/community/sonny.md` → `personas/community/simon.md`: replace the vendor-guidance citation with anti-bloat/conciseness prompting guidance; rewrite `## Role` framing Simon as the panel's anti-slop lens; write `## Focus` as a numbered list covering (1) tautological/apologetic AI comments, (2) unnecessary design patterns (factories, interfaces) over simple logic, (3) defensive-programming overkill (redundant null/nil checks where type safety already guarantees non-nil), (4) dead or hallucinated code paths — embedding the chosen category word verbatim. Keep every template token bare and the single `{{if .ToolsEnabled}}...{{end}}` block untouched in position.
   Run `go test ./internal/personas/... ./internal/registry/...` (T1) — confirm `simon.yaml`/`simon.md` pass strict-schema and prompt-structure checks in isolation. **Expected red:** `TestCommunityAccessors` / `TestTemplateFixtureRunner_CommunityPersonasPass` in `personas/` (documented gap, closes in Phase 2).
   COMMIT: `git commit -m "feat(personas): author simon anti-slop persona unit (green)"`
   **Files:** `personas/community/simon.yaml`, `personas/community/simon.md` | **Duration:** ~3 hrs

### 1.2.A [ ] **[Author the `simon` Persona Unit - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-author-the-simon-persona-unit.md)**
   **Changed Files:** `personas/community/simon.yaml`, `personas/community/simon.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `personas/community/simon.yaml`, `personas/community/simon.md`
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (specifically: does `simon.md` stay within the 8-token allow-list + single `{{if .ToolsEnabled}}...{{end}}` block, with no range/with/template/define/pipeline/field-chain smuggled in?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (specifically: category word collision against the 13 claimed words; slug regex `^[a-z]+$`; non-placeholder provider/model)
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.3 [ ] **[Author the `simon` Persona Unit - REFACTOR](plan/user-stories/01-author-the-simon-persona-unit.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Improve prose quality/clarity of `## Focus`, ensure Focus bullets are concrete and unambiguous (not generic "code quality" language) — validate differentiation intent ahead of Phase 2's automated Jaccard gate
   3. Validate all tests still pass at T2 scope (`go test ./internal/personas/... ./internal/registry/...`)
   4. COMMIT: `git commit -m "refactor(personas): address review + polish simon focus section"`
   **Duration:** ~1 hr

### 1.4 [ ] **Phase 1 - Definition of Done**
   1. `simon.yaml` + `simon.md` exist in `personas/community/`, structurally modeled on `sonny.yaml`/`sonny.md`
   2. `go test ./internal/personas/... ./internal/registry/...` green (isolated schema/template checks)
   3. Documented, expected red: `go test ./personas/...` (`TestCommunityAccessors`, `TestTemplateFixtureRunner_CommunityPersonasPass`) — tracked as open until Phase 2
   4. Adversarial review passed (or CRITICAL/HIGH fixed)
   5. Category word chosen and confirmed unclaimed against the 13 existing values

---

## Phase 2: Story 2 — Fixture Authoring & Test-Gate Integration (RGR)

**Duration:** 1 day | **Covers:** AC 02-01, AC 02-02, AC 02-03

> Author the synthetic slop fixture, then land the roster row and `index.json` entry in the same atomic change — partial registration is never a passable intermediate state (fails `require.Len` fatally for the whole `personas` package). This phase turns Phase 1's expected red state green: `TestCommunityAccessors`, `TestCommunityPersonas_FixtureAndPromptCategory`, `TestCommunityPersonas_DistinctCategories`, `TestCommunityPersonas_DistinctTaskScoping`, `TestCommunityIndex_Registration`, `TestTemplateFixtureRunner_CommunityPersonasPass` all pass with `simon` included.

### 2.1 [ ] **[Fixture Authoring & Test-Gate Integration - RED](plan/user-stories/02-fixture-authoring-test-gate-integration.md)**
   1. Confirm current red state carried over from Phase 1: `go test ./personas/...` fails on `TestCommunityAccessors` (`require.Len` 14 names vs 13 roster rows) and the embedded fixture-runner gate.
   2. Identify the exact roster/index insertion points: `communityPersonas` slice in `personas/community_test.go:117`, `personas/community/index.json` array.
   3. Cross-check the chosen `Category` word (from Phase 1) against the 13 claimed values once more, and pick a fresh `index.json` `tasks[0]` primary tag (e.g. `bloat-review`) distinct from the 13 claimed task tags.
   **Files:** `personas/community_test.go` (read), `personas/community/index.json` (read) | **Duration:** ~1 hr

### 2.2 [ ] **[Fixture Authoring & Test-Gate Integration - GREEN](plan/user-stories/02-fixture-authoring-test-gate-integration.md)**
   Author `personas/community/testdata/simon_fixture.patch` modeled on `personas/community/testdata/anthony_fixture.patch`'s structural pattern — a synthetic unified diff planting one unambiguous slop violation (e.g. a pointless single-implementation interface plus a tautological "apologetic" AI comment), sized to trigger `simon` without resembling legitimate business logic. In the same atomic change, add the `simon` row (`Slug: "simon"`, `VendorToken`, `Category`) to `communityPersonas` in `personas/community_test.go`, and a matching `PersonaIndexEntry` to `personas/community/index.json` (name/path/provider/model/description byte-matching `simon.yaml`; non-empty `tasks`/`tags`; `tasks[0]` the fresh unclaimed tag).
   Run `go test ./personas/... ./internal/personas/... ./internal/registry/...` (T2) — confirm all previously-red tests now pass. Manually run `atcr personas test simon` as the no-LLM structural smoke check.
   COMMIT: `git commit -m "feat(personas): register simon fixture, roster, and index entry (green)"`
   **Files:** `personas/community/testdata/simon_fixture.patch`, `personas/community_test.go`, `personas/community/index.json` | **Duration:** ~3 hrs

### 2.2.A [ ] **[Fixture Authoring & Test-Gate Integration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-fixture-authoring-test-gate-integration.md)**
   **Changed Files:** `personas/community/testdata/simon_fixture.patch`, `personas/community_test.go`, `personas/community/index.json`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `personas/community/testdata/simon_fixture.patch`, `personas/community_test.go`, `personas/community/index.json`
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (specifically: does `index.json`'s `path` field avoid `..`-escaping or absolute paths — path traversal per `verifyCommunityIndex`?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (specifically: roster/index field parity with `simon.yaml`; category word and task tag both genuinely unclaimed; fixture diff neither too subtle nor over-broad)
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.3 [ ] **[Fixture Authoring & Test-Gate Integration - REFACTOR](plan/user-stories/02-fixture-authoring-test-gate-integration.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve fixture clarity/naming consistency with existing `*_fixture.patch` files; tidy roster/index formatting
   3. Validate all tests still pass (T3): `go test ./personas/... ./internal/personas/... ./internal/registry/...`
   4. COMMIT: `git commit -m "refactor(personas): address review + polish simon fixture and registration"`
   **Duration:** ~1 hr

### 2.4 [ ] **Phase 2 - Definition of Done**
   1. `personas/community/testdata/simon_fixture.patch` exists and is a valid unified diff
   2. `simon` registered in `communityPersonas` roster and `personas/community/index.json`, field-parity confirmed
   3. `go test ./personas/... ./internal/personas/... ./internal/registry/...` fully green (14 personas)
   4. `atcr personas test simon` manual smoke check passes
   5. Adversarial review passed (or CRITICAL/HIGH fixed)

---

## Phase 3: Story 3 — Blog Post Outline Verification & Refresh

**Duration:** 0.5 day | **Covers:** AC 03-01, AC 03-02

> Read-only verification pass against the Phase 1-2 shipped artifacts, followed by a scoped corrective edit: replace the invalid `atcr review --persona simon` CTA with the verified `atcr personas install simon` / `atcr personas test simon` commands, and reconcile any category-word or persona-behavior drift in sections 1, 3, and 4. No new authorship; sections 2 and the already-accurate hook/pitch/example structure are left untouched. No automated Go test coverage applies (content-only file) — verification is manual + scripted grep, per `plan/test-planning-matrix.md`.

### 3.1 [ ] **[Verify and Refresh the Blog Post Outline - Verify & Edit](plan/user-stories/03-verify-and-refresh-the-blog-post-outline.md)**
   **Task:** Review `.planning/product/content/blog/slopfix-ai-code-bloat.md` against the final shipped `simon.yaml`/`simon.md` (Phase 1) and registered fixture/roster/index (Phase 2); correct any drift.
   **Priority:** Medium | **Effort:** S | **AC:** [03-01](plan/acceptance-criteria/03-01-cta-command-fix.md), [03-02](plan/acceptance-criteria/03-02-category-word-framing-alignment.md)
   1. `grep -n '\-\-persona simon\|review --persona' .planning/product/content/blog/slopfix-ai-code-bloat.md` — confirm the invalid CTA location (approx. line 38).
   2. Replace the invalid CTA (`atcr review --persona simon`) with the verified `atcr personas install simon` (production adoption) and `atcr personas test simon` (zero-setup, no-LLM demo), matching the documented pattern in `docs/personas-install.md` (`atcr personas install delia` / `atcr personas test delia`).
   3. Re-read sections 1, 3, and 4 for category-word or persona-behavior framing; reconcile any wording that drifted from `simon.md`'s shipped `## Focus` section — no wholesale rewrite, leave already-accurate sections (hook, cost narrative, before/after example) untouched.
   4. Verify: `grep -c '\-\-persona simon\|review --persona' .planning/product/content/blog/slopfix-ai-code-bloat.md` returns 0; `grep -c 'atcr personas install simon\|atcr personas test simon' ...` returns ≥1.
   5. COMMIT: `git commit -m "docs(content): fix invalid CTA and reconcile category word in slopfix outline"`
   **Success Criteria:** Zero `--persona simon` references; ≥1 `atcr personas install/test simon` reference; category word matches `simon.md` verbatim.
   **Files:** `.planning/product/content/blog/slopfix-ai-code-bloat.md` | **Duration:** ~2 hrs

### 3.1.A [ ] **[Verify and Refresh the Blog Post Outline - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-verify-and-refresh-the-blog-post-outline.md)**
   **Changed Files:** `.planning/product/content/blog/slopfix-ai-code-bloat.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `.planning/product/content/blog/slopfix-ai-code-bloat.md`, `personas/community/simon.yaml`, `personas/community/simon.md`
     - Checklist (pass verbatim, adapted for content review):
       - ACCURACY: Does every CLI command cited actually exist in `cmd/atcr/personas.go`? Does the category word match `simon.md`'s shipped `## Focus` section verbatim?
       - SCOPE: Is the diff confined to the CTA fix and confirmed word-level drift, with no unrelated rewrite of already-accurate sections?
       - EDGE CASES: Any residual `--persona` flag references anywhere else in the file?
       - CONSISTENCY: Does the outline's framing match `docs/personas-install.md`'s documented usage pattern?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.2, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.2 [ ] **[Verify and Refresh the Blog Post Outline - Finalize](plan/user-stories/03-verify-and-refresh-the-blog-post-outline.md)**
   1. Fix CRITICAL/HIGH issues from 3.1.A (if any)
   2. Re-run the grep verification commands from 3.1 step 4 to confirm they still pass after any fixes
   3. COMMIT: `git commit -m "docs(content): address review findings in slopfix outline"` (only if changes were made)
   **Duration:** ~30 min

### 3.3 [ ] **Phase 3 - Definition of Done**
   1. Zero `--persona simon` / `review --persona` references in the outline
   2. At least one `atcr personas install simon` or `atcr personas test simon` reference present
   3. Category-word framing matches `simon.md`'s shipped `## Focus` section
   4. Adversarial review passed (or CRITICAL/HIGH fixed)
   5. No unrelated sections rewritten

---

## Final Phase: Validation

**Duration:** 1 day | **Covers:** All 8 ACs (regression confirmation), Definition of Done for the sprint

> Run the complete 14-persona suite, manually smoke-check the CLI, re-verify the Jaccard differentiation ceiling, and confirm the blog outline's grep-verifiable claims.

### 4.1 [ ] **Full Test-Suite Regression**
   1. Run `go test ./personas/... ./internal/personas/... ./internal/registry/...` — confirm the complete 14-persona suite is green (all roster, index, fixture, differentiation, category, and task-scoping gates).
   2. Run `go vet ./...` and `golangci-lint run` — confirm clean.
   3. Run `go build ./...` — confirm the build succeeds.
   **Duration:** ~1 hr

### 4.2 [ ] **Cross-File Consistency & Differentiation Re-Check**
   1. Manually run `atcr personas test simon` — confirm the no-LLM structural smoke check passes against `simon_fixture.patch`.
   2. Re-verify `simon`'s `## Role`+`## Focus` Jaccard similarity stays under the 0.85 ceiling against all 13 existing personas (`TestCommunityPersonas_Differentiation`).
   3. Confirm `simon.yaml`/`simon.md`/`index.json`/roster field parity holds with no drift introduced during Phases 1-3.
   **Duration:** ~1 hr

### 4.3 [ ] **Blog Outline Claim Verification**
   1. `grep -c '\-\-persona' .planning/product/content/blog/slopfix-ai-code-bloat.md` → expect 0.
   2. `grep -c 'atcr personas install simon\|atcr personas test simon' .planning/product/content/blog/slopfix-ai-code-bloat.md` → expect ≥1.
   **Duration:** ~15 min

### Validation Checklist
- [ ] All tests passing (T3): `go test ./personas/... ./internal/personas/... ./internal/registry/...`
- [ ] Coverage meets threshold (≥80% project baseline, per `.planning/.config/config.yaml`)
- [ ] Lint/format clean (`golangci-lint run`, `go vet ./...`, `go fmt`/`goimports`)
- [ ] Build succeeds (`go build ./...`)

### Optional: Targeted Mutation Testing

Mutation testing tool: **UNAVAILABLE** in this environment (no `stryker-mutator` in package.json, no `mutmut`/`cargo-mutants` binary found — expected for a Go project without a Go mutation tool installed). Skip this step.

**WARNING:** Do NOT run full codebase mutation - it can take hours. Target specific files only, if a tool becomes available.

### Drift Analysis
(Compare against original-requirements.md)

- **AC1 (persona exists):** `simon.yaml`/`simon.md` in `personas/community/` — matches epic AC1 verbatim.
- **AC2 (prompt hyper-focused on AI bloat):** `simon.md`'s `## Focus` targets the four named anti-slop patterns — matches epic AC2.
- **AC3 (passing fixture test proves detection):** `simon_fixture.patch` + roster/index registration + full green suite — matches epic AC3.
- **AC4 (blog outline authored/positioned):** Outline pre-existed from Sprint 19.6; this sprint verifies/refreshes rather than authors fresh — a scope refinement captured in `plan/original-requirements.md`'s Refinements section, not a drift from intent.
- **No scope additions:** Roster/index registration (Story 2) was flagged during `/init-plan` codebase discovery as mandatory (not optional) to satisfy epic AC3 — documented as extended scope in `plan/plan.md`, not an uncontrolled addition.
