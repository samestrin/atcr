# Sprint 19.10: Reviewer Payload Sizing

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.10 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — `/execute-sprint` stops at each phase boundary (the `N.LAST` gate) instead of running continuously.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Per-model payload sizing for the atcr multi-agent reviewer. Today a single global byte budget is shipped to a heterogeneous roster whose models span 32k→144k-token windows, so a large diff either gets gutted (files shed) or overflows small-window models entirely. This sprint sizes each reviewer's payload to its own model's token window (reserving the output-token budget) and, when a payload still doesn't fit, chunks it to fit via the existing Epic 14.3 chunker made window-aware — degrading gracefully through a configurable `on_overflow` policy instead of silently dropping content.

### Why This Matters

Confirmed in the 19.6 multi-agent review run: a 101-file / 6,429-insertion diff returned **1 finding from 11 reviewers (5 ok, 3 timeout, 3 failed)**. A code-review product could not review its own large sprint. This sprint makes the reviewer able to review its own work without gutting the panel.

### Key Deliverables

- Per-model context-window resolver (`ContextWindowTokens`) with a conservative default (F1)
- Output-reserved, per-agent effective input budget that eliminates the confirmed `dax` `24577 + 8192 > 32768` overflow (F2)
- Window-aware chunking that delivers the whole diff across appropriately-sized chunks per model (F3)
- Configurable `on_overflow` policy — `chunk` (default) + `truncate` implemented; `fallback` + `fail` recognized (F4)
- Fallback provenance recording and reconcile de-weighting so a swapped model is never counted as a distinct reviewer (F5)
- Load-scaled request timeout so multi-chunk payloads on slow backends no longer hit the 600 s wall (F6)
- Cache-key correctness so a per-agent-sized payload is never served a stale full-payload hit (F7)
- Per-agent diagnosability fields in `summary.json` (F8)
- Configurable `max_sprint_plan_bytes` limit (F9)
- Standalone, env-coupled live-audit harness replaying the exact 19.6 range (AC-Live)

### Success Criteria

- The reviewer reviews its own 6,400-line sprint without gutting the panel
- No agent hard-fails on context overflow; degradation is visible in `summary.json`, never silent
- Panel model-diversity is preserved on the default path (chunk, same model); any fallback swap is recorded
- `go test ./...` passes; the 5 previously-failing agents (`dax`, `otto`, `greta`, `vera`, `brad`) all complete `status=ok` in the AC-Live run

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**TDD Mode:** auto (task-based, non-feature plan). This is an **infrastructure** plan sourced from `tasks/`, so each work item follows the TASK-BASED cadence (understand → test → implement → verify → document) rather than the feature RED/GREEN/REFACTOR split.

**Test-first discipline still applies:** every code-bearing task lands its unit/integration tests alongside (or before) the implementation, per the co-located `*_test.go` convention. Regression tests that explicitly name the confirmed `dax` boundary arithmetic (`24577 + 8192 > 32768`) are mandatory for Tasks 01–03 so a future refactor cannot silently reintroduce the exact bug this sprint fixes.

**Adversarial review:** ENABLED 🎯 (implied by `--gated`). Because this is a task-based plan, the adversarial pass runs at each **phase boundary** via the `N.LAST` gate (fresh subagent, hostile-integrator perspective) rather than per-task. Inline-fix bar: **CRITICAL/HIGH**. Defer to tech debt: **MEDIUM/LOW**.

**Execution mode:** Gated 🚧 — `/execute-sprint` stops at each phase's `N.LAST` gate.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [plan.md](plan/plan.md) | Plan overview |
| [tasks/](plan/tasks/) | Detailed per-task specifications (12 tasks) |
| [documentation/](plan/documentation/) | Per-feature reference docs (resolver, budget, overflow, cache, timeout, provenance) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/<pkg>/ -run <TestName>` |
| T2: Module | After completing a task | `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` (+ `go vet ./...`, lint) |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `go vet ./...` + project linters clean
4. Build: `go build ./...` succeeds
5. Docs: task files and `summary.json` field docs updated

### DoD Report Template
```
Phase-{N} DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

