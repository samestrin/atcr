---
id: mem-2026-06-15-4f8e4b
question: "How should SurvivedSkepticRate handle the ambiguous 0.0 case in the public export where no-verification and all-refuted/unverifiable are indistinguishable?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/export.go, internal/scorecard/scorecard.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, schema-design, export]
retrievals: 0
status: active
type: clarifications skill 2026-06-15
---

# How should SurvivedSkepticRate handle the ambiguous 0.0 case

## Decision

Document the behavior with a code comment — do not use a sentinel (-1 is blocked by clampRate's [0,1] guarantee) and do not add a has_verification flag (new field, out-of-scope schema change). PublicRecord deliberately has no omitempty so every field is always present (AC 04-03). AC 04-03 Scenario 8 explicitly accepts zero as the intended value for no-verification records. Add a comment on PublicRecord.SurvivedSkepticRate and/or the Export aggregation line explaining: 0.0 is ambiguous between no verification, all-refuted, and all-unverifiable; consumers disambiguate via findings_verified + findings_refuted > 0.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/export.go
- internal/scorecard/scorecard.go
