# Version & Tagging Strategy

`[CRITICAL]`

## Overview

`atcr` has two independent version variables and no tagging convention wiring them to a release. `internal/version.Version` drives the `atcr_version` field in the public leaderboard submission envelope (Epic 10.0), while `cmd/atcr`'s package-local `version` var drives what `atcr version` / `atcr --version` actually print. Neither reads the other, and neither is currently stamped by any build or release process — both default to placeholder values (`"0.0.0"` and `""` respectively) that fall back to `debug.ReadBuildInfo()` heuristics in the CLI's case. A tagged release build must set both from the same source value, or the leaderboard envelope and the CLI's own self-report will silently disagree about what version produced a given result.

`CHANGELOG.md` already has a de facto versioning convention: each entry is headed by an epic-number-as-semver, e.g. `## [20.1.0] - 2026-07-12`, tracing back to `[1.0.0]`, 20+ entries deep. No git tag has ever been cut against any of these headings — per this plan's `codebase-discovery.json` (generated 2026-07-12), `git tag` returns zero results in this repo. Objective 1 formalizes this existing changelog convention into real, bare `vX.Y.Z` git tags (e.g. `v20.1.0`) rather than inventing a new numbering scheme. This namespace must stay disjoint from `reconcile/vX.Y.Z`, which Epic 8.0 already reserved exclusively for the standalone `./reconcile` submodule's own release gate — `.github/workflows/reconcile-module.yml` is the only existing tag-triggered workflow in the repo, and its trigger filter and inline comment explicitly document that ATCR app tags (e.g. `v1.2.3`) must NOT fire it.

## Key Concepts

**Bare `vX.Y.Z` is reserved for the ATCR app release; `reconcile/vX.Y.Z` is a disjoint, already-claimed namespace.** The only tag-triggered workflow in the repo scopes itself to `reconcile/v*` with an explicit comment that app-level tags must not trigger it — establishing by precedent that bare `vX.Y.Z` is free for (and intended for) the ATCR app itself.
> Source: .github/workflows/reconcile-module.yml:14-16

**The CHANGELOG.md epic-number-as-semver convention is being formalized, not invented.** The changelog already headers each release by version number matching the epic that produced it (most recent: `[20.1.0]`, for Epic 20.1), with entries going back to `[1.0.0]`. Objective 1 turns this pre-existing documentation convention into enforced git tags rather than introducing new numbering rules.
> Source: CHANGELOG.md:1, CHANGELOG.md:19, CHANGELOG.md:37

**Zero git tags currently exist against any changelog version.** 20+ versioned changelog entries exist with no corresponding tag ever cut.
> Source: codebase-discovery.json (generated 2026-07-12), as cited in this plan's task instructions

**Two independent version variables must both be stamped from the same tag value.** `internal/version.Version` is a package var read only by the leaderboard submission envelope; its own doc comment documents the intended `-ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3"` stamping convention.
> Source: internal/version/version.go:5-7,16

`cmd/atcr`'s `version` var is separate, resolved by `atcrVersion()` with fallback precedence: ldflags value → `debug.ReadBuildInfo()` module version → VCS revision (truncated to 12 chars, prefixed `dev+`) → `"dev"`. It is what `atcr version` / `atcr --version` actually report, and it does not read `internal/version.Version` at all.
> Source: cmd/atcr/version.go:10-14,21-40

## Code Examples

**`internal/version.Version` — leaderboard envelope version, default placeholder:**
```go
// Version is the ATCR build version. Overridden at link time for releases; the
// dev/default value is the neutral "0.0.0".
var Version = "0.0.0"
```
> Source: internal/version/version.go:14-16

**`cmd/atcr`'s package-local `version` var and its fallback resolution:**
```go
// version is the atcr release version. It is overridable at build time via
// -ldflags "-X main.version=v1.2.3"; left empty for ordinary `go build`/`go
// install` so atcrVersion can derive a meaningful value from the embedded
// build info instead.
var version = ""

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

**`reconcile-module.yml` tag trigger — the disjoint reserved namespace:**
```yaml
on:
  push:
    tags:
      # Scoped to the module's release-tag convention so ATCR app tags (e.g.
      # v1.2.3) do NOT trigger this release gate. Only reconcile/vX.Y.Z fires it.
      - 'reconcile/v*'
```
> Source: .github/workflows/reconcile-module.yml:11-16

**Most recent CHANGELOG.md version heading:**
```
## [20.1.0] - 2026-07-12
```
> Source: CHANGELOG.md:1

## Quick Reference

| Variable / File | Current Default | ldflags Target Needed |
|---|---|---|
| `internal/version.Version` (internal/version/version.go:16) | `"0.0.0"` | `-X github.com/samestrin/atcr/internal/version.Version=<tag>` |
| `version` (cmd/atcr/version.go:14) | `""` (falls back to `debug.ReadBuildInfo()` / `"dev"` chain) | `-X main.version=<tag>` |
| Git tag namespace (ATCR app releases) | none exist (0 tags per codebase-discovery.json) | bare `vX.Y.Z`, formalizing CHANGELOG.md's `[X.Y.Z]` headings |
| Git tag namespace (reconcile module) | `reconcile/v*` (already reserved, Epic 8.0) | unaffected — disjoint from app tags |

## Related Documentation

- `internal/version/version.go`
- `cmd/atcr/version.go`
- `.github/workflows/reconcile-module.yml`
- `CHANGELOG.md`