Follow the project specifications:

- **Implementation:** [implementation-standards.md](../../../specifications/implementation-standards.md)
- **Coding:** [coding-standards.md](../../../specifications/coding-standards.md) — table-driven Go tests + `testify` assertions; co-located `*_test.go`.
- **Git:** [git-strategy.md](../../../specifications/git-strategy.md) — conventional commits; work on branch `feature/19.10_reviewer_payload_sizing`.

**Module boundary rules (from sprint-design Architecture):**
- `internal/payload` stays free of any `internal/registry` import (avoids an import cycle) — `ReadSprintPlan`/`ScopeConstraint` take `maxBytes int64` as a caller-supplied parameter, not a config lookup.
- `internal/fanout` is the sole consumer threading model identity, resolved config, and dispatch together.
- `internal/reconcile`'s extracted library boundary (`github.com/samestrin/atcr/reconcile`) is **not** touched — provenance is stamped only on ATCR-internal `stream.Finding`/`JSONFinding`.
- Byte→token ratio is an intentionally conservative static constant (~3.5 B/token), **not** the codebase's optimistic ~4.1 B/token comment.

---

## External Resources

Per-feature reference documentation lives under [plan/documentation/](plan/documentation/):

- [context-window-resolver.md](plan/documentation/context-window-resolver.md) — static per-model window table (F1)
- [per-agent-budget-and-chunking.md](plan/documentation/per-agent-budget-and-chunking.md) — output-reserved budget + chunk plan (F2/F3)
- [on-overflow-policy.md](plan/documentation/on-overflow-policy.md) — degradation ladder + config surface (F4)
- [cache-key-correctness.md](plan/documentation/cache-key-correctness.md) — folding sizing into the diff-cache key (F7)
- [diagnosability-fields.md](plan/documentation/diagnosability-fields.md) — per-agent `summary.json` fields (F8)
- [fallback-provenance.md](plan/documentation/fallback-provenance.md) — fallback substitution recording (F5)
- [timeout-scaling.md](plan/documentation/timeout-scaling.md) — load-scaled timeout for chunked payloads (F6)
- [config-yaml-parsing.md](plan/documentation/config-yaml-parsing.md) — `yaml.v3` patterns for `max_sprint_plan_bytes`/`on_overflow` (F9/F4)

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation

*Establish the two foundational primitives — window lookup and policy config surface — that every downstream task consumes. Tasks 01 and 05 have no hard dependency on each other and may proceed in parallel.*

### 1.1 [x] **🏗️ Per-Model Context-Window Resolver (F1)**
   **Task:** Add a static `model → token window` table keyed by model id, with a conservative default for unknown models, exposing each roster model's token window. Deterministic; no hot-path network call. Name it distinctly (`ContextWindowTokens`) so it is never confused with the per-chunk diff-line budget `MaxContextLines`.
   **Priority:** High | **Effort:** S
   1. Understand issue, identify affected files (`internal/payload`)
   2. Write tests: table-driven known-model / unknown-model / persona-coverage cases (`internal/payload/contextwindow_test.go`)
   3. Implement `ContextWindowTokens(model string) int` + static table + conservative default
   4. Verify — `ContextWindowTokens` never returns zero; unknown ids return the conservative default
   5. Document the table as the single source of truth (so Epic 19.7 can later swap it live behind the same signature)
   **Success Criteria:** AC1 (partial) — resolver returns each roster model's window; unknown → conservative default; deterministic.
   **Files:** `internal/payload/contextwindow.go` | `internal/payload/contextwindow_test.go` | **Duration:** ~0.5 day
   **Task File:** [task-01](plan/tasks/task-01-context-window-resolver.md)

