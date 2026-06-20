---
id: mem-2026-06-19-2d05be
question: "In a crash-safe backup swap using temp staging names (.bak.old, .bak.new, .bak.tmp-*), is cleaning up leftover stragglers from a prior crash considered multi-generation GC (out of scope) or in-scope straggler reconciliation?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/fanout/reviewdir.go, internal/atomicfs/atomic.go]
tags: [clarifications, epic-4.7.1_backup-swap-hardening, scope, crash-safety, backup, straggler-cleanup]
retrievals: 0
status: active
type: epic clarifications 2026-06-19
---

# In a crash-safe backup swap using temp staging names (.bak.o

## Decision

Lightweight straggler cleanup at the entry of each backup call is in scope and required — it is categorically different from multi-generation GC. The temp names (.bak.old, .bak.new, .bak.tmp-*) are atcr-owned implementation artifacts, not user backup generations. "Multi-generation GC" means retaining .bak, .bak-2, .bak-3 etc. A RemoveAll of these temp names at entry is the "Reconcile on next --force: clean stragglers" mechanism, and is required for AC5 (no backup accumulation) to hold after a crash-then-retry sequence. Leave it out and a crash leaves a second copy on disk that never gets cleaned.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/reviewdir.go
- internal/atomicfs/atomic.go
