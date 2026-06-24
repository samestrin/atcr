# Linting & CI/CD [IMPORTANT]

## Overview

Golangci-lint is the linter for the extracted `./reconcile` library module. It is a fast, parallel linters runner for Go that executes multiple code analysis tools simultaneously on Go projects, combining over a hundred built-in linters without requiring separate installation, reducing false positives through tuned default settings, and supporting multiple output formats for CI/CD integration.

> Source: [.planning/specifications/packages/golangci-lint.md:Overview]

The CI wiring for the library is dual-coverage by design, ratified by the maintainer on 2026-06-23. The root `.github/workflows/ci.yml` runs `go test ./...` against the root module, but that command does NOT cross the nested module's `go.mod` boundary, so the `./reconcile` library would otherwise be untested between tags. To close that gap, a new `.github/workflows/reconcile-module.yml` triggers on tag push as the release gate (AC#7), and a PR/push job (or step) in `ci.yml` cds into `./reconcile` and runs `go test`. Both reuse the existing self-hosted `[gauntlet]` runner and Go 1.25 setup from `ci.yml`.

> Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]

Inside `./reconcile`, the tag-push release gate runs `gofmt` check + `golangci-lint` + `go test -race`. golangci-lint is pinned to `v2.12.2` for reproducibility and is critical for the module's independent CI/CD pipeline. The plan's success criteria require that `github.com/samestrin/atcr/reconcile` exists with its own `go.mod` and passes its own CI — `reconcile-module.yml` on tag push (AC#7) plus a PR-time `./reconcile` job in `ci.yml` (AC#1, AC#7).

> Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]
> Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_create]

## Key Concepts

- **golangci-lint as the parallel linters runner.** A fast, parallel linters runner for Go that executes multiple code analysis tools simultaneously, combining over a hundred built-in linters without requiring separate installation.

  > Source: [.planning/specifications/packages/golangci-lint.md:Overview]

- **Dual-coverage CI wiring (tag-push + PR-time).** Two workflows cover the nested module: (1) `reconcile-module.yml` on tag push for the release gate (AC#7); (2) a PR/push job/step in `ci.yml` that cds into `./reconcile` and runs `go test`, so the library never breaks silently between tags.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]
  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:architecture_recommendations]

- **The nested-module `go.mod` boundary.** The root `go test ./...` does NOT cross the nested module's `go.mod` boundary, so the library would otherwise be untested between tags. This is the core reason a dedicated `./reconcile` test job is required rather than relying on the root CI.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]

- **Release-gate checks inside `./reconcile`.** On tag push, `reconcile-module.yml` runs: `cd ./reconcile && gofmt check + golangci-lint + go test -race`.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]

- **Self-hosted `[gauntlet]` runner + Go 1.25 reuse.** Both the tag-push workflow and the PR-time job reuse the existing self-hosted `[gauntlet]` runner and Go 1.25 setup from `ci.yml`. `reconcile-module.yml` is based on `.github/workflows/ci.yml`.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]
  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_create]

- **golangci-lint GitHub Action integration.** golangci-lint provides GitHub Actions integration via a dedicated GitHub Action for automated linting on every commit, and can be pinned to a specific version in GitHub Actions workflows for reproducibility.

  > Source: [.planning/specifications/packages/golangci-lint.md:Installation/CI/CD Integration]
  > Source: [.planning/specifications/packages/golangci-lint.md:Integration Notes (atcr)]

- **`.golangci.yml` configuration.** Linter selection and tuning is done via a `.golangci.yml` file, typically configured at the repository root. It is critical for module CI/CD pipelines — required for `./reconcile` module independent testing.

  > Source: [.planning/specifications/packages/golangci-lint.md:Key APIs/YAML Configuration]
  > Source: [.planning/specifications/packages/golangci-lint.md:Integration Notes (atcr)]

- **AC#7 — independent module CI on tag push.** The plan's success criteria require `github.com/samestrin/atcr/reconcile` to exist with its own `go.mod` and pass its own CI: `reconcile-module.yml` on tag push (AC#7) plus a PR-time `./reconcile` job in `ci.yml` (AC#1, AC#7). `ci.yml` may add a nested-module test job or keep it in the dedicated `reconcile-module.yml`; PR-time coverage must be verified.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_create]
  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_modify]

## Code Examples

The examples below are copied verbatim from the source documents. The `reconcile-module.yml` workflow YAML does not yet exist in source — it is to be created `based_on: .github/workflows/ci.yml` — so its YAML is not quoted here; the verbatim resolution below is the source of truth for how it must be wired.

### golangci-lint CLI — basic usage

```bash
golangci-lint run ./...
golangci-lint run ./cmd/... ./internal/...
```

> Source: [.planning/specifications/packages/golangci-lint.md:Key APIs/Command Line Interface]

### golangci-lint CLI — configuration flag

```bash
golangci-lint run --config .golangci.yml
```

