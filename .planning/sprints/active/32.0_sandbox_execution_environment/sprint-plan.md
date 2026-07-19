# Sprint 32.0: sandbox execution environment

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 32.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Route the `--auto-fix` pipeline's post-apply validation step through the existing `internal/sandbox` container isolation (built for Epic 11.0's `--exec` feature), so LLM-generated `go build`/`npm test` commands never run directly on the host or CI runner. A new resolver (mirroring `internal/verify.ResolveExecBackend`) builds and preflights a `sandbox.Backend` as part of the auto-fix gate, an adapter translates `sandbox.RunResult` into the existing `verify.ValidationResult` contract, and an explicit `--no-sandbox` flag provides a loudly-warned opt-out for environments without Docker.

### Why This Matters

Today, `internal/atomicfs` protects the filesystem from a bad auto-fix, but nothing protects the host machine itself — a hallucinated or prompt-injected `init()` function or pre-build script runs with the same privileges as the `atcr` process. Making `--auto-fix` sandboxed-by-default closes that gap and makes the feature enterprise-grade and secure by default.

### Key Deliverables

- `internal/verify/autofix_exec.go` — a resolver that builds and preflights a `sandbox.Backend` for auto-fix validation, sandboxed-on-by-default.
- A `sandbox.RunResult` → `verify.ValidationResult` adapter with a fully documented translation (combined output → `Stdout` only, Docker runtime faults → `StartError`, `TimedOut` direct-mapped).
- `validateAutoFixBackend` gate integration: sandbox resolution as the fourth checked piece of the existing all-or-nothing usage error.
- A `--no-sandbox` opt-out flag with an unconditional, non-memoized stderr security warning on every invocation.
- `docs/` coverage of the sandboxed-by-default posture, the `auto_fix:` config block, and the `--no-sandbox` risk.

### Success Criteria

- `--auto-fix`'s validation command runs inside `internal/sandbox` by default; with no `sandbox:` config and no `--no-sandbox`, the run fail-closed refuses rather than silently falling back to host execution.
- `--no-sandbox` bypasses the resolver/Preflight gate entirely and prints its warning on every run, never gated behind a "seen once" state.
- Existing `--exec` (Epic 11.0) and `--auto-fix` apply/revert (Epic 17.0) behavior is provably unaffected outside the validation call site — existing auto-fix test suite passes unmodified in outcome.
- `docs/` accurately reflects the final flag name and warning text, reconciled immediately before merge.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Complexity:** 8/12 (COMPLEX) → **Default TDD Mode:** Moderate 🔄 (RED, then GREEN+REFACTOR combined) for all 4 stories — `--tdd-strict`/`--tdd-pragmatic` were not passed, so this was calculated automatically from the complexity score.

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
| T1: Focused | After each small change | `go test ./internal/verify/... -run <Test>` |
| T2: Module | After completing element | `go test ./internal/verify/...` or `go test ./cmd/atcr/...` |
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

- Black-box interfaces: modules communicate only through documented APIs; hide implementation details.
- Replaceable components: this sprint's routing decision (sandboxed vs. direct `os/exec`) stays a `Backend`-presence branch at the call site, never baked into `runAutoFix`'s control flow.
- Primitive-first: reuse existing primitives (`sandbox.RunSpec`/`RunResult`, `verify.ValidationResult`) unmodified — no new primitives introduced.

---

## External Resources

Full index: [plan/documentation/README.md](plan/documentation/README.md)

**[CRITICAL] — read before starting implementation:**
- [Sandbox Backend Interface](plan/documentation/sandbox-backend-interface.md) — `Backend`/`RunSpec`/`RunResult` triad; every `Backend` guarantees no network, read-only snapshot, resource caps, non-root.
- [DockerBackend Implementation](plan/documentation/docker-backend-implementation.md) — `docker run` isolation flags, `Preflight`, `/scratch` writable overlay already satisfying Go's build-cache needs.
- [Resolver Pattern — ResolveExecBackend](plan/documentation/resolver-pattern-resolveexecbackend.md) — the exact resolve-and-preflight shape (`internal/verify/exec.go:24-57`) this sprint's new resolver mirrors, with the gating posture inverted.
- [Auto-Fix Gate & Config Surface](plan/documentation/autofix-gate-and-config.md) — `validateAutoFixBackend`'s all-or-nothing gate, `autoFixBackend` carrier struct, `SandboxConfig`/`AutoFixConfig` tension.
- [Auto-Fix Validation Contract](plan/documentation/autofix-validation-contract.md) — `RunConfiguredValidation`/`ValidationResult` host-path contract and the full translation-gap table.

