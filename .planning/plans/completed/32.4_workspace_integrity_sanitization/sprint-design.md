# Sprint Design: Plan 32.4: Workspace Integrity & Indirect Sandbox Escape Prevention

**Created:** July 21, 2026
**Plan:** [Plan 32.4: Workspace Integrity & Indirect Sandbox Escape Prevention](.)
**Plan Type:** Technical Debt (🔧)
**Status:** Design Complete

---

## Original User Request

> Harden ATCR against **Indirect Sandbox Escape** attacks (as disclosed in recent AI coding agent vulnerabilities affecting Cursor, Codex CLI, and Gemini CLI), where a contained sandbox execution is bypassed by modifying host configuration files (`.git/config`, `.githooks/`, `.github/workflows/`, `.vscode/`) that execute on the developer's host machine post-review. Build a strict path-protection guard blocking `--auto-fix` writes to critical host-execution paths, harden all host git subprocess invocations against poisoned config hijacking, and surface executable-bit/build-script changes as a non-blocking PR-body review warning.

**Referenced Resources:** None — this plan was routed through `/init-plan` from `.planning/epics/active/32.4_workspace_integrity_sanitization.md` (a self-contained epic plan, not an external doc). The epic underwent a deep `/refine-epic` pass on 2026-07-21 that retargeted T2's integration point, scoped T3's `internal/gitexec` package into existence, dropped an unimplementable "Skeptic" AC and later folded a corrected non-blocking version back in as T6, and recounted `COMPONENTS_TOUCHED` from a stated 4 to a derived 10 — all captured in `original-requirements.md`'s Refinements section.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Workspace Integrity Hardening
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 9 days
**Phases:** 5
**Pattern:** Foundation → Integration → CLI & Docs → Non-Blocking Review → Testing & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
path traversal blocklist validation Go
git subprocess environment hardening wrapper
CLI flag fail-closed security gate pattern
executable bit change diff detection review
atomic patch apply choke point security check
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - Two genuinely new packages (`internal/security`, `internal/gitexec`) are introduced, but both closely follow already-established codebase precedents (`internal/validation/validation.go`'s denylist matcher, `internal/autofix/apply.go`'s package-var testability pattern) rather than inventing novel architecture — "New patterns," not "Major overhaul."
- **Integration:** 3/3 - Touches 8 distinct packages/surfaces (`internal/autofix`, `cmd/atcr`, `internal/fanout`, `internal/gitrange`, `internal/payload`, `internal/personas` ×2 call sites, `internal/stream`, `docs/`) across a single-choke-point write gate, a six-site git subprocess migration, a CLI flag, and a PR-body content change — "Complex multi-system."
- **Story/Task & Test:** 3/3 - 6 tasks, each with extensive dedicated test strategies: per-blocklist-category table-driven tests, symlink-traversal fixtures, a whole-repo AC4 regression grep/AST scan, and PR-body warning assertions spanning 3 test files — "3+ extensive."
- **Risk/Unknowns:** 1/3 - The `/refine-epic` pass already resolved the major ambiguities (wrong integration point corrected, unimplementable AC dropped/replaced, exact line numbers cited for all six call sites, dependency order settled). Remaining risk is narrow and already mitigated in-plan (symlink OS variance, one-missed-call-site risk closed by AC4's binary regression test) — "Minor unknowns."

**Time Formula:** Σ(task effort: S=1d, M=2.5d) across 6 tasks, adjusted for cross-task integration overhead
**Calculation:** T1(S=1) + T2(S=1) + T3(M=2.5) + T4(S=1) + T5(M=2) + T6(M=2) = 9.5d ≈ **9 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** standard (not strong)
**Suggested command:** `/create-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

This sprint crosses both the adversarial (9 >= 6) and gated (9 >= 8, 5 phases, 9 days > 5) thresholds — security-hardening work with a fail-closed default posture and a hard AC4 regression requirement (zero remaining bare `exec.Command("git",...)` call sites) benefits from per-phase validation gates rather than a single end-of-sprint review.

---

## Phase Structure

### Phase 1: Foundation (2.5 days)
**Items:** T1 (`internal/security/pathguard.go` — `IsProtectedPath`), T3 (`internal/gitexec` package + six-site migration)
**Focus:** Build both foundational, mutually-independent primitives in parallel. T1 has zero dependencies; T3 is explicitly independent of T1/T2 per the plan's Risk Mitigation notes ("Confusing this task's scope with T1/T2's path-blocklist work"). Landing both first unblocks every downstream task.

### Phase 2: Integration (1 day)
**Items:** T2 (wire `IsProtectedPath` into `applyOne`)
**Focus:** Wire the T1 gate into the single host-repo write choke point in `internal/autofix/apply.go`, immediately after `containedPath` and before `refuseSymlinkLeaf`/the delete-modify-create branches. Depends on T1 only; can start as soon as Phase 1's T1 half lands, independent of T3's completion.

### Phase 3: CLI & Docs (1 day)
**Items:** T4 (`--allow-config-edits` flag + `docs/security.md`)
**Focus:** Land the operator escape valve and its mandatory warning, plus the security architecture doc and its `docs/README.md` index entry. Depends on T2's `AllowConfigEdits` threading target existing.

### Phase 4: Non-Blocking Review Flags (2 days)
**Items:** T6 (`FlagsForReview` executable-bit/build-script PR warnings)
**Focus:** Extend `pathguard.go` with the advisory-only check, thread `[]ReviewFlag` out of `ApplyPatch` via an out-parameter (no signature churn on `applyOne`'s dozen existing returns), and append the `## Review Warnings` PR-body section in `runAutoFix`. Depends on T1 (pathguard exists) and T2 (the `applyOne` choke point and `f.OldMode`/`f.NewMode` availability post-`gitdiff.Parse` are already wired).

### Phase 5: Testing & Validation (2.5 days)
**Items:** T5 (`pathguard_test.go`, `gitexec_test.go`, AC4 regression test)
**Focus:** Table-driven coverage for every blocklist category (canonical/relative/traversal/symlink forms per AC3), gitexec env/flag assertions (AC2), and the binary whole-tree AC4 regression test (both the negative "zero stray bare git exec" and positive "all six sites reference gitexec" assertions). This phase also serves as the sprint's overall Definition-of-Done validation gate — `go build ./...`, `go vet ./...`, `gofmt -l`, and `go test ./...` all green before sprint completion.

---

## Work Decomposition

### T1: Build `internal/security/pathguard.go` Protected-Path Blocklist
- **Testable elements:** `IsProtectedPath(path string) bool` — pure function, no I/O beyond conditional `EvalSymlinks`.
- **RGR plan:** RED — table-driven tests per blocklist category (`.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`/CI defs, `.vscode/`, `.idea/`, `.env*`, `.planning/`, `.atcr`) covering exact/nested/canonical/relative/traversal/symlink forms, all failing against a stub. GREEN — implement `filepath.Clean` + boundary-safe prefix matching + symlink resolution with existing-ancestor fallback. REFACTOR — align doc comments/style with `internal/validation/validation.go`.
- **Test types:** Unit (table-driven, `internal/security/pathguard_test.go`).
- **AC link:** AC1 (blocklist coverage), AC3 (path-format matching).

### T2: Wire pathguard into `internal/autofix/apply.go`'s `applyOne`
- **Testable elements:** `ApplyPatch`/`applyOne` with new `allowConfigEdits bool` parameter; refusal behavior for create/modify/delete entries.
- **RGR plan:** RED — tests asserting refusal for a protected create/modify/delete entry when `allowConfigEdits=false`, success when `true`, and no behavior change for non-protected paths. GREEN — insert the `IsProtectedPath` gate between `containedPath` and `refuseSymlinkLeaf`; thread the bool parameter through the one call site in `cmd/atcr/autofix.go:365` (defaulted `false` until T4 lands). REFACTOR — update `ApplyPatch`'s doc comment.
- **Test types:** Unit (`internal/autofix/apply_test.go`); ordering assertion (protected-path error fires before parse/backup/write).
- **AC link:** AC1.

### T3: Build `internal/gitexec` and Migrate All Six Host Git Call Sites
- **Testable elements:** `CommandFn`/`CommandContextFn` package vars; six migrated call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go` ×2, `internal/stream/fileindex.go`).
- **RGR plan:** RED — `gitexec_test.go` asserting `GIT_CONFIG_NOSYSTEM=1`/`GIT_CONFIG_GLOBAL=/dev/null` present in `cmd.Env`; per-call-site existing tests re-run against the swapped constructor. GREEN — implement `hardenEnv` additive-append helper; migrate all six sites one at a time, adding `--no-ext-diff` to the two diff-family invocations. REFACTOR — remove now-unused `os/exec` imports where fully replaced.
- **Test types:** Unit (`internal/gitexec/gitexec_test.go`); integration (poisoned-config regression, existing per-package git-operation suites against a real temp repo).
- **AC link:** AC2, AC4.

### T4: Add `--allow-config-edits` Flag and Document Security Architecture
- **Testable elements:** Flag registration/default; `warnAllowConfigEdits` stderr output; `docs/security.md` + `docs/README.md` index entry.
- **RGR plan:** RED — tests for flag-absent-defaults-false, flag-true-prints-warning-and-sets-field. GREEN — register the flag in `addAutoFixFlags` mirroring `--no-sandbox`; add `warnAllowConfigEdits`; thread the resolved bool onto `autoFixBackend`. REFACTOR — none required beyond doc-comment consistency.
- **Test types:** Unit (`cmd/atcr/autofix_test.go`).
- **AC link:** AC1 (bypass mechanism), documentation completeness.

### T5: Unit Tests for pathguard + gitexec, and Six-Site Migration Regression Coverage
- **Testable elements:** Full `pathguard_test.go` and `gitexec_test.go` suites; the AC4 whole-tree regression test (negative + positive assertions).
- **RGR plan:** This task is itself primarily test-authorship against already-landed T1/T3 implementations — RED/GREEN collapse into "write comprehensive coverage against confirmed symbols" (Step 1 mandates reading the finished T1/T3 code before writing assertions, not guessing signatures). REFACTOR — consolidate any duplicated fixture setup.
- **Test types:** Unit + whole-tree integration-style regression scan.
- **AC link:** AC3, AC4 (formal verification of both).

### T6: Non-Blocking `FlagsForReview` — Executable-Bit and Build-Script PR Warnings
- **Testable elements:** `FlagsForReview(path string, oldMode, newMode int) (bool, string)`; `ReviewFlag` collection via out-parameter; PR-body `## Review Warnings` section.
- **RGR plan:** RED — table-driven cases for executable-bit add/remove/create-diff, build-script matches/near-misses, combined case. GREEN — implement the check reusing T1's boundary-matching helper; thread `flags *[]ReviewFlag` through `applyOne` (append-only, no signature churn on existing returns); build the PR-body section in `runAutoFix` before `CreatePullRequest`. REFACTOR — confirm flagged-but-failed entries are excluded (only append on `applyOne` success).
- **Test types:** Unit (`internal/security/pathguard_test.go` extension); integration (`internal/autofix/apply_test.go`, `cmd/atcr/autofix_test.go` PR-body assembly).
- **AC link:** AC5.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `_test.go` files per Go package convention (no separate test tree).

**Test File Placement Examples:**
- `internal/security/pathguard_test.go` (new, T1 + T6 extension)
- `internal/gitexec/gitexec_test.go` (new, T3 + T5 regression)
- `internal/autofix/apply_test.go` (extended, T2 + T6)
- `cmd/atcr/autofix_test.go` (extended, T4 + T6)

**Unit/Integration/E2E:**
- **Unit:** Table-driven per-package tests using the codebase's existing `testify/assert`+`require` convention (per `internal/autofix/apply_test.go`'s style) for `IsProtectedPath`, `FlagsForReview`, `CommandFn`/`CommandContextFn` env assertions, and CLI flag defaults.
- **Integration:** The AC4 whole-tree regression scan (excludes `internal/gitexec/`, `internal/verify/localvalidate.go`, `internal/sandbox/docker.go`) is the sprint's primary integration-style test — it spans all six migrated call sites in one assertion. `ApplyPatch` multi-entry batch tests (mixed protected/clean/flagged entries) and `runAutoFix` PR-body construction tests round out integration coverage.
- **E2E:** None required — this is backend CLI/security-gate hardening with no UI surface; existing `--auto-fix` end-to-end flow tests (if present) are re-run unmodified post-migration as a regression check, not a new E2E suite.

