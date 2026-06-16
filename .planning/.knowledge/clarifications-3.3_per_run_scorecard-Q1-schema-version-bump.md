---
id: mem-2026-06-15-1d8641
question: "Should SchemaVersion be bumped when additive omitempty optional verification fields are added to the scorecard record schema?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/scorecard.go, internal/scorecard/export.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, schema-versioning]
retrievals: 0
status: active
type: clarifications skill 2026-06-15
---

# Should SchemaVersion be bumped when additive omitempty optio

## Decision

No — SchemaVersion correctly remains 1. The verification fields (findings_verified, findings_refuted, survived_skeptic_rate) were defined as part of the original v1 schema in original-requirements.md and are implemented as additive omitempty pointer fields (*int, *float64) in the internal Record struct. A nil pointer omits the JSON key entirely; no existing reader breaks on new records that carry these fields. Three ACs (01-02, 04-01, 04-03) explicitly pin schema_version: 1. A SchemaVersion bump is only warranted for breaking changes: field removal, type change, or mandatory→optional demotion. Additive optional fields do not trigger a version bump.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/scorecard.go
- internal/scorecard/export.go
