## Metadata
- **Plan Type:** infrastructure
- **Last Modified:** 2026-07-12
- **Original Requirements:** [original-requirements.md](original-requirements.md)

## Plan Overview
**Plan Goal:** Give the `atcr` binary a real, tag-driven release process — a documented versioning/tagging convention, build-time version stamping, cross-platform binaries via goreleaser, and a tag-triggered GitHub Actions workflow that publishes a GitHub Release — closing a gap three prior epics (7.3, 16.0, 20.0) each independently hit and deferred.
**Target Users:** The maintainer (Sam Estrin), who currently has no way to cut a versioned, distributable `atcr` release beyond `go install ...@latest` against an untagged `main`.
**Framework/Technology:** Go 1.25 (existing `atcr` CLI), GitHub Actions (`[self-hosted, gauntlet]` runner, reused from `.github/workflows/ci.yml`), goreleaser.

## Objectives
1. Decide and document a versioning/tagging strategy: bare `vX.Y.Z` tags, formalizing `CHANGELOG.md`'s existing epic-number-as-semver convention (currently at `[20.1.0]`, 20+ entries deep, zero tags cut against any of them) — and disjoint from the `reconcile/vX.Y.Z` namespace Epic 8.0 already reserved for the standalone `./reconcile` module.
2. Wire real build-time version stamping so a tagged release build sets both of the two currently-independent version variables discovered in this repo: `internal/version.Version` (drives the leaderboard submission envelope, Epic 10.0) and `cmd/atcr`'s own package-local `version` var (drives `atcr version` / `atcr --version` via `atcrVersion()`) — closing AC2 without any new Go/CLI code.
3. Add a `.goreleaser.yaml` that builds cross-platform binaries from a tag, with the ldflags from Objective 2 baked into the build config.
4. Add a new tag-triggered GitHub Actions workflow (`.github/workflows/release.yml`), `based_on: .github/workflows/ci.yml` per the reuse convention `.github/workflows/reconcile-module.yml` already established, that invokes goreleaser and publishes a GitHub Release.
5. Document the release process (`docs/release-process.md`): what triggers a release, who cuts one, and how — and correct `.planning/specifications/git-strategy.md:36`'s stale "Deploy: Merging to `main` builds production release binaries" line, which describes automation that has never existed in this repo (confirmed via codebase-discovery.json: no goreleaser config, no release workflow, `ci.yml` only lints/tests on push/PR to `main`), to instead state the real, tag-triggered mechanism this plan builds.

## Scope
### In Scope
- A documented `vX.Y.Z` tag convention and its relationship to `CHANGELOG.md`'s existing epic-number-as-semver history.
- `-ldflags` build-time stamping of both `internal/version.Version` and `cmd/atcr`'s local `version` var from the same tag value.
- A new `.goreleaser.yaml` producing cross-platform (`GOOS`/`GOARCH` matrix) binaries and checksums.
- A new `.github/workflows/release.yml` triggered on `push: tags: 'v*'`, running goreleaser and publishing a GitHub Release.
- `docs/release-process.md`.
- A one-line correction to `.planning/specifications/git-strategy.md:36` (stale "merging to `main` builds production release binaries" text) to accurately describe the tag-triggered release mechanism.

### Out of Scope
- Homebrew tap, npm wrapper, Docker image, or other package-manager distribution beyond GitHub Releases — a candidate follow-on epic once this core process is proven.
- Any change to `atcr`'s engine/review behavior — this is packaging/distribution infrastructure only.
- Re-opening Epic 20.0's scope — its `go install`-based install path is unaffected; this epic is what a *future* distribution improvement would build on.
- Retroactively tagging past `CHANGELOG.md` entries (`[1.0.0]` through `[20.1.0]`) — this plan formalizes the convention going forward; backfilling historical tags is not required to satisfy any AC.

