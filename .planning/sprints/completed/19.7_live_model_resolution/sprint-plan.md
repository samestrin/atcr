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

### Phase 4 Clarifications (recorded 2026-07-09)

**Key Decisions:**
- **Binding-string grammar (fully fail-closed; extends the forward-declared contract at `catalog.go:131-141`):** the persona's `binding:` string parses to `Binding{Family, Channel, Pin}` in this order — (1) empty `binding` → no resolution (see below); (2) `pin:` prefix → `Pin` = remainder (validated, verbatim, never floats); (3) contains `@` → split on the LAST `@`: `Family` = left part, `Channel` = the `@…` suffix (`@stable`/`@latest`); (4) bare, EXACT match against `aliasTable ∪ vendorPrefixTable` → that `Family` with default `@stable`; (5) anything else → ERROR `unrecognized binding %q: expected pin:<slug>, <family>@<channel>, or a known bare family`. Resolver dispatch order stays pin → alias → created-timestamp (`ResolveModel`).
- **Typo gap CLOSED (Option A, `pin:` sigil):** because pins now require the `pin:` prefix, the old "else → pin" fallback becomes "else → error," so an alias-shaped family typo (`anthropic/claude-opu`) fails cleanly at the resolver instead of being silently accepted as a pin and 404-ing downstream. Zero migration cost — no persona declares a `binding:` field today and no plan example shows a bare-pin string. Authoring rule: explicit pins are written `pin:<vendor>/<model>` (no `@channel`; pins ignore channels per AC 03-03).
- **Empty-binding upgrade behavior (required for AC7 / zero-migration):** when `binding` is empty (all 10 current personas), `Upgrade()` SKIPS the resolver entirely and keeps 19.6's unchanged `isNewer`/write version path (`upgrade.go:27-68`). Resolution engages only when a binding is present. An explicit test/AC-parity test for the bindingless persona is added this phase (both reviewer and maintainer flagged it as currently unspecified/untested).

**Scope Boundaries:**
- Phase 4 does NOT implement Phase 6's major-bump gate; AC 04-03 Error Scenario 1 gets a reportable hook only.
- `internal/registry.ResolvePersona` and `internal/fanout/review.go` stay UNTOUCHED (AC 04-02 guardrail).
- Story 3 is landed → tests call the real `personas.ResolveModel` and inject the catalog via an `httptest`-backed `HTTPClient`; no separate resolver-seam interface is added (minimum code).

**Technical Approach:**
- `Upgrade()` constructs `CatalogClient{HTTPClient: <injected>, BaseURL: CatalogBaseURL}` and fetches the catalog once per `Upgrade()` call (per-persona; AC 04-01 permits one fetch per persona). Resolved slug passes `validateResolvedSlug` before any lock write; a failed catalog fetch aborts cleanly with a descriptive error, leaving the existing lock unchanged (no partial advance, no stale fallback).

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

### 3.1 [x] **[Alias passthrough (7 personas) — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-alias-passthrough-seven-personas.md)
   Write failing tests: the 7 alias-covered personas bind to provider `-latest` aliases and pass through unchanged. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 3h

