# GoReleaser Configuration

`[CRITICAL]`

## Overview

goreleaser is a CLI release-automation tool for Go projects: the entire release process is declared in a `.goreleaser.yaml` file at the repo root, and running `goreleaser release` (locally or in CI) cross-compiles binaries for a `GOOS`/`GOARCH` matrix, archives and checksums them, and publishes a GitHub Release.

> Source: goreleaser.md#overview

It is not a Go module dependency — it is invoked as a standalone CLI, typically via `goreleaser/goreleaser-action` in a GitHub Actions workflow. Adopting it requires no code changes beyond the config file itself: the typical loop is `git tag vX.Y.Z && git push --tags`, then a tag-triggered CI workflow runs `goreleaser release --clean` (or it can be run locally with `--snapshot --clean` for a non-publishing dry run).

> Source: goreleaser.md#quick-start

For this repo specifically, the `.goreleaser.yaml` `builds:` block must stamp version information into two independent, unrelated Go variables at once via multiple `-X` ldflags entries — see Key Concepts below.

## Key Concepts

**What `.goreleaser.yaml` is.** Config lives in `.goreleaser.yaml` at the repo root; it drives the full release (cross-compilation, archiving, checksumming, GitHub Release publishing) with no code changes required beyond the config file itself.

> Source: goreleaser.md#quick-start

**How `ldflags` stamp version variables.** The `builds:` block's `ldflags` entry accepts multiple `-X pkg.Var={{.Version}}` entries, which is how a single build stamps multiple version variables at once. Templates are allowed in `ldflags` and can reference environment variables via `{{ .Env.VARIABLE_NAME }}`. The goreleaser default ldflags are `'-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}} -X main.builtBy=goreleaser'`.

> Source: goreleaser.md#key-apis

**Why this repo needs a dual `-X` ldflags entry.** This repo does not have a single shared version variable — it has two independent ones that both need stamping from the same tag value:

1. `internal/version.Version` — a package-level var defaulting to `"0.0.0"`, used by the leaderboard submission envelope so a submission is self-describing about which build produced it. It is a `var` (not a `const`) specifically so a release build can stamp it at link time, per its own doc comment: `go build -ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3" ./cmd/atcr`.

> Source: internal/version/version.go:1-16

2. `main.version` in `cmd/atcr` — a package-local var defaulting to `""`, overridable via `-ldflags "-X main.version=v1.2.3"`. It feeds `atcrVersion()`, which resolves the string reported by `atcr --version` and `atcr version` using a fallback chain: ldflags-injected value wins first; otherwise `debug.ReadBuildInfo()`'s module version; otherwise the embedded VCS revision (truncated to 12 chars, prefixed `dev+`); otherwise `"dev"`.

> Source: cmd/atcr/version.go:10-40

Because these are two separate variables in two separate packages, the `.goreleaser.yaml` `builds.ldflags` list must include **two** `-X` entries pointing at the same `{{.Version}}` template value — one targeting `github.com/samestrin/atcr/internal/version.Version`, one targeting `main.version` — not a single shared `-X`.

**GOOS/GOARCH matrix defaults.** If `goos`/`goarch` are omitted from the `builds:` block, goreleaser defaults to `goos: [darwin, linux, windows]` and `goarch: [386, amd64, arm64]`.

> Source: goreleaser.md#key-apis

**Reproducible builds.** The default ldflags include `-X main.date={{.Date}}`, which stamps build run time and makes otherwise-identical tag builds differ by when they ran. For reproducible release artifacts, prefer `{{.CommitDate}}` (or omit the date stamp) — the one goreleaser default worth reconsidering for public releases.
> Source: goreleaser.md (Reproducible builds note)

## Code Examples

`builds:` block (Go builder), verbatim from `.goreleaser.yaml` example in the primary source:

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

> Source: goreleaser.md#key-apis

`internal/version/version.go` (module-wide version var, defaults to a neutral placeholder because the repo carries no git tags and go.mod declares no semver):

```go
// Version is the ATCR build version. Overridden at link time for releases; the
// dev/default value is the neutral "0.0.0".
var Version = "0.0.0"
```

> Source: internal/version/version.go:14-16

`cmd/atcr/version.go` (CLI-local version var and its resolution fallback chain):

```go
// version is the atcr release version. It is overridable at build time via
// -ldflags "-X main.version=v1.2.3"; left empty for ordinary `go build`/`go
// install` so atcrVersion can derive a meaningful value from the embedded
// build info instead.
var version = ""
```

```go
func atcrVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				rev := s.Value
				if len(rev) > 12 {
					rev = rev[:12]
				}
				return "dev+" + rev
			}
		}
	}
	return "dev"
}
```

> Source: cmd/atcr/version.go:10-40

## Quick Reference

| ldflags target | variable | purpose |
|---|---|---|
| `-X github.com/samestrin/atcr/internal/version.Version={{.Version}}` | `internal/version.Version` (`internal/version/version.go:16`) | Stamps the version embedded in the leaderboard submission envelope (`atcr_version`) |
| `-X main.version={{.Version}}` | `main.version` in `cmd/atcr` (`cmd/atcr/version.go:14`) | Stamps the version returned by `atcrVersion()` and printed by `atcr --version` / `atcr version` |

> Source: internal/version/version.go:1-16; cmd/atcr/version.go:10-40; goreleaser.md#key-apis

## Related Documentation

- `.planning/specifications/packages/goreleaser.md` — primary goreleaser package specification
- `internal/version/version.go` — module-wide `Version` var used in the leaderboard submission envelope
- `cmd/atcr/version.go` — CLI-local `version` var and `atcrVersion()` resolution logic
