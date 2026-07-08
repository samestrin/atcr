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
- The existing single deterministic `ResolvePersona` precedence chain (`internal/registry/persona.go:46`) **extended** to resolve self-contained persona units (co-located `<name>.md` custom prompts), with untrusted-input guardrails — length cap + hard fixture gate + `{{ }}` guardrail (Story 1 / AC1, C1/C2/C3).
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

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files in the same package as the code under test (Go convention). New: `internal/personas/search_test.go`. Extend: `internal/registry/persona_test.go` (the **existing** `ResolvePersona` chain — no new resolver package), `client_test.go`, `test`-fixture file, `cmd/atcr/personas_test.go`, `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`. All network exercised via `httptest.NewServer` + `ATCR_PERSONAS_URL` override — **zero live network calls in CI**.

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

## Clarifications

### Phase 2 Clarifications (recorded 2026-07-07)

Answers to the Phase 2 safety-check questions (points where sprint-plan task text and the locked ACs disagreed). Authoritative for Phase 2 execution.

**Key Decisions:**

1. **index.json "population" scope at Phase 2 (tasks 2.4–2.6).** Per LOCKED decision Q4, `personas/community/index.json` is **authored in-repo, not code-generated**. Phase 2 creates it as an **empty `[]` array** alongside the AC7 enforcement test (`internal/personas/search_test.go`), and proves the gate works via a **separate negative-case fixture in `testdata`** (not by mutating the real empty file). Real entries land in Phase 5 (tasks 5.13–5.15), when AC 04-05's "reject a still-empty index" scenario applies — no conflict. Task 2.5's word "generation" is superseded: no generator is built.

