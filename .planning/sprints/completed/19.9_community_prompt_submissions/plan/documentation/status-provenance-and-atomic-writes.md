# Status/Provenance Separation and Atomic Persistence

`[IMPORTANT]`

## Overview

`PersonaMeta.Source` currently encodes where a persona came from: `"built-in"`, `"community"`, or `"project"`. Despite the comment on line 22 describing it as a two-value field, the implementation at `listProject` (line 113) already assigns a third value, `"project"`, showing that `Source` is an open provenance axis rather than a closed enum.

> Source: internal/personas/list.go:22
> Source: internal/personas/list.go:113

The new `submitted` status introduced by this plan (AC2) marks a prompt as fixture-passing but unvetted. This is a different concept from provenance: a submission can be `submitted` regardless of where it came from, and it must not become a fourth string value squeezed into `Source`. Folding it in would corrupt `Source`'s meaning — resolver provenance — with a maintainer-review lifecycle state. The two concepts must stay orthogonal: `Source` answers "where did this come from," while `submitted` answers "has a maintainer vetted this yet." If a future CLI surface needs to expose `submitted` status (e.g., `personas list --status`), the aggregation point is `List` at `internal/personas/list.go:38`, which builds the `PersonaMeta` rows — it can be extended without touching `Source` semantics.

> Source: internal/personas/list.go:19
> Source: internal/personas/list.go:38

If a local status or attribution marker needs to be persisted for a submission, it must go through one of the project's existing atomic-write helpers rather than a bare `os.WriteFile`. Both `internal/personas/unit.go:writeFileAtomic` (line 180) and `internal/atomicfs.WriteFileAtomic` (line 24) stage to a sibling temp file, refuse symlinked destinations, and rename into place, eliminating partial-write and TOCTOU risk. The marker should also be kept outside the vetted `personas/community/` tree so that graduation remains an explicit maintainer review action rather than something an automated submit path performs implicitly.

## Key Concepts

- `PersonaMeta.Source` is `string // "built-in" | "community" | "project"` — the existing provenance axis that AC2 says must stay untouched; `submitted` is orthogonal and must not be folded into this field or its string values.
  > Source: internal/personas/list.go:22

- `listProject` sets `Source` to `"project"`, proving the field already has three values in practice, not the two implied by its comment — it is not a closed two-value field.
  > Source: internal/personas/list.go:113

- `List` is the function that builds `PersonaMeta` rows for `personas list`; it is the aggregation point to extend if a `submitted` status ever needs to surface in the CLI (e.g., a future `list --status`), without changing `Source` semantics.
  > Source: internal/personas/list.go:38

- `writeFileAtomic` performs a sibling-temp-then-rename write with symlink refusal and 0600 perms; if `submit` persists any local status/attribution marker, it should reuse this helper rather than inventing a new write path.
  > Source: internal/personas/unit.go:180

- `internal/atomicfs.WriteFileAtomic` is the cross-package atomic file writer (sibling temp + rename, 0644) — an alternative to `internal/personas/unit.go:writeFileAtomic` if the submit status marker lives outside the personas package.
  > Source: internal/atomicfs/atomic.go:24

- The marker should be kept outside the vetted `personas/community/` tree so graduation remains a maintainer review action, not an automated copy (per architecture notes on the epic's terminology-collision resolution).

## Code Examples

```go
// use writeFileAtomic(markerPath, data) if submit persists any local status/attribution marker
```

## Quick Reference

| File | Symbol | Purpose |
|------|--------|---------|
| internal/personas/list.go | `PersonaMeta` (struct, line 19; `Source` field line 22) | Existing provenance axis (`"built-in"` \| `"community"` \| `"project"`) that `submitted` status must stay orthogonal to |
| internal/personas/list.go | `listProject` (line 113) | Sets `Source` to `"project"`, showing the field is not a closed two-value enum |
| internal/personas/list.go | `List` (line 38) | Builds `PersonaMeta` rows for `personas list`; extension point if `submitted` status needs CLI surfacing |
| internal/personas/unit.go | `writeFileAtomic` (line 180) | Sibling-temp-then-rename write, symlink refusal, 0600 perms; reuse for any local status/attribution marker |
| internal/atomicfs/atomic.go | `WriteFileAtomic` (line 24) | Cross-package atomic file writer (sibling temp + rename, 0644); alternative to `unit.go:writeFileAtomic` for markers outside the personas package |
| personas/community/index.json | — | Community library index; a graduated submission needs an entry here, but graduation is a maintainer action, not part of the automated submit path |

## Related Documentation

- internal/personas/list.go
- internal/personas/unit.go
- internal/atomicfs/atomic.go
- personas/community/index.json
