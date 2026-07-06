---
id: mem-2026-07-05-892056
question: "Where should committed, team-shared data (like sharded findings history) live vs. gitignored workspace/runtime state, in this repo?"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [.gitignore, cmd/atcr/review.go, cmd/atcr/history.go, internal/history/record.go, .planning/epics/active/19.4_history_time_sharding.md]
tags: [clarifications, epic-19.4_history_time_sharding, architecture]
retrievals: 0
status: active
type: clarifications
---

# Where should committed, team-shared data (like sharded findi

## Decision

Committed, team-shared artifacts belong under `.planning/` (tracked by git as a whole, with only specific subpaths like `.planning/.temp/`, `.planning/.memory/`, `.planning/.locks/` ignored per .gitignore's "Planning Specific Files" section). Gitignored, per-developer workspace/runtime output belongs under `.atcr/` (fully excluded via a bare `.atcr/` rule). Deciding where a new artifact lives requires checking whether its purpose is team-visibility (→ `.planning/`) or local/ephemeral workspace state (→ `.atcr/`); do not assume an existing sibling artifact's location sets precedent without checking .gitignore directly, since `.atcr/` categorically blocks git tracking regardless of subdirectory depth.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .gitignore
- cmd/atcr/review.go
- cmd/atcr/history.go
- internal/history/record.go
- .planning/epics/active/19.4_history_time_sharding.md
