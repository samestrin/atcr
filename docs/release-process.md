# Release Process

How `atcr` binaries are versioned, tagged, built, and published.

> **Status:** This document is built up across Sprint 21.0. The
> **Versioning & Tagging Convention** below is the decided foundation; the
> release-cutting procedure (what triggers a release, who cuts one, and the
> exact commands) is documented in later sections added by Task 04.

## Versioning & Tagging Convention

### The convention

A release is cut by pushing a **bare `vX.Y.Z` git tag** — no prefix, no
suffix (e.g. `v21.0.0`, not `release/v21.0.0` or `atcr-v21.0.0`).

Each tag maps one-to-one onto the matching [`CHANGELOG.md`](../CHANGELOG.md)
entry: the tag is the changelog heading with a `v` prepended.

| `CHANGELOG.md` heading      | Git tag   |
|-----------------------------|-----------|
| `## [20.1.0]` (latest today) | `v20.1.0` |
| `## [21.0.0]` (next, anticipated) | `v21.0.0` |

This formalizes an **epic-number-as-semver** scheme the changelog has already
followed, unbroken, from its first entry (`## [1.0.0]`) through the current
`## [20.1.0]` in [`CHANGELOG.md`](../CHANGELOG.md): each entry is versioned
`MAJOR.MINOR.0` to match the epic that produced it. The tags formalize this
existing history rather than starting an independent counter that would orphan
20+ releases' worth of version numbers.

### Forward-only — no retroactive backfill

Although the changelog carries 20+ versioned entries, **no git tag has ever
been cut** against any of them (`git tag -l 'v*'` returns nothing). The
convention is **forward-only**: the first real tag is cut at or after the epic
that stands this process up, and past changelog entries are **not**
retroactively tagged. There is intentionally no `v1.0.0 … v20.1.0` backfill.

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
`reconcile/v*`. An `atcr` app release (`v21.0.0`) triggers only the app release
workflow; a module release (`reconcile/v1.2.3`) triggers only the module gate.

### Build-time stamping contract (implemented by Task 02)

A single pushed tag stamps **two independent version variables** at link time
via goreleaser `-ldflags`. This section only *declares* the contract; the
`.goreleaser.yaml` that fulfills it is added in Task 02.

| Variable | Location | Value stamped from the tag `vX.Y.Z` | Rationale |
|----------|----------|--------------------------------------|-----------|
| `main.version` | [`cmd/atcr/version.go:14`](../cmd/atcr/version.go) | **`vX.Y.Z`** (v-prefixed) | `atcr --version` / `atcr version` reports `vX.Y.Z`, matching a `go install github.com/samestrin/atcr/cmd/atcr@vX.Y.Z` build. |
| `internal/version.Version` | [`internal/version/version.go:16`](../internal/version/version.go) | **`X.Y.Z`** (v-stripped) | The public leaderboard submission envelope (Epic 10.0) reports the bare `X.Y.Z` form; it currently defaults to the neutral `"0.0.0"` placeholder. |

Both targets **agree on the numeric `X.Y.Z`** portion of the tag — the leading
`v` prefix is the only permitted difference between them. A tag of `v21.0.0`
therefore yields `main.version = v21.0.0` and `internal/version.Version =
21.0.0`.

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

1. **Confirm the `CHANGELOG.md` entry exists.** Per the convention above, the
   tag value is the changelog heading with a `v` prepended — e.g. a
   `## [21.0.0]` heading in [`CHANGELOG.md`](../CHANGELOG.md) is released as tag
   `v21.0.0`. Do not cut a tag that has no matching changelog entry.

2. **Dry-run locally first (non-optional — run it for every cut).** From an
   up-to-date `main`, run:

   ```sh
   goreleaser release --snapshot --clean
   ```

   This builds the full cross-platform matrix into `dist/` **without** pushing a
   tag or publishing anything. Confirm the build succeeds and that both `-X`
   ldflags targets resolve and agree on the numeric `X.Y.Z` — `main.version`
   (printed by `atcr --version`) and `internal/version.Version` (the leaderboard
   submission envelope). The first real tag publishes a **public,
   hard-to-retract** GitHub Release, so this dry run is a required step, not a
   convenience.

3. **Cut the real tag.** From an up-to-date `main`:

   ```sh
   git tag v21.0.0
   git push origin v21.0.0
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