2. **docs/personas-authoring.md schema subsection — write in Phase 2, do NOT defer to Phase 6.** AC 02-02 lists `docs/personas-authoring.md` among its own Related Files, co-located with the struct and its enforcement test; the schema-subsection (documenting the full `index.json` entry schema: `name`/`version`/`description`/`path`/`provider`/`model`/`tasks`/`tags`, with `tasks`/`tags` optional/omitted-when-absent) is folded into **task 2.5 (GREEN)**. Phase 6 task 6.4 is a *different, narrower* requirement (AC 06-01: the persona YAML's own `model:` key enforced by the fixture runner) and does not cover the index schema.

**Scope Boundaries:**
- `Tasks`/`Tags` are forward-looking schema only — decoded and stored additively, with **no search filter this sprint** (AC 02-01 field-semantics note). No task/tag search matching is added.
- Phase 2 lands: struct fields (2.1–2.3), index-field population contract = empty index + AC7 gate test + negative fixture + authoring-doc schema subsection (2.4–2.6), back-compat decode (2.7–2.9), URL repoint (2.10–2.12). Out of Phase 2: fetch-and-pin / `--offline` / ResolvePersona (Phase 3), model-aware search filters (Phase 4), persona content (Phase 5).

**Technical Approach:**
- `Provider`/`Model`/`Tasks`/`Tags` added with `json:"...,omitempty"`; the original four fields (`Name`/`Version`/`Description`/`Path`) stay byte-for-byte unchanged (no `omitempty` added).
- The index-entry decode path stays **permissive** (`encoding/json`, no `KnownFields(true)`) so old-shape payloads decode with zero-value new fields — strict decode belongs to the persona-load path, not the index path.
- `RegistryBaseURL` → exactly `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community`; `BaseURL()` env-override logic untouched; the existing `TestBaseURL_DefaultWhenUnset` (asserts the old URL) is **updated** in the 2.10 RED step, not duplicated.
- Existing `fakeIndexJSON` + `testServer` helpers in `personas_test.go` are reused for the `FetchIndex`-path back-compat tests.

### Phase 3 Clarifications (recorded 2026-07-07)

Answers to the Phase 3 safety-check questions (open decisions the ACs/design-note left to Phase 3, plus one AC 01-05 internal contradiction). Authoritative for Phase 3 execution.

**Key Decisions:**

1. **`--force` never overwrites existing persona files (adjudicates AC 01-05 contradiction).** AC 01-05 Related-Files L15 ("never overwritten unless `--force`") is superseded by the higher-authority user-story SMART criteria (L28, unconditional) and AC 01-05 DoD L70 ("with or without `--force`"). Existing `.atcr/personas/*.{md,yaml}` and community `~/.config/atcr/personas/*` files are **never** overwritten by `init`/`quickstart`, with or without `--force`. `--force` retains ONLY its role of bypassing the top-level "config already exists at .atcr/config.yaml" proceed-gate (`init.go:76-78`, untouched). **Code change required (task 3.11):** today's `runInit` write closure calls `os.Remove(path)` unconditionally when `force` is true (`cmd/atcr/init.go:96-101`), clobbering every target including personas — this becomes **persona-aware** (skip the remove for a pre-existing persona file so it survives byte-for-byte), while non-persona targets (config.yaml, .gitignore) keep today's force-overwrite behavior.

2. **TD-001 darwin back-compat: record no pre-public-launch back-compat owed; do NOT build a migration in task 3.14.** Redefining `internal/personas.PersonasDir()` (`paths.go:19`) to `filepath.Dir(DefaultRegistryPath())/personas` is done for chain reconciliation; the darwin dir move (`~/Library/Application Support/atcr/personas` → `~/.config/atcr/personas`) orphans no real user pre-public-launch. Record this rationale in-code at `paths.go`; leave the one-time move/symlink migration as an open fast-follow under **TD-001** (unchanged, stays deferred).

3. **`{{ }}` untrusted-prompt guardrail = reject-at-load (design-note §4 "preferred").** A fetched `<name>.md` whose body contains `{{`/`}}` beyond the known required template variables is rejected at install/load time with a descriptive error — no silent escaping/transform. Keeps all three C3 guardrails (length cap, hard fixture gate, `{{ }}`) uniformly load-time-rejecting and avoids a hand-rolled escaper as the sole injection barrier.

**Scope Boundaries:**
- IN scope Phase 3: ACs 01-02 (fetch-and-pin), 01-03 (`--offline`), 01-04 (fetch-failure errors), 01-05 (preservation + source labeling), 01-06 (ResolvePersona extension + C3 guardrails + dir reconciliation).
- OUT of Phase 3: model-aware search filters (Phase 4), persona content authoring (Phase 5), the community-persona **fixture runner** itself (Phase 6 / AC 06-03). Phase 3 wires the **hard fixture-gate seam** in the resolve/install path; the runner that the seam invokes for community personas lands in Phase 6.
- Built-in `.md` reformatting into the unified unit format = deferred bounded fast-follow (out of scope this sprint).

**Technical Approach:**
- Length cap references `registry.MaxExecutorSystemPromptLen` (=4096) **directly**, not a hardcoded literal (resolves TD-002).
- `init` **preserves** the embedded-builtin `.md` copy into `.atcr/personas/` (project tier) AND **adds** fetch-and-pin of community personas into `~/.config/atcr/personas/` (resolver Registry tier). Fetch-and-pin never targets `.atcr/personas/`.
- The pin is the fetched YAML's own `version` field (no new pin file); existing `list`/`upgrade` read/compare it unchanged.
- `internal/personas.PersonasDir()` → `registry.DefaultRegistryPath()` introduces **no import cycle** (validated: `internal/personas` already imports `internal/registry` at `install.go:9`; `internal/registry` imports the top-level embed package `personas`, not `internal/personas`).
- Co-located `<name>.md` + `<name>.yaml` are installed **atomically together**; `Install()` today writes only `.yaml`, so the paired atomic write is net-new install work.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike — Resolution Chain Design (1 day)

> Design spike only — **no shipped code, no tests**. Output: a short design note (`plan/design-notes/resolution-chain.md`) that Phase 3 implements against. **The resolver already exists** — `internal/registry.ResolvePersona` (`internal/registry/persona.go:46`), a 6-level chain called at review time from `internal/fanout/review.go:999` over `PersonaDirs{Project, Registry}` (`review.go:150-162`). This sprint **extends** that one chain; it does **not** author a second resolver in `internal/personas`. Pre-locked decisions: precedence = project `.atcr/personas` override > pinned community (`~/.config/atcr/personas`) > embedded built-in (matches the existing `PersonaDirs` order); unit on-disk shape = **co-located `<name>.md` installed atomically with `<name>.yaml`** (single `.md` prompt format shared with built-ins, per C2). Built-in `.md` reformatting is deferred to a bounded fast-follow (built-ins resolve through the same chain via a thin adapter, not a physical file rewrite).

### 1.1 [x] **🔬 Spike: Map how to extend the existing review-time resolution chain**
   **Task:** The review-time call site and resolver are already located — `internal/fanout/review.go:999` → `internal/registry.ResolvePersona` (`internal/registry/persona.go:46`). The spike documents **how to extend** that existing chain for community units and how to reconcile the install dir with the resolver dir — NOT how to build a new resolver, and NOT re-deciding the unit shape or precedence (both pre-locked above). Confirm the length-cap constant to mirror (`MaxExecutorSystemPromptLen`=4096, `internal/registry/config.go:83`).
   **Priority:** Critical | **Effort:** 1 day
   1. Document the existing `ResolvePersona` signature and its `PersonaDirs{Project, Registry}` source order (verbatim from `persona.go:46`); identify the exact extension point for the pinned-community self-contained unit.
   2. Specify the **dir reconciliation**: `internal/personas.PersonasDir()` (today `os.UserConfigDir()/atcr/personas`) MUST equal the resolver's `Registry` dir (`filepath.Dir(DefaultRegistryPath())/personas` = `~/.config/atcr/personas`). On darwin these currently differ — record the fix so a fetched persona lands on the chain.
   3. Specify how a co-located `<name>.md` community unit is read by the existing chain (built-ins already resolve `.md` via the same path — no divergent format).
   4. Record the length-cap value and the `{{ }}` template-metacharacter guardrail for untrusted fetched prompts (C3).
   **Success Criteria:** `plan/design-notes/resolution-chain.md` exists with: the existing call site + signature (file:line), the extension point, the dir-reconciliation fix, the co-located-`.md` read path, precedence + collision rule (both pre-locked), length-cap + `{{ }}` guardrail. No production code changed.
   **Files:** `plan/design-notes/resolution-chain.md` (new) | **Duration:** ~1 day

### 1.2 [x] **Phase 1 DoD**
   1. Design note complete and internally consistent with C1/C2/C3.
   2. No code/test changes (spike only) — `git status` shows only the design note.
   3. COMMIT: `git add .planning/sprints/active/19.6_community_registry_hub/plan/design-notes/resolution-chain.md && git commit -m "docs(personas): resolution-chain design spike (phase 1)"`

### 1.LAST [x] **Phase 1 - GATE: Design Note Exit Review (subagent)**
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

   **Gate findings — first pass (1 HIGH, 1 MEDIUM, 1 LOW; all cited facts verified accurate):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | resolution-chain.md §2 | Binding-only paragraph claimed a fall-through to the referenced built-in that the resolver does not perform (`persona.go:76-79` hard-fails an explicit differently-named ref; `persona.go:97` keys embedded lookup on `agentName`) — contradicted §1/§6. | **FIXED** — §2 rewritten with the explicit `persona == agentName` precondition; no false fall-through; consistent with §1/§6. |
   | MEDIUM | paths.go:19 | Redefining `PersonasDir()` orphans darwin installs at the old `~/Library/Application Support` dir (AC1 back-compat). | **Deferred → TD-001** (`tech-debt-captured.md`); flagged in §3 for Phase 3. |
   | LOW | config.go:83 | Length cap couples to the executor `MaxExecutorSystemPromptLen`; literal `4096` invites hardcode drift. | **Deferred → TD-002** (`tech-debt-captured.md`). |

   **Gate re-run after HIGH fix:** `| NONE | Phase gate passed |` — no CRITICAL/HIGH remain. **Phase gate passed.**

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

### 2.1 [x] **[PersonaIndexEntry schema extension - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-01](plan/acceptance-criteria/02-01-persona-index-entry-schema-extension.md)
   Write comprehensive failing tests for the extended struct: `Provider`/`Model`/`Tasks`/`Tags` with `omitempty` tags decode from a full-shape `index.json`; verify fail correctly.
   **Files:** `internal/personas/search_test.go` (new) | **Duration:** ~1h

### 2.2 [x] **[PersonaIndexEntry schema extension - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Add `Provider`/`Model`/`Tasks`/`Tags` fields (all `omitempty`) to `PersonaIndexEntry`. Minimal code, one test at a time (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): extend PersonaIndexEntry with provider/model/tasks/tags (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m

### 2.2.A [x] **[PersonaIndexEntry schema extension - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Changed Files:** `internal/personas/search.go`, `internal/personas/search_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation — intentional. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including the changed files (absolute paths), the checklist verbatim (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric CRITICAL/HIGH/MEDIUM/LOW, and: "Required output: ONLY the findings table, no prose." Focus: are the new fields truly additive (no strict-decode breakage on the index path)?

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | search_test.go | No test for the bare 4-field old-shape payload (all new keys absent) — the core additive contract asserted only partially. | **FIXED in 2.3** — added `TestPersonaIndexEntry_BareOldShapeDecodes` (also fully covered by task 2.7 / AC 02-03). |
   | MEDIUM | search_test.go | No test proves the decode path stays permissive for UNKNOWN extra keys (no `KnownFields(true)` regression). | **FIXED in 2.3** — added `TestPersonaIndexEntry_UnknownKeysIgnored`. |
   | LOW | search_test.go:111 | Fragile whole-blob substring check for absent `tasks`/`tags` keys. | **FIXED in 2.3** — switched to `map[string]json.RawMessage` key assertion. |
   | LOW | search.go:34 | Confirmation only: `Search` filters Name/Description; Tasks/Tags not consumed; no injection/perf surface. | No action (non-defect). |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

   **Outcome:** No CRITICAL/HIGH. Adversarial review passed. MEDIUM/LOW resolved inline in 2.3 REFACTOR (cheaper than deferring; strengthens the additive-contract coverage that task 2.7 also covers) rather than logged as tech debt.

### 2.3 [x] **[PersonaIndexEntry schema extension - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): address review + clean up schema"`
   **Duration:** ~30m

### 2.4 [x] **[index.json field population contract - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-02](plan/acceptance-criteria/02-02-index-json-field-population-contract.md)
   Write failing tests asserting the index generation populates `provider`/`model` (and `tasks`/`tags` where present) from persona YAML sources into `index.json` entries; verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~1h

### 2.5 [x] **[index.json field population contract - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Implement the generation/population so structured fields flow from YAML → index entry. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): populate structured fields in index.json generation (green)"`
   **Files:** `internal/personas/search.go` (+ generation path) | **Duration:** ~1h

### 2.5.A [x] **[index.json field population contract - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Changed Files:** generation path + `search.go` + tests.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Adversarial review: 2.5`) with changed-file absolute paths, the verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric, "ONLY the findings table" instruction. Focus: index/YAML source drift (mismatched provider/model claims).

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | search_test.go negative test | Single fixture combines provider+model failure, so a partial regression (only one check breaks) stays green. | **FIXED in 2.6** — negative fixture split into 4 single-failure cases (provider-only mismatch, model-only mismatch, empty-provider, empty-model). |
   | MEDIUM | search_test.go negative test | Assertion pinned on the entry `path` filename (prefixed onto every message), not the discriminating reason. | **FIXED in 2.6** — asserts on discriminating substrings (`provider mismatch`, `model mismatch`, `empty provider`, `empty model`); empty-check split into separate provider/model messages. |
   | LOW | search_test.go helper | Entry identity (name/version) not cross-checked vs YAML — only provider/model. | **Doc note added in 2.6** (scope limit stated in helper comment; AC 02-02 scopes the gate to provider/model). |
   | LOW | search_test.go helper | `filepath.Join` neutralizes absolute paths but not `../` escape. Negligible (test-only, in-repo) but an unvalidated join. | **FIXED in 2.6** — added abs/escape guard that emits a problem. |
   | LOW | personas-authoring.md | Doc marks provider/model "Required" while the struct tags them `omitempty` (gate-enforced, not type-enforced). | **FIXED in 2.6** — added a note that "required" is gate-enforced. |

   **Action Required:**
   - CRITICAL/HIGH found -> List for 2.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

   **Outcome:** No CRITICAL/HIGH. Adversarial review passed. All MEDIUM/LOW resolved inline in 2.6 REFACTOR (they harden the AC7 gate itself) rather than deferred as tech debt.

### 2.6 [x] **[index.json field population contract - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   Fix CRITICAL/HIGH from 2.5.A; improve quality, maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): tighten index generation"`
   **Duration:** ~30m

### 2.7 [x] **[Backward-compatible decode - RED](plan/user-stories/02-structured-model-metadata-schema.md)**
   **AC:** [02-03](plan/acceptance-criteria/02-03-backward-compatible-decode-test.md)
   Write a failing test asserting an **old-shape** `index.json` (no new fields) decodes cleanly against the extended struct with zero-value new fields, no decode error; verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~45m

### 2.8 [x] **[Backward-compatible decode - GREEN](plan/user-stories/02-structured-model-metadata-schema.md)**
   Ensure the index-entry decode path stays permissive (`encoding/json` unknown-field tolerance / no `KnownFields(true)` on this path). Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): guarantee old-shape index.json decodes (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m

### 2.8.A [x] **[Backward-compatible decode - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-structured-model-metadata-schema.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 2.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: any strict-decode leak that could reject old payloads.

   **Subagent findings (1 HIGH — fixed in 2.9 before proceeding):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | search_test.go FetchIndex guards | Guard tests feed only KNOWN keys through FetchIndex, so switching `client.go:129` to `DisallowUnknownFields` would break forward-compat yet the suite stays green — the real path is unguarded against a strict-decode regression. | **FIXED in 2.9** — added `TestFetchIndex_UnknownKeysTolerated` serving an entry with a `future_field` unknown key through the real FetchIndex path; asserts no error. This test fails under `DisallowUnknownFields`, genuinely pinning the guarantee. |
   | MEDIUM | search_test.go ForwardCompatRestrictedTarget | Test decodes into an ad-hoc local 4-field struct via raw `json.Unmarshal` — exercises stdlib behavior, not any atcr type/path; a tautology giving false guard confidence. | **FIXED in 2.9** — removed; superseded by the FetchIndex-routed unknown-key guard above (plus the struct-level `TestPersonaIndexEntry_UnknownKeysIgnored` from 2.3). |
   | LOW | search_test.go empty/nil asserts | Absent-key zero-value assertions are trivially true; the load-bearing assertion is `require.NoError`. | Kept for documentation (per reviewer); the real coverage now lives in the unknown-key guard. |

   **Action Required:**
   - CRITICAL/HIGH -> 2.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** 1 HIGH (guard did not pin the guarantee) — fixed inline in 2.9 before proceeding; MEDIUM tautology removed. Malformed-JSON path already guarded by `TestSearch_MalformedJSON`.

### 2.9 [x] **[Backward-compatible decode - REFACTOR](plan/user-stories/02-structured-model-metadata-schema.md)**
   Fix CRITICAL/HIGH from 2.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): back-compat decode cleanup"`
   **Duration:** ~20m

