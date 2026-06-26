---
id: mem-2026-06-25-faf338
question: "Does benchmark summary.Agents include failed reviewers, or should the scorer iterate the cfg roster instead?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run.go, internal/fanout/artifacts.go, internal/fanout/resume.go]
tags: []
retrievals: 0
status: active
type: clarifications
---

# Does benchmark summary.Agents include failed reviewers, or s

## Decision

summary.Agents already includes failed reviewers — iterating it (cmd/atcr/benchmark_run.go:95,111-132) is correct; do NOT switch to the cfg roster. WritePool loops over the FULL fan-out results slice and unconditionally appends statusFor(r, fr) per agent — its doc states it "writes a full set even when every agent failed" (internal/fanout/artifacts.go:48-51, 68-88). statusFor stamps Status: r.Status for every agent and only sets token fields when usage > 0, so a failed/zero-usage agent still yields a complete AgentStatus with zeroed cost/latency (internal/fanout/artifacts.go:208-234). RebuildPool preserves this on resume (internal/fanout/resume.go:462-540). Switching to cfg.Project.Agents/cfg.Registry.Agents would regress: the cfg roster carries no token/latency data, severing the per-agent cost/latency accumulation that keys off AgentStatus. Verified non-issue.</answer>
<parameter name="tags">clarifications, epic-10.2_benchmark_run_scoring, implementation, benchmark, scoring, fanout

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run.go
- internal/fanout/artifacts.go
- internal/fanout/resume.go
