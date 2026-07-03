# User Story 2: Configurable Local Validation

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer running `--auto-fix`
**I want** every applied patch to pass a configurable local validation command (e.g. `go build`, a linter, or a formatter) before anything is committed or pushed
**So that** I never end up with a broken build or malformed file as the result of an automated fix, regardless of the target language or toolchain

## Story Context

- **Background:** `internal/verify/syntaxguard.go` already runs a `go/parser`-based syntax check on Fix content before it ever touches disk, under a "conservative failure" philosophy — flag only when the content is plausibly Go code yet fails to parse; prose and non-Go fenced blocks pass through untouched. That check happens before AC2's patch apply. This story is a distinct, later gate: after Story 1 (AC2) has applied the patch to the working tree, this step validates the *result on disk* by running a real, user-supplied command rather than a hardcoded AST parse — so it works for any language or toolchain, not just Go.
- **Assumptions:** Story 1 (AC2, patch application) has already run and produced a modified working tree plus per-file `.bak` backups via `atomicfs.BackupToDotBak`. A validation command is available to run (e.g. `go build ./...`, `golangci-lint run`, `gofmt -l`, or a project-supplied script) either via CLI flag/config or a sane per-language default. This story's output (pass/fail) does not itself perform any revert — it hands the pass/fail decision to Story 3 (AC4).
- **Constraints:** Must stay language-independent — no hardcoded per-language validation logic beyond an optional default command. Must not touch GitHub state (AC5) under any circumstances; a validation run happens strictly between "patch applied" (Story 1) and "commit/PR" (Stories 4-5). The command must be run with a bounded timeout so a hanging build/lint process cannot stall `--auto-fix` indefinitely. Exit code is the sole pass/fail signal — a non-zero exit is always treated as failure, with no attempt to interpret partial success from stdout/stderr content.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (AC2 - Safe Patch Application) must apply the patch to the working tree first; this story validates that result. Feeds its pass/fail output directly into User Story 3 (AC4 - Automatic Revert). |

## Success Criteria (SMART Format)

- **Specific:** After a patch is applied to the working tree, ATCR runs a single configured validation command against the affected package/module and captures its exit code, stdout, and stderr, without requiring any language-specific code in `internal/verify`.
- **Measurable:** 100% of applied patches in the `--auto-fix` flow have a validation result (pass or fail) recorded before the flow proceeds to Story 3's revert decision or Story 4's commit step — no patch is ever committed or reverted without first passing through this gate.
- **Achievable:** Reuses the existing `internal/verify/syntaxguard.go` conservative-failure philosophy and adds a new execution path (`os/exec` command runner) rather than building a new multi-language validator from scratch.
- **Relevant:** This is the safety gate the plan's success criteria depend on directly — "zero broken builds introduced by auto-merged fixes" is only achievable if every fix is locally validated before it can reach a branch/PR.
- **Time-bound:** Validation command execution completes (or times out) within the sprint's per-task budget; a configurable timeout (with a sane default, e.g. 2 minutes) prevents an unbounded hang from blocking the `--auto-fix` run.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-configurable-validation-command-runner.md) | Configurable Validation Command Runner | Unit |
| [02-02](../acceptance-criteria/02-02-result-capture-and-reporting.md) | Validation Result Capture and Reporting | Unit |
| [02-03](../acceptance-criteria/02-03-conservative-pass-fail-gate.md) | Conservative Pass/Fail Gate (No Mutation, No Partial Success) | Unit |

## Original Criteria Overview

1. `internal/verify` exposes a new configurable validation entry point that accepts a user-supplied command (or a sane per-language default) and runs it against the post-patch working tree.
2. The validation step captures the command's exit code, stdout, and stderr, and reports a clear pass/fail result plus captured output for diagnostics.
3. A non-zero exit code is always treated as a failed fix (conservative failure, no partial-success interpretation), and the result is handed to the AC4 revert decision without the validation step itself mutating any files.

## Technical Considerations

- **Implementation Notes:** Add a new function alongside `validateGoFixSyntax` in `internal/verify/syntaxguard.go` (or a sibling file in the same package) that shells out via `os/exec.CommandContext` with a bounded `context.WithTimeout`, running in the repository root or the affected package directory. Keep the existing AST-based `validateGoFixSyntax` untouched — it remains the pre-apply Go-only fast check; this story's command-runner is the post-apply, language-independent check. Command configuration should follow the same flag/config pattern as other opt-in ATCR behaviors (e.g. Epic 11.0's `--exec` gate) so it composes cleanly with Story 6's `--auto-fix` gate.
- **Integration Points:** Consumes the modified working tree produced by Story 1 (AC2 patch application). Produces a pass/fail result consumed by Story 3 (AC4 automatic revert) — on fail, Story 3 restores from the `.bak` backups Story 1 created; on pass, the flow proceeds to Story 4/5's branch/commit/PR steps. Must not import or call anything in `internal/ghaction` — GitHub orchestration happens strictly after this gate passes.
- **Data Requirements:** No new persistent data/schema. In-memory result struct (exit code, stdout, stderr, duration) passed along the `internal/autofix` orchestration call chain; no need to persist validation output beyond the current `--auto-fix` run's logs/diagnostics.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A hanging or slow validation command (e.g. a full `go build` on a large module) stalls the entire `--auto-fix` flow | Medium | Enforce a configurable timeout via `context.WithTimeout`; treat a timeout as a failed validation (triggers Story 3's revert) rather than blocking indefinitely |
| A validation command that is missing, misconfigured, or not executable in the current environment produces a false "failed" result and reverts a genuinely good fix | Medium | Surface a distinct error class for "command not found / not executable" versus "command ran and exited non-zero," and refuse to proceed with `--auto-fix` entirely (fail fast before touching any files) if the configured validator can't even start |
| Broadening validation beyond Go (arbitrary shell command) reintroduces the false-positive risk `syntaxguard.go`'s conservative-failure design was built to avoid | Medium | Keep the command's exit code as the only decision signal — no output-content heuristics — so behavior is fully predictable and owned by whoever configures the command, not inferred by ATCR |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Draft - Awaiting Acceptance Criteria
