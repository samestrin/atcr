---
id: mem-2026-06-25-0dc367
question: "For the exported fanout entry that builds a PreparedReview from a diff file, how much of PrepareReview should it mirror and what Base/Head values go in the manifest for a range-less diff?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/payload/manifest.go]
tags: [clarifications, epic-10.1_diff_file_ingestion, architecture, fanout, scaffolding, manifest, PreparedReview]
retrievals: 0
status: active
type: clarifications
---

# For the exported fanout entry that builds a PreparedReview f

## Decision

Full mirror of PrepareReview with empty Base/Head strings (Option A). The exported entry should mirror PrepareReview fully: same directory scaffolding, manifest write, and conditional `.atcr/latest` repoint, honoring `OutputDir`/`IDOverride`/`Force` from the same `ReviewRequest`. Epic 10.2's output-redirect requirement is already handled: `PrepareReview` already skips `.atcr/latest` when `OutputDir` is non-empty (`internal/fanout/review.go:311-314`). For a range-less diff, leave `req.Range.Base` and `req.Range.Head` as empty strings — `Manifest` has no `omitempty` on those fields (`internal/payload/manifest.go:16-58`), so they serialize cleanly. A synthetic sentinel like `"(diff)"` would require every manifest reader to special-case it.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/payload/manifest.go