### 1.2 [x] **🏗️ `on_overflow` Config Schema (F4 config)**
   **Task:** Parse/validate/resolve the `on_overflow` policy string through the registry→project precedence chain. Enum validation (`onOverflowValid`) for exactly 4 legal values (`chunk`/`truncate`/`fallback`/`fail`); strict `KnownFields(true)` YAML decoding at every tier; whitespace-only falls through to the next tier (mirroring existing `ReviewStrategy` behavior). Physically separate edits near the closest existing precedent field (`ReviewStrategy`) to minimize merge risk with Task 11.
   **Priority:** High | **Effort:** S
   1. Understand precedence chain (`internal/registry` config.go / project.go / precedence.go)
   2. Write tests: `onOverflowValid`, precedence-chain resolution, typo'd-key rejection, whitespace fallthrough
   3. Implement config key + enum validation + precedence resolution + post-resolution sanity re-check in `ResolveSettings`
   4. Verify — invalid/out-of-range value errors clearly at load time; default resolves to `chunk`
   5. Document the new config key
   **Success Criteria:** AC4 (config portion) — `on_overflow` recognized as a config key with enum validation across the precedence chain.
   **Files:** `internal/registry/*.go` | `internal/registry/on_overflow_test.go`, `precedence_test.go` | `.atcr/config.yaml` | **Duration:** ~0.5 day
   **Task File:** [task-05](plan/tasks/task-05-on-overflow-config-schema.md)

### 1.3 [x] **Phase 1 — DoD Validation**
   - [x] `go test ./internal/payload/... ./internal/registry/...` passing (T3 scoped)
   - [x] Coverage ≥80% on new code (new fns 100%; modules 90.2% / 92.0%)
   - [x] `go vet ./...` clean
   - [x] `go build ./...` succeeds
   - [x] `ContextWindowTokens` + `on_overflow` config key documented
   - [x] DoD report emitted

### 1.4 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced (`internal/payload` free of `internal/registry` import)?
       - PHASE-EXIT CONTRACT: Can Phase 2 consume `ContextWindowTokens` + resolved `on_overflow` without rework?
       - REGRESSION: Earlier behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context integration review, 2026-07-10):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | docs/registry.md:122,164 | `on_overflow` key absent from canonical reference doc (present in scaffold + struct comments) | Deferred → TD-001 in `tech-debt-captured.md` |

   No CRITICAL/HIGH. Verified clean: `internal/payload` free of `internal/registry` import; precedence overlays at both tiers + post-resolution re-check; `onOverflowValid` symmetric with `reviewStrategyValid`; back-compat holds; no stray `Effective*()` resolver. **Phase gate passed** (single LOW deferred to TD-001).
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Core Sizing

*Directly closes the confirmed `dax` boundary-overflow arithmetic (AC1, AC2) and produces the first end-to-end degradation primitive (AC3). Task 11 is an independent parallel track that shares config files with Task 05 — coordinate merges.*

### 2.1 [x] **🏗️ Per-Agent Effective Input Budget (F2)**
   **Task:** Derive an output-reserved, model-derived byte budget so estimated input tokens ≤ `contextWindow − defaultMaxTokens − promptOverhead`, using the conservative ~3.5 B/token ratio. Wire into both `ApplyByteBudget` call sites in `internal/fanout/review.go` (`:464`, `:726`). Degenerate windows (smaller than `defaultMaxTokens + promptOverhead`) return 0, never negative/panicking.
   **Priority:** High | **Effort:** M | **Depends on:** 1.1
   1. Understand both `ApplyByteBudget` call sites and the byte→token ratio decision
   2. Write tests: `EffectiveByteBudget(model, outputTokens) int64` + **dax-boundary regression test naming `24577 + 8192 > 32768`**
   3. Implement `EffectiveByteBudget` in `internal/payload/sizing.go`; wire both call sites
   4. Verify — output cap is reserved; degenerate windows return 0
   5. Document the ratio choice and reservation arithmetic
   **Success Criteria:** AC1, AC2 — payload input tokens ≤ `W − O` for both a 32k and a 144k model; the `dax` overflow cannot recur.
   **Files:** `internal/payload/sizing.go`, `sizing_test.go` | `internal/fanout/review.go` | **Duration:** ~1 day
   **Task File:** [task-02](plan/tasks/task-02-per-agent-effective-budget.md)

