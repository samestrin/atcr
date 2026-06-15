---
id: mem-2026-06-14-019cbf
question: "Which AgentConfig fields are mutated after SelectEligibleSkeptics returns, and is the shallow-copy aliasing concern about concurrent or sequential re-use?"
created: 2026-06-14
last_retrieved: ""
sprints: [3.0_adversarial_verification]
files: [internal/verify/select.go, internal/verify/invoke.go]
tags: [clarifications, sprint-3.0_adversarial_verification, correctness, shallow-copy, AgentConfig, SelectEligibleSkeptics]
retrievals: 0
status: active
type: clarifications skill, 2026-06-14
---

# Which AgentConfig fields are mutated after SelectEligibleSke

## Decision

No current caller mutates any reference field after SelectEligibleSkeptics returns. All accesses in invoke.go are read-only: derefInt(c.TimeoutSecs), derefInt(c.MaxTurns), derefInt64(c.ToolBudgetBytes), c.Temperature (pointer read, no write-through). Slice fields (Scope []string, *MaxFindings) are not accessed at all. The concern is defensive/theoretical: shallow value copy at select.go:85 (cfg := skeptics[name]) means pointer/slice fields alias registry backing memory, so a future writer or parallel goroutine (--thorough, 3-skeptic mode) could corrupt the registry. Fix: godoc read-only contract + single test asserting Scope append doesn't mutate registry — no deep-copy warranted given zero observed mutation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- internal/verify/invoke.go