**[IMPORTANT] — review during development:**
- [Sandbox Testing Patterns](plan/documentation/sandbox-testing-patterns.md) — `fakeDocker` POSIX shell shim and `dockerRunArgs` argv-assertion patterns for hermetic sandbox tests.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Sandbox Resolver & Design Decisions

**Focus:** Add `internal/verify/autofix_exec.go` with a resolver mirroring `ResolveExecBackend`'s resolve-and-preflight shape but inverting the default posture (sandboxed-on-by-default, explicit disable signal). Resolve two open design questions before any other phase builds on this one: (a) `SandboxConfig.Validate()`'s unconditional `Image`+`TestCommand` requirement, (b) timeout precedence — `auto_fix.validate_timeout` must win over `sandbox.timeout_secs` via `RunSpec.Timeout`, never silently shrunk by the backend default.

### 1.1 [ ] **[Resolver Builds and Preflights a Sandbox Backend - RED](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Analyze [AC 02-01](plan/acceptance-criteria/02-01-resolver-builds-and-preflights-sandbox-backend.md), identify testable units
   2. Write tests in `internal/verify/autofix_exec_test.go` (mirroring `exec_test.go`'s `fakeDocker` shim shape): refuses-without-backend, builds-and-preflights, `Preflight()` failure surfaces as an error
   3. Verify tests fail correctly (resolver does not exist yet)
   **Files:** `internal/verify/autofix_exec_test.go` | **Duration:** 0.5 day

### 1.2 [ ] **[Resolver Builds and Preflights a Sandbox Backend - GREEN](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   GREEN: Implement `internal/verify/autofix_exec.go` — translate `*registry.SandboxConfig` into a `sandbox.DockerConfig`, construct `sandbox.NewDockerBackend`, require `Preflight()` before returning a ready `sandbox.Backend` (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/autofix_exec.go` | **Duration:** 0.5 day

### 1.2.A [ ] **[Resolver Builds and Preflights a Sandbox Backend - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.1-1.2]

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

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.3 [ ] **[Resolver Builds and Preflights a Sandbox Backend - REFACTOR](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 1.4 [ ] **[Inverted Default Posture and SandboxConfig.Validate() Tension - RED](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Analyze [AC 02-02](plan/acceptance-criteria/02-02-inverted-default-posture-and-validation-tension.md), identify testable units
   2. Write tests asserting: sandboxed-on-by-default signature (no config → sandboxing expected, not skipped); `SandboxConfig.Validate()`'s unconditional `Image`+`TestCommand` requirement is surfaced explicitly for the auto-fix case, not silently relaxed or silently satisfied
   3. Verify tests fail correctly
   **Files:** `internal/verify/autofix_exec_test.go`, `internal/registry/sandbox_test.go` | **Duration:** 0.5 day

### 1.5 [ ] **[Inverted Default Posture and SandboxConfig.Validate() Tension - GREEN](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   GREEN: Make the explicit design decision on the `Image`+`TestCommand` validation tension (split validation, relaxed path, or parallel block — `--exec`'s existing contract must not be loosened as a side effect); implement the sandboxed-on-by-default resolver signature; ensure `RunSpec.Timeout` is sourced from `auto_fix.validate_timeout`, never silently shrunk by `sandbox.timeout_secs`'s default (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/autofix_exec.go`, `internal/registry/sandbox.go` (if design decision requires it) | **Duration:** 0.5 day

### 1.5.A [ ] **[Inverted Default Posture and SandboxConfig.Validate() Tension - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.4-1.5]

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

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.6 [ ] **[Inverted Default Posture and SandboxConfig.Validate() Tension - REFACTOR](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Fix CRITICAL/HIGH issues from 1.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 1.7 [ ] **Phase 1 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 1. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 1.8 [ ] **Phase 1 - GATE: Integration & Exit Review (subagent)**
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

## Phase 2: Core — Sandbox-Routed Validation Dispatch

**Focus:** Build the `sandbox.RunResult` → `verify.ValidationResult` adapter per the translation-gap table (combined output → `Stdout` only, `TimedOut` direct-mapped without leaking exit code 124, Docker runtime faults → `StartError`, `Duration` measured by the adapter itself, truncation flags left `false`). Wire the validation call site (`cmd/atcr/autofix.go:252`) to dispatch through a supplied `sandbox.Backend` when present, using a fake/stub backend for unit tests (no dependency on Phase 1's real resolver).

### 2.1 [ ] **[Sandbox-Routed Command Dispatch - RED](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   1. Analyze [AC 01-01](plan/acceptance-criteria/01-01-sandbox-routed-command-dispatch.md), identify testable units
   2. Write tests asserting `sandbox.Backend.Run(ctx, RunSpec{Command, Timeout, SnapshotDir})` replaces direct `os/exec` when a backend is supplied, using a stub `sandbox.Backend` (no dependency on Phase 1's real resolver)
   3. Verify tests fail correctly
   **Files:** `internal/verify/localvalidate_test.go` (or new sibling file) | **Duration:** 0.5 day

### 2.2 [ ] **[Sandbox-Routed Command Dispatch - GREEN](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   GREEN: Implement the dispatch branch so validation runs through the supplied `sandbox.Backend` when present, falling back to the existing direct `os/exec` path otherwise (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/localvalidate.go` | **Duration:** 0.5 day

### 2.2.A [ ] **[Sandbox-Routed Command Dispatch - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.1-2.2]

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

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.3 [ ] **[Sandbox-Routed Command Dispatch - REFACTOR](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 2.4 [ ] **[RunResult-to-ValidationResult Translation - RED](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   1. Analyze [AC 01-02](plan/acceptance-criteria/01-02-runresult-to-validationresult-translation.md), identify testable units
   2. Write adapter test cases per the translation-gap table: exit 0 success, non-zero exit (not a Go error), `TimedOut` direct-mapped, Docker runtime faults (exit 125-127, signal death) and spawn failures → `StartError`, combined `Output` → `Stdout` only with `Stderr` left empty
   3. Verify tests fail correctly
   **Files:** `internal/verify/localvalidate_test.go` (or new sibling file) | **Duration:** 0.5 day

### 2.5 [ ] **[RunResult-to-ValidationResult Translation - GREEN](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   GREEN: Implement the `sandbox.RunResult` → `verify.ValidationResult` adapter — `Duration` measured by the adapter itself, truncation flags left `false`, non-zero exit surfaced via `ExitCode`/`Passed()` (not `StartError`) (T1), verify all pass (T2), COMMIT
   **Files:** `internal/verify/localvalidate.go` | **Duration:** 0.5 day

### 2.5.A [ ] **[RunResult-to-ValidationResult Translation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
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

### 2.6 [ ] **[RunResult-to-ValidationResult Translation - REFACTOR](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
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

## Phase 3: Gate Integration & Opt-Out

**Focus:** Wire Phase 1's resolver into `validateAutoFixBackend` as the gate's fourth checked piece (joining the same `missing []string` collection), threading the resolved backend through `autoFixBackend` into `runAutoFix` per Phase 2's dispatch. Register the `--no-sandbox` flag in `addAutoFixFlags` with security-warning help text, short-circuit the resolver call when set, and add the dedicated (non-memoized) `warnNoSandbox` stderr helper called on every `--no-sandbox` code path.

### 3.1 [ ] **[Gate Integration — Sandbox Resolution as the Fourth Piece - RED](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Analyze [AC 02-03](plan/acceptance-criteria/02-03-gate-integration-and-combined-error.md), identify testable units
   2. Write tests asserting sandbox resolution/Preflight failure joins the same combined `missing []string` usage error alongside apply-target/validation-command/GitHub-credential failures; resolved backend rides `autoFixBackend` without re-resolution downstream
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.5 day

### 3.2 [ ] **[Gate Integration — Sandbox Resolution as the Fourth Piece - GREEN](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   GREEN: Call Phase 1's resolver from `validateAutoFixBackend`, join failures into the existing combined error, store the resolved `sandbox.Backend` on `autoFixBackend`, thread it into `runAutoFix` for Phase 2's dispatch to consume (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.5 day

### 3.2.A [ ] **[Gate Integration — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
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

### 3.3 [ ] **[Gate Integration — REFACTOR](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 3.4 [ ] **[--no-sandbox Flag Registration and Help Text - RED](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Analyze [AC 03-01](plan/acceptance-criteria/03-01-flag-registration-and-help-text.md), identify testable units
   2. Write tests asserting the `--no-sandbox` boolean flag exists, defaults to `false`, and its help text contains the required security-warning language
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.25 day

### 3.5 [ ] **[--no-sandbox Flag Registration and Help Text - GREEN](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   GREEN: Register `--no-sandbox` in `addAutoFixFlags` with security-warning help text (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.25 day

### 3.5.A [ ] **[--no-sandbox Flag Registration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
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

### 3.6 [ ] **[--no-sandbox Flag Registration - REFACTOR](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 3.7 [ ] **[--no-sandbox Bypasses Resolver/Preflight Gate - RED](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Analyze [AC 03-02](plan/acceptance-criteria/03-02-bypass-sandbox-resolver-and-preflight-gate.md), identify testable units
   2. Write tests asserting no `Preflight` call and no Docker requirement when `--no-sandbox` is set; flag is a no-op when `--auto-fix` is not also passed
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.25 day

### 3.8 [ ] **[--no-sandbox Bypasses Resolver/Preflight Gate - GREEN](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   GREEN: Short-circuit the resolver call in `validateAutoFixBackend` when `--no-sandbox` is set (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.25 day

### 3.8.A [ ] **[--no-sandbox Bypass - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.7-3.8]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.7-3.8 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.8`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.7-3.8]
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
   - CRITICAL/HIGH found -> List issues for 3.9, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.9 [ ] **[--no-sandbox Bypass - REFACTOR](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Fix CRITICAL/HIGH issues from 3.8.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 3.10 [ ] **[Every-Run stderr Security Warning - RED](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Analyze [AC 03-03](plan/acceptance-criteria/03-03-every-run-stderr-security-warning.md), identify testable units
   2. Write tests asserting the warning prints on every `--no-sandbox` invocation (never gated behind a "seen once" state, unlike the existing `ATCR_TELEMETRY` one-time-warning precedent at `cmd/atcr/main.go:348`)
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.25 day

### 3.11 [ ] **[Every-Run stderr Security Warning - GREEN](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   GREEN: Add a dedicated, non-memoized `warnNoSandbox` stderr helper called on every `--no-sandbox` code path (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.25 day

### 3.11.A [ ] **[stderr Security Warning - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.10-3.11]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.10-3.11 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.11`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.10-3.11]
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
   - CRITICAL/HIGH found -> List issues for 3.12, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.12 [ ] **[stderr Security Warning - REFACTOR](plan/user-stories/03-no-sandbox-opt-out-flag.md)**
   1. Fix CRITICAL/HIGH issues from 3.11.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 3.13 [ ] **Phase 3 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 3. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 3.14 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
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

## Phase 4: Integration Testing & Zero-Behavior-Change Verification

**Focus:** Prove the full `runAutoFix` pipeline is unaffected outside the validation call site — existing auto-fix unit/integration tests pass unmodified in outcome against a fake `sandbox.Backend`; the combined gate error names sandbox failures alongside apply-target/validation-command/GitHub-credential failures in one usage error; `verr != nil` vs `!res.Passed()` branching is provably preserved byte-for-byte regardless of execution path.

### 4.1 [ ] **[Zero Behavior Change to runAutoFix Pipeline - RED](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   1. Analyze [AC 01-03](plan/acceptance-criteria/01-03-zero-behavior-change-to-runautofix-pipeline.md), identify testable units
   2. Write/extend integration tests proving the existing auto-fix test suite passes unmodified in outcome against a fake `sandbox.Backend`; assert `verr != nil` (cannot validate) vs `!res.Passed()` (validation failed) branching is byte-for-byte identical regardless of execution path
   3. Verify tests fail correctly (or pre-existing tests still pass, confirming the baseline before wiring)
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.5 day

### 4.2 [ ] **[Zero Behavior Change to runAutoFix Pipeline - GREEN](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   GREEN: Fix any drift found by 4.1's tests so `runAutoFix`'s apply/revert/branch/commit/PR behavior is unaffected outside the validation call site (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.5 day

### 4.2.A [ ] **[Zero Behavior Change - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
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

### 4.3 [ ] **[Zero Behavior Change - REFACTOR](plan/user-stories/01-route-autofix-validation-through-sandbox.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 4.4 [ ] **[Combined Gate Error — Integration Leg - RED](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Analyze [AC 02-03](plan/acceptance-criteria/02-03-gate-integration-and-combined-error.md) integration leg, identify testable units
   2. Write an integration test asserting the combined `validateAutoFixBackend` usage error correctly names sandbox resolution/Preflight failures alongside apply-target/validation-command/GitHub-credential failures when multiple pieces are missing simultaneously
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** 0.5 day

### 4.5 [ ] **[Combined Gate Error — Integration Leg - GREEN](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   GREEN: Finalize the combined error message joining so all four gate pieces report correctly together (T1), verify all pass (T2), COMMIT
   **Files:** `cmd/atcr/autofix.go` | **Duration:** 0.5 day

### 4.5.A [ ] **[Combined Gate Error — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.4-4.5]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.4-4.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.4-4.5]
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
   - CRITICAL/HIGH found -> List issues for 4.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.6 [ ] **[Combined Gate Error — REFACTOR](plan/user-stories/02-sandbox-resolution-and-preflight-gate.md)**
   1. Fix CRITICAL/HIGH issues from 4.5.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 4.7 [ ] **Phase 4 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) against files changed in Phase 4. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 4.8 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
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

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Documentation & Final Validation

**Focus:** Write/extend `docs/` (either a new `--auto-fix` section in `docs/execution.md` or a new `docs/auto-fix.md` cross-linking it) covering the sandboxed-by-default posture, the `auto_fix:` config block (previously undocumented), and the `--no-sandbox` risk — reconciled against Phases 2-3's final flag name and warning text immediately before merge. Run the existing docs-audit test suite and full Definition of Done validation across all 4 stories.

### 5.1 [ ] **[Sandboxed-by-Default Posture Documented - RED](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   1. Analyze [AC 04-01](plan/acceptance-criteria/04-01-sandboxed-by-default-and-auto-fix-config-documented.md), identify testable units
   2. Write/extend docs-audit test assertions (existing Go test suite, no new framework) expecting the new `--auto-fix` sandboxed-by-default section and the `auto_fix:` config block to be present and cross-linked
   3. Verify tests fail correctly (docs section does not exist yet)
   **Files:** `docs/` docs-audit test file | **Duration:** 0.25 day

### 5.2 [ ] **[Sandboxed-by-Default Posture Documented - GREEN](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   GREEN: Write the new `--auto-fix` section in `docs/execution.md` (or new `docs/auto-fix.md`) covering the sandboxed-by-default posture and the previously-undocumented `auto_fix:` config block, modeled on `docs/execution.md`'s existing `--exec` security-posture section (T1), verify all pass (T2), COMMIT
   **Files:** `docs/execution.md` or `docs/auto-fix.md` | **Duration:** 0.5 day

### 5.2.A [ ] **[Docs — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 5.1-5.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.1-5.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.1-5.2]
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
   - CRITICAL/HIGH found -> List issues for 5.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 5.3 [ ] **[Sandboxed-by-Default Posture Documented - REFACTOR](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
   2. Improve docs and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 5.4 [ ] **[--no-sandbox Risk Documented and Cross-Linked - RED](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   1. Analyze [AC 04-02](plan/acceptance-criteria/04-02-no-sandbox-risk-warning-and-cross-link-accuracy.md), identify testable units
   2. Write/extend docs-audit test assertions expecting the `--no-sandbox` risk section and its cross-links to be present
   3. Verify tests fail correctly
   **Files:** `docs/` docs-audit test file | **Duration:** 0.25 day

### 5.5 [ ] **[--no-sandbox Risk Documented and Cross-Linked - GREEN](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   GREEN: Write the `--no-sandbox` risk documentation, cross-linked from the sandboxed-by-default section; reconcile flag name and warning text against the final merged CLI help text/warning strings from Phases 2-3 (T1), verify all pass (T2), COMMIT
   **Files:** `docs/execution.md` or `docs/auto-fix.md` | **Duration:** 0.5 day

### 5.5.A [ ] **[--no-sandbox Risk Docs — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 5.4-5.5]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.4-5.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.4-5.5]
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
   - CRITICAL/HIGH found -> List issues for 5.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 5.6 [ ] **[--no-sandbox Risk Documented - REFACTOR](plan/user-stories/04-document-auto-fix-sandbox-security-posture.md)**
   1. Fix CRITICAL/HIGH issues from 5.5.A (if any)
   2. Improve docs and tests (T1), validate (T3), COMMIT
   **Duration:** 0.25 day

### 5.7 [ ] **Phase 5 - DoD Verification**
   Run DoD Verification Checklist (T3 tests, coverage, lint, build, docs) across all 4 stories. Report using the DoD Report Template.
   **Duration:** 0.25 day

### 5.8 [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
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
No mutation testing tool detected on this machine (`stryker-mutator`, `mutmut`, `cargo-mutants` all unavailable) — skip this step. If a tool becomes available before this sprint executes, target only `internal/verify/autofix_exec.go` and the `RunResult`→`ValidationResult` adapter (high-risk translation logic), not the full codebase.

### Drift Analysis
Compare final implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] The Auto-Fix pipeline routes its validation steps through the existing `internal/sandbox` by default.
- [ ] A `--no-sandbox` flag exists to bypass the isolation, accompanied by strict security warnings in the CLI and documentation.
- [ ] No new sandbox backend or third-party execution package was introduced — `internal/sandbox.DockerBackend` reused as-is or minimally extended.
- [ ] Existing `--exec` (Epic 11.0) and `--auto-fix` apply/revert (Epic 17.0) behavior is unaffected outside the validation call site.
