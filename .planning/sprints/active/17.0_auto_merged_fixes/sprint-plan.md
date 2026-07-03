# Sprint 17.0: Auto-Merged Fixes Execution

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 17.0 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated** mode — `/execute-sprint` STOPS at each phase boundary (after the phase GATE task) instead of running continuously. Resume advances to the next phase.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

ATCR gains the ability to actively apply the fixes it generates instead of leaving that burden on the developer. A new opt-in `--auto-fix` flow parses a model-generated diff, applies the patch to the local working tree safely, runs a configurable local validation command, reverts automatically if validation fails, and — only after validation passes — orchestrates a Git branch, commit, and pull request through the GitHub API.

### Why This Matters

Applying flagged fixes by hand creates friction and slows remediation. Auto-merged fixes close the loop from detection to a ready-to-review PR, while a conservative validate-then-revert gate guarantees zero broken builds are introduced.

### Key Deliverables

- `internal/autofix` — safe patch apply (`ApplyPatch`) and revert (`Revert`) over `atomicfs`, wrapping `go-gitdiff` (Stories 1, 3).
- `internal/verify` — configurable local validation runner with a conservative exit-code-only pass/fail gate (Story 2).
- `internal/ghaction.Client` — new `CreateBranch`, `CreateCommit`, `CreatePullRequest`, `UpdatePullRequest` methods reusing existing `postDo`/`get` retry/backoff/redaction plumbing (Stories 4, 5).
- `cmd/atcr` — `--auto-fix` opt-in flag (off by default) with an all-or-nothing refuse-without-backend gate (Story 6).

### Success Criteria

- Auto-fix at least 70% of the simple technical-debt items ATCR flags.
- Zero broken builds introduced by auto-merged fixes in the test corpus.
- No GitHub-mutating call (branch/commit/PR) is ever reachable before local validation has passed.
- Default behavior remains byte-identical when `--auto-fix` is absent.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Default TDD Mode:** Strict 🔒 (calculated from complexity 11/12 — VERY COMPLEX)

Every user story follows the **Strict + Adversarial** cycle:
1. **RED** — write comprehensive failing tests, verify they fail correctly.
2. **GREEN** — minimal code to pass, one test at a time (T1), verify all (T2), COMMIT.
3. **ADVERSARIAL REVIEW** — a *fresh subagent* (no memory of the implementation) reviews the changed files and returns a findings table.
4. **REFACTOR** — fix CRITICAL/HIGH findings inline, improve quality while maintaining green (T1), validate (T3), COMMIT.

**Adversarial inline-fix bar:** CRITICAL/HIGH fixed inline in REFACTOR. MEDIUM/LOW deferred to `tech-debt-captured.md`.

**Gated execution:** After each phase's DoD, a Phase-Boundary GATE task spawns a fresh subagent for an integration/exit review. `/execute-sprint` stops at each phase boundary.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements (6 stories) |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD (23 ACs) |
| [documentation/](plan/documentation/) | Patch-apply, validation/revert, GitHub orchestration, CLI gate docs |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/autofix/ -run TestApply...` |
| T2: Module | After completing an element | `go test ./internal/autofix/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: `go test -coverprofile=coverage.out ./...` ≥ 80%
3. Lint: `golangci-lint run` — no errors
4. Vet: `go vet ./...` — clean
5. Build: `go build ./...` succeeds
6. Docs: Updated where behavior changed

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/6 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

Conventional Commit types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`.

---

## Development Standards

**Architecture (implementation-standards.md):** Black-box interfaces — `internal/autofix` hides `go-gitdiff` + `atomicfs` sequencing behind `ApplyPatch`/`Revert`; `internal/verify` hides `os/exec` behind `RunConfiguredValidation`; `ghaction.Client` hides Git Data API mechanics behind its methods. Primitive-first: build on existing `payload.FileEntry`, new `autofix.BackupMap`, `verify.ValidationResult`. Replaceable components — keep the patch library swappable behind `ApplyPatch`'s signature.

**Coding (coding-standards.md):** Go naming (lowercase packages, `PascalCase` exported, `camelCase` unexported). Errors returned last and wrapped with `fmt.Errorf("doing action: %w", err)`; never ignored, never `panic` for normal conditions. `context.Context` first parameter on I/O ops (validation runner, GitHub calls). Return concrete types from constructors, accept interfaces. Table-driven tests, `testify` `assert`/`require`, `*_test.go` in the same package; integration tests behind `//go:build integration`.

**Git (git-strategy.md):** Branch `feature/17.0_auto_merged_fixes` off `main`. Small atomic Conventional Commits. Squash-and-merge PR against `main`; CI (`Go CI`: fmt, vet, lint, tests) must pass. Push deferred to `/finalize-sprint`.

**Quality gates before commit:** `go fmt`/`goimports`, `go vet ./...`, `golangci-lint run`.

---

## External Resources

| Resource | Use |
|----------|-----|
| [go-gitdiff package spec](plan/documentation/) | Patch parsing/application (`gitdiff.Parse`/`gitdiff.Apply`) — Story 1. The one new external dependency; wrapped entirely inside `internal/autofix/apply.go`. |
| [Patch Application (AC2)](plan/documentation/patch-application.md) | Core write-path over `internal/atomicfs`. |
| [Validation and Automatic Revert (AC3/AC4)](plan/documentation/validation-and-revert.md) | Extending `internal/verify/syntaxguard.go`; file-level rollback. |
| [GitHub API Orchestration (AC5)](plan/documentation/github-orchestration.md) | Extending `internal/ghaction/client.go`. |
| [CLI Opt-In Gate (AC6)](plan/documentation/cli-opt-in-gate.md) | `--auto-fix` gating, fail-fast backend check. |

**Reuse anchors (do not rebuild):** `internal/payload` `BuildEntriesFromDiff` (diff parse, AC1), `internal/verify/syntaxguard.go` (validation base, AC3), `internal/atomicfs` + `restorePriorBackup` (backup/restore, AC4), `internal/ghaction/client.go` `postDo`/`get` (GitHub plumbing, AC5), `internal/verify/executor.go` `generateFixes` (upstream fix source). Preserve `internal/payload`'s existing symlink-escape guard end-to-end.

---

## Clarifications

### Phase 5 Clarifications (recorded 2026-07-03)

