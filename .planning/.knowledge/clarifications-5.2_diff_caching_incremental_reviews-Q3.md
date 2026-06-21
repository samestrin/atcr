---
id: mem-2026-06-20-9e7969
question: "Should atcr's --no-cache flag bypass only cache reads or fully disable caching (no read, no write)?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, internal/verify/emit_verification.go]
tags: [clarifications, epic-5.2_diff_caching_incremental_reviews, implementation, caching, cli-flags]
retrievals: 0
status: active
type: clarifications
---

# Should atcr's --no-cache flag bypass only cache reads or ful

## Decision

Bypass cache read only — still write fresh results to cache. The AC "forces a fresh review" means results come from live API calls, not that caching is globally suspended. Writing fresh results back refreshes stale or suspect entries so every subsequent run benefits. Option (b) (no read, no write) would leave a stale cache untouched, defeating the most likely motivation for the flag. This matches the existing --fresh flag pattern in the verify stage (cmd/atcr/review.go:42), where --fresh bypasses the persisted verdict and re-runs but writes the new verdict back (internal/verify/emit_verification.go:164).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- internal/verify/emit_verification.go
