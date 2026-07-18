---
id: mem-2026-07-17-7187de
question: "localdebt lock.go stale-reclaim race: proven-insufficient fix defers to Epic 43.0, not a new epic"
created: 2026-07-17
last_retrieved: ""
sprints: [30.0_community_prompt_quality_signal]
files: [internal/localdebt/lock.go, .planning/epics/active/43.0_localdebt-store-compaction.md]
tags: [td-clarification, resolve-td, defer-to-existing-plan, localdebt, concurrency]
retrievals: 0
status: active
type: td-clarification
---

# localdebt lock.go stale-reclaim race: proven-insufficient fi

## Decision

internal/localdebt/lock.go's stale-lock reclaim has a genuine TOCTOU race under concurrent access (a waiter's staleness read can be stale by the time it renames/removes, colliding with a freshly-acquired live lock). The reviewer-prescribed atomic-rename fix was implemented, stress-tested, and proven insufficient (TestWithLockReclaimsStaleLockWithoutOverlap, t.Skip'd with the repro). Correct mutual exclusion needs an OS-level primitive (syscall.Flock) that reworks the whole lock.go file — too large/cross-cutting for an inline TD fix.

Don't create a new Epic Plan for this — .planning/epics/active/43.0_localdebt-store-compaction.md already owns this exact territory: its Task T2 is "Implement lock-based concurrency synchronization in internal/localdebt/lock.go," and its Risks table already anticipates "advisory lock consistent with the codebase's mkdir-lock convention" for compaction-vs-append races. A flock-based rework would resolve both Epic 43.0's compaction-race concern AND this stale-reclaim race in the same primitive — designing two separate lock reworks in the same file would itself be a drift risk. Action: defer to existing plan (43.0), accept the residual rare race in the interim (narrow: requires a prior crashed/hung holder plus concurrent access during the stale-recovery window).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/localdebt/lock.go
- .planning/epics/active/43.0_localdebt-store-compaction.md