### 2.10 [x] **[RegistryBaseURL repoint - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-01](plan/acceptance-criteria/01-01-registry-base-url-repoint.md)
   Write failing tests: default `BaseURL()` resolves to the `samestrin/atcr` in-repo community path; `ATCR_PERSONAS_URL` override still wins; HTTPS-only. Verify fail correctly.
   **Files:** `internal/personas/client_test.go` (extend) | **Duration:** ~45m

### 2.11 [x] **[RegistryBaseURL repoint - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Repoint the single `RegistryBaseURL` constant (`internal/personas/client.go`) to `samestrin/atcr` + in-repo community path; leave `BaseURL()`'s env-override-else-constant logic untouched. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): repoint RegistryBaseURL to samestrin/atcr (green)"`
   **Files:** `internal/personas/client.go` | **Duration:** ~20m

### 2.11.A [x] **[RegistryBaseURL repoint - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 2.11`) — changed files (`client.go`, `client_test.go`), verbatim checklist, severity rubric, findings-table-only. Focus: MITM/HTTP-vs-HTTPS, and any subcommand path that bypasses `BaseURL()`.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | client.go:49 / fetch | `ATCR_PERSONAS_URL` override accepted verbatim; an `http://` override would fetch over plaintext. **Pre-existing** (default is HTTPS; tests deliberately override with `http://localhost`). Reviewer: "Do not block this repoint." | No action — intentional escape hatch for local/mock registries; not caused by this change; not net-new debt. |
   | LOW | personas_test.go:59 vs client_test.go | Redundant with the new concrete-value test. | No action — the env-unset→constant invariant is a valid pre-existing guard; not my file to remove (minimal-touch). |

   **Reviewer confirmations (no defect):** no BYPASS — all fetch entrypoints (`Install`, `InstallBundle`, `Search`, `Upgrade`) route through `BaseURL()`; new nested path constructs correctly for namespaced names (`.../community/security/owasp.yaml`); no stale old-URL refs; new tests pin the value and fail on revert; HTTPS pinned.

   **Action Required:**
   - CRITICAL/HIGH -> 2.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH; the two LOWs are pre-existing/harmless (not net-new debt). Adversarial review passed.

