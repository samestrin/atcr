# Original Requirements

**Date:** July 02, 2026 09:45:30PM
**Arguments:** `@.planning/epics/active/17.0_auto_merged_fixes.md`
**Target:** `.planning/epics/active/17.0_auto_merged_fixes.md`

## Content

# Feature Request: Auto-Merged Fixes Execution

- **Estimated time**: 10 days
- **Tasks/Components**: 6/5
- **Execution**: /init-plan (COMPONENT_COUNT 5 > 2 and cross-system AC5 exceed /execute-epic's scope guard)

## Problem

ATCR currently leaves the burden of applying fixes on the developer. This creates friction and slows down the remediation of identified issues.

## Solution

Enable ATCR to actively apply the fixes it generates. This includes parsing model-generated diffs, applying patches to the local tree, running local validation (e.g. `go build`), and orchestrating Git commits/PRs via the GitHub API.

## Acceptance Criteria

- [ ] AC1: System can parse LLM-generated diffs/patches robustly, handling missing context lines.
- [ ] AC2: System can apply patches to the working directory without corrupting files.
- [ ] AC3: System can run a configurable validation step (e.g., formatter, linter, or compiler) to verify the fix.
- [ ] AC4: System automatically reverts patches that fail the validation step.
- [ ] AC5: System can orchestrate Git operations via the GitHub API (create branch, commit, open/update PR).
- [ ] AC6: Feature is opt-in via a flag (e.g., `--auto-fix`).

## Success Criteria

- ATCR can successfully auto-fix at least 70% of the simple technical debt items it flags.
- Zero broken builds introduced by auto-merged fixes in the test corpus.

## Out of Scope

- Resolving complex merge conflicts if the target branch diverged significantly.
- Auto-fixing architectural or cross-repository issues.

## Existing Building Blocks (Reuse / Prior Art)

Most of this epic integrates shipped subsystems rather than building new ones. Map each AC to its anchor and reuse, do not rebuild:

| AC | Capability | Reuse anchor | Prior epic |
|----|------------|--------------|------------|
| AC1 | Parse LLM diffs (loose + git, missing context) | `internal/payload/ingest.go` `BuildEntriesFromDiff` | 10.1 |
| AC3 | Validate the fix | `internal/verify/syntaxguard.go` (extend the `go/parser` guard to a `go build`/lint/format step) | 7.1 |
| AC4 | Revert on failure | `internal/atomicfs/atomic.go`, `internal/fanout/reviewdir.go` `restorePriorBackup`, `internal/atomicwrite/` | 4.7 / 4.7.1 |
| AC5 (comment half) | GitHub API client | `cmd/atcr/github.go`, `internal/ghaction/client.go` (posts to an existing PR today) | 7.3 |
| upstream | Fix generation ("the fixes it generates") | `internal/verify/executor.go` `generateFixes` / `invokeExecutor` | 7.0 / 7.4 |

## Net-New Scope (the real work)

Confirmed absent from the codebase (zero grep matches for `--auto-fix`, `ApplyPatch`, and Go-side PR/branch creation):

- **AC2** — apply a parsed patch to the working tree without corruption (build on `internal/atomicfs`).
- **AC4** — wire revert into the validation-failure path (build on `restorePriorBackup`).
- **AC5 (new half)** — create branch, commit, open/update PR (extend `internal/ghaction`; today it only comments).
- **AC6** — `--auto-fix` opt-in flag + refuse-without-backend gate (mirror the `--exec` gate from Epic 11.0).

## Components Touched

`internal/autofix` (new — apply + revert), `internal/verify` (extend validation), `internal/atomicfs` (apply/revert primitives), `internal/ghaction` (branch/commit/PR), `cmd/atcr` (`--auto-fix` flag). `internal/payload` (diff parse) and `internal/verify/executor.go` (fix generation) are reference-only reuse. Net modify-set ≈ 5 packages — exceeds `/execute-epic`'s ≤2 limit; task decomposition is deferred to `/init-plan`.

## Refinements (2026-07-02)

This section records findings from `/refine-epic --deep` run on July 02, 2026 09:20:18PM. It is additive — original plan content above is preserved.

### Auto-applied corrections (0)

No mechanical corrections were needed. The plan cites no file paths, line numbers, or symbols, so there was nothing to verify-and-fix against the codebase. (Naming a `COMPONENTS_TOUCHED` list is intent-bearing and is surfaced below for confirmation rather than auto-applied.)

### Items needing user confirmation (3)

> **Resolved 2026-07-02 (user-approved):** Item 2 (reuse anchors) and Item 3 (component enumeration) were applied to the plan body above — see "Existing Building Blocks", "Net-New Scope", and "Components Touched", and the corrected `6/5` header. Item 1 (task decomposition) is intentionally deferred to `/init-plan`.

- ⏸️ **Plan is acceptance-criteria-only — no task decomposition.** The plan lists AC1–AC6 but no `## Tasks` / phases / file targets. `/execute-epic` needs an explicit, file-anchored task list; here every "task" had to be inferred from an AC. Either add a Tasks section (with file:region targets and RED→GREEN observable criteria per task), or run `/init-plan` which produces that decomposition as part of the full pipeline. NOT auto-applied.

- ⏸️ **Large overlap with already-shipped subsystems — plan reads as greenfield.** Codebase discovery shows ATCR already ships most of the capabilities the plan describes as new. Reuse anchors, by AC:
  - **AC1 (parse LLM diffs, missing context lines)** → `internal/payload/ingest.go` `BuildEntriesFromDiff` parses loose + git unified diffs (Epic 10.1). Reuse, don't rebuild.
  - **AC3 (validation step)** → `internal/verify/syntaxguard.go` (`go/parser` guard, Epic 7.1). AC3's configurable `go build`/lint/format step should extend this.
  - **AC4 (auto-revert)** → `internal/atomicfs/atomic.go` + `internal/fanout/reviewdir.go` `restorePriorBackup` + `internal/atomicwrite/` (Epics 4.7/4.7.1) provide crash-safe backup/swap/restore.
  - **AC5 (GitHub API orchestration)** → `cmd/atcr/github.go` + `internal/ghaction/client.go` (Epic 7.3) already post inline comments to an *existing* PR. The create-branch/commit/open-PR half is genuinely new but should extend `internal/ghaction`.
  - **Fix generation (implied upstream dependency)** → `internal/verify/executor.go` `generateFixes`/`invokeExecutor` (Epics 7.0/7.4) produce "the fixes it generates." The apply step consumes this output.
  Suggested action: add a "Reuse / Prior Art" subsection mapping each AC to its anchor, and scope the plan to the genuinely-new surface: **AC2 (patch apply), AC4 (revert integration), the create-branch/PR half of AC5, and AC6 (`--auto-fix` wiring)** — confirmed absent from the codebase (`--auto-fix`, `ApplyPatch`, and Go-side PR/branch creation all return zero grep matches). NOT auto-applied.

- ⏸️ **Touched components almost certainly exceed the stated 3.** Once the reuse anchors are counted, the real touched set spans `internal/payload`, `internal/verify`, `internal/atomicfs`/`atomicwrite`, `internal/ghaction`, `cmd/atcr`, plus a likely-new `internal/autofix` (or `internal/patch`) package — roughly 5+ top-level packages, versus the header's `6/3`. Either the `3` undercounts, or the plan must explicitly scope to net-new packages only and mark the anchors reference-only. Proposed `COMPONENTS_TOUCHED` (confirm before adopting): `[internal/autofix (new), internal/verify, internal/atomicfs, internal/ghaction, cmd/atcr]`. NOT auto-applied.

### Advisory observations (4)

- ℹ️ **Scope-guard violation — will be rejected by `/execute-epic`.** Header declares `Tasks/Components: 6/3`. `/execute-epic` Phase 1 HARD STOPs when COMPONENT_COUNT > 2 (TASK_COUNT 6 is at the ≤6 limit, OK). COMPONENT_COUNT=3 (author-stated, and realistically 5+ per the finding above) exceeds the ≤2 limit. Refining alone will not unblock `/execute-epic`; this plan should run through `/init-plan` for the full sprint pipeline.
- ℹ️ **HAS_CROSS_SYSTEM = true.** AC5 mutates external GitHub state (create branch, open/update PR). That adds risk surface the plan does not address — auth/token scope, rate limits, and rollback of *remote* state on partial failure (local revert per AC4 does not undo a pushed branch or opened PR). Independently argues for the full sprint pipeline over `/execute-epic`'s linear TDD loop.
- ℹ️ **Genuinely-new surface confirmed by grep.** `--auto-fix` / `auto_fix` / `autofix`, `ApplyPatch`/`applyPatch`, and Go-side PR/branch creation (`pulls.Create`, `CreateRef`, etc.) all return zero matches under `cmd/` and `internal/`. AC2, AC4, the create-branch/PR half of AC5, and AC6 are the true net-new scope to plan around.
- ℹ️ **Stale semantic-index chunk (no action).** The `atcr-code` index returns a phantom line-1 chunk for this file containing an `**Execution**: init-plan (COMPONENT_COUNT=...)` string that is NOT present on disk (verified by fresh read). Index noise from an earlier planning exploration; a reindex clears it. On-disk content is authoritative and unchanged.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 6 (limit: 6)
- Derived COMPONENT_COUNT: 3 stated / ~5+ actual (limit: 2)
- COMPONENTS_TOUCHED: internal/autofix (new), internal/verify, internal/atomicfs, internal/ghaction, cmd/atcr
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: true
- Cited references checked: 0 file paths, 0 file:line, 0 symbols (plan cites only the `go build` command and `--auto-fix` flag name)
- Codebase search queries (spot-check): ["auto-fix / ApplyPatch existence", "Go-side PR/branch creation", "diff/patch apply + github + validation + revert (semantic)"]
- Deep discovery method: semantic
- Deep discovery queries: ["apply unified diff patch to working tree files", "create github pull request branch and commit", "run validation command build compile check", "parse LLM generated code fix suggestion diff", "revert rollback restore files on failure"]
- Deep discovery match count: 15
- Deep discovery snapshot: .planning/.temp/refine-epic/codebase-discovery.json (temp-only — not committed)

## Purpose

This document captures the original request verbatim as the source of truth for this plan. All subsequent planning artifacts (plan.md, user stories, acceptance criteria, sprint design) must trace back to and remain consistent with the requirements captured here.