### 2.2 [x] **🏗️ Window-Aware Chunking (F3)**
   **Task:** Convert Task 02's effective token budget into a per-model chunk budget (`maxLines`) feeding the existing 14.3 `chunkDiff`. On overflow with `on_overflow: chunk`, the diff is delivered whole across N appropriately-sized chunks per model (more chunks for a 32k model than a 144k model); respect the existing 64-chunk/agent ceiling. Clamp `maxLines` to a small positive floor — never trigger `chunkDiff`'s "unlimited" branch unintentionally.
   **Priority:** High | **Effort:** M | **Depends on:** 1.1, 2.1
   1. Understand the existing `chunkDiff` (`internal/fanout/chunker.go`) and its 64-chunk cap
   2. Write tests: chunk-plan helper + lossless-reassembly test (zero files dropped); 32k vs 144k chunk-count differential
   3. Implement the per-model `maxLines` derivation + wire into `chunkDiff` overflow path
   4. Verify — whole diff reassembles; chunk count scales with window; 64-chunk ceiling respected
   5. Document chunk-plan derivation
   **Success Criteria:** AC3 — over-window payload delivered whole across appropriately-sized chunks with zero files dropped.
   **Files:** `internal/fanout/chunker.go` | `internal/fanout/overflow_test.go` (chunk-plan) | **Duration:** ~1 day
   **Task File:** [task-03](plan/tasks/task-03-window-aware-chunking.md)

### 2.3 [x] **🏗️ Configurable Sprint-Plan Limit (F9)**
   **Task:** Replace the hardcoded `MaxSprintPlanBytes` constant (16KB) with a configurable `max_sprint_plan_bytes` key in `.atcr/config.yaml` (default 65536 / 64KB), parsed through the `internal/registry` precedence chain and passed as a caller-supplied `maxBytes int64` parameter into `internal/payload/sprintplan.go` (no `internal/payload`→`internal/registry` import). `> 0` validation (0 is NOT a valid "unbounded" sentinel here). Place edits near the `CacheMaxBytes` precedent to minimize merge overlap with Task 05.
   **Priority:** Medium | **Effort:** S | **Depends on:** — (shares config files with 1.2 — coordinate merges)
   1. Understand `sprintplan.go`'s current constant and the `CacheMaxBytes` precedence precedent
   2. Write tests: `max_sprint_plan_bytes` precedence chain + caller-supplied-limit tests + `> 0` validation
   3. Implement config key + resolver; parameterize `ReadSprintPlan`/`ScopeConstraint` with `maxBytes`
   4. Verify — default 64KB; caller-supplied limit honored; invalid ≤0 rejected
   5. Document the new config key
   **Success Criteria:** AC10 — sprint-plan byte limit configurable via `max_sprint_plan_bytes`; verified by test.
   **Files:** `internal/registry/*.go`, `sprintplan_settings_test.go` | `internal/payload/sprintplan.go`, `sprintplan_test.go` | `.atcr/config.yaml` | **Duration:** ~0.5 day
   **Task File:** [task-11](plan/tasks/task-11-configurable-sprint-plan-limit.md)

### 2.4 [x] **Phase 2 — DoD Validation**
   - [x] `go test ./internal/payload/... ./internal/fanout/... ./internal/registry/...` passing (full `go test ./...` exit=0)
   - [x] dax-boundary regression test present and passing (`TestEffectiveByteBudget_DaxBoundaryRegression`)
   - [x] Lossless chunk-reassembly test passing (zero files dropped) (`TestChunkDiff_WindowDerivedMaxLines`)
   - [x] Coverage ≥80% on new code (EffectiveByteBudget/ChunkMaxLines/ScopeConstraint 100%; ResolveSettings 98.4%; modules payload 90.3% / fanout 87.9% / registry 92.1%)
   - [x] `go vet ./...` clean; `go build ./...` succeeds
   - [x] DoD report emitted

   ```
   Phase-2 DoD Complete
   Auto: 5/5 | Task-Specific: dax-regression ✓, lossless-chunk ✓, per-agent-shed ✓, F9-config ✓
   Manual Review: [ ] Code reviewed (→ Phase 2 gate subagent, next)
   ```

