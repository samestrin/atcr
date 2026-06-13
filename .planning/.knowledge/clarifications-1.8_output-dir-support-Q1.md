---
id: mem-2026-06-12-f83989
question: "Does atcr reconcile need a --output-dir flag to work with externally-directed review directories?"
created: 2026-06-12
last_retrieved: ""
sprints: []
files: [cmd/atcr/anchor.go, cmd/atcr/reconcile.go, cmd/atcr/report.go]
tags: [clarifications, epic-1.8_output-dir-support, scope, architecture]
retrievals: 0
status: active
type: epic-1.8_output-dir-support clarifications 2026-06-12
---

# Does atcr reconcile need a --output-dir flag to work with ex

## Decision

No. `atcr reconcile` and `atcr report` already accept an explicit filesystem path via their `[id-or-path]` positional argument. `anchorDir` (anchor.go) returns any absolute path, path-separator-containing arg, or "." verbatim without touching `.atcr/reviews/`. An orchestrator that ran `atcr review --output-dir /tmp/test-review` simply calls `atcr reconcile /tmp/test-review` with no new flag needed. The `--output-dir` flag belongs on `atcr review` only.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/anchor.go
- cmd/atcr/reconcile.go
- cmd/atcr/report.go
