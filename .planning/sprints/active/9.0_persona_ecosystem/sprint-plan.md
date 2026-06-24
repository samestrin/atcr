# Sprint 9.0: Persona Ecosystem

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 9.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting — EXCEPT this sprint runs in **gated mode**: stop at each phase-boundary GATE (`N.LAST`) for review before continuing.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Expand ATCR's reviewer panel beyond the 6 generalist built-in personas by (a) shipping 3 curated domain-specific bonus personas (`sentinel`, `tracer`, `idiomatic`) bundled with the binary, and (b) adding an `atcr personas` CLI that installs, lists, searches, removes, tests, and upgrades community-contributed personas from a configurable repository URL. Personas gain a `language` scope field that drives language-aware skeptic routing in the verify stage, and per-persona corroboration scores surface which personas earn their keep.

### Why This Matters

Without domain-specific personas, ATCR is a generalist tool teams layer their own expertise onto. Domain personas (security, performance, Go idioms, framework-specific bundles) make ATCR immediately valuable to vertical teams and become the primary lever for vertical market adoption.

### Key Deliverables

- 3 bonus built-in personas with CI-tested, network-free fixtures (Story 01)
- `atcr personas` CLI with 6 subcommands: install, list, search, remove, test, upgrade (Story 02)
- `AgentConfig.Language` field + language-aware `SelectEligibleSkeptics` two-partition routing (Story 03)
- Domain bundles (`bundle/django`, `bundle/go-production`) via embedded YAML manifests (Story 04)
- `atcr personas list --scores` wired to the scorecard corroboration data (Story 05)
- In-repo install guide, authoring template, registry/example updates (Story 06)

### Success Criteria

- `go test ./...` green across all packages including all fixture and integration tests
- Zero live network calls in CI — all HTTP fetch logic tested via `httptest.NewServer`
- `Names()` returns 9 personas; `atcr` root exposes 15 subcommands
- Path-traversal guard on persona install; community YAML validated via `validateAgent` before any disk write
- Coverage ≥80% for new `internal/personas/` package
- Backward compatibility: registries without the `language` field load unchanged

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## Clarifications

### Phase 2 Clarifications (recorded 2026-06-24)

**Key Decisions:**
- The plan's `SelectEligibleSkeptics(agents []AgentConfig, finding Finding, n int, scores map[string]float64) []string` is shorthand. The real signature is preserved — `reg *registry.Registry`, `finding reconcile.JSONFinding`, returns `[]Skeptic` — and `scores map[string]float64` is added as the 4th argument (matches the recorded epic clarification: caller-supplied, nil-safe).
- "general-purpose" is conceptual = a skeptic with no `Language` declared (zero literal grep hits in `internal/`). Routing is a two-partition reorder of the already-sorted eligible names: language-matching skeptics first, non-matching after; the existing n-cap then favors matched.
- Score lookup in Phase 2 uses `scores[name]` (skeptic registry name) per task 2.2 literal spec. The `strings.ToLower` join-key normalization is Phase 5/T6 (a separate gated phase), not Phase 2.

**Scope Boundaries:**
- IN: `internal/verify/select.go` two-partition reorder + nil-safe score sort; `internal/verify/pipeline.go:162` caller updated to pass `nil`; `internal/verify/select_test.go` (existing 3-arg call sites moved to 4-arg in the same RED/GREEN commits).
- NOT in scope: T6 scorecard wiring, lowercasing the score-map join key (both Phase 5).

**Technical Approach:**
- Match = `normalizeExt(filepath.Ext(finding.File))` ∈ skeptic's canonical `Language` entries; reuse the existing `select.go` `normalizeExt` (delegates to `registry.NormalizeLanguageToken`).
- Within the matched partition, sort by `scores[name]` descending then name ascending; `nil`/absent keys fall through to alphabetical (deterministic). Pre-allocate both partitions with `make([]string, 0, len(names))`. No `scorecard` import added to `verify`.

### Phase 4 Clarifications (recorded 2026-06-24)

**Key Decisions:**
- **Validation seam (AC 02-01 "run `validateAgent` before write"):** `validateAgent` is unexported and its provider-reference check (`config.go:645`) is registry-contextual, so a standalone fetched persona naming a provider would fail it. Resolution: **add one exported helper** `ValidateAgentYAML(name string, data []byte) error` in a **new file `internal/registry/validate.go`** (not appended to the large `config.go`). It strict-unmarshals the fetched bytes via the existing package-private `decodeStrictYAML`, builds a throwaway `Registry` with a single synthesized providers entry keyed by the fetched agent's `Provider` (so the existing `validateAgent` runs UNCHANGED, provider-ref satisfied), and returns its joined errors. Registry stays the single validation authority — chosen over duplicating field-level checks inside `internal/personas` because `validateAgent` was extended this sprint with the `Language` guard and a local duplicate would silently drift (sprint-design.md:272).
- `internal/personas/install.go` calls `registry.ValidateAgentYAML` before any disk write; on validation error it writes nothing and returns the error.

**Scope Boundaries:**
- IN (Phase 4): `internal/personas/{client,paths,install,list,search,remove,upgrade}.go` (+ `*_test.go`); `cmd/atcr/personas.go` + `personas_test.go`; `cmd/atcr/main.go` registration + `main_test.go` count bump (14 → 15); new `internal/registry/validate.go` (+ test) for the exported validation helper.
- NOT in Phase 4 scope: wiring installed personas into the running registry's resolution chain. `internal/fanout/review.go:143` already sets `PersonaDirs.Registry = <dir(regPath)>/personas` (resolves to `~/.config/atcr/personas/`) and `registry/persona.go` reads that dir on the next review — so writing to `PersonasDir()` is sufficient; `persona.go`/`review.go` are NOT touched. `bundle/` install delegation and `list --scores` real wiring stay deferred to Phase 5.

**Technical Approach:**
- `PersonasDir()` = `os.UserConfigDir()/atcr/personas`, overridable in tests (mirrors `scorecard.DefaultDir`, `internal/scorecard/paths.go:23`).
- Injectable HTTP client: a small `Doer`-style interface local to `internal/personas` (no reusable `Doer` exists in `internal/` today); `httptest.NewServer` swaps it. `RegistryBaseURL` const default `https://raw.githubusercontent.com/atcr/personas/main`, overridable via `ATCR_PERSONAS_URL`.
- `upgrade` adds `golang.org/x/mod/semver` (not currently in `go.mod`; plan-specified) with string-equality fallback for non-semver versions.
- Path-traversal guard: name matches `[a-zA-Z0-9_/-]+`, reject `..`/absolute, and verify the resolved path stays under `PersonasDir()` before any filesystem operation.

---

## TDD Strategy

**Mode:** STRICT 🔒 (RED → GREEN → ADVERSARIAL → REFACTOR) for all elements.

**Rationale:** Complexity 10/12 (VERY COMPLEX) maps to STRICT TDD. Each element gets comprehensive failing tests first, minimal implementation to green, a fresh-subagent adversarial review, then a refactor that incorporates CRITICAL/HIGH findings inline.

**Adversarial Review:** ENABLED 🎯 — Inline-fix severities: **CRITICAL/HIGH**. Deferred to tech debt: **MEDIUM/LOW**.

**Execution Mode:** GATED 🚧 — `/execute-sprint` stops at each phase-boundary GATE (`N.LAST`) after the phase DoD.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/README.md](plan/documentation/README.md) | Grounded package & design documentation |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/<pkg>/ -run <TestName>` |
| T2: Module | After completing element | `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `golangci-lint run` no errors; `go vet ./...` clean
4. Build: `go build ./...` succeeds
5. Docs: Updated where applicable

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

