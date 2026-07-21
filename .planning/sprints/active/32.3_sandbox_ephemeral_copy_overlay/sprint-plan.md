# Sprint 32.3: sandbox ephemeral copy overlay

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 32.3 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

An opt-in ephemeral-copy writable overlay for the ATCR Docker sandbox: a new `RunSpec.Writable` field and `DockerConfig.WorkSize` tunable that, when set, mount the project snapshot read-only at `/src` and back `/work` with a writable tmpfs populated by a `cp -a` setup step — unlocking `--auto-fix` validation for Node, Rust, and Python projects that need to write into their working directory during testing.

### Why This Matters

Today's read-only `/work` mount silently fails non-Go `--auto-fix` validation runs with `EROFS`, causing valid fixes to be discarded as invalid. This sprint fixes that without weakening `--exec`'s existing read-only guarantee, which stays byte-identical and opt-out by default.

### Key Deliverables

- `RunSpec.Writable bool` and `DockerConfig.WorkSize string` config surface, defaulting to today's behavior
- Conditional `dockerRunArgs` mount branch: `/src:ro` + `/work` tmpfs when `Writable:true`, unchanged `/work:ro` otherwise
- Shell-wrapped (Command mode) and stdin-prepended (Script mode) `cp -a /src/. /work/ && cd /work` setup injection, with no shell interpolation of the real payload
- `RunSandboxedValidation` opts `--auto-fix` into `Writable: true`
- Regression tests proving `Writable:false` stays byte-identical, plus `docs/auto-fix.md` and `autofix_exec.go` doc-comment parity updates

### Success Criteria

- Full existing `internal/sandbox` test suite and both `--exec` call sites pass unmodified with zero behavior change
- A non-Go validation command/script that writes into its working directory succeeds under `Writable:true` for both Command and Script modes
- `--auto-fix` validation no longer produces a false-negative `EROFS` failure
- The `/src` snapshot remains read-only for the container's entire lifetime; only the ephemeral `/work` tmpfs is writable and dies with the container
- `docs/auto-fix.md` and `autofix_exec.go`'s doc comment are updated to match the new behavior

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (RED, then GREEN, ADVERSARIAL, REFACTOR) across all 5 stories — driven by sprint complexity 9/12 (COMPLEX). `--gated` implies `--adversarial`: every story's GREEN task is followed by a fresh-subagent adversarial review before REFACTOR, and every phase ends with a phase-boundary GATE review before `/execute-sprint` stops.

**Inline-fix bar:** CRITICAL/HIGH findings are fixed inline in the following REFACTOR/GATE task. MEDIUM/LOW findings are deferred to `clarifications/tech-debt-captured.md`.

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
| T1: Focused | After each small change | `go test ./internal/sandbox/... -run <TestName>` |
| T2: Module | After completing element | `go test ./internal/sandbox/...` or `./internal/verify/...` |
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

## Core Philosophy

**"It's faster to write five lines of code today than to write one line today and then have to edit it in the future."**

Your goal is to create software that:
- Maintains constant developer velocity regardless of project size.
- Can be understood and maintained by any developer.
- Has modules that can be completely replaced without breaking the system.
- Optimizes for human cognitive load, not code cleverness.

### Architecture Principles
- **Black Box Interfaces:** Every module should be a black box with a clean, documented API; implementation details hidden.
- **Replaceable Components:** Any module should be rewritable from scratch using only its interface.
- **Single Responsibility Modules:** One module, one clear purpose.
- **Primitive-First Design:** Identify core primitives; build complexity through composition, not complicated primitives.

### Go & MCP Specific Guidelines
- **Panic Safety:** Ensure goroutines and worker tasks handle recovery.
- **Defer Cleanup:** Always use `defer` to close resources immediately after creation.
- **Interface Segregation:** Return concrete types (struct pointers) from constructors; consume interfaces.
- **Robust Protocol Handling:** Validate input parameters thoroughly.

## Coding Standards (Go)
- **Naming:** Packages lowercase single-word; exported `PascalCase`; unexported `camelCase`; functions `PascalCase`; files snake_case or lowercase.
- **Imports:** stdlib, then third-party, then internal (`github.com/samestrin/atcr/`) — `goimports` auto-arranges.
- **Error Handling:** Return `error` as last param; never ignore errors; wrap with `fmt.Errorf("doing action: %w", err)`; no `panic` for normal error conditions.
- **Context:** Accept `context.Context` as first param for long-running/I/O operations.
- **Testing:** Table-driven tests; `*_test.go` co-located with code under test; `testify` for assertions; integration tests behind `//go:build integration`.
- **Quality Gates:** `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...` before commit.

