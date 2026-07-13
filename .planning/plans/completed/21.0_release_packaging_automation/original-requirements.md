# Original Request

**Date:** July 12, 2026 01:43:20PM
**Arguments:** `@.planning/epics/active/21.0_release_packaging_automation.md`
**Target:** `.planning/epics/active/21.0_release_packaging_automation.md`

## Content

# Feature Request: Release & Packaging Automation

- **Estimated time**: TBD
- **Tasks/Components**: TBD
- **Execution**: init-plan

## Context

Three separate epics have independently needed a real release/packaging process for the `atcr` binary and each time explicitly declined to build one:

- Epic 16.0 (Quick Start): "Cutting a first release is not a cheap workflow-template fix: the repo has no release automation (no goreleaser config, no tag-triggered release workflow) anywhere under `.github/workflows/`, and `git tag`/`gh release list` are both empty. Establishing a first release is a repo-wide lifecycle decision, not something a single epic's task list should absorb." (`.planning/.knowledge/clarifications-16.0_quick_start-Q1.md`)
- Epic 7.3 (GitHub Action / PR Integration): scoped release artifacts/goreleaser out, building the action's binary via `actions/setup-go` + `go build` instead. (`.planning/.knowledge/clarifications-7.3_github_action_pr_integration-Q2.md`)
- Epic 20.0 (Standalone Skill Release): proposed "package and distribute the atcr engine for public release" as a task, and was descoped back to documenting the existing `go install` path during `/refine-epic`, pending this epic.

Today the only distribution path is `go install github.com/samestrin/atcr/cmd/atcr@latest`. There is no versioning/tagging strategy (`internal/version` defaults to `"0.0.0"`), no goreleaser config, and no tag-triggered release workflow.

## Problem Statement

Every epic that touches distribution keeps re-encountering the same missing infrastructure and re-punting, because standing up release automation is out of scope for whatever feature epic happens to need it first. The decision keeps getting deferred, never made — this epic exists to break that loop by giving release/packaging automation its own scoped, standalone plan.

## Proposed Solution

1. Decide and implement a versioning/tagging strategy that `internal/version` reads at build time, replacing the current `"0.0.0"` placeholder. Bare `vX.Y.Z` tags are the convention to use — already reserved for this purpose by `.github/workflows/reconcile-module.yml` (Epic 8.0), which deliberately scopes itself to `reconcile/v*` so it never fires on an ATCR app tag. The version numbers themselves should formalize the epic-number-as-semver scheme `CHANGELOG.md` has used since its first entry (`[1.0.0]` through the current `[16.0.0]`), rather than starting an independent counter that would orphan that history.
2. Add a goreleaser config (or equivalent) that produces cross-platform binaries from a tag.
3. Add a tag-triggered GitHub Actions workflow that builds and publishes a GitHub Release.
4. Document the release process (what triggers a release, who cuts one, how).

## Acceptance Criteria

