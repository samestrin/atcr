---
id: mem-2026-07-04-d71a08
question: "Should atcr fix internal/history/reader.go's unbounded in-memory Load, or accept it as a known limitation?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/history/reader.go, internal/history/writer.go, .planning/epics/completed/19.0_finding_history.md]
tags: [clarifications, epic-19.0_finding_history, scope]
retrievals: 0
status: active
type: clarifications-skill
---

# Should atcr fix internal/history/reader.go's unbounded in-me

## Decision

Accept unbounded in-memory Load as a known limitation and close as won't-fix under epic 19.0 — the epic's Out of Scope section explicitly excludes "retention/rotation policy," and Load already has defensive bounds (per-line 1MiB cap via bufio.Scanner.Buffer, malformed-line skip, fallback bufio.Reader path for oversized lines) so this is a deliberate, documented tradeoff rather than an oversight. Do not spin up a new plan for it.

Justification:
- The epic plan's "Out of Scope" section explicitly lists "retention/rotation policy" as excluded, and the recorded Clarifications never revisit that boundary.
- internal/history/reader.go:21-79 loads the entire ledger into a []Record slice with no cap on record count, but already guards against unbounded single-line growth (sc.Buffer(..., 1024*1024) at reader.go:36, fallback bufio.Reader path at reader.go:56-76).
- internal/history/writer.go:11-24 documents comparable known-limitation tradeoffs in its own doc comment, establishing a project convention of documenting-and-deferring rather than solving every durability/scale edge case inside a scoped epic.
- No AC references ledger size or retention, so this can be closed as won't-fix without a new tracking plan.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/history/reader.go
- internal/history/writer.go
- .planning/epics/completed/19.0_finding_history.md
