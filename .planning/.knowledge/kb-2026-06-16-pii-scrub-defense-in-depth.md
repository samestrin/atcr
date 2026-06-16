---
id: mem-2026-06-16-834d41
question: "What is the defense-in-depth pattern for PII scrubbing in scorecard public export?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: []
tags: [sprint-learning, 3.3_per_run_scorecard, security, export, PII]
retrievals: 0
status: active
type: sprint-learning
---

# What is the defense-in-depth pattern for PII scrubbing in sc

## Decision

The scorecard public export (leaderboard --export) uses a three-layer PII defense: (1) allowlist struct with explicit fields + no omitempty, (2) deterministic field ordering for byte-identical output, (3) regex backstop (scrubField) as secondary defense. The regex backstop has known edge cases: alnum-glued absolute paths (host/etc/passwd) and UNC/bare-backslash Windows paths can survive scrubbing. These are accepted because: the allowlist is the primary boundary, crafted model/reviewer strings are not a realistic attack vector for a local CLI, and the risk was documented and triaged. Lesson: when using regex as a backstop for structured data, document the known edge cases and the threat model that makes them acceptable.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
