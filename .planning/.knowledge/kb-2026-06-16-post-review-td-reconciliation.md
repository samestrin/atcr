---
id: mem-2026-06-16-101ef6
question: "How does /sprint-complete handle code review upgrade when all TD items are resolved post-review?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: []
tags: [sprint-learning, 3.3_per_run_scorecard, process, sprint-complete]
retrievals: 0
status: active
type: sprint-learning
---

# How does /sprint-complete handle code review upgrade when al

## Decision

When /sprint-complete runs after all TD items from a sprint's code review have been resolved (all [x] in TD README, none [ ]), it upgrades the code review result from Partial to Pass and appends a Post-Review Reconciliation block to the code review file documenting: original result, updated result, resolved/deferred/open counts, and source. This happened in sprint 3.3 where 77 items resolved + 1 deferred upgraded the review from Partial (1 incomplete AC, 3 HIGH findings) to Pass. The deferred item must have documented justification in the TD README.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
