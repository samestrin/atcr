# Sprint 32.4: workspace integrity sanitization

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 32.4 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — `/execute-sprint` stops at each phase boundary (the `N.LAST` gate) instead of running continuously.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A strict, un-bypassable path-protection guard (`internal/security/pathguard.go`) that blocks `--auto-fix` from writing to critical host-execution and configuration paths (`.git/`, `.githooks/`, `.github/workflows/`, `.vscode/`, `.idea/`, `.env*`, `.planning/`, `.atcr`) unless an operator explicitly passes `--allow-config-edits`; a new `internal/gitexec` package that hardens every host git subprocess invocation across all six existing call sites against poisoned `.git/config`/system/global config hijacking; and a non-blocking `FlagsForReview` check that surfaces executable-bit and build-script changes as a visible warning section in the generated `--auto-fix` PR body.

### Why This Matters

Recent disclosures (Pillar Security, CVE-2026-48124) show AI coding agents are compromised not by breaking the sandbox itself, but through **Host Trust Transposition** — a contained sandbox execution writes a malicious configuration artifact into the workspace, and host-side tools (Git CLI, IDEs, CI runners) execute it with full developer privileges after the sandboxed review ends. ATCR's `--auto-fix` pipeline writes LLM-generated patches to the host repository and runs host-side git commands, making it exposed to exactly this class of Indirect Sandbox Escape.

### Key Deliverables

- `internal/security/pathguard.go` exporting `IsProtectedPath(path string) bool` with a comprehensive, boundary-safe protected-path blocklist (T1)
- `pathguard.IsProtectedPath` wired into `internal/autofix/apply.go`'s `applyOne` as a fail-closed gate, bypassable only via `AllowConfigEdits` (T2)
- `internal/gitexec` package injecting `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, and `--no-ext-diff` (where applicable) into every host git subprocess, migrated across all six production call sites (T3)
- `--allow-config-edits` CLI flag (off by default, mandatory stderr warning) plus `docs/security.md` documenting the security architecture, indexed from `docs/README.md` (T4)
- Full unit/regression coverage for `pathguard` and `gitexec`, including a binary whole-tree regression test proving zero stray bare `exec.Command("git",...)` call sites remain outside `internal/gitexec` (T5)
- Non-blocking `FlagsForReview(path, oldMode, newMode) (bool, string)` check surfaced as a `## Review Warnings` PR-body section for executable-bit changes and build-script path touches (T6)

### Success Criteria

- AC1: `--auto-fix` refuses to modify or create files under `.git/`, `.githooks/`, `.github/workflows/`, `.vscode/`, `.idea/`, or `.env*` and returns a security error unless `--allow-config-edits` is set.
- AC2: All Git subprocesses executed by ATCR carry `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` in their environment variables.
- AC3: Unit tests in `internal/security/pathguard_test.go` verify 100% path matching across canonical, relative, and symlink-traversal path formats.
- AC4: All six identified host git subprocess call sites are migrated to `internal/gitexec`; no bare `exec.Command("git", ...)` / `exec.CommandContext(ctx, "git", ...)` call sites remain outside it.
- AC5: A generated `--auto-fix` PR whose patch touches an executable-bit change or a build-script path includes a visible warning section in the PR body naming the flagged path(s) and reason; a patch with no flagged paths has no such section.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Complexity:** 9/12 (COMPLEX) → **Default TDD Mode:** Moderate 🔄 — `--tdd-strict`/`--tdd-pragmatic` were not passed, so this was calculated automatically from the complexity score.

**TDD Mode:** auto (task-based, non-feature plan). This is a **tech-debt** plan sourced from `tasks/`, so each work item follows the TASK-BASED cadence (understand → test → implement → verify → document) rather than the feature RED/GREEN/REFACTOR split.

**Test-first discipline still applies:** every code-bearing task lands its unit/integration tests alongside (or before) the implementation, per the co-located `*_test.go` convention. AC4's whole-tree regression test is mandatory and binary — it is the CI-enforced gate proving no call site was missed during T3's migration.

**Adversarial review:** ENABLED 🎯 (implied by `--gated`). Because this is a task-based plan, the adversarial pass runs at each **phase boundary** via the `N.LAST` gate (fresh subagent, hostile-integrator perspective) rather than per-task. Inline-fix bar: **CRITICAL/HIGH**. Defer to tech debt: **MEDIUM/LOW**.

