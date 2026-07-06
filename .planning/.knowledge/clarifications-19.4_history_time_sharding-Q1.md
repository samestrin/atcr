---
id: mem-2026-07-06-06fe03
question: "Does the project-wide TD-004 unlocked-O_APPEND concurrency policy (accept risk, won't-fix) extend to the git-tracked history shard introduced by epic 19.4?"
created: 2026-07-06
last_retrieved: ""
sprints: []
files: [.planning/technical-debt/README.md, internal/history/writer.go, internal/audit/writer.go, internal/debate/transcript.go, internal/scorecard/store.go, internal/tools/transcript.go]
tags: [clarifications, epic-19.4_history_time_sharding, architecture, concurrency, TD-004]
retrievals: 0
status: active
type: clarifications
---

# Does the project-wide TD-004 unlocked-O_APPEND concurrency p

## Decision

Yes — accept the risk and close as won't-fix, consistent with the existing project-wide precedent, rather than adding a cross-platform file-locking dependency. The identical unlocked-O_APPEND pattern is shared across five ledgers (audit, debate, scorecard, tools, history); a localized fix for history alone would create an inconsistent concurrency contract among otherwise-identical ledger writers, and epic 19.4's own scope (shard-path helper + directory-loading reader) never contemplated introducing locking.

Justification:
- The project already has a formally accepted, project-wide policy for this exact tradeoff: .planning/technical-debt/README.md:112 — "Append relies on a single O_APPEND write being atomic across concurrent atcr review runs... (Accepted 2026-07-04: won't-fix — cross-platform flock is out of scope for epic 19.0...)"
- The identical unlocked-O_APPEND pattern is used across five ledgers: internal/history/writer.go:39, internal/audit/writer.go, internal/debate/transcript.go, internal/scorecard/store.go, internal/tools/transcript.go.
- internal/history/writer.go:16-24 already documents the caveat explicitly ("a caller needing a hard guarantee must serialize appends with an external file lock (intentionally not done here)").
- Epic 19.4's Scope Boundaries restrict OUT-of-scope items to migration/.gitignore/config changes — no locking mechanism was ever contemplated.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/history/writer.go
- internal/audit/writer.go
- internal/debate/transcript.go
- internal/scorecard/store.go
- internal/tools/transcript.go
