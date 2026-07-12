# Sprint Design: Release & Packaging Automation

**Created:** July 12, 2026
**Plan:** [Release & Packaging Automation](.)
**Plan Type:** 🏗️ Infrastructure
**Status:** Design Complete

---

## Original User Request

> Every epic that touches distribution keeps re-encountering the same missing infrastructure and re-punting, because standing up release automation is out of scope for whatever feature epic happens to need it first. The decision keeps getting deferred, never made — this epic exists to break that loop by giving release/packaging automation its own scoped, standalone plan.
>
> 1. Decide and implement a versioning/tagging strategy that `internal/version` reads at build time, replacing the current `"0.0.0"` placeholder. Bare `vX.Y.Z` tags are the convention to use.
> 2. Add a goreleaser config (or equivalent) that produces cross-platform binaries from a tag.
> 3. Add a tag-triggered GitHub Actions workflow that builds and publishes a GitHub Release.
> 4. Document the release process (what triggers a release, who cuts one, how).

**Referenced Resources:**
- [Version & Tagging Strategy](documentation/version-tagging-strategy.md)
  - **Summary**: Documents the two independent, unstamped version variables (`internal/version.Version` and `cmd/atcr`'s package-local `version`) and formalizes `CHANGELOG.md`'s existing epic-number-as-semver history into bare `vX.Y.Z` git tags.
  - **Key Points**: Zero git tags currently exist against 20+ changelog entries; bare `vX.Y.Z` is proven-free (disjoint from Epic 8.0's `reconcile/vX.Y.Z`) via `reconcile-module.yml`'s own scoping comment; both version vars must be stamped from the same tag value in one build.
- [GoReleaser Configuration](documentation/goreleaser-configuration.md)
  - **Summary**: Explains how `.goreleaser.yaml`'s `builds.ldflags` list must carry two `-X` entries — one per independent version variable — rather than the single-target default most goreleaser examples assume.
  - **Key Points**: Config is declarative, not a `go.mod` dependency; default `GOOS`/`GOARCH` matrix (`darwin,linux,windows` × `386,amd64,arm64`) needs no override; `{{.Date}}` default ldflag should be reconsidered for reproducible builds.
- [CI Workflow Reuse Convention](documentation/ci-workflow-reuse.md)
  - **Summary**: Documents the repo's `based_on:` header-comment convention for reusing runner/checkout/Go-setup/lint-pin steps verbatim across workflow files, and the one deliberate exception (`permissions: contents: write`) a release workflow must take.
  - **Key Points**: Only `reconcile-module.yml` is currently tag-triggered (scoped to `reconcile/v*`); the new `release.yml` on bare `v*` collides with nothing; `[self-hosted, gauntlet]` + `actions/checkout@v4` + `actions/setup-go@v5` (Go 1.25, `cache: false`) must be copied verbatim.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Release Packaging Automation
**Complexity:** 7/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Pattern:** Foundation → Core Implementation → Integration → Documentation → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
goreleaser Go binary release automation
tag-triggered GitHub Actions release workflow
dual ldflags version stamping Go
semver git tag convention CHANGELOG
CI workflow based_on reuse pattern
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - New tooling (goreleaser) is introduced, but the workflow itself follows the repo's established `based_on: ci.yml` reuse convention verbatim; no new Go code or package patterns.
- **Integration:** 2/3 - Touches 3+ systems: two independent version variables, the existing CI/tag-namespace ecosystem (must stay disjoint from `reconcile-module.yml`), `CHANGELOG.md`'s versioning convention, and the external GitHub Releases API.
- **Story/Task & Test:** 1/3 - 4 tasks, each rated Effort:S by `/create-tasks`, with no unit-test surface (all four tasks' Test Strategy sections are N/A for unit/integration tests) — verification is dry-run/lint-based, not TDD.
- **Risk/Unknowns:** 3/3 - The `{{.Version}}` vs `{{.Tag}}` ldflags-prefix choice is an explicitly unresolved design decision (task-02 flags it as "a decision to make deliberately, not by default"), and the first real tag push publishes a public, hard-to-retract GitHub Release — both called out by name in plan.md's own risk table.

**Time Formula:** COMPLEX baseline is 8-12 days; floor selected because all four tasks are individually Effort:S with no new Go code — the COMPLEX classification is driven by integration breadth and irreversible-action risk, not implementation size.
**Calculation:** 4 tasks × ~2 days average (author + verify each artifact) = 8 days across 5 phases.

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** standard (not strong)
**Suggested command:** `/create-sprint @.planning/plans/active/21.0_release_packaging_automation/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 (7/12, 5 phases — both trip); gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days (phase count and duration both trip; complexity alone does not); strong gated at complexity >= 10/12 (not met).

---

## Phase Structure

### Phase 1: Foundation — Versioning & Tagging Convention (~1.5 days)
- **Item:** Task 01 — Document the Versioning & Tagging Convention
- **Focus:** Create `docs/release-process.md` with the `## Versioning & Tagging Convention` section: bare `vX.Y.Z` tags, mapping to `CHANGELOG.md`'s epic-number-as-semver history, and explicit disjointness from Epic 8.0's `reconcile/vX.Y.Z` namespace. Establishes the contract Phase 2 implements. Documentation-only — no code/config/workflow changes.

### Phase 2: Core Implementation — GoReleaser Config + Dual Ldflags (~2.5 days)
- **Item:** Task 02 — GoReleaser Configuration with Dual Version Stamping
- **Focus:** Author `.goreleaser.yaml` with a `builds:` block stamping both `internal/version.Version` and `cmd/atcr`'s local `version` var via two `-X` ldflags entries. Resolve the `{{.Version}}` vs `{{.Tag}}` v-prefix decision deliberately (Risk/Unknowns driver). Verify via local `goreleaser release --snapshot --clean` dry run; confirm both stamped values agree on the numeric `X.Y.Z` portion.

### Phase 3: Integration — Tag-Triggered Release Workflow (~1.5 days)
- **Item:** Task 03 — Tag-Triggered Release Workflow
- **Focus:** Author `.github/workflows/release.yml`, `based_on: ci.yml`, triggered on `push: tags: ['v*']`, with `permissions: contents: write` (the deliberate divergence from the base workflows' `contents: read`) and a `goreleaser/goreleaser-action@v6` step. Verify tag-filter disjointness from `reconcile-module.yml` and valid YAML. No real tag pushed.

### Phase 4: Documentation — Release-Process Docs + Spec Correction (~1.5 days)
- **Item:** Task 04 — Complete Release-Process Documentation
- **Focus:** Append `## What Triggers a Release`, `## Who Cuts a Release`, and `## Cutting a Release` to `docs/release-process.md` (built on Phase 1's section). Correct `.planning/specifications/git-strategy.md:36`'s stale "merging to `main` builds production release binaries" claim to describe the real tag-triggered mechanism.

### Phase 5: Validation (~1 day)
- **Items:** Cross-task Definition of Done verification
- **Focus:** Re-run `goreleaser release --snapshot --clean`; confirm `docs/release-process.md` reads coherently end-to-end across all four sections; confirm documented commands match the actual `.goreleaser.yaml`/`release.yml` produced; confirm `git-strategy.md` correction is a single, self-contained line change; confirm no real `vX.Y.Z` tag was pushed as a side effect of implementation.

---

## Work Decomposition

Grounded in the existing `tasks/` directory (WORK_ITEM_SOURCE = tasks) — no re-scoping performed.

### Task 01: Document the Versioning & Tagging Convention (AC1)
- **Testable elements:** `docs/release-process.md` exists with `## Versioning & Tagging Convention`; section states bare `vX.Y.Z` with a `CHANGELOG.md` example; documents disjointness from `reconcile/vX.Y.Z`; notes forward-only application (no retroactive tagging); references (without implementing) the dual-ldflags contract.
- **Test type:** Manual doc review against Success Criteria checklist — N/A for automated tests.
- **Dependencies:** None.

### Task 02: GoReleaser Configuration with Dual Version Stamping (AC2, AC3)
- **Testable elements:** `.goreleaser.yaml` valid YAML with a `builds:` block containing both `-X github.com/samestrin/atcr/internal/version.Version={{.Version}}` and `-X main.version={{.Version}}` (or the deliberately-chosen `{{.Tag}}` variant); `goreleaser release --snapshot --clean` completes; built binary's `atcr version`/`--version` matches the snapshot version; `internal/version.Version` confirmed stamped to the same numeric value via binary inspection or throwaway build.
- **Test type:** Local dry-run integration test (`goreleaser --snapshot --clean`), not `go test`.
- **Dependencies:** Task 01 (tag-convention contract, not a hard code dependency).

### Task 03: Tag-Triggered Release Workflow (AC4)
- **Testable elements:** `.github/workflows/release.yml` valid YAML; triggers only on `push: tags: 'v*'`; `based_on: ci.yml` header comment present; `permissions: contents: write`; reuses `[self-hosted, gauntlet]`/`actions/checkout@v4`/`actions/setup-go@v5` verbatim; `goreleaser-action` step present; tag-filter disjointness from `reconcile-module.yml` verified.
- **Test type:** YAML parse/lint (`yamllint` or Python `yaml.safe_load`) + manual trigger-filter comparison — no real tag pushed.
- **Dependencies:** Task 02 (`.goreleaser.yaml` must exist for the workflow to be meaningfully verified end-to-end).

### Task 04: Complete Release-Process Documentation (AC5)
- **Testable elements:** `docs/release-process.md` contains all four sections (Versioning & Tagging Convention, What Triggers a Release, Who Cuts a Release, Cutting a Release); `.planning/specifications/git-strategy.md:36` no longer claims merge-triggered releases and links to the new doc.
- **Test type:** Manual doc review + cross-check documented commands against the real `.goreleaser.yaml`/`release.yml`.
- **Dependencies:** Task 01 (file/section to append to), Task 02 (`.goreleaser.yaml` to document), Task 03 (`release.yml` to document).

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** N/A — this plan produces no new Go code (`cmd/atcr/version.go` and `internal/version/version.go` are read, not modified); verification is config/doc-centric, not `go test`-based.

**Test File Placement Examples:** None — no `*_test.go` files are created or modified by this plan.

**Unit/Integration/E2E:**
- Unit: N/A across all 4 tasks (declarative YAML/config + documentation only).
- Integration: `goreleaser release --snapshot --clean` local dry run (Task 02, re-verified in Phase 5) is the closest analog — it exercises the full cross-platform build + dual-ldflags stamping path without publishing.
- E2E: Deliberately deferred — no real `vX.Y.Z` tag is pushed as part of this sprint. The first real tag push is an explicit, out-of-sprint maintainer action gated on Phase 5's Definition of Done.

**Test Environment Status:**
- Framework: `go test ./...` (existing project test command) — not exercised by this plan's changes; existing coverage baseline (80%) is unaffected since no `.go` files change.
- Execution: `goreleaser release --snapshot --clean` (local CLI, non-publishing) is the primary verification mechanism for Task 02/Phase 5.
- Coverage Tools: N/A — no code coverage impact; `yamllint`/`yaml.safe_load` covers workflow-file syntax validation for Task 03.

---

## Architecture

**Primitives:** git tag string (`vX.Y.Z`), two independent Go version variables (`internal/version.Version`, `cmd/atcr`'s package-local `version`), `.goreleaser.yaml` build spec, GitHub Release artifact (binaries + checksums + notes).

**Module Boundaries:** `.goreleaser.yaml` and `.github/workflows/release.yml` are fully declarative and external to Go source — no new Go packages, interfaces, or exported symbols. `docs/release-process.md` and the `git-strategy.md` correction are documentation-only.

**External Dependencies:** goreleaser (CLI + `goreleaser/goreleaser-action@v6`, invoked only in CI/local dry-run — not a `go.mod` dependency, per package-recommendations.md); GitHub Actions; GitHub Releases API (via goreleaser, using the default `GITHUB_TOKEN` — no new secrets).

**Replaceability:** The release mechanism (goreleaser config + workflow) is fully swappable independent of application code — neither `cmd/atcr/version.go` nor `internal/version/version.go` requires modification; only their build-time `-ldflags` stamped values are affected, so an equivalent tool could replace goreleaser later without touching CLI code.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| `release.yml` write permissions | `permissions: contents: write` (Task 03) | A workflow with write permissions is a higher-value target than the base read-only workflows; malicious tag push if branch/tag protection is weak | Trigger scoped to tag push only (never `pull_request`), runs on the trusted `[self-hosted, gauntlet]` runner, uses the default per-run scoped `GITHUB_TOKEN` (no new long-lived secrets introduced) |
| `goreleaser-action` supply chain | Third-party GitHub Action dependency (Task 03) | Unpinned or drifting action version could pull compromised release-time code with write access to the repo | Pin to a specific major version tag (`@v6`) per task instructions; verify action provenance before pinning |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Cross-platform build matrix | One CI run per tag push, 9 `GOOS`/`GOARCH` combinations (default matrix) | Complete within the `[self-hosted, gauntlet]` runner's normal CI budget | Use goreleaser defaults with no custom optimization initially; monitor build duration on the first real tag push and narrow the matrix only if a specific combination proves problematic |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Tag-namespace collision | A tag is pushed matching both `v*` and `reconcile/v*` patterns | Impossible by construction (disjoint prefixes); Task 03's Integration Tests explicitly verify `v1.2.3` fires only `release.yml` and `reconcile/v1.2.3` fires only `reconcile-module.yml` |
| Version-prefix mismatch | `{{.Version}}` (v-stripped) vs `{{.Tag}}` (v-prefixed) stamped inconsistently across the two ldflags targets | Both targets must agree on the numeric `X.Y.Z` portion; Task 02 Step 4's snapshot dry run explicitly confirms this before Phase 3 begins |
| `goreleaser` CLI unavailable locally | A developer verifies Task 02 without goreleaser installed | Documented install path (`go install .../goreleaser/v2@latest` or `brew install goreleaser`); does not block authoring/committing `.goreleaser.yaml` itself |
| First real tag push | Maintainer cuts the first real release after this sprint completes | Must be preceded by a local `--snapshot --clean` dry run and explicit maintainer confirmation, per plan.md's risk table and Task 04's documented "Cutting a Release" procedure — deliberately kept outside this sprint's scope |

### Defensive Measures Required

- **Input Validation:** N/A — no user-facing input; tag format is enforced structurally by the workflow's `push: tags: ['v*']` glob filter, not application code.
- **Error Handling:** No custom error-handling code is introduced; goreleaser's own build-step failures fail the CI job naturally (non-zero exit).
- **Logging/Audit:** GitHub Actions run logs are the audit trail for each release invocation; the `CHANGELOG.md` entry timestamp correlates with the corresponding tag per Task 01's documented convention.
- **Rate Limiting:** N/A — tag pushes are a manual, infrequent maintainer action.
- **Graceful Degradation:** N/A — a release either succeeds (full GitHub Release published) or the CI job fails outright; there is no partial-release state to reconcile.

---

## Risks

**Technical:**
- Release workflow's tag filter collides with `reconcile-module.yml`'s `reconcile/v*` filter → Disjoint bare `v*` filter, documented in both workflows' header comments (Task 03).
- Only one of the two version variables gets stamped, CLI and leaderboard envelope diverge → Both `-X` ldflags targets enumerated in `.goreleaser.yaml`; verified via local snapshot dry run (Task 02).
- golangci-lint/Go-version drift between `ci.yml`/`reconcile-module.yml` and `release.yml` → Copy pinned golangci-lint v2.12.2 + Go 1.25 setup verbatim, per `based_on` convention (Task 03).

**TDD-Specific:**
- No unit-test surface exists for this plan's artifacts (config/YAML/docs) → Substitute automated verification with `goreleaser --snapshot --clean` dry runs and YAML parse/lint checks at the task level (Tasks 02-03) and a cumulative cross-check in Phase 5, so "Definition of Done" still means something concrete despite the absence of `go test` coverage.
- First real tag push is irreversible and externally visible, and is explicitly out of this sprint's scope → Phase 5's Definition of Done stops short of pushing a real tag; Task 04 documents the maintainer-confirmed cutover procedure as a deliberate handoff, not a sprint deliverable.

---

**Next:** `/create-sprint @.planning/plans/active/21.0_release_packaging_automation/ --gated`
