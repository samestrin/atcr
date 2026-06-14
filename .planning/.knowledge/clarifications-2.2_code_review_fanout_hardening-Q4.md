---
id: mem-2026-06-13-ad07b0
question: "Where should min_severity and max_findings be enforced — in the fan-out per-source path or in the reconcile stage?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/fanout/artifacts.go, internal/reconcile/discover.go, internal/reconcile/reconcile.go, internal/reconcile/gate.go]
tags: [clarifications, epic-2.2_code_review_fanout_hardening, architecture, fan-out, enforcement, min_severity, max_findings]
retrievals: 0
status: active
type: clarifications
---

# Where should min_severity and max_findings be enforced — i

## Decision

Fan-out per-source path — inside findingsFor() in internal/fanout/artifacts.go, after ParseModelOutput() stamps REVIEWER and before findings are written to the per-agent findings.txt. AC2/AC3 both explicitly name the fan-out as the enforcing actor. Reconcile's Discover() (discover.go:44-101) is source-agnostic and has no per-agent config visibility; injecting caps there would require propagating per-source config into a pipeline designed without it — a larger structural change the epic does not call for. The existing severity infrastructure (ParseSeverity, AtOrAbove in reconcile/gate.go:22-56) is reusable from a fan-out helper.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/artifacts.go
- internal/reconcile/discover.go
- internal/reconcile/reconcile.go
- internal/reconcile/gate.go
