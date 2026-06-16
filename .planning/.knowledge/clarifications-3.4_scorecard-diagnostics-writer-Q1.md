---
id: mem-2026-06-16-4bc4b4
question: "Best-effort diagnostics path: should write errors be discarded, logged, or returned?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/scorecard/store.go]
tags: [clarifications, epic-3.4_scorecard-diagnostics-writer, architecture, go, diagnostics, best-effort]
retrievals: 0
status: active
type: clarifications /execute-epic 2026-06-16
---

# Best-effort diagnostics path: should write errors be discard

## Decision

Discard them with `_, _ = fmt.Fprintf(...)`. In a best-effort diagnostics path the contract is "never panic, never abort the primary operation due to a broken sink." Returning the write error would convert a broken diagnostic sink into a read failure (regression). Logging it would write to the same failed sink (circular). The `_, _ =` blank-assignment is the correct implementation. Applying a generic "log or return errors" rule to secondary diagnostic writes conflates primary operation errors with advisory writes — these are architecturally distinct. See `internal/scorecard/store.go:22-24` for the contract and `store.go:239` for the canonical usage.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/store.go