## Dependencies and Context
- **Epic 16.0 (Quick Start)** — first identified the missing release automation gap; deferred it here (`.planning/.knowledge/clarifications-16.0_quick_start-Q1.md`).
- **Epic 7.3 (GitHub Action / PR Integration)** — scoped goreleaser/release artifacts out, building its action's binary via `actions/setup-go` + `go build` instead (`.planning/.knowledge/clarifications-7.3_github_action_pr_integration-Q2.md`).
- **Epic 20.0 (Standalone Skill Release)** — descoped "package and distribute the atcr engine" back to documenting the existing `go install` path, pending this epic.
- **Epic 8.0 (Reconciler Library)** — already reserved and uses the `reconcile/vX.Y.Z` tag namespace and established the `based_on: ci.yml` CI-reuse convention this plan's new workflow follows.
- **Epic 10.0 (Model Eval Leaderboard)** — original decision record for `internal/version.Version` (`.planning/.knowledge/clarifications-10.0_model_eval_leaderboard-Q3.md`).

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Generated
- **Estimated Count:** 4 tasks

1. [Task 01: Document the Versioning & Tagging Convention](tasks/task-01-versioning-tagging-strategy.md) — AC1: formalize bare `vX.Y.Z` tags + `CHANGELOG.md`'s epic-number-as-semver convention in `docs/release-process.md`.
2. [Task 02: GoReleaser Configuration with Dual Version Stamping](tasks/task-02-goreleaser-configuration.md) — AC2 + AC3: `.goreleaser.yaml` with dual `-X` ldflags stamping both version variables and a `GOOS`/`GOARCH` matrix.
3. [Task 03: Tag-Triggered Release Workflow](tasks/task-03-tag-triggered-release-workflow.md) — AC4: `based_on: ci.yml` `.github/workflows/release.yml` on `v*` tag push → goreleaser → GitHub Release.
4. [Task 04: Complete Release-Process Documentation](tasks/task-04-release-process-documentation.md) — AC5: append the release-process sections to `docs/release-process.md` and correct `.planning/specifications/git-strategy.md:36`.

**Dependency order:** Task 01 → Task 02 → Task 03 → Task 04 (Task 04 runs last so it documents the finished mechanism built by Tasks 01-03; Task 01's tag convention is the soft contract Task 02's `{{.Version}}`/`{{.Tag}}` derives from).

## Codebase Discovery Highlights
- **Two disconnected version variables, not one.** The epic's own refinement assumed `cmd/atcr/version.go` reads `internal/version.Version`. It does not — `cmd/atcr/version.go:14` defines its own package-local `var version = ""`, resolved by `atcrVersion()` with its own fallback chain (ldflags → `debug.ReadBuildInfo()` module version → VCS revision → `"dev"`). A release build's ldflags must stamp **both** `-X github.com/samestrin/atcr/internal/version.Version=...` and `-X main.version=...` from the same tag, or the CLI's reported version and the leaderboard submission envelope can silently diverge.
- **Tag-namespace precedent already exists and is tested.** `.github/workflows/reconcile-module.yml` triggers only on `reconcile/v*` specifically so it never fires on an ATCR app tag — a deliberate Epic 8.0 design decision, not incidental. The new release workflow must use the disjoint bare `v*` filter.
- **CI-reuse convention.** New tag-triggered workflows in this repo declare `based_on: .github/workflows/ci.yml` in a header comment and copy the `[self-hosted, gauntlet]` runner, `actions/checkout@v4`, and `actions/setup-go@v5` (Go 1.25, `cache: false`) steps verbatim, plus the pinned `golangci-lint` v2.12.2. The new release workflow should follow the same pattern.
- **No prior art for goreleaser, release docs, or an ATCR-app release workflow** — greenfield packaging infrastructure layered onto an established CI convention. Full detail in [codebase-discovery.json](codebase-discovery.json).

## Technical Planning Notes
- **Reused, not rebuilt**: `.github/workflows/ci.yml`'s runner/checkout/Go-setup steps, the repo-root `.golangci.yml` v2.12.2 pin, and the `based_on` comment convention already demonstrated by `reconcile-module.yml`.
- **AC2 requires no new Go/CLI code** — `cmd/atcr/version.go` and `cmd/atcr/main.go:136` (`root.Version: atcrVersion()`) already fully implement `atcr version` / `atcr --version`; the only gap is the build-time ldflags wiring itself, which belongs to the goreleaser task (Objective 2/3 above), not a separate code task.
- **Tag namespace must stay disjoint from `reconcile/vX.Y.Z`** — bare `vX.Y.Z` is free today; verified via `git tag` returning zero tags and `reconcile-module.yml`'s filter excluding it explicitly.
- **CHANGELOG.md's epic-number-as-semver convention is live and current** (most recent entry `[20.1.0]`, matching Epic 20.1) — the versioning decision formalizes this existing, growing history rather than starting an independent counter.

