---
id: mem-2026-06-24-8f5ce8
question: "Is a nil-guard needed before sort.SliceStable in SelectEligibleSkeptics when the scores map is nil?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/verify/select.go, internal/verify/pipeline.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, implementation, architecture, nil-safety, sort]
retrievals: 0
status: active
type: clarifications skill — resolve-td batch 2026-06-24
---

# Is a nil-guard needed before sort.SliceStable in SelectEligi

## Decision

No nil-guard is needed. In Go, a nil map returns the zero value (0.0) for any float64 key lookup — sort.SliceStable executes safely with nil scores, every matched skeptic gets score 0.0 and falls through to alphabetical ordering. The NaN hazard is separately mitigated: select.go:152-157 sanitizes math.IsNaN to math.Inf(-1) before comparing. The correct deferral is to wait until T6 wires scorecard.Aggregate() into pipeline.go, then add a one-line comment asserting the map is keyed by lowercase skeptic registry name. A `if len(scores) > 0` guard would wrongly suppress the sort and break tests that exercise the code path.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- internal/verify/pipeline.go