**Execution mode:** Gated 🚧 — `/execute-sprint` stops at each phase's `N.LAST` gate.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [plan.md](plan/plan.md) | Plan overview |
| [tasks/](plan/tasks/) | Detailed per-task specifications (6 tasks) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/<pkg>/... -run <TestName>` |
| T2: Module | After completing a task | `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` (+ `go vet ./...`, `golangci-lint run`) |

### DoD Verification Checklist
1. Tests (T3): All passing
2. Coverage: ≥80%
3. Lint: No errors (`golangci-lint run`)
4. Build: Succeeds (`go build ./...`)
5. Docs: Updated

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

### Core Philosophy

**"It's faster to write five lines of code today than to write one line today and then have to edit it in the future."**

Your goal is to create software that maintains constant developer velocity, is understandable and maintainable by any developer, has modules that can be completely replaced without breaking the system, and optimizes for human cognitive load over code cleverness.

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

### Coding Standards (Go)
- **Naming:** Packages lowercase single-word; exported `PascalCase`; unexported `camelCase`; functions `PascalCase`; files snake_case or lowercase.
- **Imports:** stdlib, then third-party, then internal (`github.com/samestrin/atcr/`) — `goimports` auto-arranges.
- **Error Handling:** Return `error` as last param; never ignore errors; wrap with `fmt.Errorf("doing action: %w", err)`; no `panic` for normal error conditions.
- **Context:** Accept `context.Context` as first param for long-running/I/O operations.
- **Testing:** Table-driven tests; `*_test.go` co-located with code under test; `testify` for assertions; integration tests behind `//go:build integration`.
- **Quality Gates:** `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...` before commit.

### Git Strategy
- **Branching:** `feature/32.4_workspace_integrity_sanitization` from `main`; GitHub Flow / trunk-based.
- **Commits:** Conventional Commits (`type(scope): description`); small and atomic.
- **PRs:** One logical change, <400 lines ideally, squash-merged, CI (`Go CI`: fmt/vet/lint/tests) must pass.

---

## External Resources

No specifications in `.planning/specifications/` scored above the configured semantic relevance threshold (0.7) for this plan's topics (protected path validation, git subprocess hardening, auto-fix patch application, sandbox escape prevention, CLI security flags). This plan's security hardening work is novel to the codebase and not yet covered by existing architectural specifications — `internal/validation/validation.go`'s `FilePath` denylist matcher (cited throughout the task files) is the closest in-repo precedent for `pathguard.go`'s shape.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation

*Build both foundational, mutually-independent primitives in parallel. T1 has zero dependencies; T3 is explicitly independent of T1/T2 per the plan's Risk Mitigation notes. Landing both first unblocks every downstream task.*

