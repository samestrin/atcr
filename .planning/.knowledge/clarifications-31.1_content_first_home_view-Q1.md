---
id: mem-2026-07-18-05f30c
question: "What should atcr --axi (bare, home view) emit — HomeViewAXI single-row payload, reused RenderReviewSummaryAXI, or literal RenderAXIPaginated(nil)?"
created: 2026-07-18
last_retrieved: ""
sprints: []
files: [internal/report/render.go, internal/report/pagination.go, cmd/atcr/review_summary.go]
tags: [clarifications, epic-31.1_content_first_home_view, architecture, axi, toon]
retrievals: 0
status: active
type: clarifications
---

# What should atcr --axi (bare, home view) emit — HomeViewAX

## Decision

Add a new HomeViewAXI struct + RenderHomeViewAXI in internal/report that reuses the shared toonQuote/axiDelim TOON encoder, emitting a single-row tabular payload — following the exact precedent Epic 31.0 set with ReviewSummaryAXI/RenderReviewSummaryAXI for a non-findings, run-level payload (internal/report/render.go:266-342). Do NOT reuse RenderReviewSummaryAXI as-is (its columns are meaningless/zero for a bare home view) and do NOT call RenderAXIPaginated(nil) (conveys zero home-view data). Calling convention precedent: cmd/atcr/review_summary.go:100-117's writeReviewSummaryAXI wrapper.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/render.go
- internal/report/pagination.go
- cmd/atcr/review_summary.go
