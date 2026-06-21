---
id: mem-2026-06-21-f7e99f
question: "Should the cache Store eviction be optimized with a running total-size counter to avoid per-Put ReadDir scans?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/cache/store.go]
tags: [td-clarification, td-only, performance, cache, eviction, mutex, store]
retrievals: 0
status: active
type: clarifications skill, td-only mode, 2026-06-21
---

# Should the cache Store eviction be optimized with a running 

## Decision

Leave as-is. The evict ReadDir+Stat scan is called only from Put while the store mutex is held (internal/cache/store.go:36-40, 116-120), so it is strictly sequential — it never runs concurrently with any other operation. LLM API round-trips dominate latency by orders of magnitude, so the O(n) scan is negligible in absolute terms. A running counter would touch Store struct, constructor, Put, evict, and Get's corrupt-entry removal — complexity that exceeds the LOW severity rating and any measured need. Leave as-is until a concrete performance measurement justifies the change.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/cache/store.go
