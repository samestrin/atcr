---
id: mem-2026-06-21-657ff0
question: "Is the evict() mtime read vs Get() os.Chtimes a race condition in internal/cache/store.go?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/cache/store.go]
tags: [clarifications, td-clarification, td-only, correctness, cache, concurrency]
retrievals: 0
status: active
type: clarifications-td-only
---

# Is the evict() mtime read vs Get() os.Chtimes a race conditi

## Decision

No race. Store uses a single mutex (s.mu) that fully serializes all operations: Get() holds s.mu for its whole body (store.go:61-62) and evict() runs only via Put() which also holds s.mu (store.go:99-100, 114). os.Chtimes (store.go:85) and os.ReadDir/de.Info (store.go:119,134) are therefore never concurrent — the exclusive lock eliminates the race, it does not merely mitigate it. The guarantee is documented at store.go:32-35 and store.go:114. Accepted design; no functional change warranted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/cache/store.go
