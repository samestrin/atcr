---
id: mem-2026-06-24-f3702c
question: "In benchmark export, should GetString error handling and suite-field/reviewer validation be addressed together, and is the GetString fix project-wide?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark.go, internal/benchmark/benchmark.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, implementation, error-handling, validation, benchmark]
retrievals: 0
status: active
type: clarifications /execute-epic epic-10.0_model_eval_leaderboard
---

# In benchmark export, should GetString error handling and sui

## Decision

They are separate concerns. GetString errors at cmd/atcr/benchmark.go lines 90-91 follow the project-wide cobra convention (unreachable for registered flags) — no fix needed. The real actionable fix is the suite-field validation gap: change the empty-string check at runBenchmarkExport (benchmark.go ~line 101) to use strings.TrimSpace for rr.Suite and rr.SuiteVersion (matching Manifest.Validate in internal/benchmark/benchmark.go:88-91), and add a len(rr.Reviewers)==0 guard before BuildSubmission. The fix is local to runBenchmarkExport — no cascade to other commands. GetString is NOT fixed project-wide; suite validation IS fixed locally.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark.go
- internal/benchmark/benchmark.go
