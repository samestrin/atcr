# Task 01: Document the Versioning & Tagging Convention

**Source:** Plan 21.0 – Debt Item #1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
Three prior epics (7.3, 16.0, 20.0) each independently hit the same gap — `atcr` has no versioning/tagging strategy — and deferred it rather than build one. Today `internal/version.Version` (internal/version/version.go:14-16) defaults to the neutral placeholder `"0.0.0"` and `cmd/atcr`'s package-local `version` var (cmd/atcr/version.go:10-14) defaults to `""`, falling back to a `debug.ReadBuildInfo()`/VCS-revision heuristic. Neither is ever stamped by a release process because none exists. Meanwhile `CHANGELOG.md` already carries a de facto epic-number-as-semver convention — `[1.0.0]` through the current `[20.1.0]`, 20+ entries deep — with zero git tags ever cut against any of them (`git tag` returns zero results). Separately, `.github/workflows/reconcile-module.yml:11-16` is the only tag-triggered workflow in the repo, and it deliberately scopes itself to `reconcile/v*` specifically so an ATCR app tag (e.g. `v1.2.3`) never fires it — establishing bare `vX.Y.Z` as a free, reserved namespace for the ATCR app itself. No document currently records any of this as a decided convention, so the next epic that needs to cut a release still has nothing to follow.

## Solution Overview
Author the "Versioning & Tagging Convention" section of a new `docs/release-process.md`, formalizing bare `vX.Y.Z` git tags as the ATCR app's release-tag convention. The section must tie together three already-established facts rather than invent anything new: (1) `CHANGELOG.md`'s existing epic-number-as-semver numbering becomes the literal tag value going forward (e.g. the next changelog entry `[X.Y.Z]` maps to git tag `vX.Y.Z`), (2) this namespace is disjoint from and does not collide with Epic 8.0's `reconcile/vX.Y.Z` namespace already reserved for the standalone `./reconcile` module, and (3) the convention applies going forward only — no retroactive tagging of past `CHANGELOG.md` entries is required. This is documentation only; no code, workflow, or config changes are made in this task. Task 4 (Release-Process Documentation) will append the remaining sections (release trigger, who cuts a release, exact commands) to the same `docs/release-process.md` file created here.

## Technical Implementation
### Steps
1. Create `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md` with a top-level `# Release Process` heading and a `## Versioning & Tagging Convention` section (this task authors only this section; leave the file otherwise minimal so Task 4 can append its sections without restructuring what's here).
2. In that section, state the convention plainly: ATCR app releases use bare `vX.Y.Z` git tags (e.g. `v20.1.0`), matching the version number of the corresponding `CHANGELOG.md` entry (e.g. `## [20.1.0] - 2026-07-12` in `/Users/samestrin/Documents/GitHub/atcr/CHANGELOG.md:1` → tag `v20.1.0`). Cite `CHANGELOG.md`'s unbroken epic-number-as-semver history (`[1.0.0]` through `[20.1.0]`, 20+ entries) as the source this formalizes, and note that no tag has ever been cut against any past entry — this convention governs future releases, not a retroactive backfill.
3. In the same section, explicitly document the disjoint-namespace relationship to Epic 8.0: bare `vX.Y.Z` is for the ATCR app; `reconcile/vX.Y.Z` (already live, established by `.github/workflows/reconcile-module.yml:11-16`) is reserved exclusively for the standalone `./reconcile` module and must never collide with app tags. Quote or paraphrase `reconcile-module.yml`'s own header comment (lines 14-16) as the precedent evidence for why bare `vX.Y.Z` is free.
4. Note in the section (two or three sentences) that this tag value is what a release build's `-ldflags` will stamp into both `internal/version.Version` (internal/version/version.go:16) and `cmd/atcr`'s local `version` var (cmd/atcr/version.go:14) — establishing the contract Task 2 (goreleaser config) implements, without performing that wiring here. State the **decided version-string prefix convention** so Task 2 has no open choice: `atcr --version` / `atcr version` reports the **v-prefixed** `vX.Y.Z` (stamped from the full tag), matching what a `go install github.com/samestrin/atcr/cmd/atcr@vX.Y.Z` build already reports via `debug.ReadBuildInfo()`; the leaderboard submission envelope's `internal/version.Version` reports the **v-stripped** `X.Y.Z` (matching version.go:7's `1.2.3` doc-comment form and the historical bare `0.0.0` default). Both agree on the numeric `X.Y.Z`; the `v` prefix is the only permitted difference.
5. Do not touch `.planning/specifications/git-strategy.md:36`'s stale "Deploy: Merging to main builds production release binaries" line in this task — that correction belongs to Task 4, which builds out the rest of `docs/release-process.md` and reconciles the spec once the full process is documented.

