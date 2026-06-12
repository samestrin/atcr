---
id: mem-2026-06-12-bd9e43
question: "Epic 1.5: What grace margin should be used on top of timeout_secs for stale inference — flat 60s constant or percentage-of-timeout?"
created: 2026-06-12
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/artifacts.go, internal/fanout/status.go, internal/registry/config.go]
tags: [clarifications, epic-1.5_review-status-lifecycle, implementation, stale-detection, timeout]
retrievals: 0
status: active
type: clarifications
---

# Epic 1.5: What grace margin should be used on top of timeout

## Decision

Use a flat staleGraceSecs = 60 constant. The post-deadline write path (WritePool → per-agent atomic renames → summary.json rename) runs synchronously with no network I/O and completes in milliseconds on a local filesystem, so 60s is already far more conservative than needed. A percentage-of-timeout adds complexity with no benefit: 10% of the default 600s is 60s anyway, and for short configured timeouts a percentage could collapse near zero. Evidence: review.go:225-232 shows the sync write path; artifacts.go:50-95 shows the atomic rename sequence; config.go:21 shows DefaultTimeoutSecs = 600.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/artifacts.go
- internal/fanout/status.go
- internal/registry/config.go
