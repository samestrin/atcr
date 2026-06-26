---
id: mem-2026-06-25-57f50c
question: "Why does benchmark run abort on a total-roster case failure instead of scoring recall-0 and continuing?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run.go, docs/benchmark.md]
tags: []
retrievals: 0
status: active
type: clarifications
---

# Why does benchmark run abort on a total-roster case failure 

## Decision

Abort-on-total-roster-failure is intentional and documented at docs/benchmark.md:150-152. A PARTIAL roster failure already scores the failed reviewers recall-0 and continues (cmd/atcr/benchmark_run.go:111-122). A TOTAL roster failure means no reviewer saw the case, so scoring it recall-0 would conflate a transient/infra failure with a genuine missed defect and corrupt every reviewer's recall metric. Aborting (return nil, fmt.Errorf at cmd/atcr/benchmark_run.go:91-94) rather than emitting a poisoned, non-reproducible RunResult is the conservative correct choice; the function-local accs map is discarded by design on any early return. The residual concern (N-1 cases of paid LLM work lost on one transient failure) is a SEPARATE low-priority resumability/checkpointing feature, not a correctness bug — and is zero-cost under the stub-Completer test path.</answer>
<parameter name="tags">clarifications, epic-10.2_benchmark_run_scoring, architecture, benchmark, scoring

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run.go
- docs/benchmark.md