## Files to Create/Modify
- `docs/release-process.md` – create with `# Release Process` heading and a `## Versioning & Tagging Convention` section documenting the bare `vX.Y.Z` tag convention, its mapping to `CHANGELOG.md`'s epic-number-as-semver history, and its disjointness from Epic 8.0's `reconcile/vX.Y.Z` namespace.

## Documentation Links
- [Version & Tagging Strategy](../documentation/version-tagging-strategy.md)
- [CI Workflow Reuse Convention](../documentation/ci-workflow-reuse.md)

## Related Files (from codebase-discovery.json)
- `internal/version/version.go` — defines `Version` var (default `"0.0.0"`), the leaderboard envelope's ldflags target this convention feeds.
- `cmd/atcr/version.go` — defines the package-local `version` var and `atcrVersion()` fallback chain, the CLI's ldflags target this convention feeds.
- `.github/workflows/reconcile-module.yml` — the only existing tag-triggered workflow; establishes the `reconcile/v*` namespace this convention must stay disjoint from.
- `CHANGELOG.md` — the epic-number-as-semver history (`[1.0.0]` through `[20.1.0]`) this convention formalizes.
- `docs/` — target directory for the new `release-process.md` (no existing `release*.md` file present).

## Success Criteria
- [ ] `docs/release-process.md` exists with a `## Versioning & Tagging Convention` section.
- [ ] The section states bare `vX.Y.Z` as the ATCR app tag convention and ties it explicitly to a `CHANGELOG.md` version heading example.
- [ ] The section explicitly documents disjointness from Epic 8.0's `reconcile/vX.Y.Z` namespace, citing `.github/workflows/reconcile-module.yml`.
- [ ] The section notes the convention applies going forward, with no retroactive tagging of past `CHANGELOG.md` entries required.
- [ ] The section references (without implementing) the dual ldflags-stamping contract that Task 2 will fulfill, and states the decided prefix convention: `atcr --version` reports v-prefixed `vX.Y.Z`; `internal/version.Version` (leaderboard envelope) reports v-stripped `X.Y.Z`.
- [ ] No code, workflow, or `.goreleaser.yaml` changes are made by this task.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — documentation-only task

**Integration Tests:**
- N/A — documentation-only task

**Test Files:**
- N/A

## Risk Mitigation
- Risk: the documented convention could drift from what Task 2's goreleaser config actually implements (e.g. a `v` prefix mismatch between the git tag and the ldflags-stamped value). Mitigation: this task states the tag-to-version mapping decisively (`vX.Y.Z` tag → `main.version` stamped v-prefixed as `vX.Y.Z`, `internal/version.Version` stamped v-stripped as `X.Y.Z`) so Task 2 has an unambiguous contract to follow with no open prefix decision; if Task 2 needs to deviate, it should update this section rather than silently diverge.
- Risk: scope creep into Task 4's territory (full release-process doc, git-strategy.md correction). Mitigation: this task is explicitly scoped to only the "Versioning & Tagging Convention" section; Task 4 owns everything else in `docs/release-process.md` and the `.planning/specifications/git-strategy.md:36` correction.

## Dependencies
- None

## Definition of Done
- [ ] `docs/release-process.md` created with the `## Versioning & Tagging Convention` section as specified.
- [ ] Section content grounded in and cross-referenced with `internal/version/version.go`, `cmd/atcr/version.go`, `.github/workflows/reconcile-module.yml`, and `CHANGELOG.md`.
- [ ] No other files modified.
- [ ] AC1 (versioning/tagging strategy decided and documented) satisfied.