### 2.12 [x] **[RegistryBaseURL repoint - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 2.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): base URL repoint cleanup"`
   **Duration:** ~20m

### 2.13 [x] **Phase 2 DoD**
   1. Tests (T3): `go test ./internal/personas/...` all passing
   2. Coverage ≥80% on touched files; Lint/vet/fmt clean
   3. Backward-compat decode test proves old `index.json` still parses
   4. DoD report (Stories 1-partial, 2)
   5. COMMIT any residual: `git commit -m "test(personas): phase 2 DoD"`

### 2.LAST [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (`internal/personas/search.go`, `client.go`, tests).

   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 2 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (struct shape stable for Phases 3/4/5?), CONFIG SURFACE (new fields documented/defaulted/back-compat?), INTEGRATION (URL repoint doesn't break install/search/list/upgrade callers?), PHASE-EXIT CONTRACT (downstream consumes schema without rework?), REGRESSION (existing persona tests intact?). Severity rubric; "ONLY the findings table."

   **Gate findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | search_test.go (verifyCommunityIndex) | AC7 gate cross-checks provider/model at the entry `path` but never asserts `path == name + ".yaml"`; a Phase 5 name≠path divergence could pass while `install <name>` fetches a different YAML. | **Deferred → TD-003** — exact community layout/name↔path convention is a Phase 5 decision; strict assertion now risks being too rigid. |
   | LOW | search_test.go (TestCommunityIndex_ProviderModelMatchesYAML) | Gate passes vacuously against the empty `[]` index until Phase 5 authors entries. | Intended/documented; `TestVerifyCommunityIndex_FailsOnMismatch` (badindex fixture) is the standing guard. No action. |

   **Reviewer confirmations (clean):** PersonaIndexEntry shape/json-tags correct and Phase 3/4/5-consumable; docs §5 maps 1:1 to the struct (incl. omitempty); URL repoint has no stale callers (all route through `BaseURL()`); empty `[]` index tolerated by `FetchIndex`→`Search`→"No personas found"; back-compat decode guarded; gate YAML extraction matches the registry agent schema (`internal/registry/config.go:191-192`).

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **Outcome:** No CRITICAL/HIGH → **Phase gate passed.** 1 MEDIUM (TD-003) + 1 LOW deferred/no-action per protocol.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Core Resolution — Fetch-and-Pin + ResolvePersona Chain (3.5 days)

> The heaviest code phase — implement fetch-and-pin in `init`/`quickstart`, the `--offline` fallback, and **extend the existing single-precedence-chain resolver** (`internal/registry.ResolvePersona`, `persona.go:46`) with untrusted-input guardrails (length cap, hard fixture gate, `{{ }}` metacharacter guardrail, pin-for-reproducibility) — plus the install-dir↔resolver-dir reconciliation. Do NOT author a second resolver in `internal/personas`. Implement against Phase 1's design note. Test types: Integration (mock-registry) + Unit (precedence ordering, length-cap rejection) + E2E (existing-workspace preservation, source labeling).

### 3.1 [x] **[init/quickstart fetch-and-pin - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-02](plan/acceptance-criteria/01-02-init-quickstart-fetch-and-pin.md)
   Write failing integration tests (mock `httptest.NewServer` + `ATCR_PERSONAS_URL`): `init`/`quickstart` fetch personas and **pin a version** reproducibly; `atcr personas upgrade` advances the pin. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`, `internal/personas/client_test.go` | **Duration:** ~3h

### 3.2 [x] **[init/quickstart fetch-and-pin - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement fetch-and-pin version tracking in `internal/personas` and wire `init`/`quickstart` to obtain personas by fetch-and-pin. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): fetch-and-pin for init/quickstart (green)"`
   **Files:** `internal/personas/client.go`, `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** ~4h

### 3.2.A [x] **[init/quickstart fetch-and-pin - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.2`) — changed-file absolute paths, verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), severity rubric, findings-table-only. Focus: pin reproducibility, transport timeout vs. context deadline, retry/backoff reuse.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | init.go:31-38 | `atcr init` writes config+personas, then unconditionally fetches; a fetch failure traps the user behind the "config exists" guard with no non-network path. | **Addressed by next tasks in-phase:** `--offline` (3.4) adds the non-network path; never-overwrite `--force` (3.10) removes the clobber-on-rerun risk. Verify closure at Phase 3 DoD. |
   | MEDIUM | client.go:28 | Pin not reproducible — base URL targets mutable `main`; pin is the YAML `version` field. | **By design (locked):** AC 01-01 fixes the URL at `.../main/...`; US-01 Data Requirements lock pin = YAML `version` with explicit `upgrade`. Not a defect. |
   | MEDIUM | init.go:94 / unit.go:62 | Index `Path` decoded but unused; a Name/Path divergence fetches the wrong URL → 404 → init aborts. | **Covered by TD-003** (name↔path coupling, deferred to Phase 5 when the community layout locks). Install-by-name is pre-existing behavior. |
   | MEDIUM | unit.go:83-89 | Stale `.md`: a persona going binding-only upstream leaves its old custom prompt on disk on re-install. | **FIXED in 3.3** — `InstallUnit` now removes any stale co-located `.md` on a binding-only result; pinned by `TestInstallUnit_BindingOnlyRemovesStaleMD`. |
   | LOW | init.go:83 | Empty-index error message references `--offline`, not yet a registered flag. | Becomes valid in **3.4** (next task registers the flag). No action. |
   | LOW | unit.go:80-89 | Doc overclaimed crash-atomicity across the two-file write. | **FIXED in 3.3** — doc softened to "on error return"; crash-window caveat documented. |

   **Action Required:**
   - CRITICAL/HIGH -> 3.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Two findings fixed inline in 3.3 (stale-`.md` removal + doc). Two MEDIUM are addressed by later in-phase tasks (3.4/3.10) or already tracked (TD-003); one MEDIUM + one LOW are by-design/next-task, no new tech debt.

