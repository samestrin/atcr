---
id: mem-2026-07-06-01d879
question: "Should month-prefix pruning for LoadShards/LoadAll be added now by changing their public signatures, or via a separate LoadShardsSince variant, or deferred entirely?"
created: 2026-07-06
last_retrieved: ""
sprints: []
files: [internal/history/shard.go, cmd/atcr/history.go, internal/history/shard_test.go, internal/history/paths_test.go, cmd/atcr/history_test.go, cmd/atcr/resume_test.go]
tags: [clarifications, epic-19.4_history_time_sharding, implementation, performance, API-design]
retrievals: 0
status: active
type: clarifications
---

# Should month-prefix pruning for LoadShards/LoadAll be added 

## Decision

Defer — not worth doing now given the epic's stated 1-2yr history assumption (12-24 small monthly files, no measured UX degradation). If it's ever picked up, add it as a separate additive variant (e.g. LoadShardsSince) rather than changing the existing LoadShards/LoadAll public signatures, since a signature change would ripple into the one production caller plus 7+ existing test call sites for no measured benefit.

Justification:
- LoadShards/LoadAll have exactly one production caller, cmd/atcr/history.go:49, which already applies history.Filter as a post-load step (cmd/atcr/history.go:60) — pruning would only be an internal fast-path, not a behavior change.
- 7+ existing test call sites construct on the current two-arg signatures: internal/history/shard_test.go, internal/history/paths_test.go:33, cmd/atcr/history_test.go:101, cmd/atcr/resume_test.go:236,243.
- General convention: prefer an additive, backward-compatible variant over changing a public API signature with many existing callers when the optimization is not yet proven necessary.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/history/shard.go
- cmd/atcr/history.go
- internal/history/shard_test.go
- internal/history/paths_test.go
- cmd/atcr/history_test.go
- cmd/atcr/resume_test.go
