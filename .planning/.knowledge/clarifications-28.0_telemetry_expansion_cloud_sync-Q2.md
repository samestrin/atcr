---
id: mem-2026-07-16-38923b
question: "Structured invariants to pin when replacing scorecard export checksum tests"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [internal/scorecard/export_test.go, internal/scorecard/export.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, testing]
retrievals: 0
status: active
type: clarifications
---

# Structured invariants to pin when replacing scorecard export

## Decision

When replacing a byte-for-byte SHA-256 checksum test on internal/scorecard export output with structured JSON assertions, pin these five invariants: (1) group count/membership by (persona,model), (2) per-group aggregate math (avg/rate/median/cost computed from summed/median raw inputs, not per-run averages), (3) sort order ascending by (Model, Persona), (4) the verification block — SurvivedSkepticRate non-nil (with the correct verified/(verified+refuted) ratio) when verification data exists, nil/omitted when it doesn't, (5) explicit non-leakage of persona_id_hash anywhere in the marshaled output, plus the existing ErrNoExportRecords empty-input check.

Justification:
- internal/scorecard/export_test.go:592-619's checksum fixture/assertions already document these exact invariants (multi-persona aggregation, verification block, NotContains persona_id_hash, ErrNoExportRecords).
- internal/scorecard/export.go:212-251 (Export) aggregates by (persona,model) via map+order slice, sorts ascending by (Model,Persona) at export.go:246-251.
- internal/scorecard/export.go:100-136 (reviewerAcc.finalize) sets SurvivedSkepticRate only when hasVerification is true.
- internal/scorecard/export.go:35-44 (PublicRecord) is an explicit allowlist struct with no persona_id_hash field; export.go:156-158 (ScrubPublicRecord) enforces the scrub.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/export_test.go
- internal/scorecard/export.go
