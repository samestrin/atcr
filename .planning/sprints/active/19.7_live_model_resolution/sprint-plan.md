# Sprint 19.7: Live Model Resolution

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.7 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — after each phase's DoD and phase-boundary gate, `/execute-sprint` stops for review before the next phase.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-07-08)

**Key Decisions:**
- API key source: reuse the existing `LLM_OPENROUTER_API_KEY` env var, read inline at call time (`Authorization: Bearer $LLM_OPENROUTER_API_KEY`) for both the task 1.1 completion call and the task 1.2 `/api/v1/models` re-list. Never export, print, commit, or echo the value (Phase 1 API-key handling rule).
- Network egress to `https://openrouter.ai` confirmed permitted (unauthenticated `GET /api/v1/models` → HTTP 200).

**Scope Boundaries:**
- Phase 1 remains a manual design spike — findings recorded in `plan/documentation/openrouter-catalog-api.md`; no shipped resolver code.
- Existing docs-cited evidence in that file is retained; the spike ADDS an independently-confirmed live-call outcome so Stories 2/3 cite a real result, not just the quickstart docs.

**Technical Approach:**
- Task 1.1: one authenticated `POST` to `/api/v1/chat/completions` with `"model": "~openai/gpt-latest"` + minimal message; record status, resolved model, and verbatim outcome.
- Task 1.2: one authenticated `GET /api/v1/models`; scan `id`/`canonical_slug` for preview/beta/exp substrings actually present; confirm `z-ai/`, `deepseek/`, `qwen/` prefixes; record the `@stable` exclusion list + `expiration_date` rule.

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A live, auto-updating model-resolution layer over the persona `model` bindings Epic 19.6 shipped. A persona binds to a model *family/channel* (e.g. `anthropic/claude-opus@stable`); atcr resolves that to a concrete slug recorded in a **lock**; reviews always run the locked slug. The model changes only on an explicit `atcr personas upgrade` — never silently mid-review. A new `atcr models check` command reports drift, deprecation, and missing-slug conditions.

### Why This Matters

19.6's frozen slugs must be hand-edited on every vendor release across 10 personas, and a sunset model silently 404s a persona at review time with no warning. This sprint rides each vendor family's capability curve automatically while keeping code reviews reproducible by default.

### Key Deliverables

