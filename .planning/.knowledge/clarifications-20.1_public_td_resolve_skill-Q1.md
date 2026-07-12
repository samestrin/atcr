---
id: mem-2026-07-12-f986d6
question: "atcr's append-only ledgers (scorecard, history, audit, debate, tools, localdebt) share an accepted no-cross-process-lock design stance"
created: 2026-07-12
last_retrieved: ""
sprints: [20.1_public_td_resolve_skill]
files: [cmd/atcr/debt_resolve.go, internal/localdebt/doc.go, internal/localdebt/store.go, internal/scorecard/store.go, internal/history/writer.go]
tags: [clarifications, sprint-20.1_public_td_resolve_skill, architecture, concurrency, append-only-store]
retrievals: 0
status: active
type: clarifications
---

# atcr's append-only ledgers (scorecard, history, audit, debat

## Decision

None of atcr's append-only JSONL ledgers (internal/scorecard, internal/history, internal/audit, internal/debate, internal/tools, internal/localdebt) use a lock file or POSIX-only locking primitive for concurrent-write safety — a repo-wide grep for "flock|advisory lock|O_EXCL|LockFile|filelock" returns zero hits. This is a deliberate, documented won't-fix (TD-004): single os.Write per record, "concurrency-tolerant not lock-protected" — a race between two writers can produce harmless duplicate/append-only bloat, never corruption, because readers fold/dedupe on read. Do not add a lock file to just one ledger without a repo-wide decision to change the posture for all of them; instead soften any comment overclaiming absolute idempotency (e.g. "Idempotent: ..." style comments) to reflect concurrency-tolerance rather than lock-protection.

Justification:
- internal/localdebt/doc.go's "Concurrency guarantee" section and internal/scorecard/store.go:88 both cite the same TD-004 portability-caveat stance verbatim.
- cmd/atcr/debt_resolve.go:218-219 previously claimed "Idempotent" for markDebtResolved; the accurate framing is concurrency-tolerant (a redundant append-only record on a race, not corruption), because selectOpenDebt's fold treats any extra terminal record for an already-closed id as redundant.
- Confirmed via repo-wide grep: no ledger package uses flock/O_EXCL/advisory-lock mechanisms.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/debt_resolve.go
- internal/localdebt/doc.go
- internal/localdebt/store.go
- internal/scorecard/store.go
- internal/history/writer.go
