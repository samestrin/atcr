# Plan 32.4: Workspace Integrity & Indirect Sandbox Escape Prevention

## Metadata
**Plan Type:** tech-debt
**Created:** 2026-07-21
**Last Modified:** 2026-07-21
**Plan Goal:** Harden ATCR's `--auto-fix` pipeline and host git invocations against Indirect Sandbox Escape / Host Trust Transposition attacks, so LLM-generated patches cannot write malicious triggers into host-execution config paths and host git subprocesses cannot be hijacked by a poisoned `.git/config`.
**Target Users:** ATCR maintainers and operators running `--auto-fix` against untrusted LLM-generated patches; downstream reviewers of `--auto-fix` PRs.
**Framework/Technology:** Go (cobra CLI), go-gitdiff for patch parsing, `os/exec` for git subprocess invocation.

## Objectives
- Block modification or creation of critical host-execution and configuration paths (`.git/*`, `.githooks/*`, `.github/workflows/*`, `.gitlab-ci.yml` and CI definitions, `.vscode/*`, `.idea/*`, `.env*`, `.planning/`, `.atcr`) during `--auto-fix` patch application via a non-bypassable `pathguard` check in `internal/autofix/apply.go`.
- Provide an explicit `--allow-config-edits` CLI flag to allow operators to bypass path protection when legitimately refactoring build configurations.
- Harden all host Git subprocess executions across ATCR by creating `internal/gitexec` (injecting `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, and `--no-ext-diff`) and migrating all 6 host Git call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go`, `internal/stream/fileindex.go`).
- Surface executable-bit changes (`OldMode`/`NewMode`) and build-script path modifications (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, CI configs outside `.github/`) as non-blocking warnings in generated PR bodies via `FlagsForReview`.
- Document security architecture and CLI flags in `docs/security.md` and link it in `docs/README.md`.

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Refined via `/refine-tasks`
- **Estimated Count:** 6 tasks
- **Task Files:**
  - [Task 01: Build `internal/security/pathguard.go`](tasks/task-01-build-pathguard-package.md)
  - [Task 02: Wire pathguard into `applyOne`](tasks/task-02-wire-pathguard-into-apply.md)
  - [Task 03: Build `internal/gitexec` and Migrate Call Sites](tasks/task-03-build-gitexec-and-migrate-call-sites.md)
  - [Task 04: Add `--allow-config-edits` Flag and Docs](tasks/task-04-allow-config-edits-flag-and-docs.md)
  - [Task 05: Unit Tests and Regression Coverage](tasks/task-05-unit-tests-and-regression-coverage.md)
  - [Task 06: Non-blocking `FlagsForReview` PR Warnings](tasks/task-06-non-blocking-review-flags.md)

## Feature Analysis Summary
Recent disclosures show AI coding agents get bypassed not by breaking the sandbox but by writing malicious host-config files (`.git/config`, `.githooks/`, `.vscode/`, `.github/workflows/`) that execute with full host privileges after the agent's sandboxed pass ends — Host Trust Transposition. ATCR's `--auto-fix` pipeline writes LLM-generated patches to the host repo via `internal/autofix` and shells out to host git across six scattered call sites with no shared env hardening, so both halves of this attack are currently open. This plan closes the write-path gap with a blocklist check inside the existing `containedPath` choke point in `internal/autofix/apply.go`'s `applyOne`, and closes the subprocess-hijack gap by introducing one `internal/gitexec` package that injects `GIT_CONFIG_NOSYSTEM`/`GIT_CONFIG_GLOBAL`/`--no-ext-diff` and migrating all six call sites to it. A third, non-blocking layer surfaces executable-bit and build-script changes as a visible PR-body warning, since `--auto-fix` already never auto-merges and human review is the existing terminal gate.

## Technical Planning Notes
- New `internal/security` package (`pathguard.go`) with no I/O — pure prefix/path-segment matching, mirroring `internal/validation/validation.go`'s `FilePath` style. Blocklist includes `.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml` and CI definitions, `.vscode/`, `.idea/`, `.env*`, `.planning/`, `.atcr`.
- New `internal/gitexec` package wraps `exec.Command`/`exec.CommandContext` for git only; `internal/verify/localvalidate.go` (runs the operator's own `validate_command`) and `internal/sandbox/docker.go` (runs docker, not git) are confirmed out of scope by the codebase-discovery grep.
- go-gitdiff's already-parsed `File.OldMode`/`NewMode` (used in `internal/autofix/apply.go`'s `applyOne`) gives T6's executable-bit detection for free — no new parsing step needed.
- `--allow-config-edits` follows the existing `--no-sandbox` precedent in `cmd/atcr/autofix.go`: off by default, explicit opt-out, security-implication warning.