Follow the project standards (read before coding):

- [implementation-standards.md](../../../specifications/implementation-standards.md) — implementation conventions
- [coding-standards.md](../../../specifications/coding-standards.md) — Go style, naming, error handling
- [git-strategy.md](../../../specifications/git-strategy.md) — branch + commit conventions

**Branch:** `feature/9.0_persona_ecosystem` (push deferred to `/finalize-sprint`).

**Key conventions for this sprint:**
- Go test (stdlib) + `github.com/stretchr/testify/assert`; table-driven tests for validation/canonicalization.
- All HTTP fetch logic tested via `httptest.NewServer` — zero live network calls in CI.
- Filesystem-state-modifying tests use `t.TempDir()` substituted for `PersonasDir()`; tag with `//go:build integration` where appropriate.
- Typed errors (`ErrUnknownBundle`, `ErrPersonaNotFound`) — no string-matching by callers.

---

## External Resources

From [documentation/README.md](plan/documentation/README.md):

**[CRITICAL] Must read before coding:**
- [Bonus Built-In Personas](plan/documentation/bonus-personas.md) — persona registration + fixture expectations (T1)
- [Cobra CLI Patterns](plan/documentation/cobra-cli-patterns.md) — subcommand architecture (T2)
- [YAML Bundle Manifests](plan/documentation/yaml-bundle-manifests.md) — manifest parsing + `AgentConfig.Language` (T5/T8)
- [Skeptic Routing & Verification](plan/documentation/skeptic-routing-verification.md) — `SelectEligibleSkeptics` extension (T8)

**[IMPORTANT] Review during development:**
- [HTTP & Standard Library Testing](plan/documentation/http-stdlib-testing.md) — `httptest.NewServer` patterns (T2)
- [Per-Persona Corroboration Scores](plan/documentation/scorecard-corroboration.md) — scorecard wiring (T6)

---

## Sprint Phases

> **Pre-implementation grep (Phase 2 dependency):** Before Phase 2, run `grep -r "SelectEligibleSkeptics" ./internal/` to confirm the single production caller (`internal/verify/pipeline.go:162`). Update any additional callers in the same commit.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: T8 — AgentConfig Language Field (Sprint A, Days 1-2)

**Focus:** Schema change — `Language []string` on `AgentConfig`, validation, canonicalization, shared `normalizeExt` helper.
**Story:** [03 — Language-Aware Skeptic Routing](plan/user-stories/03-language-aware-skeptic-routing.md) | **ACs:** [03-01](plan/acceptance-criteria/03-01-agentconfig-language-field.md), [03-05 (partial)](plan/acceptance-criteria/03-05-registry-yaml-backward-compatibility.md)

### 1.1 [x] **[Language Field + normalizeExt - RED](plan/user-stories/03-language-aware-skeptic-routing.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestAgentConfig_LanguageField_Validation` (reject empty entries + control chars)
   - `TestAgentConfig_LanguageField_Canonicalization` (`.go`/`GO`/` go ` → `go`; idempotent)
   - `TestNormalizeExt_WithAndWithoutDot` (`.go` → `go`, `go` → `go`)
   - `TestRegistryExamples_BackwardCompat` (registry with no `language` field loads cleanly — AC 03-05 partial)
   **Files:** `internal/registry/config_test.go`, `internal/verify/select_test.go` | **Duration:** 0.5 day

### 1.2 [x] **[Language Field + normalizeExt - GREEN](plan/user-stories/03-language-aware-skeptic-routing.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `internal/registry/config.go` — add `Language []string \`yaml:"language,omitempty"\``; extend `validateAgent` (reject empty entries + control chars, mirror `Scope` guard, NO known-language allow-list); extend `applyDefaults` (trim space, strip single leading dot, lowercase)
   - `internal/verify/select.go` — add `normalizeExt(ext string) string` helper (strips dot, lowercases)
   COMMIT: `git commit -m "feat(registry): add AgentConfig.Language field + normalizeExt (green)"`
   **Files:** `internal/registry/config.go`, `internal/verify/select.go` | **Duration:** 0.5 day

### 1.2.A [x] **[Language Field - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-language-aware-skeptic-routing.md)**
   **Changed Files:** `internal/registry/config.go`, `internal/verify/select.go`, `internal/registry/config_test.go`, `internal/verify/select_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.2]
     - Checklist (pass verbatim):
       - SECURITY: Control-char / injection via language entries? Validation bypass?
       - EDGE CASES: Empty slice, nil, mixed-case, multi-dot ext (`.tar.gz`), unicode, idempotency of canonicalization?
       - ERROR HANDLING: Validation error messages clear? Swallowed errors?
       - PERFORMANCE: Allocation in canonicalization hot path?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (verified independently — see Phase 1 execution):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | internal/registry/config.go validateAgent/applyDefaults | Single-dot entry `"."` (or `" . "`) passes `validateAgent` (raw value non-empty, no control char) but `NormalizeLanguageToken(".")` strips the leading dot → `""`, storing an empty Language token that matches every extensionless finding in Phase 2 routing. Confirmed: `Language:["."]` → 0 errors, canonicalizes to `""`. | Validate the CANONICAL form: reject `NormalizeLanguageToken(s) == ""` (covers `"."`, `" . "`, whitespace-only, empty in one check). Add test cases. → fixed in 1.3 |
   | LOW | internal/registry/config.go NormalizeLanguageToken | Interior whitespace / non-leading dots (`"g o"`, `".."`→`"."`) store as junk tokens that silently never match — misconfiguration fails with no diagnostic. Routing-only (no prompt interpolation), not exploitable. | Documentation note; silent-no-match is acceptable. → TD-001 |
   | LOW | internal/registry/config_test.go canonicalization test | Idempotency test covers ASCII only; `strings.ToLower` not idempotent for all Unicode, and no allow-list invites arbitrary extensions. | Optional Unicode idempotency case. → TD-002 |

   **Action Required:**
   - HIGH found → fixed inline in 1.3 (single-dot canonical-empty bypass). Two LOWs deferred → `tech-debt-captured.md` (TD-001, TD-002).

### 1.3 [x] **[Language Field + normalizeExt - REFACTOR](plan/user-stories/03-language-aware-skeptic-routing.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Confirm `normalizeExt` is the single shared helper used by both `applyDefaults` and the routing path (no duplication); maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(registry): address review + share normalizeExt"`
   **Duration:** 0.5 day

### 1.4 [x] **Phase 1 — DoD Validation**
   - Run `go test ./internal/registry/... ./internal/verify/...` (T3 scoped) — green ✓ (full `go test ./...` also green, EXIT=0)
   - `go build ./...` clean ✓; `go vet ./...` clean ✓; `golangci-lint run` 0 issues ✓
   - Coverage: registry 89.5%, verify 95.5% (both ≥80%)
   - DoD report:
     ```
     Story-03 (partial) DoD Complete
     Auto: 5/5 (tests, coverage, lint, types/vet, build) | Story-Specific: 4/4 (AC 03-01) + 1/3 (AC 03-05 — load back-compat; routing baseline is Phase 2)
     Manual Review: [ ] Code reviewed
     ```

