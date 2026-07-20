# Sprint 32.1: Multi-Tier Fix Execution Engine

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 32.1 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A complexity-ceiling configuration surface on atcr's fix executor (`max_estimated_minutes`, optional `max_severity_for_fix`), wired into `generateFixes`'s existing pre-dispatch skip chain so a cheap/local-model executor safely skips-and-logs a finding beyond its capability instead of attempting it or hallucinating a partial fix. A second, independently-configured executor run can then pick up exactly the findings the first tier skipped, delivering a two-tier cheap-then-frontier fix workflow end-to-end.

### Why This Matters

Today every eligible finding is dispatched to whichever single executor is configured, wasting frontier-model spend on trivial fixes and risking a botched attempt when a cheap/local model tackles something beyond it. Routing on the complexity signal reviewers already emit (`EstMinutes`) lets atcr operators on the BYO-Keys architecture cut cost without sacrificing fix quality.

### Key Deliverables

- `ExecutorConfig` complexity-ceiling fields (`MaxEstimatedMinutes`, `MaxSeverityForFix`) with `EffectiveMaxEstimatedMinutes()`/`EffectiveMaxSeverityForFix()` resolvers and full `validateExecutor` range + cross-field validation
- A `withinComplexityCeiling` predicate wired into `generateFixes`'s pre-dispatch skip chain, plus a self-gating decline branch — both surfaced via the existing `FixWarning` + `logPipelineWarning("executor_ceiling_skip", ...)` contract
- An integration/E2E-verified two-tier workflow (low-ceiling tier 1, high/no-ceiling tier 2) proving every finding is fixed by exactly one tier or explicitly skipped-and-logged — never both, never neither
- Updated `docs/registry.md` / `docs/findings-format.md` and a worked two-tier example in `examples/registry-with-executor.yaml`, validated by a dry-run config load

### Success Criteria

- Executor config supports complexity ceilings, validated the same way existing executor fields are (AC 01-01, 01-02, 04-01, 04-02)
- `generateFixes` skips (and logs) over-ceiling findings without disturbing the existing confidence/severity/attribution filters (AC 02-01, 02-02, 02-03)
- A two-tier run partitions every finding exactly once, with fix attribution preventing double-processing across tiers (AC 03-01, 03-02, 03-03)
- Documentation and a runnable worked example let an operator configure a two-tier workflow without reading source (AC 05-01, 05-02)

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Complexity:** 8/12 (COMPLEX) → **Default TDD Mode:** Moderate 🔄 (RED, then GREEN+REFACTOR combined) for all 5 stories — `--tdd-strict`/`--tdd-pragmatic` were not passed, so this was calculated automatically from the complexity score.

**Adversarial Review:** ENABLED 🎯 — a fresh, memory-less subagent reviews every element's changed files after GREEN, before REFACTOR. Findings rated CRITICAL/HIGH are fixed inline in REFACTOR; MEDIUM/LOW are deferred to `clarifications/tech-debt-captured.md`.

**Gated Execution:** ENABLED 🚧 — `/execute-sprint` stops at the end of each phase for a phase-boundary integration gate review before continuing to the next phase.

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
| T1: Focused | After each small change | `go test ./internal/registry/... -run <Test>` or `go test ./internal/verify/... -run <Test>` |
| T2: Module | After completing element | `go test ./internal/registry/...` or `go test ./internal/verify/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): All passing
2. Coverage: ≥80%
3. Lint: No errors (`golangci-lint run`)
4. Build: Succeeds (`go build ./...`)
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

### Coding Standards (Go)

- **Naming:** Packages lowercase single-word; exported identifiers `PascalCase`; unexported `camelCase`; files snake_case or lowercase; interfaces ending in "-er" for single-action behavior.
- **Imports:** stdlib → third-party → `github.com/samestrin/atcr/...` internal, arranged via `goimports`.
- **Error Handling:** `error` as last return value, never ignored, wrapped with `fmt.Errorf("doing action: %w", err)`. No `panic` for normal error conditions.
- **Context:** Accept `context.Context` as first parameter for I/O/long-running calls; respect cancellation.
- **Structs/Interfaces:** Return concrete types from constructors, accept interfaces as parameters, keep interfaces small.
- **Testing:** Table-driven tests; `*_test.go` colocated with code under test; integration tests behind `//go:build integration`.
- **Quality Gates:** `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...` before commit.

