# Task 04: Complete Release-Process Documentation

**Source:** Plan 21.0 – Debt Item #4
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
Three prior epics (7.3, 16.0, 20.0) each independently needed a real release process for the `atcr` binary and each declined to build one, so no document has ever recorded what triggers a release, who is authorized to cut one, or the exact commands to run. Task 1 created `docs/release-process.md` with only its "Versioning & Tagging Convention" section (bare `vX.Y.Z` tags mapped to `CHANGELOG.md`'s epic-number-as-semver history). Task 2 (`.goreleaser.yaml`) and Task 3 (`.github/workflows/release.yml`) then built the actual tag-triggered mechanism, but nothing describes how a maintainer uses it end to end. Separately, `.planning/specifications/git-strategy.md:36` still asserts "Deploy: Merging to `main` builds production release binaries and packages the MCP server" — a claim that was never true (confirmed via this plan's `codebase-discovery.json`: no goreleaser config, no release workflow existed, and `ci.yml` only lints and tests on push/PR to `main`) and that becomes actively misleading once this plan ships a real, but *tag-triggered, not merge-triggered*, release mechanism.

## Solution Overview
Append the remaining sections to the `docs/release-process.md` file Task 1 created — "What Triggers a Release," "Who Cuts a Release," and "Cutting a Release" (the exact `git tag vX.Y.Z && git push --tags` procedure) — without restructuring Task 1's existing "Versioning & Tagging Convention" section. Then correct the single stale line at `.planning/specifications/git-strategy.md:36` so the spec describes the real tag-triggered mechanism (tag push → `release.yml` → goreleaser → GitHub Release) instead of the never-implemented merge-to-main automation. This is a documentation-only task with no code, workflow, or config changes; it runs last so it can accurately describe the finished process built by Tasks 1-3.

