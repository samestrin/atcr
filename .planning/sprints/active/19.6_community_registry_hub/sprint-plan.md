# Sprint 19.6: Community Registry Hub

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.6 step-by-step. Complete each step, check off work immediately. This sprint runs in **GATED** mode — after each phase's DoD, a Phase-Boundary Gate task runs and `/execute-sprint` STOPS at the phase boundary for review before proceeding.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Make the in-repo community-persona channel (fetched from `samestrin/atcr`, not compiled into the binary) the **canonical** source of reviewer personas, add structured `provider`/`model` metadata so a user can discover a persona **by the model they already have** ("I have DeepSeek → find the DeepSeek persona → install it"), and ship a human-named, model-indexed persona library covering both frontier providers and strong flat-rate open models. Onboarding docs lead first-run with the monetizing Synthetic path (`atcr quickstart`) and keep frontier personas opt-in.

### Why This Matters

Models change too fast to bake into a binary release; the canonical persona source must be fetched, not compiled in. Today there is no way to discover a persona by model, no curated per-model-tuned library, and role-named stragglers (`sentinel`/`tracer`/`idiomatic`) break the all-human-names convention. This sprint closes all three gaps as real code + content features.

### Key Deliverables

- Repointed community-persona fetch URL (`samestrin/atcr`) with fetch-and-pin, `--offline` fallback, and backward compatibility for existing on-disk personas (Story 1 / AC1).
- Additive `PersonaIndexEntry` structured `provider`/`model`/`tasks`/`tags` schema + `index.json` generation (Story 2 / AC2, AC7).
- Model-aware `atcr personas search` with `--model`/`--provider` structured filtering, zero free-text fallback (Story 3 / AC2, AC6).
- A single deterministic `ResolvePersona` precedence chain resolving self-contained persona units (inline/co-located custom prompts), with untrusted-input guardrails — length cap + hard fixture gate (Story 1 / AC1, C1/C2/C3).
- A model-indexed, human-named persona library (frontier flagship+fallback pairs + flat-rate open models) with passing fixtures, plus the `sentinel→sasha` / `tracer→penny` / `idiomatic→ingrid` migration with no mixed-naming state (Stories 4 & 5 / AC3, AC4).
- Authoring-contract enforcement (fixture asserts bound-model metadata) and onboarding-hierarchy docs leading with Synthetic (Stories 6 & 7 / AC5, AC7, AC8).

### Success Criteria

- Default fetch URL points at `samestrin/atcr`; fetch-and-pin, offline stub, and backward compatibility verified against a mock registry (AC1, AC6).
- `atcr personas search` finds a persona by its bound model from structured `index.json` data, not free-text (AC2).
- A model-indexed, human-named persona library exists with passing fixtures; no role-based names remain anywhere in the active set (AC3, AC4).
- `go test ./...` passes with the authoring contract enforced by the fixture test (AC7).
- `README.md` and persona docs lead with the monetizing Synthetic path and position frontier personas as opt-in (AC5, AC8).

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Strict 🔒 (auto — complexity 11/12 VERY COMPLEX → strict). Every code element runs as separate **RED → GREEN → ADVERSARIAL → REFACTOR** tasks.

**Adversarial Review:** ENABLED 🎯 — after each GREEN, a **fresh subagent** (no memory of the implementation) reviews the changed files and returns a findings table. Inline-fix bar: **CRITICAL/HIGH** (fixed in the REFACTOR task). Deferred: **MEDIUM/LOW** (appended to `tech-debt-captured.md`).

**Execution Mode:** Gated 🚧 — a Phase-Boundary Gate (`N.LAST`) runs after each phase DoD; `/execute-sprint` stops at each phase boundary.