### 2.5 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

   Fresh-context subagent (`general-purpose`) reviewed all Phase 2 changes: `internal/payload/{sizing.go,sizing_test.go,sprintplan.go,sprintplan_test.go}`, `internal/fanout/{review.go,chunker_test.go,review_test.go,review_sprintplan_test.go,sizing_review_test.go}`, `internal/registry/{config.go,precedence.go,project.go,sprintplan_settings_test.go}`, `.atcr/config.yaml`.

   **Subagent findings (fresh-context integration review, 2026-07-10):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | review.go per-agent bulk shed | Per-agent re-shed could dispatch an EMPTY payload when a single file exceeds a small model's window (`AllDropped`), causing a silent false-clean "no findings" review | **FIXED inline** — added `AllDropped` guard: keep the whole global payload + warn (`warnOversized`) instead of shipping empty; regression test `TestBuildSlots_PerAgentBudgetNeverEmptyPayload`. Re-run gate: **clean**. This also made the chunked single-oversized-file fall-through lossless. |
   | MEDIUM | review.go:951 | Degenerate window (`eff==0`) falls back to full global payload | Deferred → TD-002 (unreachable today; smallest window 32768 ≫ 12288 threshold; Phase 3 on_overflow is the net) |
   | LOW | sprintplan.go:94 | Directly-constructed `Settings{MaxSprintPlanBytes:0}` silently blanks the plan | Deferred → TD-003 (unreachable via any production config path; all tiers reject `<=0`) |

   Verified clean: `internal/payload` free of `internal/registry` import; both `ApplyByteBudget` call sites wired per-agent; 64-chunk ceiling respected; chunked path lossless; dax-boundary + lossless-reassembly tests non-tautological; `max_sprint_plan_bytes` documented/defaulted (64KB)/`>0`-validated at all three tiers + post-resolution; explicit `max_context_lines` wins. **Phase gate passed** (1 HIGH fixed inline, 2 deferred to TD).
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Overflow & Provenance

*Completes the degradation policy ladder (AC4) and closes the provenance-integrity NFR (AC5) before diagnosability/cache work needs to read from it.*

### 3.1 [ ] **🏗️ `on_overflow` Policy Dispatch (F4 dispatch)**
   **Task:** A single dispatch function (`applyOverflowPolicy` in `internal/fanout/overflow.go`) routing `chunk`/`truncate`/`fallback`/`fail` to the correct primitive. `chunk` (default, → window-aware chunker) and `truncate` (→ in-repo auto-truncate signal, drop lowest-priority tail + flag) implemented; `fallback`/`fail` return typed/sentinel errors when prerequisites unmet — never silent no-ops or accidental fallthrough to another arm.
   **Priority:** High | **Effort:** M | **Depends on:** 2.2
   1. Understand the degradation ladder and existing shed/chunk primitives
   2. Write tests: 4-arm dispatch + unrecognized-policy error test
   3. Implement `applyOverflowPolicy` + `OverflowPolicy`/`OverflowResult` types
   4. Verify — each arm routes correctly; unimplemented arms error clearly
   5. Document the dispatch contract
   **Success Criteria:** AC4 — `chunk`/`truncate` implemented + tested; `fallback`/`fail` recognized and error clearly if prerequisites unmet.
   **Files:** `internal/fanout/overflow.go`, `overflow_test.go` | **Duration:** ~1 day
   **Task File:** [task-04](plan/tasks/task-04-on-overflow-policy-dispatch.md)

