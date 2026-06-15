---
id: mem-2026-06-15-e8e235
question: "Is the case-insensitive EqualFold comparison for skipped-finding verdicts in pipeline.go intentional or a bug?"
created: 2026-06-15
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go, internal/verify/verdict.go]
tags: [td-clarification, td-only, correctness, verdict-comparison, EqualFold, pipeline, intentional-design]
retrievals: 0
status: active
type: td-clarification
---

# Is the case-insensitive EqualFold comparison for skipped-fin

## Decision

Intentional. Verdict constants are defined as all-lowercase in verdict.go:15 ("confirmed", "refuted", "unverifiable") — no case-fold collision exists between them. parseVerdict at verdict.go:61 normalizes the LLM's raw response with strings.ToLower before storing, so every verdict written by a live run is already lowercase. EqualFold handles hand-edited prior files (a human writing "Confirmed" or "CONFIRMED") per the pipeline.go:275-278 comment. hasTrustedVerdict at pipeline.go:510 uses the same strings.ToLower+TrimSpace pattern — this is the established package convention for disk-originated verdict data.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
- internal/verify/verdict.go
