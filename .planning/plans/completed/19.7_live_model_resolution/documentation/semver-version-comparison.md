# Semantic Version Comparison (golang.org/x/mod/semver)

**Priority:** [IMPORTANT]

## Overview

The `semver` subpackage of `golang.org/x/mod` implements comparison of semantic version strings without requiring a full semver-parsing dependency. atcr already vendors this package at v0.37.0 and uses only this one subpackage — no other part of `golang.org/x/mod` is in play. Its comparison semantics (validity checking, ordering, and major/minor prefix extraction) are considered stable and are widely relied upon across the Go ecosystem, even though the parent module itself has not reached v1.

atcr already depends on this package for persona upgrade detection: `internal/personas/upgrade.go`'s `isNewer` function is the sole call site, using `semver.IsValid` and `semver.Compare` to decide whether a re-fetched persona's version string represents an upgrade over the installed copy. Plan 19.7's AC6 (major-bump re-validation gate) needs a related but distinct judgment — not "is this newer" but "is this a *major* jump versus a minor advance" — because a minor version advance (e.g. 4.8 to 4.9) should auto-lock, while a major jump (e.g. 4.x to 5.x) must gate on the persona's fixture still passing and surface a re-tune flag.

Rather than reimplementing version parsing, AC6's gate can build directly on top of the existing `isNewer` comparison path and the same `semver` package it already imports. The key additional primitive is `semver.Major(v)`, which extracts just the major-version prefix (e.g. `"v4"` from `"v4.9.2"`). Comparing `semver.Major(local)` against `semver.Major(remote)` gives a direct, package-native way to classify a transition as "major" versus "minor" without writing custom parsing logic, reusing the same normalization (`"v" + strings.TrimPrefix(...)`) and validity checks that `isNewer` already performs.

## Key Concepts

- **Package scope in atcr**: atcr uses only the `semver` subpackage of `golang.org/x/mod` (`golang.org/x/mod/semver`), pinned at v0.37.0 (latest upstream v0.38.0). The subpackage implements comparison of semantic version strings.
  > Source: [.planning/specifications/packages/utilities.md]

- **Core APIs available**: `IsValid(v string) bool` reports whether v is a valid semantic version string; `Compare(v, w string) int` returns -1, 0, or +1 comparing two valid semver strings; `Major(v string)` / `MajorMinor(v string)` extract the major or major.minor prefix of a version.
  > Source: [.planning/specifications/packages/utilities.md]

- **Stability caveat**: the `golang.org/x/mod` module itself is not at v1 and is not considered API-stable overall, but the `semver` subpackage's comparison semantics specifically are stable and widely relied upon across the Go ecosystem — safe to build AC6 on top of.
  > Source: [.planning/specifications/packages/utilities.md]

- **Existing sole call site**: `internal/personas/upgrade.go`'s `isNewer` is the only place in atcr that currently calls into `semver`, using `IsValid`/`Compare` to decide whether a re-fetched persona's version is newer than the installed copy.
  > Source: [.planning/specifications/packages/utilities.md]

- **isNewer's comparison logic**: both `local` and `remote` version strings are normalized to a `"v"`-prefixed form via `"v" + strings.TrimPrefix(x, "v")`, then checked with `semver.IsValid`. If both sides are valid semver, the result is a structural comparison (`semver.Compare(rv, lv) > 0`). If only one side is valid semver, the two versions are treated as not comparable and the function returns `false` (local is treated as up-to-date) to avoid silently overwriting a newer or customized local persona. If neither side is valid semver, the function falls back to a plain string inequality check.
  > Source: [internal/personas/upgrade.go]

- **Reusing this structure for AC6's gate**: the major-version jump trigger condition for the fixture-repass + re-tune-flag gate is `semver.Major(remote) != semver.Major(local)` — distinct from a minor advance, which already auto-writes via the current `isNewer` path. This lets AC6 reuse the same normalized, validity-checked version strings `isNewer` already produces, layering a `Major()` comparison on top rather than introducing a second version-parsing mechanism.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/upgrade.go:89]

## Code Examples

Existing production code, quoted verbatim — do not modify:

```go
// isNewer reports whether remote is a newer version than local. Valid semver
// is compared structurally. When exactly one side is valid semver the versions
// are not comparable, so the local copy is treated as up-to-date to avoid
// silently overwriting a newer or customized local persona. Otherwise any
// difference is treated as an upgrade.
func isNewer(local, remote string) bool {
	lv := "v" + strings.TrimPrefix(local, "v")
	rv := "v" + strings.TrimPrefix(remote, "v")
	lValid := semver.IsValid(lv)
	rValid := semver.IsValid(rv)
	switch {
	case lValid && rValid:
		return semver.Compare(rv, lv) > 0
	case lValid || rValid:
		return false
	default:
		return local != remote
	}
}
```

The following is **illustrative, not existing code** — a sketch of how `semver.Major` could be layered on top of the same normalized/validated strings for AC6's gate, not something that ships today:

```go
// ILLUSTRATIVE ONLY — not existing code, not implemented anywhere yet.
// Sketch of how AC6's major-bump gate might classify a transition once
// isNewer has already confirmed both sides are valid, comparable semver.
func isMajorBump(local, remote string) bool {
	lv := "v" + strings.TrimPrefix(local, "v")
	rv := "v" + strings.TrimPrefix(remote, "v")
	return semver.Major(lv) != semver.Major(rv)
}
```

## Quick Reference

| API | Signature | Purpose |
|-----|-----------|---------|
| IsValid | `func IsValid(v string) bool` | Reports whether v is a valid semantic version string |
| Compare | `func Compare(v, w string) int` | Returns -1, 0, or +1 comparing two valid semver strings |
| Major | `func Major(v string) string` | Extracts the major version prefix (e.g. `"v4"` from `"v4.9.2"`) |
| MajorMinor | `func MajorMinor(v string) string` | Extracts the major.minor version prefix (e.g. `"v4.9"` from `"v4.9.2"`) |

## Related Documentation
- .planning/specifications/packages/utilities.md
- internal/personas/upgrade.go
- https://pkg.go.dev/golang.org/x/mod/semver