**Content phases (Phase 5) note:** persona authoring is judgment-heavy content. RED = author/lock the persona fixture (render + category assertion); GREEN = author the persona YAML + prompt to pass it; ADVERSARIAL = fresh-subagent review of schema/naming/prompt-grounding/fixture integrity; REFACTOR = tighten per findings. Manual per-persona verification (category word authored into the prompt, not leaked from the injected diff) is required alongside the automated pass.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request + locked Clarifications C1/C2/C3 (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements (7 stories) |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD (30 ACs) |
| [documentation/](plan/documentation/) | Grounded implementation references (fetch, CLI flags, YAML schema, testing, migration, onboarding) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/personas/ -run <TestName>` |
| T2: Module | After completing an element | `go test ./internal/personas/...` / `go test ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files in the same package as the code under test (Go convention). New: `internal/personas/search_test.go`, `internal/personas/resolve_test.go`. Extend: `client_test.go`, `test`-fixture file, `cmd/atcr/personas_test.go`, `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`. All network exercised via `httptest.NewServer` + `ATCR_PERSONAS_URL` override — **zero live network calls in CI**.

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `golangci-lint run` clean; `go vet ./...` clean
4. Format: `go fmt ./...` clean
5. Docs: Updated where the phase touches user-facing behavior

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

Grounded in `.planning/specifications/{implementation-standards,coding-standards,git-strategy}.md`.

- **Language:** Go (stdlib `net/http` / `encoding/json` + `spf13/cobra`); no new dependency warranted.
- **Errors:** wrap with `fmt.Errorf("...: %w", err)`; descriptive, non-zero-exit errors on fetch failure (no silent fallback unless `--offline`). `RunE` returns errors — never `os.Exit`.
- **YAML decode:** strict `Decoder.KnownFields(true)` on the persona-load path only; index-entry decode stays permissive so `Provider`/`Model`/`Tasks`/`Tags` remain additive/backward-compatible.
- **CLI output:** route through `cmd.OutOrStdout()`; follow the existing `--scores`-on-`newPersonasListCmd` flag-registration pattern.
- **Tests:** `testing` + `testify/assert`/`require`, table-driven `t.Run` subtests; colocated; 80% coverage baseline.
- **Security:** HTTPS-only raw-content URL; persona prompt length cap mirroring `MaxExecutorSystemPromptLen` (`internal/registry/config.go`); no code execution from fetched content at any point.

---

## External Resources

From [plan/documentation/README.md](plan/documentation/README.md):

- **[CRITICAL]** [Community Persona Fetch & Distribution](plan/documentation/fetch-and-distribution.md) — `RegistryBaseURL` repoint, fetch-and-pin, `--offline`, backward compat, `index.json` generation, C1/C2/C3 resolution model.
- **[CRITICAL]** [CLI Flag Wiring for Model-Aware Search](plan/documentation/cli-search-flags.md) — Cobra `--model`/`--provider` registration.
- **[IMPORTANT]** [Persona YAML Schema & Struct Tags](plan/documentation/persona-yaml-schema.md) — `yaml.v3` tags, strict-vs-permissive decode split.
- **[IMPORTANT]** [Testing Patterns: testify + httptest Mock Registry](plan/documentation/testing-mock-registry.md) — `httptest.NewServer` + `ATCR_PERSONAS_URL` pattern.
- **[IMPORTANT]** [Human-Names Migration for Built-in Stragglers](plan/documentation/human-names-migration.md) — atomic four-part rename checklist.
- **[IMPORTANT]** [Onboarding Hierarchy and Discover-by-Model Flow](plan/documentation/onboarding-hierarchy.md) — locked 5-tier order + discover-install-verify flow.

Package specs: `.planning/specifications/packages/{yaml-v3,standard-library,cobra,testify}.md`.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike — Resolution Chain Design (1 day)

> Design spike only — **no shipped code, no tests**. Output: a short design note (`plan/design-notes/resolution-chain.md`) that Phase 3 implements against. Precedence order is pre-locked in sprint-design: project `.atcr/personas` override > pinned community (`~/.config/atcr/personas`) > embedded built-in. Built-in `.md` reformatting is deferred to a bounded fast-follow (built-ins resolve through the same chain via a thin adapter, not a physical file rewrite).

### 1.1 [ ] **🔬 Spike: Map review-time persona-to-prompt resolution & lock the interface**
   **Task:** Locate the current review-time call site that turns `AgentConfig.Persona` into prompt text (the codebase-discovery snapshot covers install/search/list/upgrade but NOT this path). Confirm the concrete `ResolvePersona` signature, decide the self-contained unit's on-disk shape (inline YAML field vs. co-located file installed atomically), confirm the exact precedence ordering, and confirm the length-cap constant to mirror (`MaxExecutorSystemPromptLen`, `internal/registry/config.go`).
   **Priority:** Critical | **Effort:** 1 day
   1. Grep/trace `AgentConfig.Persona` → prompt-text usage at review time; document the call site (file:line).
   2. Draft the `ResolvePersona` function signature (inputs, winning-source result shape, error cases).
   3. Decide unit on-disk shape (inline vs. co-located) and how built-ins adapt into the same chain.
   4. Record precedence order + collision rule + length-cap constant value.
   **Success Criteria:** `plan/design-notes/resolution-chain.md` exists with: resolution call site (file:line), `ResolvePersona` signature, unit shape decision, precedence + collision rule, length-cap value. No production code changed.
   **Files:** `plan/design-notes/resolution-chain.md` (new) | **Duration:** ~1 day

### 1.2 [ ] **Phase 1 DoD**
   1. Design note complete and internally consistent with C1/C2/C3.
   2. No code/test changes (spike only) — `git status` shows only the design note.
   3. COMMIT: `git add .planning/sprints/active/19.6_community_registry_hub/plan/design-notes/resolution-chain.md && git commit -m "docs(personas): resolution-chain design spike (phase 1)"`

### 1.LAST [ ] **Phase 1 - GATE: Design Note Exit Review (subagent)**
   **Scope:** `plan/design-notes/resolution-chain.md`

   **Spawn a fresh subagent** via the Agent tool to review the design note. No memory of the spike — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - File to review (absolute path): `.../plan/design-notes/resolution-chain.md`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the `ResolvePersona` signature concrete enough for Phase 3 to implement without re-deciding?
       - CONFIG SURFACE: Is the length-cap constant and precedence order unambiguous?
       - INTEGRATION: Does the unit-shape decision honor C2 (one unit, one resolution chain, no second delivery path)?
       - PHASE-EXIT CONTRACT: Can Phase 3 consume this note without rework?
       - REGRESSION: Does the built-in adapter approach avoid a divergent second format?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Foundation — Schema Extension + Registry Repoint (1.5 days)

> Land the additive `PersonaIndexEntry` schema extension and the one-constant URL repoint — the two changes every other phase depends on. Test types: Unit (struct tags, index population, old-shape backward-compat decode).

### 2.1 [ ] **[PersonaIndexEntry schema extension - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-01](plan/acceptance-criteria/02-01-persona-index-entry-schema-extension.md)
   Write comprehensive failing tests for the extended struct: `Provider`/`Model`/`Tasks`/`Tags` with `omitempty` tags decode from a full-shape `index.json`; verify fail correctly.
   **Files:** `internal/personas/search_test.go` (new) | **Duration:** ~1h

### 2.2 [ ] **[PersonaIndexEntry schema extension - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Add `Provider`/`Model`/`Tasks`/`Tags` fields (all `omitempty`) to `PersonaIndexEntry`. Minimal code, one test at a time (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): extend PersonaIndexEntry with provider/model/tasks/tags (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m

### 2.2.A [ ] **[PersonaIndexEntry schema extension - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Changed Files:** `internal/personas/search.go`, `internal/personas/search_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation — intentional. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including the changed files (absolute paths), the checklist verbatim (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric CRITICAL/HIGH/MEDIUM/LOW, and: "Required output: ONLY the findings table, no prose." Focus: are the new fields truly additive (no strict-decode breakage on the index path)?

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.3 [ ] **[PersonaIndexEntry schema extension - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): address review + clean up schema"`
   **Duration:** ~30m

### 2.4 [ ] **[index.json field population contract - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-02](plan/acceptance-criteria/02-02-index-json-field-population-contract.md)
   Write failing tests asserting the index generation populates `provider`/`model` (and `tasks`/`tags` where present) from persona YAML sources into `index.json` entries; verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~1h

### 2.5 [ ] **[index.json field population contract - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Implement the generation/population so structured fields flow from YAML → index entry. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): populate structured fields in index.json generation (green)"`
   **Files:** `internal/personas/search.go` (+ generation path) | **Duration:** ~1h

### 2.5.A [ ] **[index.json field population contract - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Changed Files:** generation path + `search.go` + tests.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Adversarial review: 2.5`) with changed-file absolute paths, the verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric, "ONLY the findings table" instruction. Focus: index/YAML source drift (mismatched provider/model claims).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List for 2.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.6 [ ] **[index.json field population contract - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   Fix CRITICAL/HIGH from 2.5.A; improve quality, maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): tighten index generation"`
   **Duration:** ~30m

### 2.7 [ ] **[Backward-compatible decode - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-03](plan/acceptance-criteria/02-03-backward-compatible-decode-test.md)
   Write a failing test asserting an **old-shape** `index.json` (no new fields) decodes cleanly against the extended struct with zero-value new fields, no decode error; verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~45m

### 2.8 [ ] **[Backward-compatible decode - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Ensure the index-entry decode path stays permissive (`encoding/json` unknown-field tolerance / no `KnownFields(true)` on this path). Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): guarantee old-shape index.json decodes (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m

### 2.8.A [ ] **[Backward-compatible decode - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 2.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: any strict-decode leak that could reject old payloads.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 2.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 2.9 [ ] **[Backward-compatible decode - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   Fix CRITICAL/HIGH from 2.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): back-compat decode cleanup"`
   **Duration:** ~20m

### 2.10 [ ] **[RegistryBaseURL repoint - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-01](plan/acceptance-criteria/01-01-registry-base-url-repoint.md)
   Write failing tests: default `BaseURL()` resolves to the `samestrin/atcr` in-repo community path; `ATCR_PERSONAS_URL` override still wins; HTTPS-only. Verify fail correctly.
   **Files:** `internal/personas/client_test.go` (extend) | **Duration:** ~45m

### 2.11 [ ] **[RegistryBaseURL repoint - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Repoint the single `RegistryBaseURL` constant (`internal/personas/client.go`) to `samestrin/atcr` + in-repo community path; leave `BaseURL()`'s env-override-else-constant logic untouched. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): repoint RegistryBaseURL to samestrin/atcr (green)"`
   **Files:** `internal/personas/client.go` | **Duration:** ~20m

### 2.11.A [ ] **[RegistryBaseURL repoint - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 2.11`) — changed files (`client.go`, `client_test.go`), verbatim checklist, severity rubric, findings-table-only. Focus: MITM/HTTP-vs-HTTPS, and any subcommand path that bypasses `BaseURL()`.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 2.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 2.12 [ ] **[RegistryBaseURL repoint - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 2.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): base URL repoint cleanup"`
   **Duration:** ~20m

### 2.13 [ ] **Phase 2 DoD**
   1. Tests (T3): `go test ./internal/personas/...` all passing
   2. Coverage ≥80% on touched files; Lint/vet/fmt clean
   3. Backward-compat decode test proves old `index.json` still parses
   4. DoD report (Stories 1-partial, 2)
   5. COMMIT any residual: `git commit -m "test(personas): phase 2 DoD"`

### 2.LAST [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (`internal/personas/search.go`, `client.go`, tests).

   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 2 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (struct shape stable for Phases 3/4/5?), CONFIG SURFACE (new fields documented/defaulted/back-compat?), INTEGRATION (URL repoint doesn't break install/search/list/upgrade callers?), PHASE-EXIT CONTRACT (downstream consumes schema without rework?), REGRESSION (existing persona tests intact?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Core Resolution — Fetch-and-Pin + ResolvePersona Chain (3.5 days)

> The heaviest code phase — implement fetch-and-pin in `init`/`quickstart`, the `--offline` fallback, and the new single-precedence-chain resolver with untrusted-input guardrails (length cap, hard fixture gate, pin-for-reproducibility). Implement against Phase 1's design note. Test types: Integration (mock-registry) + Unit (precedence ordering, length-cap rejection) + E2E (existing-workspace preservation, source labeling).

### 3.1 [ ] **[init/quickstart fetch-and-pin - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-02](plan/acceptance-criteria/01-02-init-quickstart-fetch-and-pin.md)
   Write failing integration tests (mock `httptest.NewServer` + `ATCR_PERSONAS_URL`): `init`/`quickstart` fetch personas and **pin a version** reproducibly; `atcr personas upgrade` advances the pin. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`, `internal/personas/client_test.go` | **Duration:** ~3h

### 3.2 [ ] **[init/quickstart fetch-and-pin - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement fetch-and-pin version tracking in `internal/personas` and wire `init`/`quickstart` to obtain personas by fetch-and-pin. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): fetch-and-pin for init/quickstart (green)"`
   **Files:** `internal/personas/client.go`, `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** ~4h

### 3.2.A [ ] **[init/quickstart fetch-and-pin - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.2`) — changed-file absolute paths, verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric, findings-table-only. Focus: pin reproducibility, transport timeout vs. context deadline, retry/backoff reuse.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 3.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 3.3 [ ] **[init/quickstart fetch-and-pin - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fetch-and-pin cleanup"`
   **Duration:** ~1h

### 3.4 [ ] **[--offline flag fallback - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-03](plan/acceptance-criteria/01-03-offline-flag-fallback.md)
   Write failing tests: `--offline` skips the community fetch entirely (zero network calls) and falls back to embedded built-ins. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go` / `quickstart_test.go` | **Duration:** ~1.5h

### 3.5 [ ] **[--offline flag fallback - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement the `--offline` stub path. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): --offline embedded-builtin fallback (green)"`
   **Files:** `cmd/atcr/init.go` / `quickstart.go`, `internal/personas/client.go` | **Duration:** ~2h

### 3.5.A [ ] **[--offline flag fallback - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.5`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: does `--offline` truly make zero network calls; graceful degradation completeness.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 3.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 3.6 [ ] **[--offline flag fallback - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.5.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): offline fallback cleanup"`
   **Duration:** ~45m

### 3.7 [ ] **[Fetch-failure error handling - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-04](plan/acceptance-criteria/01-04-fetch-failure-error-handling.md)
   Write failing tests: fetch failure (without `--offline`) returns a descriptive, non-zero-exit error wrapped with `%w`; no silent fallback. Verify fail correctly.
   **Files:** `internal/personas/client_test.go` | **Duration:** ~1.5h

### 3.8 [ ] **[Fetch-failure error handling - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement descriptive error path (reuse existing retry/backoff for transient 429/5xx). Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): descriptive fetch-failure errors (green)"`
   **Files:** `internal/personas/client.go` | **Duration:** ~1.5h

### 3.8.A [ ] **[Fetch-failure error handling - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: swallowed errors, silent fallback, exit-code correctness.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 3.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 3.9 [ ] **[Fetch-failure error handling - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fetch-error cleanup"`
   **Duration:** ~45m

### 3.10 [ ] **[Preserve existing personas + source labeling - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-05](plan/acceptance-criteria/01-05-preserve-existing-personas-and-source-labeling.md)
   Write failing E2E test: rerun `init --force` against a workspace with a hand-edited `.atcr/personas/*.md` — the file is **byte-for-byte unchanged**; missing community personas install alongside it; each persona's source is labeled. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go` | **Duration:** ~2h

### 3.11 [ ] **[Preserve existing personas + source labeling - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement workspace preservation + source labeling. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): preserve on-disk personas + source labels (green)"`
   **Files:** `cmd/atcr/init.go`, `internal/personas/*` | **Duration:** ~2h

### 3.11.A [ ] **[Preserve existing personas + source labeling - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.11`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: data-loss (overwriting a user's edited file), idempotence of `--force`.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 3.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 3.12 [ ] **[Preserve existing personas + source labeling - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): preservation/labeling cleanup"`
   **Duration:** ~45m

### 3.13 [ ] **[Custom-prompt ResolvePersona precedence chain - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-06](plan/acceptance-criteria/01-06-custom-prompt-resolution-precedence.md)
   Write comprehensive failing unit tests for `ResolvePersona`: single deterministic precedence (project `.atcr/personas` override > pinned community > embedded built-in); collision resolves to exactly one source; **length cap** rejects oversized custom prompts; **hard fixture gate** blocks a fixture-failing prompt from resolving; a fetched custom prompt (inline/co-located) resolves as one self-contained unit (C1/C2/C3). Verify fail correctly.
   **Files:** `internal/personas/resolve_test.go` (new) | **Duration:** ~3h

### 3.14 [ ] **[Custom-prompt ResolvePersona precedence chain - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement `ResolvePersona` per the design note: one chain, one unit, guardrails (length cap mirroring `MaxExecutorSystemPromptLen`, hard fixture gate, pin). Built-ins resolve through the same chain via a thin adapter. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): single ResolvePersona precedence chain + guardrails (green)"`
   **Files:** `internal/personas/resolve.go` (new), `internal/personas/*` | **Duration:** ~5h

### 3.14.A [ ] **[Custom-prompt ResolvePersona precedence chain - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.14`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus (HIGH-RISK, security-sensitive): prompt-injection via fetched prompt, oversized-prompt DoS, leftover `{{ }}` template injection, ambiguous collision / double-load / panic, fixture-gate bypass.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 3.15, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 3.15 [ ] **[Custom-prompt ResolvePersona precedence chain - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.14.A (security findings are non-negotiable inline fixes); maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): harden ResolvePersona + guardrails"`
   **Duration:** ~1.5h

### 3.16 [ ] **Phase 3 DoD**
   1. Tests (T3): `go test ./...` all passing (Story 1 complete)
   2. Coverage ≥80%; Lint/vet/fmt clean
   3. Security guardrails (length cap, fixture gate) proven by tests
   4. DoD report (Story 1)
   5. COMMIT residual: `git commit -m "test(personas): phase 3 DoD"`

### 3.LAST [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`client.go`, `resolve.go`, `init.go`, `quickstart.go`, tests).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 3 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (`ResolvePersona` signature matches design note & consumable by review-time callers?), CONFIG SURFACE (`--offline`, pin file documented/back-compat?), INTEGRATION (fetch-and-pin doesn't regress install/upgrade?), PHASE-EXIT CONTRACT (Phase 5 personas can be delivered via this chain?), REGRESSION (Phase 2 schema still intact?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Discovery — Model-Aware Search (1.5 days)

> Structured `--model`/`--provider` filtering with **zero free-text fallback**, backward-compatible keyword search, flag/arg validation. Test types: Integration (flag registration, table rendering) + Unit (structured-field-only matching, near-miss substring cases).

### 4.1 [ ] **[Structured model/provider filtering - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-structured-model-provider-filtering.md)
   Write failing tests: `Search` matches structured `Provider`/`Model` fields only; a persona whose `Description` mentions a model but whose structured `Model` differs is **NOT** returned under `--model` (no free-text fallback). Verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~1h

### 4.2 [ ] **[Structured model/provider filtering - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Extend `Search()` to filter on structured fields. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): structured model/provider filtering (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~1h

### 4.2.A [ ] **[Structured model/provider filtering - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.2`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: any free-text leak into `--model` matching; case/normalization edge cases.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 4.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 4.3 [ ] **[Structured model/provider filtering - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): filtering cleanup"`
   **Duration:** ~30m

### 4.4 [ ] **[Keyword search backward-compatibility - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-02](plan/acceptance-criteria/03-02-keyword-search-backward-compatibility.md)
   Write failing tests: bare `atcr personas search <term>` still matches `Name`/`Description` substrings exactly as before (no regression). Verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~45m

### 4.5 [ ] **[Keyword search backward-compatibility - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Preserve keyword path alongside structured filters. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): preserve keyword search path (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m

### 4.5.A [ ] **[Keyword search backward-compatibility - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.5`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: interaction of keyword + structured flags (AND/OR semantics).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 4.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 4.6 [ ] **[Keyword search backward-compatibility - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.5.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): search path cleanup"`
   **Duration:** ~20m

### 4.7 [ ] **[Flag registration & arg validation - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-03](plan/acceptance-criteria/03-03-flag-registration-and-arg-validation.md)
   Write failing tests: `--model`/`--provider` registered on `newPersonasSearchCmd` following the `--scores` pattern; invalid arg combos return a `RunE` error (no `os.Exit`). Verify fail correctly.
   **Files:** `cmd/atcr/personas_test.go` | **Duration:** ~1h

### 4.8 [ ] **[Flag registration & arg validation - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Register flags + validation on the search command. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(cli): --model/--provider flags on personas search (green)"`
   **Files:** `cmd/atcr/personas.go` | **Duration:** ~1h

### 4.8.A [ ] **[Flag registration & arg validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: error paths use `RunE` returns not `os.Exit`; output via `cmd.OutOrStdout()`.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 4.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 4.9 [ ] **[Flag registration & arg validation - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(cli): search flag cleanup"`
   **Duration:** ~30m

### 4.10 [ ] **[Search table provider/model columns - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-04](plan/acceptance-criteria/03-04-search-table-provider-model-columns.md)
   Write failing tests for `renderPersonaSearch` output including `provider`/`model` columns. Verify fail correctly.
   **Files:** `cmd/atcr/personas_test.go` | **Duration:** ~45m

### 4.11 [ ] **[Search table provider/model columns - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Render provider/model columns. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(cli): render provider/model columns in search (green)"`
   **Files:** `cmd/atcr/personas.go` | **Duration:** ~45m

### 4.11.A [ ] **[Search table provider/model columns - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.11`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: empty-field rendering, column alignment with `omitempty` values.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 4.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 4.12 [ ] **[Search table provider/model columns - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(cli): search rendering cleanup"`
   **Duration:** ~20m

### 4.13 [ ] **Phase 4 DoD**
   1. Tests (T3): `go test ./...` all passing (Story 3 complete)
   2. Coverage ≥80%; Lint/vet/fmt clean
   3. Structured-only matching proven (no free-text fallback)
   4. DoD report (Story 3)
   5. COMMIT residual: `git commit -m "test(personas): phase 4 DoD"`

### 4.LAST [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (`search.go`, `cmd/atcr/personas.go`, tests).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 4 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT, CONFIG SURFACE (flags documented for Phase 7 docs?), INTEGRATION (search consumes Phase 2 schema correctly?), PHASE-EXIT CONTRACT (Story 7 docs can cite real flag names?), REGRESSION (keyword search intact?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Content Authoring — Persona Library + Human-Names Migration (5 days)

> Isolated from schema/network code per the plan's risk mitigation — content review cadence (genuine vendor-guidance research) must not block code merge cadence. Runs after Phase 2 (schema) and delivers via Phase 3's resolution chain. Test types: Unit (schema validation, fixture pass, naming compliance) + Integration (retired-slug repo-wide verification scoped to persona paths).
>
> **Per-persona TDD:** RED = author/lock the persona fixture; GREEN = author YAML + prompt to pass it; ADVERSARIAL = fresh-subagent review of schema/naming/vendor-grounding/fixture integrity (verify the category word is authored into the prompt, not leaked from the injected diff); REFACTOR = tighten. Follow `docs/personas-authoring.md`'s contribution checklist.

### 5.1 [ ] **[Frontier flagship+fallback persona pairs - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-01](plan/acceptance-criteria/04-01-frontier-flagship-fallback-persona-pairs.md)
   Author/lock fixtures for the 3 frontier pairs (Anthropic/OpenAI/Google, each flagship primary + same-family fallback). Verify fixtures fail (personas not yet authored).
   **Files:** `personas/testdata/*_fixture.patch` | **Duration:** ~3h

### 5.2 [ ] **[Frontier flagship+fallback persona pairs - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Author each persona (YAML binding `provider`+`model` flagship+fallback + Markdown prompt phrased per that provider's official guide), human-named. Fixtures pass (T1/T2). COMMIT: `git commit -m "content(personas): frontier flagship+fallback library (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~6h

### 5.2.A [ ] **[Frontier flagship+fallback persona pairs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.2`) — persona file paths, checklist adapted (SCHEMA: required `provider`/`model` present & structured? NAMING: human first name, no role name? GROUNDING: prompt reflects the provider's official guide, not a generic template? FIXTURE INTEGRITY: category word authored in the prompt itself, not only from the injected diff?), severity rubric, findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.3 [ ] **[Frontier flagship+fallback persona pairs - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.2.A; re-run fixtures (T3); COMMIT: `git commit -m "content(personas): refine frontier personas"`
   **Duration:** ~1h

### 5.4 [ ] **[Flat-rate open-model personas - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-02](plan/acceptance-criteria/04-02-flat-rate-open-model-personas.md)
   Author/lock fixtures for the flat-rate open-model personas (DeepSeek/Qwen/Kimi/GLM). Verify fail.
   **Files:** `personas/testdata/*_fixture.patch` | **Duration:** ~3h

### 5.5 [ ] **[Flat-rate open-model personas - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Author each open-model persona (YAML + vendor-grounded prompt), human-named. Fixtures pass (T1/T2). COMMIT: `git commit -m "content(personas): flat-rate open-model library (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~6h

### 5.5.A [ ] **[Flat-rate open-model personas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.5`) — same content-review checklist as 5.2.A. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.6 [ ] **[Flat-rate open-model personas - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.5.A; re-run fixtures (T3); COMMIT: `git commit -m "content(personas): refine open-model personas"`
   **Duration:** ~1h

### 5.7 [ ] **[Vendor-grounded prompt structure compliance - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-03](plan/acceptance-criteria/04-03-vendor-grounded-prompt-structure-compliance.md)
   Add tests/checks asserting each prompt renders all required template variables (`{{.AgentName}}`, `{{.ScopeRule}}`, etc.) with no leftovers, and follows the per-vendor structure. Verify fail.
   **Files:** `personas/*_test.go` / fixtures | **Duration:** ~1.5h

### 5.8 [ ] **[Vendor-grounded prompt structure compliance - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Bring all prompts into structural compliance. Fixtures/tests pass (T2). COMMIT: `git commit -m "content(personas): vendor-grounded prompt compliance (green)"`
   **Files:** `personas/*.md` | **Duration:** ~2h

### 5.8.A [ ] **[Vendor-grounded prompt structure compliance - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.8`) — content-review checklist (esp. leftover `{{ }}` template injection). Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.9 [ ] **[Vendor-grounded prompt structure compliance - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.8.A; re-run (T3); COMMIT: `git commit -m "content(personas): prompt structure cleanup"`
   **Duration:** ~45m

### 5.10 [ ] **[Fixture authoring & fixture-test pass - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-04](plan/acceptance-criteria/04-04-fixture-authoring-and-fixture-test-pass.md)
   Ensure every library persona has a `<slug>_fixture.patch` in `personas/testdata/`; run the fixture test and confirm the currently-missing ones fail. Verify fail.
   **Files:** `personas/testdata/*` | **Duration:** ~1.5h

### 5.11 [ ] **[Fixture authoring & fixture-test pass - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Complete all fixtures; full fixture test passes (T2). COMMIT: `git commit -m "content(personas): complete fixtures (green)"`
   **Files:** `personas/testdata/*` | **Duration:** ~2h

### 5.11.A [ ] **[Fixture authoring & fixture-test pass - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.11`) — checklist focus: does any fixture pass only because the category word leaks from the injected diff rather than the prompt? Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.12 [ ] **[Fixture authoring & fixture-test pass - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.11.A; re-run (T3); COMMIT: `git commit -m "content(personas): fixture integrity cleanup"`
   **Duration:** ~45m

### 5.13 [ ] **[Community index registration - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-05](plan/acceptance-criteria/04-05-community-index-registration.md)
   Write failing test: every authored persona appears in the in-repo community `index.json`, discoverable by model. Verify fail.
   **Files:** `personas/community/index.json`, `internal/personas/search_test.go` | **Duration:** ~1h

### 5.14 [ ] **[Community index registration - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Generate/populate the community `index.json` from YAML sources so every persona is registered. Test passes (T2). COMMIT: `git commit -m "content(personas): register library in community index.json (green)"`
   **Files:** `personas/community/index.json` | **Duration:** ~1h

### 5.14.A [ ] **[Community index registration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.14`) — focus: index/YAML source drift (does every entry's `provider`/`model` match its persona YAML?). Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.15, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.15 [ ] **[Community index registration - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.14.A; re-run (T3); COMMIT: `git commit -m "content(personas): index registration cleanup"`
   **Duration:** ~30m

### 5.16 [ ] **[Strict schema & naming compliance - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-06](plan/acceptance-criteria/04-06-strict-schema-and-naming-compliance.md)
   Write failing tests: every persona decodes under strict `KnownFields(true)`; all names are human first names (no role names). Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1h

### 5.17 [ ] **[Strict schema & naming compliance - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Bring all personas into strict-schema + human-name compliance. Tests pass (T2). COMMIT: `git commit -m "content(personas): strict schema + human-name compliance (green)"`
   **Files:** `personas/*.yaml` | **Duration:** ~1h

### 5.17.A [ ] **[Strict schema & naming compliance - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.17`) — focus: any unknown YAML field, any residual role-name. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.18, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.18 [ ] **[Strict schema & naming compliance - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.17.A; re-run (T3); COMMIT: `git commit -m "content(personas): schema/naming cleanup"`
   **Duration:** ~30m

### 5.19 [ ] **[Model-appropriate task-scoping differentiation - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-07](plan/acceptance-criteria/04-07-model-appropriate-task-scoping-differentiation.md)
   Write failing tests/checks: each persona's `tasks`/scope reflects its model's strength and personas are meaningfully differentiated (not templated clones). Verify fail.
   **Files:** `personas/*_test.go` / metadata checks | **Duration:** ~1h

### 5.20 [ ] **[Model-appropriate task-scoping differentiation - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Tune each persona's task-scoping. Tests pass (T2). COMMIT: `git commit -m "content(personas): model-appropriate task scoping (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~2h

### 5.20.A [ ] **[Model-appropriate task-scoping differentiation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.20`) — focus: are personas genuinely differentiated per model strength, or near-duplicate content? Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.21, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.21 [ ] **[Model-appropriate task-scoping differentiation - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.20.A; re-run (T3); COMMIT: `git commit -m "content(personas): task-scoping cleanup"`
   **Duration:** ~30m

### 5.22 [ ] **[Atomic rename sentinel/tracer/idiomatic - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-atomic-rename-sentinel-tracer-idiomatic.md)
   Write failing tests: `sentinel→sasha`, `tracer→penny`, `idiomatic→ingrid` renamed atomically across all four parts (template, fixture, YAML, registration in `personas/personas.go`'s `names` slice); no mixed-naming state; init-time panic guard passes. Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1.5h

### 5.23 [ ] **[Atomic rename sentinel/tracer/idiomatic - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Perform the four-part atomic rename for all three stragglers. Tests pass (T2). COMMIT: `git commit -m "content(personas): rename sentinel/tracer/idiomatic to human names (green)"`
   **Files:** `personas/*.md`, `personas/*.yaml`, `personas/testdata/*`, `personas/personas.go` | **Duration:** ~2h

### 5.23.A [ ] **[Atomic rename sentinel/tracer/idiomatic - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.23`) — checklist verbatim + focus: any partial rename (template renamed but `names` slice stale → startup panic), any lingering old slug. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.24, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.24 [ ] **[Atomic rename sentinel/tracer/idiomatic - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.23.A; re-run (T3); COMMIT: `git commit -m "content(personas): rename cleanup"`
   **Duration:** ~45m

### 5.25 [ ] **[ingrid generalized idiomatic lens - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-02](plan/acceptance-criteria/05-02-ingrid-generalized-idiomatic-lens.md)
   Write failing tests/fixture: `ingrid` is generalized beyond Go (language-agnostic idiomatic lens). Verify fail.
   **Files:** `personas/testdata/ingrid_fixture.patch`, tests | **Duration:** ~1h

### 5.26 [ ] **[ingrid generalized idiomatic lens - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Rewrite `ingrid`'s prompt to be language-agnostic. Fixture passes (T2). COMMIT: `git commit -m "content(personas): generalize ingrid beyond Go (green)"`
   **Files:** `personas/ingrid.md` | **Duration:** ~1.5h

### 5.26.A [ ] **[ingrid generalized idiomatic lens - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.26`) — focus: any residual Go-specific assumption; fixture integrity. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.27, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.27 [ ] **[ingrid generalized idiomatic lens - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.26.A; re-run (T3); COMMIT: `git commit -m "content(personas): ingrid generalization cleanup"`
   **Duration:** ~30m

### 5.28 [ ] **[Retired-slug verification - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-03](plan/acceptance-criteria/05-03-retired-slug-verification.md)
   Write a failing repo-wide (scoped to persona paths) verification test asserting no `sentinel`/`tracer`/`idiomatic` slug remains anywhere in the active set. Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1h

### 5.29 [ ] **[Retired-slug verification - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Eliminate any remaining retired slug. Test passes (T2). COMMIT: `git commit -m "content(personas): retired-slug verification (green)"`
   **Files:** persona paths | **Duration:** ~45m

### 5.29.A [ ] **[Retired-slug verification - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.29`) — focus: is the scope of the verification wide enough (fixtures, index, registration, docs)? Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.30, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.30 [ ] **[Retired-slug verification - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.29.A; re-run (T3); COMMIT: `git commit -m "content(personas): slug verification cleanup"`
   **Duration:** ~20m

### 5.31 [ ] **[Migration documentation updates - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-04](plan/acceptance-criteria/05-04-documentation-updates.md)
   Identify every doc reference to the old slugs (`docs/`, README) that must change to the new names; capture as a checklist / failing doc-lint. Verify the gaps exist.
   **Files:** `docs/*`, `README.md` (audit) | **Duration:** ~45m

### 5.32 [ ] **[Migration documentation updates - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Update all doc references to the migrated names. Verify checklist clear (T2 where testable). COMMIT: `git commit -m "docs(personas): update references for straggler rename (green)"`
   **Files:** `docs/*`, `README.md` | **Duration:** ~1h

### 5.32.A [ ] **[Migration documentation updates - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.32`) — focus: any missed doc reference to a retired slug; consistency of the new names across docs. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 5.33, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 5.33 [ ] **[Migration documentation updates - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.32.A; final read-through; COMMIT: `git commit -m "docs(personas): migration doc cleanup"`
   **Duration:** ~30m

### 5.34 [ ] **Phase 5 DoD**
   1. Tests (T3): `go test ./...` all passing (Stories 4 & 5 complete); all fixtures pass
   2. No role-based names remain anywhere in the active set; strict schema holds
   3. Coverage ≥80%; Lint/vet/fmt clean
   4. Manual per-persona verification complete (category word authored into prompt)
   5. DoD report (Stories 4, 5)
   6. COMMIT residual: `git commit -m "content(personas): phase 5 DoD"`

### 5.LAST [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All persona content + `personas/personas.go` + fixtures + index changed during Phase 5.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 5 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (every persona resolvable via Phase 3 `ResolvePersona`?), CONFIG SURFACE (index entries carry structured metadata?), INTEGRATION (no mixed-naming state; built-in panic guard passes at startup?), PHASE-EXIT CONTRACT (Story 6 can assert bound-model metadata against real personas; Story 7 can cite real names?), REGRESSION (existing built-in fixtures still pass?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 6: Contract Enforcement + Onboarding Docs (1.5 days)

> Close the two remaining documentation/enforcement gaps and rewrite onboarding docs — sequenced after Phases 4 and 5 so cited flags/persona names are accurate. Test types: Unit (fixture test asserts bound-model metadata) + Manual (doc-content review against `plan/documentation/onboarding-hierarchy.md`'s locked tier language).

### 6.1 [ ] **[Fixture test asserts bound-model metadata - RED](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-03](plan/acceptance-criteria/06-03-fixture-test-asserts-bound-model-metadata.md)
   Write a failing test extending the fixture runner to assert every community persona's bound `provider`/`model` appears in structured metadata (additive path; keep the `isBuiltin(name)` branch separate). Verify fail correctly.
   **Files:** `internal/personas/` fixture test file | **Duration:** ~1.5h

### 6.2 [ ] **[Fixture test asserts bound-model metadata - GREEN](plan/user-stories/06-authoring-contract-enforcement.md)**
   Implement the additive assertion. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "test(personas): fixture asserts bound-model metadata (green)"`
   **Files:** `internal/personas/test.go` (+ fixture runner) | **Duration:** ~1.5h

### 6.2.A [ ] **[Fixture test asserts bound-model metadata - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.2`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: does the new assertion weaken/alter the existing built-in fixture pass/fail contract?

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> 6.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.3 [ ] **[Fixture test asserts bound-model metadata - REFACTOR](plan/user-stories/06-authoring-contract-enforcement.md)**
   Fix CRITICAL/HIGH from 6.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fixture assertion cleanup"`
   **Duration:** ~30m

### 6.4 [ ] **[Document model-in-structured-metadata convention](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-model-in-structured-metadata-convention.md)
   Update `docs/personas-authoring.md` to document the model-in-structured-metadata convention as a forward-looking authoring rule (asserted by the fixture test from 6.1-6.3).
   1. Author the doc section.
   2. Verify it matches the enforced behavior.
   3. COMMIT: `git commit -m "docs(personas): document model-in-metadata convention"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~45m

### 6.4.A [ ] **[Convention docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.4`) — review `docs/personas-authoring.md` changes for accuracy vs. the enforced fixture behavior and completeness. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.5 [ ] **[Document all-human-names convention](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-02](plan/acceptance-criteria/06-02-all-human-names-convention-documented.md)
   Document the all-human-names convention in `docs/personas-authoring.md` as a forward-looking rule (shared with Epic 23.0 AC5).
   1. Author the section. 2. Cross-reference the migration. 3. COMMIT: `git commit -m "docs(personas): document all-human-names convention"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~30m

### 6.5.A [ ] **[Human-names convention docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.5`) — review for accuracy/completeness; ensure no contradiction with 23.0. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.6 [ ] **[README Quickstart hierarchy rewrite](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-01](plan/acceptance-criteria/07-01-readme-quickstart-hierarchy-rewrite.md)
   Rewrite `README.md`'s Quickstart to lead with `atcr quickstart` (Synthetic, monetizing default); position frontier/majors personas as opt-in "bring your own key," out of the default funnel. Match `plan/documentation/onboarding-hierarchy.md`'s locked tier language.
   1. Rewrite Quickstart. 2. Verify tier order (Synthetic > DashScope > Chutes/Featherless > LiteLLM(advanced) > majors(opt-in)). 3. COMMIT: `git commit -m "docs(readme): lead Quickstart with Synthetic onboarding hierarchy"`
   **Files:** `README.md` | **Duration:** ~1h

### 6.6.A [ ] **[README rewrite - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.6`) — verify tier order and framing exactly match the locked onboarding-hierarchy language; no royal-we; frontier truly opt-in. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.7 [ ] **[personas-install tier detail + discover flow](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-02](plan/acceptance-criteria/07-02-personas-install-tier-detail-and-discover-flow.md)
   Update `docs/personas-install.md` with the full tier detail (DashScope secondary; Chutes then Featherless with caveats; LiteLLM advanced proxy) and the exact discover-install-verify-by-model bash flow (using real `--model`/`--provider` flags and real persona names).
   1. Author tier detail + flow. 2. Verify commands run against the shipped CLI. 3. COMMIT: `git commit -m "docs(personas): install tiers + discover-by-model flow"`
   **Files:** `docs/personas-install.md` | **Duration:** ~1h

### 6.7.A [ ] **[personas-install docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.7`) — verify the documented flow uses real flags/names shipped in Phases 4/5 and the tier caveats match the locked language. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.8 [ ] **[personas-authoring discover-by-model cross-reference](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-03](plan/acceptance-criteria/07-03-personas-authoring-discover-by-model-cross-reference.md)
   Add the discover-by-model cross-reference to `docs/personas-authoring.md` linking the authoring contract to the discovery flow.
   1. Add cross-reference. 2. Verify links resolve. 3. COMMIT: `git commit -m "docs(personas): cross-reference discover-by-model flow"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~30m

### 6.8.A [ ] **[authoring cross-ref docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.8`) — verify links resolve and the cross-reference is accurate/consistent with 6.4/6.5. Findings-table-only.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

### 6.9 [ ] **Phase 6 DoD**
   1. Tests (T3): `go test ./...` all passing (fixture asserts bound-model metadata)
   2. Docs match the enforced behavior and locked onboarding-hierarchy language
   3. Coverage ≥80%; Lint/vet/fmt clean
   4. DoD report (Stories 6, 7)
   5. COMMIT residual: `git commit -m "docs(personas): phase 6 DoD"`

### 6.LAST [ ] **Phase 6 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 6 (fixture runner, `docs/personas-authoring.md`, `docs/personas-install.md`, `README.md`).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 6 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (enforcement matches documented convention?), CONFIG SURFACE (docs cite real flags/names/tiers?), INTEGRATION (fixture assertion doesn't break built-in path?), PHASE-EXIT CONTRACT (nothing left for Phase 7 but validation?), REGRESSION (all prior tests intact?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 7 / Final Phase: Integration & Validation (1 day)

> Full-suite pass and cross-story integration proof. This is the validation phase — no new feature work.

### 7.1 [ ] **Cross-phase integration: custom prompts resolve via ResolvePersona**
   **Task:** Confirm Story 4's authored personas' custom prompts actually resolve via Story 1/3's `ResolvePersona` chain (not just per-story unit tests). Drive the delivery end-to-end for at least one frontier and one flat-rate persona.
   1. Install an authored persona from the mock registry.
   2. Resolve it via `ResolvePersona`; assert the winning source and the resolved custom prompt text.
   3. Assert guardrails still hold (length cap, fixture gate).
   **Success Criteria:** Authored custom prompts resolve deterministically through the single chain; guardrails enforced.
   **Files:** `internal/personas/resolve_test.go` (integration) | **Duration:** ~2h

### 7.2 [ ] **AC6 end-to-end discover-by-model flow (mock registry)**
   **Task:** Drive the full "I have model X → find and install its persona" flow against `httptest.NewServer` + `ATCR_PERSONAS_URL`: `search --model <X>` → `install` → `list` → `test` (fixture).
   1. Exercise search → install → list → test for a representative model.
   2. Assert each step succeeds and the persona is discoverable strictly by structured model data.
   **Success Criteria:** AC6 flow passes end-to-end against the mock registry (live `samestrin/atcr` deferred until the repo is public).
   **Files:** `internal/personas/*_test.go` / `cmd/atcr/*_test.go` | **Duration:** ~2h

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥80%
- [ ] Lint/format clean: `golangci-lint run`, `go vet ./...`, `go fmt ./...`
- [ ] Build succeeds: `go build ./...`
- [ ] Zero live network calls in CI (all fetch tests use `httptest.NewServer`)

### Optional: Targeted Mutation Testing
MUTATION_TOOL = **UNAVAILABLE** (no Go mutation tool detected: no `stryker-mutator`/`mutmut`/`cargo-mutants`). Skip. If a Go mutation tool (e.g. `go-mutesting`/`gremlins`) is installed later, target ONLY high-risk changed files (`internal/personas/resolve.go`, `search.go`) — never the full codebase.
**WARNING:** Do NOT run full codebase mutation — it can take hours. Target specific files.

### Drift Analysis
Compare the delivered sprint against [plan/original-requirements.md](plan/original-requirements.md) — verify each AC (AC1-AC8) is satisfied and no scope was added beyond the original request, and that Clarifications C1/C2/C3 hold (custom prompts resolve; one unit + one resolution chain; untrusted-input guardrails).
- [ ] AC1-AC8 traced to delivered work
- [ ] C1/C2/C3 honored
- [ ] No out-of-scope work introduced (DashScope quickstart wiring, separate org repo, OAuth, PII redaction, marketplace UI, registry.yaml mapping — all remain out of scope)

### 7.LAST [ ] **Final GATE: Sprint Exit Review (subagent)**
   **Scope:** Full sprint diff.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Sprint exit review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (all 8 ACs delivered?), CONFIG SURFACE (all new config/flags documented + back-compat?), INTEGRATION (cross-story flow proven?), PHASE-EXIT CONTRACT (ready for `/execute-code-review`?), REGRESSION (no earlier-phase behavior broken?). Severity rubric; "ONLY the findings table."

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before sprint exit, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Sprint gate passed" — ready for /execute-code-review
   **Duration:** 15-30 min
