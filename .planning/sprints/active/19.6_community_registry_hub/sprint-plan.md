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

### Phase 5 Clarifications (recorded 2026-07-07)

Answers to the Phase 5 safety-check questions (content decisions the ACs left to "the author at commit time"). Authoritative for Phase 5 execution.

**Key Decisions:**

1. **Human names for the 10 community personas (LOCKED).** Roster, keyed by vendor/tier:

   | Vendor / tier | Name |
   |---------------|------|
   | Claude flagship | **Anthony** |
   | Claude fallback | **Sonny** |
   | GPT flagship | **Gene** |
   | GPT fallback | **Milo** |
   | Gemini flagship | **Gia** |
   | Gemini fallback | **Flint** |
   | DeepSeek | **Delia** |
   | Qwen | **Quinn** |
   | Kimi (Moonshot) | **Celeste** |
   | GLM (Zhipu) | **Glenna** |

   All ten clear the 14 already-taken names (`bruce/greta/kai/mira/dax/otto` built-ins, `sasha/penny/ingrid` renames, `pace/vera/brad/archer/ronin` production panel). The slug = the lowercased name (e.g. `personas/community/anthony.yaml`).

2. **Routing key = `openrouter` for all 10 (LOCKED).** NOT `synthetic`, not a mix. Rationale: atcr's bundled synthetic catalog covers only MiniMax/glm-5/kimi-k2.5 and cannot resolve DeepSeek, Qwen, or any of the 3 frontier vendors — it physically can't back 6 of the 10 personas. `openrouter` is a real multi-vendor gateway covering all ten and is an established fixture in the codebase's tests. The content-lint allowlist for AC 04-01/04-02 Edge Case 2 is therefore `{openrouter}` (may include `synthetic` as an also-allowed key, but every authored persona uses `openrouter`).

3. **Model-id strings: verified verbatim against the live OpenRouter catalog (`https://openrouter.ai/api/v1/models`, 2026-07-07).** No best-guesses — every persona's `model` is a real catalog slug (checked byte-for-byte against the raw `/api/v1/models` JSON): anthony `anthropic/claude-opus-4.8`, sonny `anthropic/claude-sonnet-5`, gene `openai/gpt-5.5`, milo `openai/gpt-5.4-mini` (no `gpt-5.5-mini` exists in-catalog; newest mini is 5.4), gia `google/gemini-2.5-pro`, flint `google/gemini-2.5-flash` (stable same-family Pro/Flash pair — the newer 3.x Pro is preview-only), delia `deepseek/deepseek-v4-pro`, quinn `qwen/qwen3-coder-plus`, celeste `moonshotai/kimi-k2.7-code`, glenna `z-ai/glm-5.2`. Flagship≠fallback within each frontier family; the vendor token in `model` (`claude`/`gpt`/`gemini`/`deepseek`/`qwen`/`kimi`/`glm`) — the load-bearing grouping key — is correct for all ten. YAML and `index.json` model fields kept byte-for-byte in lockstep (enforced by the AC7 `verifyCommunityIndex` gate).

4. **Vendor-guidance sourcing: author from training knowledge (cutoff Jan 2026) now.** The `<!-- vendor-guidance: <url-or-section> -->` marker is machine-checked for presence/traceability only; genuine grounding fidelity is the explicit MANUAL review gate (tasks 5.2.A / 5.8.A). During the 5.8.A adversarial review, run a cheap live-fetch spot-check of the 3 frontier vendors' canonical prompting-guide URLs (Anthropic/OpenAI/Google) as cheap insurance — not mandated, applied opportunistically.

**Scope Boundaries:**
- Frontier = exactly 6 (3 vendors × flagship+fallback, distinct model ids per pair); open = exactly 4 (DeepSeek/Qwen/Kimi/GLM, one each). Total = 10 community personas.
- Community layout is authoritative over the sprint task-line shorthand: `personas/community/<slug>.{yaml,md}`, fixtures at `personas/community/testdata/<slug>_fixture.patch` (new `//go:embed community/testdata/*.patch` runner path per AC 04-04), registered in `personas/community/index.json`.
- `Tasks`/`Tags` are additive display/search metadata only; no new search matching added this phase.

**Technical Approach:**
- `provider: openrouter` on every community persona; vendor identity via the `model` token. This deliberately differs from `examples/registry-*.yaml` (which use `provider: anthropic/openai/google`) — the AC's LOCKED Q3 routing-key semantics win for the community library.
- Each persona's category word is authored into the prompt template itself (not leaked from the injected fixture diff), verified per-persona.

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

### 3.16 [x] **Phase 3 DoD**
   1. ✅ Tests (T3): `go test ./...` all passing (Story 1 complete — 40 packages ok)
   2. ✅ Coverage ≥80% (internal/personas 83.3%, internal/registry 93.3%, cmd/atcr 83.8%); ✅ golangci-lint 0 issues; `go vet`/`gofmt` clean
   3. ✅ Security guardrails proven by tests: length cap + `{{ }}` reject at install (`TestInstallUnit_Rejects*`) and resolve (`TestPersonaResolution_Registry*`); dir reconciliation (`TestPersonasDir_EqualsResolverRegistryDir`); namespaced traversal + intermediate-symlink refusal; all-or-nothing rollback; never-overwrite preservation
   4. DoD report (Story 1) — see Outcome below
   5. COMMIT residual: `git commit -m "test(personas): phase 3 DoD"`

   **Story 1 DoD (ACs 01-02..01-06):** All Auto-Verified (tests pass / lint clean / build ok) and Story-Specific items satisfied — fetch-and-pin into the resolver Registry dir with version pin (01-02); `--offline` zero-network embedded fallback (01-03); descriptive fetch-failure errors + all-or-nothing rollback (01-04); never-overwrite existing personas with/without `--force` + 3-tier source labeling (01-05); single `ResolvePersona` chain resolves community units (flat + namespaced) with C1/C2/C3 guardrails, dir reconciliation, deterministic precedence (01-06). Deferred: TD-001, TD-004..TD-007 (all non-blocking, documented).

### 3.LAST [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`internal/personas/{unit,paths,list}.go`, `internal/registry/persona.go`, `cmd/atcr/{init,quickstart,personas}.go`, tests).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 3 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT, CONFIG SURFACE, INTEGRATION, PHASE-EXIT CONTRACT, REGRESSION.

   **Gate findings — first pass (1 HIGH, 2 MEDIUM):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | persona.go validateName vs config.go validateAgent | Namespaced `<provider>/<name>` personas unreachable: `validateName` rejected any `/` while `validateAgent` permits it in the `persona:` field — a namespaced community unit installed to a nested path could never resolve at review time (breaks C1). | **FIXED before boundary** — `validateName` now accepts a bounded `/`-separated namespace, validating each segment (no `''`/`.`/`..`/leading-dot/`_base` segment, no backslash, not absolute); install and resolve now agree. New tests: `TestPersonaResolution_NamespacedCommunityResolves`, `_NamespacedTraversalStillRejected`. **Gate re-run → passed.** |
   | MEDIUM | upgrade.go:27-92 | `Upgrade` refreshes only the `.yaml`, not the co-located `.md` → stale-prompt hazard on upgrade. | **Deferred → TD-007** (AC 01-02 scoped `upgrade` to "no logic change"; unit-refresh work parked with TD-006). |
   | MEDIUM | bundles.go:152 | Bundle install delivers YAML only, not the co-located `.md` (C2 delivery-path inconsistency). | **Deferred → TD-006** (already captured in 3.14.A). |

   **Gate re-run after HIGH fix (1 LOW):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | persona.go readNonEmpty | Leaf-only symlink guard; a symlinked INTERMEDIATE namespace dir could read outside the pin dir (traversal-via-symlink opened by allowing `/`). | **FIXED inline** — `hasSymlinkedParent` refuses a symlinked intermediate (falls through); pinned by `TestPersonaResolution_NamespacedSymlinkedIntermediateRefused`. No-op for flat names. |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop

   **Outcome:** HIGH fixed before the boundary and re-gated; the sole re-gate LOW (intermediate-symlink) fixed inline as a security-hardening (self-introduced vector). Reviewer confirmed: `ResolvePersona` signature unchanged & consumed at `review.go:999`; no second resolver (C2); `--offline` registered on both commands; PersonasDir == resolver Registry == upgrade dir on all OSes; Phase 2 index schema untouched (Phase 4 safe); Phase 5 namespaced + co-located-`.md` units resolve through the existing chain. **Phase gate passed.**

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Discovery — Model-Aware Search (1.5 days)

