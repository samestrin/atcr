# CI Workflow Reuse Convention

`[IMPORTANT]`

## Overview

The repo has a single existing tag-triggered workflow, `.github/workflows/reconcile-module.yml`, and it establishes a convention for how a new tag-triggered workflow should relate to the base `.github/workflows/ci.yml` workflow: a `based_on:` header comment that names the source workflow and states in prose exactly which parts are being reused verbatim.

> Source: [.github/workflows/reconcile-module.yml:8-9]
> ```
> # based_on: .github/workflows/ci.yml — reuses the [self-hosted, gauntlet] runner
> # and the Go 1.25 setup verbatim; no new runners or Go versions are provisioned.
> ```

This convention exists because GitHub Actions has no native workflow-inheritance mechanism — reuse across workflow files is either full `workflow_call` composition (not used here) or, as adopted in this repo, disciplined verbatim copying with a header comment that documents the source and scope of the copy. The comment gives a human reviewer (or a future edit) a single place to check whether a change to `ci.yml`'s runner, Go version, or lint pin needs to be mirrored into the dependent workflow.

A new tag-triggered workflow (e.g. `release.yml`) that follows this convention must: (1) carry a `based_on: .github/workflows/ci.yml` header comment stating what is reused verbatim, (2) run on the same `[self-hosted, gauntlet]` runner label, (3) use the same `actions/checkout@v4` and `actions/setup-go@v5` steps with `go-version: '1.25'` and `cache: false`, and (4) if it lints, pin `golangci-lint-action@v8` to `version: v2.12.2` against the repo-root `.golangci.yml` (version `"2"`), matching both `ci.yml` and `reconcile-module.yml`.

## Key Concepts

### The `based_on:` header-comment convention

`reconcile-module.yml` opens with a block comment explaining why it is a separate workflow (the root CI's `go test ./...` doesn't cross the nested `./reconcile` module's `go.mod` boundary) followed by the explicit `based_on:` line.

> Source: [.github/workflows/reconcile-module.yml:3-9]

### Runner, checkout, setup-go, and lint steps to reuse verbatim

Both `ci.yml`'s primary `ci` job and `reconcile-module.yml` use the identical runner label, checkout action, and Go setup:

- Runner: `runs-on: [self-hosted, gauntlet]`
  > Source: [.github/workflows/ci.yml:19], [.github/workflows/reconcile-module.yml:28]
- Checkout: `uses: actions/checkout@v4`
  > Source: [.github/workflows/ci.yml:23], [.github/workflows/reconcile-module.yml:36]
- Setup Go: `uses: actions/setup-go@v5` with `go-version: '1.25'` and `cache: false` (the `cache: false` choice is deliberate — gauntlet runners are persistent self-hosted hardware, not ephemeral GitHub-hosted VMs, so `cache: true` would round-trip an unnecessary tarball through GitHub's network Actions Cache)
  > Source: [.github/workflows/ci.yml:25-36], [.github/workflows/reconcile-module.yml:38-44]
- Lint pin: `golangci-lint-action@v8` at `version: v2.12.2`, described in `ci.yml` as "Single pinned version shared with reconcile-module.yml and the root .golangci.yml so a tag cannot pass locally but fail in CI (or vice versa)"
  > Source: [.github/workflows/ci.yml:46-51], [.github/workflows/reconcile-module.yml:54-63]
- The repo-root `.golangci.yml` itself declares `version: "2"` (its own config-schema version, referenced by the reconcile job's explicit `--config ../.golangci.yml` pin)
  > Source: [.golangci.yml:14]

### The one verbatim-copy exception: `permissions:`

Both base workflows declare `permissions: contents: read` — appropriate for a test/lint gate that never writes to the repo. A `release.yml` that publishes a GitHub Release **must not** copy this verbatim: goreleaser needs `permissions: contents: write` to create the release. This is the single field where the `based_on: ci.yml` reuse convention deliberately diverges from the base — the release workflow's job (publishing) is write-side where the base workflow's job (gating) is read-only.
> Source: [.github/workflows/ci.yml:9-10], [.github/workflows/reconcile-module.yml:18-19]

### Full `.github/workflows/` inventory and tag-trigger status

There are four workflow files in the repo. Only `reconcile-module.yml` is currently tag-triggered, and it is scoped to the `reconcile/v*` tag pattern specifically to avoid colliding with an ATCR app tag such as `v1.2.3`.

- `ci.yml` triggers on `push: branches: [main]` and `pull_request: branches: [main]` — not tag-triggered.
  > Source: [.github/workflows/ci.yml:3-7]
- `reconcile-module.yml` triggers on `push: tags: - 'reconcile/v*'` — tag-triggered, but scoped to the `reconcile/` prefix so it will not fire on a bare `v*` tag.
  > Source: [.github/workflows/reconcile-module.yml:11-16]
- `hermes-auto-merge.yml` triggers on `pull_request: types: [opened, synchronize, labeled]` (plus a second trigger reacting to CI completion) — not tag-triggered.
  > Source: [.github/workflows/hermes-auto-merge.yml:28-32]
- `refresh-synthetic-manifest.yml` triggers on `schedule: cron '0 6 * * 1'` and `workflow_dispatch` — not tag-triggered.
  > Source: [.github/workflows/refresh-synthetic-manifest.yml:8-11]

Because none of the four existing workflows listens for a bare `v*` push tag, a new `release.yml` filtered on `push: tags: ['v*']` (or similar) would not collide with any existing trigger — the only other tag consumer, `reconcile-module.yml`, is namespaced under `reconcile/v*`.

## Code Examples

`reconcile-module.yml` header comment and tag filter (verbatim):

```yaml
# based_on: .github/workflows/ci.yml — reuses the [self-hosted, gauntlet] runner
# and the Go 1.25 setup verbatim; no new runners or Go versions are provisioned.

on:
  push:
    tags:
      # Scoped to the module's release-tag convention so ATCR app tags (e.g.
      # v1.2.3) do NOT trigger this release gate. Only reconcile/vX.Y.Z fires it.
      - 'reconcile/v*'
```
> Source: [.github/workflows/reconcile-module.yml:8-16]

`ci.yml`'s reusable runner + checkout + setup-go steps:

```yaml
runs-on: [self-hosted, gauntlet]

steps:
  - name: Check out code
    uses: actions/checkout@v4

  - name: Set up Go
    uses: actions/setup-go@v5
    with:
      go-version: '1.25'
      cache: false
```
> Source: [.github/workflows/ci.yml:19-36]

## Quick Reference

| Workflow file | Trigger | Tag-scoped? | `based_on:`? |
|---|---|---|---|
| `.github/workflows/ci.yml` | `push`/`pull_request` on `main` | No | N/A (base workflow) |
| `.github/workflows/reconcile-module.yml` | `push: tags: reconcile/v*` | Yes (`reconcile/v*` only) | Yes — `based_on: .github/workflows/ci.yml` |
| `.github/workflows/hermes-auto-merge.yml` | `pull_request` (opened/synchronize/labeled) + CI-completion | No | No |
| `.github/workflows/refresh-synthetic-manifest.yml` | `schedule` (cron) + `workflow_dispatch` | No | No |

## Related Documentation

- `.github/workflows/reconcile-module.yml`
- `.github/workflows/ci.yml`
- `.github/workflows/hermes-auto-merge.yml`
- `.github/workflows/refresh-synthetic-manifest.yml`
- `.golangci.yml`
