## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-07-02T21:55:14-07:00
**Plan Goal:** Let ATCR apply the fixes it generates instead of leaving that burden on the developer: parse the generated diff, apply it to the working tree safely, validate it locally, and — on success — push a branch and open/update a PR via the GitHub API. On validation failure, automatically revert.
**Target Users:** ATCR maintainers and CI/CD pipeline operators who currently copy-paste generated fixes by hand and want a fully opt-in `--auto-fix` path.
**Framework/Technology:** Go 1.25, cobra CLI, GitHub REST API (via `internal/ghaction`).

## Objectives
- **AC1: Robust Diff Parsing**: Parse LLM-generated diffs/patches robustly, handling missing context lines by reusing `internal/payload/ingest.go` `BuildEntriesFromDiff`.
- **AC2: Safe Patch Application**: Apply patches to the working directory without corrupting files, leveraging `github.com/bluekeyes/go-gitdiff` and `internal/atomicfs`.
- **AC3: Configurable Validation**: Run a configurable validation step (e.g. formatter, linter, or compiler) to verify the fix before pushing changes.
- **AC4: Automatic Revert**: Automatically revert patches that fail the validation step, restoring files from prior backups.
- **AC5: GitHub API Orchestration**: Orchestrate Git operations via the GitHub API (create branch, commit, open/update PR) extending the `internal/ghaction` client.
- **AC6: Opt-In Gated Flag**: Feature is opt-in via a flag (e.g. `--auto-fix` or an `autofix` command) and refuses to run without a fully configured backend.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 6 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/17.0_auto_merged_fixes/`

## Feature Analysis Summary
Most of the capability this epic asks for already exists in shipped subsystems (Epics 10.1, 7.1, 4.7/4.7.1, 7.3) — diff parsing, crash-safe backup/swap, local syntax validation, and posting to an existing PR. The genuinely new surface is narrow: applying a parsed patch to the working tree without corruption (AC2), wiring an automatic revert into the validation-failure path (AC4), the create-branch/commit/open-PR half of GitHub orchestration (AC5, since today's `internal/ghaction` only comments on an existing PR), and an opt-in `--auto-fix` flag with a refuse-without-backend gate (AC6, mirroring Epic 11.0's `--exec` gate). A new `internal/autofix` package is the natural home for the orchestration logic that ties these together, built as a thin layer over the existing `internal/payload`, `internal/atomicfs`, `internal/verify`, and `internal/ghaction` primitives rather than a rebuild. `--auto-fix` mutates external GitHub state (branch/PR creation), so this plan carries cross-system risk beyond the local revert path: a local revert (AC4) does not undo a pushed branch or an already-opened PR, and that gap needs an explicit answer during design.

## Technical Planning Notes
- Net-new package: `internal/autofix` (apply + revert), consuming `payload.BuildEntriesFromDiff` (AC1 reuse) and writing through `atomicfs.WriteFileAtomic`/`BackupToDotBak` (AC2/AC4 reuse).
- `internal/verify/syntaxguard.go` extends with a configurable `go build`/lint/format validation mode (AC3) alongside its existing `go/parser`-only check — same conservative-failure philosophy, new execution path.
- `internal/ghaction/client.go` extends with `CreateBranch`/`CreateCommit`/`CreatePullRequest`/`UpdatePullRequest` methods, reusing its existing retry/backoff/secret-redaction plumbing rather than a second HTTP client.
- `--auto-fix` (AC6) is off by default and refuses to run without a fully configured backend (apply target + validation command + GitHub token/repo), mirroring Epic 11.0's `--exec gate exactly`.
- AC4's revert must be scoped per-file (one `.bak` per touched file), not directory-wide — a single patch can touch multiple files and a partial apply must be revertible file-by-file.

## Implementation Strategy
Build `internal/autofix` as an orchestrator over four existing subsystems rather than a monolith: it calls `internal/payload` to parse the fix diff, `internal/atomicfs` to back up and atomically write each touched file, `internal/verify` (extended) to run the configured validation command, and — only on success — `internal/ghaction` (extended) to push a branch, commit, and open/update a PR. A validation failure triggers an automatic per-file revert from the `atomicfs` backups before any GitHub state is touched, so remote mutation only ever happens after a locally-verified fix. The `--auto-fix` CLI flag in `cmd/atcr` is the single entry point gating this entire flow, off by default and refusing to start without a fully configured backend.

## Documentation References
- **[CRITICAL]** [Patch Application (AC2)](documentation/patch-application.md)
- **[CRITICAL]** [Validation and Automatic Revert (AC3/AC4)](documentation/validation-and-revert.md)
- **[IMPORTANT]** [GitHub API Orchestration (AC5)](documentation/github-orchestration.md)
- **[REFERENCE]** [Opt-In Flag with Refuse-Without-Backend Gate (AC6)](documentation/cli-opt-in-gate.md)

## Recommended Packages
go-gitdiff (patch application — see package-recommendations.md)

## User Story Themes
1. **Apply a parsed patch to the working tree without corruption** (AC2) — the core write-path a developer trusts not to mangle their files.
2. **Run a configurable local validation step** (AC3) — extend the existing syntax guard to a real `go build`/lint/format check.
3. **Automatically revert a patch that fails validation** (AC4) — the safety net that makes auto-apply trustworthy.
4. **Create a branch and commit the verified fix** (AC5, new half) — the first remote-mutating step, gated on a passing local validation.
5. **Open or update a pull request via the GitHub API** (AC5, new half) — surface the fix for human review rather than merging silently.
6. **Opt in via `--auto-fix` with a refuse-without-backend gate** (AC6) — the single flag that turns this entire flow on, off by default.

## Success Criteria
- ATCR can successfully auto-fix at least 70% of the simple technical debt items it flags.
- Zero broken builds introduced by auto-merged fixes in the test corpus.
- `--auto-fix` is fully opt-in: default `atcr` behavior is unchanged when the flag is absent.
- A validation failure never leaves the working tree in a patched-but-broken state (AC4's revert always fires before exit).

## Risk Mitigation
- **Risk:** A pushed branch or opened PR cannot be undone by AC4's local revert, since that revert only covers the working tree. **Mitigation:** Sequence the flow so no GitHub-mutating call happens until local validation has already passed; treat branch/PR creation as the last, not first, step.
- **Risk:** A generic `go build`/lint/format validation step is more failure-prone across languages/toolchains than the existing Go-only `go/parser` guard. **Mitigation:** Scope AC3 to a configurable *command* (user-supplied), not a hardcoded multi-language validator — the existing conservative-failure philosophy from `syntaxguard.go` still applies to the Go path, and non-Go projects supply their own command.
- **Risk:** Complex merge conflicts on a diverged target branch could corrupt the apply step. **Mitigation:** Explicitly out of scope per the epic — AC2/AC4 assume a clean apply target and fail fast (then revert) rather than attempt conflict resolution.

## Next Steps
1. `/find-documentation @.planning/plans/active/17.0_auto_merged_fixes/`
2. `/create-documentation @.planning/plans/active/17.0_auto_merged_fixes/`
3. `/create-user-stories @.planning/plans/active/17.0_auto_merged_fixes/`
4. `/create-acceptance-criteria @.planning/plans/active/17.0_auto_merged_fixes/`
5. `/design-sprint @.planning/plans/active/17.0_auto_merged_fixes/`
6. `/create-sprint @.planning/plans/active/17.0_auto_merged_fixes/`