**Test Environment Status:**
- Framework: `go test` (stdlib), no new test framework introduced
- Execution: `go test ./...` (existing project command, unchanged)
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (existing project command, 80% baseline)

---

## Architecture

**Primitives:**
- Repo-relative protected path (`string`) evaluated by `IsProtectedPath`
- `ReviewFlag{Path, Reason string}` — advisory record, additive to `BackupMap`
- Hardened `*exec.Cmd` — every host git subprocess constructed through `gitexec.CommandFn`/`CommandContextFn`

**Module Boundaries:**
- `internal/security` — exports `IsProtectedPath(path string) bool` and `FlagsForReview(path string, oldMode, newMode int) (bool, string)`; pure functions, no I/O side effects beyond a conditional `filepath.EvalSymlinks` read. No knowledge of `--auto-fix`, CLI flags, or PR construction.
- `internal/gitexec` — exports `CommandFn`/`CommandContextFn` as swappable package vars; sole sanctioned constructor for git subprocesses anywhere in the codebase (AC4's invariant).
- `internal/autofix` — `applyOne`/`ApplyPatch` own the single choke point where both `internal/security` checks attach; owns `AllowConfigEdits` threading and `[]ReviewFlag` collection.
- `cmd/atcr` — CLI flag registration/warning (T4), PR-body warning-section assembly (T6); the only consumer of `internal/autofix`'s new return values.

**External Dependencies:** `go-gitdiff` (already vendored, `File.OldMode`/`NewMode` feed T6 directly — no new parsing step); stdlib `os/exec`, `path/filepath`, `strings`. No new third-party packages.

**Replaceability:** `internal/security` and `internal/gitexec` are each independently swappable behind their exported function/var surface — no caller reaches into either package's internals, matching the codebase's existing black-box convention (`internal/validation`, `internal/atomicfs`).

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| `internal/security/pathguard.go` `IsProtectedPath` | Patch write-path gating (T1) | Symlink traversal into a protected dir, `../` traversal, case-sensitivity bypass on case-insensitive filesystems, trailing-slash boundary tricks (`.gitignore` vs `.git/`) | `filepath.Clean` normalization, `filepath.EvalSymlinks` with existing-ancestor fallback for not-yet-created paths, boundary-safe prefix matching (never bare `strings.HasPrefix`), explicit documented case-sensitivity behavior with tests |
| `internal/autofix/apply.go` `applyOne` gate ordering | Fail-closed enforcement before any write (T2) | Gate placed after a write instead of before; `--allow-config-edits` becoming a habitual bypass | Gate inserted between `containedPath` and `refuseSymlinkLeaf`/delete-modify-create branches — verified by reading the diff, not just test pass/fail; flag off-by-default with mandatory non-memoized stderr warning |
| `internal/gitexec` git subprocess wrapper | All host git invocations across 6 call sites (T3) | Poisoned `.git/config`/system/global config injecting malicious `core.pager`, `diff.external`, alias, or `credential.helper` entries | `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` on every invocation (additive over `cmd.Environ()`); `--no-ext-diff` on both diff-family invocations (`payload/diff.go`, `personas/submit.go`'s `gitHasStagedChanges`) |
| `--allow-config-edits` CLI flag | Operator escape valve (T4) | Habitual bypass quietly defeating protection; operators unaware of implications | Off by default; unconditional (non-memoized) stderr warning on every use, mirroring `--no-sandbox`; documented explicitly in `docs/security.md` |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `IsProtectedPath`/`FlagsForReview` per-entry check inside `applyOne`'s loop | One call per patch file entry (typically well under 100 files per `--auto-fix` PR) | Negligible overhead added to existing apply loop | Pure string matching; `EvalSymlinks` only invoked when the path exists on disk, falls back to lexical clean otherwise |
| AC4 whole-tree regression scan (`internal/gitexec/gitexec_test.go` or sibling) | Full repo `.go` file walk on every `go test ./...` run | Sub-second scan, no measurable CI slowdown | Regex or `go/ast` walk excluding `vendor/`, `.git/`, and confirmed out-of-scope files; runs once per test invocation, not per-file in the app itself |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Path normalization | `./x`, bare `x`, `../` traversal into protected dir, absolute path, mixed-case on case-insensitive FS, trailing slash, symlink whose target resolves into a protected dir, not-yet-existing path | Consistently resolves to the documented protected/not-protected verdict; symlink resolution falls back to deepest-existing-ancestor for not-yet-created files |
| Executable-bit / build-script overlap | Create-diff with `oldMode=0` and executable `newMode`; a path both build-script-matched and executable-bit-changed; a path already on T1's blocklist reaching `FlagsForReview` only via `--allow-config-edits` | `FlagsForReview` only ever evaluates a path that already passed T2's gate; combined-condition reason names both triggers; never blocks |
| Git call-site behavioral parity post-migration | `LC_ALL=C`/`LANG=C` additive appends in `gitrange`/`payload`, `-c credential.helper=...` prefix in `personas/submit.go`, exit-code-1-is-true semantics in `gitHasStagedChanges` | Byte-identical pre-existing behavior preserved; hardened env vars are additive, never replace existing customizations |
| Partial apply failure with a flagged entry | An entry that would be flagged for review fails mid-write (e.g. via `writeFileAtomicFn` test-failure indirection) | Failed entry excluded from the returned `[]ReviewFlag` — no misleading PR warning for a change that never actually landed |

### Defensive Measures Required

- **Input Validation:** `filepath.Clean` + conditional `EvalSymlinks` normalization before every blocklist comparison; boundary-safe segment matching (exact match or `prefix + "/"`), never bare substring matching.
- **Error Handling:** `IsProtectedPath` rejection returns a wrapped (`%w`) sentinel/typed error so callers can `errors.Is`; existing per-entry error-isolation contract in `ApplyPatch` (one rejected entry never blocks siblings) preserved unchanged for the new rejection path.
- **Logging/Audit:** Non-memoized stderr warning fires on every `--allow-config-edits` use (no `sync.Once` dedup); generated PR body carries a `## Review Warnings` section naming every flagged path and its reason when `FlagsForReview` triggers.
- **Rate Limiting:** Not applicable — no network-facing or high-frequency surface is introduced.
- **Graceful Degradation:** `FlagsForReview` never errors or panics (purely advisory contract); environments where `EvalSymlinks` fails (path doesn't exist yet, restricted permissions) fall back to the lexically-cleaned path rather than crashing.

---

## Risks

**Technical:**
- Risk: A missed call site during T3's six-site migration silently reopens the exact subprocess-hijack gap this epic exists to close. → Mitigation: T5's AC4 regression test is a binary, CI-enforced gate (zero remaining bare `exec.Command("git",...)` outside `internal/gitexec`), not a judgment call.
- Risk: `internal/gitexec`'s hardened environment breaks an existing test that implicitly relies on the developer's global git config (e.g. `user.name`/`user.email` for commit tests). → Mitigation: additive `cmd.Environ()` composition removes nothing else; any such test must set config via `-c user.name=...` rather than reverting the hardening.
- Risk: `--no-ext-diff`'s argv placement is wrong for one of the two diff-family invocations (`diff.go`'s `diff|show`, `submit.go`'s `gitHasStagedChanges`). → Mitigation: verified against each subcommand's accepted flag position; existing `internal/payload/diff_test.go` suite re-run unmodified as a regression check.
- Risk: Changing `ApplyPatch`'s public signature (T2's `allowConfigEdits` param, T6's `[]ReviewFlag` return) is a breaking change for its one known caller. → Mitigation: the caller (`cmd/atcr/autofix.go:365`) is updated atomically in the same task, never left broken between commits.

**TDD-Specific:**
- Risk: T5 (test-authorship task) is written against unstable or guessed T1/T3 symbol names since it runs after both. → Mitigation: T5 Step 1 explicitly mandates reading the finished implementation files before writing any test code — no signatures are invented ahead of landing.
- Risk: Symlink-traversal tests are flaky across CI runner OSes (some Windows runners restrict `os.Symlink`). → Mitigation: use `t.TempDir()` + `os.Symlink` with a `t.Skip` fallback on permission errors, not a hardcoded fixture path.
- Risk: The AC4 regression test itself produces false positives against `internal/verify/localvalidate.go` and `internal/sandbox/docker.go`, both of which match a naive `exec.CommandContext(` grep without calling git. → Mitigation: both files are explicitly excluded by path with an inline comment explaining why, confirmed against `codebase-discovery.json`'s scope notes, so a future maintainer doesn't "fix" the exclusion and reintroduce a false failure.

---

**Next:** `/create-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/`