- [ ] AC1: A versioning/tagging strategy is decided and documented — bare `vX.Y.Z` tags (the namespace already reserved by Epic 8.0's `reconcile-module.yml`), with version numbers formalizing `CHANGELOG.md`'s existing epic-number-as-semver convention.
- [ ] AC2: `atcr version` / `atcr --version` reflects the real tagged version at build time.
- [ ] AC3: A goreleaser (or equivalent) config exists and produces cross-platform binaries.
- [ ] AC4: A tag-triggered GitHub Actions workflow builds and publishes a GitHub Release.
- [ ] AC5: A documented process exists describing how to cut a release.

## Out of Scope

- Homebrew tap, npm wrapper, Docker image, or other package-manager distribution beyond GitHub Releases — candidate for a follow-on epic once the core release process is proven.
- Any changes to atcr's engine behavior — this is packaging/distribution infrastructure only.
- Re-opening Epic 20.0's scope — that epic's install path stays `go install`-based regardless of this epic's outcome; this epic is what a *future* distribution improvement would build on.

## Dependencies / Related

- Referenced by (each deferred this exact work here): Epic 7.3 (GitHub Action PR Integration), Epic 16.0 (Quick Start), Epic 20.0 (Standalone Skill Release).

## Refinements (2026-07-03)

This section records findings from `/refine-epic --deep` run on July 03, 2026 01:47:26PM. It is additive — original plan content above is preserved.

### Auto-applied corrections (0)

None — every cited claim verified accurate against the live repository: `internal/version/version.go:16` still defaults to `"0.0.0"`; `git tag` returns zero tags; `gh release list` returns zero releases; no `.goreleaser.yaml`/`.goreleaser.yml` exists; `.github/workflows/` contains only `ci.yml`, `reconcile-module.yml`, and `refresh-synthetic-manifest.yml` — no release workflow. No typo'd paths or structural gaps found.

### Items needing user confirmation (2)

1. ⏸️ **The tag-namespace decision AC1 poses as open is already made and documented.** `.github/workflows/reconcile-module.yml:11-16` scopes its trigger to `reconcile/v*` tags specifically so that "ATCR app tags (e.g. v1.2.3) do NOT trigger this release gate" — and this is not an incidental comment: `.planning/sprints/completed/8.0_reconciler_library/plan/acceptance-criteria/06-01-tag-push-release-gate-workflow.md:40-44` ("Edge Case 1: Tag filter scopes to module releases, not ATCR app tags") records it as a deliberate, tested design decision from Epic 8.0. AC1 ("decide... a versioning/tagging strategy") should adopt the bare `vX.Y.Z` convention already reserved for it, rather than treating tag-naming as a fresh open question that risks colliding with the module's `reconcile/vX.Y.Z` namespace. **Suggested action:** note in Proposed Solution #1 that the tag convention is bare `vX.Y.Z`, already reserved by Epic 8.0's `reconcile-module.yml`.

2. ⏸️ **`CHANGELOG.md` already tracks a de facto epic-number-as-semver convention, 16+ releases deep, with zero matching git tags.** Every epic's `CHANGELOG.md` entry is versioned as `MAJOR.MINOR.0` matching its own epic number (`## [16.0.0]` for Epic 16.0, `## [15.1.0]` for Epic 15.1, `## [14.4.0]` for Epic 14.4, down to `## [1.0.0]` for Epic 1.0 in `docs/CHANGELOG_archive.md:359`) — an unbroken convention since the project's first entry, but no git tag has ever been cut to match any of them. AC1's "decide... a versioning strategy" is really a choice between (a) formalizing this existing epic-number scheme with real tags, or (b) deliberately diverging to independent semver — and diverging would orphan 16+ releases of existing version history from any future tag. **Suggested action:** name this explicitly in Proposed Solution #1 / AC1 so `/init-plan` treats it as a documented existing convention to formalize, not a greenfield decision.

### Advisory observations (3)

1. ℹ️ **Scope-guard violation (expected, matches the plan's own header):** Derived TASK_COUNT=4, COMPONENT_COUNT=3-4 (`internal/version` build-time wiring; new `.goreleaser.yaml`; new `.github/workflows/` release workflow; new release-process doc) — exceeds `/execute-epic`'s ≤6 tasks / ≤2 components limit. The plan already declares `Execution: init-plan` and `Tasks/Components: TBD`, so this confirms rather than changes the routing. Additionally, HAS_CROSS_SYSTEM is effectively true in spirit: a tag-triggered release workflow publishes a public, externally-visible GitHub Release — a hard-to-fully-reverse action once real users start pulling tagged binaries — so whoever executes this plan should treat the first real release cut with the same care as any other externally-visible, difficult-to-undo action.

2. ℹ️ **AC2 requires no new Go/CLI code.** `cmd/atcr/version.go` and `cmd/atcr/main.go:132` (`root.Version`) already fully implement `atcr version` / `atcr --version`, both reading `internal/version.Version` — confirmed via `.planning/.knowledge/clarifications-10.0_model_eval_leaderboard-Q3.md`, which records the original decision to add this package specifically so a release build could stamp it via `-ldflags`. The only remaining gap is the build-time stamping itself, which belongs to AC3's goreleaser config, not a separate task.

3. ℹ️ **Existing CI pattern to reuse for the new release workflow.** `.github/workflows/reconcile-module.yml` is the only existing tag-triggered workflow in the repo and is explicitly `based_on: .github/workflows/ci.yml` (per its own header comment) — reusing the `[self-hosted, gauntlet]` runner, Go 1.25 via `actions/setup-go`, and golangci-lint pinned to the repo-root `../.golangci.yml`. The new release workflow (AC4) should follow the same reuse pattern for CI consistency rather than introducing new runner/tooling conventions.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 4 (limit: 6)
- Derived COMPONENT_COUNT: 3-4 (limit: 2)
- COMPONENTS_TOUCHED: `internal/version` (build-time wiring only, no code change); new `.goreleaser.yaml`; new `.github/workflows/` release workflow; new release-process doc
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: true (publishes a public, externally-visible GitHub Release once a real tag is cut)
- Cited references checked: 7 (`internal/version/version.go`, `.planning/.knowledge/clarifications-16.0_quick_start-Q1.md`, `.planning/.knowledge/clarifications-7.3_github_action_pr_integration-Q2.md`, `.planning/epics/active/20.0_standalone_skill_release.md`, `.github/workflows/`, `.planning/epics/completed/16.0_quick_start.md`, `.planning/epics/completed/7.3_github_action_pr_integration.md`)
- Codebase search queries (spot-check): ["git tag / gh release list", "goreleaser config existence", "atcr version / --version CLI wiring"]
- Deep discovery method: semantic + keyword
- Deep discovery queries: ["goreleaser cross-platform binary release automation", "version tagging strategy git tag semver release", "reconcile-module release gate workflow tag", "CHANGELOG semver version per epic"]
- Deep discovery match count: 6
- Deep discovery snapshot: /Users/samestrin/Documents/GitHub/atcr/.planning/.temp/refine-epic/codebase-discovery.json (temp-only — not committed)

## Purpose

This document is the source of truth for this plan. All subsequent planning artifacts (plan.md, user-stories/, acceptance-criteria/ or tasks/, sprint-design.md) must trace back to the requirements captured here.