## Git Strategy
- **Branching:** `feature/short-description` from `main`; GitHub Flow / trunk-based.
- **Commits:** Conventional Commits (`type(scope): description`); small and atomic.
- **PRs:** One logical change, <400 lines ideally, squash-merged, CI (`Go CI`: fmt/vet/lint/tests) must pass.

---

## External Resources

**[current-sandbox-guarantees.md](plan/documentation/current-sandbox-guarantees.md)** — Contract map of every existing doc/code-comment claim about `--exec`'s read-only `/work` guarantee, each annotated PRESERVE (`--exec` control group, must stay true for `Writable:false`) or UPDATE (T6/Story 5 docs-parity scope): `docs/execution.md`, `docs/auto-fix.md`, the `internal/sandbox` package doc, `internal/verify/autofix_exec.go`.

**[docker-tmpfs-and-read-only-mounts.md](plan/documentation/docker-tmpfs-and-read-only-mounts.md)** — Official Docker reference excerpts for `--tmpfs` syntax (`rw`/`exec`/`size=`), the global `--read-only` rootfs flag, and `:ro` bind mounts — the exact mount semantics the `Writable:true` overlay mirrors from the existing `/scratch` pattern.

**Scope note:** `.planning/specifications/` has no standard covering Docker/container mount internals; the Docker docs above are the source of truth for the mechanism. Stdlib (`os/exec`, `context`) and test (`testify`) idioms follow `.planning/specifications/packages/standard-library.md` and `.../testify.md`.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Config Surface

### 1.1 [ ] **[Opt-In Writable Configuration Surface - RED](plan/user-stories/01-opt-in-writable-configuration-surface.md)**
   1. Analyze [AC 01-01](plan/acceptance-criteria/01-01-runspec-writable-field.md), [AC 01-02](plan/acceptance-criteria/01-02-dockerconfig-worksize-default.md), [AC 01-03](plan/acceptance-criteria/01-03-zero-behavior-change-for-existing-callers.md); identify testable units
   2. Write tests: `RunSpec.Writable` defaults `false`; `DockerConfig.WorkSize` default set in `DefaultDockerConfig()`; full-suite regression assertion proving both `--exec` call sites and the existing `internal/sandbox` suite are unaffected
   3. Verify tests fail correctly (new fields/assertions do not yet exist)
   **Files:** `internal/sandbox/sandbox_test.go`, `internal/sandbox/docker_test.go` | **Duration:** 2-3 hours

### 1.2 [ ] **[Opt-In Writable Configuration Surface - GREEN](plan/user-stories/01-opt-in-writable-configuration-surface.md)**
   Add `Writable bool` to `RunSpec` (`internal/sandbox/sandbox.go`) and `WorkSize string` to `DockerConfig` with a sane default in `DefaultDockerConfig()` (`internal/sandbox/docker.go`), mirroring the `ScratchSize` pattern exactly — no branching logic yet (T1). Verify all pass (T2). COMMIT: `git commit -m "feat(sandbox): add opt-in RunSpec.Writable and DockerConfig.WorkSize fields"`
   **Files:** `internal/sandbox/sandbox.go`, `internal/sandbox/docker.go` | **Duration:** 2-3 hours

### 1.2.A [ ] **[Opt-In Writable Configuration Surface - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-opt-in-writable-configuration-surface.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.2]
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

### 1.3 [ ] **[Opt-In Writable Configuration Surface - REFACTOR](plan/user-stories/01-opt-in-writable-configuration-surface.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Improve code and tests (T1), validate all still pass (T3): `go test ./internal/sandbox/... ./internal/tools/...`
   3. COMMIT: `git commit -m "refactor(sandbox): address review + clean up config surface"`
   **Duration:** 1 hour