## Implementation Strategy
Build `internal/security/pathguard.go` first (T1) since both `apply.go`'s gate (T2) and the PR-body warning (T6) depend on it. Build `internal/gitexec` (T3) independently and migrate the six call sites one at a time, each migration independently testable and revertable. Wire pathguard into `internal/autofix/apply.go`'s `applyOne` once `IsProtectedPath` exists (T2), then extend the same file with `FlagsForReview` once gitdiff's `OldMode`/`NewMode` are threaded through (T6). Land the `--allow-config-edits` flag and `docs/security.md` (T4) alongside T2 since they're tightly coupled. Write `pathguard_test.go` and `gitexec_test.go` last (T5) once both packages are stable, including the AC4 regression check that greps for stray `exec.Command("git",...)` outside `gitexec`.

## Recommended Packages
No high-ROI packages identified — path validation, git-env hardening, and executable-bit detection are covered by stdlib `os/exec`/`path/filepath`/`strings` plus the already-vendored `go-gitdiff` dependency (confirmed present in `go.mod`).

## Tasks Overview

| ID | Task | Primary Files | Type |
|----|------|----------------|------|
| T1 | [Build `internal/security/pathguard.go`](tasks/task-01-build-pathguard-package.md) with `IsProtectedPath(path string) bool` blocklist (`.git/*`, `.githooks/*`, `.github/workflows/*`, `.gitlab-ci.yml`, `.vscode/*`, `.idea/*`, `.env*`, `.planning/`, `.atcr`) | `internal/security/pathguard.go` | New package |
| T2 | [Wire pathguard into `internal/autofix/apply.go`'s `applyOne`](tasks/task-02-wire-pathguard-into-apply.md) after `containedPath`, gated on `AllowConfigEdits` | `internal/autofix/apply.go` | Integration |
| T3 | [Build `internal/gitexec` wrapper](tasks/task-03-build-gitexec-and-migrate-call-sites.md) (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `--no-ext-diff`); migrate all six git call sites | `internal/gitexec/gitexec.go`, `cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go`, `internal/stream/fileindex.go` | New package + migration |
| T4 | [Add `--allow-config-edits` flag; write `docs/security.md`; index it in `docs/README.md`](tasks/task-04-allow-config-edits-flag-and-docs.md) | `cmd/atcr/autofix.go`, `docs/security.md`, `docs/README.md` | CLI + docs |
| T5 | [Unit tests for pathguard and gitexec, plus AC4 regression grep](tasks/task-05-unit-tests-and-regression-coverage.md) for stray `exec.Command("git",...)` | `internal/security/pathguard_test.go`, `internal/gitexec/gitexec_test.go` | Tests |
| T6 | [Extend pathguard with non-blocking `FlagsForReview`](tasks/task-06-non-blocking-review-flags.md); thread flagged entries through `ApplyPatch` into the PR body | `internal/security/pathguard.go`, `internal/autofix/apply.go`, `cmd/atcr/autofix.go` | Integration |

## Planning Success Criteria
- `--auto-fix` refuses to modify or create files under `.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`, `.vscode/`, `.idea/`, `.env*`, `.planning/`, or `.atcr` and returns a security error unless `--allow-config-edits` is set.
- All Git subprocesses executed by ATCR carry `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, and `--no-ext-diff` (where applicable) in their environment / flags.
- `pathguard_test.go` verifies 100% path matching across canonical, relative, and symlink-traversal formats.
- All six identified host git subprocess call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go`, `internal/stream/fileindex.go`) are migrated to `internal/gitexec`; no bare `exec.Command("git",...)` or `exec.CommandContext(ctx, "git",...)` call sites remain outside it.
- A generated `--auto-fix` PR whose patch touches an executable-bit change or a build-script path (per `FlagsForReview`) includes a visible warning section in the PR body naming the flagged path(s) and reason; a patch with no flagged paths has no such section.

## Risk Mitigation
- **Risk:** `--allow-config-edits` could become a habitual bypass that quietly defeats path protection. **Mitigation:** keep it off by default and pair it with a mandatory, non-memoized stderr warning (mirroring `--no-sandbox`), plus document it explicitly in `docs/security.md`.
- **Risk:** `internal/gitexec` migration touches six call sites across five packages — a missed site leaves exactly the gap this epic exists to close. **Mitigation:** T5's regression test greps the whole tree for bare `exec.Command`/`exec.CommandContext` invoking `"git"` outside `internal/gitexec`, so a missed site fails CI rather than passing silently.
- **Risk:** `FlagsForReview`'s soft build-script list could either over-warn (noise fatigue) or under-warn (missed real risk). **Mitigation:** scope the list to the epic's named set (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, non-`.github` CI configs) and treat it as advisory-only (never blocking), so a miss degrades to "no extra visibility" rather than a false refusal.

## Next Steps
1. `/find-documentation @.planning/plans/active/32.4_workspace_integrity_sanitization/`
2. `/create-documentation @.planning/plans/active/32.4_workspace_integrity_sanitization/`
3. `/create-tasks @.planning/plans/active/32.4_workspace_integrity_sanitization/`
4. `/design-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/`
5. `/create-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/`