### 3.2 [ ] **🏗️ Fallback Provenance — Fanout side (F5 part 1)**
   **Task:** Add a run-level `FallbackCount` tally in `Summary`/`PoolSummary`, plus fixture/e2e proof the bulk path already threads `FallbackUsed`/`FallbackFrom` correctly. Fail-closed: missing/malformed status data → treated as non-fallback, never "assume fallback". `fallback_count` is always present (non-omitted-when-zero, per the `Truncation`/`TruncatedZeroFindings` precedent).
   **Priority:** High | **Effort:** S | **Depends on:** — (parallel with 3.1)
   1. Understand `PoolSummary`/`Summary` in `internal/fanout/artifacts.go` and existing `FallbackUsed`/`FallbackFrom` threading
   2. Write tests: bulk-path fixture + e2e proving provenance threads through
   3. Implement `Summary.FallbackCount` / `PoolSummary.FallbackCount` tally
   4. Verify — count present with zero value on zero-fallback runs; fail-closed on malformed data
   5. Document the summary field
   **Success Criteria:** AC5 (part 1) — `summary.json` records fallback substitutions.
   **Files:** `internal/fanout/artifacts.go`, `artifacts_test.go`, `outcome_test.go` | **Duration:** ~0.5 day
   **Task File:** [task-06](plan/tasks/task-06-fallback-provenance-fanout.md)

### 3.3 [ ] **🏗️ Reconcile Fallback-Aware De-Weighting (F5 part 2)**
   **Task:** Consume Task 06's provenance to collapse shared-fallback-model reviewers into one independent voice in the distinct-reviewer CONFIDENCE calculus (`distinctReviewerCount`). Stamp provenance only on ATCR-internal `stream.Finding`/`JSONFinding` — do NOT touch the extracted `github.com/samestrin/atcr/reconcile` library boundary (mirrors the `PathValid`/`PathWarning` precedent).
   **Priority:** High | **Effort:** M | **Depends on:** 3.2
   1. Understand `distinctReviewerCount` and the CONFIDENCE calculus in `internal/reconcile`
   2. Write tests: collapsed-independence fixture — two personas on the same fallback model count as one voice
   3. Implement fallback-aware de-weighting
   4. Verify — reconcile does not count a fallback as the original distinct reviewer
   5. Document the de-weighting rule
   **Success Criteria:** AC5 (part 2) — reconcile does not count a fallback slot as the original distinct reviewer.
   **Files:** `internal/reconcile/disagree.go`/`emit.go`, `disagree_test.go`, `emit_test.go`, `adapter/adapter_test.go` | **Duration:** ~1 day
   **Task File:** [task-07](plan/tasks/task-07-reconcile-fallback-deweighting.md)

### 3.4 [ ] **Phase 3 — DoD Validation**
   - [ ] `go test ./internal/fanout/... ./internal/reconcile/...` passing
   - [ ] 4-arm dispatch + unrecognized-policy tests passing
   - [ ] Collapsed-independence fixture passing
   - [ ] Coverage ≥80% on new code
   - [ ] `go vet ./...` clean; `go build ./...` succeeds
   - [ ] DoD report emitted

### 3.5 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Every `on_overflow` arm routes correctly; `fallback`/`fail` error (never silent no-op)?
       - CONFIG SURFACE: Policy dispatch reads the resolved `on_overflow` value from Phase 1?
       - INTEGRATION: Reconcile de-weighting stamps only ATCR-internal findings, not the extracted `reconcile` library; fail-closed on malformed provenance?
       - PHASE-EXIT CONTRACT: Can Phase 4 diagnosability read `fallback_count` + degradation action without rework?
       - REGRESSION: Chunk default path + earlier reconcile behavior intact?
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

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Integration

*Where every prior task's output becomes observable and safe under caching — the three tasks most exposed to "silent regression" risk per the plan's Risk table (timeout, cache staleness, diagnosability).*

### 4.1 [ ] **🏗️ Timeout Scaling (F6)**
   **Task:** Scale both the per-call (`invokeAgent`) and aggregate (`runEngine`) deadlines from `(base timeout, chunk count)`, monotonic and clamped to `registry.MaxTimeoutSecs` (86400s). Read the already-resolved `EffectiveTimeoutSecs` value in `internal/fanout` (`review.go:516`, `engine.go:610`) — do NOT modify `internal/registry`'s resolvers. No-op at `chunkTotal <= 1`.
   **Priority:** High | **Effort:** S | **Depends on:** 2.2
   1. Understand the deadline seams (`review.go:516`, `engine.go:610`) and `EffectiveTimeoutSecs`
   2. Write tests: `scaledTimeoutSecs` monotonicity + clamp + `chunkTotal<=1` no-op
   3. Implement scaling at both per-call and aggregate seams
   4. Verify — a large-but-valid multi-chunk payload no longer hits the 600s wall; clamped to max
   5. Document the scaling curve
   **Success Criteria:** AC6 — `greta`/`vera`/`brad` complete on a large-but-valid multi-chunk payload without hitting the wall.
   **Files:** `internal/fanout/review.go`, `engine.go`, `engine_test.go` | **Duration:** ~0.5 day
   **Task File:** [task-08](plan/tasks/task-08-timeout-scaling.md)