### 1.4 [ ] **Phase 1 - Definition of Done**
   1. `go test ./internal/sandbox/... ./internal/tools/...` — all passing (T3)
   2. Coverage ≥80% on touched files
   3. `golangci-lint run` — no errors
   4. `go build ./...` — succeeds
   5. Story-specific: [AC 01-01](plan/acceptance-criteria/01-01-runspec-writable-field.md), [AC 01-02](plan/acceptance-criteria/01-02-dockerconfig-worksize-default.md), [AC 01-03](plan/acceptance-criteria/01-03-zero-behavior-change-for-existing-callers.md) all verified
   **Report:** `Story-1 DoD Complete / Auto: {X}/5 | Story-Specific: {Y}/3`

### 1.5 [ ] **Phase 1 - GATE: Integration & Exit Review (subagent)**
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
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as 1.2.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Core Mount Mechanism

### 2.1 [ ] **[Conditional Writable /work Mount - RED](plan/user-stories/02-conditional-writable-work-mount.md)**
   1. Analyze [AC 02-01](plan/acceptance-criteria/02-01-writable-false-argv-byte-identical.md), [AC 02-02](plan/acceptance-criteria/02-02-writable-true-src-and-work-tmpfs-mounts.md), [AC 02-03](plan/acceptance-criteria/02-03-writable-setup-step-copies-src-into-work.md) (argv/stdin-level scope only — end-state integration scenarios verified in Phase 3 alongside Story 3); identify testable units
   2. Write tests: `Writable:false` argv stays exactly `-v SnapshotDir:/work:ro` (byte-identical to today, including `TestDockerRunArgs_HardeningFlagsPresent` staying green unmodified); `Writable:true` argv contains `SnapshotDir:/src:ro` plus `--tmpfs /work:rw,exec,size=<cfg.WorkSize>` and does NOT contain the old `/work:ro` bind form
   3. Verify tests fail correctly
   **Files:** `internal/sandbox/sandbox_test.go`, `internal/sandbox/docker_test.go` | **Duration:** 3-4 hours

### 2.2 [ ] **[Conditional Writable /work Mount - GREEN](plan/user-stories/02-conditional-writable-work-mount.md)**
   Branch `dockerRunArgs` (`internal/sandbox/docker.go`) on `spec.Writable`: `false` path stays textually untouched; `true` path mounts `SnapshotDir:/src:ro` and adds `--tmpfs /work:rw,exec,size=<cfg.WorkSize>` (T1). Verify all pass (T2): `go test ./internal/sandbox/...`. COMMIT: `git commit -m "feat(sandbox): branch dockerRunArgs mount on spec.Writable"`
   **Files:** `internal/sandbox/docker.go` | **Duration:** 4-5 hours

### 2.2.A [ ] **[Conditional Writable /work Mount - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-conditional-writable-work-mount.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
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

### 2.3 [ ] **[Conditional Writable /work Mount - REFACTOR](plan/user-stories/02-conditional-writable-work-mount.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code and tests (T1), validate all still pass (T3): `go test ./internal/sandbox/...`
   3. Confirm `TestDockerRunArgs_HardeningFlagsPresent` and Preflight's argv are byte-identical to pre-story output
   4. COMMIT: `git commit -m "refactor(sandbox): address review + clean up mount branching"`
   **Duration:** 1-2 hours

### 2.4 [ ] **Phase 2 - Definition of Done**
   1. `go test ./internal/sandbox/...` — all passing (T3)
   2. Coverage ≥80% on touched files
   3. `golangci-lint run` — no errors
   4. `go build ./...` — succeeds
   5. Story-specific: [AC 02-01](plan/acceptance-criteria/02-01-writable-false-argv-byte-identical.md), [AC 02-02](plan/acceptance-criteria/02-02-writable-true-src-and-work-tmpfs-mounts.md) verified; AC 02-03's argv/stdin-level assertions verified (end-state integration scenarios deferred to Phase 3 per sprint-design.md scope note)
   **Report:** `Story-2 DoD Complete / Auto: {X}/5 | Story-Specific: {Y}/3 (02-03 partial — completes in Phase 3)`

