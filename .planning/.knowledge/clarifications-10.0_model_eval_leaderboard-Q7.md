---
id: mem-2026-06-24-ceac24
question: "writeExportFile is defined in leaderboard.go but called from benchmark.go — is this a problem, and which TD fix should be applied at benchmark.go:94?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark.go, cmd/atcr/leaderboard.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, implementation, benchmark, same-package-sharing]
retrievals: 0
status: active
type: clarifications /execute-epic epic-10.0_model_eval_leaderboard
---

# writeExportFile is defined in leaderboard.go but called from

## Decision

Not a problem. writeExportFile (leaderboard.go:183) is shared same-package code — both files are in package main under cmd/atcr/, so cross-file function references are valid Go. The TD reviewer's "not defined in this file" note is a benign style observation, not a bug. The actionable fix at the benchmark.go:94 cite-group is the suite-field validation: apply strings.TrimSpace to rr.Suite/rr.SuiteVersion and add a len(rr.Reviewers)==0 guard in runBenchmarkExport. Do not move or duplicate writeExportFile.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark.go
- cmd/atcr/leaderboard.go