### 3.3 [x] **[init/quickstart fetch-and-pin - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fetch-and-pin cleanup"`
   **Duration:** ~1h

### 3.4 [x] **[--offline flag fallback - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-03](plan/acceptance-criteria/01-03-offline-flag-fallback.md)
   Write failing tests: `--offline` skips the community fetch entirely (zero network calls) and falls back to embedded built-ins. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go` / `quickstart_test.go` | **Duration:** ~1.5h

### 3.5 [x] **[--offline flag fallback - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement the `--offline` stub path. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): --offline embedded-builtin fallback (green)"`
   **Files:** `cmd/atcr/init.go` / `quickstart.go`, `internal/personas/client.go` | **Duration:** ~2h

### 3.5.A [x] **[--offline flag fallback - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.5`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: does `--offline` truly make zero network calls; graceful degradation completeness.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | quickstart.go flag help + keyEnvFlow | `--offline --open` still launches the OS browser to the signup URL (an explicit second flag), so the flag help's "(zero network)" wording overclaims. | **FIXED in 3.6** — dropped "(zero network)" from both `init` and `quickstart` `--offline` help; the fetch-zero-network guarantee stands, `--open` is an explicit user-initiated browser hand-off. |

   Reviewer confirmed: offline gating is correct on BOTH commands and every sub-path (fresh / existing-workspace-skip / --force); `personasClient` is never reached offline (proven by `failingHTTPClient` t.Fatal); manifest load / registry write / workflow scaffold touch no network; offline workspace resolves the roster fully against embedded built-in `.md` files.

   **Action Required:**
   - CRITICAL/HIGH -> 3.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Single LOW fixed inline in 3.6 (help-text precision); no tech debt.

