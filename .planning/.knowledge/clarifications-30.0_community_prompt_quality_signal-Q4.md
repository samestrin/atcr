---
id: mem-2026-07-17-9eaa4f
question: "TD questions citing \"AC NN-NN\" without an epic — check which epic actually owns that AC number"
created: 2026-07-17
last_retrieved: ""
sprints: [30.0_community_prompt_quality_signal]
files: [cmd/atcr/main.go, docs/telemetry.md]
tags: [td-clarification, ac-numbering, cross-epic, resolve-td]
retrievals: 0
status: active
type: td-clarification
---

# TD questions citing "AC NN-NN" without an epic — check whi

## Decision

AC numbers (e.g. "02-01") are NOT globally unique across epics/sprints — every epic's acceptance-criteria/ directory restarts numbering from 01-01. When a TD clarification question cites "AC 02-01" without naming which epic/sprint, don't assume it's the current sprint's AC file of that number — grep for the actual filename slug across all plans/sprints (e.g. `find .planning -iname "*02-01*"`) before trusting the question's framing.

Concrete case (Sprint 30.0, cmd/atcr/main.go:269, telemetryEnabledFromEnv): the question claimed "AC 02-01" mandated fail-OPEN-to-enabled, but Sprint 30.0's own AC 02-01 (quality-signal-off-by-default.md) is unrelated — it's about the NEW quality-signal gate's fail-CLOSED/disabled-by-default contract, not the pre-existing telemetry env parsing. The actual mandate lives in Epic 28.0's AC 02-01 (env-var-disables-telemetry.md, Edge Case 2), a completed/shipped epic. This changed the answer materially: overriding a shipped, cross-sprint, documented-everywhere (main.go doc comment + docs/telemetry.md + two locking tests) contract to fix an unrelated sprint's TD item is out of scope — the reviewer's own fallback option ("document the asymmetry as a deliberate, tested contract") was already fully satisfied by existing code, so the correct resolution was "no change needed," not "pick A or B."

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main.go
- docs/telemetry.md
