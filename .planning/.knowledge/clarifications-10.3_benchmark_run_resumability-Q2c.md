---
id: mem-2026-06-25-2d9df7
question: "Should a short 4-line string-truncation helper be deduplicated into a shared package or exported from its origin package when no shared-helpers package exists?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_checkpoint.go, internal/fanout/resume.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, deduplication, shared-package, local-copy]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Should a short 4-line string-truncation helper be deduplicat

## Decision

No — keep it as a local copy. A 4-line helper is a legitimate intentional local copy that does not warrant creating a new shared package or exporting from a domain-specific package. Introducing a shared-helpers package for a single 4-line utility is over-engineering. Exporting the helper from a domain-specific package (like internal/fanout) adds an unrelated utility to that package's public API surface — a category mismatch. Document the intentional mirroring relationship in a comment ("mirrors fanout's shortRef"). This applies when: no shared-helpers package exists, the helper is trivial (under ~10 lines), and the duplication is between two packages that should not cross-import each other for other reasons.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_checkpoint.go
- internal/fanout/resume.go