## Documentation References
See [documentation/README.md](documentation/README.md) for the full index.
- [Version & Tagging Strategy](documentation/version-tagging-strategy.md) `[CRITICAL]` — the two independent version variables and the bare `vX.Y.Z` tag convention.
- [GoReleaser Configuration](documentation/goreleaser-configuration.md) `[CRITICAL]` — `.goreleaser.yaml` Go builder config, dual `-X` ldflags, `GOOS`/`GOARCH` matrix.
- [CI Workflow Reuse Convention](documentation/ci-workflow-reuse.md) `[IMPORTANT]` — the `based_on:` convention the new `release.yml` must follow.

## Implementation Strategy
1. **Versioning/tagging convention** — record the bare `vX.Y.Z` convention and its relationship to `CHANGELOG.md`'s existing epic-number-as-semver history and to Epic 8.0's disjoint `reconcile/vX.Y.Z` namespace, as the "Versioning & Tagging Convention" section of `docs/release-process.md` (AC1). This is the same file step 4 builds out, so AC1 and AC5 share one release doc.
2. **goreleaser config with dual ldflags** — author `.goreleaser.yaml` with a `GOOS`/`GOARCH` build matrix and `-ldflags` stamping both `internal/version.Version` and `cmd/atcr`'s local `version` var from the pushed tag (AC2 + AC3).
3. **Tag-triggered release workflow** — author `.github/workflows/release.yml`, `based_on: .github/workflows/ci.yml`, triggered on `push: tags: 'v*'`, invoking goreleaser to build and publish a GitHub Release (AC4).
4. **Release-process documentation** — write `docs/release-process.md` covering what triggers a release, who cuts one, and the exact `git tag vX.Y.Z && git push --tags` (or equivalent) steps, alongside the "Versioning & Tagging Convention" section from step 1 (AC5). Also correct `.planning/specifications/git-strategy.md:36`'s stale merge-to-main deploy line to describe the real tag-triggered mechanism, so the spec doesn't keep asserting non-existent automation after this plan ships.

## Recommended Packages
goreleaser (invoked via `goreleaser/goreleaser-action` in CI — see [package-recommendations.md](package-recommendations.md) for full detail; not a go.mod dependency).

## Success Criteria
- A versioning/tagging strategy is decided and documented (bare `vX.Y.Z`, formalizing `CHANGELOG.md`'s epic-number-as-semver convention, disjoint from `reconcile/vX.Y.Z`).
- `atcr version` / `atcr --version` reflects the real tagged version at build time, and the leaderboard submission envelope's `internal/version.Version` reflects the same value from the same build.
- `.goreleaser.yaml` exists and produces cross-platform binaries from a tag push.
- A tag-triggered `.github/workflows/release.yml` builds and publishes a GitHub Release.
- `docs/release-process.md` documents the end-to-end release process.

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Release workflow's tag filter collides with `reconcile-module.yml`'s `reconcile/v*` filter, double-firing or misfiring on the wrong tag | Low | Medium | Use the disjoint bare `v*` filter (no `reconcile/` prefix); document the disjointness explicitly in both workflows' header comments |
| Only one of the two version variables gets stamped, leaving the CLI and the leaderboard envelope reporting different versions from the same release | Medium | Medium | Explicitly enumerate both `-X` ldflags targets in `.goreleaser.yaml` and verify both in a local `goreleaser release --snapshot --clean` dry run before the first real tag |
| First real tag publishes a public, externally-visible GitHub Release that is hard to fully retract once users pull it | Low | High | Treat the first real release cut with the same care as any other externally-visible, difficult-to-undo action — dry-run via `--snapshot` first, confirm with the maintainer before pushing the first real tag |
| golangci-lint or Go-version drift between `ci.yml`/`reconcile-module.yml` and the new `release.yml` causes a tag to pass CI but fail the release build (or vice versa) | Low | Medium | Copy the pinned `golangci-lint` v2.12.2 and Go 1.25 setup verbatim from `ci.yml`, per the `based_on` convention |

## Next Steps
1. `/create-tasks @.planning/plans/active/21.0_release_packaging_automation/`
2. `/design-sprint @.planning/plans/active/21.0_release_packaging_automation/`
3. `/create-sprint @.planning/plans/active/21.0_release_packaging_automation/`
4. `/execute-sprint`