- Two-layer binding: logical family/channel binding + resolved concrete-slug lock (19.6's pinned `model` seeds the first lock — zero migration).
- Hybrid resolver: alias-bind (7 alias-covered personas), `created`-timestamp newest-in-vendor-prefix (DeepSeek/Qwen/GLM via `z-ai/`), explicit-pin escape hatch; `@stable`/`@latest` channels.
- Reproducible `atcr personas upgrade` with before→after lock reporting; resolution isolated to the upgrade path (zero endpoint calls on the review path).
- `atcr models check [--json]` drift report with a 0/1/2 exit-code contract.
- Major-bump re-validation gate (fixture must re-pass + human "verify" flag); minor jumps auto-lock.
- init/quickstart roster reconciliation (closes 19.6's deferred TD-011 HIGH).
- Checked-in catalog snapshot fixture + `atcr models refresh` command; zero live network in CI; updated docs.

### Success Criteria

- A persona resolves its family/channel binding to a locked slug; reviews consume the lock with no endpoint call (AC2).
- `atcr personas upgrade` advances the lock and reports before→after per persona; no silent runtime model change (AC4).
- `atcr models check [--json]` reports drift/deprecation/missing with the exit-code contract (AC5).
- A major jump gates on fixture re-pass + verify flag; a minor jump auto-locks (AC6).
- Online `init`/`quickstart` deliver a working, non-noisy community persona set (AC7).
- `go test ./...` passes with all resolver/catalog tests backed by a checked-in snapshot (zero live network); docs updated (AC8).

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Strict 🔒 for all stories (derived from complexity 10/12 — VERY COMPLEX).

Every code element follows RED → GREEN → 🎯 ADVERSARIAL → REFACTOR:
1. **RED** — write comprehensive failing tests, verify they fail correctly.
2. **GREEN** — minimal code, one test at a time (T1), verify all pass (T2), COMMIT.
3. **🎯 ADVERSARIAL** — spawn a **fresh subagent** (no memory of the implementation) to review the changed files against the security/edge/error/performance checklist.
4. **REFACTOR** — fix CRITICAL/HIGH findings inline, improve quality, maintain green (T3), COMMIT.

**Adversarial inline-fix bar:** CRITICAL/HIGH fixed inline in REFACTOR; MEDIUM/LOW deferred to `clarifications/tech-debt-captured.md`.

**Gated execution:** each phase ends with a DoD task and a Phase-Boundary Gate (subagent integration review). `/execute-sprint` stops at each phase boundary.

**Phase 1 exception:** Phase 1 is a design spike (manual, no shipped code) — it produces findings written into `plan/documentation/openrouter-catalog-api.md`, not RED/GREEN tests. This mirrors 19.6 Phase 1's precedent.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/](plan/documentation/) | OpenRouter API, existing patterns, fixture discipline, command design, semver |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command |
|------|------|---------|
| T1: Focused | After each small change | `go test ./internal/personas/ -run <TestName>` |
| T2: Module | After completing an element | `go test ./internal/personas/...` / `go test ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

**Coverage:** `go test -coverprofile=coverage.out ./...` (≥80% baseline). **Zero live network in CI** — all resolver/catalog tests use `httptest.NewServer` + the checked-in `internal/personas/testdata/catalog_snapshot.json`.

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80%
3. Lint: `golangci-lint run` clean; `go vet ./...` clean; `gofmt`/`goimports` applied
4. Build: `go build ./...` succeeds
5. Docs: Updated where behavior changed

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

Conventional Commits (`feat`, `fix`, `docs`, `refactor`, `test`, `chore`); scope = package (e.g. `personas`, `models`, `registry`).

---

## Development Standards

Full standards: [coding-standards.md](../../../specifications/coding-standards.md), [implementation-standards.md](../../../specifications/implementation-standards.md), [git-strategy.md](../../../specifications/git-strategy.md).

**Key rules for this sprint (Go):**
- **Error handling:** return `error` last; never ignore; wrap with `fmt.Errorf("doing action: %w", err)`. Resolver failures produce a clear, actionable CLI error and abort — never a silent fallback to a stale/wrong model.
- **Additive schema:** new binding field is `omitempty` with permissive decode, per 19.6 convention — do NOT alter or bypass the 19.6 AC7 `Provider`/`Model` exact-match gate.
- **Boundary rule:** `internal/registry.ResolvePersona` (`persona.go:47`) resolves prompt text and is strictly downstream — the resolver is a separate upstream concern; do not touch `ResolvePersona`.
- **Injection seam:** the catalog client talks to OpenRouter only through `internal/personas.HTTPClient` (the same seam `client.go`'s `fetch()` uses) so it is swappable for `httptest.NewServer` in every test. Reuse `fetch()`'s body-size cap + timeout + backoff unchanged.
- **Input validation:** catalog-derived slugs are validated as plain, printable identifiers before being written to the lock or `AgentConfig.Model` (mirrors 19.6's TD-008 control-char sanitization).
- **Testing:** table-driven `*_test.go` colocated in the package under test; `testify/assert`/`require`; integration tests behind `//go:build integration` where applicable.
- **Formatting/Lint:** `gofmt`/`goimports`, `golangci-lint run`, `go vet ./...` before every commit.

---

## External Resources

- **[CRITICAL]** [openrouter-catalog-api.md](plan/documentation/openrouter-catalog-api.md) — model schema (`id`, `canonical_slug`, `created`, `expiration_date`), missing stability flag, `~`-alias behavior (AC1/AC2/AC3/AC5).
- **[CRITICAL]** [existing-resolver-patterns.md](plan/documentation/existing-resolver-patterns.md) — `fetch()` retry template, `Upgrade()`/`isNewer()` extension seam, additive-schema convention, `ResolvePersona` boundary, command-registration points, AC7 two-call-site drift risk.
- **[IMPORTANT]** [catalog-snapshot-fixture.md](plan/documentation/catalog-snapshot-fixture.md) — checked-in `testdata/catalog_snapshot.json`, zero-live-network `httptest` discipline, refresh command (AC8).
- **[IMPORTANT]** [models-check-command.md](plan/documentation/models-check-command.md) — drift/deprecation/missing reporting, exit codes, `--json` shape, registration next to `personas` (AC5).
- **[IMPORTANT]** [semver-version-comparison.md](plan/documentation/semver-version-comparison.md) — `golang.org/x/mod/semver`, existing `isNewer`, AC6 major/minor gate via `semver.Major` (AC6).

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike (1 day)

**Story:** [01: Catalog Routability Spike & Stable-Channel Heuristic](plan/user-stories/01-catalog-routability-spike-stable-channel-heuristic.md)
**Focus:** Design spike only — no shipped resolver code. Record findings in `plan/documentation/openrouter-catalog-api.md` before Phase 3 begins.

> ⚠️ **API key handling:** treat `OPENROUTER_API_KEY` exactly as 19.6's `quickstart.go` treats Synthetic keys — never print it, never commit it, never echo it to logs/terminal history. Record only the outcome (routable: yes/no), never the raw request/response.

### 1.1 [x] **[Alias routability spike — MANUAL](plan/user-stories/01-catalog-routability-spike-stable-channel-heuristic.md)**
   **AC:** [01-01](plan/acceptance-criteria/01-01-latest-alias-routability-confirmed.md)
   1. Make ONE authenticated completion call against a `~…-latest` alias (e.g. `~anthropic/claude-opus-latest`) to confirm real server-side routability.
   2. Record the finding (routable: yes/no) in `plan/documentation/openrouter-catalog-api.md`.
   3. If NOT routable: note the fallback path (hybrid resolver uses `created`-timestamp/explicit-pin for affected personas — no epic-blocking failure per `HAS_GATED_WORK: false`).
   **Files:** `plan/documentation/openrouter-catalog-api.md` | **Duration:** 2-3h

### 1.2 [x] **[@stable heuristic & z-ai prefix — MANUAL](plan/user-stories/01-catalog-routability-spike-stable-channel-heuristic.md)**
   **AC:** [01-02](plan/acceptance-criteria/01-02-stable-channel-heuristic-z-ai-prefix.md)
   1. Against the live schema, enumerate preview/beta/exp token patterns; define the `@stable` exclusion heuristic (exclude preview/beta/exp tokens + honor non-null `expiration_date`).
   2. Pin the GLM vendor prefix as `z-ai/` (NOT `glm/`) for glenna; confirm `delia → deepseek/`, `quinn → qwen/`.
   3. Record the heuristic + confirmed prefixes in `plan/documentation/openrouter-catalog-api.md`.
   **Files:** `plan/documentation/openrouter-catalog-api.md` | **Duration:** 2-3h

### 1.3 [x] **Phase 1 — DoD**
   - [x] Both spike findings recorded in `plan/documentation/openrouter-catalog-api.md`
   - [x] `@stable` heuristic defined against live schema; `z-ai/` prefix pinned
   - [x] No API key printed/committed
   - [x] COMMIT: `git add plan/documentation/openrouter-catalog-api.md && git commit -m "docs(personas): record catalog routability spike + @stable heuristic"`

### 1.4 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**
   **Scope:** The recorded spike findings (informs Phase 3 resolver design).

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - File: `plan/documentation/openrouter-catalog-api.md` (absolute path)
     - Checklist (hostile-integrator perspective):
       - CONTRACT EXIT: Is the routability finding unambiguous enough for Phase 3's alias-bind design to depend on it?
       - CONFIG SURFACE: Is the `@stable` heuristic fully specified (which tokens excluded, how `expiration_date` is honored)?
       - INTEGRATION: Is the `z-ai/` prefix decision recorded so Phase 3 never assumes a `glm/` namespace?
       - REGRESSION: Any 19.6 assumption contradicted?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below, no prose

   **Subagent findings — initial gate (2 HIGH + 1 MEDIUM), all resolved before boundary:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | openrouter-catalog-api.md (created-timestamp) | Resolver output validated only for glenna, not delia/quinn; quinn's `qwen3-coder-plus` pin ≠ newest `qwen/` member (`qwen3.7-plus`) | FIXED — added per-persona strategy table; validated newest-non-expiring per prefix; quinn reclassified to explicit-pin (specialized coder variant) |
   | HIGH | openrouter-catalog-api.md (alias-bind) | No explicit persona→alias mapping; celeste's `kimi-k2.7-code` pin generalized by `~moonshotai/kimi-latest` | FIXED — added 10-row persona→alias mapping; celeste reclassified to explicit-pin. Final split: 6 alias-bind + 2 created-timestamp + 2 explicit-pin |
   | MEDIUM | openrouter-catalog-api.md (@stable matching) | Segment-match rule doesn't strip `:variant` suffix/vendor prefix before tokenizing (`...-preview:free` escapes exclusion) | Captured → tech-debt-captured.md TD-001 (Phase 3) |

   **Subagent findings — re-run gate (0 CRITICAL/HIGH → PASS; 1 MEDIUM + 2 LOW):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | openrouter-catalog-api.md (created-timestamp) | Resolver defined as "newest non-expiring" — didn't state it applies the `@stable` preview-token exclusion; a future newest `-exp` deepseek member could float delia | FIXED — clarified resolver applies the active channel filter before max-`created`; verified `deepseek-v4-pro` (1777000679) > `deepseek-v3.2-exp` (1759150481) so delia is safe today |
   | LOW | openrouter-catalog-api.md (expiration_date) | Any-non-null exclusion treats far-future sentinel `z-ai/glm-5v-turbo` (2098-12-31) as deprecated (channel-only; no pin affected) | Captured → tech-debt-captured.md TD-002 (Phase 3 deprecation-horizon decision) |
   | LOW | openrouter-catalog-api.md (Task 1.2 block) | Task 1.2 vendor-prefix block implied created-timestamp applies to quinn; per-persona table reclassifies to explicit-pin | FIXED — added forward reference in Task 1.2 `qwen/` line |

   **Action Taken:** 2 HIGH fixed inline + gate re-run (0 CRITICAL/HIGH). MEDIUM/LOW either fixed inline or captured to `tech-debt-captured.md` (TD-001, TD-002). ✅ **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 1 complete. Await review before Phase 2.

---

## Phase 2: Foundation — Family/Channel Binding & Lock Schema (2 days)

**Story:** [02: Family/Channel Binding & Resolved Lock](plan/user-stories/02-family-channel-binding-resolved-lock.md)
**Focus:** Extend `PersonaIndexEntry`/`AgentConfig` additively with the family/channel binding field; confirm the existing `Model` field is the lock consumed at review time (no new field, zero migration); verify 19.6's AC7 exact-match gate is unaffected.

### 2.1 [x] **[Family/channel binding schema extension — RED](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **AC:** [02-01](plan/acceptance-criteria/02-01-family-channel-binding-schema-extension.md)
   Write comprehensive failing tests: binding field decodes (`omitempty`, permissive), absent field is backward-compatible, 19.6 AC7 `Provider`/`Model` parity gate still passes. Verify fail correctly.
   **Files:** `internal/personas/search_test.go` (+ `internal/registry/config_test.go`) | **Duration:** 3h

### 2.2 [x] **[Family/channel binding schema extension — GREEN](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   Add the additive `Binding` field to `PersonaIndexEntry`/`AgentConfig` (`omitempty`, permissive decode). Minimal code, one test at a time (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): add additive family/channel binding field (green)"`
   **Files:** `internal/personas/search.go`, `internal/registry/config.go` | **Duration:** 3h

### 2.2.A [x] **[Family/channel binding schema extension — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **Changed Files:** `internal/personas/search.go`, `internal/registry/config.go`, `internal/personas/search_test.go`

   **Spawn a fresh subagent** via the Agent tool. The subagent has no memory of the implementation — intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
     - Checklist (verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (unvalidated binding string reaching a slug?)
       - EDGE CASES: Null, empty, malformed binding; absent field; concurrent decode?
       - ERROR HANDLING: Missing catches, swallowed decode errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - REGRESSION: Does the new field weaken/bypass 19.6's AC7 `Provider`/`Model` parity gate?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below, no prose

   **Subagent findings (fresh-context general-purpose subagent):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No findings — `.Binding` has zero reads on any non-test path; AC7 `Provider`/`Model` gate untouched; `KnownFields(true)` widened by exactly the one `binding` key (proven by `TestValidateCommunityPersonaYAML_BindingDoesNotWidenGate`) | - |

   **Action Taken:** ✅ Adversarial review passed — no CRITICAL/HIGH/MEDIUM/LOW findings. Proceeding.

### 2.3 [x] **[Family/channel binding schema extension — REFACTOR](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any) — **none** (adversarial review passed with zero findings)
   2. Improve quality, maintain green (T1), validate (T3) — ✅ `go build ./...` + `go test ./...` full suite green
   3. COMMIT: no refactor commit — the GREEN implementation is already minimal (additive `omitempty` field + doc comment); no code change to make. No empty/no-op commit created.
   **Duration:** 2h

### 2.4 [x] **[Review path reads locked slug, zero endpoint calls — RED](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **AC:** [02-02](plan/acceptance-criteria/02-02-review-path-reads-locked-slug-zero-endpoint-calls.md)
   Write failing tests proving the review path consumes `AgentConfig.Model` (the lock) and makes ZERO catalog/endpoint calls (assert against an injected `HTTPClient` that fails on any call). **High-complexity AC — most adversarial test design.** Verify fail correctly.
   **Files:** `internal/fanout/lock_test.go` (new — placed alongside `engine_test.go` per AC 02-02 guidance) | **Duration:** 3h
   **Note — vacuous RED (transparent):** AC 02-02 is a *verify/regression-lock* AC — its own Related Files section labels `renderAgent`/Invocation construction as "reference/verify, no behavioral change expected." The review path already reads `ac.Model` and never `Binding` (review.go:1057 primary, :1144 fallback), so the 3 regression tests (`TestReviewPath_InvocationModelIsLockNotBinding`, `TestReviewPath_FallbackModelIsLockNotBinding`, `TestReviewPath_ZeroCatalogEndpointToResolveModel`) PASS on first run. There is no pre-existing behavioral gap to drive; manufacturing a fake failure (e.g. asserting `Model == Binding`) would be dishonest. The tests' value is the build-time guard they now provide: any future change that consumes `Binding` on the review path or adds a live resolution call fails them. Distinct sentinels (`model-greta` vs `binding-greta`) make any leak obvious. End-to-end test records every outbound request and asserts model==lock, no `/models`/`catalog` path.

### 2.5 [x] **[Review path reads locked slug, zero endpoint calls — GREEN](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   Confirm/wire the review path to read the locked `Model` field only. Minimal code (T1), verify all pass (T2), COMMIT: `git commit -m "feat(registry): confirm review path reads locked slug, no endpoint call (green)"`
   **Files:** `internal/registry/config.go`, `internal/fanout/review.go` (read-only verification) | **Duration:** 2h

### 2.5.A [x] **[Review path reads locked slug — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **Changed Files:** `internal/registry/config.go`, `internal/fanout/review.go`, `internal/registry/config_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief with the files above and the verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), plus: "Prove no code path on the review hot path can trigger a catalog fetch." Severity rubric CRITICAL/HIGH/MEDIUM/LOW. Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | internal/fanout/lock_test.go:149-150 | The "no catalog fetch" guard rejected paths by substring blocklist (`/models`, `catalog`); a differently-named resolution endpoint (`/resolve`, `/v1/registry`) could evade it. Binding-leak (Model-sentinel) assertion is robust; only the endpoint-shape check was partial. | FIXED inline in 2.6 — replaced blocklist with a positive allowlist: every recorded request path must equal the known completion endpoint `/chat/completions`, so ANY unexpected request fails the test. (Fixed inline rather than deferred: it strengthens the epic's load-bearing reproducibility guard and is freshly-authored test code — cleaning up own work.) |

   **Action Taken:** No CRITICAL/HIGH. One LOW strengthened inline in 2.6 (positive-assertion allowlist on the completion endpoint). Production code unchanged — the review path was already correct. Proceeding.

### 2.6 [x] **[Review path reads locked slug — REFACTOR](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any) — none; strengthened the one LOW endpoint-guard inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ green
   3. COMMIT: `git commit -m "refactor(registry): clean up locked-slug review path"`
   **Duration:** 2h

### 2.7 [x] **[Pinned model seeds initial lock, zero migration — RED](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **AC:** [02-03](plan/acceptance-criteria/02-03-pinned-model-seeds-initial-lock-zero-migration.md)
   Write failing tests: an existing 19.6 on-disk persona (pinned `Model`, no `Binding`) is treated as its own initial lock with zero migration; AC7 parity unaffected. Verify fail correctly.
   **Files:** `internal/personas/community_schema_test.go` (added `TestVerifyCommunityIndex_BindingExempt`, `TestPinnedModelIsLockZeroMigration`) | **Duration:** 2h
   **Note — vacuous RED (transparent):** Like AC 02-02, 02-03 is a verify/exemption/zero-migration AC. 19.6's pinned `Model` already IS the lock (no new field, no transform) and `verifyCommunityIndex` (the AC7 gate) already enumerates Provider/Model only — so both tests PASS on first run. No behavioral gap exists to drive; the tests are permanent guards: `TestVerifyCommunityIndex_BindingExempt` fails the build if anyone makes the gate enumerate Binding; `TestPinnedModelIsLockZeroMigration` fails if any of the 10 personas' pinned model lock goes empty. Zero migration is satisfied by NOT touching any persona YAML (byte-for-byte unchanged — verified: no persona YAML edited this story).

### 2.8 [x] **[Pinned model seeds initial lock — GREEN](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   Minimal code so a pinned `Model` seeds the lock. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): pinned model seeds initial lock, zero migration (green)"`
   **Files:** `internal/personas/search.go` | **Duration:** 2h

### 2.8.A [x] **[Pinned model seeds initial lock — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   **Changed Files:** `internal/personas/search.go`, `internal/personas/search_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.8`
   - prompt: Files above + verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), plus: "Confirm zero migration — no existing on-disk persona is rewritten or invalidated." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent):** Zero migration confirmed — no persona YAML/index.json modified by the branch; `BindingExempt` test genuinely discriminating.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | community_schema_test.go:169 | `TestPinnedModelIsLockZeroMigration` docstring claimed it proves "a persona shipping no binding decodes Binding as ''" but the loop only asserted `NotEmpty(ac.Model)` — the binding-decode guarantee was unverified, making it a weak near-duplicate of `TestCommunityPersonas_NoPlaceholderModel`. | FIXED inline in 2.9 — added a per-persona assertion: when the raw YAML carries no `binding:` key, `ac.Binding` must decode as "" (Binding inertness), matching the docstring and differentiating the test. Fixed inline (freshly-authored test, own cleanup). |

   **Action Taken:** No CRITICAL/HIGH. One LOW strengthened inline in 2.9 (added the missing Binding-inertness assertion). Proceeding.

### 2.9 [x] **[Pinned model seeds initial lock — REFACTOR](plan/user-stories/02-family-channel-binding-resolved-lock.md)**
   1. Fix CRITICAL/HIGH issues from 2.8.A (if any) — none; strengthened the one LOW test-rigor gap inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ green
   3. COMMIT: `git commit -m "refactor(personas): clean up initial-lock seeding"`
   **Duration:** 1h

### 2.10 [x] **Phase 2 — DoD**
   - [x] All Phase 2 tests passing (T3: `go test ./...`) — full suite exit 0
   - [x] Coverage ≥80% for changed packages — personas 84.7%, registry 91.8%, fanout 87.8%
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run` 0 issues, `go vet` clean, `gofmt -l` empty, `go build ./...` exit 0
   - [x] 19.6 AC7 `Provider`/`Model` parity gate still green — `TestCommunityIndex_ProviderModelMatchesYAML`/`TestVerifyCommunityIndex_FailsOnMismatch`/`TestCommunityIndex_Registration` pass unchanged
   - [x] DoD report per template

   ```
   Story-02 DoD Complete
   Auto: 3/3 (tests passing, no lint errors, build succeeds)
   Story-Specific (AC 02-01/02/03): 4/4 + 4/4 + 4/4
     02-01: Binding field added (json+yaml, omitempty); old-shape decodes Binding=""; KnownFields(true) accepts binding, still rejects unknown; writePersonaUnit untouched (byte-for-byte persist)
     02-02: Invocation.Model derives solely from AgentConfig.Model (regression test); zero outbound calls beyond LLM completion (positive-allowlist end-to-end); ResolvePersona untouched; fallback chain confirmed unaffected
     02-03: every persona model unchanged byte-for-byte (git diff empty); AC7 gate assertions pass unmodified; Binding drift/absence exempt from gate (explicit test); Binding inertness asserted
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 2.11 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (integration-level).

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief with Phase 2 changed files (absolute paths) + verbatim hostile-integrator checklist (CONTRACT EXIT / CONFIG SURFACE / INTEGRATION / PHASE-EXIT CONTRACT / REGRESSION). Emphasize: additive schema is back-compat; the `Model` field IS the lock consumed downstream; AC7 gate intact. Output: ONLY the findings table.

   **Subagent findings (fresh-context hostile-integrator subagent):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | No findings — additive Binding is back-compat everywhere (index json has no strict decode; community YAML `KnownFields(true)` auto-recognizes the inlined field); zero production reads of `.Binding` (verified by grep); `renderAgent`/`buildFallbackAgent` consume `ac.Model` exclusively; AC7 Provider/Model gate + strict `KnownFields(true)` gate intact and unweakened; nothing normalizes/validates Binding, so Phase 3's resolver design is not pre-empted | - |

   **Action Taken:** ✅ Phase gate passed — 0 CRITICAL/HIGH/MEDIUM/LOW. No re-run needed. No tech-debt captured this phase (both adversarial LOWs from 2.5.A/2.8.A were strengthened inline, none deferred).
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 2 complete. Await review before Phase 3.

---

## Phase 3: Core Resolution — Hybrid Resolver (3.5 days)

**Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)
**Focus:** The heaviest phase. Build `internal/personas/catalog.go` (OpenRouter catalog client mirroring `client.go`'s `fetch()`/`HTTPClient` seam) + the three independently-testable resolver strategies + `@stable`/`@latest` channel logic. Write RED tests per strategy, not one monolithic test.

> All catalog access goes through the injected `HTTPClient`; every test uses `httptest.NewServer` + fixture data. Reuse `fetch()`'s body-size cap/timeout/backoff. Validate slugs as plain printable identifiers before returning them as a lock.

### 3.1 [ ] **[Alias passthrough (7 personas) — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-alias-passthrough-seven-personas.md)
   Write failing tests: the 7 alias-covered personas bind to provider `-latest` aliases and pass through unchanged. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 3h

### 3.2 [ ] **[Alias passthrough — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement alias-bind path + catalog client scaffolding. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): alias-bind resolver for 7 personas (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 4h

### 3.2.A [ ] **[Alias passthrough — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Files above + verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), plus: "Verify catalog client reuses fetch()'s body-size cap + timeout; verify slug validation before return." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.3 [ ] **[Alias passthrough — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up alias-bind path"`
   **Duration:** 2h

### 3.4 [ ] **[Created-timestamp vendor-prefix scan — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-02](plan/acceptance-criteria/03-02-created-timestamp-vendor-prefix-scan.md)
   Write failing tests: newest-in-vendor-prefix resolver for `deepseek/`, `qwen/`, `z-ai/` (glenna); missing `created` = ineligible; **no `glm/` namespace assumption anywhere**. **High-complexity AC.** Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 4h

### 3.5 [ ] **[Created-timestamp vendor-prefix scan — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `created`-timestamp newest-in-prefix resolver. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): created-timestamp vendor-prefix resolver (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 4h

### 3.5.A [ ] **[Created-timestamp scan — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — null/missing `created`, ties, `z-ai/` vs `glm/` prefix; SECURITY — untrusted slug/timestamp fields." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.6 [ ] **[Created-timestamp scan — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up created-timestamp resolver"`
   **Duration:** 2h

### 3.7 [ ] **[Explicit-pin never floats — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-03](plan/acceptance-criteria/03-03-explicit-pin-never-floats.md)
   Write failing tests: an explicit-slug pin resolves to itself verbatim and NEVER floats to a newer member. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 3.8 [ ] **[Explicit-pin never floats — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement explicit-pin escape hatch. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): explicit-pin escape hatch never floats (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 2h

### 3.8.A [ ] **[Explicit-pin — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm an explicit pin can never be silently advanced by any strategy." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.9, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.9 [ ] **[Explicit-pin — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.8.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up explicit-pin path"`
   **Duration:** 1h

### 3.10 [ ] **[@stable excludes preview & expiring — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-04](plan/acceptance-criteria/03-04-stable-channel-excludes-preview-and-expiring.md)
   Write failing tests for `@stable`: excludes preview/beta/exp tokens AND models with non-null `expiration_date`. **High-complexity AC.** The `@stable`/`expiration_date`/preview interaction is now PINNED in the ACs: `@stable` excludes BOTH preview/beta/exp tokens AND non-null `expiration_date`; the `@latest`×`expiration_date` rule is pinned in AC 03-05 (only the preview-token exclusion is bypassed under `@latest` — deprecation is ALWAYS excluded, failing closed to the next-newest non-expiring entry). Encode these pinned rules directly in the tests. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 3h

### 3.11 [ ] **[@stable excludes preview & expiring — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `@stable` channel logic per the decided semantics. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): @stable channel excludes preview + expiring (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 3h

### 3.11.A [ ] **[@stable channel — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.11`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — a model both preview-tagged AND with non-null `expiration_date`; confirm the documented `@stable`/`@latest` decision is applied consistently." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.12, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.12 [ ] **[@stable channel — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.11.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up @stable channel logic"`
   **Duration:** 2h

### 3.13 [ ] **[@latest includes preview — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-05](plan/acceptance-criteria/03-05-latest-channel-includes-preview.md)
   Write failing tests: `@latest` includes preview-tagged members BUT still excludes non-null `expiration_date` (deprecation), per the rule pinned in AC 03-05. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 3.14 [ ] **[@latest includes preview — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `@latest` channel logic. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): @latest channel includes preview (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 2h

### 3.14.A [ ] **[@latest channel — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.14`
   - prompt: Files above + verbatim checklist, plus: "Confirm `@latest` vs `@stable` boundary is consistent with the 3.10 decision; no strategy cross-contamination." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.15, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.15 [ ] **[@latest channel — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.14.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up @latest channel logic"`
   **Duration:** 1h

### 3.16 [ ] **Phase 3 — DoD**
   - [ ] All three resolver strategies + both channels tested independently (T3)
   - [ ] Coverage ≥80%; zero live network (httptest + fixture)
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] `ResolvePersona` untouched; `@stable`/`@latest` decision recorded
   - [ ] DoD report per template

### 3.17 [ ] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Phase 3 changed files + verbatim hostile-integrator checklist (CONTRACT EXIT / CONFIG SURFACE / INTEGRATION / PHASE-EXIT CONTRACT / REGRESSION). Emphasize: three strategies are independently testable and don't cross-contaminate; catalog client is the ONLY code that talks to the external API; `ResolvePersona` untouched. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 3 complete. Await review before Phase 4.

---

## Phase 4: Upgrade Integration (2 days)

**Story:** [04: Reproducible Upgrade with Before→After Lock Reporting](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)
**Focus:** Wire the Phase 3 resolver into `Upgrade()` immediately before the existing `isNewer`/write logic; extend `atcr personas upgrade` reporting to show before→after resolved slug; prove zero endpoint calls occur outside this explicit path.

### 4.1 [ ] **[Upgrade resolves & advances lock + before→after report — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-01](plan/acceptance-criteria/04-01-upgrade-resolves-advances-lock-slug-report.md)
   Write failing tests: `upgrade` re-resolves, advances the lock, and reports before→after per persona (e.g. `anthony: opus-4.8 → 5.0`). Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go`, `cmd/atcr/personas_test.go` | **Duration:** 3h

### 4.2 [ ] **[Upgrade resolves & advances lock — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Insert resolver call into `Upgrade()` before `isNewer`/write; extend reporting. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): upgrade re-resolves + before→after lock report (green)"`
   **Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go` | **Duration:** 4h

### 4.2.A [ ] **[Upgrade resolves & advances lock — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Files above + verbatim checklist, plus: "SECURITY — resolved slug validated before write to lock; ERROR HANDLING — a failed catalog fetch aborts cleanly (no partial lock advance, no silent stale fallback)." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 4.3 [ ] **[Upgrade resolves & advances lock — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up upgrade resolver integration"`
   **Duration:** 2h

### 4.4 [ ] **[Resolution isolated to upgrade path — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-02](plan/acceptance-criteria/04-02-resolution-isolated-to-upgrade-path.md)
   Write failing tests proving NO endpoint/catalog call occurs on any path except explicit `upgrade`/`models` (inject an `HTTPClient` that fails on unexpected calls; exercise review + other commands). **High-complexity AC — most adversarial test design.** Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 3h

### 4.5 [ ] **[Resolution isolated to upgrade path — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Ensure resolution is invoked ONLY on the explicit upgrade path. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): isolate resolution to upgrade path (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 2h

### 4.5.A [ ] **[Resolution isolation — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.5`
   - prompt: Files above + verbatim checklist, plus: "Prove no hot-path or incidental command triggers a catalog fetch — enumerate every caller of the resolver." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 4.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 4.6 [ ] **[Resolution isolation — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up resolution isolation"`
   **Duration:** 1h

### 4.7 [ ] **[Dry-run reports without writing — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-03](plan/acceptance-criteria/04-03-dry-run-reports-without-writing.md)
   Write failing tests: a dry-run reports the before→after it WOULD apply and writes nothing to disk. Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go`, `cmd/atcr/personas_test.go` | **Duration:** 2h

### 4.8 [ ] **[Dry-run reports without writing — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Implement dry-run reporting path. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): upgrade dry-run reports without writing (green)"`
   **Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go` | **Duration:** 2h

### 4.8.A [ ] **[Dry-run — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm dry-run writes NOTHING (no lock file, no persona YAML mutation) on any branch." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 4.9, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 4.9 [ ] **[Dry-run — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.8.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up dry-run path"`
   **Duration:** 1h

### 4.10 [ ] **Phase 4 — DoD**
   - [ ] All Phase 4 tests passing (T3)
   - [ ] Coverage ≥80%; zero live network
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] No silent runtime model change anywhere; failed fetch aborts cleanly
   - [ ] DoD report per template

### 4.11 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Phase 4 changed files + verbatim hostile-integrator checklist. Emphasize: resolution happens ONLY at upgrade; before→after report is accurate; graceful degradation on fetch failure. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 4 complete. Await review before Phase 5.

---

## Phase 5: Discovery — `atcr models check` (2 days)

**Story:** [05: `atcr models check` Drift Report](plan/user-stories/05-atcr-models-check-drift-report.md)
**Focus:** Net-new `cmd/atcr/models.go` command family; enumerate installed personas' locked slugs (via a `ListTiers`-style pattern); report newer-member/deprecation/missing with `--json` and a 0/1/2 exit-code contract; default to the checked-in snapshot for determinism.

### 5.1 [ ] **[Command registration + human-readable drift report — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-command-registration-human-readable-drift-report.md)
   Write failing tests: `atcr models check` is registered next to `personas`; prints a human-readable drift report (newer member / deprecation / missing). Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 3h

### 5.2 [ ] **[Command registration + drift report — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement `cmd/atcr/models.go` `check` subcommand + registration at `cmd/atcr/main.go`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): register models check + human-readable drift report (green)"`
   **Files:** `cmd/atcr/models.go`, `cmd/atcr/main.go` | **Duration:** 4h

### 5.2.A [ ] **[Command registration + drift report — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/main.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Files above + verbatim checklist, plus: "Confirm `models check` is diagnostic-only (never on the review path); enumeration handles a persona with a missing slug without panicking." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.3 [ ] **[Command registration + drift report — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up check command"`
   **Duration:** 2h

### 5.4 [ ] **[--json machine-readable output — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-02](plan/acceptance-criteria/05-02-json-machine-readable-output.md)
   Write failing tests: `--json` emits a stable machine-readable shape (the seam Epic 19.8 wraps). Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h

### 5.5 [ ] **[--json output — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement `--json`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): --json machine-readable drift output (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 2h

### 5.5.A [ ] **[--json output — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm JSON shape is stable/documented and does not leak secrets; handles empty/no-drift case." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.6 [ ] **[--json output — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up json output"`
   **Duration:** 1h

### 5.7 [ ] **[Exit-code contract (0/1/2) — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-03](plan/acceptance-criteria/05-03-exit-code-contract.md)
   Write failing tests for the 0 (no drift) / 1 (drift) / 2 (error) exit-code contract. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h

### 5.8 [ ] **[Exit-code contract — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement exit codes. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): 0/1/2 exit-code contract (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 1h

### 5.8.A [ ] **[Exit-code contract — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm every code path maps to exactly one of 0/1/2; an internal error never returns 0 or 1." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.9, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.9 [ ] **[Exit-code contract — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.8.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up exit-code handling"`
   **Duration:** 1h

### 5.10 [ ] **[Deterministic catalog-snapshot default — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-04](plan/acceptance-criteria/05-04-deterministic-catalog-snapshot-default.md)
   Write failing tests: `models check` defaults to the checked-in snapshot (zero network) for deterministic output. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h

### 5.11 [ ] **[Snapshot default — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Default to the checked-in snapshot. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): default to checked-in snapshot for determinism (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 2h

### 5.11.A [ ] **[Snapshot default — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.11`
   - prompt: Files above + verbatim checklist, plus: "Confirm the default path makes zero network calls; live-catalog mode (if any) is explicit opt-in." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.12, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.12 [ ] **[Snapshot default — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.11.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up snapshot-default path"`
   **Duration:** 1h

### 5.13 [ ] **Phase 5 — DoD**
   - [ ] All Phase 5 tests passing (T3); exit codes verified
   - [ ] Coverage ≥80%; zero live network (snapshot default)
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] `models check` never invoked on the review path
   - [ ] DoD report per template

### 5.14 [ ] **Phase 5 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Phase 5 changed files + verbatim hostile-integrator checklist. Emphasize: the `--json`/exit-code contract is stable enough for Epic 19.8 to wrap; snapshot-default determinism. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 5 complete. Await review before Phase 6.

---

## Phase 6: Validation Gate — Major-Bump Re-Validation (1 day)

**Story:** [06: Major-Bump Re-Validation Gate](plan/user-stories/06-major-bump-re-validation-gate.md)
**Focus:** Layer `semver.Major(local) != semver.Major(remote)` on top of the existing `isNewer` normalization to classify major vs. minor jumps; gate major jumps on `TemplateFixtureRunner` re-passing + an unconditional human "verify" flag; minor jumps auto-lock unchanged. Reuse `isNewer`'s exact normalization — do NOT fork it.

### 6.1 [ ] **[Major-jump fixture gate + verify flag — RED](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-major-jump-fixture-gate-and-verify-flag.md)
   Write failing tests: a major jump (4.x→5.x) gates on the fixture re-passing AND surfaces an unconditional "prompt tuned for the prior major — verify" flag before the lock advances. Non-semver strings are NOT treated as a major-bump trigger (per `isNewer` precedent). **High-complexity AC.** Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 3h

### 6.2 [ ] **[Major-jump fixture gate + verify flag — GREEN](plan/user-stories/06-major-bump-re-validation-gate.md)**
   Implement `semver.Major` classification + fixture gate + verify flag, reusing `isNewer`'s normalization. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): major-bump re-validation gate + verify flag (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 3h

### 6.2.A [ ] **[Major-jump gate — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 6.2`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — non-semver/`v`-prefix strings; confirm the gate reuses `isNewer`'s exact normalization (no divergent parallel impl); the verify flag is unconditional on a major jump." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 6.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 6.3 [ ] **[Major-jump gate — REFACTOR](plan/user-stories/06-major-bump-re-validation-gate.md)**
   1. Fix CRITICAL/HIGH issues from 6.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up major-bump gate"`
   **Duration:** 1h

### 6.4 [ ] **[Minor-jump auto-lock regression guard — RED](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **AC:** [06-02](plan/acceptance-criteria/06-02-minor-jump-auto-lock-regression-guard.md)
   Write failing tests: a minor jump (4.8→4.9) auto-locks with no verify flag; regression guard on `isNewer` behavior. Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 2h

### 6.5 [ ] **[Minor-jump auto-lock — GREEN](plan/user-stories/06-major-bump-re-validation-gate.md)**
   Implement minor-jump auto-lock. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): minor-jump auto-lock (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 1h

### 6.5.A [ ] **[Minor-jump auto-lock — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 6.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm a minor jump never triggers the verify flag and a major jump never auto-locks — no boundary off-by-one." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 6.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 6.6 [ ] **[Minor-jump auto-lock — REFACTOR](plan/user-stories/06-major-bump-re-validation-gate.md)**
   1. Fix CRITICAL/HIGH issues from 6.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up minor-jump auto-lock"`
   **Duration:** 1h

### 6.7 [ ] **Phase 6 — DoD**
   - [ ] All Phase 6 tests passing (T3); major/minor boundary verified
   - [ ] Coverage ≥80%
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] `isNewer` normalization reused unmodified (no fork)
   - [ ] DoD report per template

### 6.8 [ ] **Phase 6 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 6.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 6 gate review`
   - prompt: Phase 6 changed files + verbatim hostile-integrator checklist. Emphasize: gate composes with Phase 4's upgrade path; `isNewer` reuse; verify flag human-facing. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 6 complete. Await review before Phase 7.

---

## Phase 7: Roster Reconciliation — init/quickstart (1.5 days)

**Story:** [07: init/quickstart Roster Reconciliation](plan/user-stories/07-init-quickstart-roster-reconciliation.md)
**Focus:** Closes 19.6's deferred TD-011 HIGH per the **locked Option B decision** — derive the fetch-and-pin roster from the community index's own fetched entries instead of the hardcoded `builtins.Names()` list, fixed once in a shared location to avoid the TD-006/TD-007 two-call-site drift pattern. Independent of Phases 1-6.

### 7.1 [ ] **[Working non-empty community roster — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-01](plan/acceptance-criteria/07-01-working-nonempty-community-roster.md)
   Write failing tests: online `init`/`quickstart` install a working, non-empty community persona set derived from the fetched index (not `builtins.Names()`). Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 3h

### 7.2 [ ] **[Working non-empty roster — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Derive the roster from the single existing `FetchIndex` call inside `installCommunityPersonas`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(cmd): derive init/quickstart roster from fetched index (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** 3h

### 7.2.A [ ] **[Working non-empty roster — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.2`
   - prompt: Files above + verbatim checklist, plus: "Confirm no additional network round-trip introduced; all-or-nothing rollback / skip-then-continue behavior preserved; no new two-call-site drift (single shared reconciliation point)." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 7.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 7.3 [ ] **[Working non-empty roster — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(cmd): clean up roster derivation"`
   **Duration:** 1h

### 7.4 [ ] **[No misleading skip warnings — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-02](plan/acceptance-criteria/07-02-no-misleading-skip-warnings.md)
   Write failing tests: no `not found in community index — skipping` warnings emitted for the reconciled roster. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 2h

### 7.5 [ ] **[No misleading skip warnings — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Remove the source of the misleading warning under the reconciled roster. (T1), verify all pass (T2), COMMIT: `git commit -m "fix(cmd): drop misleading skip warnings under reconciled roster (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** 1h

### 7.5.A [ ] **[No misleading skip warnings — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, `cmd/atcr/init_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm legitimate skip cases (genuine absence) still warn; only the misleading roster/index-disjoint warning is removed." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 7.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 7.6 [ ] **[No misleading skip warnings — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(cmd): clean up skip-warning handling"`
   **Duration:** 1h

### 7.7 [ ] **[Shared reconciliation point + backward compat — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-03](plan/acceptance-criteria/07-03-shared-reconciliation-point-and-backward-compat.md)
   Write failing tests: the roster derivation lives in ONE shared location (both `init` and `quickstart` call it — no drift); existing on-disk personas remain backward-compatible. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 2h

### 7.8 [ ] **[Shared reconciliation point — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Extract a single shared reconciliation function; wire both call sites to it. (T1), verify all pass (T2), COMMIT: `git commit -m "refactor(cmd): single shared roster-reconciliation point (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` (+ shared helper) | **Duration:** 2h

### 7.8.A [ ] **[Shared reconciliation point — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, shared helper, tests

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm there is exactly ONE reconciliation point (no TD-006/TD-007 two-call-site drift); backward compat with existing on-disk personas." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 7.9, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 7.9 [ ] **[Shared reconciliation point — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.8.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(cmd): finalize shared reconciliation point"`
   **Duration:** 1h

### 7.10 [ ] **Phase 7 — DoD**
   - [ ] All Phase 7 tests passing (T3)
   - [ ] Coverage ≥80%
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] 19.6 TD-011 HIGH closed; single reconciliation point; backward compat
   - [ ] DoD report per template

### 7.11 [ ] **Phase 7 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 7.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 7 gate review`
   - prompt: Phase 7 changed files + verbatim hostile-integrator checklist. Emphasize: single shared reconciliation point; no new network round-trip; rollback/skip behavior preserved; backward compat. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 7 complete. Await review before Phase 8.

---

## Phase 8: Integration & Docs (2 days)

**Story:** [08: Catalog Snapshot Fixture, Refresh Command & Documentation](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)
**Focus:** Depends on Phases 2, 3, 5. Author the checked-in catalog snapshot fixture covering every resolver branch; build `atcr models refresh` (maintainer-initiated, never CI-invoked); update `docs/personas-authoring.md`/`docs/personas-install.md`.

### 8.1 [ ] **[Checked-in catalog snapshot coverage — RED](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-01](plan/acceptance-criteria/08-01-checked-in-catalog-snapshot-coverage.md)
   Write failing tests asserting the fixture covers every resolver branch (aliases, `created`-timestamp candidates, expiring models, preview tokens, all 10 pinned slugs, `z-ai/` prefix). Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 8.2 [ ] **[Checked-in catalog snapshot coverage — GREEN](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   Author `internal/personas/testdata/catalog_snapshot.json` covering all branches. (T1), verify all pass (T2), COMMIT: `git commit -m "test(personas): checked-in catalog snapshot fixture (green)"`
   **Files:** `internal/personas/testdata/catalog_snapshot.json` | **Duration:** 3h

### 8.2.A [ ] **[Snapshot coverage — ADVERSARIAL REVIEW (subagent)](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **Changed Files:** `internal/personas/testdata/catalog_snapshot.json`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 8.2`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — is every resolver branch exercised by the fixture (missing `created`, null/non-null `expiration_date`, preview token, `z-ai/`)? Any branch untested?" Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 8.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 8.3 [ ] **[Snapshot coverage — REFACTOR](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   1. Fix CRITICAL/HIGH issues from 8.2.A (if any)
   2. Improve fixture/test quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): tighten snapshot fixture coverage"`
   **Duration:** 1h

### 8.4 [ ] **[models refresh regenerates snapshot — RED](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-02](plan/acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md)
   Write failing tests: `atcr models refresh` regenerates the snapshot from a live fetch; it is maintainer-initiated and NEVER CI-invoked. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h

### 8.5 [ ] **[models refresh — GREEN](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   Implement the `refresh` subcommand. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): refresh command regenerates snapshot (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 2h

### 8.5.A [ ] **[models refresh — ADVERSARIAL REVIEW (subagent)](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 8.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm `refresh` is the ONLY live-network touchpoint and cannot be triggered in CI; slug validation applied to fetched data before writing the fixture." Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 8.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 8.6 [ ] **[models refresh — REFACTOR](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   1. Fix CRITICAL/HIGH issues from 8.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up refresh command"`
   **Duration:** 1h

### 8.7 [ ] **[Docs: family/channel/lock + reproducibility — DOCUMENTATION](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-03](plan/acceptance-criteria/08-03-docs-family-channel-lock-and-reproducibility.md)
   1. Update `docs/personas-authoring.md` and `docs/personas-install.md` to document the family/channel/lock model and the reproducible-by-default vs. explicit-upgrade behavior.
   2. Include `atcr personas upgrade`, `atcr models check`, and `atcr models refresh` usage.
   3. Verify examples match real command output and the real roster (`anthony, sonny, gene, milo, gia, flint, delia, quinn, celeste, glenna`).
   4. COMMIT: `git commit -m "docs(personas): document family/channel/lock model + reproducibility"`
   **Files:** `docs/personas-authoring.md`, `docs/personas-install.md` | **Duration:** 3h

### 8.8 [ ] **Phase 8 — DoD**
   - [ ] `go test ./...` passes with ALL resolver/catalog tests backed by the checked-in snapshot (zero live network in CI)
   - [ ] Coverage ≥80%
   - [ ] Lint/vet/fmt clean; build succeeds
   - [ ] Docs updated and accurate; `refresh` never CI-invoked
   - [ ] DoD report per template

### 8.9 [ ] **Phase 8 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 8 + full-epic integration.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 8 gate review`
   - prompt: Phase 8 changed files + verbatim hostile-integrator checklist. Emphasize: zero live network in CI holds end-to-end; docs match real behavior; refresh is maintainer-only. Output: ONLY the findings table.

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 8 complete. Proceed to Final Phase validation.

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3: `go test ./...`)
- [ ] Coverage meets threshold (≥80%, `go test -coverprofile=coverage.out ./...`)
- [ ] Lint/format clean (`golangci-lint run`, `go vet ./...`, `gofmt`/`goimports`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Zero live network in CI (httptest + checked-in snapshot)
- [ ] All 8 ACs from original-requirements.md satisfied (AC1–AC8)

### Optional: Targeted Mutation Testing
Mutation tooling is **UNAVAILABLE** in this project (no `cargo-mutants`/`mutmut`/`stryker`). Skip mutation testing. If a Go mutation tool (e.g. `go-mutesting`) is added later, target ONLY the high-risk changed files (`internal/personas/catalog.go`, `internal/personas/upgrade.go`).
**WARNING:** Do NOT run full codebase mutation — it can take hours. Target specific files only.

### Drift Analysis
Compare the delivered implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] AC1: alias routability confirmed/refuted + `@stable` heuristic recorded (Phase 1)
- [ ] AC2: family/channel binding → locked slug; review runs the lock, zero endpoint call (Phase 2)
- [ ] AC3: 7 alias-covered personas + created-timestamp for DeepSeek/Qwen/GLM (`z-ai/`) + explicit-pin never floats (Phase 3)
- [ ] AC4: `personas upgrade` advances lock + before→after report; no silent runtime change (Phase 4)
- [ ] AC5: `models check [--json]` drift/deprecation/missing + exit codes (Phase 5)
- [ ] AC6: major-bump gate + verify flag; minor auto-locks (Phase 6)
- [ ] AC7: init/quickstart working non-noisy roster; 19.6 HIGH closed; backward compat (Phase 7)
- [ ] AC8: `go test ./...` green with checked-in snapshot (zero live network); docs updated (Phase 8)

**Next:** `/execute-code-review @.planning/sprints/active/19.7_live_model_resolution/`
