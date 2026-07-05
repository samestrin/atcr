---
id: mem-2026-07-04-42947d
question: "Should atcr add cross-platform advisory file locking to internal/history/writer.go's Append, or accept the documented low-probability interleaving risk?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/history/writer.go, internal/tools/open_unix.go, internal/tools/open_other.go]
tags: [clarifications, epic-19.0_finding_history, scope]
retrievals: 0
status: active
type: clarifications-skill
---

# Should atcr add cross-platform advisory file locking to inte

## Decision

Accept the documented low-probability interleaving risk — do not add cross-platform flock. The epic's acceptance criteria only require sequential `review` runs to append correctly (AC1: two consecutive runs append two record batches), never require concurrent-write atomicity, and neither the Objective nor Out of Scope section mentions concurrency guarantees, so introducing a platform-split lock (new _unix.go/_windows.go files, a new syscall dependency, Windows-fallback semantics) would be scope creep beyond this 2-3 day epic.

Justification:
- internal/history/writer.go:16-24 already documents the exact tradeoff: batches are serialized in memory and written in one f.Write, so interleaving across concurrent processes is possible in theory but low-probability given small batch sizes; the comment states a caller needing a hard guarantee must serialize appends with an external file lock (intentionally not done here).
- Commit 56292fd9 ("fix: GREEN - correct overstated O_APPEND atomicity guarantee in Append doc comment") shows the established resolution path for this exact finding is documentation, not a code fix.
- The epic's only stated AC (two consecutive runs append two record batches) covers sequential runs, not concurrent ones.
- The codebase's only existing //go:build unix / !unix precedent is internal/tools/open_unix.go / internal/tools/open_other.go, handling an unrelated concern (TOCTOU-safe symlink-free open), not file locking — so a real fix would be net-new platform-specific code, disproportionate for a low-probability edge case outside stated scope.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/history/writer.go
- internal/tools/open_unix.go
- internal/tools/open_other.go