**Key Decisions:**
1. **Orchestration shape — (a) gate + injectable `runAutoFix`.** Phase 5 delivers the `--auto-fix` flag (off by default), the fully-tested `validateAutoFixBackend` gate, and a `runAutoFix(ctx, …, backend, entries)` orchestration in `cmd/atcr/autofix.go` that runs apply → validate → revert-or-continue → branch/commit → PR over the existing public APIs (`autofix`, `verify`, `ghaction`). It does NOT modify those internal packages (Story 6 = CLI-level gate). Phase 6 drives `runAutoFix` end-to-end against the httptest stub with injected entries (where the zero-HTTP-on-failure guard lives).
2. **Live finding→entry selection is thin (deferred detail).** When wired live, `atcr review --auto-fix` feeds `runAutoFix` the reconciled findings that carry a non-empty `Fix` (converted via `payload.BuildEntriesFromDiff`): at/above the resolved `--fail-on` threshold when set, else all findings with a `Fix`. `--verify` is NOT hard-required (VERIFIED-only stays an optional future tightening mirroring `--require-verified`). No new AC governs selection; kept minimal.
3. **Config surface — top-level `auto_fix:` block** on `.atcr/config.yaml` project config (mirrors `sandbox:`), a new `AutoFix *AutoFixConfig` field on `registry.ProjectConfig`, strict-decoded. Leaf keys: `apply_target` (default repo root `.`), `validate_command` (optional argv; falls back to Story 2's `verify.ResolveValidateCommand` Go default), `validate_timeout` (duration string, default ~2 min per TD-008). Operator-facing gate error strings name `auto_fix.apply_target` / `auto_fix.validate_command` so guidance and the real key path agree (reconciles the AC 06-02 `apply.target` vocabulary to the actual block).
4. **Host command:** `--auto-fix` registers on `atcr review` (AC 06-01), alongside `--exec`. GitHub token/repo/api-url resolve via new `--token`/`--repo`/`--api-url` review flags + `GITHUB_TOKEN`/`GITHUB_REPOSITORY`/`GITHUB_API_URL` env (reusing `envOr`/`parseRepo`), NOT the `auto_fix:` block.

**Scope Boundaries:**
- IN: flag + gate + `runAutoFix` orchestration wired to `--auto-fix`; `AutoFixConfig` block + validation; `defaultValidationTimeout` resolver (TD-008); token-scope documentation in gate/help (TD-001, shape-only, no live scope check).
- OUT: fix generation (upstream reuse), live GitHub scope preflight, finding-selection behavior beyond the thin policy above, complex merge-conflict resolution.

**Technical Approach:**
- Gate reuses `usageError`/`envOr`/`parseRepo` (`cmd/atcr/github.go`) and `verify.ResolveValidateCommand` (`internal/verify/localvalidate.go`). Aggregates ALL missing/malformed pieces into one exit-2 `usageError` (`--auto-fix cannot run: …; …`).
- `runAutoFix`: `autofix.ApplyPatch` → `verify.RunConfiguredValidation` → on fail `autofix.RevertPatch` + non-zero exit, **zero** GitHub calls; on pass `autofix.CleanupBackups` → `ghaction.Client.CreateBranch`/`CreateCommit` → `FindOpenPullRequest` → `Create|UpdatePullRequest`.
- TD-013 honored: `CommitRequest.Message` stays atcr-generated boilerplate (not diagnostics-sourced), so no commit-message redaction gap.

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike (1 day)

**Goal:** De-risk the two most novel unknowns — a never-before-used `go-gitdiff` library and the GitHub Git Data API's 4-call sequence — before committing to `internal/autofix` and `ghaction.Client` interfaces. Spikes are throwaway; capture findings, then discard or seed fixtures.

### 1.1 [x] **Spike: `go-gitdiff` against representative fixtures**
   **Task:** Add `github.com/bluekeyes/go-gitdiff` to go.mod. In a throwaway `internal/autofix/spike_test.go` (or scratch main), run `gitdiff.Parse` + `gitdiff.Apply` against fixture diffs covering: modify, create (`/dev/null` old-side), delete (`/dev/null` new-side), and **drifted-context** (fuzzy hunk match). Record: does `Apply` return a clean error on a hunk that cannot be located, or mis-apply silently?
   **Success Criteria:** Documented behavior for each diff type; confirmed that a non-locatable hunk yields a non-nil error (the "hard failure" contract Story 1 depends on). Reusable fixtures captured for Phase 2 RED.
   **Files:** `go.mod`, `go.sum`, throwaway spike + fixtures | **Duration:** ~3h

### 1.2 [x] **Spike: GitHub Git Data API 4-call sequence against `httptest.Server`**
   **Task:** Stand up an `httptest.Server` stub routing on method+path for the blob → tree → commit → ref sequence (and the 422 ref-exists case). Drive it through the existing `internal/ghaction` `postDo`/`get` plumbing to confirm the request/response shapes, auth header flow, and retry/backoff behavior are compatible before Story 4 commits to method signatures.
   **Success Criteria:** Confirmed request bodies and response parsing for each of the 4 calls; confirmed 422 "ref already exists" is distinguishable; confirmed `postDo`/`get` can carry these calls without a second HTTP client. Stub routing pattern captured for Phase 4/6.
   **Files:** throwaway `internal/ghaction/spike_test.go` | **Duration:** ~3h

### 1.3 [x] **Phase 1 DoD**
   - [x] Both spikes executed; findings recorded (in commit message or a scratch note).
   - [x] `go-gitdiff` present in go.mod/go.sum; `go build ./...` succeeds.
   - [x] Interface decisions confirmed for Stories 1 and 4 (hard-failure-on-bad-hunk; single HTTP client reuse).
   - [x] Throwaway spike code removed or clearly marked; only fixtures/notes retained.
   - COMMIT: `git add go.mod go.sum && git commit -m "chore(autofix): add go-gitdiff dependency + spike findings"`

### 1.LAST [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (go.mod/go.sum, any retained fixtures/notes) — integration-level, not TDD cadence.

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's work — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are the interface decisions (hard-failure on bad hunk; single HTTP client for Git Data API) actually validated by the spike, or assumed?
       - CONFIG SURFACE: Is the new `go-gitdiff` dependency pinned to a specific version in go.sum?
       - INTEGRATION: Does reusing `postDo`/`get` for the 4-call sequence introduce hidden coupling or auth-scope assumptions?
       - PHASE-EXIT CONTRACT: Can Phase 2 (Story 1) and Phase 4 (Story 4) consume these findings without rework?
       - REGRESSION: Does adding the dependency break any existing build/test?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (first pass) + resolution:**

   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | HIGH | go.mod:13 | `go-gitdiff` was an unimported `// indirect` require; `go mod tidy` would prune it, destroying the durable artifact Phase 2 needs. | FIXED — added retained `internal/autofix/gitdiff_contract_test.go` (real importer + drift-hard-failure regression guard) and `"autofix": {}` allowlist entry; go-gitdiff now a **direct** require (go.mod:6), proven by `go mod why`. |
   | MEDIUM | findings note | Token `contents: write` scope unvalidated by the stub. | Deferred → TD-001 (Phase 4/5 gate precondition). |
   | MEDIUM | findings note:88 | Git Data API request shapes retained only as prose (1.2 spike deleted). | Deferred → TD-002 (Phase 4.1 RED re-establishes executable fixtures). |
   | LOW | findings note:88 | Note says "postDo/get" but only `postDo` used; 422 contract is POST-path-dependent. | Deferred → TD-003. |

   **Re-review (after HIGH fix): CLEAN** — "No findings — HIGH resolved, gate clean." Independently verified direct require, real importer chain, contract-test legitimacy, allowlist correctness, and green build/vet/test.

   **Phase gate passed.**

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin Phase 2.

---

## Phase 2: Foundation (4 days)

**Goal:** Build the local write-path (`internal/autofix/apply.go`) and the post-apply validation gate (`internal/verify` extension) — the foundation every later story depends on.

### 2.1 [x] **[Apply Patch to Working Tree - RED](plan/user-stories/01-apply-patch-to-working-tree-without-corruption.md)**
   **AC:** [01-01](plan/acceptance-criteria/01-01-parse-and-apply-hunks.md), [01-02](plan/acceptance-criteria/01-02-atomic-write-to-target-path.md), [01-03](plan/acceptance-criteria/01-03-per-file-backup-before-overwrite.md), [01-04](plan/acceptance-criteria/01-04-per-file-error-isolation.md)
   Write comprehensive failing tests for `ApplyPatch(entries []payload.FileEntry) (BackupMap, error)`: parse+apply hunks for modify/create/delete via `gitdiff.Parse`/`gitdiff.Apply` (01-01); every write goes through `atomicfs.WriteFileAtomic` (01-02); per-file backup via `atomicfs.BackupToDotBak` before any overwrite (01-03); a failed hunk yields a clear per-file error without corrupting prior successes (01-04). Include a path-traversal/symlink-escape fixture (preserve `payload` guard). Verify all fail correctly.
   **Files:** `internal/autofix/apply_test.go` | **Duration:** ~0.75 day

### 2.2 [x] **[Apply Patch to Working Tree - GREEN](plan/user-stories/01-apply-patch-to-working-tree-without-corruption.md)**
   Minimal `internal/autofix/apply.go`, one test at a time (T1), verify all (T2). Wrap `go-gitdiff` entirely; return a populated `BackupMap` (`originalPath -> backupPath`). COMMIT.
   COMMIT: `git add internal/autofix/apply.go internal/autofix/apply_test.go && git commit -m "feat(autofix): apply parsed patch atomically with per-file backup (green)"`
   **Files:** `internal/autofix/apply.go` | **Duration:** ~1 day

### 2.2.A [x] **[Apply Patch - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-apply-patch-to-working-tree-without-corruption.md)**
   **Changed Files:** `internal/autofix/apply.go`, `internal/autofix/apply_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `internal/autofix/apply.go`, `internal/autofix/apply_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Path-traversal target (`../../etc/passwd`)? Symlink-escape guard preserved? Arbitrary file write outside repo root?
       - EDGE CASES: Empty diff, create-on-existing-file, delete-missing-file, drifted hunk, partial multi-file failure?
       - ERROR HANDLING: Is every non-nil `gitdiff.Apply` error a hard per-file failure? Are prior successes left revertible?
       - PERFORMANCE: Full-tree scans? Unnecessary reads?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings + resolution:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | HIGH | apply.go containedPath | Symlink-escape guard not preserved — purely lexical containment; a symlinked directory component inside root (root/link -> /etc) lets a write follow the link outside the tree. | FIXED in 2.3 — `containedPath` now resolves symlinks on the parent dir + root (mirrors payload's `rejectDiffSymlinkEscape`) and re-checks containment; added `TestApplyPatch_SymlinkedDirComponentRefused`. |
   | MEDIUM | apply.go:108 (create branch) | Create-on-existing-file silently overwrites instead of rejecting like `git apply`. | Deferred → TD-004. Not data loss: prior content is backed up + revertible. |
   | LOW | apply.go:129 | Modify/delete of a symlink *leaf* → empty backup → Story-3 Revert would delete rather than restore the link. | Deferred → TD-005 (Story-3 Revert concern). |

   **HIGH resolved inline in 2.3; MEDIUM/LOW deferred to tech-debt-captured.md. Adversarial review complete.**

### 2.3 [x] **[Apply Patch to Working Tree - REFACTOR](plan/user-stories/01-apply-patch-to-working-tree-without-corruption.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any).
   2. Improve quality, maintain green (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(autofix): address review + clean up apply path"`
   **Duration:** ~0.5 day

### 2.4 [x] **[Configurable Local Validation - RED](plan/user-stories/02-configurable-local-validation.md)**
   **AC:** [02-01](plan/acceptance-criteria/02-01-configurable-validation-command-runner.md), [02-02](plan/acceptance-criteria/02-02-result-capture-and-reporting.md), [02-03](plan/acceptance-criteria/02-03-conservative-pass-fail-gate.md)
   Write failing tests for `RunConfiguredValidation(ctx, cmd string, timeout time.Duration) (ValidationResult, error)`: runner via `os/exec.CommandContext` with bounded timeout (02-01); result capture — exit code, stdout, stderr, duration (02-02); conservative exit-code-only pass/fail gate, no partial-success interpretation, no mutation (02-03). Include distinct cases for command-missing/not-executable vs. command-runs-nonzero, and timeout-is-hard-failure. Verify fail correctly.
   **Files:** `internal/verify/localvalidate_test.go` | **Duration:** ~0.5 day

### 2.5 [x] **[Configurable Local Validation - GREEN](plan/user-stories/02-configurable-local-validation.md)**
   Minimal `internal/verify/localvalidate.go` sibling to `syntaxguard.go`, one test at a time (T1), verify all (T2). `context.WithTimeout`; timeout is a hard failure, never retried; command sourced from operator config only. COMMIT.
   COMMIT: `git add internal/verify/localvalidate.go internal/verify/localvalidate_test.go && git commit -m "feat(verify): configurable local validation runner with conservative gate (green)"`
   **Files:** `internal/verify/localvalidate.go` | **Duration:** ~0.75 day

### 2.5.A [x] **[Configurable Local Validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-configurable-local-validation.md)**
   **Changed Files:** `internal/verify/localvalidate.go`, `internal/verify/localvalidate_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.5 — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `internal/verify/localvalidate.go`, `internal/verify/localvalidate_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Command injection if the configured string is attacker-influenced? Is the command operator-config-only, never diff/PR-derived? Shell interpolation vs. explicit argv?
       - EDGE CASES: Command missing/not-executable, non-zero exit, timeout, empty stdout/stderr, huge output?
       - ERROR HANDLING: Is a timeout a hard failure (never retried)? Is any non-zero exit treated as failure (no partial-success)? No state mutation on the pass/fail decision?
       - PERFORMANCE: Unbounded output buffering? Leaked process/goroutine on timeout?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings + resolution:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | MEDIUM | localvalidate.go:81 | Timeout SIGKILLs only the direct child; a command that spawns subprocesses (`sh -c "make ..."`) leaves grandchildren orphaned. WaitDelay unblocks Run but does not reap the group. | Deferred → TD-006. Core "never stalls indefinitely" guarantee already met by `cmd.WaitDelay`; only orphan reaping missing. |
   | LOW | localvalidate.go:106 | `exec.ErrWaitDelay` / non-deadline `context.Canceled` fall through the ExitError guard into the StartError branch, polluting that class. | Deferred → TD-007. Fails closed; unreachable via the `--auto-fix` bounded-timeout path (deadline is caught as TimedOut first). |

   **No CRITICAL/HIGH — nothing to fix inline. MEDIUM/LOW deferred to tech-debt-captured.md. Adversarial review complete.**

### 2.6 [x] **[Configurable Local Validation - REFACTOR](plan/user-stories/02-configurable-local-validation.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any). — None found.
   2. Improve quality, maintain green (T1), validate (T3). — Runner already clean; the timeout-stall robustness fix (`cmd.WaitDelay`) was folded into 2.5 GREEN when caught, so no separate refactor code change was warranted.
   3. COMMIT — no code refactor; planning-doc deferrals (TD-006/TD-007) committed with the phase.
   **Duration:** ~0.5 day

### 2.7 [x] **Phase 2 DoD (Stories 1 & 2)**
   - [x] Tests (T3): `go test ./internal/autofix/... ./internal/verify/...` all passing.
   - [x] Coverage ≥ 80% on new files. (apply.go 91.7%; localvalidate.go core fns 100%, Write branch 75% — well above bar.)
   - [x] `go vet ./...` clean; `golangci-lint run` 0 issues; `go build ./...` succeeds.
   - [x] `BackupMap` (`map[string]string` originalPath→backupPath) and `ValidationResult` contracts match sprint-design.md Architecture (ValidationResult adds AC-mandated `TimedOut`/`StartError` + truncation flags; `Passed()` is a method per AC 02-03).
   - [x] Story checkboxes and AC files updated to `[x]` (7 Phase-2 AC files fully checked).

   **Story-1 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific: apply modify/create/delete, per-file error isolation, atomic write, per-file backup, symlink-escape re-check — all green.
   **Story-2 DoD Complete** — Auto: 3/3 | Story-Specific: configurable argv runner, result capture (exit/stdout/stderr/duration/timeout/start-error/truncation), conservative `Passed()` gate, Go convenience default + hard refusal — all green.

### 2.8 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (`internal/autofix/apply.*`, `internal/verify/localvalidate.*`).

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does `ApplyPatch` return the `BackupMap` shape Story 3's `Revert` will consume? Does `ValidationResult` carry the trigger signal Story 3 needs?
       - CONFIG SURFACE: Validation command/timeout config keys documented, defaulted (~2min), back-compat?
       - INTEGRATION: Do `autofix` and `verify` couple only through their documented types, not internals?
       - PHASE-EXIT CONTRACT: Can Phase 3 (revert) and Phase 4 (GitHub) consume these outputs without rework?
       - REGRESSION: Existing `internal/verify` (syntaxguard) behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings + resolution (files changed: apply.go/apply_test.go, localvalidate.go/localvalidate_test.go, boundaries_test.go):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | MEDIUM | apply.go:129 | `BackupMap` empty-value sentinel overloaded across created / in-tree-symlink-target / already-gone-delete — Story 3 Revert would delete a symlinked target instead of restoring it. | Deferred → TD-005 (expanded), flagged as a **Phase 3 (Story 3 Revert) precondition** to disambiguate the sentinel. |
   | LOW | localvalidate.go:130 | No default validation-timeout constant/config key; a Phase-5 caller passing `0` gets an immediate false `TimedOut`. | Deferred → TD-008 (Phase 5 / Story 6 wiring owns the config surface). |

   **No CRITICAL/HIGH — phase gate passed. Green build/tests, boundaries intact, syntaxguard untouched. Happy-path contract (absolute-path BackupMap consumed by root-less Revert) confirmed sound. MEDIUM/LOW deferred to tech-debt-captured.md.**

   **Phase gate passed.**
   **Duration:** 15-30 min

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin Phase 3.

---

## Phase 3: Core Items (2 days)

**Goal:** Complete the local safety net (`internal/autofix/revert.go`) that makes the apply→validate pipeline trustworthy enough to gate remote mutation on.

### 3.1 [x] **[Automatic Revert on Validation Failure - RED](plan/user-stories/03-automatic-revert-on-validation-failure.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-backup-map-tracking.md), [03-02](plan/acceptance-criteria/03-02-restore-on-validation-failure.md), [03-03](plan/acceptance-criteria/03-03-cleanup-on-validation-success.md), [03-04](plan/acceptance-criteria/03-04-hard-error-on-restore-failure.md)
   Write failing tests for `Revert(backupMap BackupMap) error`: backup-map precondition/tracking as input contract from Story 1 (03-01); restore all touched files on validation failure **strictly before any `ghaction` call** — includes an integration-level sequencing test (03-02); cleanup backup files on validation success (03-03); hard error surfacing on restore failure, never silently continue (03-04). Verify fail correctly.
   **Files:** `internal/autofix/revert_test.go` | **Duration:** ~0.5 day

### 3.2 [x] **[Automatic Revert on Validation Failure - GREEN](plan/user-stories/03-automatic-revert-on-validation-failure.md)**
   Minimal `internal/autofix/revert.go` (build on `restorePriorBackup` semantics), one test at a time (T1), verify all (T2). COMMIT.
   COMMIT: `git add internal/autofix/revert.go internal/autofix/revert_test.go && git commit -m "feat(autofix): restore touched files on validation failure (green)"`
   **Files:** `internal/autofix/revert.go` | **Duration:** ~0.5 day

### 3.2.A [x] **[Automatic Revert - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-automatic-revert-on-validation-failure.md)**
   **Changed Files:** `internal/autofix/revert.go`, `internal/autofix/revert_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. No memory of the implementation in 3.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `internal/autofix/revert.go`, `internal/autofix/revert_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Can a crafted backup path restore outside the repo root? Backup file tampering?
       - EDGE CASES: Empty backup map, partially-applied batch, backup file missing at restore time, restore of a create (should delete) vs. a modify (should overwrite)?
       - ERROR HANDLING: Is a restore failure a HARD error (never silent)? Is restore guaranteed before ANY `ghaction` call? Cleanup only on success?
       - PERFORMANCE: Leaked backup files on the success path?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings + resolution (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | MEDIUM | revert.go:47-51 | Hard-error branch for a failed removal of a patch-created file (empty sentinel, non-`ErrNotExist`) untested through `RevertPatch` — AC 03-04's named-aggregate guarantee unverified for the create-deletion path. | FIXED in 3.3 — added `TestRevertPatch_CreatedFileRemovalFailureNamedError` (injects `removeFn` failure on a created target; asserts named error + remaining entries still processed). |
   | MEDIUM | revert_test.go | "Backup file missing at restore time" (checklist edge case) untested — a future change silently tolerating a missing `.bak` would go unnoticed. | FIXED in 3.3 — added `TestRevertPatch_MissingBackupIsHardError` (removes `.bak` out-of-band; asserts hard error names the file and the target is not silently left patched). |
   | LOW | revert.go | Restore copies content byte-for-byte but not file *mode* — an original 0755/0600 comes back 0644 (`copyFile` ignores perm on an existing `O_TRUNC` target). Content restored; mode regresses. | Deferred → TD-009. Out of AC scope (ACs specify byte content); TD-fix corpus is 0644 Go source. |
   | LOW | revert.go:41-72 | `RevertPatch` trusts every path in the (exported) `BackupMap` with no independent containment re-check; a hand-built/corrupted map could act outside root. | Deferred → TD-010. Map is apply-produced + `containedPath`-validated upstream; defense-in-depth only. |

   **No CRITICAL/HIGH — nothing blocking. Both MEDIUM findings are test-coverage gaps on this story's ACs, closed inline in 3.3 (strengthening). Both LOW deferred to tech-debt-captured.md. Adversarial review complete.**

### 3.3 [x] **[Automatic Revert - REFACTOR](plan/user-stories/03-automatic-revert-on-validation-failure.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any).
   2. Improve quality, maintain green (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(autofix): address review + clean up revert path"`
   **Duration:** ~0.5 day

### 3.4 [x] **Phase 3 DoD (Story 3)**
   - [x] Tests (T3): `go test ./internal/autofix/...` all passing (43 tests), incl. the restore-before-ghaction sequencing model tests (`TestRevertPatch_RemoteStepUnreachableOnValidationFailure` / `...ReachedOnlyAfterCleanupOnSuccess`). Full `go test ./...` green.
   - [x] Coverage ≥ 80% on `revert.go` — `RevertPatch` 93.8%, `CleanupBackups` 100%, package total 93.3%.
   - [x] `go vet ./...` clean; `golangci-lint run ./internal/autofix/...` 0 issues; `go build ./...` succeeds.
   - [x] Restore-failure is a hard error (logged at Warn AND returned as a named `errors.Join` aggregate); cleanup runs only on validation success and is best-effort (never fails the run).
   - [x] Story-3 checkboxes (3.1–3.3) and all 4 AC files (03-01…03-04) updated to `[x]`.

   **Story-3 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific: backup-map coverage matches writes, restore-on-failure (single/multi/partial/create-delete/delete-restore), all-errors-collected aggregate, cleanup-on-success + already-absent tolerance + best-effort, hard-error naming, revert-before-remote sequencing, TD-005 symlink-leaf sentinel disambiguation — all green.
   Manual Review: [x] Reviewed via 3.2.A fresh-subagent adversarial pass (2 MEDIUM closed inline as tests, 2 LOW → TD-009/010).

### 3.5 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`internal/autofix/revert.*`).

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the apply→validate→revert pipeline complete and self-consistent (Story 1 backup map ↔ Story 3 revert)?
       - CONFIG SURFACE: N/A new config, or documented if any.
       - INTEGRATION: Is revert provably sequenced before any GitHub-mutating call (the guarantee Phase 4 relies on)?
       - PHASE-EXIT CONTRACT: Can Phase 4 assume "local safety net is trustworthy" without rework?
       - REGRESSION: Phase 2 apply/validate behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate result: CLEAN — No findings.** Fresh-context integrator verified: every `BackupMap` value shape `ApplyPatch` emits has a correct `RevertPatch` route; `refuseSymlinkLeaf` runs on all apply paths before parse and closes the TD-005 sentinel ambiguity un-bypassably; `containedPath` + `refuseSymlinkLeaf` jointly cover directory-component and leaf symlink escapes; the apply.go change is additive (Phase 2 behavior intact); the `log` allowlist addition reflects a real, justified dependency; revert-before-remote sequencing is soundly modeled at the package boundary for Phase 4/5 to wire. `internal/autofix` and `internal` (boundaries) suites both green.

   **Phase gate passed.**
   **Duration:** 15-30 min

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin Phase 4.

---

## Phase 4: Advanced (4 days)

**Goal:** The cross-system GitHub orchestration — the highest-risk surface (`HAS_CROSS_SYSTEM=true`). Extend `ghaction.Client` with branch/commit/PR-create methods routed through existing `postDo`/`get` plumbing.

### 4.1 [x] **[Create Branch and Commit Verified Fix - RED](plan/user-stories/04-create-branch-and-commit-verified-fix.md)**
   **AC:** [04-01](plan/acceptance-criteria/04-01-create-branch-ref.md), [04-02](plan/acceptance-criteria/04-02-branch-collision-handling.md), [04-03](plan/acceptance-criteria/04-03-create-commit-multi-file-sequence.md), [04-04](plan/acceptance-criteria/04-04-validation-gated-call-site.md), [04-05](plan/acceptance-criteria/04-05-retry-backoff-redaction-reuse.md)
   Write failing tests (against `httptest.Server`, routing on method+path per Phase 1 spike): `CreateBranch` creates a ref at a base SHA (04-01); 422 ref-exists is distinguishable/recoverable (04-02); `CreateCommit` builds a multi-file commit via blob → tree → commit → ref-update (04-03); `CreateBranch`/`CreateCommit` unreachable without prior validation success — integration (04-04); new endpoints inherit retry/backoff/redaction from `postDo`/`get` (04-05). Verify fail correctly.
   **Files:** `internal/ghaction/client_test.go` | **Duration:** ~0.75 day

### 4.2 [x] **[Create Branch and Commit - GREEN](plan/user-stories/04-create-branch-and-commit-verified-fix.md)**
   Minimal additions to `internal/ghaction/client.go`: `CreateBranch` + `CreateCommit` using `CommitRequest{Branch, Message, ParentSHA, Files}`. Route all HTTP through existing `postDo`/`get` — no second client. One test at a time (T1), verify all (T2). COMMIT.
   COMMIT: `git add internal/ghaction/client.go internal/ghaction/client_test.go && git commit -m "feat(ghaction): CreateBranch + CreateCommit via Git Data API (green)"`
   **Files:** `internal/ghaction/client.go` | **Duration:** ~1.25 day

### 4.2.A [x] **[Create Branch and Commit - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-create-branch-and-commit-verified-fix.md)**
   **Changed Files:** `internal/ghaction/client.go`, `internal/ghaction/client_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 4.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `internal/ghaction/client.go`, `internal/ghaction/client_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Token leaked via error messages/logs? `redactSecrets` applied? Minimal scope assumed? Is a GitHub-mutating call reachable before validation success?
       - EDGE CASES: 422 ref-exists, base SHA missing, partial 4-call failure (orphaned blob/tree, stale/missing ref), empty file set?
       - ERROR HANDLING: Does a partial-failure surface a clear `APIError` naming the failed step? No silent orphaned-ref?
       - PERFORMANCE: Reuses existing retry/backoff (no custom throttling)? Sequential calls bounded?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context) + resolution — no CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | MEDIUM | client.go:196 | `CreateCommit`'s parent-commit read used `get`, which returned an untyped `fmt.Errorf` — asymmetric with `sendDo`; a missing base SHA (404) was not `errors.As`-inspectable. (Same issue Phase-1 flagged as TD-003.) | FIXED in 4.3 — `get` now returns `*APIError` on non-2xx (mirrors `sendDo`); `TestCreateCommitParentReadErrorIsTyped` locks it. Closes TD-003. |
   | LOW | client.go:182 | Only empty `Files` was guarded; empty `ParentSHA`/`Branch` let a real commit be created then orphaned by a bad ref PATCH. | FIXED in 4.3 — non-empty `ParentSHA`/`Branch` guards before any HTTP call; `TestCreateCommitRejectsEmptyParentOrBranch`. |
   | LOW | client.go:180 | Non-idempotent POST retried on 5xx/transport error can yield a spurious 422 collision on a lost-response first success. | Deferred → TD-011. Inherent to the reused `postDo`/`sendDo` plumbing (all mutating endpoints share it); collision policy is the caller's per AC 04-02. |

   **No CRITICAL/HIGH — nothing blocking. Both fixable findings closed inline in 4.3 (one closes the pre-existing TD-003); one LOW deferred to tech-debt-captured.md. Adversarial review complete.**

### 4.3 [x] **[Create Branch and Commit - REFACTOR](plan/user-stories/04-create-branch-and-commit-verified-fix.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any).
   2. Improve quality, maintain green (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(ghaction): address review + clean up branch/commit path"`
   **Duration:** ~0.5 day

### 4.4 [x] **[Open or Update Pull Request - RED](plan/user-stories/05-open-or-update-pull-request-via-github-api.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-create-pull-request.md), [05-02](plan/acceptance-criteria/05-02-existence-check-avoids-duplicate-prs.md), [05-03](plan/acceptance-criteria/05-03-update-pull-request.md), [05-04](plan/acceptance-criteria/05-04-retry-backoff-redaction-reuse.md)
   Write failing tests (against `httptest.Server`): `CreatePullRequest` opens a new PR from the auto-fix branch (05-01); existence check decides create-vs-update, avoids duplicate PRs — integration (05-02); `UpdatePullRequest` refreshes an existing open PR (05-03); PR endpoints reuse retry/backoff and **redact secrets from outbound PR title/body** (05-04). Verify fail correctly.
   **Files:** `internal/ghaction/client_test.go` | **Duration:** ~0.5 day

### 4.5 [x] **[Open or Update Pull Request - GREEN](plan/user-stories/05-open-or-update-pull-request-via-github-api.md)**
   Minimal additions to `internal/ghaction/client.go`: `CreatePullRequest` + `UpdatePullRequest` using `PullRequestRequest{Head, Base, Title, Body}`; existence-check routing. `redactSecrets` on outbound PR content. One test at a time (T1), verify all (T2). COMMIT.
   COMMIT: `git add internal/ghaction/client.go internal/ghaction/client_test.go && git commit -m "feat(ghaction): create/update PR with existence check + redaction (green)"`
   **Files:** `internal/ghaction/client.go` | **Duration:** ~0.5 day

### 4.5.A [x] **[Open or Update Pull Request - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-open-or-update-pull-request-via-github-api.md)**
   **Changed Files:** `internal/ghaction/client.go`, `internal/ghaction/client_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 4.5 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `internal/ghaction/client.go`, `internal/ghaction/client_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Is `redactSecrets` applied to the outbound PR title AND body (new outbound attack surface, AC 05-04)? Token in logs?
       - EDGE CASES: PR already exists for branch (must update, never duplicate), branch has no diff, closed-PR reuse, concurrent second run?
       - ERROR HANDLING: Does the existence check fail closed (no accidental duplicate on ambiguous response)?
       - PERFORMANCE: Reuses retry/backoff plumbing?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context) + resolution — no CRITICAL/HIGH:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | LOW | client.go:289 | `postDo`/`sendDo` ignore body-decode errors, so a 2xx whose body carried no `number` made `CreatePullRequest` return `(0, nil)`; an orchestrator could misread 0 as "not created" and open a duplicate PR — defeating the never-duplicate AC. | FIXED in 4.6 — `CreatePullRequest` now returns an error when a 2xx yields `Number == 0` (a real PR number is always ≥1); `TestCreatePullRequestZeroNumberIsError` locks it. |
   | LOW | client.go:315 | `findOpenPullRequest` scopes on `head` only, not the `(head, base)` pair; GitHub permits multiple open PRs from the same head to different bases, so the lowest-number tiebreak could pick a PR against an unintended base. | Deferred → TD-012. AC 05-02 explicitly specifies the `head + state=open` query and resolves multi-match by lowest number *by design*; adding `base` would deviate from the contract, and the real auto-fix flow always targets the single default base. |

   **No CRITICAL/HIGH — nothing blocking. LOW #1 (duplicate-PR correctness) closed inline in 4.6; LOW #2 deferred to tech-debt-captured.md as a by-design/AC-contract note. Adversarial review complete.**

### 4.6 [x] **[Open or Update Pull Request - REFACTOR](plan/user-stories/05-open-or-update-pull-request-via-github-api.md)**
   1. Fix CRITICAL/HIGH issues from 4.5.A (if any).
   2. Improve quality, maintain green (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(ghaction): address review + clean up PR path"`
   **Duration:** ~0.5 day

### 4.7 [x] **Phase 4 DoD (Stories 4 & 5)**
   - [x] Tests (T3): `go test ./internal/ghaction/...` all passing (full `go test ./...` green). Existence-check tests (`TestFindOpenPullRequest*`) live in `internal/ghaction`. **Validation-gated-call-site (AC 04-04)** is orchestrator-level: modeled at the package boundary in Phase 3 (`autoFixFlowModel` / `TestRevertPatch_RemoteStepUnreachableOnValidationFailure`), wired in Phase 5, proven end-to-end in Phase 6 — not a `ghaction`-package concern (these methods are pure API wrappers, per AC 04-04's own "the gate lives in the caller").
   - [x] Coverage ≥ 80% on new methods — package 90.7%; CreateBranch 100%, CreateCommit 97.3%, CreatePullRequest 100%, UpdatePullRequest 100%, findOpenPullRequest 92.9%, sendDo 88.6%.
   - [x] `go vet ./...` clean; `golangci-lint run ./internal/ghaction/` 0 issues; `go build ./...` succeeds.
   - [x] All new methods route through `postDo`/`get`/`sendDo` (grep-confirmed: only 2 `http.Client.Do` sites, both in the shared plumbing); `redactSecrets` on outbound PR title AND body verified (`TestCreatePullRequestRedactsOutboundTitleAndBody`, `TestUpdatePullRequestRedactsOutboundContent`) and on inbound error bodies for every new endpoint.
   - [x] AC files updated to `[x]`: 04-01/02/03/05 and 05-01/03/04 fully; 05-02 Client-half checked (lookup query, found/not-found, `get` retry reuse), its end-to-end create-vs-update item left for Phase 5/6; **04-04 left entirely open by design** (orchestrator-owned, Phase 5/6).

   **Story-4 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific: CreateBranch ref-create + normalization + 422 collision/invalid-SHA distinction + retry; CreateCommit get-commit→blob→tree→commit→ref sequence, base_tree resolution, multi-file atomicity, null-SHA deletion, empty/partial-failure short-circuit, step-named errors, 5xx/429 retry on POST & PATCH, token redaction — all green.
   **Story-5 DoD Complete** — Auto: 3/3 | Story-Specific: CreatePullRequest (number decode + zero-guard + 422 typed + 5xx retry), findOpenPullRequest (head:branch+state=open query, found/not-found, lowest-numbered multi-match, branch escaping, get retry), UpdatePullRequest (PATCH title/body, 404 typed, 429 retry, idempotent), outbound title+body redaction — all green.
   Manual Review: [x] Reviewed via 4.2.A + 4.5.A fresh-subagent adversarial passes (0 CRITICAL/HIGH; 1 MEDIUM + 2 LOW → 2 fixed inline closing TD-003, 2 LOW deferred TD-011/TD-012).

### 4.8 [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (`internal/ghaction/client.*`).

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Do `CommitRequest`/`PullRequestRequest` shapes match sprint-design.md? Return types stable for Story 6 wiring?
       - CONFIG SURFACE: Token/scope requirements documented? Repo slug validated via `parseRepo`?
       - INTEGRATION: All 4 methods share the single existing HTTP client + retry/redaction plumbing (no second client, no scattered auth)?
       - PHASE-EXIT CONTRACT: Can Phase 5 (`--auto-fix` wiring) call these without rework? Is "no GitHub call before validation" enforced at the seam?
       - REGRESSION: Existing `ghaction` comment-posting behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (first pass) + resolution — files changed: `internal/ghaction/client.{go,_test.go}`:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | HIGH | client.go:322 | `findOpenPullRequest` was unexported, but AC 05-02 places the create-vs-update orchestrator in `internal/autofix` (a different package) — the duplicate-PR existence check was unreachable at the Phase-5 seam. | FIXED before boundary — exported as `FindOpenPullRequest` (AC 05-02 permits "or equivalent exported lookup"); tests renamed. Phase 5 can now run the existence check without editing `client.go`. |
   | MEDIUM | client.go:424 | `sendDo`'s 2xx success-decode used an 8KB `LimitReader`; a PR/tree object exceeding 8KB truncates → `Number==0` false "not created" (the exact duplicate-PR failure the zero-guard prevents) or empty commit SHA. | FIXED before boundary — success-decode limit raised to 8MB (`8<<20`, matching `get`); `TestCreatePullRequestDecodesNumberInLargeBody` (16KB body) locks it. |
   | LOW | client.go:250 | `CreateCommit` did not guard an empty decoded blob/tree/commit SHA; a silent decode miss would PATCH the ref with an empty commit sha and surface a misleading error. | FIXED before boundary — empty-SHA guards after each blob/tree/commit decode; ref is never advanced on an empty commit sha; `TestCreateCommitEmptyCommitShaIsError` locks it. |
   | LOW | client.go:39 | Stale `Client` doc ("check runs and PR review comments" only); token scopes for the new mutating methods undocumented. | FIXED before boundary — `Client` doc updated to cover the mutating methods and note `contents: write` + `pull_requests: write` scope. |
   | LOW | sprint-design.md:196 | Architecture summary declared `Files []FileChange`; no such type — impl + AC 04-03 use `CommitFile`. | FIXED before boundary — sprint-design corrected to `Files []CommitFile` (+ `CommitFile` shape). |
   | LOW | client.go:245 | `CommitRequest.Message` sent verbatim (no outbound redaction), asymmetric with PR title/body. | Deferred → TD-013 (Phase-5 precondition — redact if the message is diagnostics-sourced; speculative to add now, no AC backing). |

   **Re-review (after fixes): CLEAN — "No findings."** Fresh-context integrator independently verified the export is cross-package callable, the 8MB decode limit, the three empty-SHA guards (ref unreachable on empty commit sha), documented token scopes, single-plumbing integration (no second client), and `comments.go` regression-free (posting uses `out=nil`). Green build/vet/lint/tests.

   **Phase gate passed.**
   **Duration:** 15-30 min

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin Phase 5.

---

## Phase 5: Integration (2 days)

**Goal:** Wire Stories 1-5 into a single gated entry point in `cmd/atcr`. Default behavior must remain byte-identical when `--auto-fix` is absent.

### 5.1 [x] **[Opt In via --auto-fix with Refuse-Without-Backend Gate - RED](plan/user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-auto-fix-off-by-default.md), [06-02](plan/acceptance-criteria/06-02-single-gate-refuses-on-any-missing-piece.md), [06-03](plan/acceptance-criteria/06-03-gate-passes-silently-when-fully-configured.md)
   Write failing tests (unit + integration via in-process `cobra.Command`, mirroring `cmd/atcr/verify_test.go`'s refuse-without-sandbox pattern): `--auto-fix` off by default, zero behavior change when absent (06-01); single gate refuses the ENTIRE run on any one missing/malformed backend piece — apply target, validation command, GitHub credentials — tested **independently and in combination** (06-02); gate passes silently when fully configured, no overhead into Story 1 (06-03). Verify fail correctly.
   **Files:** `cmd/atcr/autofix_test.go` | **Duration:** ~0.5 day

### 5.2 [x] **[Opt In via --auto-fix - GREEN](plan/user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)**
   Minimal `cmd/atcr` wiring: `--auto-fix` flag (default off) + `validateAutoFixBackend` all-or-nothing gate (mirror the `--exec` gate). On pass, orchestrate apply → validate → revert-or-continue → branch/commit → PR. One test at a time (T1), verify all (T2). COMMIT.
   COMMIT: `git add cmd/atcr/autofix.go cmd/atcr/flags.go cmd/atcr/autofix_test.go && git commit -m "feat(atcr): --auto-fix opt-in flag with refuse-without-backend gate (green)"`
   **Files:** `cmd/atcr/autofix.go`, `cmd/atcr/flags.go` | **Duration:** ~0.75 day

### 5.2.A [x] **[Opt In via --auto-fix - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)**
   **Changed Files:** `cmd/atcr/autofix.go`, `cmd/atcr/flags.go`, `cmd/atcr/autofix_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the implementation in 5.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `cmd/atcr/autofix.go`, `cmd/atcr/flags.go`, `cmd/atcr/autofix_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Does the gate fail CLOSED on any partial/misconfigured backend (never fail-open)? Token presence validated before any GitHub call?
       - EDGE CASES: Exactly one of {apply target, validation command, GitHub creds} missing/malformed — independently AND in combination? Flag absent = zero new code path?
       - ERROR HANDLING: All-or-nothing refusal (never apply+validate locally then silently skip PR)? Clear operator-facing error + non-zero exit at each gate?
       - PERFORMANCE: Any overhead added to the default (flag-absent) path?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context) + resolution:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | HIGH | autofix.go orchestrateAutoFix/runAutoFix | PR `Base` was hard-coded empty (`Base: ""`); GitHub 422s an empty base, so a live run would push an orphan branch + commit via `CreateBranch`/`CreateCommit` and then fail at `CreatePullRequest` — the headline deliverable broken and the remote littered. Unit tests missed it because `fakeGitHub` ignores `Base`. | FIXED in 5.3 — added `autoFixRun.Base`, threaded `res.DefaultBranch` (gitrange) through `orchestrateAutoFix`; `runAutoFix` guards an empty base **after** clean validation but **before** any GitHub call (no orphan branch possible); `orchestrateAutoFix` refuses an empty base up front. New regression test `TestRunAutoFix_EmptyBaseRefusesBeforeGitHub` asserts zero GitHub calls on empty base; happy-path test now sets `Base`. |
   | LOW | autofix.go:~150 | `--api-url` is resolved but not shape-checked in the gate; a malformed/insecure value passes the all-or-nothing gate and fails only at first HTTP call (after apply+validate+cleanup) — no GitHub mutation (parse fails pre-HTTP), but violates the gate's "refuse before any file is touched" contract. | Deferred → TD-014. Fails closed (no remote mutation); the tree is left validated. |
   | LOW | autofix.go orchestrateAutoFix | Branch name is always a unique UTC timestamp, so the live create-vs-update path (AC 05-02) is unreachable in production — every re-run opens a fresh PR/branch rather than updating one. The update path IS implemented + unit-tested in `runAutoFix`; only the live adapter always creates. | Deferred → TD-015. Create-per-run is acceptable MVP behavior; deterministic-branch/one-stable-PR is a refinement. |

   **HIGH resolved inline in 5.3; both LOW deferred to tech-debt-captured.md. Sequencing invariant (no GitHub call before `Passed()`) independently confirmed CLEAN by the reviewer across every apply/validate/timeout/revert path. Adversarial review complete.**

### 5.3 [x] **[Opt In via --auto-fix - REFACTOR](plan/user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any).
   2. Improve quality, maintain green (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(atcr): address review + clean up auto-fix gate"`
   **Duration:** ~0.5 day

### 5.4 [x] **Phase 5 DoD (Story 6)**
   - [x] Tests (T3): `go test ./...` all passing (37 packages green), incl. off-by-default (`TestReviewCmd_AutoFixFlagExists`) + per-piece refusal (`TestValidateAutoFixBackend_Refusals` table) + no-network + no-mutation gate tests.
   - [x] Coverage ≥ 80% on new wiring — `cmd/atcr` 82.1%, `internal/registry` 90.2%; the gate `validateAutoFixBackend` 100%, config `AutoFixConfig.Validate` 100%, `addAutoFixFlags` 100%, `selectAutoFixEntries` 90.9%. The below-bar functions (`runAutoFix` live-PR branch 59%, `orchestrateAutoFix` git/PR path 52.9%, `commitFilesFrom` 63.6%) are exactly the GitHub-stub end-to-end surface assigned to Phase 6 by the sprint's shape-(a) decision; their sequencing branches (apply-fail/validation-fail → revert, zero GitHub) ARE covered.
   - [x] `go vet ./...` clean; `golangci-lint run` 0 issues; `go build ./...` succeeds; `gofmt` clean.
   - [x] Flag-absent path proven byte-identical — every new code path is guarded by `if autoFix`; the full pre-existing `cmd/atcr` review/verify/reconcile suite passes unchanged, and `--auto-fix` defaults to `false`.
   - [x] Story-6 checkbox (5.1–5.3) and all 3 AC files (06-01/02/03) updated to `[x]`.

   **Story-6 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific: `--auto-fix` off-by-default + help-discoverable + absent from other commands; single all-or-nothing gate refusing on each of apply-target / validation-command / GitHub-token / malformed-repo independently AND aggregated; no live network/exec in the gate; no filesystem mutation on refusal; fully-configured pass with flag>env precedence, resolved-timeout default (TD-008) + configured override; `runAutoFix` sequencing (no GitHub call before `Passed()`, revert on apply/validate failure), empty-base guard before any GitHub call (5.2.A HIGH) — all green.
   Manual Review: [ ] Code reviewed (pending `/execute-code-review`) — adversarial 5.2.A fresh-subagent pass done (1 HIGH fixed inline, 2 LOW → TD-014/TD-015).

### 5.5 [x] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 (`cmd/atcr/autofix.*`, `cmd/atcr/flags.go`).

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the full apply→validate→revert-or-continue→branch/commit→PR flow wired in the correct order?
       - CONFIG SURFACE: `--auto-fix` flag + backend config keys documented, defaulted off, back-compat?
       - INTEGRATION: Does the orchestration call Stories 1-5 only through their public interfaces? No GitHub mutation before validation success at the wired seam?
       - PHASE-EXIT CONTRACT: Can Phase 6 exercise the end-to-end flow against stubs without rework?
       - REGRESSION: All existing `cmd/atcr` subcommands unaffected when `--auto-fix` absent?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (fresh-context integrator) + resolution — no CRITICAL/HIGH:**
   Verified: git-confirmed Phase-5 commits touched only the six declared files (no illegal edit of internal/autofix/verify/ghaction); apply→validate→revert-or-continue→cleanup→base-guard→branch/commit/PR order correct; no GitHub mutation reachable before `Passed()`; seam is Phase-6-stubbable (api-url override + `resolveHeadSHAFn` var + `autoFixGitHub` interface); `|| autoFix` trigger and terminal return leave every non-auto-fix path byte-identical.

   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----------|
   | MEDIUM | autofix.go selectAutoFixEntries | Duplicate target paths across findings were never deduped: two findings touching one file → two `FileEntry` for one path → `ApplyPatch` re-runs `BackupToDotBak` on the already-patched file, clobbering the original backup so a later `RevertPatch` restores once-patched content — breaking the sprint's central revert-safety invariant. | **FIXED at gate** — `selectAutoFixEntries` now dedupes by `Path` (one fix per file per run), preserving one-backup-per-file. Regression test `TestSelectAutoFixEntries_DedupesByPath`. |
   | MEDIUM | autofix.go selectAutoFixEntries | Auto-fix scope silently coupled to the `--fail-on` threshold (config `fail_on` defaults HIGH in `atcr init`), so a stock project drops MEDIUM/LOW fixes with a misleading "nothing to apply". | Deferred → TD-016. Matches the user-approved selection policy; fail-safe (fewer fixes). UX/doc gap. |
   | MEDIUM | registry (DefaultProjectConfigYAML) | `auto_fix` keys not emitted in the `atcr init` template (documented only via doc-comments + flag help). | Deferred → TD-017. All keys optional with working defaults. |
   | LOW | review.go terminal return | `--auto-fix` bypasses the `--fail-on` CI exit gate (deliberate, undocumented in help). | Deferred → TD-018. Intentional design. |
   | LOW | autofix.go validateAutoFixBackend | `applyTarget` stored relative when `repoRoot="."` (works because CWD==repo root). | Deferred → TD-019. Correct in the only call path. |

   **No CRITICAL/HIGH — phase gate passed. The one revert-safety-relevant MEDIUM fixed at the gate; remaining MEDIUM/LOW deferred to tech-debt-captured.md (TD-016…TD-019).**

   **Phase gate passed.**
   **Duration:** 15-30 min

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin Phase 6.

---

## Phase 6: Testing (1 day)

**Goal:** Prove the sequencing guarantees (no GitHub mutation before local validation passes) hold end-to-end, not just per-story.

### 6.1 [ ] **Cross-story integration test: full auto-fix flow against stubs**
   **Task:** Write an integration test (`//go:build integration`) exercising apply → validate → revert-or-continue → branch/commit → PR against an `httptest.Server` GitHub stub, driven through the `cmd/atcr` `--auto-fix` entry point. Cover both branches: validation-pass (proceeds to branch/commit/PR) and validation-fail (reverts, zero GitHub calls).
   **Success Criteria:** Full happy-path produces a PR against the stub; failure-path restores files and makes zero HTTP calls.
   **Files:** `cmd/atcr/autofix_integration_test.go` | **Duration:** ~0.5 day

### 6.2 [ ] **Zero-HTTP-calls-on-validation-failure regression test**
   **Task:** Independent cross-check asserting that when validation fails, the GitHub stub receives **zero** requests (guard against a false-green from `httptest` mis-routing). Assert revert ran and a clear operator error surfaced.
   **Success Criteria:** Stub request counter == 0 on the validation-failure path; test fails loudly if any GitHub-mutating call fires pre-validation.
   **Files:** `cmd/atcr/autofix_integration_test.go` | **Duration:** ~0.25 day

### 6.3 [ ] **Phase 6 DoD**
   - [ ] Tests (T3): `go test -tags integration ./...` all passing.
   - [ ] Both sequencing branches (pass/fail) covered end-to-end.
   - [ ] Zero-HTTP regression test in place and green.
   - [ ] `go vet` / lint / build clean.
   - DoD Report per template.

### 6.4 [ ] **Phase 6 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 6 (integration tests).

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 6 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 6 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Do the integration tests actually assert the sequencing guarantee, or could they false-green (stub mis-routing)?
       - CONFIG SURFACE: Build tag `//go:build integration` correct so CI runs them intentionally?
       - INTEGRATION: Is the stub routing on method+path (not order-dependent)?
       - PHASE-EXIT CONTRACT: Does Phase 7 have everything needed for the 23-AC DoD verification?
       - REGRESSION: Do integration tests leave any temp files/branches behind?
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

> **GATED STOP** — `/execute-sprint` halts here. Resume to begin the Final Phase.

---

## Final Phase: Validation (Phase 7)

**Goal:** Definition of Done verification across all 23 ACs and the full quality gate before `/execute-code-review`.

### 7.1 [ ] **Full DoD verification across 23 ACs**
   Walk every AC file in `plan/acceptance-criteria/` (23 total) and confirm each is satisfied and checked `[x]`. Confirm each maps to a passing test.

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...` and `go test -tags integration ./...`
- [ ] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥ 80% baseline
- [ ] Lint/format clean: `golangci-lint run`, `go fmt ./...` (no diff), `goimports`
- [ ] Vet clean: `go vet ./...`
- [ ] Build succeeds: `go build ./...`
- [ ] All 6 stories and 23 ACs checked `[x]`
- [ ] `tech-debt-captured.md` reviewed; deferred MEDIUM/LOW findings recorded

### Optional: Targeted Mutation Testing
MUTATION_TOOL = **UNAVAILABLE** (no `stryker`/`mutmut`/`cargo-mutants` for this Go project). Skip — no mutation step. If a Go mutation tool is added later, target only the highest-risk changed files (`internal/autofix/apply.go`, `internal/autofix/revert.go`), never the full codebase.
**WARNING:** Do NOT run full-codebase mutation — it can take hours.

### Drift Analysis
Compare the delivered surface against [original-requirements.md](plan/original-requirements.md):
- AC1 (robust diff parse) — reused via `internal/payload` `BuildEntriesFromDiff`; confirm no rebuild.
- AC2 (safe apply) — Story 1. AC3 (configurable validation) — Story 2. AC4 (auto-revert) — Story 3.
- AC5 (GitHub branch/commit/PR) — Stories 4-5. AC6 (`--auto-fix` opt-in) — Story 6.
- Success criteria: ≥70% simple-TD auto-fix; zero broken builds in corpus.
- Out of scope confirmed NOT built: complex merge-conflict resolution; architectural/cross-repo fixes.
- Flag any task that drifted beyond the original request; if found, STOP and reconcile.

### 7.LAST [ ] **Final GATE: Sprint Exit Review (subagent)**
   **Scope:** Full sprint diff (all files changed across Phases 1-7).

   **Spawn a fresh subagent** via the Agent tool for a final hostile integration review across the whole sprint. No memory of the implementation — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Final sprint gate review`
   - prompt: Self-contained brief including:
     - Full changed-file list (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - SEQUENCING: Is it provably impossible to reach a GitHub-mutating call before local validation passes, across every entry path?
       - SECURITY: Token redaction on all outbound content; symlink-escape guard preserved; command-injection surface closed?
       - CROSS-SYSTEM ROLLBACK: Is the known remote-rollback gap (pushed branch/PR can't be locally reverted) surfaced clearly to the operator, per sprint-design Risks?
       - REGRESSION: Default (`--auto-fix` absent) behavior byte-identical?
       - COVERAGE: All 23 ACs satisfied and tested?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before sprint completion, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Final gate passed" — sprint ready for /execute-code-review

> **GATED STOP** — Sprint complete. Next: `/execute-code-review`.
