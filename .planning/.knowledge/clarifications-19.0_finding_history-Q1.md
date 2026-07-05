---
id: mem-2026-07-04-033119
question: "Which findings artifact should feed atcr's findings-history ledger — pool-merged (every run) or reconciled-only?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, internal/fanout/artifacts.go, internal/reconcile/emit.go]
tags: [clarifications, epic-19.0_finding_history, architecture]
retrievals: 0
status: active
type: clarifications
---

# Which findings artifact should feed atcr's findings-history 

## Decision

Hook the history append after WritePool (right after fanout.ExecuteReview returns in cmd/atcr/review.go:297-325), reading result.Dir/sources/pool/findings.txt. This is the only findings artifact guaranteed to exist on every review run — reconcile only runs conditionally (cmd/atcr/review.go:340, gated on --fail-on/--verify/--debate/--auto-fix), so gating history on reconciled/findings.json would silently break for a bare `atcr review`.

Justification:
- cmd/atcr/review.go:297 calls fanout.ExecuteReview unconditionally; reconcile is conditional at cmd/atcr/review.go:340.
- internal/fanout/artifacts.go:71-115 (WritePool) runs on every ExecuteReview call, writing merged pool findings to sources/pool/findings.txt (internal/fanout/artifacts.go:23, :14-19).
- The pool file is documented as "merged (REVIEWER per row)" — pre-dedup, one row per reviewer that caught an issue (internal/fanout/artifacts.go:18,114).
- result.Dir (from ExecuteReview) is the natural attachment point for a post-review hook, needing no new plumbing.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- internal/fanout/artifacts.go
- internal/reconcile/emit.go