### 2.5 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
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
       - REGRESSION: Earlier-phase behavior still intact (Phase 1's config surface, `TestDockerRunArgs_HardeningFlagsPresent`)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as 2.2.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Setup Injection & Auto-Fix Wiring

### 3.1 [ ] **[Ephemeral-Copy Setup Injection - RED](plan/user-stories/03-ephemeral-copy-setup-injection.md)**
   1. Analyze [AC 03-01](plan/acceptance-criteria/03-01-command-mode-shell-wrap-injection.md), [AC 03-02](plan/acceptance-criteria/03-02-script-mode-stdin-prepend-injection.md), [AC 03-03](plan/acceptance-criteria/03-03-no-interpolation-injection-safety.md), and Phase 2's deferred end-state scenarios for [AC 02-03](plan/acceptance-criteria/02-03-writable-setup-step-copies-src-into-work.md); identify testable units
   2. Write tests: Command-mode `Writable:true` argv contains `/bin/sh`, `-c`, the copy-step string, and `--` followed by the original command tokens (never string-interpolated); Script-mode `Writable:true` has the copy step in `cmd.Stdin` content preceding the original script body; a metacharacter-bearing command token (`;`, `$(...)`, backticks) survives as a literal argv element
   3. Verify tests fail correctly
   **Files:** `internal/sandbox/docker_test.go`, `internal/sandbox/sandbox_test.go` | **Duration:** 3-4 hours

### 3.2 [ ] **[Ephemeral-Copy Setup Injection - GREEN](plan/user-stories/03-ephemeral-copy-setup-injection.md)**
   When `spec.Writable` is true: wrap Command-mode argv as `/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"' -- <original command...>` in `dockerRunArgs`; prepend `cp -a /src/. /work/ && cd /work\n` to Script-mode's script body before it is written to `cmd.Stdin` in `Run` (T1). Verify all pass (T2): `go test ./internal/sandbox/...`. COMMIT: `git commit -m "feat(sandbox): inject ephemeral-copy setup step for Writable:true"`
   **Files:** `internal/sandbox/docker.go` | **Duration:** 4-5 hours

### 3.2.A [ ] **[Ephemeral-Copy Setup Injection - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-ephemeral-copy-setup-injection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline. This review carries extra weight: the setup injection is the sprint's primary shell-injection surface.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.2]
     - Checklist (pass verbatim, with extra emphasis on SECURITY given the shell wrap):
       - SECURITY: Auth bypass, injection, data exposure? Specifically: is `spec.Command`/`spec.Script` ever string-concatenated into shell text, or exclusively passed via positional `-- "$@"` / stdin?
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

### 3.3 [ ] **[Ephemeral-Copy Setup Injection - REFACTOR](plan/user-stories/03-ephemeral-copy-setup-injection.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code and tests (T1), validate all still pass (T3): `go test ./internal/sandbox/...`
   3. Confirm `TestDockerRunArgs_ScriptUsesStdinShell` stays green unmodified
   4. COMMIT: `git commit -m "refactor(sandbox): address review + clean up setup injection"`
   **Duration:** 1-2 hours

### 3.4 [ ] **[`--auto-fix` Opts Into the Writable Overlay - RED](plan/user-stories/04-auto-fix-opts-into-writable-overlay.md)**
   1. Analyze [AC 04-01](plan/acceptance-criteria/04-01-auto-fix-validation-requests-writable-overlay.md), [AC 04-02](plan/acceptance-criteria/04-02-writable-flag-pinned-by-test-and-exec-preflight-stay-read-only.md); identify testable units
   2. Extend `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` with `assert.True(t, fb.gotSpec.Writable, ...)`, alongside its existing Command/SnapshotDir/Timeout/Script checks
   3. Verify test fails correctly (assertion not yet satisfied)
   **Files:** `internal/verify/sandboxvalidate_test.go` | **Duration:** 1 hour

### 3.5 [ ] **[`--auto-fix` Opts Into the Writable Overlay - GREEN](plan/user-stories/04-auto-fix-opts-into-writable-overlay.md)**
   Set `Writable: true` on the `sandbox.RunSpec` constructed by `RunSandboxedValidation` (`internal/verify/sandboxvalidate.go`) — a single field addition to an existing struct literal. `--exec`'s call sites and `ResolveAutoFixSandbox`'s Preflight call remain unmodified (T1). Verify all pass (T2): `go test ./internal/verify/...`. COMMIT: `git commit -m "feat(verify): opt --auto-fix validation into the writable overlay"`
   **Files:** `internal/verify/sandboxvalidate.go` | **Duration:** 1 hour

### 3.5.A [ ] **[`--auto-fix` Opts Into the Writable Overlay - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-auto-fix-opts-into-writable-overlay.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.5]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.5]
     - Checklist (pass verbatim, with emphasis on the `--exec`/Preflight control group remaining untouched):
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

### 3.6 [ ] **[`--auto-fix` Opts Into the Writable Overlay - REFACTOR](plan/user-stories/04-auto-fix-opts-into-writable-overlay.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve code and tests (T1), validate all still pass (T3): `go test ./internal/verify/... ./internal/sandbox/...`
   3. COMMIT: `git commit -m "refactor(verify): address review + clean up auto-fix opt-in"`
   **Duration:** 30-60 min

### 3.7 [ ] **Phase 3 - Definition of Done**
   1. `go test ./internal/sandbox/... ./internal/verify/...` — all passing (T3)
   2. Coverage ≥80% on touched files
   3. `golangci-lint run` — no errors
   4. `go build ./...` — succeeds
   5. Story-specific: [AC 03-01](plan/acceptance-criteria/03-01-command-mode-shell-wrap-injection.md), [AC 03-02](plan/acceptance-criteria/03-02-script-mode-stdin-prepend-injection.md), [AC 03-03](plan/acceptance-criteria/03-03-no-interpolation-injection-safety.md), [AC 04-01](plan/acceptance-criteria/04-01-auto-fix-validation-requests-writable-overlay.md), [AC 04-02](plan/acceptance-criteria/04-02-writable-flag-pinned-by-test-and-exec-preflight-stay-read-only.md) verified; [AC 02-03](plan/acceptance-criteria/02-03-writable-setup-step-copies-src-into-work.md)'s deferred end-state integration scenarios now closed
   **Report:** `Story-3/4 DoD Complete / Auto: {X}/5 | Story-Specific: {Y}/5 (+ AC 02-03 closed)`

### 3.8 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline. This is the highest-stakes gate in the sprint: it is the first point the full end-to-end mechanism (mount + injection + auto-fix wiring) exists together.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced? Does `RunSandboxedValidation`'s `Writable: true` actually reach a working `/src`+`/work` overlay end-to-end?
       - PHASE-EXIT CONTRACT: Downstream phases (Phase 4 regression/docs) can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact (`TestDockerRunArgs_HardeningFlagsPresent`, `--exec` call sites, Preflight)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as 3.2.A/3.5.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Regression Proof & Docs Parity

**Note:** This phase adds no new production code — it is the test-and-docs closing pass. It also serves as sprint Validation (see Final Phase below).

### 4.1 [ ] **[Regression Proof and Documentation Parity - RED](plan/user-stories/05-regression-proof-and-docs-parity.md)**
   1. Analyze [AC 05-01](plan/acceptance-criteria/05-01-writable-true-argv-stdin-shape-tests.md), [AC 05-02](plan/acceptance-criteria/05-02-fakedocker-write-proof-under-work.md), [AC 05-03](plan/acceptance-criteria/05-03-writable-false-regression-test-anchor.md), [AC 05-04](plan/acceptance-criteria/05-04-docs-and-doc-comment-parity-rewrite.md); identify testable units
   2. Write tests: `Writable:true` argv/stdin shape assertions for both modes; a `writeFakeDocker`-based test proving a mock validation script can write a file under `/work` and the write is observable; an explicit `Writable:false` byte-identical regression assertion
   3. Verify tests fail correctly (or, where they already reflect Phase 1-3 behavior, confirm they pass and add the missing gap-closing case for AC 05-02's write-proof)
   **Files:** `internal/sandbox/docker_test.go`, `internal/sandbox/sandbox_test.go` | **Duration:** 2-3 hours

### 4.2 [ ] **[Regression Proof and Documentation Parity - GREEN](plan/user-stories/05-regression-proof-and-docs-parity.md)**
   1. Land the new test cases from 4.1 using existing helpers (`writeFakeDocker`, `fakeDockerRecording`, `runArgsLine`) — no new scaffolding (T1). Verify all pass (T2): `go test ./internal/sandbox/... ./internal/verify/...`. COMMIT: `git commit -m "test(sandbox): add Writable regression and write-proof coverage"`
   2. Rewrite `docs/auto-fix.md`'s three stale passages (the unconditional "mount mode is still read-only" claim at line ~47, the "Limitation (read-only /work)" paragraph at lines ~55-60, and the EROFS "effectively Go-only" blockquote at lines ~62-71) plus `internal/verify/autofix_exec.go`'s duplicate `ResolveAutoFixSandbox` doc comment (lines ~47-55) to describe the opt-in writable overlay, including a note on the `/bin/sh` + `cp -a` image requirement. `grep -i "read-only\|go-only"` both files post-edit to confirm no stale phrasing survives. COMMIT: `git commit -m "docs(auto-fix): update EROFS limitation notes for the writable overlay"`
   **Files:** `internal/sandbox/docker_test.go`, `internal/sandbox/sandbox_test.go`, `docs/auto-fix.md`, `internal/verify/autofix_exec.go` | **Duration:** 3-4 hours

### 4.2.A [ ] **[Regression Proof and Documentation Parity - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-regression-proof-and-docs-parity.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.2]
     - Checklist (pass verbatim, plus: do `TestDockerRunArgs_HardeningFlagsPresent` and `TestResolveAutoFixSandbox_BuildsAndPreflights` show zero diffs to their own assertions? Does the docs rewrite leave any "read-only"/"Go-only" stale phrasing?):
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

### 4.3 [ ] **[Regression Proof and Documentation Parity - REFACTOR](plan/user-stories/05-regression-proof-and-docs-parity.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve tests and docs wording (T1), validate all still pass (T3): `go test ./...`
   3. Confirm `TestDockerRunArgs_HardeningFlagsPresent` and `TestResolveAutoFixSandbox_BuildsAndPreflights` remain unmodified and green
   4. COMMIT: `git commit -m "refactor(sandbox): address review + polish regression/docs pass"`
   **Duration:** 1 hour

### 4.4 [ ] **Phase 4 - Definition of Done**
   1. `go test ./...` — all passing (T3)
   2. Coverage ≥80% overall
   3. `golangci-lint run` — no errors
   4. `go build ./...` — succeeds
   5. Story-specific: [AC 05-01](plan/acceptance-criteria/05-01-writable-true-argv-stdin-shape-tests.md), [AC 05-02](plan/acceptance-criteria/05-02-fakedocker-write-proof-under-work.md), [AC 05-03](plan/acceptance-criteria/05-03-writable-false-regression-test-anchor.md), [AC 05-04](plan/acceptance-criteria/05-04-docs-and-doc-comment-parity-rewrite.md) all verified
   **Report:** `Story-5 DoD Complete / Auto: {X}/5 | Story-Specific: {Y}/4`

### 4.5 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline. This is the sprint's final gate — treat it as a full end-to-end sanity check of the entire ephemeral-copy overlay mechanism, not just this phase's diff.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Sprint-level Final Phase Validation can run cleanly against this state?
       - REGRESSION: All four phases' behavior still intact end-to-end (`--exec`, Preflight, `--auto-fix`, docs)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as 4.2.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%, `go test -coverprofile=coverage.out ./...`)
- [ ] Lint/format clean: `golangci-lint run`, `go fmt ./...` / `goimports`
- [ ] Build succeeds: `go build ./...`

### Optional: Targeted Mutation Testing
No mutation testing tool detected in this environment (`stryker-mutator`, `mutmut`, `cargo-mutants` all unavailable) — skip this step. If one becomes available, target only `internal/sandbox/docker.go`'s mount-branch and setup-injection logic (the highest-risk changed code); do NOT run full-codebase mutation testing.

### Drift Analysis
Compare final state against `plan/original-requirements.md`:
- [ ] `RunSpec.Writable` defaults `false`; every existing caller (both `--exec` call sites, full existing `internal/sandbox` suite including `TestDockerRunArgs_HardeningFlagsPresent`) provably unaffected
- [ ] `Writable:true` lets a validation command/script write into its working directory (Node/Rust/Python), for both Command-mode and Script-mode `RunSpec`s
- [ ] `RunSandboxedValidation` sets `Writable: true`, so non-Go `validate_command`s no longer produce a false-negative `EROFS` failure
- [ ] The `/src` snapshot remains read-only for the container's entire lifetime; only the ephemeral `/work` tmpfs (and its writes) dies with the container — no host file is ever mutated
- [ ] `docs/auto-fix.md`'s EROFS/read-only note and `internal/verify/autofix_exec.go`'s duplicate doc comment are both updated to match the new behavior
