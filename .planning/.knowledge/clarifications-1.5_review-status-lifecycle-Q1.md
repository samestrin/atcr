---
id: mem-2026-06-12-0ec5c8
question: "Epic 1.5: Has the ExecuteReview write-order reorder (manifest before summary.json) already landed, or does this epic need to include it?"
created: 2026-06-12
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/status.go]
tags: [clarifications, epic-1.5_review-status-lifecycle, scope, architecture, write-ordering, fanout]
retrievals: 0
status: active
type: clarifications
---

# Epic 1.5: Has the ExecuteReview write-order reorder (manifes

## Decision

The reorder has already landed. ExecuteReview calls WritePool first (which writes summary.json as the last pool artifact), then WriteManifest — so summary.json is the last artifact written, which is exactly the adjudicated design. Task 4's read-side invariant and concurrency test should document the ordering the code already produces; no write-order changes are needed. Key subtlety for the concurrency test: the reader reads manifest first, and if manifest is absent it returns ErrNotExist (not a false in_progress) — so the specific interleaving to cover is "summary present, manifest not yet written." Evidence: internal/fanout/review.go:232 (WritePool call), review.go:243 (WriteManifest call after WritePool returns).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/status.go