### 3.6 [x] **[--offline flag fallback - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.5.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): offline fallback cleanup"`
   **Duration:** ~45m

### 3.7 [x] **[Fetch-failure error handling - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-04](plan/acceptance-criteria/01-04-fetch-failure-error-handling.md)
   Write failing tests: fetch failure (without `--offline`) returns a descriptive, non-zero-exit error wrapped with `%w`; no silent fallback. Verify fail correctly.
   **Files:** `internal/personas/client_test.go` | **Duration:** ~1.5h

### 3.8 [x] **[Fetch-failure error handling - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement descriptive error path (reuse existing retry/backoff for transient 429/5xx). Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): descriptive fetch-failure errors (green)"`
   **Files:** `internal/personas/client.go` | **Duration:** ~1.5h

### 3.8.A [x] **[Fetch-failure error handling - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: swallowed errors, silent fallback, exit-code correctness.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | init.go rollback + unit.go:94-98 | Rollback only tracked prior successes, so the currently-failing persona's own leftover `.yaml` (from the binding-only stale-`.md`-remove failure path in `InstallUnit`, which returns without rolling back its yaml) would survive — violating all-or-nothing. | **FIXED in 3.9** — rollback candidates are now recorded BEFORE the `InstallUnit` call (with a pre-existed flag), so the failing persona's files are included; rollback removes only newly-created (`!preExisted && exists`) files. |
   | LOW | init.go fileExists | `fileExists` returned `err == nil` (false on ANY error), contradicting its doc and risking deleting a pre-existing file (or dropping a created one) on a non-ENOENT stat error. | **FIXED in 3.9** — now `err == nil \|\| !errors.Is(err, fs.ErrNotExist)` (non-ENOENT treated as present → errs toward NOT deleting). |
   | LOW | init.go per-persona error | Per-persona failure was prefixed "failed to fetch community personas: installing %q" even for validation/disk errors — a misleading label. | **FIXED in 3.9** — neutral prefix "failed to install community persona %q"; the index-fetch branch keeps "failed to fetch community personas". |

   Reviewer confirmed: no silent fallback; every failure branch returns a non-nil descriptive error that propagates to a non-zero exit; the `os.Remove` rollback error is correctly ignored (best-effort).

   **Action Required:**
   - CRITICAL/HIGH -> 3.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. All three findings (1 MEDIUM + 2 LOW) fixed inline in 3.9 — they harden the all-or-nothing rollback and error accuracy; no tech debt.