### 1.LAST [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `Language` field shape + canonical form (`["go","ts"]`, no dot, lowercased) honored?
       - CONFIG SURFACE: `language` YAML key documented intent, defaulted (nil = no constraint), back-compat?
       - INTEGRATION: `normalizeExt` ready for Phase 2 routing consumption without rework?
       - PHASE-EXIT CONTRACT: Phase 2 can build the two-partition reorder on this?
       - REGRESSION: Existing `AgentConfig` load/validate behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings:** PASS on all 5 checklist items (CONTRACT EXIT, CONFIG SURFACE, INTEGRATION, PHASE-EXIT CONTRACT, REGRESSION). Build clean; registry+verify tests green; no import cycle.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | internal/verify/select.go:42 | `normalizeExt` has no non-test caller until Phase 2 (deliberate Phase 1→2 handoff seam; correct and contract-ready now). | None required — Phase 2 wires it into `SelectEligibleSkeptics`. Resolved within this sprint; not captured as TD. |

   **Action:** No CRITICAL/HIGH. Single LOW is an intra-sprint handoff seam (reviewer: "None required") — not debt. **Phase gate passed.**
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: T8 — SelectEligibleSkeptics Routing (Sprint A, Days 3-4)

**Focus:** Two-partition reorder in `select.go`; nil-safe score sort; pipeline caller signature update.
**Story:** [03 — Language-Aware Skeptic Routing](plan/user-stories/03-language-aware-skeptic-routing.md) | **ACs:** [03-02](plan/acceptance-criteria/03-02-select-eligible-skeptics-routing.md), [03-03](plan/acceptance-criteria/03-03-pipeline-caller-update.md), [03-04](plan/acceptance-criteria/03-04-silent-fallback-no-match.md), [03-05 (remainder)](plan/acceptance-criteria/03-05-registry-yaml-backward-compatibility.md)

> **Pre-implementation check:** `grep -r "SelectEligibleSkeptics" ./internal/` — confirm single caller; update any additional callers in the same commit if found.

### 2.1 [x] **[Skeptic Routing - RED](plan/user-stories/03-language-aware-skeptic-routing.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestSelectEligibleSkeptics_LanguageMatch`, `_NoMatchFallback`, `_TieBreakByScore`, `_TieBreakAlphabeticalWhenNoScores`, `_NilScoresMap`, `_BackwardCompatNoLanguageField`
   **Files:** `internal/verify/select_test.go` | **Duration:** 0.5 day

### 2.2 [x] **[Skeptic Routing - GREEN](plan/user-stories/03-language-aware-skeptic-routing.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `internal/verify/select.go:55` — change signature to `SelectEligibleSkeptics(agents []AgentConfig, finding Finding, n int, scores map[string]float64) []string`; after `sort.Strings(names)`, partition into matched (finding file ext ∈ skeptic's `Language` via `normalizeExt`) and unmatched; rebuild `append(matched, unmatched...)`; within matched, sort by `scores[name]` descending then name ascending; nil map → alphabetical-only
   - `internal/verify/pipeline.go:162` — update sole production caller to pass scores map (nil acceptable until T6 wires it)
   COMMIT: `git commit -m "feat(verify): language-aware two-partition skeptic routing (green)"`
   **Files:** `internal/verify/select.go`, `internal/verify/pipeline.go` | **Duration:** 0.75 day

