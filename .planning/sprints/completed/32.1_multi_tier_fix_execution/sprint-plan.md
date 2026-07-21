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

### 2.5.A [x] **[Self-Gating Decline - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   **Changed Files:** `internal/verify/executor.go`, `internal/verify/executor_ceiling_test.go`

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

   **Subagent findings** (3 LOW — no CRITICAL/HIGH/MEDIUM):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | executor.go (buildFixPrompt) | Prompt rendered the sentinel via `%q`, telling the model to output it WITH surrounding quotes, but `parseSelfDecline` stripped no quotes → a model echoing the quoted form would slip past detection and land the sentinel as `f.Fix`. | Fixed in 2.6: render the marker unquoted in both prompts + defensively strip a surrounding quote pair in `parseSelfDecline`. |
   | LOW | executor.go (parseSelfDecline) | Only `ATCR_DECLINE` exactly or `ATCR_DECLINE:` prefix detected; `ATCR_DECLINE <reason>` (space/newline separator) — plausible given the "followed by a reason" wording — was not detected, leaking the sentinel into `f.Fix`. | Fixed in 2.6: accept a whitespace/colon token boundary after the bare marker while still requiring it as the whole leading token (so `ATCR_DECLINED` stays a non-decline). |
   | LOW | executor.go (buildFixPrompt reviewer interpolation) | A malicious reviewer could prompt-inject via `Problem`/`Fix`/`Evidence` to coax the executor into declining, suppressing a fix for their own finding. Parser is NOT spoofable (leading-token only) and the skip is fully visible via `FixWarning`+`executor_ceiling_skip` — inherent to any LLM self-gating signal, bounded and auditable. | Accepted design property (no code change). The visible-skip audit trail is the mitigation; noted, not deferred. |

   **Action Required:** No CRITICAL/HIGH. The two robustness LOWs (#1, #2) are fixed in 2.6 (they harden this story's own decline detector). The injection-suppression LOW (#3) is an accepted, auditable design property of LLM self-gating — documented, no code change, not tech-debt.

### 2.6 [x] **[Self-Gating Decline Never Presents a Partial Fix - REFACTOR](plan/user-stories/02-skip-over-ceiling-findings-safely.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 2.7 [x] **Phase 2 - DoD Verification**
   **Result:** T3 full suite passing; `internal/verify` coverage 94.9% (≥80%); `golangci-lint` 0 issues; `go build ./...` OK. Docs deferred to Phase 4 (scheduled). AC 02-01/02-02/02-03 Auto-Verified + Story-Specific checked; Manual Review left for `/execute-code-review`.
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 2. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 2.8 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2: `internal/verify/severity.go`, `internal/verify/executor.go`, `internal/verify/severity_test.go`, `internal/verify/executor_ceiling_test.go`

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

   **First-pass gate findings** (1 HIGH, 2 LOW — hostile integrator, no AC context):
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | executor.go (`hasFixAttribution(f.Evidence, ex.Name)`) | Attribution guard is name-scoped: with two DISTINCT tier `Name`s, tier 2 does not skip a tier-1-fixed finding → would break the "exactly one tier" partition invariant. | NOT a Phase 2 defect. **AC 03-02 anticipates this exactly**: Scenario 1 achieves the partition via a SHARED executor `Name` (default `"executor"`); Edge Case 1 states the distinct-`Name` gap must be surfaced by explicit Phase 3 test assertion + documented in Phase 4 `docs/registry.md`. The gate's suggested `f.Fix != ""` guard would be WRONG — findings arrive with a reviewer-suggested `Fix` the executor is meant to REFINE (`buildFixPrompt`), so skipping on `f.Fix != ""` breaks the executor's core purpose and existing tests. Resolution = documented precondition (below), no code change. |
   | LOW | executor.go (ceiling-skip branch) | A ceiling-skip sets `FixWarning` without checking `f.Fix != ""`; if a lower-ceiling tier ran AFTER a higher one, a finding could carry both a tier-1 `Fix` and a skip warning. | Not triggered by the workflow's tier1-low → tier2-high ordering (tier 2 high/no-ceiling never ceiling-skips). Documented as a Phase 3 precondition (ceilings non-decreasing across tier order). |
   | LOW | executor.go (confidence/severity-floor silent skips) | A below-floor/below-confidence finding is skipped silently by both tiers (neither fixed nor logged) → fails a strict "never neither" assertion. | Partition invariant is over the ELIGIBLE subset (HIGH+ confidence, at/above floor) by design. Documented as a Phase 3 precondition (scope the partition assertion to eligible findings). |

   **Phase 3 preconditions (carry into Story 3 execution):**
   1. **Shared `Name` for the partition invariant** — both tier configs use the same executor `Name` (default `"executor"`) so `hasFixAttribution` prevents tier 2 re-attempting a tier-1-fixed finding (AC 03-02 Scenario 1). The distinct-`Name` gap is characterized by an explicit test assertion (AC 03-02 Edge Case 1) and documented in Phase 4 docs — not "fixed" in code.
   2. **Ceilings non-decreasing across tier order** — tier 1 low-ceiling, tier 2 high/no-ceiling, so tier 2 never ceiling-skips and never overwrites a tier-1 `Fix`.
   3. **Partition scoped to eligible findings** — the "exactly one tier / never neither" assertion covers only HIGH+ confidence, at/above-floor findings; below-eligibility findings are an intentional pre-partition filter.

   **Re-gate (with AC 03-02 design context):** a second gate subagent, given the intended same-`Name` partition design and the three preconditions above, confirmed the phase-exit contract is consumable by Phase 3 without Phase 2 rework — **no CRITICAL/HIGH**.

   **Phase gate passed** — the sole HIGH was a design-intended, AC-documented characteristic (resolved via precondition, not code); the two LOWs are documented Phase 3 test-scoping preconditions. Contract-exit, config back-compat (ceilings default to no-op), and Phase 3 consumability all confirmed.
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Two-Tier Integration & Verification

**Items:** Story 3 (Run a Second Tier Over Skipped Findings)
**Focus:** Integration/E2E test running `generateFixes` twice against the same fixture finding set with two different `ExecutorConfig`s (low-ceiling then high/no-ceiling), asserting every finding is fixed by exactly one tier or explicitly skipped-and-logged — never both, never neither. Dedicated assertion on fix-attribution state to prove tier 2 never re-attempts a tier-1-fixed finding. Interpretation locked per sprint-design.md: a single `ExecutorConfig` gains a ceiling; "tier 2" is a second, independently-configured run against the same `findings.json` — no `Registry.Executor` schema redesign in this plan.

### 3.1 [x] **[Two-Tier Partition & Attribution - RED](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Analyze [AC 03-01](plan/acceptance-criteria/03-01-two-tier-run-partitions-every-finding-exactly-once.md) and [AC 03-02](plan/acceptance-criteria/03-02-fix-attribution-prevents-double-processing-across-tiers.md), identify testable units
   2. Write `TestGenerateFixes_TwoTierPartitionsFindingsExactlyOnce` in `internal/verify/executor_test.go`: a fixture finding set with a deliberate mix of below-ceiling / tier-1-only / tier-2-only / above-both-ceilings `EstMinutes` values; assert each finding is fixed by exactly one tier or explicitly skipped-and-logged, and that fix attribution prevents tier 2 from re-attempting a tier-1-fixed finding
   3. Verify tests fail correctly
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.6 day

### 3.2 [x] **[Two-Tier Partition & Attribution - GREEN](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   GREEN: Wire the two-tier test harness — invoke `generateFixes` twice in sequence against the same `[]Finding`, once with a low-ceiling `ExecutorConfig`, once with a high/no-ceiling one — fixing any gap that surfaces in the existing skip-chain/attribution logic so the partition invariant holds (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/executor.go`, `internal/verify/executor_test.go` | **Duration:** 0.6 day

### 3.2.A [x] **[Two-Tier Partition & Attribution - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   **Changed Files:** `internal/verify/executor_test.go`

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

   **Subagent findings** (1 HIGH, 1 MEDIUM, 1 LOW — all fixed in 3.3, none deferred; each directly hardens Story 3's own partition proof, matching the Phase 1/2 precedent):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | executor_test.go (recordingExecutor.Complete) | The partition test dispatches 4 eligible tier-1 findings through the bounded worker pool (default MaxParallel=4), so `recordingExecutor.Complete` mutates `calls`/`prompts`/`temps` from up to 4 goroutines with no mutex — a real data race; `assert.Equal(4, rec1.calls)` can under-count and `go test -race` fails deterministically. | Fixed in 3.3: added a `sync.Mutex` guarding recordingExecutor's recorded fields (makes the shared helper race-safe for every 2+-eligible-finding test). Verified `go test -race ./internal/verify/` clean. |
   | MEDIUM | executor_test.go (partition loop) | The `assert.NotEqual(fixed, skipLogged)` XOR check only catches the "neither" (silent-drop) case; a finding carrying BOTH a Fix and a stale FixWarning yields fixed=true/skipLogged=false and passes — the exact stale-warning hazard generateFixes:315 guards. | Fixed in 3.3: split into three explicit impossibilities — never-both (Fix + FixWarning), never-neither (empty + empty), and fixed-XOR-skipLogged. |
   | LOW | executor_test.go (two-tier suite) | Only the estimated-minutes ceiling axis is exercised; the severity-ceiling axis (`withinSeverityCeiling`/`MaxSeverityForFix`) is never driven across two tiers — a severity-axis regression would leave Story 3 green. | Fixed in 3.3: added `TestGenerateFixes_TwoTierSeverityCeilingPartition` (tier-1 HIGH ceiling skip-logs a CRITICAL finding, tier 2 fixes it; HIGH finding fixed by tier 1, not re-touched by tier 2). |

   **Action Required:** The HIGH data race is fixed inline (required). The MEDIUM and LOW are also fixed inline (not deferred) because both directly harden Story 3's partition deliverable — leaving them would ship a partition test that does not fully prove the partition. `-race`, `go vet`, `golangci-lint` (0 issues) all clean post-fix.

### 3.3 [x] **[Two-Tier Partition & Attribution - REFACTOR](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 3.4 [x] **[Two-Tier Workflow E2E Reproducibility - RED](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Analyze [AC 03-03](plan/acceptance-criteria/03-03-two-tier-workflow-is-test-verified-and-reproducible.md), identify testable units
   2. Write `TestGenerateFixes_TwoTierWorkflowReproducible` as a full E2E reproduction against a fixture `findings.json` with the same below/above-ceiling mix, run through the exact two-tier sequence an operator would run, proving the workflow is automated and reproducible (not manual-only)
   3. Verify tests fail correctly
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.5 day

### 3.5 [x] **[Two-Tier Workflow E2E Reproducibility - GREEN](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   GREEN: Finalize the fixture `findings.json` and harness so the E2E test passes deterministically end-to-end (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/executor_test.go` | **Duration:** 0.4 day

### 3.5.A [x] **[Two-Tier Workflow E2E - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   **Changed Files:** `internal/verify/executor_test.go`

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

   **Subagent findings** (1 MEDIUM, 2 LOW — no CRITICAL/HIGH; all fixed in 3.6, none deferred, each hardens Story 3's E2E deliverable). The subagent independently confirmed the determinism check is not flaky (order-preserving slice, no timestamps/temp-paths/random sentinels/serialized maps in output) and the partition assertions are not tautological:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | executor_test.go (determinism check) | With the 3-finding fixture each tier dispatched exactly ONE eligible finding, so `generateFixes`'s worker pool never had 2+ concurrent writers — a regression making fix generation order-dependent would still pass the "byte-identical" determinism claim. | Fixed in 3.6: seeded a second within-ceiling finding for EACH tier (cheap+cheap2 for tier 1; mid+mid2 for tier 2) so each tier dispatches 2 fixes concurrently. Determinism check now guards ordering; verified `-race -count=3` clean. |
   | LOW | executor_test.go (`unmarshalFindings`) | The final on-disk assertion used a bespoke `unmarshalFindings` instead of the production `reconcile.ReadReconciledFindings`, so the final read did not exercise the real reader path it claims to prove. | Fixed in 3.6: final read now rides `reconcile.ReadReconciledFindings(dir)`; the bespoke helper was deleted. |
   | LOW | executor_test.go (silent-drop invariant) | The "never silently dropped" invariant is only valid because every fixture finding is fix-eligible, but that precondition was assumed, not asserted — a future fixture edit lowering a finding below the gate would misfire as a false failure. | Fixed in 3.6: added an explicit eligibility precondition loop (confidence + `meetsSeverityFloor`) asserting every seed finding clears the fix gate, with a comment scoping the invariant. |

   **Action Required:** No CRITICAL/HIGH. All three findings fixed inline in 3.6 (not deferred) — each directly strengthens the E2E deliverable (AC 03-03). `-race -count=3`, `go vet`, `golangci-lint` (0 issues) all clean post-fix.

### 3.6 [x] **[Two-Tier Workflow E2E Reproducibility - REFACTOR](plan/user-stories/03-run-second-tier-over-skipped-findings.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 3.7 [x] **Phase 3 - DoD Verification**
   **Result:** T3 full suite passing; `internal/verify` coverage 95.2% (≥80%); `golangci-lint run` 0 issues; `go build ./...` OK; `go test -race ./internal/verify/` clean. Phase 3 is test-only (`internal/verify/executor_test.go`) — docs (Story 5, AC 03-02/03-03 doc items) are scheduled Phase 4, not a Phase 3 gap. AC 03-01/03-02/03-03 Auto-Verified + test-based Story-Specific items checked; docs items + Manual Review left for Phase 4 / `/execute-code-review`.
   ```
   Story-3 DoD Complete
   Auto: 3/3 | Story-Specific: 8/12 (4 docs items deferred to Phase 4)
   Manual Review: [ ] Code reviewed
   ```
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 3. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 3.8 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3: `internal/verify/executor_test.go` (test-only phase)

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

   **Gate findings** (2 LOW — no CRITICAL/HIGH). The hostile-integrator gate independently CONFIRMED the phase-exit contract: attribution survives a real second `atcr verify` pass (pipeline.go:140 reads the prior findings.json; no verify stage rewrites `Evidence`), the `recordingExecutor` mutex added in Phase 3 altered no existing test's meaning (full suite green under `-race`), and config back-compat (ceilings default no-op) holds.
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | executor_test.go (determinism comment) | `TestGenerateFixes_TwoTierWorkflowReproducible`'s comment overclaimed the byte-identical re-run "guards against order-dependent fix generation" — but with a constant stub `out` and per-index writes the artifact is order-independent by construction, so the comparison cannot detect dispatch-order effects. Genuinely deterministic behavior; a comment mischaracterization, not a wrong result. | Fixed inline (not deferred — corrects a misleading claim in the deliverable): reworded both comments to state the check proves deterministic findings.json SERIALIZATION (no map-order/timestamp/sentinel instability), while concurrent-dispatch safety is asserted separately by `-race`. |
   | LOW | executor_test.go (coverage) | No test drives the actual operator command sequence (two `runVerify` passes over one review dir); the two-tier partition is proven at the `generateFixes` level (exactly Story 3's stated scope). The command-level composition is confirmed-by-inference, not asserted end-to-end. | Captured as **TD-002** (beyond Story 3's `generateFixes`-level scope; composition confirmed by the gate to hold, so a regression-guard `runVerify`-twice test is an additive hardening for a follow-up pass). |

   **Phase gate passed** — no CRITICAL/HIGH; the determinism-comment LOW is fixed inline, the command-level-coverage LOW is captured as TD-002. Contract-exit, cross-run attribution survival, config back-compat, and Phase 4 consumability (the tests accurately characterize the same-Name partition / distinct-Name gap / ceiling boundaries / findings.json handoff that Phase 4's docs worked-example must describe) all confirmed by the gate.
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Documentation & Validation

**Items:** Story 5 (Document the Multi-Tier Workflow)
**Focus:** `docs/registry.md` executor field table gains `max_estimated_minutes`/`max_severity_for_fix` rows immediately after `min_severity_for_fix`; `docs/findings-format.md`'s `EST_MINUTES` description cross-references the new routing consumer; `examples/registry-with-executor.yaml` gains a worked cheap-tier + frontier-tier example, validated by loading it through atcr's registry loader (dry-run, zero load errors). Sprint-wide Definition of Done validation.

### 4.1 [x] **[Document the Multi-Tier Workflow - RED](plan/user-stories/05-document-multi-tier-workflow.md)**
   1. Analyze [AC 05-01](plan/acceptance-criteria/05-01-ceiling-fields-documented-in-registry-and-findings-format-docs.md) and [AC 05-02](plan/acceptance-criteria/05-02-worked-two-tier-example-is-valid-and-runnable.md), identify testable units
   2. Extend `internal/registry/examples_test.go`'s `TestExampleRegistriesLoad`/`TestRegistryExamples_Valid` coverage to assert the extended `examples/registry-with-executor.yaml` (with its new two-tier block) loads and validates with zero errors
   3. Verify the new/extended test fails correctly (worked example not yet added)
   **Files:** `internal/registry/examples_test.go` | **Duration:** 0.4 day

### 4.2 [x] **[Document the Multi-Tier Workflow - GREEN](plan/user-stories/05-document-multi-tier-workflow.md)**
   GREEN: Add the `max_estimated_minutes`/`max_severity_for_fix` rows and explanatory prose to `docs/registry.md`'s executor field table (matching the existing `min_severity_for_fix`/`fix_timeout` phrasing convention); add a one-sentence cross-reference in `docs/findings-format.md`'s `EST_MINUTES` description; extend `examples/registry-with-executor.yaml` with a worked cheap-tier + frontier-tier two-tier example (T1), verify all pass (T2), COMMIT
   **Files:** `docs/registry.md`, `docs/findings-format.md`, `examples/registry-with-executor.yaml` | **Duration:** 0.7 day

### 4.2.A [x] **[Document the Multi-Tier Workflow - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-document-multi-tier-workflow.md)**
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

   **Subagent findings** (1 MEDIUM, 1 LOW — no CRITICAL/HIGH; both fixed in 4.3, not deferred, each directly hardens Story 5's own AC 05-01 accuracy). The subagent independently confirmed both example files load/validate clean, field names/ranges/error phrasing match the shipped `config.go`, both tiers share `name: executor`, the findings-format anchor resolves, and the ceiling boundary semantics match `withinComplexityCeiling`/`withinSeverityCeiling`:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | docs/registry.md (max_estimated_minutes row) | Table row said "Unset (or `0`) means no ceiling," but `validateExecutor` rejects any non-nil pointer `<= 0` (`must be within 1..10080`) — only the in-memory resolver treats 0 as no-ceiling, unreachable from loaded YAML. `max_estimated_minutes: 0` is a copy-paste trap that fails to load. | Fixed in 4.3: dropped "(or `0`)"; row now reads "a non-positive or out-of-range value is a load error. Unset (omit the field) means no ceiling." Matches AC 05-01 Error Scenario 1 (docs must match real load-time behavior). |
   | LOW | examples/*.yaml + docs/registry.md (two-tier prose) | The invariant "every finding is fixed by exactly one tier or explicitly skipped-and-logged, never both, never neither" overstates: a below-floor / below-HIGH-confidence finding is fixed by neither tier and is NOT ceiling-skip-logged — it fails the confidence/`min_severity_for_fix` gate silently. "Never neither" holds only over the fix-eligible subset (the Phase 2 gate precondition #3, carried forward). | Fixed in 4.3: scoped the invariant to fix-eligible findings (HIGH+ confidence, at/above `min_severity_for_fix`) in the two example comments and the docs prose. |

   **Action Required:** No CRITICAL/HIGH. Both the MEDIUM and the LOW are fixed inline in 4.3 (not deferred) — the MEDIUM is a factual docs defect (a load-failing recommendation) that undermines AC 05-01's "docs match shipped validation" requirement, and the LOW aligns the prose with the Phase 2 gate's eligible-subset precondition; both are ~1-line edits to this story's own deliverable, matching the Phase 1-3 fix-inline precedent.

### 4.3 [x] **[Document the Multi-Tier Workflow - REFACTOR](plan/user-stories/05-document-multi-tier-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve docs and tests (T1), validate (T3), COMMIT
   **Duration:** 0.3 day

### 4.4 [x] **Phase 4 - DoD Verification**
   **Result:** T3 full suite passing; `internal/registry` coverage 91.0% (≥80%); `golangci-lint run` 0 issues; `go build ./...` OK; `go vet ./...` clean. Docs phase — `docs/registry.md`, `docs/findings-format.md`, and the two example YAMLs updated and validated by the real registry loader (`TestTwoTierExecutorExamples_Load`). AC 05-01/05-02 Auto-Verified + all 8 Story-Specific items checked; Manual Review left for `/execute-code-review`.
   ```
   Story-5 DoD Complete
   Auto: 3/3 | Story-Specific: 8/8 (AC 05-01: 4/4, AC 05-02: 4/4)
   Manual Review: [ ] Code reviewed
   ```
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 4. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 4.5 [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
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

   **Gate findings** (none — clean gate). The hostile-integrator gate independently CONFIRMED: both example files load/validate clean through the real `LoadRegistry`; `TestTwoTierExecutorExamples_Load` genuinely asserts the ceiling contrast (Positive tier-1 / Zero tier-2 / NotEqual), not a hand-rolled YAML check; converting the with-executor example to the cheap tier broke no existing assertion (`TestExampleRegistriesLoad`, `TestRegistryExamples_Valid` both still pass); documented defaults/ranges/error phrasing match `config.go` exactly (max_estimated_minutes 1..10080, non-positive = load error, nil/0/neg → no ceiling; max_severity_for_fix canonical + not-below-floor); the findings-format anchor `registry.md#executor-fix-generation-active-in-70` resolves to the executor heading; the inclusive-boundary "exceeds" wording matches `withinComplexityCeiling` (`estMinutes <= maxMinutes`); `go build ./...` and `go test ./internal/registry/ ./internal/verify/` all clean.
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | None | — | — | — |

   **Phase gate passed** — no findings. Contract-exit (docs describe the shipped resolver/validation contract), config back-compat (both ceilings default to no-op/unset), no regression (existing example tests green), and downstream consumability (the worked example + docs let an operator assemble the two-tier workflow without reading source) all confirmed.
   **Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [x] All tests passing (T3): `go test ./...` — full suite green
- [x] Coverage meets threshold (≥80%): `go test -coverprofile=coverage.out ./...` — registry 91.0%, verify 95.2%
- [x] Lint/format clean: `golangci-lint run` (0 issues), `go fmt ./...` (gofmt -l clean), `go vet ./...` clean
- [x] Build succeeds: `go build ./...`

### Optional: Targeted Mutation Testing
No mutation testing tool detected on this machine (`stryker-mutator`, `mutmut`, `cargo-mutants` all unavailable) — skip this step. If a tool becomes available before this sprint executes, target only `withinComplexityCeiling` and the `generateFixes` skip-chain/self-gating branches (highest-risk routing logic), not the full codebase.

### Drift Analysis
Compare final implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [x] Reviewer agents output an estimated complexity/time metric for every finding (original epic AC1) — already satisfied pre-sprint by `EstMinutes`; no regression (full suite green; `EST_MINUTES` still emitted/parsed, now also documented as a routing input).
- [x] `atcr.yaml` configuration supports defining complexity ceilings for the executor (original epic AC2) — `ExecutorConfig.MaxEstimatedMinutes`/`MaxSeverityForFix` + validation (Phase 1), documented in `docs/registry.md` (Phase 4).
- [x] The Execution Engine successfully skips findings that exceed the configured complexity boundaries (original epic AC3) — `generateFixes` ceiling skip chain + self-gating decline (Phase 2).
- [x] A multi-tier workflow can be successfully run: a cheap model knocks out LOW complexity bugs, and a second run tackles the remaining HIGH complexity bugs (original epic AC4) — two-tier partition/E2E tests (Phase 3) + runnable worked example `examples/registry-with-executor.yaml` (cheap) + `examples/registry-with-executor-tier2.yaml` (frontier), loader-validated (Phase 4).
- [x] No `Registry.Executor` schema redesign (single-executor-plus-ceiling interpretation) — confirmed `Registry.Executor` remains a single `*ExecutorConfig` pointer (`internal/registry/config.go:509`), per the Design Decision locked in sprint-design.md.
