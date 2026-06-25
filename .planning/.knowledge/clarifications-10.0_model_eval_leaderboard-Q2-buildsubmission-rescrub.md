---
id: mem-2026-06-24-943730
question: "Should benchmark BuildSubmission re-scrub reviewer PublicRecords (defense-in-depth) or enforce anonymization at the producer? Which scorecard scrub API?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/benchmark/benchmark.go, internal/scorecard/export.go, cmd/atcr/benchmark.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, security, anonymization, privacy, defense-in-depth, scorecard]
retrievals: 0
status: active
type: clarifications
---

# Should benchmark BuildSubmission re-scrub reviewer PublicRec

## Decision

Use defense-in-depth re-scrub inside BuildSubmission now (path a). Producer-side enforcement (path b) is not possible: `atcr benchmark run` does not exist (Epic 10.1, deferred), while `atcr benchmark export` already reads an external run-result JSON (cmd/atcr/benchmark.go:86,118) verbatim through BuildSubmission (benchmark.go:266) — a live PII path; a hand-crafted --in file bypasses any future producer, so the scrub belongs at BuildSubmission permanently. There is NO existing public API to scrub a PublicRecord: AnonymizeRecord (internal/scorecard/export.go:137) takes an internal Record, and scrubField (export.go:292) is unexported. Path (a) therefore requires exposing a new scorecard entry point — promote scrubField to exported ScrubField(string)string, or add ScrubPublicRecord(PublicRecord)PublicRecord — then apply it to .Model/.Persona of each reviewer in BuildSubmission and update the PRIVACY CONTRACT comment (benchmark.go:225-231).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/benchmark/benchmark.go
- internal/scorecard/export.go
- cmd/atcr/benchmark.go