> Structured `--model`/`--provider` filtering with **zero free-text fallback**, backward-compatible keyword search, flag/arg validation. Test types: Integration (flag registration, table rendering) + Unit (structured-field-only matching, near-miss substring cases).

### 4.1 [x] **[Structured model/provider filtering - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-structured-model-provider-filtering.md)
   Write failing tests: `Search` matches structured `Provider`/`Model` fields only; a persona whose `Description` mentions a model but whose structured `Model` differs is **NOT** returned under `--model` (no free-text fallback). Verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~1h

### 4.2 [x] **[Structured model/provider filtering - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Extend `Search()` to filter on structured fields. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): structured model/provider filtering (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~1h

### 4.2.A [x] **[Structured model/provider filtering - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.2`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: any free-text leak into `--model` matching; case/normalization edge cases.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | search.go:45-48 | Stale `Search` doc comment says keyword matches "name or description" only, understating that `matchesKeyword` also reaches structured Provider/Model. | **FIXED in 4.3** — comment updated to name all four fields. |
   | LOW | search_test.go | No test here exercises the Keyword path's structured reach (OR into Provider/Model), so the "keyword reaches structured, but --model/--provider do NOT reach free text" distinction is only half-verified in this file. | **FIXED in 4.3** — added `TestSearchWithOptions_KeywordReachesStructuredFields` (keyword OR-reach + whitespace trim). AC 03-02 (task 4.4) adds the dedicated back-compat regression suite. |

   **Reviewer confirmation (no defect):** no free-text leak into `--model`/`--provider` (both match only their structured field); AND semantics correct; empty/whitespace filters trimmed to absent; substring/case-insensitivity deliberate.

   **Action Required:**
   - CRITICAL/HIGH -> 4.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. Adversarial review passed. Both LOWs resolved inline in 4.3 REFACTOR (they harden the AC 03-01/03-02 contract coverage) rather than deferred as tech debt.

### 4.3 [x] **[Structured model/provider filtering - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): filtering cleanup"`
   **Duration:** ~30m

### 4.4 [x] **[Keyword search backward-compatibility - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   > **Note:** AC 03-02's core behavior (positional keyword reaches structured Provider/Model; Name/Description parity preserved) was already implemented in task 4.2's `matchesKeyword`, so these backward-compat/regression guards pass on first run — a genuine RED failure is not achievable without artificially breaking working code. Tests added: `TestSearchWithOptions_KeywordPlusFlagAND` (Scenario 2 AND), `TestSearch_OldShapeKeywordParity` (Error Scenario 1 old-shape parity across the legacy `Search` wrapper + `SearchWithOptions`). Structured-reach (Scenario 3) already pinned by 4.3's `TestSearchWithOptions_KeywordReachesStructuredFields`.
   **AC:** [03-02](plan/acceptance-criteria/03-02-keyword-search-backward-compatibility.md)
   Write failing tests: bare `atcr personas search <term>` still matches `Name`/`Description` substrings exactly as before (no regression). Verify fail correctly.
   **Files:** `internal/personas/search_test.go` | **Duration:** ~45m

### 4.5 [x] **[Keyword search backward-compatibility - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Preserve keyword path alongside structured filters. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(personas): preserve keyword search path (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** ~30m
   > **Green with no production change:** the keyword path was already preserved (and extended to structured fields) by 4.2. `go test ./internal/personas/...` green; committed the AC 03-02 guard tests under the GREEN message.

### 4.5.A [x] **[Keyword search backward-compatibility - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.5`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: interaction of keyword + structured flags (AND/OR semantics).

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | search_test.go parity | Cross-call `ElementsMatch(legacy, opts)` is a tautology (`Search` just wraps `SearchWithOptions`); the concrete literal-set assertions carry the real proof. | **FIXED in 4.6** — replaced the cross-call comparison with a hardcoded expected-set assertion on the `SearchWithOptions` keyword result. |
   | LOW | search_test.go coverage | No `Keyword + Model` AND case — the one pairing where the keyword's OR-reach into structured Model could interact with the structured `--model` filter. | **FIXED in 4.6** — added `{Keyword:"deepseek", Model:"gpt-4"}` → `finn` only, proving AND-narrowing. |
   | LOW | search.go:75 | Per-entry redundant lowercasing (flag path + `matchesKeyword` each lowercase `e.Model`/`e.Provider`). | **Declined (not deferred):** AC 03-01 Performance explicitly deems this negligible for index sizes in the hundreds; precomputing per-entry lowercase adds complexity for no measurable gain (minimum-code). Recorded here, not logged as TD. |

   **Action Required:**
   - CRITICAL/HIGH -> 4.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. Adversarial review passed. Two LOWs (test-hardening) fixed inline in 4.6; the perf LOW declined per the AC's own negligible-overhead stance.

### 4.6 [x] **[Keyword search backward-compatibility - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.5.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): search path cleanup"`
   **Duration:** ~20m

### 4.7 [x] **[Flag registration & arg validation - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-03](plan/acceptance-criteria/03-03-flag-registration-and-arg-validation.md)
   Write failing tests: `--model`/`--provider` registered on `newPersonasSearchCmd` following the `--scores` pattern; invalid arg combos return a `RunE` error (no `os.Exit`). Verify fail correctly.
   **Files:** `cmd/atcr/personas_test.go` | **Duration:** ~1h

### 4.8 [x] **[Flag registration & arg validation - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Register flags + validation on the search command. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(cli): --model/--provider flags on personas search (green)"`
   **Files:** `cmd/atcr/personas.go` | **Duration:** ~1h

