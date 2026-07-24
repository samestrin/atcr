# Release Process

How `atcr` binaries are versioned, tagged, built, and published.

> **Status:** This document is built up across Sprint 21.0. The
> **Versioning & Tagging Convention** below is the decided foundation; the
> release-cutting procedure (what triggers a release, who cuts one, and the
> exact commands) is documented in later sections added by Task 04.

## Versioning & Tagging Convention

### The convention

A release is cut by pushing a **bare `vX.Y.Z` git tag** — no prefix, no
suffix (e.g. `v0.1.0`, not `release/v0.1.0` or `atcr-v0.1.0`).

App release tags use **standard, independent semantic versioning starting at
`v0.1.0`**, decoupled from the epic/sprint numbers that head
[`CHANGELOG.md`](../CHANGELOG.md).

| Release                              | Git tag  |
|--------------------------------------|----------|
| First tagged release (Sprint 34.0)   | `v0.1.0` |
| Subsequent patch / minor (pre-1.0)   | `v0.1.1` / `v0.2.0` / … |

**Why not epic-number-as-semver (the previous convention):** earlier drafts of
this doc mapped each tag onto the matching `CHANGELOG.md` heading (`## [20.1.0]`
→ `v20.1.0`), an "epic-number-as-semver" scheme. That is now **invalid for a
tagged release**: as of Sprint 34.0 the `atcr` command tree is an importable Go
module (`github.com/samestrin/atcr/cli`, consumed by the private
`atcr-enterprise` repo). Under Go semantic-import versioning, any tag with a
**major version ≥ 2** (`v20.1.0`, `v33.2.0`, …) requires a matching `/vN` suffix
on the module path — which `github.com/samestrin/atcr` does not have — so such a
tag cannot be `require`d by another module (`go` rejects it). The tag series
therefore restarts at `v0.1.0` and stays in the import-compatible `v0.x` / `v1.x`
range. Reaching `v2.0.0` later would require adding a `/v2` module-path suffix
repo-wide — a deliberate, separate decision, not an automatic bump.

The `CHANGELOG.md` epic/sprint headings (`## [1.0.0]` … `## [33.2.0]`) remain as
the historical **development** changelog; they are no longer 1:1 with git tags.

### Forward-only — no retroactive backfill

**No git tag was ever cut** against any of the 20+ historical changelog entries
(`git tag -l 'v*'` returned nothing before `v0.1.0`). The convention is
**forward-only**: the tag series starts fresh at `v0.1.0` (Sprint 34.0), and the
past epic-numbered changelog entries are **not** retroactively tagged. There is
intentionally no `v1.0.0 … v33.2.0` backfill.

### Disjoint from the `reconcile/vX.Y.Z` module namespace

Bare `vX.Y.Z` is free to use as the `atcr` **app** release namespace because
Epic 8.0 deliberately reserved a *separate* namespace for the standalone
`./reconcile` module. [`.github/workflows/reconcile-module.yml`](../.github/workflows/reconcile-module.yml)
scopes its release gate to `reconcile/v*` tags specifically so that app tags
never fire it (lines 14-16):

```yaml
      # Scoped to the module's release-tag convention so ATCR app tags (e.g.
      # v1.2.3) do NOT trigger this release gate. Only reconcile/vX.Y.Z fires it.
      - 'reconcile/v*'
```

The two namespaces are provably disjoint: no tag can match both `v*` and
`reconcile/v*`. An `atcr` app release (`v0.1.0`) triggers only the app release
workflow; a module release (`reconcile/v1.2.3`) triggers only the module gate.

### Build-time stamping contract (implemented by Task 02)

A single pushed tag stamps **two independent version variables** at link time
via goreleaser `-ldflags`. This section only *declares* the contract; the
`.goreleaser.yaml` that fulfills it is added in Task 02.

| Variable | Location | Value stamped from the tag `vX.Y.Z` | Rationale |
|----------|----------|--------------------------------------|-----------|
| `github.com/samestrin/atcr/cli.version` | [`cli/version.go`](../cli/version.go) | **`vX.Y.Z`** (v-prefixed) | `atcr --version` / `atcr version` reports `vX.Y.Z`, matching a `go install github.com/samestrin/atcr/cmd/atcr@vX.Y.Z` build. (The `version` var moved from package `main` in `cmd/atcr` into the importable `cli` package in Sprint 34.0 Task 03; the `-ldflags -X` target moved with it.) |
| `github.com/samestrin/atcr/internal/version.Version` | [`internal/version/version.go`](../internal/version/version.go) | **`X.Y.Z`** (v-stripped) | The public leaderboard submission envelope (Epic 10.0) reports the bare `X.Y.Z` form; it currently defaults to the neutral `"0.0.0"` placeholder. |

Both targets **agree on the numeric `X.Y.Z`** portion of the tag — the leading
`v` prefix is the only permitted difference between them. A tag of `v0.1.0`
therefore yields `cli.version = v0.1.0` and `internal/version.Version =
0.1.0`.

## What Triggers a Release

A release is triggered by **pushing a bare `vX.Y.Z` git tag** to the
repository — nothing else. That tag push fires
[`.github/workflows/release.yml`](../.github/workflows/release.yml) (scoped to
`push: tags: ['v*']`, disjoint from the module's `reconcile/v*` namespace as
described above), which runs goreleaser against
[`.goreleaser.yaml`](../.goreleaser.yaml) to cross-compile the binaries and
publish a GitHub Release.

