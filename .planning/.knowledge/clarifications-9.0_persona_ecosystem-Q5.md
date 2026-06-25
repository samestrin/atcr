---
id: mem-2026-06-24-96a4eb
question: "Is the NaN guard in SelectEligibleSkeptics score comparator implemented, and where?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/verify/select.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, implementation, sorting, NaN, select]
retrievals: 0
status: active
type: clarifications skill — Sprint 9.0 resolve-td run 2026-06-24
---

# Is the NaN guard in SelectEligibleSkeptics score comparator 

## Decision

Yes — the `sort.SliceStable` comparator in `SelectEligibleSkeptics` at `internal/verify/select.go:142-148` contains explicit `math.IsNaN` guards. Any NaN score is replaced with `math.Inf(-1)` before comparison, making the comparator safe against NaN input from any caller regardless of the original "CLI caller sources finite rates" precondition. This eliminates the strict-weak ordering hazard. Fix was introduced in commit `eab6878` as the GREEN phase for RED test `05eca3d` ("reproduce NaN score ordering hazard"). NaN-valued entries sort deterministically last (below all finite scores) and tie-break alphabetically.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
