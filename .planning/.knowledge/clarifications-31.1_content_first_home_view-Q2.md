---
id: mem-2026-07-18-1bc717
question: "Should a cobra root command's no-args home-view branch fire whenever root's RunE is reached with no subcommand (regardless of flags), rather than requiring literally zero flags?"
created: 2026-07-18
last_retrieved: ""
sprints: []
files: [cmd/atcr/main.go, cmd/atcr/review.go]
tags: [clarifications, epic-31.1_content_first_home_view, implementation, cobra, flags]
retrievals: 0
status: active
type: clarifications
---

# Should a cobra root command's no-args home-view branch fire 

## Decision

Yes — fire the branch whenever root's RunE runs with no subcommand, not on a literal "zero flags" check. Args: usageArgs(cobra.NoArgs) (cmd/atcr/main.go:235) only enforces zero positional args and does not inspect flags, so a bare invocation carrying persistent flags (e.g. --log-format json) already reaches RunE today (main.go:262) — requiring "zero flags" would need new restrictive code with no basis in the plan. -h/--help/--version short-circuit inside cobra's Execute() before PersistentPreRunE/RunE ever run (main.go:209-213,240-243), so they never need an explicit guard. A subcommand's LOCAL flag (e.g. --axi registered only on `review`, cmd/atcr/review.go:90) is invisible to root when no subcommand is given — to make `atcr --axi` (bare) work, --axi must be newly registered at the root level as part of this work, not inherited.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main.go
- cmd/atcr/review.go