## Technical Implementation
### Steps
1. Read the existing `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md` (created by Task 1) to confirm its current structure — a `# Release Process` heading followed by the `## Versioning & Tagging Convention` section — and append new sections after it rather than rewriting what is there.
2. Add a `## What Triggers a Release` section stating that a release is triggered by pushing a `vX.Y.Z` git tag to the repository (not by merging to `main`), which fires `.github/workflows/release.yml` (Task 3's tag-push-triggered workflow, scoped to bare `v*` per the disjoint-namespace convention Task 1 documented) and runs goreleaser (Task 2's `.goreleaser.yaml`) to build cross-platform binaries and publish a GitHub Release. Note explicitly that a normal PR merge to `main` does **not** produce a release — only an explicit tag push does.
3. Add a `## Who Cuts a Release` section stating that `atcr` currently has a single maintainer (Sam Estrin) and release-cutting is a solo maintainer decision, not a formal release-manager rotation or approval process. Keep this section short — one or two sentences, reflecting current reality rather than prescribing process that doesn't exist yet.
4. Add a `## Cutting a Release` section with the exact step-by-step procedure:
   - Confirm `CHANGELOG.md` has an entry for the version being released (per Task 1's convention, the tag value matches the changelog heading, e.g. `## [21.0.0] - 2026-07-12` → tag `v21.0.0`).
   - Recommend a local dry run first: `goreleaser release --snapshot --clean` (no tag push, no publish) to verify the build and both `-X` ldflags targets (`internal/version.Version` and `cmd/atcr`'s local `version` var) resolve correctly before doing anything externally visible.
   - The real cut: `git tag vX.Y.Z && git push origin vX.Y.Z` (or `git push --tags`), run from an up-to-date `main`.
   - What happens next: the tag push fires `release.yml`, which runs goreleaser and publishes the GitHub Release automatically — no further manual step.
   - Note the risk explicitly: the first real tag publishes a public, externally-visible, hard-to-retract GitHub Release, so the snapshot dry run above is not optional for a maintainer's first time through this process.
5. Edit `/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/git-strategy.md:36` to replace the stale line. Change:
   `- **Deploy**: Merging to \`main\` builds production release binaries and packages the MCP server.`
   to a corrected statement describing the real, tag-triggered mechanism, e.g.:
   `- **Deploy**: Merging to \`main\` does not build a release. A maintainer-cut \`vX.Y.Z\` git tag push triggers \`.github/workflows/release.yml\`, which runs goreleaser to build cross-platform binaries and publish a GitHub Release — see [\`docs/release-process.md\`](../../docs/release-process.md).`
   Adjust the relative path to `docs/release-process.md` to match `.planning/specifications/git-strategy.md`'s actual location relative to the repo-root `docs/` directory.
6. Re-read both edited files in full to confirm the appended sections read coherently after Task 1's existing content, and that the git-strategy.md correction is a single, self-contained line change with no other text disturbed.

## Files to Create/Modify
- `docs/release-process.md` – append `## What Triggers a Release`, `## Who Cuts a Release`, and `## Cutting a Release` sections after Task 1's existing `## Versioning & Tagging Convention` section.
- `.planning/specifications/git-strategy.md` – correct line 36's stale "Deploy: Merging to `main` builds production release binaries" claim to describe the real tag-triggered release mechanism.

## Documentation Links
- [Version & Tagging Strategy](../documentation/version-tagging-strategy.md)
- [CI Workflow Reuse Convention](../documentation/ci-workflow-reuse.md)

## Related Files (from codebase-discovery.json)
- `docs/release-process.md` — target file; Task 1 already created it with the "Versioning & Tagging Convention" section, this task appends the rest.
- `docs/ci-integration.md` — structural precedent for how a process doc under `docs/` reads (short, scannable sections, exact commands in fenced code blocks).
- `.planning/specifications/git-strategy.md` — line 36 holds the stale "Deploy: Merging to `main` builds production release binaries" claim that must be corrected.
- `.github/workflows/release.yml` — Task 3's tag-triggered workflow this task documents the usage of (not modified here).
- `.goreleaser.yaml` — Task 2's goreleaser config this task documents the usage of (not modified here).
- `CHANGELOG.md` — the epic-number-as-semver history whose next entry the "Cutting a Release" procedure references as the tag-value source.

## Success Criteria
- [ ] `docs/release-process.md` contains all four sections: `## Versioning & Tagging Convention` (Task 1, unchanged), `## What Triggers a Release`, `## Who Cuts a Release`, `## Cutting a Release`.
- [ ] "What Triggers a Release" states plainly that a `vX.Y.Z` tag push (not a merge to `main`) triggers `release.yml` → goreleaser → GitHub Release.
- [ ] "Who Cuts a Release" reflects Sam Estrin as sole maintainer, not a formal rotation.
- [ ] "Cutting a Release" gives the exact `git tag vX.Y.Z && git push origin vX.Y.Z` (or `git push --tags`) procedure, including the recommended `goreleaser release --snapshot --clean` dry run before the first real tag.
- [ ] `.planning/specifications/git-strategy.md:36` no longer claims merging to `main` builds release binaries; it accurately describes the tag-triggered mechanism and links to `docs/release-process.md`.

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
- Risk: documenting a "Cutting a Release" procedure that turns out to not match what Tasks 2-3 actually implemented (e.g. wrong tag format, wrong workflow filename, wrong goreleaser invocation). Mitigation: this task runs last, after Tasks 1-3 are complete, specifically so it can verify the documented commands against the real `.goreleaser.yaml` and `release.yml` files rather than the plan's design intent.
- Risk: the first real tag push is a public, externally-visible, hard-to-retract GitHub Release — a maintainer following this doc for the first time could publish an unintended release. Mitigation: the "Cutting a Release" section makes the local `goreleaser release --snapshot --clean` dry run an explicit, non-optional first step before any tag push.
- Risk: editing `git-strategy.md:36` in isolation could leave surrounding CI/CD context inconsistent (e.g. still implying "Deploy" happens automatically on every merge). Mitigation: keep the edit scoped to the one line, phrase the replacement to explicitly contrast "merging to main" (no release) against "tag push" (triggers release), and re-read the full CI/CD subsection after editing to confirm it reads coherently.

## Dependencies
- Task-01 (creates `docs/release-process.md` with the Versioning & Tagging Convention section this task appends to)
- Task-02 (`.goreleaser.yaml` must exist and be accurate for this task to document its usage correctly)
- Task-03 (`.github/workflows/release.yml` must exist and be accurate for this task to document its usage correctly)

## Definition of Done
- [ ] `docs/release-process.md` is complete with all four sections and reads as a single coherent document (Task 1's section plus this task's three sections).
- [ ] `.planning/specifications/git-strategy.md:36` corrected to describe the real tag-triggered mechanism, with no other line in the file disturbed.
- [ ] Documented commands (`goreleaser release --snapshot --clean`, `git tag vX.Y.Z && git push origin vX.Y.Z`) verified against the actual `.goreleaser.yaml` and `release.yml` produced by Tasks 2-3.
- [ ] AC5 (documented process exists describing how to cut a release) satisfied.