### 4.2 [ ] **🏗️ Cache-Key Correctness (F7)**
   **Task:** Fold the per-agent effective budget / chunk-plan into `diffCacheKey`'s NUL-separated tuning token (`internal/fanout/cache.go`), so a per-agent-sized payload is never served a stale full-payload (or differently-sized) cache hit. Include the boundary case where two runs render identical prompt text under different sizing regimes.
   **Priority:** High | **Effort:** S | **Depends on:** 2.1, 2.2
   1. Understand `diffCacheKey` and its tuning-token construction
   2. Write tests: `TestEngine_DifferentSizingMissesCache` (verified to fail against pre-F7 code) + collision + backward-compat
   3. Implement the sizing-token fold-in
   4. Verify — distinct keys for distinct sizing regimes even on identical prompt text
   5. Document the key composition
   **Success Criteria:** AC7 — fan-out cache key incorporates per-agent budget/chunk plan; no stale full-payload hit.
   **Files:** `internal/fanout/cache.go`, `cache_test.go` | **Duration:** ~0.5 day
   **Task File:** [task-09](plan/tasks/task-09-cache-key-correctness.md)

### 4.3 [ ] **🏗️ Diagnosability Fields (F8)**
   **Task:** Extend `AgentStatus` with `effective_budget`/`resolved_window`/`reserved_output_tokens`/`chunk_count`/`degradation_action` — pure aggregation from Tasks 02/03/04/06's already-computed values. Fields always present with zero/absent per `omitempty` discipline (never silently omitted when `0` is the meaningful answer). JSON round-trip verified.
   **Priority:** High | **Effort:** S | **Depends on:** 2.1, 2.2, 3.1, 3.2
   1. Understand `AgentStatus` and the `omitempty` discipline precedent
   2. Write tests: 5-field extension JSON round-trip + omitempty tests
   3. Implement the field aggregation into `summary.json`/`status.json`
   4. Verify — all fields recorded per agent; zero values present where meaningful
   5. Document the fields
   **Success Criteria:** AC8 — `summary.json` records per-agent effective budget, resolved window, reserved output tokens, chunk count, degradation action.
   **Files:** `internal/fanout/artifacts.go`/`status.go`, `status_test.go`, `artifacts_test.go` | **Duration:** ~0.5 day
   **Task File:** [task-10](plan/tasks/task-10-diagnosability-fields.md)

### 4.4 [ ] **Phase 4 — DoD Validation**
   - [ ] `go test ./internal/fanout/...` passing
   - [ ] `TestEngine_DifferentSizingMissesCache` present and passing
   - [ ] Diagnosability JSON round-trip passing
   - [ ] Coverage ≥80% on new code
   - [ ] `go vet ./...` clean; `go build ./...` succeeds
   - [ ] DoD report emitted

### 4.5 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Timeout scaling clamped + monotonic + no-op at chunkTotal<=1; cache key distinct per sizing regime?
       - CONFIG SURFACE: No `internal/registry` resolver modified by timeout scaling (reads resolved values only)?
       - INTEGRATION: Diagnosability fields aggregate real values from Tasks 02/03/04/06; omitempty discipline correct (zero present when meaningful)?
       - PHASE-EXIT CONTRACT: Can the AC-Live harness read all diagnosability fields from `summary.json`?
       - REGRESSION: Cache backward-compat + earlier phases intact?
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

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Validation

*Full-suite regression plus the standalone live-audit dry run. Task 12 is sequenced last by design and depends on Tasks 01–11.*