### 4.8.A [x] **[Flag registration & arg validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.8`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: error paths use `RunE` returns not `os.Exit`; output via `cmd.OutOrStdout()`.

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   **Reviewer confirmations (no defect):** guard fires the exact canonical string via `usageError` (exit 2), never `os.Exit`; flag values trimmed before the guard (defense-in-depth with `SearchWithOptions`' own trim) so empty flags never trigger an unfiltered match; guard reachable for every all-empty combo; all output via `cmd.OutOrStdout()`; `MaximumNArgs(1)` scoped to the search command only; keyword-path "No personas found matching %q" preserved verbatim.

   **Action Required:**
   - CRITICAL/HIGH -> 4.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No findings. Adversarial review passed.

### 4.9 [x] **[Flag registration & arg validation - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.8.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(cli): search flag cleanup"`
   **Duration:** ~30m
   > **No-op REFACTOR:** 4.8.A returned zero findings; no CRITICAL/HIGH to fix and no cleanup warranted (the GREEN already routes output through `cmd.OutOrStdout()`, uses `RunE`/`usageError`, and trims flags). Suite green — no separate commit.

### 4.10 [x] **[Search table provider/model columns - RED](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **AC:** [03-04](plan/acceptance-criteria/03-04-search-table-provider-model-columns.md)
   Write failing tests for `renderPersonaSearch` output including `provider`/`model` columns. Verify fail correctly.
   **Files:** `cmd/atcr/personas_test.go` | **Duration:** ~45m

### 4.11 [x] **[Search table provider/model columns - GREEN](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Render provider/model columns. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "feat(cli): render provider/model columns in search (green)"`
   **Files:** `cmd/atcr/personas.go` | **Duration:** ~45m

### 4.11.A [x] **[Search table provider/model columns - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-model-aware-search-and-discovery.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 4.11`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: empty-field rendering, column alignment with `omitempty` values.

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | personas.go renderPersonaSearch/writeTable | Untrusted community-index fields (Name/Provider/Model/Description) written raw to the terminal → control-char/ANSI injection (newline row-forgery, tab misalignment, escape spoofing). Partly pre-existing; new Provider/Model columns widen it. | **Deferred → TD-008** (below CRITICAL/HIGH inline bar; largely pre-existing; no untrusted index served in CI pre-public-launch). |
   | LOW | personas.go:458 | `Description` emitted raw (not via `orDash`), so an empty Description is a blank cell rather than "-". | **Declined (intentional):** AC 03-04 scopes the "-" placeholder to Version/Provider/Model; Description was rendered raw pre-change (no regression), and real entries always carry a description. |

   **Reviewer confirmations (no defect):** header order pinned correctly (`NAME VERSION PROVIDER MODEL DESCRIPTION`); empty-set renders header-only; empty Version/Provider/Model correctly yield "-"; `%` in a value is never reinterpreted (all `%s` args).

   **Action Required:**
   - CRITICAL/HIGH -> 4.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. Adversarial review passed. MEDIUM → TD-008; LOW declined as intentional (out of AC 03-04 scope).

### 4.12 [x] **[Search table provider/model columns - REFACTOR](plan/user-stories/03-model-aware-search-and-discovery.md)**
   Fix CRITICAL/HIGH from 4.11.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(cli): search rendering cleanup"`
   **Duration:** ~20m
   > **No-op REFACTOR:** 4.11.A found no CRITICAL/HIGH; the sole MEDIUM was deferred to TD-008 (not an inline fix). GREEN already extracted the shared `orDash` placeholder helper, so no additional cleanup warranted. Suite green — no separate commit.

### 4.13 [x] **Phase 4 DoD**
   1. Tests (T3): `go test ./...` all passing (Story 3 complete)
   2. Coverage ≥80%; Lint/vet/fmt clean
   3. Structured-only matching proven (no free-text fallback)
   4. DoD report (Story 3)
   5. COMMIT residual: `git commit -m "test(personas): phase 4 DoD"`

### 4.LAST [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (`search.go`, `cmd/atcr/personas.go`, tests).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 4 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT, CONFIG SURFACE (flags documented for Phase 7 docs?), INTEGRATION (search consumes Phase 2 schema correctly?), PHASE-EXIT CONTRACT (Story 7 docs can cite real flag names?), REGRESSION (keyword search intact?). Severity rubric; "ONLY the findings table."

   **Gate findings — first pass (no CRITICAL/HIGH; 2 LOW test-coverage gaps):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | personas_test.go NoMatch | REGRESSION gate weak: `TestPersonasSearch_NoMatch` asserted only `Contains("No personas found")`, which also passes the flag-only branch — the keyword-path `No personas found matching %q` wording was pinned by no test. | **FIXED inline** — asserts the full `No personas found matching "quantum"` string. |
   | LOW | personas_test.go (no search-help test) | CONFIG SURFACE: no `search --help` test asserting `--model`/`--provider` + the `--model deepseek` example that Phase 7 docs will cite; a later edit could drop them undetected. | **FIXED inline** — added `TestPersonasSearch_HelpCitesModelProviderFlags`. |

   **Reviewer confirmations (no defect):** `SearchOptions`/`SearchWithOptions` concrete & stable; flags registered with descriptions + `MaximumNArgs(1)` + all-empty guard behave as specified; search consumes Phase 2 `PersonaIndexEntry.Provider`/`Model` correctly (structured-only for flags, OR-reach for keyword); build + all Search/Render tests green. Both findings were test-coverage gaps, non-blocking.

   **Gate re-run after LOW fixes:** `| NONE | Phase gate passed |` — no CRITICAL/HIGH; the two LOW coverage gaps closed inline (they harden the phase-exit/config-surface contract Phase 7 depends on). **Phase gate passed.**

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

### 5.1 [x] **[Frontier flagship+fallback persona pairs - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-01](plan/acceptance-criteria/04-01-frontier-flagship-fallback-persona-pairs.md)
   Author/lock fixtures for the 3 frontier pairs (Anthropic/OpenAI/Google, each flagship primary + same-family fallback). Verify fixtures fail (personas not yet authored).
   **Files:** `personas/community/testdata/*_fixture.patch` | **Duration:** ~3h

### 5.2 [x] **[Frontier flagship+fallback persona pairs - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Author each persona (YAML binding `provider`+`model` flagship+fallback + Markdown prompt phrased per that provider's official guide), human-named. Fixtures pass (T1/T2). COMMIT: `git commit -m "content(personas): frontier flagship+fallback library (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~6h

### 5.2.A [x] **[Frontier flagship+fallback persona pairs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.2`) — persona file paths, checklist adapted (SCHEMA: required `provider`/`model` present & structured? NAMING: human first name, no role name? GROUNDING: prompt reflects the provider's official guide, not a generic template? FIXTURE INTEGRITY: category word authored in the prompt itself, not only from the injected diff?), severity rubric, findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | community/index.json | Index empty — the 6 personas aren't registered, so discovery-by-model can't find them and the AC7 gate passes vacuously. | **Not a defect / planned work** — index registration is tasks 5.13–5.15 (AC 04-05). No TD; will be closed there. |
   | LOW | milo.md Focus #4 | Lens overlap: milo's empty/nil-default bullet duplicates sonny's nil/empty-handling bullet — two lenses claim the same finding. | **FIXED in 5.3** — milo #4 narrowed to *externally-supplied* empty/zero crossing the trust boundary; internal nil-flow left to sonny (logic). |
   | LOW | gia.yaml / flint.yaml | Concurrency/leak prompts lean on Go-specific idioms (goroutine/defer) but declare no `language` scope, so they route onto non-Go reviews where the idiom is inapt. | **FIXED in 5.3** — Focus bullets generalized (goroutine/thread/async task; defer/finally/using/RAII) so the model-indexed personas stay language-agnostic (they are model-tuned, not language-scoped by design — `language` intentionally omitted). |
   | LOW | testdata/*_fixture.patch | `@@` hunk line counts are approximate (render-only payloads, never `git apply`-ed). | **No action** — matches the existing built-in fixture convention (sentinel/tracer fixtures use the same loose counts); fixtures are embedded as diff text, never applied. |

   **Action Required:**
   - CRITICAL/HIGH -> 5.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Two LOWs fixed inline in 5.3 (differentiation + language-agnostic generalization, both harden AC 04-07); the empty-index MEDIUM is planned task 5.13–5.15 (no TD); the fixture-count LOW matches existing convention (no action).

### 5.3 [x] **[Frontier flagship+fallback persona pairs - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.2.A; re-run fixtures (T3); COMMIT: `git commit -m "content(personas): refine frontier personas"`
   **Duration:** ~1h

### 5.4 [x] **[Flat-rate open-model personas - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-02](plan/acceptance-criteria/04-02-flat-rate-open-model-personas.md)
   Author/lock fixtures for the flat-rate open-model personas (DeepSeek/Qwen/Kimi/GLM). Verify fail.
   **Files:** `personas/community/testdata/*_fixture.patch` | **Duration:** ~3h

### 5.5 [x] **[Flat-rate open-model personas - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Author each open-model persona (YAML + vendor-grounded prompt), human-named. Fixtures pass (T1/T2). COMMIT: `git commit -m "content(personas): flat-rate open-model library (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~6h

### 5.5.A [x] **[Flat-rate open-model personas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.5`) — same content-review checklist as 5.2.A. Findings-table-only.

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   Reviewer verified all four: SCHEMA (`provider: openrouter`, vendor token in `model`), NAMING (human names), GROUNDING (lens maps to each model's documented strength; one vendor-guidance citation each), FIXTURE INTEGRITY (category authored into the template, not leaked; genuine synthetic instances), STRUCTURE (Role + byte-for-byte 7-col contract + all 7 template vars), DIFFERENTIATION (complexity/type/dependency/observability distinct from each other and the 6 frontier lenses).

   **Action Required:**
   - CRITICAL/HIGH -> 5.6, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No findings. Adversarial review passed.

### 5.6 [x] **[Flat-rate open-model personas - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.5.A; re-run fixtures (T3); COMMIT: `git commit -m "content(personas): refine open-model personas"`
   **Duration:** ~1h
   > **No-op REFACTOR:** 5.5.A returned zero findings; no CRITICAL/HIGH to fix and no cleanup warranted. Fixtures green — no separate commit.

### 5.7 [x] **[Vendor-grounded prompt structure compliance - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-03](plan/acceptance-criteria/04-03-vendor-grounded-prompt-structure-compliance.md)
   Add tests/checks asserting each prompt renders all required template variables (`{{.AgentName}}`, `{{.ScopeRule}}`, etc.) with no leftovers, and follows the per-vendor structure. Verify fail.
   **Files:** `personas/*_test.go` / fixtures | **Duration:** ~1.5h

### 5.8 [x] **[Vendor-grounded prompt structure compliance - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Bring all prompts into structural compliance. Fixtures/tests pass (T2). COMMIT: `git commit -m "content(personas): vendor-grounded prompt compliance (green)"`
   **Files:** `personas/*.md` | **Duration:** ~2h
   > **RED not independently achievable:** the 10 prompts were authored structurally-compliant in 5.2/5.5 (all required tokens, `## Role`/`## Output Format`, byte-for-byte 7-col contract, one vendor-guidance citation each), so `TestCommunityPersonas_PromptStructure` + `_RendersInBothToolStates` pass on first run — a genuine RED would require artificially breaking working content (same pattern as Phase 4 tasks 4.4/4.5). Tests committed under the GREEN message; no production `.md` change needed.

### 5.8.A [x] **[Vendor-grounded prompt structure compliance - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.8`) — content-review checklist (esp. leftover `{{ }}` template injection). Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer confirmed all 10 prompts render cleanly with no leftover-brace/injection/contract-drift and one non-empty vendor-guidance citation each. All findings were test blind spots:
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | community_test.go contract check | `canonicalOutputContract` asserted anywhere in the file, not anchored to the `## Output Format` section; a mangled block could pass. | **FIXED in 5.9** — contract + "7 pipe-delimited columns" rule text now asserted inside `sectionBody(text, "## Output Format")`. |
   | MEDIUM | community_test.go render guard | Leak guard checked `{{` only, never a stray `}}`; renders never asserted a value actually surfaced. | **FIXED in 5.9** — added `NotContains(out, "}}")` and a positive `Contains(out, "tester")` marker to the render guards. |
   | LOW | community_test.go token presence | Source-text `Contains` for required tokens would pass a token in a dead `{{if false}}` branch or comment. | **FIXED in 5.9** — added `TestCommunityPersonas_RequiredValuesRender`: renders each persona with distinctive sentinel field values and asserts every one surfaces in output. |

   **Action Required:**
   - CRITICAL/HIGH -> 5.9, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. All three test-hardening findings fixed inline in 5.9 (they strengthen the AC 04-03 gate itself); no content defects, no tech debt.

### 5.9 [x] **[Vendor-grounded prompt structure compliance - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.8.A; re-run (T3); COMMIT: `git commit -m "content(personas): prompt structure cleanup"`
   **Duration:** ~45m

### 5.10 [x] **[Fixture authoring & fixture-test pass - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-04](plan/acceptance-criteria/04-04-fixture-authoring-and-fixture-test-pass.md)
   Ensure every library persona has a `<slug>_fixture.patch` in `personas/community/testdata/` (the community-fixture location locked in AC 04-04, with the extended `//go:embed community/testdata/*.patch` runner path); run the fixture test and confirm the currently-missing ones fail. Verify fail.
   **Files:** `personas/community/testdata/*` | **Duration:** ~1.5h

### 5.11 [x] **[Fixture authoring & fixture-test pass - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Complete all fixtures; full fixture test passes (T2). COMMIT: `git commit -m "content(personas): complete fixtures (green)"`
   **Files:** `personas/community/testdata/*` | **Duration:** ~2h

### 5.11.A [x] **[Fixture authoring & fixture-test pass - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.11`) — checklist focus: does any fixture pass only because the category word leaks from the injected diff rather than the prompt? Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer verified leakage is structurally impossible (the category assertion reads raw template text, never the diff), all 10 fixtures plant genuine correctly-labeled category instances, no built-in/community runner collision, embed captures exactly 10+10, no credential-like values. Two LOWs on the CLI-facing runner:
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | test.go renderFixture | Runner gated on `{{` only, not `}}` — weaker than the committed `community_test.go` contract. | **FIXED in 5.12** — `renderFixture` now rejects a leftover `}}` too. |
   | LOW | test.go renderFixture | A template that dropped every token renders brace-free yet substitutes nothing, still reporting `Passed:1`. | **FIXED in 5.12** — `renderFixture` now also requires the AgentName value to be interpolated into the output, so an all-token-dropped template fails. |

   **Action Required:**
   - CRITICAL/HIGH -> 5.12, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Both LOWs fixed inline in 5.12 (they bring the CLI `atcr persona test` runner up to the go-test gate's strength); no leakage, no tech debt.

### 5.12 [x] **[Fixture authoring & fixture-test pass - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.11.A; re-run (T3); COMMIT: `git commit -m "content(personas): fixture integrity cleanup"`
   **Duration:** ~45m

### 5.13 [x] **[Community index registration - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-05](plan/acceptance-criteria/04-05-community-index-registration.md)
   Write failing test: every authored persona appears in the in-repo community `index.json`, discoverable by model. Verify fail.
   **Files:** `personas/community/index.json`, `internal/personas/search_test.go` | **Duration:** ~1h

### 5.14 [x] **[Community index registration - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Generate/populate the community `index.json` from YAML sources so every persona is registered. Test passes (T2). COMMIT: `git commit -m "content(personas): register library in community index.json (green)"`
   **Files:** `personas/community/index.json` | **Duration:** ~1h

### 5.14.A [x] **[Community index registration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.14`) — focus: index/YAML source drift (does every entry's `provider`/`model` match its persona YAML?). Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** all 10 entries verified byte-for-byte consistent with their YAML (provider/model/name/path/description), exactly 10 unique, correct vendor grouping (claude/gpt/gemini=2, deepseek/qwen/kimi/glm=1), `provider=openrouter` on all, tasks/tags non-empty and lens-scoped, no empty `[]`. Two LOW gate-strength gaps:
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | search_test.go verifyCommunityIndex | `description` drift closed by neither gate (verifyCommunityIndex checks only provider/model; registration didn't check description). | **FIXED in 5.15** — `TestCommunityIndex_Registration` now asserts index `description` == YAML `description`. |
   | LOW | community_test.go registration | Never asserted `provider == "openrouter"`; a vendor-named provider in both index+YAML would pass yet break OpenRouter routing. | **FIXED in 5.15** — added `require.Equal("openrouter", e.Provider)` to pin the routing key. |

   **Action Required:**
   - CRITICAL/HIGH -> 5.15, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Both LOWs fixed inline in 5.15 (harden the AC 04-05 gate); no drift, no tech debt.

### 5.15 [x] **[Community index registration - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.14.A; re-run (T3); COMMIT: `git commit -m "content(personas): index registration cleanup"`
   **Duration:** ~30m

### 5.16 [x] **[Strict schema & naming compliance - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-06](plan/acceptance-criteria/04-06-strict-schema-and-naming-compliance.md)
   Write failing tests: every persona decodes under strict `KnownFields(true)`; all names are human first names (no role names). Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1h

### 5.17 [x] **[Strict schema & naming compliance - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Bring all personas into strict-schema + human-name compliance. Tests pass (T2). COMMIT: `git commit -m "content(personas): strict schema + human-name compliance (green)"`
   **Files:** `personas/*.yaml` | **Duration:** ~1h

### 5.17.A [x] **[Strict schema & naming compliance - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.17`) — focus: any unknown YAML field, any residual role-name. Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer empirically confirmed the strict decode rejects unknown keys (`foobar`/`notes`/`author`) while accepting every inline agent field + all 7 catalog keys; no `AgentConfig` field is unreachable (no wiring regression); negative tests genuinely exercise the strict path; empty/malformed handled without panic/silent-pass; all 10 slugs + YAML names are human. One LOW:
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | community_schema_test.go humanNameRe | Naming guard = `^[a-z]+$` + a small denylist; a single-word disguised role (critic/analyst/inspector/…) could pass, and the test read only the slug, not each YAML `name`. | **FIXED in 5.18** — expanded the role denylist, and `TestCommunityPersonas_HumanNames` now asserts each YAML `name` == slug so a role-based name can't hide in a human-slugged file. (A complete first-name allow-list is impractical; name==slug + backstop denylist + manual review is the proportionate guard.) |

   **Action Required:**
   - CRITICAL/HIGH -> 5.18, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. LOW fixed inline in 5.18 (name==slug consistency + expanded denylist); no unknown-field leak, no residual role name, no tech debt.

### 5.18 [x] **[Strict schema & naming compliance - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.17.A; re-run (T3); COMMIT: `git commit -m "content(personas): schema/naming cleanup"`
   **Duration:** ~30m

### 5.19 [x] **[Model-appropriate task-scoping differentiation - RED](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **AC:** [04-07](plan/acceptance-criteria/04-07-model-appropriate-task-scoping-differentiation.md)
   Write failing tests/checks: each persona's `tasks`/scope reflects its model's strength and personas are meaningfully differentiated (not templated clones). Verify fail.
   **Files:** `personas/*_test.go` / metadata checks | **Duration:** ~1h

### 5.20 [x] **[Model-appropriate task-scoping differentiation - GREEN](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Tune each persona's task-scoping. Tests pass (T2). COMMIT: `git commit -m "content(personas): model-appropriate task scoping (green)"`
   **Files:** `personas/*.yaml`, `personas/*.md` | **Duration:** ~2h
   > **RED not independently achievable:** the 10 lenses were authored distinct in 5.2/5.5 (each mapped to a different model strength), so the Jaccard-≤0.85 distinctness test + distinct-primary-task test pass on first run — a genuine RED would require artificially cloning two personas. Tests committed under GREEN; no content change needed. All 45 pairs pass comfortably below threshold.

### 5.20.A [x] **[Model-appropriate task-scoping differentiation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.20`) — focus: are personas genuinely differentiated per model strength, or near-duplicate content? Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer confirmed all 10 lenses genuinely distinct and every lens matches its model tier (reasoning→deep lenses, large-context→whole-surface, fast/cheap→narrow sweeps); measured max Jaccard 0.168.
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | community_test.go Differentiation | Locked 0.85 Jaccard threshold ~5x looser than observed (0.168) — only catches near-verbatim copies. | **Recorded → TD-009** (threshold is AC 04-07-LOCKED; not overridden) + **complementary guard added in 5.21** (`TestCommunityPersonas_DistinctCategories` — distinct category words — plus the existing distinct-primary-task test catch same-lens duplication the loose Jaccard misses). |
   | LOW | flint.md #5 / delia.md #5 | Semantic overlap on unbounded in-memory growth (append-in-loop). | **FIXED in 5.21** — flint #5 narrowed to scarce-handle-pool growth; in-memory-growth-by-cost explicitly ceded to delia's complexity lens (mirrors the milo/sonny handoff). |
   | LOW | index.json tags | `"frontier"` tag reads as a capability claim on fast/cheap fallbacks. | **FIXED in 5.21** — renamed to `"frontier-vendor"` (vendor-class, not capability). |

   **Action Required:**
   - CRITICAL/HIGH -> 5.21, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. Both LOWs fixed inline in 5.21; the MEDIUM is an AC-locked-threshold tension recorded as TD-009 with a complementary categorical guard added (respecting the lock rather than overriding it).

### 5.21 [x] **[Model-appropriate task-scoping differentiation - REFACTOR](plan/user-stories/04-model-indexed-persona-library-authoring.md)**
   Fix CRITICAL/HIGH from 5.20.A; re-run (T3); COMMIT: `git commit -m "content(personas): task-scoping cleanup"`
   **Duration:** ~30m

### 5.22 [x] **[Atomic rename sentinel/tracer/idiomatic - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-atomic-rename-sentinel-tracer-idiomatic.md)
   Write failing tests: `sentinel→sasha`, `tracer→penny`, `idiomatic→ingrid` renamed atomically across all four parts (template, fixture, YAML, registration in `personas/personas.go`'s `names` slice); no mixed-naming state; init-time panic guard passes. Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1.5h

### 5.23 [x] **[Atomic rename sentinel/tracer/idiomatic - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Perform the four-part atomic rename for all three stragglers. Tests pass (T2). COMMIT: `git commit -m "content(personas): rename sentinel/tracer/idiomatic to human names (green)"`
   **Files:** `personas/*.md`, `personas/*.yaml`, `personas/testdata/*`, `personas/personas.go` | **Duration:** ~2h

### 5.23.A [x] **[Atomic rename sentinel/tracer/idiomatic - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.23`) — checklist verbatim + focus: any partial rename (template renamed but `names` slice stale → startup panic), any lingering old slug. Findings-table-only.

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   Reviewer ran `go build`/`go test` (exercising the `init()` names↔embedded-files panic guard) and confirmed: all three parts renamed atomically (`.md` + `_fixture.patch` + `names` slice); `Get("sentinel"/"tracer"/"idiomatic")` return unknown-persona errors (no alias); lenses preserved (sasha=security/injection, penny=perf/n+1, ingrid=idioms); fixtures resolve with matching category words. All lingering slug refs are AC 05-03 Edge-Case-2 exempt (list_test.go sort fixtures, the `performance/tracer` namespaced community fixture, the `retiredRoleSlugs` denylist, and the Go "sentinel errors" idiom in ingrid.md).

   **Action Required:**
   - CRITICAL/HIGH -> 5.24, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No findings. Adversarial review passed.

### 5.24 [x] **[Atomic rename sentinel/tracer/idiomatic - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.23.A; re-run (T3); COMMIT: `git commit -m "content(personas): rename cleanup"`
   **Duration:** ~45m
   > **No-op REFACTOR:** 5.23.A found no issues. Suite green — no cleanup or separate commit warranted.

### 5.25 [x] **[ingrid generalized idiomatic lens - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-02](plan/acceptance-criteria/05-02-ingrid-generalized-idiomatic-lens.md)
   Write failing tests/fixture: `ingrid` is generalized beyond Go (language-agnostic idiomatic lens). Verify fail.
   **Files:** `personas/testdata/ingrid_fixture.patch`, tests | **Duration:** ~1h

### 5.26 [x] **[ingrid generalized idiomatic lens - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Rewrite `ingrid`'s prompt to be language-agnostic. Fixture passes (T2). COMMIT: `git commit -m "content(personas): generalize ingrid beyond Go (green)"`
   **Files:** `personas/ingrid.md` | **Duration:** ~1.5h

### 5.26.A [x] **[ingrid generalized idiomatic lens - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.26`) — focus: any residual Go-specific assumption; fixture integrity. Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer confirmed Role/Focus are genuinely language-agnostic with concrete categories retained (not diluted), structure preserved, both fixtures genuine + synthetic. Three test/example-strength findings:
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | personas_test.go non-Go fixture check | Render-only assertion was vacuous (any payload renders identically). | **FIXED in 5.27** — asserts the rendered output contains the Python fixture's `except Exception`, so the non-Go payload genuinely flows through the generalized lens. |
   | MEDIUM | personas_test.go `\bgo\b` guard | Bare-word guard misses Go constructs (goroutine/defer/strconv/golang/sync.). | **FIXED in 5.27** — added a Go-idiom-token denylist over Role+Focus. |
   | LOW | ingrid.md example | Output-Format example telegraphed the lang2 fixture (same path/violation). | **FIXED in 5.27** — swapped to a distinct Ruby `rescue` example not matching any committed fixture. |

   **Action Required:**
   - CRITICAL/HIGH -> 5.27, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH → proceed. All three fixed inline in 5.27 (they harden the AC 05-02 "generalized beyond Go" verification); no tech debt.

### 5.27 [x] **[ingrid generalized idiomatic lens - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.26.A; re-run (T3); COMMIT: `git commit -m "content(personas): ingrid generalization cleanup"`
   **Duration:** ~30m

### 5.28 [x] **[Retired-slug verification - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-03](plan/acceptance-criteria/05-03-retired-slug-verification.md)
   Write a failing repo-wide (scoped to persona paths) verification test asserting no `sentinel`/`tracer`/`idiomatic` slug remains anywhere in the active set. Verify fail.
   **Files:** `personas/*_test.go` | **Duration:** ~1h

### 5.29 [x] **[Retired-slug verification - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Eliminate any remaining retired slug. Test passes (T2). COMMIT: `git commit -m "content(personas): retired-slug verification (green)"`
   **Files:** persona paths | **Duration:** ~45m

### 5.29.A [x] **[Retired-slug verification - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.29`) — focus: is the scope of the verification wide enough (fixtures, index, registration, docs)? Findings-table-only.

   **Subagent findings (1 HIGH — fixed in 5.30 before proceeding):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | retired_slugs_test.go bareRetiredRe / docs/personas-authoring.md:119 | `\b(sentinel\|tracer)\b` cannot match a `_fixture` stem (`_` is a word char), so a REAL stale `sentinel_fixture.patch` doc ref (introduced in 5.11) false-PASSED the scan. | **FIXED in 5.30** — regex extended with `(sentinel\|tracer)_`; the stale doc ref corrected to `sasha_fixture.patch`. Scan now fails on any underscore-suffixed retired stem. |
   | MEDIUM | retired_slugs_test.go retiredSlugScanFiles | AC scopes package `*_test.go` too, but only `*.md`+`personas.go` were globbed. | **FIXED in 5.30** — now globs `*_test.go` (excluding this self-referential file). |
   | MEDIUM | retired_slugs_test.go regexes | A bare `# idiomatic  built-in` doc code-fence row would slip past (adjective ambiguity). | **Accepted + documented** — catching bare "idiomatic" false-positives on the legitimate adjective ingrid's prompt uses; none exists in-scope; noted in a code comment + manual review of the built-in table. |
   | LOW | retired_slugs_test.go stem check | Only `testdata/*.patch` stem-checked, not `community/testdata/*.patch`. | **FIXED in 5.30** — stem check now globs both. |

   Reviewer also confirmed (no finding): excluding `internal/personas/*_test.go` is defensible (all remaining refs are Edge-Case-2 placeholders + the intentional denylist); the new/old resolution test is correct; and NO retired-slug PERSONA refs remain in other docs/examples — the remaining `sentinel lines`/`sentinel-tagged blocks`/`idiomatic Go` hits are non-persona jargon/adjective and must NOT be changed (5.31–5.33 handoff: nothing to do).

   **Action Required:**
   - CRITICAL/HIGH -> 5.30, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** HIGH fixed before the boundary (real stale doc + scan false-pass closed); two MEDIUM + one LOW fixed inline in 5.30; the adjective-ambiguity MEDIUM accepted with documentation. Scan re-run green.

### 5.30 [x] **[Retired-slug verification - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.29.A; re-run (T3); COMMIT: `git commit -m "content(personas): slug verification cleanup"`
   **Duration:** ~20m

### 5.31 [x] **[Migration documentation updates - RED](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **AC:** [05-04](plan/acceptance-criteria/05-04-documentation-updates.md)
   Identify every doc reference to the old slugs (`docs/`, README) that must change to the new names; capture as a checklist / failing doc-lint. Verify the gaps exist.
   **Files:** `docs/*`, `README.md` (audit) | **Duration:** ~45m

### 5.32 [x] **[Migration documentation updates - GREEN](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Update all doc references to the migrated names. Verify checklist clear (T2 where testable). COMMIT: `git commit -m "docs(personas): update references for straggler rename (green)"`
   **Files:** `docs/*`, `README.md` | **Duration:** ~1h
   > **Subsumed by 5.29/5.30:** AC 05-04 shares AC 05-03's scoped grep, so the doc edits had to land in 5.29 GREEN (personas-install.md L3/L78/L91, personas-authoring.md L61/L130/L148) + 5.30 (personas-authoring.md L119 `sasha_fixture.patch`) to green the retired-slug scan. README was already clean. Committed under the retired-slug (green) + slug-verification-cleanup commits rather than a separate docs commit — no new doc edits remained for this task.

### 5.32.A [x] **[Migration documentation updates - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 5.32`) — focus: any missed doc reference to a retired slug; consistency of the new names across docs. Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH — proceed):** reviewer confirmed ZERO retired-slug hits across the three docs; new slugs present; lens descriptions correct (no stale "Go idioms" for ingrid). One pre-existing roster inaccuracy (not a rename defect):
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | README.md:207 | Said `personas/` holds "six embedded default personas", but the dir embeds nine — contradicts personas-install.md ("nine built-in"). | **FIXED in 5.33** — corrected to "nine". |
   | LOW | README.md:42, :78 | `atcr init` comments/table said "six editable/default personas"; init scaffolds nine. | **FIXED in 5.33** — both corrected to "nine". |

   **Action Required:**
   - CRITICAL/HIGH -> 5.33, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH, no missed rename refs → proceed. Three README count inaccuracies (pre-existing; surfaced by the rename review) fixed inline in 5.33, making the built-in roster count consistent (nine) across README + personas-install.md.

### 5.33 [x] **[Migration documentation updates - REFACTOR](plan/user-stories/05-human-names-migration-for-built-in-stragglers.md)**
   Fix CRITICAL/HIGH from 5.32.A; final read-through; COMMIT: `git commit -m "docs(personas): migration doc cleanup"`
   **Duration:** ~30m

### 5.34 [x] **Phase 5 DoD**
   **DoD report:** `go test ./...` green; retired-slug scan + strict-schema + human-name tests pass (no role-based names in the active set); coverage — personas 84.7%, internal/personas 83.8%, internal/registry 92.2%, cmd/atcr 83.9% (all ≥80%); golangci-lint 0 issues; go vet + gofmt clean. Manual per-persona verification: each fixture test asserts the category word is authored into the prompt TEMPLATE (not leaked from the injected diff); vendor-grounding confirmed by the 5.2.A/5.5.A/5.8.A adversarial reviews.
   **Story-4 (Model-Indexed Library, AC 04-01..04-07):** Complete — 10 personas (6 frontier + 4 open) authored, structured index, strict schema, differentiation ≤0.85 Jaccard.
   **Story-5 (Human-Names Migration, AC 05-01..05-04):** Complete — sentinel→sasha / tracer→penny / idiomatic→ingrid atomic rename, ingrid generalized beyond Go, retired-slug scan green, docs updated.
   1. Tests (T3): `go test ./...` all passing (Stories 4 & 5 complete); all fixtures pass
   2. No role-based names remain anywhere in the active set; strict schema holds
   3. Coverage ≥80%; Lint/vet/fmt clean
   4. Manual per-persona verification complete (category word authored into prompt)
   5. DoD report (Stories 4, 5)
   6. COMMIT residual: `git commit -m "content(personas): phase 5 DoD"`

### 5.LAST [x] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All persona content + `personas/personas.go` + fixtures + index changed during Phase 5.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 5 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (every persona resolvable via Phase 3 `ResolvePersona`?), CONFIG SURFACE (index entries carry structured metadata?), INTEGRATION (no mixed-naming state; built-in panic guard passes at startup?), PHASE-EXIT CONTRACT (Story 6 can assert bound-model metadata against real personas; Story 7 can cite real names?), REGRESSION (existing built-in fixtures still pass?). Severity rubric; "ONLY the findings table."

   **Gate findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   Gate subagent ran `go build`/`go test`/`go vet` (all clean, embed `init()` guard proven by the passing suite) and confirmed: all 10 community personas validate + resolve (slug==YAML name==path stem==`.md` basename); `verifyCommunityIndex` green with exactly 10 `openrouter` entries carrying vendor-token models byte-for-byte matching their YAML; NO mixed-naming state (only the "idiomatic" adjective in ingrid.md, excluded); Phase 6-ready (provider/model in YAML+index, human names); built-in `sasha`/`penny`/`ingrid` + all community fixtures pass; coverage ≥80% across touched packages.

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **Outcome:** No CRITICAL/HIGH/MEDIUM/LOW — **Phase gate passed.**

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 6: Contract Enforcement + Onboarding Docs (1.5 days)

> Close the two remaining documentation/enforcement gaps and rewrite onboarding docs — sequenced after Phases 4 and 5 so cited flags/persona names are accurate. Test types: Unit (fixture test asserts bound-model metadata) + Manual (doc-content review against `plan/documentation/onboarding-hierarchy.md`'s locked tier language).

### 6.1 [x] **[Fixture test asserts bound-model metadata - RED](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-03](plan/acceptance-criteria/06-03-fixture-test-asserts-bound-model-metadata.md)
   Write a failing test extending the fixture runner to assert every community persona's bound `provider`/`model` appears in structured metadata (additive path; keep the `isBuiltin(name)` branch separate). Verify fail correctly.
   **Files:** `internal/personas/` fixture test file | **Duration:** ~1.5h

### 6.2 [x] **[Fixture test asserts bound-model metadata - GREEN](plan/user-stories/06-authoring-contract-enforcement.md)**
   Implement the additive assertion. Minimal code (T1), verify all (T2), COMMIT: `git commit -m "test(personas): fixture asserts bound-model metadata (green)"`
   **Files:** `internal/personas/test.go` (+ fixture runner) | **Duration:** ~1.5h

### 6.2.A [x] **[Fixture test asserts bound-model metadata - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.2`) — changed files, verbatim checklist, severity rubric, findings-table-only. Focus: does the new assertion weaken/alter the existing built-in fixture pass/fail contract?

   **Subagent findings (no CRITICAL/HIGH — proceed):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | test.go:57-60 | Model assertion gated behind fixture presence — a library persona with `.md`+`.yaml` (blank model) but no `_fixture.patch` returns `HasFixture:false` and bypasses the bound-model contract. | **FIXED in 6.3** — reordered so the model-binding assertion runs immediately after `CommunityGet` confirms a library persona, independent of fixture presence. |
   | LOW | test.go:65-68 vs community.go | Asymmetric handling of incomplete bundles + `CommunityModel` doc comment implies soft handling while `RunFixture` hard-fails. | **FIXED in 6.3** — reorder makes `.yaml`-missing a deliberate hard-fail for a resolved library persona; `CommunityModel` doc comment tightened to match. |
   | LOW | test_test.go:46 | Hardcodes churning model id `deepseek/deepseek-v4-pro` — asserts content, breaks on legitimate repin. | **FIXED in 6.3** — assert `provider/model` shape (non-empty, contains `/`) instead of the exact churning literal. |
   | LOW | community.go / test.go | Per-call embed reads/decodes not reused. | No action — reviewer marked "acceptable as-is"; in-memory embed, negligible. |

   **Action Required:**
   - CRITICAL/HIGH -> 6.3, do NOT proceed until fixed | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. The MEDIUM + two LOWs harden the AC7 enforcement gate itself, so resolved inline in 6.3 REFACTOR (consistent with Phase 2's 2.5.A/2.8.A disposition) rather than deferred as tech debt.

### 6.3 [x] **[Fixture test asserts bound-model metadata - REFACTOR](plan/user-stories/06-authoring-contract-enforcement.md)**
   Fix CRITICAL/HIGH from 6.2.A; maintain green (T1), validate (T3); COMMIT: `git commit -m "refactor(personas): fixture assertion cleanup"`
   **Duration:** ~30m

### 6.4 [x] **[Document model-in-structured-metadata convention](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-model-in-structured-metadata-convention.md)
   Update `docs/personas-authoring.md` to document the model-in-structured-metadata convention as a forward-looking authoring rule (asserted by the fixture test from 6.1-6.3).
   1. Author the doc section.
   2. Verify it matches the enforced behavior.
   3. COMMIT: `git commit -m "docs(personas): document model-in-metadata convention"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~45m

### 6.4.A [x] **[Convention docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.4`) — review `docs/personas-authoring.md` changes for accuracy vs. the enforced fixture behavior and completeness. Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | personas-authoring.md §1 | Discovery sentence attributed `search --model` matching to "the `model:` YAML key," blurring the fixture-gate (reads YAML) vs. index-gate (search matches `index.json` entry, synced to YAML by §5) distinction. Core claim ("prose-only model invisible to search") is still true. | **FIXED inline** — reworded to "matches the structured `model` field of the community `index.json` entry — kept in lockstep with this YAML key by the §5 gate." Doc-content test still green. |

   Reviewer confirmed accurate: enforcement point (`internal/personas/test.go`/`RunFixture`) named correctly; built-in EXEMPT (not "asserted to pass"); presence-only (no real/served claim); error wording matches; §3 cross-ref not restated; AC 06-01 complete.

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. The single LOW was a one-line doc-accuracy fix — corrected inline (cheaper than deferring a trivial doc nit to TD) and committed.

### 6.5 [x] **[Document all-human-names convention](plan/user-stories/06-authoring-contract-enforcement.md)**
   **AC:** [06-02](plan/acceptance-criteria/06-02-all-human-names-convention-documented.md)
   Document the all-human-names convention in `docs/personas-authoring.md` as a forward-looking rule (shared with Epic 23.0 AC5).
   1. Author the section. 2. Cross-reference the migration. 3. COMMIT: `git commit -m "docs(personas): document all-human-names convention"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~30m

### 6.5.A [x] **[Human-names convention docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-authoring-contract-enforcement.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.5`) — review for accuracy/completeness; ensure no contradiction with 23.0. Findings-table-only.

   **Subagent findings:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | CRITICAL | personas-authoring.md §1 | The example embedded the literal retired slug tokens `sentinel`/`tracer`/`` `idiomatic` `` — `personas/retired_slugs_test.go:TestNoRetiredSlugs` scans this doc and FAILS on them, breaking CI. (Pre-commit hook runs vet+build only, not `go test`, so it slipped the 6.5 commit.) | **FIXED inline (6.5.A commit)** — reworded to "role- or function-descriptor slug (the style … the built-in stragglers carried before their Phase 5 rename)" emitting none of the forbidden tokens. `TestNoRetiredSlugs` + full `go test ./...` now green. |
   | LOW | personas-authoring.md §1 | `human-names-migration.md` code-span reference is unreachable from `docs/` (lives under `.planning/`). Correctly NOT a hyperlink (no dangling link). | No change — informational artifact name, not a link; acceptable. |

   Reviewer confirmed (no defect): all six example names are real personas; rule stated once, covers built-in AND community, forward-looking, cross-references + records 23.0 as absorbed/superseded, superset-not-contradiction framing, no straggler-mapping re-derivation; grounding test passes.

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** 1 CRITICAL (CI-breaking retired-slug token in the doc) — fixed inline before proceeding, full suite re-verified green. LOW: no action.

### 6.6 [x] **[README Quickstart hierarchy rewrite](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-01](plan/acceptance-criteria/07-01-readme-quickstart-hierarchy-rewrite.md)
   Rewrite `README.md`'s Quickstart to lead with `atcr quickstart` (Synthetic, monetizing default); position frontier/majors personas as opt-in "bring your own key," out of the default funnel. Match `plan/documentation/onboarding-hierarchy.md`'s locked tier language.
   1. Rewrite Quickstart. 2. Verify tier order (Synthetic > DashScope > Chutes/Featherless > LiteLLM(advanced) > majors(opt-in)). 3. COMMIT: `git commit -m "docs(readme): lead Quickstart with Synthetic onboarding hierarchy"`
   **Files:** `README.md` | **Duration:** ~1h

### 6.6.A [x] **[README rewrite - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.6`) — verify tier order and framing exactly match the locked onboarding-hierarchy language; no royal-we; frontier truly opt-in. Findings-table-only.

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   Reviewer ran both hard-gate greps scoped to the section (lines 36-74): `claude|gpt|gemini` and `\b(we|our|us)\b` both ZERO matches. Tier order 1-5 correct, Chutes before Featherless, all verbatim caveats present, DashScope no-wiring + link, bash walkthrough preserved, tier 5 opt-in.

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No findings — proceed.

### 6.7 [x] **[personas-install tier detail + discover flow](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-02](plan/acceptance-criteria/07-02-personas-install-tier-detail-and-discover-flow.md)
   Update `docs/personas-install.md` with the full tier detail (DashScope secondary; Chutes then Featherless with caveats; LiteLLM advanced proxy) and the exact discover-install-verify-by-model bash flow (using real `--model`/`--provider` flags and real persona names).
   1. Author tier detail + flow. 2. Verify commands run against the shipped CLI. 3. COMMIT: `git commit -m "docs(personas): install tiers + discover-by-model flow"`
   **Files:** `docs/personas-install.md` | **Duration:** ~1h

### 6.7.A [x] **[personas-install docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.7`) — verify the documented flow uses real flags/names shipped in Phases 4/5 and the tier caveats match the locked language. Findings-table-only.

   **Subagent findings (no CRITICAL/HIGH/MEDIUM):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | personas-install.md:126 | `test` section said "embedded/installed community-library personas"; the runner resolves only built-in + EMBEDDED library personas (via `CommunityGet`), not on-disk third-party installs. | **FIXED inline** — reworded to "built-in and the embedded community-library personas … a third-party persona … reports `No fixture defined` instead." |

   Reviewer verified against shipped code: `test` runner is genuinely wired (rewrite TRUE); `--model`/`--provider` flags exist and `--model` is case-insensitive substring on structured model; `delia`→`deepseek/deepseek-v4-pro` real (not a placeholder); DashScope snippet matches the real `providers.<name>.{api_key_env,base_url}` schema; caveats verbatim + match README; royal-we ZERO over added sections (124-211).

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No CRITICAL/HIGH. Single LOW accuracy nit fixed inline. Note: this task also drove the discovery of TD-010 (guardrail bug, now fixed) via its AC 07-02 Edge Case 1 "verify against real CLI" requirement, and corrected the stale `test`-subcommand "not yet wired" note.

### 6.8 [x] **[personas-authoring discover-by-model cross-reference](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **AC:** [07-03](plan/acceptance-criteria/07-03-personas-authoring-discover-by-model-cross-reference.md)
   Add the discover-by-model cross-reference to `docs/personas-authoring.md` linking the authoring contract to the discovery flow.
   1. Add cross-reference. 2. Verify links resolve. 3. COMMIT: `git commit -m "docs(personas): cross-reference discover-by-model flow"`
   **Files:** `docs/personas-authoring.md` | **Duration:** ~30m

### 6.8.A [x] **[authoring cross-ref docs - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-onboarding-hierarchy-documentation.md)**
   **Spawn a fresh subagent** (description `Adversarial review: 6.8`) — verify links resolve and the cross-reference is accurate/consistent with 6.4/6.5. Findings-table-only.

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No issues found | - |

   Reviewer verified: no hierarchy/bash duplication; connection named (YAML `provider`/`model` → `search --model`/`--provider`); link resolves to `personas-install.md:156` `## Discover and install a persona by model` (anchor match); human-naming/metadata rules untouched; `--model`/`--provider` real flags; royal-we ZERO.

   **Action Required:**
   - CRITICAL/HIGH -> fix inline before proceeding | MEDIUM/LOW -> `tech-debt-captured.md` | None -> proceed

   **Outcome:** No findings — proceed.

### 6.9 [x] **Phase 6 DoD**
   1. Tests (T3): `go test ./...` all passing (fixture asserts bound-model metadata)
   2. Docs match the enforced behavior and locked onboarding-hierarchy language
   3. Coverage ≥80%; Lint/vet/fmt clean
   4. DoD report (Stories 6, 7)
   5. COMMIT residual: `git commit -m "docs(personas): phase 6 DoD"`

### 6.LAST [x] **Phase 6 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 6 (fixture runner, `docs/personas-authoring.md`, `docs/personas-install.md`, `README.md`, + the TD-010 guardrail fix in `internal/personas/unit.go`, `internal/registry/persona.go`).
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Phase 6 gate review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (enforcement matches documented convention?), CONFIG SURFACE (docs cite real flags/names/tiers?), INTEGRATION (fixture assertion doesn't break built-in path?), PHASE-EXIT CONTRACT (nothing left for Phase 7 but validation?), REGRESSION (all prior tests intact?). Severity rubric; "ONLY the findings table."

   **Gate findings (no CRITICAL/HIGH/MEDIUM):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | personas-authoring.md §3 | "What the test does" step 1 named only `personas/testdata/` (built-in); community fixtures load from `personas/community/testdata/`. | **FIXED inline** — step 1 now names both locations. |
   | LOW | persona.go validatePersonaTemplateNode | Comment said only `{{if .ToolsEnabled}}` permitted; the validator accepts `{{if <any allowlisted field>}}` (harmless — all are safe scalars). | **FIXED inline** — comment reworded to "an `{{if <allowed-field>}}` block". |

   Gate subagent ran `go test ./...` (all ok) + `go vet ./...` (clean); threw 17 malicious/edge prompts at the weakened guardrail (`{{range}}`/`{{template}}`/`{{define}}`/`{{block}}`/`{{.Secret}}`/`{{.Payload.X}}`/unbalanced-if/half-open/`{{with}}`/`{{printf}}`/pipe/`{{$x:=}}`/`{{call}}`/`{{.}}`/`{{(.Payload).X}}`/`{{index}}`) — ALL rejected; all 10 community prompts pass; built-in `isBuiltin` path byte-for-byte unchanged (`test bruce` → "No fixture defined"); config surface (flags/names/schema/caveats) verified real; built `atcr` binary → `test delia`/`sonny` PASS; Phase 7 has only validation/integration proof left.

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **Outcome:** No CRITICAL/HIGH/MEDIUM — **Phase gate passed.** Two LOW accuracy nits fixed inline (cheaper than deferring). Affected packages re-verified green.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 7 / Final Phase: Integration & Validation (1 day)

> Full-suite pass and cross-story integration proof. This is the validation phase — no new feature work.

### 7.1 [x] **Cross-phase integration: custom prompts resolve via ResolvePersona**
   **Task:** Confirm Story 4's authored personas' custom prompts actually resolve via Story 1/3's `ResolvePersona` chain (not just per-story unit tests). Drive the delivery end-to-end for at least one frontier and one flat-rate persona.
   1. Install an authored persona from the mock registry.
   2. Resolve it via `ResolvePersona`; assert the winning source and the resolved custom prompt text.
   3. Assert guardrails still hold (length cap, fixture gate).
   **Success Criteria:** Authored custom prompts resolve deterministically through the single chain; guardrails enforced.
   **Files:** `internal/registry/persona_test.go` (integration) | **Duration:** ~2h

### 7.2 [x] **AC6 end-to-end discover-by-model flow (mock registry)**
   **Task:** Drive the full "I have model X → find and install its persona" flow against `httptest.NewServer` + `ATCR_PERSONAS_URL`: `search --model <X>` → `install` → `list` → `test` (fixture).
   1. Exercise search → install → list → test for a representative model.
   2. Assert each step succeeds and the persona is discoverable strictly by structured model data.
   **Success Criteria:** AC6 flow passes end-to-end against the mock registry (live `samestrin/atcr` deferred until the repo is public).
   **Files:** `internal/personas/*_test.go` / `cmd/atcr/*_test.go` | **Duration:** ~2h

### Validation Checklist
- [x] All tests passing (T3): `go test ./...`
- [x] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥80% (personas 83.5%, registry 91.8%)
- [x] Lint/format clean: `golangci-lint run` (0 issues), `go vet ./...`, `go fmt ./...`
- [x] Build succeeds: `go build ./...`
- [x] Zero live network calls in CI (all fetch tests use `httptest.NewServer` via the `testServer` helper)

### Optional: Targeted Mutation Testing
MUTATION_TOOL = **UNAVAILABLE** (no Go mutation tool detected: no `stryker-mutator`/`mutmut`/`cargo-mutants`). Skip. If a Go mutation tool (e.g. `go-mutesting`/`gremlins`) is installed later, target ONLY high-risk changed files (`internal/registry/persona.go`, `internal/personas/search.go`) — never the full codebase.
**WARNING:** Do NOT run full codebase mutation — it can take hours. Target specific files.

### Drift Analysis
Compare the delivered sprint against [plan/original-requirements.md](plan/original-requirements.md) — verify each AC (AC1-AC8) is satisfied and no scope was added beyond the original request, and that Clarifications C1/C2/C3 hold (custom prompts resolve; one unit + one resolution chain; untrusted-input guardrails).
- [x] AC1-AC8 traced to delivered work
- [x] C1/C2/C3 honored
- [x] No out-of-scope work introduced (DashScope quickstart wiring, separate org repo, OAuth, PII redaction, marketplace UI, registry.yaml mapping — all remain out of scope)

### 7.LAST [x] **Final GATE: Sprint Exit Review (subagent)**
   **Scope:** Full sprint diff.
   **Spawn a fresh subagent** (subagent_type `general-purpose`, description `Sprint exit review`). Checklist verbatim (hostile integrator): CONTRACT EXIT (all 8 ACs delivered?), CONFIG SURFACE (all new config/flags documented + back-compat?), INTEGRATION (cross-story flow proven?), PHASE-EXIT CONTRACT (ready for `/execute-code-review`?), REGRESSION (no earlier-phase behavior broken?). Severity rubric; "ONLY the findings table."

   **Subagent findings (fresh-context hostile integrator, 2026-07-08):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | HIGH | cmd/atcr/init.go:47, quickstart.go:102 | init/quickstart fetch-and-pin roster (`builtins.Names()`) is disjoint from the shipped `personas/community/index.json` (10 model-indexed personas) → online init/quickstart pin ZERO community personas + 9 skip warnings; AC 01-02 fetch-and-pin delivers no value in shipped config. | **Verified accurate.** Not reachable pre-public-launch (repo private → URL 404s → clean `--offline` hard-fail; embedded built-in panel still works). Correct fix reconciles AC1 vs AC5 vs C2 — a product decision, out of Phase-7 validation scope. **Maintainer-adjudicated → deferred to Epic 19.7 (TD-011).** |
   | MEDIUM | internal/personas/install.go:32 | `Install` now strict-decodes via `ValidateCommunityPersonaYAML`; `InstallBundle` routes members through `Install` → bundle members strict-decoded with no bundle-specific test (back-compat narrowing). | **Verified accurate.** Shipped bundles carry only recognized keys (still install); MEDIUM. **Deferred → TD-012** (pairs with TD-006). |
   | LOW | internal/personas/e2e_discover_test.go:80 | 7.2 fixture step runs the EMBEDDED `delia` (via `CommunityGet`), not the on-disk installed unit — "e2e" holds only because embedded == served. | **Verified accurate** (documented in the test comment; on-disk fixture support is the reserved `PersonasDir` seam). **Deferred → TD-013.** |

   **Independently confirmed by the reviewer (run live):** `go test ./...` passes, `go vet ./...` clean, `go build ./...` succeeds, and no test makes a live network call (all fetch tests use `httptest.NewServer`).

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before sprint exit, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Sprint gate passed" — ready for /execute-code-review
   **Duration:** 15-30 min

   **Outcome:** No CRITICAL. 1 HIGH — verified, unreachable pre-public-launch, and its correct fix is a product decision reconciling AC1/AC5/C2; **maintainer-adjudicated at the gate to defer to Epic 19.7 (TD-011)** rather than expand Phase-7 validation scope. 1 MEDIUM (TD-012) + 1 LOW (TD-013) appended to `tech-debt-captured.md` per protocol. Full suite green, zero live network. **Gate passed with the HIGH formally deferred — ready for /execute-code-review** (which re-reviews the full diff and will re-surface TD-011 for public-launch sequencing).
