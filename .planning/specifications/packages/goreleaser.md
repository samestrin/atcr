# goreleaser

**Version:** v2.17.0
**Registry:** [https://pkg.go.dev/github.com/goreleaser/goreleaser/v2](https://pkg.go.dev/github.com/goreleaser/goreleaser/v2)
**Official Docs:** [https://goreleaser.com/](https://goreleaser.com/)
**Tier:** Important
**Last Updated:** July 12, 2026 02:00:57PM

---

## Overview

goreleaser is a CLI release-automation tool for Go projects. The entire release process is declared in a `.goreleaser.yaml` file at the repo root: push a git tag, then run `goreleaser release` (locally or, more commonly, in CI) to cross-compile binaries for a `GOOS`/`GOARCH` matrix, archive and checksum them, and publish a GitHub Release. It is not a Go module dependency — it is invoked as a standalone CLI, typically via `goreleaser/goreleaser-action` in a GitHub Actions workflow.

## Quick Start

- Config lives in `.goreleaser.yaml`; it drives the full release, so no code changes are needed to adopt it beyond the config file itself.
- Typical loop: `git tag vX.Y.Z && git push --tags`, then let a tag-triggered CI workflow run `goreleaser release --clean` (or run it locally with `--snapshot --clean` for a dry run that doesn't publish).
- Local install is optional — CI is the primary execution path via `goreleaser/goreleaser-action`.

## Key APIs

**`builds:` block (Go builder)** — `.goreleaser.yaml`:
```yaml
builds:
  - main: ./cmd/my-app       # default: .
    binary: program           # default: project directory name
    ldflags:
      - -s -w -X main.build={{.Version}}
      # Default ldflags: '-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}
      #                   -X main.date={{.Date}} -X main.builtBy=goreleaser'
    env:
      - CGO_ENABLED=0
    goos:
      - freebsd
      - windows
      # Default: [darwin, linux, windows]
    goarch:
      - amd64
      - arm
      - arm64
      # Default: [386, amd64, arm64]
```

- **`ldflags`** accepts multiple `-X pkg.Var={{.Version}}` entries — this is how a single build stamps *multiple* version variables at once (relevant here: both `internal/version.Version` and `cmd/atcr`'s local `version` var need `-X` entries pointing at the same `{{.Version}}` template value).
- Templates are allowed in `ldflags` and can reference environment variables via `{{ .Env.VARIABLE_NAME }}`.
- Reproducible builds: `main.Date` defaults to run time (`{{.Date}}`); use `{{.CommitDate}}` instead (or omit) for reproducibility.
- `main` can be a single path (`./cmd/my-app`) or an ellipsis path (`./...`) to build every `main` package in one pass — binary names are inferred the same way `go build` does.

---
**Source:** Extracted from official sources on July 12, 2026 02:00:57PM.
