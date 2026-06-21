---
id: mem-2026-06-21-ebc2e7
question: "Should evict() in internal/cache/store.go surface/log file deletion errors, or is silent best-effort eviction intentional?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/cache/store.go]
tags: [clarifications, td-clarification, td-only, error-handling, cache, best-effort]
retrievals: 0
status: active
type: clarifications-td-only
---

# Should evict() in internal/cache/store.go surface/log file d

## Decision

Intentional best-effort; accepted design. The swallowed os.Remove error at store.go:150 is the documented contract (store.go:89-92: eviction is best-effort and never fails a Put; a failed delete only defers reclaiming disk to the next Put, no correctness impact), consistent with other best-effort degradations like _ = os.Chtimes (store.go:85). Do NOT add a logger to Store and do NOT fail Put. This is principled asymmetry with the Get-path corrupt-entry removal at store.go:77-79 (resolved in commit 971f389), which IS surfaced because a failed remove there returns stale corrupt data forever. Convention: the cache package degrades silently except where swallowing an error would cause a correctness bug (stale data).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/cache/store.go
