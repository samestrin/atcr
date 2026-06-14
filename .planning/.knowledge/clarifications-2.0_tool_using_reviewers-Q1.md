---
id: mem-2026-06-13-c27dfe
question: "In the fanout tool loop, can max_turns and tool_budget_bytes both appear in TrippedBudgets from the same run? Can they co-trip on the same turn?"
created: 2026-06-13
last_retrieved: ""
sprints: [2.0_tool_using_reviewers]
files: [internal/fanout/loop.go]
tags: [clarifications, sprint-2.0_tool_using_reviewers, budget, architecture, loop-semantics, max_turns, tool_budget_bytes, TrippedBudgets]
retrievals: 0
status: active
type: clarifications skill, 2026-06-13
---

# In the fanout tool loop, can max_turns and tool_budget_bytes

## Decision

No — under Model-A semantics, max_turns and tool_budget_bytes are mutually exclusive in TrippedBudgets for any given run, and they cannot co-trip on the same turn.

Timing in loop.go:
- max_turns is checked at loop.go:147 (`if l.res.Turns >= l.maxTurns`) BEFORE dispatchTurn (tool execution). When it trips, answerSkipped() is called (no tools run, no bytes added), and requestFinalAnswer is returned immediately.
- tool_budget_bytes is checked at loop.go:170-173 AFTER dispatchTurn. This check is structurally unreachable on the turn where max_turns trips.

Consequence: if max_turns trips, tools never run on that turn → no bytes accumulate → tool_budget_bytes cannot also trip. If tool_budget_bytes trips on turn N, the loop halts at line 172 → max_turns never fires. Whichever budget trips first halts the loop; the other cannot also trip.

The only valid multi-budget scenario in TrippedBudgets is tool_budget_bytes + timeout_secs: byte budget trips at loop.go:171, requestFinalAnswer is called, and if the model's response times out during that call, timeout_secs is added by requestFinalAnswer's error handler (loop.go:270).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/loop.go
