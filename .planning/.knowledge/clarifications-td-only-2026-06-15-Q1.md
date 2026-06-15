---
id: mem-2026-06-15-403864
question: "Is the strings.EqualFold comparison in the skip-already-verified path of winningAttribution/runVerify intentional or should it be replaced with strict equality?"
created: 2026-06-15
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go]
tags: [td-clarification, td-only, correctness, verify-pipeline, EqualFold, verdict-comparison]
retrievals: 0
status: active
type: td-clarification
---

# Is the strings.EqualFold comparison in the skip-already-veri

## Decision

Intentional and documented in-code. The EqualFold comparison in the skip path (pipeline.go, carry-forward verdict match) is protective for hand-edited verification.json files where a human might write "Confirmed" or "CONFIRMED". parseVerdict normalizes verdicts to lowercase on write, so EqualFold is harmless for machine-generated files and adds resilience for hand-edited ones. Canonical verdicts (confirmed, refuted, unverifiable) are disjoint single-token lowercase strings — EqualFold cannot conflate them. The code comment at the comparison site documents this rationale explicitly. Do not replace with strict equality.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
