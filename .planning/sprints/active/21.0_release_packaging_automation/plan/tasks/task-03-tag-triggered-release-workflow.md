# Task 03: Tag-Triggered Release Workflow

**Source:** Plan 21.0 – Debt Item #3
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
Three separate epics (16.0, 7.3, 20.0) each independently needed a real release/packaging process for the `atcr` binary and each explicitly declined to build one. Today the only distribution path is `go install github.com/samestrin/atcr/cmd/atcr@latest` against an untagged `main` — there is no tag-triggered mechanism that builds cross-platform binaries and publishes a GitHub Release. The repo already has one tag-triggered workflow, `.github/workflows/reconcile-module.yml`, scoped to the `reconcile/v*` tag namespace for the standalone `./reconcile` module, but nothing analogous exists for the ATCR app itself.

## Solution Overview
Add a new `.github/workflows/release.yml` GitHub Actions workflow that triggers on push of a bare `vX.Y.Z` tag (disjoint from `reconcile-module.yml`'s `reconcile/v*` filter, so the two workflows never both fire on the same tag) and invokes `goreleaser/goreleaser-action` to run `goreleaser release --clean` against the `.goreleaser.yaml` config Task 2 creates. The workflow follows this repo's established `based_on: .github/workflows/ci.yml` CI-reuse convention — reusing the `[self-hosted, gauntlet]` runner, `actions/checkout@v4`, and `actions/setup-go@v5` (Go 1.25, `cache: false`) steps verbatim — with one deliberate divergence: `permissions: contents: write` instead of the base workflows' `contents: read`, because goreleaser creates a GitHub Release (a write operation).

## Technical Implementation
### Steps
1. Create `.github/workflows/release.yml` with a header comment block explaining why the workflow exists (mirroring `reconcile-module.yml:1-9`'s style) and the `based_on: .github/workflows/ci.yml` line stating exactly what is reused verbatim (the `[self-hosted, gauntlet]` runner and the Go 1.25 setup).
2. Set the trigger to `on: push: tags: ['v*']`, with an inline comment (mirroring `reconcile-module.yml:11-16`'s comment style) explaining the bare `v*` filter is deliberately disjoint from `reconcile-module.yml`'s `reconcile/v*` filter so a push of either tag pattern only fires one workflow.
3. Set `permissions: contents: write` at the workflow level (not `contents: read`) — the single deliberate divergence from the `based_on` reuse convention, since goreleaser's `release` step publishes a GitHub Release.
4. Add a `concurrency:` block scoped to `release-${{ github.ref }}` with `cancel-in-progress: true`, matching the pattern already used in `ci.yml:12-14` and `reconcile-module.yml:21-23`.
5. Define a single job (e.g. `release`) running on `runs-on: [self-hosted, gauntlet]` with these steps, copied verbatim from `ci.yml`/`reconcile-module.yml`:
   - `actions/checkout@v4` — with `fetch-depth: 0` (goreleaser needs full git history/tags to generate changelogs and resolve the previous tag; this is an addition beyond the base workflows' checkout, since neither `ci.yml` nor `reconcile-module.yml` needs tag history).
   - `actions/setup-go@v5` with `go-version: '1.25'` and `cache: false` (carry over the same `cache: false` rationale comment from `ci.yml:25-36`).
6. Add a `goreleaser/goreleaser-action@v6` step (pin to the latest stable major tag available at implementation time) with `args: release --clean`, relying on the default `GITHUB_TOKEN` secret (already available to workflow runs) for GitHub Release publishing — do not hardcode or introduce a new secret.
7. Add a short comment near the goreleaser step referencing the dry-run precedent from `plan.md`'s risk mitigation table: the first real tag push publishes a public, hard-to-retract GitHub Release, so `goreleaser release --snapshot --clean` should be run locally (Task 2's deliverable) and confirmed with the maintainer before the first real `git push --tags`.
8. Validate the new YAML with a linter/parser (e.g. `yamllint .github/workflows/release.yml` or `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))"`) to confirm it parses before considering the task done.

## Files to Create/Modify
- `.github/workflows/release.yml` – new tag-triggered GitHub Actions workflow that runs goreleaser on `v*` tag push and publishes a GitHub Release.

## Documentation Links
- [CI Workflow Reuse Convention](../documentation/ci-workflow-reuse.md)
- [GoReleaser Configuration](../documentation/goreleaser-configuration.md)

## Related Files (from codebase-discovery.json)
- `.github/workflows/reconcile-module.yml` — the only existing tag-triggered workflow; establishes the tag-namespace-scoping and `based_on` CI-reuse conventions this task's new workflow must follow (reference-only).
- `.github/workflows/ci.yml` — the base `based_on:` target; runner label, Go setup, and lint-version pin all originate here (reference-only).
- `.github/workflows/hermes-auto-merge.yml` — not tag-triggered; listed for collision-check completeness only (reference-only).
- `.github/workflows/refresh-synthetic-manifest.yml` — not tag-triggered; listed for collision-check completeness only (reference-only).
- `.goreleaser.yaml` — new file created by Task 2; this task's `goreleaser-action` step consumes it (dependency, not modified by this task).

## Success Criteria
- `.github/workflows/release.yml` exists, triggers only on `push: tags: 'v*'`, and does not modify or duplicate any trigger already covered by `ci.yml`, `reconcile-module.yml`, `hermes-auto-merge.yml`, or `refresh-synthetic-manifest.yml`.
- The workflow carries a `based_on: .github/workflows/ci.yml` header comment naming exactly what is reused verbatim (runner, Go 1.25 setup).
- The workflow declares `permissions: contents: write`, not `contents: read`.
- The workflow reuses `[self-hosted, gauntlet]`, `actions/checkout@v4`, and `actions/setup-go@v5` (`go-version: '1.25'`, `cache: false`) verbatim from `ci.yml`.
- A `goreleaser/goreleaser-action` step runs `release --clean` against `.goreleaser.yaml`.
- A comment in the workflow (or adjacent doc) documents the `v*` vs `reconcile/v*` tag-filter disjointness, mirroring `reconcile-module.yml`'s inline comment style.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — GitHub Actions workflow file, not Go code.

**Integration Tests:**
- Verify `.github/workflows/release.yml` is valid YAML (`yamllint` or a Python/Node YAML parse) with no syntax errors.
- Confirm the `on.push.tags` filter (`v*`) is disjoint from `reconcile-module.yml`'s `reconcile/v*` filter — a tag matching one pattern must not match the other (e.g. `v1.2.3` fires only `release.yml`; `reconcile/v1.2.3` fires only `reconcile-module.yml`).
- Confirm `permissions.contents` is `write`, not `read`.
- Locally dry-run `goreleaser release --snapshot --clean` (once Task 2's `.goreleaser.yaml` exists) to confirm the workflow's goreleaser invocation is well-formed before any real tag is pushed — do not push a real `v*` tag as part of this task's verification.
- Optional: push a throwaway tag on a fork/branch or use `act`/manual workflow inspection to confirm the job triggers correctly, without publishing a real release against the primary repo.

**Test Files:**
- N/A

## Risk Mitigation
- **First real tag push publishes a public, hard-to-retract GitHub Release.** Do not push a real `vX.Y.Z` tag as part of implementing or verifying this task. Validate via YAML parsing and a local `goreleaser --snapshot --clean` dry run (Task 2) only; the first real tag push requires explicit maintainer confirmation, per `plan.md`'s risk mitigation table.
- **Tag-filter collision with `reconcile-module.yml`.** The bare `v*` filter is deliberately disjoint from `reconcile/v*`; both this task's inline comment and `ci-workflow-reuse.md` document the non-collision, and the integration test above explicitly checks it.
- **Divergent `permissions:` block silently breaking release publishing.** Copying `contents: read` from `ci.yml`/`reconcile-module.yml` verbatim (the normal `based_on` reuse pattern) would cause goreleaser's `release` step to fail when creating the GitHub Release. This is called out explicitly in Step 3 and Success Criteria so it isn't missed by a literal verbatim copy.
- **Go/lint version drift between `ci.yml` and `release.yml`.** Copy `go-version: '1.25'` and `cache: false` verbatim from `ci.yml`, per the `based_on` convention, so a tag that passed CI cannot fail the release build due to toolchain drift.

## Dependencies
- Task 02 (`.goreleaser.yaml` GoReleaser Configuration) — this workflow's `goreleaser-action` step invokes the `.goreleaser.yaml` config Task 2 creates; `release.yml` cannot be meaningfully verified end-to-end until that file exists.

## Definition of Done
- `.github/workflows/release.yml` is created, valid YAML, and matches the Steps/Success Criteria above.
- The workflow's tag trigger, `based_on` header comment, runner/checkout/setup-go reuse, and `permissions: contents: write` divergence are all present and reviewed.
- Disjointness from `reconcile-module.yml`'s tag filter is verified and documented in-line.
- No real `vX.Y.Z` tag has been pushed as a side effect of implementing or testing this task.
- AC4 ("A tag-triggered GitHub Actions workflow builds and publishes a GitHub Release") is satisfied pending Task 2's `.goreleaser.yaml` and a maintainer-confirmed first real tag push.