### 3.9 [x] **[Fetch-failure error handling - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fetch-error cleanup"`
   **Duration:** ~45m

### 3.10 [x] **[Preserve existing personas + source labeling - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-05](plan/acceptance-criteria/01-05-preserve-existing-personas-and-source-labeling.md)
   Write failing E2E test: rerun `init --force` against a workspace with a hand-edited `.atcr/personas/*.md` — the file is **byte-for-byte unchanged**; missing community personas install alongside it; each persona's source is labeled. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go` | **Duration:** ~2h

### 3.11 [x] **[Preserve existing personas + source labeling - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Implement workspace preservation + source labeling. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): preserve on-disk personas + source labels (green)"`
   **Files:** `cmd/atcr/init.go`, `internal/personas/*` | **Duration:** ~2h

### 3.11.A [x] **[Preserve existing personas + source labeling - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.11`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: data-loss (overwriting a user's edited file), idempotence of `--force`.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | init.go:171-176 | Exists-gate trips on any target (incl. a surviving persona when config.yaml was deleted); "config already exists" message can be inaccurate. | **Deferred → TD-004** — Q1 LOCKED the gate untouched and `TestInit_AlreadyExists` pins the message; protective intent is correct. Message-accuracy follow-up. |
   | LOW | personas.go:205 | `list --scores` still uses `List` (2-tier), diverging from plain `list` (3-tier) on the Source column. | **Deferred → TD-005** — `--scores` is a separate feature; threading projectDir is beyond AC 01-05 scope. |
   | LOW | list.go listProject | Nested `sub/_base.md` emitted as a bogus persona row (only top-level `_base` skipped). | **FIXED in 3.12** — skip whenever `filepath.Base(path) == "_base.md"` (any depth). |
   | LOW | init.go community skip / unit.go | Skip guarded only `<name>.yaml`; a lone hand-edited `<name>.md` (no sibling yaml) was overwritten → silent prompt data loss. | **FIXED in 3.12** — skip when EITHER `.yaml` OR `.md` exists; pinned by `TestInstallCommunityPersonas_SkipsLoneExistingMD`. |
   | LOW | list.go | Post-init all 9 built-ins render Source `project` (init scaffolds editable copies). | Confirmed intended — matches `ResolvePersona` level-2 project precedence. No action. |

   Reviewer confirmed: `write` preserve-branch protects persona files/symlinks under --force; community skip + `writeFileAtomic` symlink guard hold; rollback correctly excludes skipped/pre-existing files; `ListTiers` precedence is correct and deterministic.

   **Action Required:**
   - CRITICAL/HIGH -> 3.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Two data-safety/correctness LOWs fixed inline in 3.12 (lone-`.md` skip closes a silent data-loss path; nested `_base.md` skip); one MEDIUM + one LOW deferred to TD-004/TD-005 (locked-gate message + `--scores` tier parity); one LOW confirmed by-design.

### 3.12 [x] **[Preserve existing personas + source labeling - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): preservation/labeling cleanup"`
   **Duration:** ~45m

### 3.13 [x] **[Custom-prompt ResolvePersona precedence chain - RED](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **AC:** [01-06](plan/acceptance-criteria/01-06-custom-prompt-resolution-precedence.md)
   Write comprehensive failing unit tests **extending `internal/registry/persona_test.go`** for the existing `ResolvePersona`: single deterministic precedence (project `.atcr/personas` override > pinned community `~/.config/atcr/personas` > embedded built-in); collision resolves to exactly one source; the community install dir == the resolver's `Registry` dir (darwin regression); **length cap** rejects oversized custom prompts; **hard fixture gate** blocks a fixture-failing prompt from resolving; **`{{ }}` metacharacters** in a fetched prompt are not expanded; a fetched custom prompt (co-located `<name>.md`) resolves as one self-contained unit (C1/C2/C3). Verify fail correctly.
   **Files:** `internal/registry/persona_test.go` (extend) | **Duration:** ~3h

