# Sprint 32.4: Workspace Integrity & Sandbox Escape Prevention

**Type:** Technical Debt 🔧
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 9 days
**Phases:** 5
**Execution Mode:** Gated 🚧 (adversarial ENABLED, inline-fix CRITICAL/HIGH)
**Branch:** `feature/32.4_workspace_integrity_sanitization`

---

## Overview

Hardens ATCR against **Indirect Sandbox Escape** attacks (Host Trust Transposition), where a contained sandbox execution is bypassed by modifying host configuration files (`.git/config`, `.githooks/`, `.github/workflows/`, `.vscode/`) that execute on the developer's host machine post-review. Builds a strict path-protection guard blocking `--auto-fix` writes to critical host-execution paths, hardens all host git subprocess invocations against poisoned config hijacking, and surfaces executable-bit/build-script changes as a non-blocking PR-body review warning.

## Timeline

| Phase | Focus | Tasks | Est. |
|-------|-------|-------|------|
| 1. Foundation | `pathguard.IsProtectedPath` + `internal/gitexec` package + six-site migration | 01, 03 | ≈2.5 days |
| 2. Integration | Wire `IsProtectedPath` into `applyOne`'s write choke point | 02 | ≈1 day |
| 3. CLI & Docs | `--allow-config-edits` flag + `docs/security.md` | 04 | ≈1 day |
| 4. Non-Blocking Review Flags | `FlagsForReview` executable-bit/build-script PR warnings | 06 | ≈2 days |
| 5. Testing & Validation | `pathguard`/`gitexec` unit tests + AC4 whole-tree regression | 05 | ≈2.5 days |

Each phase ends with a `N.LAST` phase-boundary gate (fresh-subagent adversarial review); `/execute-sprint` stops at each gate.

## Expected Outcomes

- `--auto-fix` refuses to write to `.git/`, `.githooks/`, `.github/workflows/`, `.vscode/`, `.idea/`, `.env*`, `.planning/`, or `.atcr` unless `--allow-config-edits` is explicitly passed
- Every host git subprocess across all six production call sites carries `GIT_CONFIG_NOSYSTEM=1`/`GIT_CONFIG_GLOBAL=/dev/null`, neutralizing poisoned `.git/config`/system/global config hijacking
- A binary, CI-enforced regression test proves zero stray bare `exec.Command("git",...)` call sites remain outside `internal/gitexec`
- Executable-bit changes and build-script path touches surface as a visible `## Review Warnings` PR-body section without blocking the apply
- `docs/security.md` documents the full security architecture and is indexed from `docs/README.md`

## Risk Summary (top 3)

1. **A missed call site during T3's six-site migration silently reopens the exact subprocess-hijack gap this epic exists to close.** → Mitigated by T5's AC4 regression test: a binary, CI-enforced gate (zero remaining bare `exec.Command("git",...)` outside `internal/gitexec`), not a judgment call.
2. **`internal/gitexec`'s hardened environment breaks an existing test that implicitly relies on the developer's global git config** (e.g. `user.name`/`user.email` for commit tests). → Mitigated by additive `cmd.Environ()` composition that removes nothing else; any such test must set config via `-c user.name=...` rather than reverting the hardening.
3. **`--no-ext-diff`'s argv placement is wrong for one of the two diff-family invocations** (`diff.go`'s `diff|show`, `submit.go`'s `gitHasStagedChanges`). → Mitigated by verifying the flag position against each subcommand's accepted grammar; existing `internal/payload/diff_test.go` suite re-run unmodified as a regression check.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — executable phase/task plan (gated)
- [metadata.md](metadata.md) — sprint tracking + complexity/schedule
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest
- [plan/](plan/) — archived plan (original-requirements, sprint-design, plan.md, 6 tasks, documentation)

---

**Next:** `/refine-sprint @.planning/sprints/active/32.4_workspace_integrity_sanitization/`