### Git Strategy

- Trunk-based: `main` always deployable, short-lived `feature/*` branches.
- Small, atomic [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description` (`feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`).
- Squash and merge to `main`; CI (`Go CI`: fmt, vet, lint, unit tests) must pass before merge.

### Implementation Philosophy

- Black-box interfaces: `internal/registry` owns config schema/defaults/validation; `internal/verify` owns the routing decision and consumes only the resolved effective-value methods, never re-deriving defaults itself.
- Replaceable components: `withinComplexityCeiling` is a pure predicate that can be swapped or extended (e.g. a future weighted score) without changing `generateFixes`'s skip-chain control flow.
- Primitive-first: route on the existing `Finding.EstMinutes`/`JSONFinding.EstMinutes` signal and reuse `FixWarning`/`logPipelineWarning` unchanged — no new primitives, no parallel `complexity_score` concept, no new logging sink.

---

## External Resources

`/find-documentation` found no specifications directly matching this plan's scope (threshold 0.7); `plan/documentation/source.md` lists zero sources. No external resources to review beyond the codebase primitives already cited in this document (`internal/registry/config.go`, `internal/verify/executor.go`, `internal/verify/severity.go`, `internal/reconcile/emit.go`).

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Config Surface & Validation

**Items:** Story 1 (Configure a Complexity Ceiling), Story 4 (Validate Ceiling Configuration)
**Focus:** Add `MaxEstimatedMinutes *int` and `MaxSeverityForFix string` to `ExecutorConfig` (`internal/registry/config.go:206-225`) with their `EffectiveMaxEstimatedMinutes()`/`EffectiveMaxSeverityForFix()` resolvers, then harden `validateExecutor` (`internal/registry/config.go:593-677`) with a new `MaxExecutorEstimatedMinutes` named-constant range check, a `max_severity_for_fix` normalization check, and the floor/ceiling cross-field contradiction check. Config-only — nothing consumes these fields yet.

### 1.1 [x] **[Configure a Complexity Ceiling - RED](plan/user-stories/01-configure-complexity-ceiling.md)**
   1. Analyze [AC 01-01](plan/acceptance-criteria/01-01-executorconfig-exposes-complexity-ceiling-fields.md) and [AC 01-02](plan/acceptance-criteria/01-02-effective-value-resolvers-return-correct-defaults.md), identify testable units
   2. Write tests in `internal/registry/executor_config_test.go` asserting: `MaxEstimatedMinutes`/`MaxSeverityForFix` fields exist and round-trip through YAML; `EffectiveMaxEstimatedMinutes()`/`EffectiveMaxSeverityForFix()` return "no ceiling" defaults when unset and the configured value when set
   3. Verify tests fail correctly (fields/resolvers do not exist yet)
   **Files:** `internal/registry/executor_config_test.go` | **Duration:** 0.4 day

### 1.2 [x] **[Configure a Complexity Ceiling - GREEN](plan/user-stories/01-configure-complexity-ceiling.md)**
   GREEN: Add `MaxEstimatedMinutes *int` (yaml: `max_estimated_minutes,omitempty`) and `MaxSeverityForFix string` (yaml: `max_severity_for_fix,omitempty`) to `ExecutorConfig`, mirroring the `TimeoutSecs`/`MaxToolCalls` pointer convention; add `EffectiveMaxEstimatedMinutes()`/`EffectiveMaxSeverityForFix()` resolver methods matching `EffectiveFixMinSeverity`/`EffectiveMaxToolCalls` naming and doc-comment style (T1), verify all pass (T2), COMMIT
   **Files:** `internal/registry/config.go` | **Duration:** 0.4 day

### 1.2.A [x] **[Configure a Complexity Ceiling - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-configure-complexity-ceiling.md)**
   **Changed Files:** `internal/registry/config.go`, `internal/registry/executor_config_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.1-1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.1-1.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings:** None. **Adversarial review passed.**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | None | — | — | — |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.3 [x] **[Configure a Complexity Ceiling - REFACTOR](plan/user-stories/01-configure-complexity-ceiling.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.2 day

### 1.4 [x] **[Validate Ceiling Configuration - RED](plan/user-stories/04-validate-ceiling-configuration.md)**
   1. Analyze [AC 04-01](plan/acceptance-criteria/04-01-numeric-and-severity-ceiling-values-are-range-validated.md) and [AC 04-02](plan/acceptance-criteria/04-02-floor-ceiling-contradiction-is-rejected-at-load-time.md), identify testable units
   2. Write tests in `internal/registry/executor_config_test.go`: `TestExecutor_MaxEstimatedMinutesOutOfRangeRejected`, `TestExecutor_MaxSeverityForFixInvalidRejected`, `TestExecutor_MaxSeverityForFixBelowMinSeverityRejected`, plus a positive-path valid-combination test
   3. Verify tests fail correctly
   **Files:** `internal/registry/executor_config_test.go` | **Duration:** 0.4 day

### 1.5 [x] **[Validate Ceiling Configuration - GREEN](plan/user-stories/04-validate-ceiling-configuration.md)**
   GREEN: Add `MaxExecutorEstimatedMinutes` named constant alongside `MaxExecutorToolCalls`/`MaxExecutorRules`; add the `max_estimated_minutes` range check (mirroring the `TimeoutSecs` shape) and the `max_severity_for_fix` normalization check (mirroring the `MinSeverity` check) to `validateExecutor`, accumulating into `errs`; add the floor/ceiling cross-field contradiction check using a local severity rank comparison (T1), verify all pass (T2), COMMIT
   **Files:** `internal/registry/config.go` | **Duration:** 0.4 day

### 1.5.A [x] **[Validate Ceiling Configuration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-validate-ceiling-configuration.md)**
   **Changed Files:** `internal/registry/config.go`, `internal/registry/executor_config_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.4-1.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.4-1.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings** (2 LOW — both resolved in 1.6, not deferred, as they harden Story 4's own contradiction check):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | config.go (cross-field check) | Whitespace-only `min_severity_for_fix` ("   ") leaks past the contradiction check: `EffectiveFixMinSeverity()` guards on literal `== ""`, so "   " returns verbatim → normalizes to "" → cross-field skipped, yet `applyDefaults` collapses it to the MEDIUM default → a dead `min:"   " + max:LOW` config loads clean. | Compute floorNorm = NormalizeSeverity(MinSeverity); if empty, use DefaultFixMinSeverity — matches runtime effective floor without touching shared EffectiveFixMinSeverity. |
   | LOW | executor_config_test.go | Missing coverage: (a) invalid-min + valid-max accumulates only the per-field error (no false "below"); (b) unset-min + `max:LOW` is rejected against the defaulted MEDIUM floor. | Add both cases to the ceiling-validation tests. |

   **Action Required:** No CRITICAL/HIGH. The two LOW findings are fixed in 1.6 (REFACTOR) rather than deferred — both directly harden this story's contradiction check and are ~3 lines + tests.

### 1.6 [x] **[Validate Ceiling Configuration - REFACTOR](plan/user-stories/04-validate-ceiling-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.2 day

### 1.7 [x] **Phase 1 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 1. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 1.8 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1: `internal/registry/config.go`, `internal/registry/executor_config_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings** (1 MEDIUM — scheduled scope, not deferred as debt):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | docs/registry.md | The two new executor keys are undocumented in the field table/example. | Already the explicit deliverable of Phase 4 / Story 5 / Task 4.2 — sprint sequences all docs to Phase 4. No tech-debt entry created (not debt; scheduled). |

   **Phase gate passed** — no CRITICAL/HIGH; the lone MEDIUM is Phase 4's planned work. Contract-exit, config back-compat, and phase-exit consumability (Phase 2 `generateFixes` can call both `EffectiveXxx` resolvers) all confirmed by the gate.
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Core Routing — Skip Chain & Self-Gating

**Items:** Story 2 (Skip Over-Ceiling Findings Safely)
**Focus:** Add a `withinComplexityCeiling` predicate in `internal/verify/severity.go`; wire it into `generateFixes`'s existing pre-dispatch skip chain (`internal/verify/executor.go:104-232`) as a fourth condition, alongside confidence/severity/attribution; add the `executor_ceiling_skip` `logPipelineWarning` class and `FixWarning` message; add the self-gating decline branch so an executor that judges a dispatched fix too complex declines through the identical skip-and-log contract rather than returning a partial fix.

### 2.1 [x] **[Skip Over-Ceiling Findings Before Dispatch - RED](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   1. Analyze [AC 02-01](plan/acceptance-criteria/02-01-ceiling-exceeding-findings-are-skipped-before-dispatch.md) and [AC 02-03](plan/acceptance-criteria/02-03-existing-skip-chain-and-failure-branches-remain-unaffected.md), identify testable units
   2. Write tests: `internal/verify/severity_test.go` — `withinComplexityCeiling` predicate in isolation (at/below ceiling passes, above ceiling fails, zero/unset `EstMinutes` treated as "no estimate provided," unset ceiling means unlimited); `internal/verify/executor_test.go` — `TestGenerateFixes_SkipsAboveComplexityCeiling` plus a regression case proving the existing confidence/severity/attribution skip chain and failure branches (`executor_fix_failed`, `executor_truncated_fix`, `executor_empty_fix`) are unaffected
   3. Verify tests fail correctly
   **Files:** `internal/verify/severity_test.go`, `internal/verify/executor_test.go` | **Duration:** 0.5 day

### 2.2 [x] **[Skip Over-Ceiling Findings Before Dispatch - GREEN](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   GREEN: Add `withinComplexityCeiling(estMinutes, maxMinutes int) bool` to `internal/verify/severity.go` alongside `meetsSeverityFloor`; insert it as a fourth pre-dispatch skip-chain condition in `generateFixes`, calling `logPipelineWarning(log.FromContext(ctx), "executor_ceiling_skip", "<file>:<line>: ...")` and setting `f.FixWarning` to an explicit reason before `continue` — unlike the existing silent pre-dispatch skips (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/severity.go`, `internal/verify/executor.go` | **Duration:** 0.6 day

### 2.2.A [x] **[Skip Over-Ceiling Findings - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   **Changed Files:** `internal/verify/severity.go`, `internal/verify/executor.go`, `internal/verify/severity_test.go`, `internal/verify/executor_ceiling_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.1-2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.1-2.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings** (2 LOW — no CRITICAL/HIGH/MEDIUM):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | executor.go:67-75 (`anyFixEligible`) | Harness-build gate consults only confidence + severity-floor, not the new ceilings; a single-tier config where every floor-eligible finding is over-ceiling builds the full snapshot/client then skips everything — the ceiling's cost-avoidance is bypassed one layer up. Benign in the intended multi-tier flow (over-ceiling findings are meant for a later tier). | Captured as TD-001 (out of Story 2 scope — Story 2 owns the skip mechanics, not harness-build gating). |
   | LOW | executor.go (severity-ceiling reason) | Reason string interpolated the raw un-normalized `f.Severity` (e.g. lowercase `critical`) against the canonical `maxSev`. Cosmetic only; comparison is correct/case-insensitive. | Fixed in 2.3 REFACTOR — normalize the displayed severity. |

   **Action Required:** No CRITICAL/HIGH. The casing LOW is fixed in 2.3 (directly hardens this story's output text); the `anyFixEligible` efficiency LOW is captured as TD-001 (broader, out-of-scope for Story 2).

### 2.3 [x] **[Skip Over-Ceiling Findings Before Dispatch - REFACTOR](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 2.4 [x] **[Self-Gating Decline Never Presents a Partial Fix - RED](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   1. Analyze [AC 02-02](plan/acceptance-criteria/02-02-self-gating-decline-never-presents-a-partial-fix-as-complete.md), identify testable units
   2. Write `TestGenerateFixes_SelfGatingDeclineNotPartialFix` in `internal/verify/executor_test.go` asserting a self-declined fix lands as a skip (non-empty `FixWarning`, no `Fix` set, `logPipelineWarning("executor_ceiling_skip", ...)` distinct from `executor_fix_failed`) and never as partial `Fix` content
   3. Verify tests fail correctly
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.4 day

### 2.5 [x] **[Self-Gating Decline Never Presents a Partial Fix - GREEN](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   GREEN: Add the self-gating decline branch inside `generateFixes`'s per-finding goroutine, parallel to the existing `warn`/`truncated`/empty-string handling — returning before any `f.Fix` assignment (matching the file's documented early-return invariant) and surfacing the decline via the same `FixWarning` + `logPipelineWarning` contract as a pre-dispatch ceiling skip (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/executor.go` | **Duration:** 0.5 day

### 2.5.A [ ] **[Self-Gating Decline - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.4-2.5]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.4-2.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.4-2.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
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
   - CRITICAL/HIGH found -> List issues for 2.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.6 [ ] **[Self-Gating Decline Never Presents a Partial Fix - REFACTOR](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 2.7 [ ] **Phase 2 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 2. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 2.8 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Two-Tier Integration & Verification

**Items:** Story 3 (Run a Second Tier Over Skipped Findings)
**Focus:** Integration/E2E test running `generateFixes` twice against the same fixture finding set with two different `ExecutorConfig`s (low-ceiling then high/no-ceiling), asserting every finding is fixed by exactly one tier or explicitly skipped-and-logged — never both, never neither. Dedicated assertion on fix-attribution state to prove tier 2 never re-attempts a tier-1-fixed finding. Interpretation locked per sprint-design.md: a single `ExecutorConfig` gains a ceiling; "tier 2" is a second, independently-configured run against the same `findings.json` — no `Registry.Executor` schema redesign in this plan.

### 3.1 [ ] **[Two-Tier Partition & Attribution - RED](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Analyze [AC 03-01](plan/acceptance-criteria/03-01-two-tier-run-partitions-every-finding-exactly-once.md) and [AC 03-02](plan/acceptance-criteria/03-02-fix-attribution-prevents-double-processing-across-tiers.md), identify testable units
   2. Write `TestGenerateFixes_TwoTierPartitionsFindingsExactlyOnce` in `internal/verify/executor_test.go`: a fixture finding set with a deliberate mix of below-ceiling / tier-1-only / tier-2-only / above-both-ceilings `EstMinutes` values; assert each finding is fixed by exactly one tier or explicitly skipped-and-logged, and that fix attribution prevents tier 2 from re-attempting a tier-1-fixed finding
   3. Verify tests fail correctly
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.6 day

### 3.2 [ ] **[Two-Tier Partition & Attribution - GREEN](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   GREEN: Wire the two-tier test harness — invoke `generateFixes` twice in sequence against the same `[]Finding`, once with a low-ceiling `ExecutorConfig`, once with a high/no-ceiling one — fixing any gap that surfaces in the existing skip-chain/attribution logic so the partition invariant holds (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/executor.go`, `internal/verify/executor_test.go` | **Duration:** 0.6 day

### 3.2.A [ ] **[Two-Tier Partition & Attribution - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.1-3.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.1-3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.1-3.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
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
   - CRITICAL/HIGH found -> List issues for 3.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.3 [ ] **[Two-Tier Partition & Attribution - REFACTOR](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 3.4 [ ] **[Two-Tier Workflow E2E Reproducibility - RED](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Analyze [AC 03-03](plan/acceptance-criteria/03-03-two-tier-workflow-is-test-verified-and-reproducible.md), identify testable units
   2. Write `TestGenerateFixes_TwoTierWorkflowReproducible` as a full E2E reproduction against a fixture `findings.json` with the same below/above-ceiling mix, run through the exact two-tier sequence an operator would run, proving the workflow is automated and reproducible (not manual-only)
   3. Verify tests fail correctly
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.5 day

### 3.5 [ ] **[Two-Tier Workflow E2E Reproducibility - GREEN](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   GREEN: Finalize the fixture `findings.json` and harness so the E2E test passes deterministically end-to-end (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.4 day

### 3.5.A [ ] **[Two-Tier Workflow E2E - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.4-3.5]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.4-3.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.4-3.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
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
   - CRITICAL/HIGH found -> List issues for 3.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.6 [ ] **[Two-Tier Workflow E2E Reproducibility - REFACTOR](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 3.7 [ ] **Phase 3 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 3. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 3.8 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Documentation & Validation

**Items:** Story 5 (Document the Multi-Tier Workflow)
**Focus:** `docs/registry.md` executor field table gains `max_estimated_minutes`/`max_severity_for_fix` rows immediately after `min_severity_for_fix`; `docs/findings-format.md`'s `EST_MINUTES` description cross-references the new routing consumer; `examples/registry-with-executor.yaml` gains a worked cheap-tier + frontier-tier example, validated by loading it through atcr's registry loader (dry-run, zero load errors). Sprint-wide Definition of Done validation.

### 4.1 [ ] **[Document the Multi-Tier Workflow - RED](plan/user-stories/05-document-multi-tier-workflow.md)**
   1. Analyze [AC 05-01](plan/acceptance-criteria/05-01-ceiling-fields-documented-in-registry-and-findings-format-docs.md) and [AC 05-02](plan/acceptance-criteria/05-02-worked-two-tier-example-is-valid-and-runnable.md), identify testable units
   2. Extend `internal/registry/examples_test.go`'s `TestExampleRegistriesLoad`/`TestRegistryExamples_Valid` coverage to assert the extended `examples/registry-with-executor.yaml` (with its new two-tier block) loads and validates with zero errors
   3. Verify the new/extended test fails correctly (worked example not yet added)
   **Files:** `internal/registry/examples_test.go` | **Duration:** 0.4 day

### 4.2 [ ] **[Document the Multi-Tier Workflow - GREEN](plan/user-stories/05-document-multi-tier-workflow.md)**
   GREEN: Add the `max_estimated_minutes`/`max_severity_for_fix` rows and explanatory prose to `docs/registry.md`'s executor field table (matching the existing `min_severity_for_fix`/`fix_timeout` phrasing convention); add a one-sentence cross-reference in `docs/findings-format.md`'s `EST_MINUTES` description; extend `examples/registry-with-executor.yaml` with a worked cheap-tier + frontier-tier two-tier example (T1), verify all pass (T2), COMMIT
   **Files:** `docs/registry.md`, `docs/findings-format.md`, `examples/registry-with-executor.yaml` | **Duration:** 0.7 day

### 4.2.A [ ] **[Document the Multi-Tier Workflow - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-document-multi-tier-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.1-4.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.1-4.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.1-4.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
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
   - CRITICAL/HIGH found -> List issues for 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.3 [ ] **[Document the Multi-Tier Workflow - REFACTOR](plan/user-stories/05-document-multi-tier-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve docs and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 4.4 [ ] **Phase 4 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 4. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 4.5 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%): `go test -coverprofile=coverage.out ./...`
- [ ] Lint/format clean: `golangci-lint run`, `go fmt ./...`
- [ ] Build succeeds: `go build ./...`

### Optional: Targeted Mutation Testing
No mutation testing tool detected on this machine (`stryker-mutator`, `mutmut`, `cargo-mutants` all unavailable) — skip this step. If a tool becomes available before this sprint executes, target only `withinComplexityCeiling` and the `generateFixes` skip-chain/self-gating branches (highest-risk routing logic), not the full codebase.

### Drift Analysis
Compare final implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] Reviewer agents output an estimated complexity/time metric for every finding (original epic AC1) — already satisfied pre-sprint by `EstMinutes`; confirm no regression.
- [ ] `atcr.yaml` configuration supports defining complexity ceilings for the executor (original epic AC2) — `ExecutorConfig.MaxEstimatedMinutes`/`MaxSeverityForFix` (Phase 1).
- [ ] The Execution Engine successfully skips findings that exceed the configured complexity boundaries (original epic AC3) — `generateFixes` ceiling skip chain (Phase 2).
- [ ] A multi-tier workflow can be successfully run: a cheap model knocks out LOW complexity bugs, and a second run tackles the remaining HIGH complexity bugs (original epic AC4) — two-tier integration/E2E test + worked example (Phases 3-4).
- [ ] No `Registry.Executor` schema redesign (single-executor-plus-ceiling interpretation) — confirm `Registry.Executor` remains a single `*ExecutorConfig` pointer, per the Design Decision locked in sprint-design.md.
