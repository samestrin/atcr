---
id: mem-2026-07-12-1c4b81
question: "The local TD store's dedup key (FindingID) deliberately excludes severity, so a severity escalation on an already-persisted finding never surfaces in the backlog"
created: 2026-07-12
last_retrieved: ""
sprints: [20.1_public_td_resolve_skill]
files: [internal/localdebt/record.go, cmd/atcr/reconcile.go, cmd/atcr/debt_resolve.go, plan/documentation/local-td-store-schema.md]
tags: [clarifications, sprint-20.1_public_td_resolve_skill, architecture, technical-debt, deduplication]
retrievals: 0
status: active
type: clarifications
---

# The local TD store's dedup key (FindingID) deliberately excl

## Decision

internal/localdebt's Record identity (StampID, via history.FindingID(file,line,problem)) deliberately excludes severity so a re-settled severity keeps the same stable ID across reconcile runs. The consequence: if a finding is first persisted at a lower severity and later re-reconciled at a higher severity, persistLocalDebt's dedup-by-ID skips the append as a duplicate — the backlog freezes the first-seen severity forever. atcr debt resolve's selection path (selectOpenDebt) sorts/filters strictly on this frozen persisted severity with no live re-check, so this frozen value is what actually drives resolution priority. This is accepted-by-design (TD-001, LOW) — real escalation-surfacing logic (compare severity on a matched id, append an updated record when it rises) is explicitly deferred to a future epic; for now, only a documentation note in the schema's "Identity and Deduplication" section is warranted, not a code change.

Justification:
- internal/localdebt/record.go's StampID doc comment: "Severity is deliberately excluded so a re-settled severity keeps the same ID across runs."
- plan/documentation/local-td-store-schema.md's "Identity and Deduplication" section already has a "Severity is intentionally not part of the ID" bullet — the escalation-not-surfaced consequence should be added there.
- cmd/atcr/reconcile.go's persistLocalDebt dedup logic skips append when the id is already seen, regardless of severity delta.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/localdebt/record.go
- cmd/atcr/reconcile.go
- cmd/atcr/debt_resolve.go
- plan/documentation/local-td-store-schema.md