### 1.1 [ ] **🔧 Build `internal/security/pathguard.go` Protected-Path Blocklist**
   **Task:** Create the new `internal/security` package exporting `IsProtectedPath(path string) bool`, normalizing the input (`filepath.Clean` + conditional `filepath.EvalSymlinks` with existing-ancestor fallback for not-yet-created paths) and matching it via boundary-safe prefix comparison (never bare `strings.HasPrefix`) against a blocklist covering `.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`/CI defs, `.vscode/`, `.idea/`, `.env*`, `.planning/`, and `.atcr` — mirroring `internal/validation/validation.go`'s `FilePath` denylist-matcher shape.
   **Priority:** P1 | **Effort:** S
   1. Read `internal/validation/validation.go`'s `FilePath`/`windowsSystemPath` pattern for boundary-safe matching precedent
   2. Write table-driven tests per blocklist category: exact match, nested file, canonical absolute, relative (`./x`, bare `x`), `../`-traversal, symlink-traversal, plus negative cases (`.gitignore`, `.githubx/`, empty string)
   3. Implement `IsProtectedPath` with `filepath.Clean` + conditional `EvalSymlinks` + boundary-safe segment matching
   4. Verify no I/O side effects beyond the necessary `EvalSymlinks` read; `go vet`/`gofmt` clean
   5. Document scope in the package doc comment (repo-relative blocklist, not `internal/validation`'s absolute-system-dir job)
   **Success Criteria:** `IsProtectedPath` compiles and exports correctly; blocklist covers all named categories; canonical/relative/traversal/symlink forms all resolve correctly; no false positives on lookalike paths (`.gitignore`, `.githubx/`).
   **Files:** `internal/security/pathguard.go` | **Duration:** ~1 day
   **Task File:** [task-01](plan/tasks/task-01-build-pathguard-package.md)

### 1.2 [ ] **🔧 Build `internal/gitexec` and Migrate All Six Host Git Call Sites**
   **Task:** Create `internal/gitexec/gitexec.go` exposing `CommandFn`/`CommandContextFn` as swappable package-level vars (mirroring the existing `resolveHeadSHAFn`/`removeFn`/`writeFileAtomicFn` testability pattern) that unconditionally inject `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` additively over `cmd.Environ()`. Migrate all six production call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go` ×2, `internal/stream/fileindex.go`) to construct their `*exec.Cmd` exclusively through it, adding `--no-ext-diff` to the two diff-family invocations.
   **Priority:** P1 | **Effort:** M
   1. Understand the six existing call sites and the package-var testability precedent (`internal/autofix/apply.go`)
   2. Write `gitexec_test.go` asserting the hardening env vars are present in `cmd.Env`
   3. Implement `hardenEnv` + `CommandFn`/`CommandContextFn`; migrate all six call sites one at a time, preserving each site's existing env customizations (`LC_ALL=C`/`LANG=C`, `-c credential.helper=...`) as additive appends
   4. Verify with a repo-wide grep for bare `exec.Command("git"`/`exec.CommandContext(ctx, "git"` outside `internal/gitexec/`, confirming zero remaining matches (excluding the two confirmed out-of-scope files `internal/verify/localvalidate.go`, `internal/sandbox/docker.go`)
   5. Remove now-unused `os/exec` imports where fully replaced; document the threat model in the package doc comment
   **Success Criteria:** All six call sites route through `gitexec`; hardening env vars present on every constructed command; `--no-ext-diff` present on the two diff-family invocations; repo-wide grep returns zero stray matches; all pre-existing per-call-site behavior preserved exactly.
   **Files:** `internal/gitexec/gitexec.go`, `cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go`, `internal/stream/fileindex.go` | **Duration:** ~2.5 days
   **Task File:** [task-03](plan/tasks/task-03-build-gitexec-and-migrate-call-sites.md)

### 1.3 [ ] **Phase 1 — DoD Validation**
   - [ ] `go test ./internal/security/... ./internal/gitexec/... ./cmd/atcr/... ./internal/fanout/... ./internal/gitrange/... ./internal/payload/... ./internal/personas/... ./internal/stream/...` passing (T3 scoped)
   - [ ] Coverage ≥80% on new code
   - [ ] `golangci-lint run` clean
   - [ ] `go build ./...` succeeds
   - [ ] `internal/security` and `internal/gitexec` package/function doc comments complete
   - [ ] DoD report emitted

### 1.4 [ ] **Phase 1 - GATE: Integration & Exit Review (subagent)**
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
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced? Are T1 and T3 genuinely independent (no accidental cross-import)?
       - PHASE-EXIT CONTRACT: Can Phase 2 wire `IsProtectedPath` into `applyOne` without rework?
       - REGRESSION: Earlier behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Integration

*Wire the T1 gate into the single host-repo write choke point in `internal/autofix/apply.go`, immediately after `containedPath` and before `refuseSymlinkLeaf`/the delete-modify-create branches. Depends on T1 only.*

### 2.1 [ ] **🔧 Wire pathguard into `internal/autofix/apply.go`'s `applyOne`**
   **Task:** Extend `ApplyPatch`/`applyOne` with a new `allowConfigEdits bool` parameter. Insert `security.IsProtectedPath(e.Path)` immediately after `containedPath` succeeds and before `refuseSymlinkLeaf`, refusing with a wrapped security error when the path is protected and `allowConfigEdits` is false. Update the sole call site (`cmd/atcr/autofix.go:365`) to pass the new parameter, defaulted `false` until T4 lands the flag.
   **Priority:** P1 | **Effort:** S | **Depends on:** 1.1 (T1)
   1. Understand `applyOne`'s existing choke-point ordering (`containedPath` → `refuseSymlinkLeaf` → delete/modify/create branches)
   2. Write tests: refusal for protected create/modify/delete entries when `allowConfigEdits=false`; success when `true`; no behavior change for non-protected paths; ordering assertion (protected-path error fires before parse/backup/write)
   3. Implement the gate insertion, thread `allowConfigEdits` through `ApplyPatch`/`applyOne`, update the one call site
   4. Verify the existing per-entry error-isolation contract (one rejected entry never blocks siblings) holds for the new rejection path
   5. Update `ApplyPatch`'s doc comment to document the new parameter and refusal behavior
   **Success Criteria:** `applyOne` calls `security.IsProtectedPath` at the documented choke point; protected paths refused by default across all three entry kinds (create/modify/delete); bypass works when `allowConfigEdits=true`; non-protected paths completely unaffected.
   **Files:** `internal/autofix/apply.go`, `cmd/atcr/autofix.go` | **Duration:** ~1 day
   **Task File:** [task-02](plan/tasks/task-02-wire-pathguard-into-apply.md)

### 2.2 [ ] **Phase 2 — DoD Validation**
   - [ ] `go test ./internal/autofix/... ./cmd/atcr/...` passing (full `go test ./...` exit=0)
   - [ ] Coverage ≥80% on new code
   - [ ] `golangci-lint run` clean
   - [ ] `go build ./...` succeeds
   - [ ] `ApplyPatch` doc comment updated
   - [ ] DoD report emitted

### 2.3 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
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
       - PHASE-EXIT CONTRACT: Can Phase 3's `--allow-config-edits` flag thread into `AllowConfigEdits` without rework?
       - REGRESSION: Earlier-phase behavior still intact (Phase 1's `pathguard`/`gitexec`, existing `applyOne` non-protected-path behavior)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: CLI & Docs

*Land the operator escape valve and its mandatory warning, plus the security architecture doc and its `docs/README.md` index entry. Depends on T2's `AllowConfigEdits` threading target existing.*

### 3.1 [ ] **🔧 Add `--allow-config-edits` Flag and Document Security Architecture**
   **Task:** Register `--allow-config-edits` in `addAutoFixFlags` mirroring the `--no-sandbox` precedent (off by default, help text documents the risk), add `warnAllowConfigEdits` (non-memoized stderr warning firing on every use), thread the resolved bool onto `autoFixBackend` for T2's gate to consume. Write `docs/security.md` covering the threat model, pathguard blocklist, `--allow-config-edits`, `internal/gitexec` hardening, and `FlagsForReview`; add its index entry to `docs/README.md`.
   **Priority:** P1 | **Effort:** S | **Depends on:** 2.1 (T2)
   1. Understand the `--no-sandbox`/`warnNoSandbox` precedent in `cmd/atcr/autofix.go`
   2. Write tests: flag-absent-defaults-false (no warning, field false), flag-true-prints-warning-and-sets-field, no flag-name collision
   3. Implement flag registration, `warnAllowConfigEdits`, threading onto `autoFixBackend`; write `docs/security.md`; add the `docs/README.md` index entry
   4. Verify `docs/README.md` renders with a working relative link to `docs/security.md`
   5. Confirm doc-comment consistency; `gofmt`/`go vet` clean
   **Success Criteria:** `--allow-config-edits` off by default, byte-identical behavior when absent; warning fires unconditionally on every use; resolved value reaches T2's gate without re-parsing; `docs/security.md` exists and is indexed from `docs/README.md`.
   **Files:** `cmd/atcr/autofix.go`, `docs/security.md`, `docs/README.md` | **Duration:** ~1 day
   **Task File:** [task-04](plan/tasks/task-04-allow-config-edits-flag-and-docs.md)

### 3.2 [ ] **Phase 3 — DoD Validation**
   - [ ] `go test ./cmd/atcr/...` passing (full `go test ./...` exit=0)
   - [ ] Coverage ≥80% on new code
   - [ ] `golangci-lint run` clean
   - [ ] `go build ./...` succeeds
   - [ ] `docs/security.md` written; `docs/README.md` index entry present and link verified
   - [ ] DoD report emitted

### 3.3 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat? Is `--allow-config-edits` genuinely off by default with zero behavior change when absent?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Can Phase 4's `FlagsForReview` PR-body wiring proceed without rework?
       - REGRESSION: Earlier-phase behavior still intact (Phase 1/2's pathguard/gitexec/applyOne gate)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Non-Blocking Review Flags

*Extend `pathguard.go` with the advisory-only check, thread `[]ReviewFlag` out of `ApplyPatch` via an out-parameter (no signature churn on `applyOne`'s dozen existing returns), and append the `## Review Warnings` PR-body section in `runAutoFix`. Depends on T1 (pathguard exists) and T2 (the `applyOne` choke point and `f.OldMode`/`f.NewMode` availability post-`gitdiff.Parse` are already wired).*

### 4.1 [ ] **🔧 Non-Blocking `FlagsForReview` — Executable-Bit and Build-Script PR Warnings**
   **Task:** Add `FlagsForReview(path string, oldMode, newMode int) (bool, string)` to `internal/security/pathguard.go` (executable-bit change via `oldMode&0111 != newMode&0111`, plus a soft build-script path list). Add `ReviewFlag{Path, Reason}` to `internal/autofix/apply.go`, threaded through `applyOne` via an out-parameter (`flags *[]ReviewFlag`) so no existing `return` statement needs editing. Append a `## Review Warnings` section to the PR body in `runAutoFix` when the returned flag slice is non-empty.
   **Priority:** P2 | **Effort:** M | **Depends on:** 1.1 (T1), 2.1 (T2)
   1. Understand the T1/T2 choke point and where `f.OldMode`/`f.NewMode` become available post-`gitdiff.Parse`
   2. Write table-driven tests: executable bit added/removed, create-diff of an executable, each soft-list category (positive + near-miss negative), combined case
   3. Implement `FlagsForReview`, the `ReviewFlag` out-parameter threading through `applyOne`/`ApplyPatch`, and the PR-body warning-section builder in `runAutoFix`
   4. Verify a flagged-but-failed entry never appears in the returned `[]ReviewFlag`; zero flagged entries leaves the PR body byte-identical to today
   5. Update `ApplyPatch`'s doc comment for the new `[]ReviewFlag` return value
   **Success Criteria:** `FlagsForReview` never errors or panics; executable-bit and build-script conditions each flagged independently with a specific reason; `ApplyPatch` returns one entry per successfully-applied flagged path; PR body carries `## Review Warnings` exactly when flags exist.
   **Files:** `internal/security/pathguard.go`, `internal/autofix/apply.go`, `cmd/atcr/autofix.go` | **Duration:** ~2 days
   **Task File:** [task-06](plan/tasks/task-06-non-blocking-review-flags.md)

### 4.2 [ ] **Phase 4 — DoD Validation**
   - [ ] `go test ./internal/security/... ./internal/autofix/... ./cmd/atcr/...` passing (full `go test ./...` exit=0)
   - [ ] Coverage ≥80% on new code
   - [ ] `golangci-lint run` clean
   - [ ] `go build ./...` succeeds
   - [ ] `ApplyPatch` doc comment reflects the new `[]ReviewFlag` return value
   - [ ] DoD report emitted

### 4.3 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
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
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced? Does `FlagsForReview` only ever evaluate a path that already passed T2's gate?
       - PHASE-EXIT CONTRACT: Can Phase 5's regression/verification suite exercise all prior phases without rework?
       - REGRESSION: Earlier-phase behavior still intact (Phase 1-3's pathguard/gitexec/applyOne gate/CLI flag)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Testing & Validation

*Table-driven coverage for every blocklist category (canonical/relative/traversal/symlink forms per AC3), gitexec env/flag assertions (AC2), and the binary whole-tree AC4 regression test. This phase also serves as the sprint's overall Definition-of-Done validation gate.*

### 5.1 [ ] **🔧 Unit Tests for pathguard + gitexec, and Six-Site Migration Regression Coverage**
   **Task:** Read the finished T1/T3 implementations first (no guessed signatures), then write `internal/security/pathguard_test.go` (table-driven per blocklist category: exact, nested, canonical, relative, traversal, symlink, plus negatives) and `internal/gitexec/gitexec_test.go` (env/flag hardening assertions). Add the AC4 whole-tree regression test: greps/ASTs every `.go` file (excluding `internal/gitexec/`, `internal/verify/localvalidate.go`, `internal/sandbox/docker.go`) for bare `exec.Command("git",...)`/`exec.CommandContext(ctx,"git",...)`, failing on any match, and positively confirms all six known call sites reference `gitexec.`.
   **Priority:** P1 | **Effort:** M | **Depends on:** 1.1 (T1), 1.2 (T3)
   1. Read the finished `internal/security/pathguard.go` and `internal/gitexec/gitexec.go` to confirm exact exported names/signatures before writing assertions
   2. Write `pathguard_test.go`'s per-category table-driven suite (including a dedicated symlink-traversal subtest group for AC3)
   3. Write `gitexec_test.go`'s env/flag hardening assertions
   4. Implement the AC4 regression test (negative: zero stray bare git exec; positive: all six sites reference gitexec), with an inline comment explaining the two out-of-scope exclusions
   5. Run `go test ./...` tree-wide to confirm no false positives/negatives
   **Success Criteria:** `pathguard_test.go` exercises AC3's canonical/relative/traversal/symlink requirement in full; `gitexec_test.go` covers AC2's env/flag hardening; AC4 regression test passes against the current tree and fails loudly on a reverted/missed call site.
   **Files:** `internal/security/pathguard_test.go`, `internal/gitexec/gitexec_test.go` | **Duration:** ~2.5 days
   **Task File:** [task-05](plan/tasks/task-05-unit-tests-and-regression-coverage.md)

### 5.2 [ ] **Phase 5 — DoD Validation**
   - [ ] `go test ./...` passing (full suite)
   - [ ] AC4 whole-tree regression test present and passing (both negative and positive assertions)
   - [ ] Coverage ≥80% overall
   - [ ] `golangci-lint run` clean; `gofmt -l` clean
   - [ ] `go build ./...` succeeds
   - [ ] All five ACs (AC1–AC5) traced to a passing test or documented manual verification
   - [ ] DoD report emitted

### 5.3 [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 + full-sprint integration (final gate)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline. This is the sprint's final gate — treat it as a full end-to-end sanity check of the entire workspace-integrity mechanism, not just this phase's diff.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths), plus full-mechanism files from Phases 1-4 reviewed as reference for end-to-end regression: [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Sprint-level Final Phase Validation can run cleanly against this state?
       - REGRESSION: All five phases' behavior still intact end-to-end (pathguard, gitexec, applyOne gate, CLI flag/docs, FlagsForReview PR warnings)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop / Final Validation.
   **Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%)
- [ ] Lint/format clean: `golangci-lint run` + `gofmt -l`
- [ ] Build succeeds: `go build ./...`
- [ ] All 6 tasks checked off; all 5 phase gates passed
- [ ] Every AC (AC1–AC5) traced to a passing test or documented manual verification

### Optional: Targeted Mutation Testing
No mutation testing tool detected in this environment (`stryker-mutator`, `mutmut`, `cargo-mutants` all unavailable) — skip this step. If one becomes available, target only `internal/security/pathguard.go` and `internal/gitexec/gitexec.go` (the highest-risk changed code); do NOT run full-codebase mutation testing.

### Drift Analysis
Compare final state against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] `--auto-fix` refuses protected-path writes by default; `--allow-config-edits` is the sole documented bypass (AC1)
- [ ] Every host git subprocess carries the hardened environment (AC2), across all six call sites with no stray bare `exec.Command("git",...)` remaining (AC4)
- [ ] `pathguard_test.go` proves 100% path-format matching (canonical/relative/symlink-traversal) (AC3)
- [ ] `FlagsForReview` surfaces executable-bit/build-script changes as a non-blocking PR-body warning, never blocking the apply (AC5)
- [ ] `docs/security.md` documents the full security architecture and is indexed from `docs/README.md`
- [ ] Out-of-scope items NOT implemented: no LLM-based risk-scoring persona (the "Skeptic" AC3 idea dropped/replaced during `/refine-epic`); no changes to `internal/verify/localvalidate.go` or `internal/sandbox/docker.go`
- [ ] No scope added beyond the original request