**Merging a PR to `main` does _not_ produce a release.** The only CI on `main`
is [`ci.yml`](../.github/workflows/ci.yml), which formats, vets, lints, and
tests — it never builds or publishes a release. A release happens only when a
maintainer explicitly pushes a `vX.Y.Z` tag.

## Who Cuts a Release

`atcr` currently has a single maintainer, Sam Estrin, and cutting a release is a
solo maintainer decision — there is no formal release-manager rotation or
multi-party approval step. Whoever holds the maintainer role inherits this
responsibility as-is.

## Cutting a Release

Install goreleaser locally first (`go install github.com/goreleaser/goreleaser/v2@latest` or `brew install goreleaser`) — the project uses the v2 configuration line, matching [`.goreleaser.yaml`](../.goreleaser.yaml) and [`.github/workflows/release.yml`](../.github/workflows/release.yml).

1. **Pick the next semver tag.** Per the convention above, app tags are
   independent semver in the `v0.x` / `v1.x` range (starting at `v0.1.0`), **not**
   the epic/sprint number. Increment from the latest `v*` tag
   (`git tag -l 'v*' | sort -V | tail -1`) per the change's scope
   (patch/minor/major, staying `< v2` unless the module path gains a `/vN`
   suffix). Add or update the corresponding `CHANGELOG.md` release notes for the
   cut; the changelog heading no longer has to equal the tag number.

2. **Dry-run locally first (non-optional — run it for every cut).** From an
   up-to-date `main`, run:

   ```sh
   goreleaser release --snapshot --clean
   ```

   This builds the full cross-platform matrix into `dist/` **without** pushing a
   tag or publishing anything. Confirm the build succeeds and that both `-X`
   ldflags targets resolve and agree with each other — `cli.version`
   (`github.com/samestrin/atcr/cli`) reports the v-prefixed form and
   `internal/version.Version` reports the v-stripped form. Only a real tag build stamps the exact `vX.Y.Z`; the snapshot verifies
   the mechanism, not the final release number. The first real tag publishes a **public,
   hard-to-retract** GitHub Release, so this dry run is a required step, not a
   convenience.

3. **Cut the real tag.** From an up-to-date `main`:

   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

   Substitute the actual version being released. Push **only** that single tag
   by name — do _not_ use `git push --tags`, which pushes every local tag at
   once and could fire an unintended release (or the disjoint `reconcile/v*`
   module gate).

4. **Let the workflow publish.** The tag push fires
   [`release.yml`](../.github/workflows/release.yml), which runs
   `goreleaser release --clean` and publishes the GitHub Release automatically —
   no further manual step is required. Watch the workflow run to confirm it
   succeeds.

## Cutting a Reconcile Module Release

The nested `./reconcile` module (extracted in Epic 8.0) is released in its own
`reconcile/vX.Y.Z` tag namespace — **disjoint** from the app's bare `vX.Y.Z`
namespace described above (a tag can never match both `v*` and `reconcile/v*`).
For the app release, see [Cutting a Release](#cutting-a-release).

Unlike the app release, there is **no goreleaser / GitHub Release step** for the
module: the tag push plus its CI gate *is* the entire release. The tag makes the
module fetchable through the public Go proxy; consuming code pins it via a normal
`require github.com/samestrin/atcr/reconcile vX.Y.Z` in the root `go.mod`.

**Precondition — the repository must be public.** `proxy.golang.org` returns 404
for a private module, so `go install`/`go mod download` cannot resolve
`github.com/samestrin/atcr/reconcile` until the repo is public (see Sprint 33.2 /
TD-002). Do not cut a module tag against a private repo.

1. **Confirm the module builds clean and `reconcile/go.mod` is unchanged.** From
   an up-to-date `main`:

   ```sh
   ( cd reconcile && go build ./... && go test ./... )
   ```

   The tagged content is the `reconcile/` subtree at the tagged commit; verify it
   is the intended state before tagging.

2. **Cut the module tag.** From the commit whose `reconcile/` subtree you are
   publishing:

   ```sh
   git tag reconcile/v0.1.1
   git push origin reconcile/v0.1.1
   ```

   Substitute the actual version. Push **only** that single tag by name — do
   _not_ use `git push --tags`, which would also push every app `v*` tag and fire
   the app release workflow.

3. **Let the module CI gate run.** The `reconcile/v*` tag push fires
   [`reconcile-module.yml`](../.github/workflows/reconcile-module.yml) on the
   `[self-hosted, gauntlet]` runner (`gofmt`, `golangci-lint`, `go test -race`
   inside `./reconcile`). Confirm it is green with no `could not read Username`
   or other module-fetch/auth error. That green run is the module release —
   there is no further publish step.

4. **(When bumping the consumed version) pin the new version in the root module.**
   Update `require github.com/samestrin/atcr/reconcile vX.Y.Z` in the root
   `go.mod`, run `go mod tidy` so `go.sum` records the real `h1:` hashes from the
   proxy, and land the change through a normal PR to `main` (never a direct
   push). The root [Go CI](../.github/workflows/ci.yml) run on that PR verifies
   the whole repo builds against the real tagged module rather than a local
   `replace`.