### 3.2 [x] **[Alias passthrough — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement alias-bind path + catalog client scaffolding. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): alias-bind resolver for 7 personas (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 4h
   **Done:** created `catalog.go` (`CatalogModel`, `CatalogClient.FetchModels` via the injected `HTTPClient`/`fetch()` seam, `Binding`, 7-entry `aliasTable`, `ResolveModel` alias branch + descriptive error) + checked-in `testdata/catalog_snapshot.json` (7 aliases, all 10 pins, deepseek//qwen//z-ai/ members, preview + expiring entries). Commit `8222def8`-parent.

### 3.2.A [x] **[Alias passthrough — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Files above + verbatim checklist (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE), plus: "Verify catalog client reuses fetch()'s body-size cap + timeout; verify slug validation before return." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | catalog.go:42-55 | `created` decoded via `json.Number` aborts the WHOLE `data` parse on a non-numeric value (`"abc"`/`true`/object) — breaks the "one malformed entry never crashes the scan" contract + AC 03-02 EC4 | FIXED inline in 3.3 — switched to `json.RawMessage` + `parseCreated` best-effort (number, float, numeric-string → int64; anything else → 0); added `TestCatalogModel_TolerantCreated` |
   | MEDIUM | catalog.go:9-16 | Dead `envCatalogURL` const + doc comment promising a `CatalogBaseURLFromEnv` override that does not exist; `FetchModels` reads only `c.BaseURL` | FIXED inline in 3.3 — deleted dead const, corrected comment to describe only the `BaseURL` injection seam (minimal fix; no speculative env override added per "minimum code" rule) |
   | LOW | catalog_test.go:88-99 | `failingHTTPClient` constructed then discarded (`_ =`) — misleading dead scaffold; `ResolveModel` holds no client | FIXED inline in 3.3 — removed the type + construction; test renamed/rewritten to honestly assert alias resolves identically against nil/empty/arbitrary model lists |
   | LOW | catalog_test.go | No malformed-catalog-JSON test for `FetchModels` | FIXED inline in 3.3 — added `TestCatalogClient_FetchModels_MalformedJSON` asserting the wrapped "parse model catalog" error |

   **Action Taken:** No CRITICAL/HIGH. All 4 findings were defects/inconsistencies in this element's freshly-authored code (two against my own stated contract + AC 03-02 EC4), so fixed inline in 3.3 ("clean up your own mess") rather than deferred. No tech-debt captured.

### 3.3 [x] **[Alias passthrough — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any) — none; fixed 2 MEDIUM + 2 LOW inline (own freshly-authored code)
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite `go test ./...` PASS; `golangci-lint run ./internal/personas/` 0 issues; `go vet`/`gofmt` clean
   3. COMMIT: `refactor(personas): tolerant created parse, honest alias tests, drop dead env const` (`8222def8`)
   **Duration:** 2h

### 3.4 [x] **[Created-timestamp vendor-prefix scan — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-02](plan/acceptance-criteria/03-02-created-timestamp-vendor-prefix-scan.md)
   Write failing tests: newest-in-vendor-prefix resolver for `deepseek/`, `qwen/`, `z-ai/` (glenna); missing `created` = ineligible; **no `glm/` namespace assumption anywhere**. **High-complexity AC.** Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 4h

### 3.5 [x] **[Created-timestamp vendor-prefix scan — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `created`-timestamp newest-in-prefix resolver. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): created-timestamp vendor-prefix resolver (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 4h
   **Done (`68fe25a8`):** `vendorPrefixTable` (deepseek→`deepseek/`, qwen→`qwen/`, glm→`z-ai/`), `resolveNewestInPrefix` (exact-prefix `HasPrefix`, ineligible `created<=0` skipped, `newerCandidate` total order [created desc, then slug desc] → array-order-independent, fail-closed descriptive error), `validateResolvedSlug` (TD-008 control-char mirror). Element 2 selects purely by newest eligible `created`; channel preview/deprecation filter layered in Elements 4–5.

### 3.5.A [x] **[Created-timestamp scan — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — null/missing `created`, ties, `z-ai/` vs `glm/` prefix; SECURITY — untrusted slug/timestamp fields." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | catalog.go (validateResolvedSlug) | A bare vendor-prefix ID (`"z-ai/"`) passes validation (non-empty, control-char-free, contains `/`); if it were the newest entry it would be returned as a malformed lock value | FIXED inline in 3.6 — require a non-empty segment on BOTH sides of the first `/` (rejects `"z-ai/"`, `"/glm-5.2"`, `"vendor/"`) |
   | LOW | catalog_test.go | `validateResolvedSlug`'s empty/control-char reject branches never exercised via the scan path | FIXED inline in 3.6 — added `TestResolveModel_CreatedScan_ControlCharSlug_Rejected` (scan fails closed on a control-char slug) + `TestValidateResolvedSlug` direct table (empty/blank/no-slash/bare-vendor/empty-vendor/control-char) |

   **Action Taken:** No CRITICAL/HIGH. Both findings hardened the load-bearing slug-sanitization guard on freshly-authored code → fixed inline in 3.6. No tech-debt captured.

### 3.6 [x] **[Created-timestamp scan — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any) — none; hardened `validateResolvedSlug` + added guard tests inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite PASS; lint 0 issues; vet/fmt clean
   3. COMMIT: `refactor(personas): reject bare vendor/model slugs, test sanitization guard` (`2df47e7f`)
   **Duration:** 2h

### 3.7 [x] **[Explicit-pin never floats — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-03](plan/acceptance-criteria/03-03-explicit-pin-never-floats.md)
   Write failing tests: an explicit-slug pin resolves to itself verbatim and NEVER floats to a newer member. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 3.8 [x] **[Explicit-pin never floats — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement explicit-pin escape hatch. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): explicit-pin escape hatch never floats (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 2h
   **Done (`0e23f0e4`):** pin short-circuit at the TOP of `ResolveModel` (before alias + scan): non-empty trimmed `Pin` validated via `validateResolvedSlug` then returned verbatim (never floats, channel/family irrelevant); empty/whitespace pin falls through; invalid pin → `invalid pin %q for family %q` error.

### 3.8.A [x] **[Explicit-pin — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm an explicit pin can never be silently advanced by any strategy." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH/MEDIUM:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | catalog_test.go (Pin suite) | No pin-path test drives a control-char / bare-vendor / bare-model pin through `ResolveModel`; the sanitization was only asserted transitively (scan path + direct `validateResolvedSlug`), so a refactor dropping `validateResolvedSlug(pin)` would leave every `Pin` test green | FIXED inline in 3.9 — widened `TestResolveModel_Pin_Invalid_Error` to a table (`not-a-slug`, `deepseek/x\n y`, `z-ai/`, `/glm-5.2`) pinning the rejection to the pin short-circuit itself |

   **Subagent verdict:** short-circuit returns unconditionally before alias + scan (pin can never float); trimmed pin == validated == returned value; empty/whitespace/no-slash all handled. No CRITICAL/HIGH/MEDIUM.

   **Action Taken:** No CRITICAL/HIGH. One LOW test-rigor gap on freshly-authored code → fixed inline in 3.9. No tech-debt captured.

### 3.9 [x] **[Explicit-pin — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.8.A (if any) — none; strengthened the one LOW test-rigor gap inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite PASS; lint 0 issues; vet/fmt clean
   3. COMMIT: `refactor(personas): pin-path rejection test for invalid pins` (`89d0fa1d`)
   **Duration:** 1h

### 3.10 [x] **[@stable excludes preview & expiring — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-04](plan/acceptance-criteria/03-04-stable-channel-excludes-preview-and-expiring.md)
   Write failing tests for `@stable`: excludes preview/beta/exp tokens AND models with non-null `expiration_date`. **High-complexity AC.** The `@stable`/`expiration_date`/preview interaction is now PINNED in the ACs: `@stable` excludes BOTH preview/beta/exp tokens AND non-null `expiration_date`; the `@latest`×`expiration_date` rule is pinned in AC 03-05 (only the preview-token exclusion is bypassed under `@latest` — deprecation is ALWAYS excluded, failing closed to the next-newest non-expiring entry). Encode these pinned rules directly in the tests. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 3h

### 3.11 [x] **[@stable excludes preview & expiring — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `@stable` channel logic per the decided semantics. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): @stable channel excludes preview + expiring (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 3h
   **Done (`18f55171`):** `passesStableFilter` = `!hasPreviewToken && !isDeprecated`, wired into `resolveNewestInPrefix`. **TD-001 RESOLVED** — `slugHasPreviewSegment` normalizes (strip `:variant` suffix + vendor prefix) then hyphen-segment matches `previewTokenSet` {preview,beta,exp,alpha,rc,experimental,nightly,snapshot}, never bare substrings. **TD-002 DECIDED** — `isDeprecated` = any-non-null expiration_date (fails closed, no horizon; empty/whitespace == not-deprecated per AC 03-04 EC3). Applied unconditionally this element (channel gate added in Element 5).

### 3.11.A [x] **[@stable channel — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.11`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — a model both preview-tagged AND with non-null `expiration_date`; confirm the documented `@stable`/`@latest` decision is applied consistently." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent, no plan context):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | HIGH | catalog.go:206 | `passesStableFilter` applied unconditionally; `b.Channel` never read, so `@latest` also excludes preview (AC 03-05 requires `@latest` to INCLUDE preview) | NOT a defect in Element 4 scope — this is **Element 5's explicit deliverable** (tasks 3.13–3.15, AC 03-05). The subagent has no plan context; the plan sequences `@stable` (Element 4) then `@latest` (Element 5). Driven by 3.13 RED → 3.14 GREEN (channel gate), resolved before the Phase 3 gate (3.17). Resolver is not wired anywhere yet (Phase 4) → no escaped defect, not tech-debt. |
   | MEDIUM | catalog_test.go:149 | No `@latest` created-scan test | This IS 3.13's RED test (Element 5). Added there. |
   | LOW | catalog.go:267 | Preview match splits only on `-`; a non-hyphen-joined marker (`v5preview`, `_preview`) would pass @stable | Acceptable per spec — the `openrouter-catalog-api.md` CRITICAL rule defines segment match over HYPHEN delimiters (bare-substring match over-excludes stable models). Subagent concurs it is within documented scope. No change. |

   **Subagent confirmed correct:** all substring false-positive risks avoided ("latest"⊃"test", "arcee"⊃"rc", "devstral"⊃"dev", "export"⊃"exp") via segment equality + `:variant`/prefix stripping; `isDeprecated` no nil-deref + honors empty/whitespace==not-deprecated; no panics on empty/multi-`/`/multi-`:` slugs; filter confined to created-scan path (alias/pin unaffected); selects newest-among-eligible.

   **Action Taken:** No in-scope CRITICAL/HIGH for Element 4. The one HIGH is Element 5's planned deliverable (sequenced next, TDD-driven, resolved before the phase gate) — transparently recorded, not deferred to tech-debt and not an escaped defect. LOW is spec-compliant. No tech-debt captured.

### 3.12 [x] **[@stable channel — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.11.A (if any) — none in Element 4 scope; the sole HIGH is Element 5's deliverable (below), driven by TDD next
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite PASS; lint 0 issues; vet/fmt clean
   3. COMMIT: no separate refactor commit — the `@stable` GREEN implementation (`18f55171`) is already minimal and correct; no code change to make. No empty/no-op commit created (mirrors Phase 2 task 2.3 precedent).
   **Duration:** 2h

### 3.13 [x] **[@latest includes preview — RED](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **AC:** [03-05](plan/acceptance-criteria/03-05-latest-channel-includes-preview.md)
   Write failing tests: `@latest` includes preview-tagged members BUT still excludes non-null `expiration_date` (deprecation), per the rule pinned in AC 03-05. Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 3.14 [x] **[@latest includes preview — GREEN](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   Implement `@latest` channel logic. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): @latest channel includes preview (green)"`
   **Files:** `internal/personas/catalog.go` | **Duration:** 2h
   **Done (`0c7e9bd2`):** `normalizeChannel` (""→@stable default; validates the two literals, case-sensitive; ok=false else) + `channelEligible` (deprecation ALWAYS excluded both channels; @latest bypasses ONLY the preview-token exclusion). Wired into `resolveNewestInPrefix` — channel validated BEFORE the scan (unrecognized → fail closed), applied per-entry. `passesStableFilter` removed (fully replaced by `channelEligible`; @stable behavior preserved). **Resolves the 3.11.A HIGH** (@latest now includes preview) via genuine RED→GREEN.

### 3.14.A [x] **[@latest channel — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   **Changed Files:** `internal/personas/catalog.go`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.14`
   - prompt: Files above + verbatim checklist, plus: "Confirm `@latest` vs `@stable` boundary is consistent with the 3.10 decision; no strategy cross-contamination." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH/MEDIUM (full-resolver-boundary pass):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | catalog_test.go | No test pins whitespace-padded (`"  @latest  "`) or whitespace-only (`"   "`/`"\t"`) channel handling (code correct via TrimSpace) | FIXED inline in 3.15 — added `TestResolveModel_Channel_WhitespaceTrimmed` + widened `TestResolveModel_EmptyChannel_DefaultsStable` to whitespace-only |
   | LOW | catalog_test.go | No test pins that an unrecognized channel on an alias/pin binding is IGNORED (code correct — alias/pin short-circuit before validation) | FIXED inline in 3.15 — added `TestResolveModel_InvalidChannel_IgnoredOnAliasAndPin` |

   **Subagent verdict (correctness NONE):** @stable via `channelEligible` exactly reproduces prior `passesStableFilter`; deprecated-AND-preview still excluded under @latest; channel validated before scan (unrecognized errors even with eligible entries); pin/alias short-circuit before channel → channel ignored there; @stable/@latest cannot diverge with no preview/deprecated entry; strict total-order tie-break order-independent; no panic; no untrusted channel persisted.

   **Action Taken:** No CRITICAL/HIGH/MEDIUM. Two LOW coverage gaps on freshly-authored code (code already correct) → fixed inline in 3.15. No tech-debt captured.

### 3.15 [x] **[@latest channel — REFACTOR](plan/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)**
   1. Fix CRITICAL/HIGH issues from 3.14.A (if any) — none; added 2 LOW edge-coverage tests inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite PASS; lint 0 issues; vet/fmt clean
   3. COMMIT: `refactor(personas): channel-gate edge tests (whitespace, alias/pin ignore)` (`62ee3c29`)
   **Duration:** 1h

### 3.16 [x] **Phase 3 — DoD**
   - [x] All three resolver strategies + both channels tested independently (T3) — alias / created-timestamp / explicit-pin + @stable / @latest each have dedicated `TestResolveModel_*` subtests; `go test ./...` exit 0
   - [x] Coverage ≥80%; zero live network (httptest + fixture) — personas pkg **85.9%**; core resolver funcs (`ResolveModel`, `resolveNewestInPrefix`, channel/filter helpers) 100%; all catalog access via injected `HTTPClient` + `testdata/catalog_snapshot.json`, no real network
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run ./internal/personas/` 0 issues, `go vet` clean, `gofmt -l` empty, `go build ./...` OK
   - [x] `ResolvePersona` untouched; `@stable`/`@latest` decision recorded — `git diff main...HEAD -- internal/registry/persona.go` empty; channel semantics recorded in 3.11/3.14 + `channelEligible` doc comment; TD-001 resolved + TD-002 decided (any-non-null) in `tech-debt-captured.md`
   - [x] DoD report per template

   ```
   Story-03 DoD Complete
   Auto: 3/3 (tests passing, no lint errors, build succeeds)
   Story-Specific (AC 03-01/02/03/04/05): all green
     03-01: 7 alias tiers resolve verbatim, no catalog scan, exact-match table, unknown-family error
     03-02: newest-in-prefix (deepseek//qwen//z-ai/), glm→z-ai/ regression, exact-prefix (no z-ai-evil/), desc-lex tie-break (order-independent), ineligible-created excluded, fail-closed error
     03-03: pin verbatim, invariant across 2 snapshots, overrides channel, precedence over alias+scan, empty/whitespace falls through, invalid-pin rejected on pin path
     03-04: @stable excludes preview segment tokens (TD-001 normalized) + any-non-null expiration (TD-002 decided), ""==not-deprecated, all-excluded fail-closed
     03-05: @latest includes preview but still excludes deprecated, newest-among-all, ==@stable when clean, unrecognized-channel fail-closed, alias/pin ignore channel
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 3.17 [x] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Phase 3 changed files + verbatim hostile-integrator checklist (CONTRACT EXIT / CONFIG SURFACE / INTEGRATION / PHASE-EXIT CONTRACT / REGRESSION). Emphasize: three strategies are independently testable and don't cross-contaminate; catalog client is the ONLY code that talks to the external API; `ResolvePersona` untouched. Output: ONLY the findings table.

   **Subagent findings (fresh-context hostile-integrator subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | catalog_test.go | The checked-in `catalog_snapshot.json` was only parsed, never fed through `ResolveModel` → fixture↔resolver drift (or a filter regression) could break Phase 4 zero-migration with a green suite | FIXED inline (`e64a3bdb`) — added `TestResolveModel_AgainstFixture_ScanFamilies`: deepseek→`deepseek-v4-pro`, glm→`z-ai/glm-5.2` (@stable + @latest) EQUAL the 19.6 pins; qwen→`qwen3.7-plus` ≠ coder pin (documents why quinn is explicit-pin). Fixed inline (not deferred): it locks the exact artifact Phase 4 inherits + is own freshly-authored test code |
   | LOW | catalog.go (Family tables) | Heterogeneous `Family` key space (vendor/tier for alias, bare-brand for scan, `glm`→`z-ai/`); Phase 4's binding parser must emit exact shapes or hit the generic unresolvable-family error | FIXED inline (`e64a3bdb`) — added a "Family grammar" contract doc comment beside the tables; the new fixture test asserts each scan family resolves |
   | LOW | catalog.go (resolveNewestInPrefix) | Channel validated only on the scan path; a typo'd channel on an alias/pin binding is silently ignored (8/10 personas) | ACCEPTED AS DESIGNED — uniform top-level channel validation would contradict AC 03-03 (pin overrides/ignores channel) and AC 03-05 EC2 (alias ignores channel entirely). `TestResolveModel_InvalidChannel_IgnoredOnAliasAndPin` encodes this deliberate, AC-grounded behavior. No change |

   **Subagent confirmed:** `persona.go`/`client.go` empty diff vs main (genuinely untouched); `CatalogClient` is the ONLY OpenRouter caller (reuses `fetch()` guards, not forked); all 10 of 19.6's pinned slugs present in the fixture; no strategy cross-contamination (pin can't float, alias isn't scanned, channel doesn't leak onto alias/pin).

   **Action Taken:** 0 CRITICAL/HIGH → no re-run required. 1 MEDIUM + 1 LOW were gaps in this phase's own freshly-authored test/doc surface guarding Phase 4's inherited contract → fixed inline (`e64a3bdb`). 1 LOW accepted as AC-grounded design. No tech-debt captured. ✅ **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 3 complete. Await review before Phase 4.

---

## Phase 4: Upgrade Integration (2 days)

**Story:** [04: Reproducible Upgrade with Before→After Lock Reporting](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)
**Focus:** Wire the Phase 3 resolver into `Upgrade()` immediately before the existing `isNewer`/write logic; extend `atcr personas upgrade` reporting to show before→after resolved slug; prove zero endpoint calls occur outside this explicit path.

### 4.1 [x] **[Upgrade resolves & advances lock + before→after report — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-01](plan/acceptance-criteria/04-01-upgrade-resolves-advances-lock-slug-report.md)
   Write failing tests: `upgrade` re-resolves, advances the lock, and reports before→after per persona (e.g. `anthony: opus-4.8 → 5.0`). Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go`, `cmd/atcr/personas_test.go` | **Duration:** 3h

### 4.2 [x] **[Upgrade resolves & advances lock — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Insert resolver call into `Upgrade()` before `isNewer`/write; extend reporting. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): upgrade re-resolves + before→after lock report (green)"`
   **Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go` | **Duration:** 4h

### 4.2.A [x] **[Upgrade resolves & advances lock — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Files above + verbatim checklist, plus: "SECURITY — resolved slug validated before write to lock; ERROR HANDLING — a failed catalog fetch aborts cleanly (no partial lock advance, no silent stale fallback)." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | upgrade.go (upgradeResolvedLock gate) | An empty current lock never establishes: `isNewer("", newSlug)` hits the mixed-validity branch → false, so a no-prior-lock persona is reported up-to-date and the resolved slug is never written — violates AC 04-01 Edge Case 2 | FIXED inline in 4.3 — an empty current lock establishes the resolved slug unconditionally (skips the version-advance gate); added `TestUpgrade_BindingEstablishesLockFromEmpty` |
   | MEDIUM | upgrade.go (writePersonaUnit reuse) | The binding path re-fetches the co-located `.md`; a transient non-404 `.md` error aborts an otherwise-successful lock advance, and a local custom prompt is re-synced from remote | CAPTURED → tech-debt-captured.md TD-003 (LOW). Reusing `writePersonaUnit` is the story's explicit mandate ("writes continue to flow through writePersonaUnit so install and upgrade stay consistent") and the `.md` fetch is AC 04-02's expected "persona-unit fetch"; write-only-yaml is a future robustness optimization |
   | MEDIUM | upgrade.go:218 / personas.go:374 | `--all` fetches the full catalog once per bound persona (N fetches) | ACCEPTED AS DESIGNED — AC 04-01 & AC 04-03 explicitly state "one resolver/catalog fetch per persona, no batching optimization required by this story." No change |
   | LOW | upgrade.go (gate) | Selection authority mismatch: `resolveNewestInPrefix` picks newest-by-`created`, but the lock gate advances only on higher semver — a newer-created-but-lower-semver slug is selected yet not adopted | CAPTURED → tech-debt-captured.md TD-004 (LOW, Story 6). This is the created-vs-semver divergence surfaced to the maintainer at the Phase 4 safety gate; story mandates `isNewer` reuse. Story 6's major/minor classification revisits it |
   | LOW | upgrade_test.go | Coverage gaps: no empty-lock advance, no alias-family-through-Upgrade, no `.md`-preservation test | FIXED inline in 4.3 — added `TestUpgrade_BindingEstablishesLockFromEmpty` and `TestUpgrade_AliasFamilyDoesNotAdvance` (encodes the documented alias-is-constant semantic). `.md` preservation tied to TD-003 |

   **Action Taken:** No CRITICAL/HIGH. One MEDIUM was a genuine AC-04-01-EC2 violation in this element's own code → fixed inline in 4.3 (+ 2 tests). One MEDIUM + one LOW captured to `tech-debt-captured.md` (TD-003, TD-004). One MEDIUM + test-gap LOW are AC-grounded/addressed. Proceeding.

### 4.3 [x] **[Upgrade resolves & advances lock — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up upgrade resolver integration"`
   **Duration:** 2h

### 4.4 [x] **[Resolution isolated to upgrade path — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-02](plan/acceptance-criteria/04-02-resolution-isolated-to-upgrade-path.md)
   Write failing tests proving NO endpoint/catalog call occurs on any path except explicit `upgrade`/`models` (inject an `HTTPClient` that fails on unexpected calls; exercise review + other commands). **High-complexity AC — most adversarial test design.** Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 3h
   **Note — transparent regression-lock RED:** Like AC 02-02/02-03, 04-02 is a negative-space/guardrail AC — the isolation already holds by construction after task 4.2 (the catalog is fetched only inside `upgradeResolvedLock`'s binding branch, and `internal/registry`↔`internal/personas` is an import cycle so `ResolvePersona`/review fan-out structurally cannot reach the catalog client). So the two tests PASS on first run rather than failing. They are permanent guards: `TestUpgrade_CatalogFetchedOnlyOnBindingPath` (recording `HTTPClient`) is genuinely discriminating — it fails if a bindingless upgrade fetches the catalog OR if the binding path stops fetching it; `TestReviewAndResolvePathsCannotReachCatalog` fails the build the moment any file in `internal/registry` or `internal/fanout` imports `internal/personas` (a superset of the "no import in persona.go/review.go" DoD bullet — compile-enforced, stronger than a per-path behavioral check).

### 4.5 [x] **[Resolution isolated to upgrade path — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Ensure resolution is invoked ONLY on the explicit upgrade path. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): isolate resolution to upgrade path (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 2h
   **Done:** No production change required — the isolation was satisfied by task 4.2's design (single catalog construction site in `upgradeResolvedLock`; no cross-package import path to the catalog). Confirmed by both 4.4 tests. `--all` uses the same `runPersonaUpgrades`→`Upgrade` loop (each name through the same binding branch) and is exercised directly by AC 04-03. Committed test-only.

### 4.5.A [x] **[Resolution isolation — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.5`
   - prompt: Files above + verbatim checklist, plus: "Prove no hot-path or incidental command triggers a catalog fetch — enumerate every caller of the resolver." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — claim NOT refuted:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | NONE | upgrade.go:218 | Claim confirmed. Only production `CatalogClient{}` construction + only `FetchModels`/`ResolveModel` callers are in `upgradeResolvedLock`, reached solely from `Upgrade` when `parseBinding` returns present=true; bindingless branch never touches the catalog. `personas test/list/search/install` reach none of it. `registry.ResolvePersona` imports the embedded-prompt `personas` package, NOT `internal/personas`; `internal/fanout` imports neither — `go list -deps` reports ZERO transitive dependency on `internal/personas` from both. Both isolation tests genuinely discriminating (recordingClient wraps the exact client threaded into CatalogClient; import-scan matches the exact quoted path). | — |
   | LOW (advisory) | upgrade_test.go (import-scan) | The import-scan was direct-only/non-recursive — a future subpackage under `internal/registry`/`internal/fanout` could evade it | FIXED inline in 4.6 — `TestReviewAndResolvePathsCannotReachCatalog` now `filepath.WalkDir`s the trees recursively (own freshly-authored test surface). |

   **Action Taken:** Claim confirmed (0 CRITICAL/HIGH/MEDIUM). One advisory LOW hardened inline (recursive import-scan). Production code unchanged — the isolation holds by construction. Proceeding.

### 4.6 [x] **[Resolution isolation — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.5.A (if any) — none; hardened the one advisory LOW (recursive import-scan) inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ green, lint 0 issues
   3. COMMIT: `git commit -m "refactor(personas): clean up resolution isolation"`
   **Duration:** 1h

### 4.7 [x] **[Dry-run reports without writing — RED](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **AC:** [04-03](plan/acceptance-criteria/04-03-dry-run-reports-without-writing.md)
   Write failing tests: a dry-run reports the before→after it WOULD apply and writes nothing to disk. Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go`, `cmd/atcr/personas_test.go` | **Duration:** 2h
   **Note — transparent regression-lock RED:** the dry-run path (`if dryRun { return res }` before the write) and the CLI `Would upgrade … → …` report already landed in task 4.2, so the library dry-run tests PASS on first run. The genuinely-new, discriminating coverage is `TestUpgrade_BindingDryRunParity` (dry-run == real-run UpgradeResult, disk unchanged — the shared-computation guarantee) and `TestPersonasUpgrade_AllDryRunReportsNoWrite` (multi-persona `--all` fan-out: one line per persona, changing + unchanged, zero writes). AC 04-03 Error Scenario 1 (blocked major-bump reporting) is Story 6/Phase 6 (the gate does not exist yet) — reportable-hook only per the Phase 4 clarification.

### 4.8 [x] **[Dry-run reports without writing — GREEN](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   Implement dry-run reporting path. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): upgrade dry-run reports without writing (green)"`
   **Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go` | **Duration:** 2h
   **Done:** No production change required — dry-run report parity was satisfied by task 4.2 (shared computation up to the `if dryRun` short-circuit; CLI dry-run branch). Committed test-only.

### 4.8.A [x] **[Dry-run — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `cmd/atcr/personas.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm dry-run writes NOTHING (no lock file, no persona YAML mutation) on any branch." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — core claim confirmed:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | NONE (core) | upgrade.go / unit.go | "No disk write on dry-run" holds: every filesystem mutation is confined to `writePersonaUnit`, called only AFTER the dryRun short-circuit on BOTH the 19.6 version path and the 19.7 resolution path; `from/to` slug + `SlugChanged` computed before the branch (report parity); tests compare byte-for-byte + per-persona `--all` | — |
   | LOW | upgrade.go (resolution dryRun return) | Report-parity deviation: dry-run returned BEFORE `setModelField`+`ValidateCommunityPersonaYAML`, so a dry-run could advertise a would-be advance a real run would ERROR on (e.g. a binding persona with no `model:` key). Asymmetric with the 19.6 path, which validates before its dry-run return | FIXED inline in 4.9 — moved `setModelField`+validation BEFORE the dryRun short-circuit (only the write stays gated); added `TestUpgrade_BindingDryRunReportsValidationError` asserting dry-run and real-run errors are identical |
   | LOW | upgrade_test.go | "No `.md` write/delete on dry-run" proven by code path but not by a test | FIXED inline in 4.9 — added `TestUpgrade_BindingDryRunLeavesMarkdownUntouched` (pre-plants a stale `.md`, asserts it survives a dry-run advance) |

   **Action Taken:** Core claim confirmed. Two LOWs in this element's own code fixed inline in 4.9 (report-parity + the missing `.md`-untouched test). No tech-debt captured. Proceeding.

### 4.9 [x] **[Dry-run — REFACTOR](plan/user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)**
   1. Fix CRITICAL/HIGH issues from 4.8.A (if any) — none; fixed 2 LOW report-parity/test gaps inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite green, lint 0 issues
   3. COMMIT: `git commit -m "refactor(personas): dry-run report parity + .md-untouched guard"`
   **Duration:** 1h

### 4.10 [x] **Phase 4 — DoD**
   - [x] All Phase 4 tests passing (T3: `go test ./...`) — full suite exit 0
   - [x] Coverage ≥80%; zero live network — personas 85.9%, cmd/atcr 84.0%; all resolver/catalog tests use `httptest` + `ATCR_CATALOG_URL` (no live network)
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run` 0 issues, `go vet ./...` clean, `gofmt -l` empty, `go build ./...` OK
   - [x] No silent runtime model change anywhere; failed fetch aborts cleanly — resolution confined to `Upgrade`'s binding branch (AC 04-02 spy + import-zero tests); `TestUpgrade_BindingResolveFailAbortsCleanly` proves a failed fetch/unresolvable binding leaves the lock unchanged
   - [x] DoD report per template

   ```
   Story-04 DoD Complete
   Auto: 3/3 (tests passing, lint/vet/fmt clean, build succeeds)
   Story-Specific (AC 04-01/02/03): 5/5 + 4/4 + 4/4
     04-01: Upgrade resolves binding → advances lock only on differ+version-advance; before→after slug report (change + unchanged, never omitted); empty-lock establishes with "(none)" placeholder; resolver/validation failures reported per-persona without corrupting the persona; catalog-fetch failure aborts cleanly (no partial advance / stale fallback)
     04-02: recording-HTTPClient proves catalog fetched only on the binding branch (bindingless never fetches); recursive import-zero guard proves internal/registry (ResolvePersona) + internal/fanout (review) cannot reach the catalog (compile-enforced, go list -deps confirms zero transitive dep)
     04-03: dry-run reports identical before→after with zero writes (byte-for-byte); dry-run/real-run UpgradeResult + error parity (setModelField+validate before the write gate); --all fan-out one line per persona; .md-untouched on dry-run. NOTE: blocked-major-bump reporting (AC 04-03 ErrorScenario1) is Story 6/Phase 6 — gate not yet built
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 4.11 [x] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Phase 4 changed files + verbatim hostile-integrator checklist. Emphasize: resolution happens ONLY at upgrade; before→after report is accurate; graceful degradation on fetch failure. Output: ONLY the findings table.

   **Subagent findings (fresh-context hostile-integrator subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | upgrade.go (upgradeResolvedLock → writePersonaUnit) | Resolved-lock advance re-fetches/deletes the co-located `.md`; a binding persona with a locally-customized `.md` but no upstream `.md` loses its prompt on a slug bump, and the advance is coupled to the personas endpoint; contradicts the `setModelField` doc-comment | RESOLVED (post-gate, at maintainer request) → TD-003 marked Resolved. The resolved path now writes only the model-bumped YAML via `writeLockYAML` (`refuseSymlinkedIntermediate` + `writeFileAtomic`), never touching/fetching the `.md`; `upgradeResolvedLock` dropped its now-unused `baseURL` param. `TestUpgrade_BindingAdvancePreservesLocalMarkdown` locks it; `setModelField` comment corrected |
   | LOW | upgrade.go (parseBinding @-branch) | Empty channel `family@` parsed as valid `Channel:"@"`: alias family silently ignores it (hides typo) while scan family fails at `normalizeChannel` — asymmetric | FIXED inline at the gate — `parseBinding` now rejects a bare trailing `@` (empty channel) for ALL families; added `TestParseBinding/empty_channel_errors` |
   | LOW | upgrade.go (dry-run) | Dry-run parity excludes the `.md` write step (real run fetches/writes `.md`, dry-run does not) | RESOLVED with TD-003 — the write-only-yaml resolved path means neither dry-run nor real-run touches the `.md`, so the parity gap is closed |

   **Checklist items that held (subagent-confirmed):** resolver confinement sound (no `ResolveModel`/`FetchModels`/`CatalogClient` outside `catalog.go`/`upgrade.go`; import-zero guard bans registry/fanout from `internal/personas` → C3 holds); CLI switch in `runPersonaUpgrades` covers every `UpgradeResult` shape with a `default`, errors to stderr with `failed=true`, `--all` uses `continue` (no persona omitted, no partial corruption); `parseBinding`↔`ResolveModel` grammar consistent (multi-`@`/unknown families fail closed before any write); `binding` is a recognized `KnownFields(true)` key so strict decode accepts resolved YAML; byte-for-byte zero-migration preserved for unchanged personas; AC7 parity gate + paired-write rollback intact.

   **Action Taken:** 0 CRITICAL/HIGH → no gate re-run required. At the gate: 1 LOW fixed inline (empty-channel fail-closed + test), doc-comment honesty fixed. Post-gate, at maintainer request, the MEDIUM (`.md` re-sync) was fully RESOLVED inline (write-only-yaml `writeLockYAML`; TD-003 closed), which also closed the dry-run `.md`-parity LOW — full suite green, coverage personas 85.9%/cmd 84.0%, lint 0 issues. ✅ **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 4 complete. Await review before Phase 5.

---

## Phase 5: Discovery — `atcr models check` (2 days)

**Story:** [05: `atcr models check` Drift Report](plan/user-stories/05-atcr-models-check-drift-report.md)
**Focus:** Net-new `cmd/atcr/models.go` command family; enumerate installed personas' locked slugs (via a `ListTiers`-style pattern); report newer-member/deprecation/missing with `--json` and a 0/1/2 exit-code contract; default to the checked-in snapshot for determinism.

### 5.1 [x] **[Command registration + human-readable drift report — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-command-registration-human-readable-drift-report.md)
   Write failing tests: `atcr models check` is registered next to `personas`; prints a human-readable drift report (newer member / deprecation / missing). Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 3h

### 5.2 [x] **[Command registration + drift report — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement `cmd/atcr/models.go` `check` subcommand + registration at `cmd/atcr/main.go`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): register models check + human-readable drift report (green)"`
   **Files:** `cmd/atcr/models.go`, `cmd/atcr/main.go` | **Duration:** 4h

### 5.2.A [x] **[Command registration + drift report — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `internal/personas/drift.go`, `internal/personas/snapshot.go`, `cmd/atcr/main.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Files above + verbatim checklist, plus: "Confirm `models check` is diagnostic-only (never on the review path); enumeration handles a persona with a missing slug without panicking." Output: ONLY the findings table.

   **Subagent confirmed:** go:embed compiles into the production binary; finding order deterministic (ListTiers order, newer-member before deprecation); catalog indexed once for missing/deprecation; `missing` correctly terminal/exclusive; no injection/exit-code/AC-breaking defect.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | drift.go (deriveFamilyPrefix/inFamilyPrefix) | Bindingless family-prefix fallback can cross tiers: `openai/gpt-5.5` → prefix `openai/gpt` also matches `openai/gpt-6-mini`; a higher-versioned sibling tier could be floated as "newer member" | FIXED inline in 5.3 — matched candidates by `deriveFamilyPrefix(candidate)==familyPrefix` (same-tier equality) instead of raw prefix; `inFamilyPrefix` removed; added `TestCheckDrift_BindinglessFallback_NoCrossTierBleed` |
   | MEDIUM | drift.go (missing check) | Alias-bound persona locks Model to the synthetic `~vendor/-latest` slug; if Phase 8's `models refresh` regenerates a live catalog that omits the `~` ids, every alias-bound persona reports a false `missing` | CAPTURED → tech-debt-captured.md TD-005 (MEDIUM, Phase 8). Today the snapshot lists the `~` entries and the Phase 1 spike confirmed the `~…-latest` aliases are real, routable, listed catalog entries; the risk is only a future live-catalog shape change coupled to refresh |
   | LOW | models.go (filter no-match) | `[name]` matching no community persona prints "nothing to check" even when community personas exist | FIXED inline in 5.3 — distinct `no community persona named %q to check` message when a filter is supplied and nothing matched; added `TestModelsCheck_FilterNoMatch_DistinctMessage` |
   | LOW | drift.go (human display) | Displayed slug/expiration printed raw; a crafted persona `model:` or catalog id could inject terminal control chars into stdout (JSON path already safe via encoder) | FIXED inline in 5.3 — newer-member scan now skips candidates failing `validateResolvedSlug` (mirrors resolveNewestInPrefix), and the human render strips control chars via `sanitizeDisplay`; added `TestDriftLine_StripsControlChars` |
   | LOW | drift.go:63-69 | Docstring claimed O(n) but newer-member re-scans per persona (O(personas×models)) | FIXED inline in 5.3 — corrected the CheckDrift docstring to state the missing/deprecation lookups are O(1) via the index while newer-member scans per persona |

   **Action Taken:** 0 CRITICAL/HIGH → no blocker. 1 MEDIUM (cross-tier) + 3 LOW were defects/gaps in this element's own freshly-authored code → fixed inline in 5.3 ("clean up own mess", per Phase 2-4 precedent). 1 MEDIUM (alias false-missing) is forward-looking + Phase-8-coupled → captured to `tech-debt-captured.md` (TD-005). ✅ Adversarial review passed.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.3 [x] **[Command registration + drift report — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any) — none; fixed 1 MEDIUM (cross-tier bleed) + 3 LOW (filter-no-match msg, control-char display sanitization, docstring) inline on own freshly-authored code; captured 1 forward-looking MEDIUM (alias false-missing) to TD-005
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite `go test ./...` 0 failures; `golangci-lint run` 0 issues; vet/fmt clean
   3. COMMIT: `refactor(models): same-tier family match, sanitize display, filter-no-match msg`
   **Duration:** 2h

### 5.4 [x] **[--json machine-readable output — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-02](plan/acceptance-criteria/05-02-json-machine-readable-output.md)
   Write failing tests: `--json` emits a stable machine-readable shape (the seam Epic 19.8 wraps). Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h
   **Note — transparent regression-lock RED:** the `--json` flag and `renderDriftJSON` derive from the SAME `personas.DriftFinding` structure the human renderer uses, which landed with the shared-structure design in element 1 (task 5.2) per AC 05-02's "never two independently-computed code paths." So the 6 JSON tests (`TestModelsCheckJSON_*`: array-one-object-per-condition, newer-member field set, omitempty of inapplicable fields + no `null`, `[]` on empty + no-personas, human/JSON (persona,condition) parity) PASS on first run. Their value is the permanent contract guard on Epic 19.8's machine-readable seam.

### 5.5 [x] **[--json output — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement `--json`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): --json machine-readable drift output (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 2h
   **Done:** No production change required — `--json` (flag + `renderDriftJSON` emitting `[]DriftFinding` with `omitempty`, `[]` on empty) was implemented in element 1 as part of the single shared structure feeding both renderers. Committed test-only (`test(models): --json machine-readable drift output contract (green)`).

### 5.5.A [x] **[--json output — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm JSON shape is stable/documented and does not leak secrets; handles empty/no-drift case." Output: ONLY the findings table.

   **Subagent confirmed (0 CRITICAL/HIGH/MEDIUM):** empty findings emit `[]` (nil-guard + `make([],0)`); both renderers consume the single `findings` slice (shared-structure parity real by construction); condition strings are stable constants; newer-member always carries family+channel and deprecation always carries expiration_date (omitempty never drops a required field); no secret-bearing fields; stdlib encoder escapes control chars; command failures precede any stdout write.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | LOW | models_test.go (JSON suite) | No JSON test asserts a single persona with two conditions emits TWO objects (the `byPersona` map masks it) | FIXED inline in 5.6 — added `TestModelsCheckJSON_MultiCondition_TwoObjects` (own freshly-authored test surface) |
   | LOW | models_test.go (JSON suite) | No JSON test exercises quotes/control chars in a value (the `--json` path deliberately opts out of `sanitizeDisplay`, relying on stdlib escaping) | FIXED inline in 5.6 — added `TestRenderDriftJSON_EscapesControlChars` (ESC/quote/U+2028 escaped + round-trips) |
   | LOW | models.go:196-198 | Theoretical partial-JSON on a stdout short-write mid-`enc.Encode` | ACCEPTED AS DESIGNED — unreachable by any computable command failure (personasDir/SnapshotModels fail before the write); a raw stdout short-write is unrecoverable and buffer-then-write cannot fully prevent it either. No change |

   **Action Taken:** 0 CRITICAL/HIGH/MEDIUM. Two LOW test-coverage gaps in this element's own tests → fixed inline in 5.6. One LOW theoretical io-error accepted as designed. ✅ Adversarial review passed.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.6 [x] **[--json output — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.5.A (if any) — none; added 2 LOW JSON coverage tests inline (multi-condition two-objects + control-char escaping/round-trip); 1 LOW accepted as designed
   2. Improve quality, maintain green (T1), validate (T3) — ✅ JSON suite green, full suite 0 failures
   3. COMMIT: `test(models): JSON multi-condition + control-char escaping coverage`
   **Duration:** 1h

### 5.7 [x] **[Exit-code contract (0/1/2) — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-03](plan/acceptance-criteria/05-03-exit-code-contract.md)
   Write failing tests for the 0 (no drift) / 1 (drift) / 2 (error) exit-code contract. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h
   **Note — transparent regression-lock RED:** the 0/1/2 mapping was implemented in element 1 (`driftFoundError.ExitCode()==exitFailure(1)`; snapshot load/parse failure wrapped in `usageError`→2; clean→nil→0; cobra flag errors→root `SetFlagErrorFunc`→usageError→2), so the 6 exit tests (`TestModelsCheckExit_*`: clean=0, conditions=1, usage=2+no-report, findings+read-failure=1-not-2, missing-snapshot=2, malformed-snapshot=2 — each covering `--json` parity) PASS on first run. Their value is the permanent contract guard Epic 19.8 and CI depend on.

### 5.8 [x] **[Exit-code contract — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Implement exit codes. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): 0/1/2 exit-code contract (green)"`
   **Done:** No production change required — the typed `driftFoundError` (exit 1), `usageError`-wrapped snapshot failures (exit 2), clean-nil (exit 0), and cobra flag-error → exit 2 all landed in element 1. Committed test-only (`test(models): 0/1/2 exit-code contract (green)`).
   **Files:** `cmd/atcr/models.go` | **Duration:** 1h

### 5.8.A [x] **[Exit-code contract — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm every code path maps to exactly one of 0/1/2; an internal error never returns 0 or 1." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | models.go:72-75 | `personasDir()` failure returned an UNCODED error → `main()` maps it to exit 1 (drift), but it is an unrecoverable command failure that must exit 2 — inconsistent with the `SnapshotModels` failure 3 lines below which IS wrapped in `usageError`→2 | FIXED inline in 5.9 — wrapped in `usageError(err)` (exit 2), matching the snapshot handling; own freshly-authored code |
   | LOW | models.go:124-126 | `renderDriftJSON` write error returned uncoded → exit 1, while the text path swallows write errors → a stdout-write failure with zero findings would exit 1 under `--json` but 0 under default (parity gap, AC 05-03 EC2) | FIXED inline in 5.9 — the JSON render error is now swallowed symmetrically (`_ = renderDriftJSON(...)`) so the exit code is purely findings-based in both modes; `renderDriftJSON` cannot fail on marshaling, only on an unrecoverable stdout write |
   | LOW | models_test.go (exit suite) | The "per-persona read failure with ZERO findings → exit 0" path was untested | FIXED inline in 5.9 — added `TestModelsCheckExit_ReadFailureOnly_Zero` (default → 0 + "No drift…"; `--json` → 0 + `[]`) |

   **Action Taken:** 0 CRITICAL/HIGH. One MEDIUM (an exit-code leak: command failure mis-mapped to 1) + two LOW (json/text exit parity, missing test) were all defects/gaps in this element's own freshly-authored code → fixed inline in 5.9. No tech-debt captured. ✅ Adversarial review passed.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.9, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.9 [x] **[Exit-code contract — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.8.A (if any) — none; fixed 1 MEDIUM (personasDir failure → exit 2) + 2 LOW (json/text exit parity, zero-findings-read-failure test) inline
   2. Improve quality, maintain green (T1), validate (T3) — ✅ all models tests green; `golangci-lint run` 0 issues
   3. COMMIT: `refactor(models): personasDir failure exits 2, json/text exit parity`
   **Duration:** 1h

### 5.10 [x] **[Deterministic catalog-snapshot default — RED](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **AC:** [05-04](plan/acceptance-criteria/05-04-deterministic-catalog-snapshot-default.md)
   Write failing tests: `models check` defaults to the checked-in snapshot (zero network) for deterministic output. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go`, `internal/personas/drift_test.go` | **Duration:** 2h
   **Note — transparent regression-lock + new unit coverage:** the deterministic embedded-snapshot default (`//go:embed testdata/catalog_snapshot.json` via `SnapshotModels`, zero network by construction — no `HTTPClient` in the default path) and the deterministic finding order landed in element 1, so the integration tests (`TestModelsCheck_Deterministic_RepeatedRuns` — identical stdout+exit across repeated default and `--json` runs; `TestModelsCheck_DefaultPath_ZeroNetwork` — a `failRoundTripper` + failing httptest server prove no HTTP call) PASS on first run. Added NEW `internal/personas/drift_test.go` unit coverage of the classifier + loader: missing/deprecation/null-expiration(EC2)/empty-string-expiration/empty-lock/same-tier-newer-member/bound-resolver-newer-member/deterministic-order + `TestSnapshotModels_RoundTrip` (slug/created/expiration survive the loader). The missing/malformed-snapshot → exit 2 error paths were already covered in element 3 (`TestModelsCheckExit_MissingSnapshot_Two`/`_MalformedSnapshot_Two`).

### 5.11 [x] **[Snapshot default — GREEN](plan/user-stories/05-atcr-models-check-drift-report.md)**
   Default to the checked-in snapshot. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): default to checked-in snapshot for determinism (green)"`
   **Files:** `cmd/atcr/models.go`, `internal/personas/snapshot.go` | **Duration:** 2h
   **Done:** No production change required — `SnapshotModels` (embedded snapshot via `//go:embed`, `ATCR_CATALOG_SNAPSHOT` override for the error-path tests, zero network in the default path) landed in element 1 and is the sole catalog source `models check` uses. Committed test-only (`test(models): deterministic snapshot default + zero-network guard (green)`).

### 5.11.A [x] **[Snapshot default — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-atcr-models-check-drift-report.md)**
   **Changed Files:** `internal/personas/snapshot.go`, `internal/personas/drift.go`, `internal/personas/drift_test.go`, `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.11`
   - prompt: Files above + verbatim checklist, plus: "Confirm the default path makes zero network calls; live-catalog mode (if any) is explicit opt-in." Output: ONLY the findings table.

   **Subagent confirmed (0 CRITICAL/HIGH/MEDIUM), all AC 05-04 contracts hold with tests:** determinism real (no map iteration reaches stdout; `bySlug` + static tables are lookup-only; total-order tie-break is array-order-independent; byte-identical verified); zero-network real (`strings` on the built binary confirms the snapshot is compiled in; failing RoundTripper asserts no calls); go:embed of a single explicit `testdata/` file DOES compile into the non-test production binary (empty embed would fail `TestSnapshotModels_RoundTrip`); round-trip + `_fixture_meta`/unknown-field ignore; null AND empty-string expiration both non-deprecated; far-future sentinel + `~`-aliases correctly excluded from bindingless suggestions; snapshot parsed once per invocation; `ATCR_CATALOG_SNAPSHOT` is an intended operator/test seam reading a file the invoking user can already read (no traversal/privilege/TOCTOU vector).

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH/MEDIUM:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | LOW | drift.go (deriveFamilyPrefix) | Strips only a TRAILING version segment, so a `<family>-<version>-<tier>` slug (`openai/gpt-5.4-mini`) derives to itself and a bindingless persona on it never groups with tier siblings — silently no-drift-forever (fails SAFE, documented) | CAPTURED → tech-debt-captured.md TD-006 (LOW). Fails safe (no false/cross-tier suggestion); bound personas unaffected (they use `ResolveModel`'s scan); all 10 shipping personas have trailing versions. Interior-version stripping is speculative complexity risking cross-tier bleed |
   | LOW | drift.go (bySlug vs scan) | Theoretical: duplicate catalog ids with differing expiration could make the deprecation lookup (first occurrence) and newer-member scan (all occurrences) disagree | FIXED inline in 5.12 (doc) — added a comment stating the snapshot is assumed ID-unique and first-occurrence-wins keeps the result deterministic; no behavior change (OpenRouter `/models` + the authored snapshot are id-unique) |

   **Action Taken:** 0 CRITICAL/HIGH/MEDIUM. One LOW (bindingless interior-version gap) is documented fail-safe conservatism → captured to `tech-debt-captured.md` (TD-006). One LOW (theoretical duplicate-id) addressed with a clarifying doc comment inline (5.12). ✅ Adversarial review passed.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.12, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.12 [x] **[Snapshot default — REFACTOR](plan/user-stories/05-atcr-models-check-drift-report.md)**
   1. Fix CRITICAL/HIGH issues from 5.11.A (if any) — none; captured 1 LOW (TD-006, documented fail-safe conservatism) + added 1 clarifying doc comment (duplicate-id determinism), no behavior change
   2. Improve quality, maintain green (T1), validate (T3) — ✅ personas suite green; `golangci-lint run` 0 issues
   3. COMMIT: `docs(personas): note snapshot ID-uniqueness assumption; capture TD-006`
   **Duration:** 1h

### 5.13 [x] **Phase 5 — DoD**
   - [x] All Phase 5 tests passing (T3); exit codes verified — `go test ./...` 0 failures; 0/1/2 exit contract covered (`TestModelsCheckExit_*`)
   - [x] Coverage ≥80%; zero live network (snapshot default) — personas 84.6%, cmd/atcr 84.2%; default path uses the embedded snapshot, `TestModelsCheck_DefaultPath_ZeroNetwork` (failRoundTripper) proves no HTTP call
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run` 0 issues, `go vet` clean, `gofmt -l` empty, `go build ./...` OK
   - [x] `models check` never invoked on the review path — grep of `internal/fanout/` + `internal/registry/` for `CheckDrift`/`SnapshotModels`/`runModelsCheck` empty; drift/snapshot are new upstream code only, review path (`ResolvePersona`, review fan-out) untouched
   - [x] DoD report per template

   ```
   Story-05 DoD Complete
   Auto: 3/3 (tests passing, lint/vet/fmt clean, build succeeds)
   Story-Specific (AC 05-01/02/03/04): all green
     05-01: `models check` registered next to personas (22-subcommand invariant updated); enumerates via ListTiers (matches personas list); 3 condition line-formats + multi-condition one-line-per-condition; canonical no-drift + distinct nothing-to-check/filter-no-match messages; per-persona read failure surfaced+excluded not aborting
     05-02: single shared personas.DriftFinding feeds table + --json; array one-object-per-condition; condition-specific fields via omitempty (no null); `[]` on empty; human/JSON (persona,condition) parity; control-char escaping
     05-03: 0 clean / 1 conditions-found (typed driftFoundError) / 2 usage|command-failure; findings+per-persona-read-failure stays 1; personasDir+snapshot load/parse failure → 2; identical exit codes across default and --json
     05-04: embedded snapshot default (zero network, go:embed compiled into production binary); repeated runs byte-identical; only non-null expiration_date → deprecation; missing→"failed to load", malformed→"failed to parse", both exit 2
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 5.14 [x] **Phase 5 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Phase 5 changed files + verbatim hostile-integrator checklist. Emphasize: the `--json`/exit-code contract is stable enough for Epic 19.8 to wrap; snapshot-default determinism. Output: ONLY the findings table.

   **Subagent confirmed:** `go build ./...`/`go vet` clean; all Phase 5 tests pass; `models` registers exactly once (main.go:203); no `internal/personas → cmd` import cycle; `models check` enumerates the SAME set as `personas list` (identical `filepath.Join(".atcr","personas")` + `ListTiers`); reuses `ResolveModel`/`lockMetaOf`/`personaPath` (no forked logic); single `DriftFinding` feeds both renderers; JSON `condition` values are stable constants; default path diagnostic-only, zero network, no lock writes; review hot path + AC7 gate + Phase 3/4 resolver/upgrade untouched.

   **Subagent findings (fresh-context hostile-integrator subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-----|
   | MEDIUM | models.go (runModelsCheck) | A machine consumer (Epic 19.8) cannot distinguish "clean" from "incompletely checked": `listErr` + per-persona `LoadLock` failures go only to stderr, never affecting exit code or the `--json` array, so `exit 0`/`[]` can be a false all-clear on partial enumeration | CAPTURED → tech-debt-captured.md TD-007 (MEDIUM). Per-persona-to-stderr/exit-unaffected is AC-MANDATED (AC 05-01 ES1 + AC 05-03 EC1); the structured-envelope fix CONTRADICTS AC 05-02's pinned array shape; escalating `listErr` diverges from sibling `personas list`. The machine-consumer incomplete-check contract belongs to Epic 19.8 (out of scope) |
   | LOW | drift.go (newerMemberFinding Family) | `family` JSON field carries two shapes: bound → bare token (`"glm"`), bindingless → vendor/tier prefix (`"z-ai/glm"`) | CAPTURED → TD-008 (LOW) + FIXED inline (doc) — added a `DriftFinding.Family` doc comment marking it advisory (not a stable grouping key) with different bound/bindingless provenance; normalization deferred to 19.8 (different provenance, lossless single shape non-trivial) |
   | LOW | snapshot.go / models.go | Embed sources from `testdata/`; `ATCR_CATALOG_SNAPSHOT` undocumented in `--help`; Phase 8 `refresh`-to-testdata won't reach an installed binary's embed | CAPTURED → TD-009 (LOW) + FIXED inline — documented `ATCR_CATALOG_SNAPSHOT` in `models check --help`; the single-fixture-shared-with-tests design is pinned by `catalog-snapshot-fixture.md` and the refresh-consumption model is explicitly Phase 8's story |

   **Action Taken:** 0 CRITICAL/HIGH → no gate re-run required. 1 MEDIUM + 2 LOW captured to `tech-debt-captured.md` (TD-007/008/009); the two LOWs additionally got cheap inline doc clarifications (family-field semantics + env-override help text) that reduce their impact now. ✅ **Phase gate passed.**

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

### 6.1 [x] **[Major-jump fixture gate + verify flag — RED](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-major-jump-fixture-gate-and-verify-flag.md)
   Write failing tests: a major jump (4.x→5.x) gates on the fixture re-passing AND surfaces an unconditional "prompt tuned for the prior major — verify" flag before the lock advances. Non-semver strings are NOT treated as a major-bump trigger (per `isNewer` precedent). **High-complexity AC.** Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 3h

### 6.2 [x] **[Major-jump fixture gate + verify flag — GREEN](plan/user-stories/06-major-bump-re-validation-gate.md)**
   Implement `semver.Major` classification + fixture gate + verify flag, reusing `isNewer`'s normalization. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): major-bump re-validation gate + verify flag (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 3h

### 6.2.A [x] **[Major-jump gate — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 6.2`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — non-semver/`v`-prefix strings; confirm the gate reuses `isNewer`'s exact normalization (no divergent parallel impl); the verify flag is unconditional on a major jump." Output: ONLY the findings table.

   **Findings (no CRITICAL/HIGH; 1 MEDIUM deferred → TD-010):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | upgrade.go isMajorJump vs isNewer default branch | `versionSegRe` accepts version-shaped non-semver tokens (4+ components, leading-zero dates); `isNewer` advances on string-inequality but `isMajorJump` returns false → theoretical silent cross-major advance. No real catalog slug can trigger it (all ≤3-component semver). | Deferred to TD-010 — fail-safe classifier or tighten `versionSegRe`; no shipping persona affected. |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 6.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md` ✅ captured as TD-010 in `tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 6.3 [x] **[Major-jump gate — REFACTOR](plan/user-stories/06-major-bump-re-validation-gate.md)** — no CRITICAL/HIGH from 6.2.A; impl already minimal (shared `normalizeSemver`, single-purpose helpers); gofmt/vet/T3 clean; no code change → no refactor commit
   1. Fix CRITICAL/HIGH issues from 6.2.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up major-bump gate"`
   **Duration:** 1h

### 6.4 [x] **[Minor-jump auto-lock regression guard — RED](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **AC:** [06-02](plan/acceptance-criteria/06-02-minor-jump-auto-lock-regression-guard.md)
   Write failing tests: a minor jump (4.8→4.9) auto-locks with no verify flag; regression guard on `isNewer` behavior. Verify fail correctly.
   **Files:** `internal/personas/upgrade_test.go` | **Duration:** 2h

### 6.5 [x] **[Minor-jump auto-lock — GREEN](plan/user-stories/06-major-bump-re-validation-gate.md)** — no production code needed (6.2 gate's `isMajorJump` classification already excludes minor jumps from the fixture check); deliverable is the regression-guard tests, committed
   Implement minor-jump auto-lock. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(personas): minor-jump auto-lock (green)"`
   **Files:** `internal/personas/upgrade.go` | **Duration:** 1h

### 6.5.A [x] **[Minor-jump auto-lock — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-major-bump-re-validation-gate.md)**
   **Changed Files:** `internal/personas/upgrade.go`, `internal/personas/upgrade_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 6.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm a minor jump never triggers the verify flag and a major jump never auto-locks — no boundary off-by-one." Output: ONLY the findings table.

   **Findings — a SECOND independent reviewer converged on the same TD-010 divergence; escalated to inline fix (maps to Story 06's HIGH-impact "silent cross-major advance" risk):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM→fixed | upgrade.go isMajorJump vs isNewer | Non-semver version-shaped tokens (4+ components / leading zeros): `isNewer` advances on string-inequality but `isMajorJump` returned false → v4→v5 could auto-lock without the fixture gate or verify flag. Boundary/ordering otherwise clean. | Fixed inline: `isMajorJump` now falls back to `leadingMajor` numeric-component compare for non-semver tokens; establish-from-empty & alias stay ungated. TD-010 marked Resolved. Guard: `TestUpgrade_NonSemverMajorJumpStillGates` + extended `TestIsMajorJump`. |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 6.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md` — escalated: fixed inline in 6.6 (two-reviewer convergence + Story-06 HIGH-impact risk); TD-010 Resolved
   - None found → Note "Adversarial review passed" and proceed

### 6.6 [x] **[Minor-jump auto-lock — REFACTOR](plan/user-stories/06-major-bump-re-validation-gate.md)** — fixed the escalated 6.5.A finding (TD-010 fail-safe `leadingMajor` fallback); T3 + gofmt/vet clean; committed `fix(personas): fail-safe major-jump classification…`
   1. Fix CRITICAL/HIGH issues from 6.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): clean up minor-jump auto-lock"`
   **Duration:** 1h

### 6.7 [x] **Phase 6 — DoD**
   - [x] All Phase 6 tests passing (T3); major/minor boundary verified — full `go test ./...` green; boundary covered by `TestIsMajorJump` + `TestUpgrade_{MajorJump*,MinorJump,NoChange,NonSemver}`
   - [x] Coverage ≥80% — personas 85.2%, cmd/atcr 84.2%; new gate fns (isMajorJump/leadingMajor/normalizeSemver/fixturePassed/fixtureBlockReason/isNewer) 100%
   - [x] Lint/vet/fmt clean; build succeeds — golangci-lint 0 issues; `go vet` clean; gofmt clean; pre-commit build passed
   - [x] `isNewer` normalization reused unmodified (no fork) — extracted shared `normalizeSemver`; isNewer behavior byte-identical (regression: `TestIsNewer_MixedValidityTreatsAsUpToDate` green); `isMajorJump` consumes the same normalized string
   - [x] DoD report per template — mini-report below

### 6.8 [x] **Phase 6 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 6.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 6 gate review`
   - prompt: Phase 6 changed files + verbatim hostile-integrator checklist. Emphasize: gate composes with Phase 4's upgrade path; `isNewer` reuse; verify flag human-facing. Output: ONLY the findings table.

   **Findings — none (all seams A–F confirmed clean): Phase 4 paths intact; single `normalizeSemver`; verify flag human-facing on every major jump and never on minor; seam var non-leaking; per-persona `--all` independence; exit-code coherence.**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | — | — | none | — |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop ✅ Phase gate passed
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 6 complete. Await review before Phase 7.

---

## Phase 7: Roster Reconciliation — init/quickstart (1.5 days)

**Story:** [07: init/quickstart Roster Reconciliation](plan/user-stories/07-init-quickstart-roster-reconciliation.md)
**Focus:** Closes 19.6's deferred TD-011 HIGH per the **locked Option B decision** — derive the fetch-and-pin roster from the community index's own fetched entries instead of the hardcoded `builtins.Names()` list, fixed once in a shared location to avoid the TD-006/TD-007 two-call-site drift pattern. Independent of Phases 1-6.

### 7.1 [x] **[Working non-empty community roster — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-01](plan/acceptance-criteria/07-01-working-nonempty-community-roster.md)
   Write failing tests: online `init`/`quickstart` install a working, non-empty community persona set derived from the fetched index (not `builtins.Names()`). Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 3h

### 7.2 [x] **[Working non-empty roster — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Derive the roster from the single existing `FetchIndex` call inside `installCommunityPersonas`. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(cmd): derive init/quickstart roster from fetched index (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** 3h

### 7.2.A [x] **[Working non-empty roster — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.2`
   - prompt: Files above + verbatim checklist, plus: "Confirm no additional network round-trip introduced; all-or-nothing rollback / skip-then-continue behavior preserved; no new two-call-site drift (single shared reconciliation point)." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | init.go (installCommunityPersonas nil-roster path) | Derived roster installs untrusted index `e.Name` values (FetchIndex does not validate); a malformed published entry (empty/invalid name) trips the all-or-nothing rollback and aborts online init/quickstart for all users — the derived path has no per-name skip-with-warning tolerance | Captured → tech-debt-captured.md TD-011. Reviewer's "skip-on-InstallUnit-failure" fix REJECTED (all-or-nothing rollback is AC 07-03 EC1-mandated + regression-locked). Only the narrower empty/invalid-NAME pre-filter is the deferrable robustness item; no path-traversal write possible (InstallUnit→personaPath validates + fails closed) |
   | LOW | init.go (rbCandidate append) | `rbCandidate.preExisted` re-stat is always false (reached only after the exists-guard `continue`) — dead bookkeeping | Captured → tech-debt-captured.md TD-012 (pre-existing 19.6 code, untouched by this phase; story mandates preserving installCommunityPersonas mechanics) |

   **Subagent verdict (confirmed clean):** no additional network round-trip (nil path reuses the already-fetched `entries`); all-or-nothing rollback + never-overwrite guard preserved; exactly ONE reconciliation point (both call sites pass nil); genuine-absence skip-warning path still reachable via a non-nil roster; nil-vs-empty roster, duplicate entries, and ordering all handled.

   **Action Taken:** No CRITICAL/HIGH. 1 MEDIUM + 1 LOW captured to `tech-debt-captured.md` (TD-011/TD-012) — neither an inline fix (MEDIUM's core is AC-mandated behavior; LOW is pre-existing untouched code). ✅ Adversarial review passed, proceeding.

### 7.3 [x] **[Working non-empty roster — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.2.A (if any) — none (0 CRITICAL/HIGH; the MEDIUM/LOW were deferred to TD-011/TD-012, not inline fixes)
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite `go test ./...` PASS
   3. COMMIT: no refactor commit — the GREEN implementation is already minimal (nil-sentinel derivation + doc comment); no code change to make. No empty/no-op commit created (matches 2.3/2.9 precedent).
   **Duration:** 1h

### 7.4 [x] **[No misleading skip warnings — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-02](plan/acceptance-criteria/07-02-no-misleading-skip-warnings.md)
   Write failing tests: no `not found in community index — skipping` warnings emitted for the reconciled roster. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 2h
   **Note — transparent vacuous RED (mirrors 2.4/2.7):** AC 07-02 ("no misleading skip warnings") is satisfied by the SAME line of code as AC 07-01 — deriving the roster from the fetched index (7.2 GREEN) means every roster member is present in the index by construction, so the skip-warning cannot fire. The 4 new tests therefore PASS on first run against the REAL `personas/community/index.json` (served via a new `realCommunityServer` helper anchored by `runtime.Caller`, robust to `t.Chdir`): `TestInstallCommunityPersonas_NilRoster_NoSkipWarnings_RealIndex`, `TestInit_Online_NoSkipWarnings` (asserts stderr via `executeSplit`), `TestQuickstart_Online_NoSkipWarnings`, `TestInstallCommunityPersonas_NeverOverwriteWarningDistinct` (AC 07-02 EC2 — the never-overwrite notice still prints and is not conflated). They are permanent guards: any revert to a hardcoded index-disjoint roster re-fires the warnings → red. The discriminating counterpart `TestInstallCommunityPersonas_MissingRosterSkipsWithWarning` proves the warning path still fires for a genuinely-absent name (non-nil roster), so these are not an always-green tautology.

### 7.5 [x] **[No misleading skip warnings — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Remove the source of the misleading warning under the reconciled roster. (T1), verify all pass (T2), COMMIT: `git commit -m "fix(cmd): drop misleading skip warnings under reconciled roster (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` | **Duration:** 1h
   **Done:** No new production code — the misleading warning was already eliminated by 7.2's index-derived roster (every derived name is in the index → the `init.go:129` skip path is unreachable for the nil-roster production path). GREEN commit is honestly `test(cmd)` (test-only regression guards), not `fix(cmd)`, since no production line changed here: `test(cmd): assert zero misleading skip warnings under index-derived roster (green)` (`2bcf6adb`). T2 `go test ./cmd/atcr/...` PASS. The skip-warning code stays as defensive dead-path handling (still reachable/tested via an explicit non-nil roster).

### 7.5.A [x] **[No misleading skip warnings — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, `cmd/atcr/init_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm legitimate skip cases (genuine absence) still warn; only the misleading roster/index-disjoint warning is removed." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH/MEDIUM:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | init_test.go (MissingRosterSkipsWithWarning) | The genuine-absence discriminator asserted `NoFileExists("tracer.yaml")` — a name never in the roster/index, so vacuously true; the intended invariant (absent `penny` not written) went unverified | FIXED inline in 7.6 — asserts `NoFileExists("penny.yaml")` (the actually-absent roster name), strengthening the discriminator my 07-02 anti-tautology argument depends on |
   | LOW | init_test.go / quickstart_test.go (`*_Online_NoSkipWarnings`) | Negative-only against the real index; a silent empty-roster regression (0 warnings + 0 installs) would pass | FIXED inline in 7.6 — added `assert.NotEmpty(communityPinNames(...))` positive install guard to both command-level tests |
   | LOW | init_test.go (realCommunityServer) | `runtime.Caller` anchor is package-relative under `go test -trimpath`; mitigated (fails loud, never false-green) | Documented inline (assumption comment) rather than a speculative go.mod walker — the project's CI + hooks run `go test -race ./...` with NO -trimpath (verified), and the helper fails loud if the assumption ever breaks; a walker would be code for a mode this project never uses |

   **Subagent verdict (confirmed clean):** never-overwrite-vs-skip distinction genuinely tested (disjoint substrings, both asserted); genuine-absence warning coverage survives via `MissingRosterSkipsWithWarning`; negative `NotContains` assertions not too loose (cannot mask the never-overwrite notice); anti-tautology holds (a `builtins.Names()` revert re-fires warnings → red; empty-roster revert caught by `NotEmpty`).

   **Action Taken:** No CRITICAL/HIGH/MEDIUM. 3 LOW — all on freshly-authored / load-bearing guard code → fixed/documented inline in 7.6 (`6400b465`) rather than deferred, consistent with this sprint's inline-LOW precedent (2.5.A/2.8.A/3.2.A). ✅ Adversarial review passed.

### 7.6 [x] **[No misleading skip warnings — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.5.A (if any) — none; fixed 2 LOW test-quality gaps + documented 1 LOW harness assumption inline (own/load-bearing test code)
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite `go test ./...` PASS; `golangci-lint run ./cmd/atcr/` 0 issues; `go vet`/`gofmt` clean
   3. COMMIT: `test(cmd): strengthen roster-reconciliation guards (positive install + genuine-absence)` (`6400b465`) — test-only (no production change this element; the fix rode 7.2)
   **Duration:** 1h

### 7.7 [x] **[Shared reconciliation point + backward compat — RED](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **AC:** [07-03](plan/acceptance-criteria/07-03-shared-reconciliation-point-and-backward-compat.md)
   Write failing tests: the roster derivation lives in ONE shared location (both `init` and `quickstart` call it — no drift); existing on-disk personas remain backward-compatible. Verify fail correctly.
   **Files:** `cmd/atcr/init_test.go`, `cmd/atcr/quickstart_test.go` | **Duration:** 2h
   **Note — transparent vacuous RED (mirrors 2.4/2.7):** AC 07-03 (single shared reconciliation point) is already satisfied by 7.2 — the derivation lives INSIDE `installCommunityPersonas` and both call sites pass `nil`, so the two cannot drift. 3 new guards PASS on first run: `TestRosterReconciliation_InitQuickstartParity` (drives BOTH real call paths against the same real index and asserts an identical installed set — the TD-006/TD-007 drift guard), `TestInstallCommunityPersonas_NilRoster_MidRosterFailure_RollsBack` (all-or-nothing rollback preserved under the nil roster), `TestInit_BuiltinScaffoldUntouchedByCommunityInstall` (EC2 — built-in `.md` scaffolds intact + community units land in the separate pin dir, decoupled). The never-overwrite-under-nil-roster bullet is already covered by 7.4's `TestInstallCommunityPersonas_NeverOverwriteWarningDistinct`. Guards fail red if either call site diverges, rollback regresses, or the scaffold/community dirs are conflated.

### 7.8 [x] **[Shared reconciliation point — GREEN](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   Extract a single shared reconciliation function; wire both call sites to it. (T1), verify all pass (T2), COMMIT: `git commit -m "refactor(cmd): single shared roster-reconciliation point (green)"`
   **Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` (+ shared helper) | **Duration:** 2h
   **Done:** No new production code / no separate helper extracted — the single shared reconciliation point already lives INSIDE `installCommunityPersonas` (the one routine both `init.go` and `quickstart.go` call), with both passing `nil` (landed in 7.2). AC 07-03 explicitly permits "inside `installCommunityPersonas` itself"; a standalone helper would be extra code for no benefit (minimum-code rule). GREEN commit is honestly `test(cmd)` (07-03 guards), not `refactor(cmd)`: `test(cmd): guard single shared roster-reconciliation point + backward compat (green)` (`e5b70834`). T2 `go test ./cmd/atcr/...` PASS.

### 7.8.A [x] **[Shared reconciliation point — ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   **Changed Files:** `cmd/atcr/init.go`, `cmd/atcr/quickstart.go`, shared helper, tests

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 7.8`
   - prompt: Files above + verbatim checklist, plus: "Confirm there is exactly ONE reconciliation point (no TD-006/TD-007 two-call-site drift); backward compat with existing on-disk personas." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH/MEDIUM:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | init_test.go (InitQuickstartParity) | Asserted "both call sites agree + non-empty" but not "index-derived" — would miss both sites changed to the SAME non-nil hardcoded roster | FIXED inline in 7.9 — also asserts the installed set equals the fetched index's own entry names (`FetchIndex` → sorted), so the guard is "index-derived," not merely "the two agree" |
   | LOW | init_test.go (NilRoster_MidRosterFailure_RollsBack) | A reverted Option B (nil→install-nothing→returns nil) fails `require.Error` for the WRONG reason, so a green did not isolate "rollback works" | FIXED inline in 7.9 — tightened to `Contains(err, 'failed to install community persona "c"')`, proving the derived roster reached c and the rollback fired (isolated from an empty-roster no-op) |
   | LOW | init_test.go (BuiltinScaffoldUntouched) | Decoupling assertion was near-tautological — the test itself pointed `personasDir` at a separate temp dir, so community units structurally could not collide with scaffolds | FIXED inline in 7.9 — dropped the `personasDir` override; the pin dir is now production-resolved (`$HOME/.config/atcr/personas`), so the `NoFileExists` routing/decoupling assertion genuinely exercises real behavior |

   **Subagent verdict (confirmed clean):** exactly ONE reconciliation point (both sites pass nil; real index disjoint from `builtins.Names()` so a single-site revert → empty → guards fail red); backward compat preserved; no flakiness (no `t.Parallel()`; `realCommunityServer`/`personasDir`/`ATCR_PERSONAS_URL` ordering correct — real dir resolved before `t.Chdir`, globals restored via `t.Cleanup`).

   **Action Taken:** No CRITICAL/HIGH/MEDIUM. 3 LOW — all hardening MY freshly-authored guards (each strengthens a load-bearing anti-tautology assertion) → fixed inline in 7.9 (`a130a1ac`), consistent with this sprint's inline-LOW precedent. ✅ Adversarial review passed.

### 7.9 [x] **[Shared reconciliation point — REFACTOR](plan/user-stories/07-init-quickstart-roster-reconciliation.md)**
   1. Fix CRITICAL/HIGH issues from 7.8.A (if any) — none; fixed 3 LOW test-tautology gaps inline (own guard code)
   2. Improve quality, maintain green (T1), validate (T3) — ✅ full suite `go test ./...` PASS; `golangci-lint run ./cmd/atcr/` 0 issues; `go vet`/`gofmt` clean
   3. COMMIT: `test(cmd): tighten 07-03 guards (index-derived parity, isolated rollback, real routing)` (`a130a1ac`) — test-only (the shared point already landed in 7.2)
   **Duration:** 1h

### 7.10 [x] **Phase 7 — DoD**
   - [x] All Phase 7 tests passing (T3) — `go test ./...` full suite exit 0
   - [x] Coverage ≥80% — `cmd/atcr` 84.2% of statements
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run ./cmd/atcr/` 0 issues, `go vet ./...` clean, `gofmt -l cmd/atcr/` empty, `go build ./...` exit 0
   - [x] 19.6 TD-011 HIGH closed; single reconciliation point; backward compat — roster derived from the fetched index inside the single `installCommunityPersonas` (both call sites pass nil, no drift); existing on-disk personas preserved (never-overwrite guard); all-or-nothing rollback intact
   - [x] DoD report per template

   ```
   Story-07 DoD Complete
   Auto: 3/3 (tests passing, lint/vet/fmt clean, build succeeds)
   Story-Specific (AC 07-01/02/03): 3/3 + 3/3 + 4/4
     07-01: online init installs a non-empty index-derived roster; online quickstart installs the identical set via the same shared source; roster tracks index contents (grow-index test, no code change)
     07-02: zero misleading skip-warnings for online init (stderr) + quickstart against the REAL index; never-overwrite notice still prints, disjoint from the skip-warning, hand-edited unit byte-untouched
     07-03: init + quickstart resolve to the identical index-derived roster (parity == fetched index names); never-overwrite guard + all-or-nothing rollback preserved under the nil roster; built-in .md scaffolds decoupled from community units (production-resolved pin dir)
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 7.11 [x] **Phase 7 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 7.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 7 gate review`
   - prompt: Phase 7 changed files + verbatim hostile-integrator checklist. Emphasize: single shared reconciliation point; no new network round-trip; rollback/skip behavior preserved; backward compat. Output: ONLY the findings table.

   **Subagent findings (fresh-context hostile-integrator subagent) — 0 CRITICAL/HIGH/MEDIUM:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | init.go (config roster) / quickstart.go (synthetic registry) | Online init/quickstart pin the 10 index-derived community personas into the pin dir, but `.atcr/config.yaml`'s roster stays `builtins.Names()` — the community set is an available POOL, not active on default `atcr review` until the user edits the roster | Captured → tech-debt-captured.md TD-013. WITHIN the LOCKED Option B contract (not a regression; asserted by AC 07-01/07-03) — a future roster-wiring decision, not a defect |

   **Gate verdict (verified clean):** exactly ONE reconciliation point (`installCommunityPersonas`, both call sites pass nil — no TD-006/TD-007 drift); NO new network round-trip (nil roster reuses the already-fetched `entries`, single FetchIndex call); `builtins.Names()` still used unchanged for the embedded scaffold + synthetic registry (decoupled); empty-index hard error fires BEFORE nil derivation (a nil roster can never silently derive to empty); all-or-nothing rollback + never-overwrite guard preserved; genuine-absence skip-warning still reachable via a non-nil roster; backward compat intact (all pre-existing tests pass).

   **Action Taken:** No CRITICAL/HIGH/MEDIUM. 1 LOW captured to `tech-debt-captured.md` (TD-013, a within-scope coherence caveat). ✅ **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 7 complete. Await review before Phase 8.

---

## Phase 8: Integration & Docs (2 days)

**Story:** [08: Catalog Snapshot Fixture, Refresh Command & Documentation](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)
**Focus:** Depends on Phases 2, 3, 5. Author the checked-in catalog snapshot fixture covering every resolver branch; build `atcr models refresh` (maintainer-initiated, never CI-invoked); update `docs/personas-authoring.md`/`docs/personas-install.md`.

### 8.1 [x] **[Checked-in catalog snapshot coverage — RED](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-01](plan/acceptance-criteria/08-01-checked-in-catalog-snapshot-coverage.md)
   Write failing tests asserting the fixture covers every resolver branch (aliases, `created`-timestamp candidates, expiring models, preview tokens, all 10 pinned slugs, `z-ai/` prefix). Verify fail correctly.
   **Files:** `internal/personas/catalog_test.go` | **Duration:** 2h

### 8.2 [x] **[Checked-in catalog snapshot coverage — GREEN](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   Author `internal/personas/testdata/catalog_snapshot.json` covering all branches. (T1), verify all pass (T2), COMMIT: `git commit -m "test(personas): checked-in catalog snapshot fixture (green)"`
   **Files:** `internal/personas/testdata/catalog_snapshot.json` | **Duration:** 3h

### 8.2.A [x] **[Snapshot coverage — ADVERSARIAL REVIEW (subagent)](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **Changed Files:** `internal/personas/testdata/catalog_snapshot.json`, `internal/personas/catalog_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 8.2`
   - prompt: Files above + verbatim checklist, plus: "EDGE CASES — is every resolver branch exercised by the fixture (missing `created`, null/non-null `expiration_date`, preview token, `z-ai/`)? Any branch untested?" Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | catalog_snapshot.json (scan prefixes) | No scan-prefix member carries an absent/zero `created`, so the resolver's `m.Created <= 0` ineligibility branch (AC 03-02 EC4) is never driven by the CHECKED-IN fixture (only synthetic `cm(id,0)` unit tests). AC 08-01's "single source exercises every branch" is incomplete for this branch. | FIXED inline in 8.3 — added `deepseek/deepseek-legacy` (`created:0`) to the fixture + `TestCatalogSnapshot_CoversIneligibleCreatedUnderScanPrefix` asserting it is present yet excluded from @stable/@latest selection against the real fixture. |
   | LOW | catalog_test.go:252-256 | Stale comment: after the fixture addition the newest deepseek is `deepseek-v5-pro` (deprecation-excluded), but the inline rationale still attributed the @stable/@latest `→ v4-pro` result solely to `v3.2-exp` being preview-excluded. Assertion still correct; documentation value weakened. | FIXED inline in 8.3 — updated the comment to note v5-pro is the newest and deprecation-excluded, v3.2-exp preview-excluded → v4-pro. |
   | LOW | catalog_snapshot.json (google gemini preview entry) | `google/gemini-2.5-flash-lite-preview-09-2025` (preview+expiring) is inert for resolver-branch selection: `google/` is alias-only, never reached by the created-timestamp scan. | NO ACTION — pre-existing fixture content (not this phase's addition); it still provides valid preview+expiring PARSER/schema coverage (a realistic `~`-less preview+expiring row). Preview/deprecation SELECTION branches are now driven under real scan prefixes by qwen4-preview + deepseek-v5-pro. Left as realistic catalog noise. |

   **Action Taken:** No CRITICAL/HIGH. The MEDIUM + one LOW were gaps/staleness in this phase's own freshly-authored fixture/test → fixed inline in 8.3 (consistent with 3.2.A/3.5.A precedent). One LOW is pre-existing, harmless, and retains schema-coverage value → no action, rationale recorded. No tech-debt captured.

### 8.3 [x] **[Snapshot coverage — REFACTOR](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   1. Fix CRITICAL/HIGH issues from 8.2.A (if any)
   2. Improve fixture/test quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): tighten snapshot fixture coverage"`
   **Duration:** 1h

### 8.4 [x] **[models refresh regenerates snapshot — RED](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-02](plan/acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md)
   Write failing tests: `atcr models refresh` regenerates the snapshot from a live fetch; it is maintainer-initiated and NEVER CI-invoked. Verify fail correctly.
   **Files:** `cmd/atcr/models_test.go` | **Duration:** 2h

### 8.5 [x] **[models refresh — GREEN](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   Implement the `refresh` subcommand. (T1), verify all pass (T2), COMMIT: `git commit -m "feat(models): refresh command regenerates snapshot (green)"`
   **Files:** `cmd/atcr/models.go` | **Duration:** 2h

### 8.5.A [x] **[models refresh — ADVERSARIAL REVIEW (subagent)](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **Changed Files:** `cmd/atcr/models.go`, `cmd/atcr/models_test.go`

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 8.5`
   - prompt: Files above + verbatim checklist, plus: "Confirm `refresh` is the ONLY live-network touchpoint and cannot be triggered in CI; slug validation applied to fetched data before writing the fixture." Output: ONLY the findings table.

   **Subagent findings (fresh-context general-purpose subagent) — 0 CRITICAL:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | models.go (WriteFile) | Non-atomic `os.WriteFile` opens `O_TRUNC` — truncates the existing fixture BEFORE writing, so a partial write (ENOSPC/kill/I/O) leaves the checked-in snapshot empty/corrupt, not "untouched on write error" (violates AC 08-02 req 3). | FIXED inline in 8.6 — write via new `personas.WriteSnapshot` (reuses `writeFileAtomic`: temp file + rename in the same dir; the prior file survives any marshal/write failure). Added `TestModelsRefresh_WriteFailure_PreservesExistingFixture` (read-only dir → temp create fails, prior fixture byte-for-byte intact). |
   | MEDIUM | models.go (key gate) | CI-safety rested only on key-presence; this repo's CI exports `OPENROUTER_API_KEY`, so a live-path run with the key set + no `ATCR_CATALOG_URL` would fetch live — defeating "never CI-invoked" (AC/DoD load-bearing). | FIXED inline in 8.6 — added an explicit CI guard: on the live path (no override) refuse when `CI`/`GITHUB_ACTIONS` is set, exit 2, even with a key present. `TestModelsRefresh_RefusesInCI` asserts it. |
   | MEDIUM | models.go (empty guard) | `len(models)==0` guard is length-only; a substanceless `{"data":[{}]}` (len 1, blank id) passed and clobbered the good fixture. | FIXED inline in 8.6 — `substantiveModelCount` requires ≥1 entry with a non-empty id; `TestModelsRefresh_RefusesEmptyCatalog` now table-tests empty-array + blank-entry + empty-id-entry. |
   | MEDIUM | models_test.go (round-trip) | Round-trip test asserted only `Len==3`; a MarshalSnapshot regression zeroing `Created`/dropping `canonical_slug`/collapsing nil↔"" expiration would pass silently. | FIXED inline in 8.6 — refreshCatalogBody now carries a non-null-expiration entry; the test asserts `Created`, `CanonicalSlug`, and nil-vs-non-null `ExpirationDate` fidelity. |
   | LOW | models.go (default output) | Default `--output` is repo-relative; an `ATCR_CATALOG_URL` override run with no `--output` from repo root could rewrite the real fixture from an arbitrary catalog (latent — all tests pass `--output`). | FIXED inline in 8.6 — under an override, `--output` is REQUIRED (refuse the default), exit 2. `TestModelsRefresh_DefaultOutputUnderOverride_Requires_Output` asserts it. |
   | LOW | models.go (fetched ids) | Fetched catalog ids are written to the fixture without `validateResolvedSlug`; the GET is unauthenticated, so a MITM/compromised upstream could persist garbage slugs (no file-injection — JSON-escaped). | DEFERRED → tech-debt-captured.md TD-014. Not exploitable today (fixture is human-reviewed in the PR diff before commit; no shipping `binding:` persona resolves against a refreshed catalog); a validate-and-skip pass would change the "faithfully snapshot the live catalog" contract speculatively. |

   **Action Taken:** No CRITICAL. The HIGH + 3 MEDIUM + 1 LOW were fixed inline in 8.6 (atomic write, CI guard, substantive-empty guard, round-trip field assertions, default-output-under-override guard) — all on this phase's own freshly-authored code and several strengthening the AC-load-bearing "never CI-invoked" / "fixture untouched on error" guarantees. One LOW (slug validation on write) deferred to TD-014 with rationale. `golangci-lint`/`vet`/`gofmt` clean; full suite green.

### 8.6 [x] **[models refresh — REFACTOR](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   1. Fix CRITICAL/HIGH issues from 8.5.A (if any)
   2. Improve quality, maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(models): clean up refresh command"`
   **Duration:** 1h

### 8.7 [x] **[Docs: family/channel/lock + reproducibility — DOCUMENTATION](plan/user-stories/08-catalog-snapshot-refresh-command-and-docs.md)**
   **AC:** [08-03](plan/acceptance-criteria/08-03-docs-family-channel-lock-and-reproducibility.md)
   1. Update `docs/personas-authoring.md` and `docs/personas-install.md` to document the family/channel/lock model and the reproducible-by-default vs. explicit-upgrade behavior.
   2. Include `atcr personas upgrade`, `atcr models check`, and `atcr models refresh` usage.
   3. Verify examples match real command output and the real roster (`anthony, sonny, gene, milo, gia, flint, delia, quinn, celeste, glenna`).
   4. COMMIT: `git commit -m "docs(personas): document family/channel/lock model + reproducibility"`
   **Files:** `docs/personas-authoring.md`, `docs/personas-install.md` | **Duration:** 3h

### 8.8 [x] **Phase 8 — DoD**
   - [x] `go test ./...` passes with ALL resolver/catalog tests backed by the checked-in snapshot (zero live network in CI) — full suite exit 0; every new resolver/catalog/refresh test uses `httptest` + `testdata/catalog_snapshot.json` (grep confirms no live-OpenRouter dial in test files)
   - [x] Coverage ≥80% — personas 83.8%, cmd/atcr 84.4%
   - [x] Lint/vet/fmt clean; build succeeds — `golangci-lint run` 0 issues, `go vet` clean, `gofmt -l` empty, `go build ./...` OK
   - [x] Docs updated and accurate; `refresh` never CI-invoked — authoring §6 + install `models`/reproducibility sections added, all links resolve; refresh fails closed under CI env AND on a missing key (two independent gates), not wired into any CI script
   - [x] DoD report per template

   ```
   Story-08 DoD Complete
   Auto: 3/3 (tests passing, no lint errors, build succeeds)
   Story-Specific (AC 08-01/02/03): 3/3 + 4/4 + 4/4
     08-01: fixture at testdata/catalog_snapshot.json covers aliases, ≥2 members per created-scan prefix, all 10 pins, z-ai/ (never glm/), preview-under-prefix (EC1), expiring-newest-under-prefix (EC2), created<=0 ineligible; resolver tests run via httptest zero-live-network
     08-02: `models refresh` registered under `models`; fetches /models + writes the fixture (atomic); errors (missing key, CI env, empty/blank catalog, fetch failure, unwritable path, default-under-override) reported + existing fixture untouched; written file round-trips through SnapshotModels (field fidelity asserted)
     08-03: personas-authoring.md binding/lock/zero-migration section; personas-install.md upgrade lock report + major-bump verify flag + models check/refresh + reproducibility; sections link to plan documentation/; all links resolve
   Manual Review: [ ] Code reviewed (deferred to /execute-code-review)
   ```

### 8.9 [x] **Phase 8 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 8 + full-epic integration.

   **Spawn a fresh subagent** via the Agent tool. Do NOT review inline.
   - subagent_type: `general-purpose`
   - description: `Phase 8 gate review`
   - prompt: Phase 8 changed files + verbatim hostile-integrator checklist. Emphasize: zero live network in CI holds end-to-end; docs match real behavior; refresh is maintainer-only. Output: ONLY the findings table.

   **Subagent findings (fresh-context hostile-integrator subagent) — 0 CRITICAL/HIGH/MEDIUM; 3 LOW:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | docs/personas-install.md (models check example) | The `models check` example printed 3 drift/deprecation/missing lines and cited slugs not in the fixture (`anthropic/claude-opus-5.0`, glenna mislocked to `glm-4.5`); the shipped roster actually prints the clean message, so a first-run user sees output contradicting the doc. | FIXED inline — labeled the block explicitly as an illustrative/hypothetical example (real slugs anthony/glenna/quinn, hypothetical drift), and stated the shipped roster prints the clean message today. |
   | LOW | internal/personas/snapshot.go (MarshalSnapshot) | `MarshalSnapshot` emitted only `{"data":[…]}`, so `atcr models refresh` silently dropped the fixture's `_fixture_meta` provenance header — which the fixture's own note advertises refresh as the way to regenerate. `data` round-trips fine; only provenance was lost. | FIXED inline — `MarshalSnapshot` now re-emits `_fixture_meta` (note + today's UTC fetch date + source `<CatalogBaseURL>/models`), ignored on read; `TestModelsRefresh_WritesFixtureFromLiveFetch` now asserts the header + source are present. |
   | LOW | AC 08-02 Security vs cmd/atcr/models.go | AC 08-02 Security prose says the command "sends OPENROUTER_API_KEY as a Bearer token," but the code uses the key only as a local maintainer/CI presence-gate and never transmits it (the catalog GET is unauthenticated per the Phase 1 clarification). Code is correct + safer; the AC prose is the stale part. | DEFERRED → tech-debt-captured.md TD-016. Editing the AC mid-execution is avoided (AC files are being edited by a concurrent session); reconciliation belongs to `/execute-code-review` / a maintainer. |

   **Gate verdict (verified clean):** round-trip fidelity intact (`data` parses identically via SnapshotModels; nil vs non-null `expiration_date` preserved); the 3 fixture additions (deepseek-v5-pro expiring+newest, qwen4-preview preview+newest, deepseek-legacy created:0) change NO other resolver/drift result — delia→deepseek-v4-pro, quinn→qwen3.7-plus (@stable), glenna→z-ai/glm-5.2 all unchanged; review-path lock invariant (zero endpoint calls) intact; refresh cannot run in CI (CI-env refusal + key gate, no CI wiring) and cannot clobber the real fixture in tests (override requires `--output`); AC7 provider/model gate, major-bump gate, and created-timestamp resolver all unweakened; docs links resolve.

   **Action Taken:** No CRITICAL/HIGH/MEDIUM. 2 LOW fixed inline (doc accuracy + refresh provenance), 1 LOW deferred to TD-016 (AC-text reconciliation). Post-fix: `go test ./...` green, `golangci-lint`/`vet`/`gofmt` clean. ✅ **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 8 complete. Proceed to Final Phase validation.

---

## Final Phase: Validation

### Validation Checklist
- [x] All tests passing (T3: `go test ./...`) — full suite exit 0
- [x] Coverage meets threshold (≥80%, `go test -coverprofile=coverage.out ./...`) — 89.1% repo total
- [x] Lint/format clean (`golangci-lint run`, `go vet ./...`, `gofmt`/`goimports`) — 0 issues / clean / clean
- [x] Build succeeds (`go build ./...`)
- [x] Zero live network in CI (httptest + checked-in snapshot) — no live-OpenRouter dial in any test; `models refresh` gated off in CI
- [x] All 8 ACs from original-requirements.md satisfied (AC1–AC8) — see Drift Analysis below

### Optional: Targeted Mutation Testing
Mutation tooling is **UNAVAILABLE** in this project (no `cargo-mutants`/`mutmut`/`stryker`). Skip mutation testing. If a Go mutation tool (e.g. `go-mutesting`) is added later, target ONLY the high-risk changed files (`internal/personas/catalog.go`, `internal/personas/upgrade.go`).
**WARNING:** Do NOT run full codebase mutation — it can take hours. Target specific files only.

### Drift Analysis
Compare the delivered implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [x] AC1: alias routability confirmed/refuted + `@stable` heuristic recorded (Phase 1)
- [x] AC2: family/channel binding → locked slug; review runs the lock, zero endpoint call (Phase 2)
- [x] AC3: 7 alias-covered personas + created-timestamp for DeepSeek/Qwen/GLM (`z-ai/`) + explicit-pin never floats (Phase 3)
- [x] AC4: `personas upgrade` advances lock + before→after report; no silent runtime change (Phase 4)
- [x] AC5: `models check [--json]` drift/deprecation/missing + exit codes (Phase 5)
- [x] AC6: major-bump gate + verify flag; minor auto-locks (Phase 6)
- [x] AC7: init/quickstart working non-noisy roster; 19.6 HIGH closed; backward compat (Phase 7)
- [x] AC8: `go test ./...` green with checked-in snapshot (zero live network); docs updated (Phase 8)

**Next:** `/execute-code-review @.planning/sprints/active/19.7_live_model_resolution/`