### 5.1 [ ] **🏗️ Live Audit Harness (AC-Live)**
   **Task:** `examples/19.10-live-audit.sh`: skip-guards on unreachable roster (`atcr doctor`), re-runs the exact confirmed 19.6 range (base `f9d5161…` → head `b6bcb67…`, 101 files) against the real `orchestrator.lan` roster, hard-gates on zero `ContextWindowExceededError` + all five previously-failing agents (`dax`, `otto`, `greta`, `vera`, `brad`) `status=ok` (auto-checked from `summary.json`) + findings from ≥2 agents, and prints a before/after evidence table. Must remain fully decoupled from `go test ./...` so CI never blocks on external reachability.
   **Priority:** High | **Effort:** M | **Depends on:** 1.1–4.3 (all prior tasks)
   1. Understand the confirmed 19.6 range + `summary.json` gate assertions
   2. Write the skip/pass/fail gate logic (skip-path verifiable without live access)
   3. Implement `examples/19.10-live-audit.sh` (standalone-runnable + invoked by the execution loop)
   4. Verify — skip-path is a no-op when roster unreachable; full-path gates hard on the deterministic assertions
   5. Document the manual dry-run procedure (skip-path locally; full-path requires `orchestrator.lan`)
   **Success Criteria:** AC-Live — zero `ContextWindowExceededError`; the 5 previously-failing agents all `status=ok`; findings from ≥2 agents; before/after evidence captured.
   **Files:** `examples/19.10-live-audit.sh` | **Duration:** ~1 day + live-audit buffer
   **Task File:** [task-12](plan/tasks/task-12-live-audit-harness.md)

### 5.2 [ ] **Phase 5 — DoD Validation**
   - [ ] `go test ./...` passing (full suite)
   - [ ] `go vet ./...` clean; project linters clean; `go build ./...` succeeds
   - [ ] Coverage ≥80% overall
   - [ ] Live-audit **skip-path** dry run verified (no live access required); full-path procedure documented
   - [ ] All 10 ACs (AC1–AC10) + AC-Live traced to a passing test or documented manual verification
   - [ ] DoD report emitted

### 5.3 [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 + full-sprint integration (final gate)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 + cross-sprint integration (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Live-audit harness fully decoupled from `go test ./...`; skip-guard correct?
       - CONFIG SURFACE: Both new config keys (`on_overflow`, `max_sprint_plan_bytes`) documented + defaulted + back-compat across the full config surface?
       - INTEGRATION: End-to-end sizing concept (model → window → budget → chunk plan → degradation action) threads correctly through dispatch, cache, timeout, diagnosability, reconcile?
       - PHASE-EXIT CONTRACT: Sprint delivers every AC (AC1–AC10 + AC-Live)?
       - REGRESSION: `go test ./...` green; no earlier-phase behavior broken?
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

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%)
- [ ] Lint/format clean: `go vet ./...` + project linters
- [ ] Build succeeds: `go build ./...`
- [ ] All 12 tasks checked off; all 5 phase gates passed
- [ ] Every AC (AC1–AC10 + AC-Live) traced to a passing test or documented manual verification

### Optional: Targeted Mutation Testing
Mutation tooling is **UNAVAILABLE** in this project (no `stryker-mutator` / `mutmut` / `cargo-mutants` detected). Skip. If a Go mutation tool (e.g. `go-mutesting`, `gremlins`) is later added, target only the high-risk changed files (`internal/payload/sizing.go`, `internal/fanout/overflow.go`, `internal/fanout/cache.go`) — **do NOT run full-codebase mutation** (it can take hours).

### Drift Analysis
Compare the delivered sprint against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] Every functional requirement F1–F9 delivered and traced to a task
- [ ] Non-functional requirements honored (determinism; conservative ~3.5 B/token over-reservation; no content loss on the `chunk` default path; provenance integrity)
- [ ] Out-of-scope items NOT implemented (live/dynamic window resolution → Epic 19.7; broader prompt-cache layer; per-model config-schema window field; reconciler wording beyond F5)
- [ ] No scope added beyond the original request