> Source: [.planning/specifications/packages/golangci-lint.md:Key APIs/Command Line Interface]

### `.golangci.yml` — linter selection and tuning

```yaml
linters:
  enable:
    - vet           # Go vet
    - ineffassign   # Detects ineffective assignments
    - staticcheck   # Advanced static analysis
    - errcheck      # Unchecked error returns

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - gocyclo
        - errcheck

run:
  timeout: 5m
  tests: true
```

> Source: [.planning/specifications/packages/golangci-lint.md:Key APIs/YAML Configuration]

### golangci-lint GitHub Action — canonical lint workflow

```yaml
name: Lint
on: [push, pull_request]

jobs:
  golangci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - uses: golangci/golangci-lint-action@v3
        with:
          version: v2.12.2
          args: --timeout 5m
```

> Source: [.planning/specifications/packages/golangci-lint.md:Common Patterns/GitHub Actions Integration]

Note: the canonical snippet above uses `runs-on: ubuntu-latest`. The `./reconcile` module CI reuses the self-hosted `[gauntlet]` runner from `ci.yml` instead — see the verbatim resolution below.

### Local pre-commit hook

```bash
#!/bin/bash
golangci-lint run ./... || exit 1
```

> Source: [.planning/specifications/packages/golangci-lint.md:Common Patterns/Local Pre-commit Hook]

### Dual-coverage CI wiring — verbatim resolution

> Dual coverage (ratified by maintainer 2026-06-23). (1) New .github/workflows/reconcile-module.yml triggers on tag push for the release gate (AC#7): cd ./reconcile && gofmt check + golangci-lint + go test -race. (2) Add a PR/push job (or step) in ci.yml that cds into ./reconcile and runs go test, because the root 'go test ./...' does NOT cross the nested module's go.mod boundary, so the library would otherwise be untested between tags. Both reuse the existing self-hosted [gauntlet] runner and Go 1.25 setup from ci.yml.

> Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:integration_gaps/Nested module CI wiring]

The corresponding plan entries are also verbatim:

- files_to_create: `.github/workflows/reconcile-module.yml — Independent module test CI on tag push (AC#7) (based_on: .github/workflows/ci.yml)`.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_create]

- files_to_modify: `.github/workflows/ci.yml — May add a nested-module test job or keep it in the dedicated reconcile-module.yml; verify PR-time coverage (scope: minor)`.

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:files_to_modify]

- architecture_recommendations: "Add nested-module test coverage in .github/workflows/reconcile-module.yml triggered on tag push, and consider invoking it from .github/workflows/ci.yml on every PR so the library never breaks silently."

  > Source: [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json:architecture_recommendations]

## Quick Reference

| Component | Value / Detail | Source |
|-----------|----------------|--------|
| Linter | golangci-lint v2.12.2 (May 6, 2026) | golangci-lint.md |
| Lint config file | `.golangci.yml` at repository root | golangci-lint.md:Integration Notes (atcr) |
| Tag-push workflow | `.github/workflows/reconcile-module.yml` (AC#7), `based_on: .github/workflows/ci.yml` | codebase-discovery.json:files_to_create |
| Tag-push checks | `cd ./reconcile && gofmt check + golangci-lint + go test -race` | codebase-discovery.json:integration_gaps |
| PR-time job | Job/step in `ci.yml` that cds into `./reconcile` and runs `go test` | codebase-discovery.json:integration_gaps |
| Boundary reason | Root `go test ./...` does NOT cross the nested module's `go.mod` boundary | codebase-discovery.json:integration_gaps |
| Runner | Existing self-hosted `[gauntlet]` runner (reused from `ci.yml`) | codebase-discovery.json:integration_gaps |
| Go version | 1.25 (reused from `ci.yml`) | codebase-discovery.json:integration_gaps |
| golangci-lint action | `golangci/golangci-lint-action@v3`, `version: v2.12.2`, `args: --timeout 5m` | golangci-lint.md:Common Patterns |
| Success criteria | `github.com/samestrin/atcr/reconcile` passes its own CI (AC#1, AC#7) | codebase-discovery.json:files_to_create |

## Related Documentation

- [.planning/specifications/packages/golangci-lint.md](../../../../../specifications/packages/golangci-lint.md) — golangci-lint package spec (CLI, YAML config, GitHub Action, integration notes).
- [.planning/plans/active/8.0_reconciler_library/codebase-discovery.json](../codebase-discovery.json) — codebase discovery with the `integration_gaps/Nested module CI wiring` resolution, `files_to_create`, `files_to_modify`, and `architecture_recommendations`.
- [.planning/plans/active/8.0_reconciler_library/plan.md](../plan.md) — Epic 8.0 plan, including Planning Success Criteria (AC#1, AC#7).
- [golangci-lint official docs](https://golangci-lint.run/) — referenced from golangci-lint.md.
- [golangci-lint GitHub Action](https://github.com/golangci/golangci-lint-action) — referenced from golangci-lint.md.
