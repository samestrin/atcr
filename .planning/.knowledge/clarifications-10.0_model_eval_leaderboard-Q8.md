---
id: mem-2026-06-24-1c0567
question: "Should benchmark export validate against whitespace-only suite fields and empty reviewers, and does this cascade to other validation logic?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark.go, internal/benchmark/benchmark.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, implementation, validation, benchmark, error-handling]
retrievals: 0
status: active
type: clarifications /execute-epic epic-10.0_model_eval_leaderboard
---

# Should benchmark export validate against whitespace-only sui

## Decision

Yes — add both checks in runBenchmarkExport (cmd/atcr/benchmark.go). The current guard (rr.Suite == "" || rr.SuiteVersion == "") catches empty strings but not whitespace-only values like " ", which is asymmetric with Manifest.Validate in internal/benchmark/benchmark.go:88-91 (which uses strings.TrimSpace throughout). An empty rr.Reviewers slice produces a valid-looking public submission with zero reviewer rows and should be explicitly rejected before BuildSubmission. Fix: change empty-string checks to strings.TrimSpace(rr.Suite) == "" and strings.TrimSpace(rr.SuiteVersion) == "", and add if len(rr.Reviewers) == 0 { return fmt.Errorf(...) }. Scope is local to runBenchmarkExport — no cascade to other commands needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark.go
- internal/benchmark/benchmark.go