### 3.14 [x] **[Custom-prompt ResolvePersona precedence chain - GREEN](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Extend** the existing `internal/registry.ResolvePersona` per the design note: keep one chain, resolve the pinned-community co-located `<name>.md` unit, add guardrails (length cap mirroring `MaxExecutorSystemPromptLen`=4096, hard fixture gate, `{{ }}` guardrail, pin). Reconcile `internal/personas.PersonasDir()` to the resolver's `Registry` dir (`~/.config/atcr/personas`) so installs land on the chain. Built-ins already resolve `.md` through the same chain — no new format. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): extend ResolvePersona chain for community units + guardrails (green)"`
   **Files:** `internal/registry/persona.go` (extend), `internal/personas/paths.go` (dir reconcile), `internal/personas/*` (install co-located `.md`) | **Duration:** ~5h

### 3.14.A [x] **[Custom-prompt ResolvePersona precedence chain - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 3.14`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus (HIGH-RISK, security-sensitive): prompt-injection via fetched prompt, oversized-prompt DoS, leftover `{{ }}` template injection, ambiguous collision / double-load / panic, fixture-gate bypass.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | persona.go level-4 `_base.md` read | Registry-tier `_base.md` bypasses `validateCommunityPrompt`; the resolve-guard comment overclaimed "catches hand-dropped files". | **FIXED in 3.15 (comment)** — `_base.md` is a structural template that MUST contain `{{.Payload}}` and CANNOT be fetched (name `_base` fails validation), so it is intentionally exempt; comment narrowed to state the guard covers `<persona>.md` only. No behavior change (guarding `_base` would break base templates). |
   | LOW | unit.go install guard / personas.go:134 | Install-time guard was near-dead: `personas install` used `Install` (YAML only, no `.md`), so it delivered no custom prompt and skipped the install-time guardrail (C2 gap). | **FIXED in 3.15** — single-persona `personas install` routed through `InstallUnit` (delivers the co-located `.md` + applies the guard); pinned by `TestPersonasInstall_DeliversCustomPrompt`. Bundle `.md` delivery deferred → **TD-006**. |

   Reviewer confirmed: untrusted text flows to `text/template.Parse`, and `Contains("{{")` catches every Go template trigger; fetch body capped at 5 MB before validation; install/resolve caps agree (both `len()` bytes, 4096); `PersonasDir()` == resolver Registry dir on all OSes, no import cycle; precedence deterministic, no double-load/panic.

   **Action Required:**
   - CRITICAL/HIGH -> 3.15, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. MEDIUM resolved as a comment-accuracy fix (the flagged `_base` path is intentionally exempt + unfetchable); LOW fixed inline in 3.15 (install now delivers the unit) with bundle delivery deferred to TD-006.

### 3.15 [x] **[Custom-prompt ResolvePersona precedence chain - REFACTOR](plan/user-stories/01-community-canonical-fetch-and-pin-distribution.md)**
   Fix CRITICAL/HIGH from 3.14.A (security findings are non-negotiable inline fixes); maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): harden ResolvePersona + guardrails"`
   **Duration:** ~1.5h

### 3.16 [ ] **Phase 3 DoD**
   1. Tests (T3): `go test ./...` all passing (Story 1 complete)
   2. Coverage ≥80%; Lint/vet/fmt clean
   3. Security guardrails (length cap, fixture gate) proven by tests
   4. DoD report (Story 1)
   5. COMMIT residual: `git commit -m "test(personas): phase 3 DoD"`

### 3.LAST [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`client.go`, `internal/registry/persona.go`, `internal/personas/paths.go`, `init.go`, `quickstart.go`, tests).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 3 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (the extended `ResolvePersona` still honors its existing signature & consumable by review-time callers, no second resolver introduced?), CONFIG SURFACE (`--offline`, pin file documented/back-compat?), INTEGRATION (fetch-and-pin doesn't regress install/upgrade; install dir == resolver dir on darwin?), PHASE-EXIT CONTRACT (Phase 5 personas can be delivered via this chain?), REGRESSION (Phase 2 schema still intact; existing built-in `.md` resolution unbroken?). Severity rubric; "ONLY the findings table."

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
   **Files:** `personas/community/testdata/*_fixture.patch` | **Duration:** ~3h

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
   **Files:** `personas/community/testdata/*_fixture.patch` | **Duration:** ~3h

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
   Ensure every library persona has a `<slug>_fixture.patch` in `personas/community/testdata/` (the community-fixture location locked in AC 04-04, with the extended `//go:embed community/testdata/*.patch` runner path); run the fixture test and confirm the currently-missing ones fail. Verify fail.
   **Files:** `personas/community/testdata/*` | **Duration:** ~1.5h

### 5.11 [ ] **[Fixture authoring & fixture-test pass - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Complete all fixtures; full fixture test passes (T2). COMMIT: `git commit -m "content(personas): complete fixtures (green)"`
   **Files:** `personas/community/testdata/*` | **Duration:** ~2h

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
   **Files:** `internal/registry/persona_test.go` (integration) | **Duration:** ~2h

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
MUTATION_TOOL = **UNAVAILABLE** (no Go mutation tool detected: no `stryker-mutator`/`mutmut`/`cargo-mutants`). Skip. If a Go mutation tool (e.g. `go-mutesting`/`gremlins`) is installed later, target ONLY high-risk changed files (`internal/registry/persona.go`, `internal/personas/search.go`) — never the full codebase.
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