### 2.2.A [x] **[Skeptic Routing - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-language-aware-skeptic-routing.md)**
   **Changed Files:** `internal/verify/select.go`, `internal/verify/pipeline.go`, `internal/verify/select_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
     - Checklist (pass verbatim):
       - SECURITY: Any data exposure via reordering? Score-map injection?
       - EDGE CASES: Nil scores map, empty names, all-match, no-match, duplicate scores, n-cap boundary, finding with no extension?
       - ERROR HANDLING: Panics on nil map? Determinism of tie-break?
       - PERFORMANCE: Slice growth copies? Pre-allocated `make([]string, 0, len(names))`? No scorecard import leaked into verify?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/verify/select.go (matched sort) | NaN corroboration score breaks the matched-partition total order → silently non-deterministic routing. | Deferred → TD-003 (not active; caller passes nil scores until T6). |
   | MEDIUM/LOW | internal/verify/select_test.go | No independence test for the returned `Skeptic.Config.Language` alias slice (Scope has one). | Deferred → TD-004 (no live bug; `languageMatches` only reads). |
   | LOW | internal/verify/pipeline.go:165 | Score tie-break path has zero production coverage until T6 wires `scores`. | By design (Phase 5 handoff seam) — no action. |
   | LOW | internal/verify/select.go (partition pre-alloc) | `unmatched` pre-allocated to `len(names)` may be unused in all-matched case. | Per task 2.3 spec (`make([]string, 0, len(names))`) — accepted. |

   **Action Required:**
   - No CRITICAL/HIGH found → Adversarial review passed; proceed.
   - 2 MEDIUM/LOW actionable → deferred to `tech-debt-captured.md` (TD-003, TD-004). 2 LOW are by-design/per-spec → no capture.

### 2.3 [x] **[Skeptic Routing - REFACTOR](plan/user-stories/03-language-aware-skeptic-routing.md)**
   1. No CRITICAL/HIGH from 2.2.A → nothing to fix inline (2 MEDIUM/LOW deferred to TD-003/TD-004).
   2. Pre-allocation (`make([]string, 0, len(names))`, select.go:124-125) and the no-`scorecard`-import invariant (only a comment reference at pipeline.go:162) were both satisfied in the GREEN commit (28aadd4). T3 green.
   3. COMMIT: folded — no code delta over GREEN, so no separate refactor commit (avoids an empty commit).
   **Duration:** 0.5 day

### 2.4 [x] **Phase 2 — DoD Validation**
   - `go test ./...` green (EXIT=0); `go build ./...` clean; `go vet ./internal/verify/...` clean; `golangci-lint run ./internal/verify/...` 0 issues
   - Coverage: verify 95.6% (≥80%)
   - No API leakage from `verify` into `scorecard` — score map is caller-built; only a comment references scorecard (no import)
   - DoD report (Story-03 complete):
     ```
     Story-03 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 4/4 (AC 03-02 routing, 03-03 pipeline caller, 03-04 silent fallback, 03-05 backward-compat) + AC 03-01 from Phase 1
     Manual Review: [ ] Code reviewed
     ```

### 2.LAST [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: 4-arg signature stable; `map[string]float64` carrier shape matches what T6 will build from `scorecard.Aggregate()`?
       - CONFIG SURFACE: Routing fallback silent + automatic (AC 03-04)?
       - INTEGRATION: Sole caller updated; grep confirmed no stragglers?
       - PHASE-EXIT CONTRACT: T6 (Phase 5) can supply the scores map without verify changes?
       - REGRESSION: Backward-compat — registries without `language` still route correctly?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings:** PASS on all 5 checklist items (CONTRACT EXIT, DECOUPLING, CONFIG SURFACE/fallback, INTEGRATION sole-caller, REGRESSION back-compat). Fresh `go test ./internal/verify/...` green, `go build ./...` clean, grep confirms only production caller is `pipeline.go:162` passing `nil`.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | internal/verify/pipeline.go:162 | Score-map keyspace (skeptic registry name) vs T6's `scorecard.Aggregate()` "reviewer-name" source could silently mismatch when wired. | Deferred → TD-005 (nil today; document keyspace at call site when T6 wires it). |
   | LOW | internal/verify/select.go (matched sort) | NaN score → non-deterministic comparator. | Already deferred → TD-003. |

   **Action:** No CRITICAL/HIGH. Two LOW integration notes deferred to `tech-debt-captured.md` (TD-005 new, TD-003 prior). **Phase gate passed.**
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: T1 — Bonus Built-In Personas (Sprint A, Days 5-7)

**Focus:** 3 persona `.md` files + 3 fixtures + registry update + CI-passing, network-free tests.
**Story:** [01 — Bonus Built-In Domain Personas](plan/user-stories/01-bonus-built-in-domain-personas.md) | **ACs:** [01-01](plan/acceptance-criteria/01-01-names-registry-returns-nine.md), [01-02](plan/acceptance-criteria/01-02-bonus-persona-prompt-content.md), [01-03](plan/acceptance-criteria/01-03-fixture-ci-tests-no-network.md)

> **Atomic-commit rule:** Rename `TestNames_ReturnsAllSix` → `TestNames_ReturnsAllNine` (count 6 → 9) as a standalone RED commit; then add the 3 `.md` files + 3 fixtures + `names` slice update **all in one GREEN commit** to avoid a CI failure window. **Fixture content rules:** synthetic values only (`FAKE_API_KEY_00000000`), mode 0644, no live network in rendering path.

### 3.1 [x] **[Bonus Personas - RED](plan/user-stories/01-bonus-built-in-domain-personas.md)**
   Write comprehensive failing tests, verify they fail correctly (commit RED standalone):
   - Rename `TestNames_ReturnsAllSix` → `TestNames_ReturnsAllNine` (expect 9)
   - `TestGet_BonusPersonasNonEmpty`, `TestBonusPersonas_TemplateRenders`
   - `TestSentinelFixture`, `TestTracerFixture`, `TestIdiomaticFixture` (assert on finding category string: `"injection"`, `"n+1"`, `"error"` — not just non-empty); reuse existing `Payload` struct
   COMMIT (RED): `git commit -m "test(personas): expect 9 personas + bonus fixtures (red)"`
   **Files:** `personas/personas_test.go` | **Duration:** 0.5 day

### 3.2 [x] **[Bonus Personas - GREEN](plan/user-stories/01-bonus-built-in-domain-personas.md)**
   Minimal code (T1), verify all (T2), single atomic COMMIT:
   - `personas/personas.go` — append `"sentinel"`, `"tracer"`, `"idiomatic"` to `names` (after `"dax"`, before `"otto"`)
   - `personas/sentinel.md` — security: OWASP Top 10, SQL/command injection, secrets leakage, insecure defaults (follow `bruce.md` template structure exactly)
   - `personas/tracer.md` — performance: N+1 queries, memory leaks, allocation hot paths, escape analysis
   - `personas/idiomatic.md` — Go idioms: error handling, goroutine leaks, sync misuse, stdlib misuse
   - `personas/testdata/sentinel_fixture.patch` — synthetic SQL string concat
   - `personas/testdata/tracer_fixture.patch` — ORM call inside a `for` loop
   - `personas/testdata/idiomatic_fixture.patch` — ignored error return (`val, _ := strconv.Atoi(s)`)
   COMMIT (GREEN, atomic): `git commit -m "feat(personas): add sentinel/tracer/idiomatic bonus personas (green)"`
   **Files:** `personas/personas.go`, `personas/*.md`, `personas/testdata/*.patch` | **Duration:** 1.5 days

### 3.2.A [x] **[Bonus Personas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-bonus-built-in-domain-personas.md)**
   **Changed Files:** `personas/personas.go`, `personas/sentinel.md`, `personas/tracer.md`, `personas/idiomatic.md`, `personas/testdata/*.patch`, `personas/personas_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. No memory of the implementation in 3.2. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.2]
     - Checklist (pass verbatim):
       - SECURITY: Any real (non-synthetic) credential in fixtures? Network call in render path?
       - EDGE CASES: Persona template variable slots match `bruce.md` exactly? `go:embed` picks up only declared names?
       - ERROR HANDLING: `Get(name)` rejects names not in `names` slice regardless of embedded files?
       - PERFORMANCE: Fixture rendering does no I/O beyond embedded read?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review — verified independently):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | personas/personas.go:27-33 | `Get(name)` reads `files.ReadFile(name+".md")` directly without gating on the `names` registry — serves `_base` or any embedded `.md`, bypassing the registry-as-source-of-truth invariant. (Reviewer's `../x` traversal premise is false — embed.FS is sandboxed and `agentName` is path-validated upstream at `registry/persona.go:49`.) | Gate `Get` on `names` membership before reading; `_base` stays served by `Base()`. → fixed in 3.3 |
   | MEDIUM | personas/personas_test.go:88-101 | `fixtureTest` asserts the category keyword anywhere in the rendered output, but `{{.Payload}}` injects the whole fixture diff — `idiomatic_fixture.patch` already contains "error", so `TestIdiomaticFixture` passed from the payload, not the template (tautological). | Assert the category against the raw persona template (`Get(name)`), not the payload-injected render; keep fixture load + clean-render checks. → fixed in 3.3 |
   | LOW | personas/personas.go:1-2,19 | Doc comments hardcode the persona count ("nine") in prose; can drift silently. Matches pre-existing convention (prior comment said "six"). | Documentation nit; count is asserted by `TestNames_ReturnsAllNine`. → TD-006 |

   **Action Required:**
   - HIGH (Get registry gate) → fixed inline in 3.3. MEDIUM (test tautology) → fixed inline in 3.3 (test-soundness refactor). LOW deferred → `tech-debt-captured.md` (TD-006).

### 3.3 [x] **[Bonus Personas - REFACTOR](plan/user-stories/01-bonus-built-in-domain-personas.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Review persona template quality; ensure all variable slots match `bruce.md` exactly; maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): address review + template polish"`
   **Duration:** 0.5 day

### 3.4 [x] **Phase 3 — DoD Validation**
   - `go test ./personas/...` green including all 3 fixture tests
   - Confirm no outbound connections in test run
   - DoD report (Story-01 complete):
     ```
     Story-01 DoD Complete
     Auto: {X}/5 | Story-Specific: {Y}/{Z}
     Manual Review: [ ] Code reviewed
     ```

### 3.LAST [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `Names()` returns exactly 9; canonical ordering preserved?
       - CONFIG SURFACE: Fixtures synthetic-only, 0644, documented intent?
       - INTEGRATION: Bonus personas usable by Phase 4 `list` (built-in source) without rework?
       - PHASE-EXIT CONTRACT: `TestNames_ReturnsAllNine` stable for downstream count assertions?
       - REGRESSION: Original 6 personas still render?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings (fresh-context hostile integrator):** PASS on all 5 checklist items (CONTRACT EXIT — `Names()` returns 9 in canonical order; CONFIG SURFACE — fixtures synthetic `FAKE_API_KEY_00000000`, mode 0644, intent documented; INTEGRATION — bonus personas usable by Phase 4 `list` via `Names()`/`Get()`, `init` roster derives from `personas.Names()` so all 9 install; PHASE-EXIT CONTRACT — `TestNames_ReturnsAllNine` stable; REGRESSION — original 6 render, `go test ./...`/`go build`/`go vet`/`golangci-lint` all clean).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | - | Phase gate passed | - |

   **Action:** No CRITICAL/HIGH/MEDIUM/LOW findings. **Phase gate passed.**
   **Duration:** 15-30 min
   **— END SPRINT A (Phases 1-3) —**

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: T2 — atcr personas CLI (Sprint B, Days 8-11)

**Focus:** New `internal/personas` package + 6 Cobra sub-subcommands + atomic root-count test update.
**Story:** [02 — Personas CLI Discovery & Lifecycle](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md) | **ACs:** [02-01](plan/acceptance-criteria/02-01-install-persona-from-community-repo.md), [02-02](plan/acceptance-criteria/02-02-list-installed-personas.md), [02-03](plan/acceptance-criteria/02-03-search-community-repo-index.md), [02-04](plan/acceptance-criteria/02-04-remove-installed-persona.md), [02-05](plan/acceptance-criteria/02-05-test-persona-fixture.md), [02-06](plan/acceptance-criteria/02-06-upgrade-installed-personas.md)

> **Atomic-commit rule:** `root.AddCommand(newPersonasCmd())` in `main.go` + `TestRootCmd_HasExactlyFifteenSubcommands` count bump must land in the **same commit** to avoid a CI failure window. All HTTP tested via `httptest.NewServer`; all `PersonasDir()` substituted with `t.TempDir()`.

### Element A — `internal/personas` package core

### 4.1 [x] **[internal/personas core - RED](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `install_test.go` — install fetch → `validateAgent` → write; path-traversal guard (`..` rejected); HTTP via `httptest.NewServer`
   - `list_test.go` — merge built-in (`personas.Names()`) + community (`os.ReadDir`); graceful on missing dir
   - `search_test.go` — fetch `index.json`; keyword filter
   - `remove_test.go` — remove by name
   - `upgrade_test.go` — version compare via `golang.org/x/mod/semver`; `--dry-run` prints, no write
   **Files:** `internal/personas/*_test.go` | **Duration:** 1 day

### 4.2 [x] **[internal/personas core - GREEN](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   Minimal code (T1), verify all (T2), COMMIT:
   - `client.go` — `RegistryBaseURL = "https://raw.githubusercontent.com/atcr/personas/main"`; injectable `HTTPClient` interface; `ATCR_PERSONAS_URL` env override
   - `paths.go` — `PersonasDir() string` via `os.UserConfigDir()`; overridable in tests
   - `install.go` — `Install(client HTTPClient, baseURL, name, destDir string) error`; fetch → `validateAgent` → write; path-traversal guard (`[a-zA-Z0-9_/-]+`, reject `..`/absolute); `bundle/` prefix detection deferred to Phase 5
   - `list.go` — `List(personasDir string) ([]PersonaMeta, error)`; graceful on missing dir
   - `search.go` — `Search(client, baseURL, keyword string) ([]PersonaMeta, error)`
   - `remove.go` — `Remove(name, personasDir string) error`
   - `upgrade.go` — `Upgrade(client, baseURL, name, personasDir string, dryRun bool) error`
   COMMIT: `git commit -m "feat(personas): internal/personas lifecycle package (green)"`
   **Files:** `internal/personas/{client,paths,install,list,search,remove,upgrade}.go` | **Duration:** 1.5 days

### 4.2.A [x] **[internal/personas core - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   **Changed Files:** `internal/personas/{client,paths,install,list,search,remove,upgrade}.go` + `*_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 4.2. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.2]
     - Checklist (pass verbatim):
       - SECURITY: **Path traversal** in `install`/`remove`/`upgrade` name → destination (`../../etc/...`)? Absolute path segments rejected? Malicious community YAML reaches disk before `validateAgent`?
       - EDGE CASES: Missing personas dir, empty `index.json`, HTTP non-200, partial write on error, env-var override empty string?
       - ERROR HANDLING: Typed errors (`ErrPersonaNotFound`)? No string-matching by callers? Write-then-validate ordering correct (validate BEFORE write)?
       - PERFORMANCE: Any live network call possible in CI path?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/personas/client.go:67 | Unbounded `io.ReadAll` on the community-repo response (persona YAML + index.json) → OOM via a multi-GB body from a compromised repo/MITM. | Deferred → TD-007 (HTTPS trust-on-first-use source; `io.LimitReader` cap in follow-up). |
   | MEDIUM | internal/personas/client.go:71 | Exported `FetchPersonaYAML` interpolates raw `name` into the fetch URL without `validatePersonaName`; future external callers could inject `../`/`?`/`scheme://`. | Deferred → TD-008 (all current callers pre-validate via `personaPath`; self-guard the seam in follow-up). |
   | LOW | internal/personas/upgrade.go:77 | Non-semver version diff treated as upgrade (downgrade masquerade). | Deferred → TD-009 (matches AC 02-06 EC1 — specified behavior). |
   | LOW | internal/personas/install.go:30, upgrade.go:55 | Non-atomic `os.WriteFile` — crash mid-write can truncate a previously valid persona. | Deferred → TD-010 (temp-file + rename in follow-up). |
   | LOW | internal/personas/list.go:71-83 | `listCommunity` silently degrades a corrupt/unreadable persona to `Version "-"`. | Deferred → TD-011 (read-only display; warn in follow-up). |

   **Action Required:**
   - No CRITICAL/HIGH found → **Adversarial review passed; proceed.**
   - 2 MEDIUM + 3 LOW → deferred to `tech-debt-captured.md` (TD-007…TD-011).

### Element B — Cobra subcommands + registration

### 4.3 [x] **[personas subcommands - RED](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   Write failing tests, verify they fail correctly:
   - `cmd/atcr/personas_test.go` — integration tests via `httptest.NewServer` for each of `install`, `list`, `search`, `remove`, `test`, `upgrade`
   - `cmd/atcr/main_test.go` — rename `TestRootCmd_HasExactlyFourteenSubcommands` → `...FifteenSubcommands` (count 14 → 15)
   **Files:** `cmd/atcr/personas_test.go`, `cmd/atcr/main_test.go` | **Duration:** 0.75 day

### 4.4 [x] **[personas subcommands - GREEN](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   Minimal code (T1), verify all (T2), atomic COMMIT:
   - `cmd/atcr/personas.go` — `newPersonasCmd()` + 6 sub-subcommands wired to `internal/personas`
   - `cmd/atcr/main.go` — `root.AddCommand(newPersonasCmd())` (SAME commit as count test bump)
   COMMIT (atomic): `git commit -m "feat(cmd): atcr personas command + 6 subcommands (green)"`
   **Files:** `cmd/atcr/personas.go`, `cmd/atcr/main.go` | **Duration:** 0.75 day

### 4.4.A [x] **[personas subcommands - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   **Changed Files:** `cmd/atcr/personas.go`, `cmd/atcr/main.go`, `cmd/atcr/personas_test.go`, `cmd/atcr/main_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 4.4. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.4`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.4]
     - Checklist (pass verbatim):
       - SECURITY: User-supplied persona name flows to filesystem safely? Error messages leak paths?
       - EDGE CASES: Subcommand with no args, unknown subcommand, conflicting flags, count test exactly 15?
       - ERROR HANDLING: Errors → stderr, success → stdout? Non-zero exit on failure?
       - PERFORMANCE: Any live network in CI? All tests use temp dirs?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | personas_test.go (execute merges streams) | `execute` points SetOut/SetErr at one buffer, so tests can't verify the success→stdout / error→stderr contract (FAIL path, `--scores` notice). | Addressed in 4.5 — added `executeSplit` + stream-separation assertions. |
   | MEDIUM | personas.go:97-102 | `list` warns + exits 0 even on a real dir I/O error, not just absent dir. | **Not a defect** — AC 02-02 Error Scenario 1 mandates exit 0 + stderr warning on an unreadable personas dir (graceful degradation). Spec-compliant; no change. |
   | LOW | personas.go:171-172 | `test` FAIL emits "FAIL" on stdout AND a returned error on stderr (double output). | Accepted: stdout carries the report, stderr the error — a reasonable convention. No change. |
   | LOW | personas.go:188-212 | `upgrade` flag-conflict (`--all` + name) and no-arg branches had no test. | Addressed in 4.5 — added exit-2 tests for both. |
   | LOW | personas.go:33-35 | Default `noFixtureRunner` path never exercised (every test injects a stub). | Addressed in 4.5 — added a default-runner "No fixture" test. The real LLM-backed runner is not wired → TD-012. |

   **Action Required:**
   - No CRITICAL/HIGH found → **Adversarial review passed; proceed.**
   - Two cheap test gaps (stream separation, untested branches) closed in 4.5; one MEDIUM is spec-compliant (not a defect); the production fixture-runner scope gap captured as TD-012.

### 4.5 [x] **[personas CLI - REFACTOR](plan/user-stories/02-personas-cli-discovery-and-lifecycle.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A and 4.4.A (if any)
   2. Consolidate HTTP client injection pattern; ensure all tests use temp dirs for `PersonasDir`; maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): address review + consolidate HTTP injection"`
   **Duration:** 0.5 day

### 4.6 [x] **Phase 4 — DoD Validation**
   - `go test ./...` green (EXIT=0, all packages incl. `cmd/atcr` + `internal/personas` integration); zero live network calls in CI (all fetch via `httptest.NewServer`); `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 issues
   - Coverage: `internal/personas` 84.4%, `cmd/atcr` 84.1% (both ≥80%)
   - `Names()` returns 9; root exposes 15 subcommands (`TestRootCmd_HasExactlyFifteenSubcommands` green); path-traversal guard + validate-before-write verified by tests
   - DoD report (Story-02 complete):
     ```
     Story-02 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 23/24 (AC 02-01..02-06 covered; 1 deferred → TD-012: production LLM-backed fixture runner for `personas test` out of phase scope, CLI surface + exit-code contract + injectable seam delivered/tested via stub)
     Manual Review: [ ] Code reviewed
     ```

### 4.LAST [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `Install` signature ready for Phase 5 `bundle/` delegation? `List` ready for `--scores` extension?
       - CONFIG SURFACE: `ATCR_PERSONAS_URL` override documented; default URL not hardcoded unconditionally?
       - INTEGRATION: `install.go` `bundle/` hook point present for Phase 5? Root has exactly 15 subcommands?
       - PHASE-EXIT CONTRACT: Phase 5 can add `bundles.Resolve` + `--scores` without restructuring?
       - REGRESSION: Existing subcommands unaffected; `go test ./...` clean?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings (fresh-context hostile integrator):** PASS on all 5 checklist items. `go build`/`go test ./...`/`go vet`/`golangci-lint` all clean. CONTRACT EXIT — `Install(client, baseURL, name, destDir)` is name-first so Phase 5 can branch on `bundle/` prefix with no signature change; `PersonaMeta`/`renderPersonaList` admit a SCORE column additively. CONFIG SURFACE — `ATCR_PERSONAS_URL` override present, default not unconditional, HTTPS. INTEGRATION — clean `bundle/` hook point; root exposes exactly 15 subcommands (asserted). PHASE-EXIT CONTRACT — `internal/personas → registry` boundary declared (boundaries_test.go), acyclic; Phase 5 can add `bundles.Resolve` + `--scores` additively. REGRESSION — existing subcommands unaffected.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | cmd/atcr/personas.go:92-96 | `--scores` is a no-op stderr notice (as specified for Phase 4). | By design — Phase 5 wires the scorecard map into `List`/`ListWithScores`. No action. |

   **Phase 5 handoff note (not debt):** `validatePersonaName` permits `/`, so a `bundle/<name>` already passes name validation and would round-trip through the single-persona fetch path. Phase 5 MUST intercept `strings.HasPrefix(name, "bundle/")` at the TOP of `Install` (before `personaPath`/`FetchPersonaYAML`) and delegate to `bundles.Resolve`. A new `internal/bundles` (or `bundles.go` in-package) needs no boundary entry if kept in `internal/personas`; a separate package needs its own `boundaries_test.go` allowlist entry.

   **Action:** No CRITICAL/HIGH/MEDIUM. Single LOW is by-design. **Phase gate passed.**
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: T5 Domain Bundles + T6 Corroboration Scores (Sprint B, Days 12-14)

**Focus:** Bundle resolver + embedded YAML manifests + `--scores` flag wired to scorecard.
**Stories:** [04 — Domain Bundles](plan/user-stories/04-domain-bundles.md), [05 — Corroboration Feedback](plan/user-stories/05-corroboration-feedback.md)
**ACs:** [04-01](plan/acceptance-criteria/04-01-clean-bundle-install.md), [04-02](plan/acceptance-criteria/04-02-partial-install-skip.md), [04-03](plan/acceptance-criteria/04-03-unknown-bundle-error.md), [04-04](plan/acceptance-criteria/04-04-manifest-parse-validation.md), [04-05](plan/acceptance-criteria/04-05-bundle-test-coverage.md), [05-01](plan/acceptance-criteria/05-01-baseline-list-no-regression.md), [05-02](plan/acceptance-criteria/05-02-scores-column-display.md), [05-03](plan/acceptance-criteria/05-03-sort-ordering.md), [05-04](plan/acceptance-criteria/05-04-help-documentation.md)

> **Score-map key convention:** `strings.ToLower(reviewerName)` — same normalization on both sides of the join (persona list + scorecard aggregate).

### Element A — T5 Domain Bundles

### 5.1 [x] **[Domain Bundles - RED](plan/user-stories/04-domain-bundles.md)**
   Write failing tests, verify they fail correctly:
   - `bundles_test.go` — `TestBundleResolve_Django`, `_GoProduction`, `_Unknown` (typed `ErrUnknownBundle`), `_PartialInstallSkip`, `_ManifestParseMissingFields`
   **Files:** `internal/personas/bundles_test.go` | **Duration:** 0.5 day

### 5.2 [x] **[Domain Bundles - GREEN](plan/user-stories/04-domain-bundles.md)**
   Minimal code (T1), verify all (T2), COMMIT:
   - `internal/personas/bundles.go` — `Resolve(name string) ([]string, error)`; `go:embed bundles/*.yaml`; typed `ErrUnknownBundle`; parse-time validation (missing `name`/`personas` → error)
   - `internal/personas/bundles/django.yaml` — members: `django-orm`, `python-types`, `security/owasp`, `security/secrets`
   - `internal/personas/bundles/go-production.yaml` — members: `security/owasp`, `security/secrets`, `performance/memory`
   - `internal/personas/install.go` — detect `bundle/` prefix via `strings.HasPrefix`; delegate to `bundles.Resolve`, loop single-persona install; per-persona outcome report (idempotent recovery / partial skip)
   COMMIT: `git commit -m "feat(personas): domain bundles + bundle/ install delegation (green)"`
   **Files:** `internal/personas/bundles.go`, `internal/personas/bundles/*.yaml`, `internal/personas/install.go` | **Duration:** 0.75 day

### 5.2.A [x] **[Domain Bundles - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-domain-bundles.md)**
   **Changed Files:** `internal/personas/bundles.go`, `internal/personas/bundles/*.yaml`, `internal/personas/install.go`, `internal/personas/bundles_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 5.2. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.2]
     - Checklist (pass verbatim):
       - SECURITY: `go:embed bundles/*.yaml` picks up only intended files? Bundle member names path-traversal-safe through install loop?
       - EDGE CASES: Unknown bundle, empty manifest, missing `name`/`personas`, partial install (one member fails — others still attempted)?
       - ERROR HANDLING: `ErrUnknownBundle` typed; per-persona outcome reported; install idempotent on re-run?
       - PERFORMANCE: Manifest parsed once / embedded, not re-read per install?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/personas/bundles.go InstallBundle | Duplicate members in a manifest are not deduplicated → a dup is installed once then reported `AlreadyPresent`, a misleading outcome. | AC 04-04 EC3 makes dedup the install loop's responsibility → fixed inline in 5.5 (order-preserving dedup + test). |
   | MEDIUM | internal/personas/client.go:67 | Unbounded `io.ReadAll` on the community response. | Already captured Phase 4 → TD-007 (not a Phase 5 file; no re-capture). |
   | LOW | internal/personas/bundles.go Resolve | `isValidBundleName` pre-check is redundant with the embed `ReadFile` gate; the comment claiming the lookup is "the only gate" contradicts the pre-check. | Comment corrected in 5.5; pre-check kept as defense in depth. |
   | LOW | internal/personas/install.go:21-23 | Defensive `bundle/` reject is not a typed sentinel (test asserts only "not ErrPersonaNotFound"). | By design — unreachable in practice (members come from trusted embedded manifests). No action. |

   **Action Required:**
   - No CRITICAL/HIGH found → **Adversarial review passed; proceed.**
   - One MEDIUM is AC-mandated (04-04 EC3 dedup) → fixed inline in 5.5; one MEDIUM is prior TD-007; LOWs are doc-fix/by-design.

### Element B — T6 Corroboration Scores

### 5.3 [x] **[Corroboration Scores - RED](plan/user-stories/05-corroboration-feedback.md)**
   Write failing tests, verify they fail correctly:
   - `list_test.go` — `TestPersonasList_WithScores_HasRate`, `_NaForMissing`, `_SortOrder` (numeric desc, then n/a alphabetical), `_BaselineNoRegression`
   **Files:** `internal/personas/list_test.go` | **Duration:** 0.5 day

### 5.4 [x] **[Corroboration Scores - GREEN](plan/user-stories/05-corroboration-feedback.md)**
   Minimal code (T1), verify all (T2), COMMIT:
   - `internal/personas/list.go` — extend `List()` or add `ListWithScores(map[string]float64)`; join on `strings.ToLower` of reviewer name; format rate `"XX.X%"` or `"n/a"`; sort numeric desc then n/a alphabetical
   - `cmd/atcr/personas.go` — wire `--scores` boolean flag to `list`; call `scorecard.Aggregate()` when set; pass map to list logic; `--scores` shows `n/a` for all + footer note when scorecard file absent
   COMMIT: `git commit -m "feat(personas): list --scores corroboration display (green)"`
   **Files:** `internal/personas/list.go`, `cmd/atcr/personas.go` | **Duration:** 0.75 day

### 5.4.A [x] **[Corroboration Scores - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-corroboration-feedback.md)**
   **Changed Files:** `internal/personas/list.go`, `cmd/atcr/personas.go`, `internal/personas/list_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 5.4. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.4`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.4]
     - Checklist (pass verbatim):
       - SECURITY: No sensitive scorecard data leaked beyond rate?
       - EDGE CASES: Missing scorecard file → all `n/a` + footer; mixed-case reviewer name join; empty list; ties in sort?
       - ERROR HANDLING: Baseline `list` (no `--scores`) unchanged (no regression)? Graceful when `scorecard.Aggregate()` errors?
       - PERFORMANCE: `scorecard.Aggregate()` called once per invocation only when flag set?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context general-purpose review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | cmd/atcr/personas.go loadPersonasScores | `scorecard.Aggregate` groups by (reviewer, model); a reviewer that ran under multiple models yields multiple rows sharing one Reviewer name, and the loop let the last row overwrite → arbitrary/misleading rate. | **Fixed inline in 5.5:** extracted `reviewerCorroborationRates` that sums corroborated/raised across models per lowercase reviewer and recomputes the ratio. Regression test `TestReviewerCorroborationRates_CollapsesModels`. |
   | MEDIUM | cmd/atcr/personas.go footer | Footer keyed on raw record count (`hasData`) → "records present but no reviewer scores" showed an all-n/a table with no footer. | **Fixed inline in 5.5:** dropped `hasData`; footer now fires on `len(rates) == 0`, covering absent/empty/no-reviewer-rows uniformly. |
   | MEDIUM | internal/personas/list.go FormatRate | Clamp branches (<0, >1) untested. | **Fixed inline in 5.5:** added clamp tests (`-0.5`→`0.0%`, `1.5`→`100.0%`). |
   | LOW | internal/personas/list.go sortScoredPersonas | NaN rate would break strict-weak ordering; guarantee lived only upstream. | **Fixed inline in 5.5:** documented the finite-rate precondition (scorecard guards div-by-zero, so no NaN reaches the comparator). |
   | LOW | cmd/atcr/personas.go:GetBool | `GetBool("scores")` error ignored. | By design — flag registered two lines below; no action. |

   **Action Required:**
   - One HIGH (multi-model score collapse) → fixed inline in 5.5 with regression test. Two MEDIUM + one LOW also fixed inline in 5.5 (cheap, code already open). One LOW by-design.

### 5.5 [x] **[Bundles + Scores - REFACTOR](plan/user-stories/05-corroboration-feedback.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A and 5.4.A (if any)
   2. Confirm join-key normalization consistent both sides; `verify` still decoupled from `scorecard`; maintain green (T1), validate (T3)
   3. COMMIT: `git commit -m "refactor(personas): address review + score-key consistency"`
   **Duration:** 0.5 day

### 5.6 [x] **Phase 5 — DoD Validation**
   - `go test ./...` green (EXIT=0); `go test ./internal/personas/...` green for all bundle + score tests ✓
   - `atcr personas install bundle/django` integration test (`TestPersonasInstall_BundleClean`) passes against `httptest` server ✓; zero live network
   - `go build ./...` clean ✓; `go vet ./...` clean ✓; `golangci-lint run ./internal/personas/... ./cmd/atcr/...` 0 issues ✓
   - Coverage: internal/personas 86.9%, cmd/atcr 84.0% (both ≥80%)
   - DoD report (Stories 04 + 05 complete):
     ```
     Story-04 + Story-05 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 8/8 (AC 04-01 clean install, 04-02 partial skip, 04-03 unknown-bundle error, 04-04 manifest parse validation, 04-05 bundle test coverage; 05-01 baseline no-regression, 05-02 scores column, 05-03 sort ordering, 05-04 help docs)
     Manual Review: [ ] Code reviewed
     ```

### 5.LAST [x] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `--scores` map shape matches the T8 routing carrier (`map[string]float64`, lowercase key)?
       - CONFIG SURFACE: Bundle manifests documented; `--scores` help text present (AC 05-04)?
       - INTEGRATION: `bundle/` install reuses Phase 4 single-install path correctly?
       - PHASE-EXIT CONTRACT: Phase 6 docs can describe stable CLI/bundle surface?
       - REGRESSION: Baseline `list` + earlier phases intact; `go test ./...` clean?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings (fresh-context hostile integrator):** No CRITICAL/HIGH. REGRESSION clean — `go test ./...`, `go build ./...`, `go vet ./...`, `golangci-lint run` all pass; baseline `personas list` provably never loads the scorecard (asserted); bundle manifests well-formed with exact member lists; `bundle/` install reuses `Install` (same traversal guard + validate-before-write); unknown/malformed bundle writes nothing; `--scores` help text accurate (AC 05-04).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | personas.go reviewerCorroborationRates vs verify/select.go | Score-map key convention diverges: T6 lowercases reviewer name, T8 routing keys by raw registry name. No live bug (T8 passes nil; not wired to scores). | Deferred → TD-013 (reconcile when scores wire into routing; per Phase 2 clarification this lowercasing was a deliberate Phase 5/T6 deferral). |
   | LOW | internal/personas/list.go sortScoredPersonas | Comparator couples to upstream no-NaN invariant; a non-CLI caller could pass NaN. | Deferred → TD-014 (CLI caller sources finite rates; precondition documented). |
   | LOW | internal/personas/bundles.go InstallBundle | `personaPath` recomputed redundantly with `Install`. | Deferred → TD-015 (harmless defense-in-depth on a network-bound path). |

   **Action:** No CRITICAL/HIGH. One MEDIUM + two LOW integration notes deferred to `tech-debt-captured.md` (TD-013, TD-014, TD-015). **Phase gate passed.**
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 6: T7-in-repo — Docs + Validation (Sprint B, Days 15-17)

**Focus:** Installation guide, authoring template, registry.md update, example YAML updates, cumulative adversarial review.
**Story:** [06 — In-Repo Documentation](plan/user-stories/06-in-repo-documentation.md) | **ACs:** [06-01](plan/acceptance-criteria/06-01-personas-install-guide.md), [06-02](plan/acceptance-criteria/06-02-personas-authoring-guide.md), [06-03](plan/acceptance-criteria/06-03-registry-and-example-updates.md)

> AC 06-01 and 06-02 are **manual review** — verified by reading and following the docs without source-code lookups. AC 06-03 has an automated gate (`TestRegistryExamples_Valid`).

### 6.1 [ ] **[Docs + Example Validation - RED](plan/user-stories/06-in-repo-documentation.md)**
   Write failing test, verify it fails correctly:
   - `TestRegistryExamples_Valid` — loads both example YAML files through `internal/registry` to confirm clean parse after `language` additions
   **Files:** `internal/registry/<examples>_test.go` | **Duration:** 0.25 day

### 6.2 [ ] **[Docs + Example Validation - GREEN](plan/user-stories/06-in-repo-documentation.md)**
   Minimal code (T1), verify all (T2), COMMIT:
   - `docs/personas-install.md` — all 6 `atcr personas` subcommands, bundle syntax, `~/.config/atcr/personas/` path, `ATCR_PERSONAS_URL` override
   - `docs/personas-authoring.md` — persona template (prompt/severity rubric/output format/payload slots), canonical `language` format (`["go","ts"]` — no dot, lowercased), fixture requirements (`.patch`/`.diff` in `personas/testdata/`, synthetic values), contribution checklist
   - `docs/registry.md` — add `language` field entry: type (`[]string`), canonical form, nil semantics, routing behavior (two-partition reorder, silent fallback)
   - `examples/registry-without-executor.yaml` — add ≥1 agent with `language: ["go"]`
   - `examples/registry-with-executor.yaml` — same; remain valid YAML
   COMMIT: `git commit -m "docs(personas): install + authoring guides, registry + examples (green)"`
   **Files:** `docs/personas-install.md`, `docs/personas-authoring.md`, `docs/registry.md`, `examples/*.yaml` | **Duration:** 1.5 days

### 6.2.A [ ] **[Docs + Validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-in-repo-documentation.md)**
   **Changed Files:** `docs/personas-install.md`, `docs/personas-authoring.md`, `docs/registry.md`, `examples/registry-with-executor.yaml`, `examples/registry-without-executor.yaml`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 6.2. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 6.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 6.2]
     - Checklist (pass verbatim):
       - SECURITY: Docs advise synthetic-only fixture secrets? No real credentials in examples?
       - EDGE CASES: Install guide covers all 6 subcommands + bundle syntax + env override? Authoring fixture field list matches `TestPersonaFixture` logic?
       - ERROR HANDLING: Any reference to deprecated `docs/examples/registry.yaml` path? Example YAML still valid?
       - PERFORMANCE: N/A — doc accuracy: canonical `language` form consistent across all docs?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 6.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 6.3 [ ] **[Docs + Validation - REFACTOR](plan/user-stories/06-in-repo-documentation.md)**
   1. Fix CRITICAL/HIGH issues from 6.2.A (if any)
   2. Cross-reference authoring guide fixture field list against `TestPersonaFixture`; confirm no deprecated path references; maintain green (T3)
   3. Manual review: follow `docs/personas-install.md` and `docs/personas-authoring.md` without source lookups (AC 06-01, 06-02)
   4. COMMIT: `git commit -m "docs(personas): address review + cross-reference fixtures"`
   **Duration:** 0.5 day

### 6.4 [ ] **Phase 6 — DoD Validation**
   - `go test ./...` clean (all packages) including `TestRegistryExamples_Valid`
   - DoD report (Story-06 complete):
     ```
     Story-06 DoD Complete
     Auto: {X}/5 | Story-Specific: {Y}/{Z}
     Manual Review: [ ] Install guide walkthrough  [ ] Authoring guide validation
     ```

### 6.LAST [ ] **Phase 6 - GATE: Cumulative Integration & Exit Review (subagent)**
   **Scope:** Cumulative — full sprint diff (integration-level)

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 6 cumulative gate review`
   - prompt: Self-contained brief including:
     - Full sprint diff scope (absolute paths of all changed files)
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All acceptance criteria across Stories 01-06 satisfied?
       - CONFIG SURFACE: `language` field, `ATCR_PERSONAS_URL`, bundle manifests all documented + back-compat?
       - INTEGRATION: install → list → test fixture roundtrip via `httptest` works end-to-end?
       - PHASE-EXIT CONTRACT: Binary ships 9 personas, 15 subcommands, 2 bundles; community fetch path isolated?
       - REGRESSION: `go test ./...` clean, `golangci-lint run` clean, `go vet ./...` clean, `go build ./...` clean?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to Final Phase
   **Duration:** 15-30 min
   **— END SPRINT B (Phases 4-6) —**

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%); `internal/personas/` ≥80%
- [ ] Lint/format clean: `golangci-lint run`, `gofmt`/`go vet ./...`
- [ ] Build succeeds: `go build ./...`
- [ ] Zero live network calls in CI (all HTTP via `httptest.NewServer`)
- [ ] `Names()` returns 9; root exposes 15 subcommands
- [ ] Path-traversal guard verified; community YAML validated before write

### Optional: Targeted Mutation Testing
MUTATION_TOOL: **UNAVAILABLE** — no mutation tool detected (`stryker-mutator` / `mutmut` / `cargo-mutants` absent). Skip mutation testing.
**WARNING:** Do NOT run full codebase mutation — it can take hours. Target specific files only if a tool becomes available.

### Drift Analysis
Compare delivered work against [plan/original-requirements.md](plan/original-requirements.md):
- 3 bonus built-in personas with CI fixtures ✓ (Phase 3)
- `atcr personas` install/list/search/remove/test/upgrade ✓ (Phase 4)
- `bundle/` install + `bundle/django`, `bundle/go-production` ✓ (Phase 5)
- `language` scope field + language-aware `SelectEligibleSkeptics` routing + silent fallback ✓ (Phases 1-2)
- `atcr personas list --scores` corroboration ✓ (Phase 5)
- In-repo install + authoring docs ✓ (Phase 6)
- **Descoped (out of scope, confirmed):** T3/T4 community repo scaffold + seed personas, community-repo half of T7 (contribution guide + community CI) — external repo does not exist in this workspace.

If any task drifted from the original request, STOP and validate before marking the sprint complete.

---

**Next:** `/execute-sprint @.planning/sprints/active/9.0_persona_ecosystem/`
