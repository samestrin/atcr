# Epic 49: pathutil Consolidation

- **Estimated time**: 0.5 day
- **Tasks/Components**: 1/3
- **Type**: tech-debt
- **Source**: Deferred from Sprint 32.0 (`/resolve-td` clarifications, 2026-07-19)

## Objective

Unify the four hand-rolled copies of the "relativize a path against `$HOME`, fall
back to verbatim on `..`" idiom (and its inverse) into a single shared
`internal/pathutil` package, so the `rel == ".."` vs `".."+sep` boundary guard
lives in exactly one place and a fix propagates instead of being copied a fifth time.

## Scope

### In Scope
- Design a small `internal/pathutil` API covering the shared cases.
- Migrate the four existing call sites to it, preserving each one's observable behavior.

### Out of Scope
- Any behavior change to path rendering (this is a consolidation, not a redesign).

## Why this is its own epic (not a Sprint 32.0 fix)

**Clarification (92% confidence):** the four implementations are genuinely
non-identical in signature and algorithm — different arity, different return
shapes, and one (`relativizePaths`) is a whole-string `strings.ReplaceAll` strip
rather than `filepath.Rel`. Unification is real interface design, not a mechanical
extraction. Additionally, `internal/tools/` and `internal/log/` were **not** in
Sprint 32.0's declared `COMPONENTS_TOUCHED`, so this is cross-package work out of
that sprint's scope regardless of difficulty.

## Source call sites

**Source TD:** `cmd/atcr/home.go:44` (LOW, maintainability) — `relHome` is a third
hand-rolled copy of the idiom; the code comment openly admits copying the others.

| Function | File | Direction / algorithm |
|----------|------|-----------------------|
| `relHome` | `cmd/atcr/home.go:44` | home → relative, `filepath.Rel`, verbatim on `..` |
| `relDisplay` | `internal/tools/dispatch.go:371` | home → relative display |
| `relativizePaths` | `internal/log/redact.go:144` | `strings.ReplaceAll` (NOT `filepath.Rel`) |
| `expandHome` | `cmd/atcr/quickstart.go:281` | inverse: `~` → home |

## Acceptance Criteria

- [ ] AC1: A single `internal/pathutil` package exposes the shared relativize/expand
  helpers with a coherent API.
- [ ] AC2: All four call sites delegate to `internal/pathutil`; no copy of the
  `rel == ".."` boundary guard remains outside it.
- [ ] AC3: Existing behavior is preserved — `TestRelHome` and each call site's
  existing tests pass unchanged.

## References

- `cmd/atcr/home.go` `TestRelHome`
- Pre-existing `expandHome` TD note in `cmd/atcr/quickstart.go`
