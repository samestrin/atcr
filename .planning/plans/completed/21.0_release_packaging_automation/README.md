## Overview
Plan 21.0 gives the `atcr` binary a real, tag-driven release process: a documented `vX.Y.Z` versioning/tagging convention (formalizing `CHANGELOG.md`'s existing epic-number-as-semver history), build-time version stamping across both of the repo's two currently-independent version variables, a `.goreleaser.yaml` producing cross-platform binaries, and a tag-triggered GitHub Actions workflow publishing a GitHub Release. This closes a gap three prior epics (7.3, 16.0, 20.0) each independently hit and deferred to this standalone plan.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/21.0_release_packaging_automation/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/21.0_release_packaging_automation/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/21.0_release_packaging_automation/`
- [ ] **Execute Sprint** - `/execute-sprint`

## Timeline & Milestones
| Phase | Deliverables |
|-------|--------------|
| Phase 1: Versioning/Tagging Strategy | Documented bare `vX.Y.Z` convention, formalizing `CHANGELOG.md`'s epic-number-as-semver history, disjoint from Epic 8.0's `reconcile/vX.Y.Z` |
| Phase 2: goreleaser Config + Dual ldflags | `.goreleaser.yaml` with `GOOS`/`GOARCH` matrix, stamping both `internal/version.Version` and `cmd/atcr`'s local `version` var |
| Phase 3: Tag-Triggered Release Workflow | `.github/workflows/release.yml`, `based_on: ci.yml`, triggered on `push: tags: 'v*'` |
| Phase 4: Release-Process Documentation | `docs/release-process.md` |

## Resource Requirements
- **Personnel**: 1 maintainer (Sam)
- **Tools**: GitHub Actions (`[self-hosted, gauntlet]` runner, already provisioned), goreleaser (new, CI-invoked only — no go.mod dependency)
- **External Dependencies**: None new to `go.mod`
- **Testing**: Existing `go test ./...` / `gofmt` / `golangci-lint` gate (reused, unmodified); local `goreleaser release --snapshot --clean` dry run before the first real tag

## Expected Outcomes
1. **A real distribution path beyond `go install ...@latest` against untagged `main`** — tagged, versioned releases with downloadable cross-platform binaries.
2. **A single, unambiguous version story** — `atcr version`/`--version` and the leaderboard submission envelope report the same value from the same tagged build, closing a discrepancy the codebase discovery surfaced (they are currently two independent variables).
3. **No more re-punting** — the three epics (7.3, 16.0, 20.0) that each deferred this exact work now have a release process to build on.
4. **A documented, repeatable process** — anyone (not just whoever last touched CI) can look up what triggers a release and how to cut one.

## Risk Summary
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Release workflow's tag filter collides with `reconcile-module.yml`'s `reconcile/v*` filter | Low | Medium | Disjoint bare `v*` filter, documented in both workflows |
| Only one of the two version variables gets stamped, CLI and leaderboard envelope diverge | Medium | Medium | Both `-X` ldflags targets enumerated in `.goreleaser.yaml`; verified via local snapshot dry run |
| First real tag publishes a public, hard-to-retract GitHub Release | Low | High | Snapshot dry run first; confirm with maintainer before the first real tag push |
| CI tooling drift between `ci.yml`/`reconcile-module.yml` and the new `release.yml` | Low | Medium | Copy pinned golangci-lint v2.12.2 + Go 1.25 setup verbatim, per `based_on` convention |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Package Recommendations](package-recommendations.md)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)

## Documentation References
See [documentation/README.md](documentation/README.md) for the full index.
- [Version & Tagging Strategy](documentation/version-tagging-strategy.md) `[CRITICAL]`
- [GoReleaser Configuration](documentation/goreleaser-configuration.md) `[CRITICAL]`
- [CI Workflow Reuse Convention](documentation/ci-workflow-reuse.md) `[IMPORTANT]`

## Related Epics
- **Epic 16.0 (Quick Start)**: first identified the missing release-automation gap and deferred it here.
- **Epic 7.3 (GitHub Action / PR Integration)**: scoped goreleaser/release artifacts out of its own build, deferred here.
- **Epic 20.0 (Standalone Skill Release)**: descoped "package and distribute the atcr engine" back to `go install` docs, pending this plan.
- **Epic 8.0 (Reconciler Library)**: established the `reconcile/vX.Y.Z` tag namespace and the `based_on: ci.yml` CI-reuse convention this plan follows.
- **Epic 10.0 (Model Eval Leaderboard)**: original decision record for `internal/version.Version`.
