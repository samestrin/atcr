---
id: mem-2026-06-14-af6aa6
question: "How should reviewer independence be computed for disagreement scoring when no model-strength or independence metric exists?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/reconcile/merge.go]
tags: [clarifications, epic-3.2_disagreement_radar, architecture, scoring, reviewer-independence]
retrievals: 0
status: active
type: clarifications /3.2_disagreement_radar.md
---

# How should reviewer independence be computed for disagreemen

## Decision

Use distinct-reviewer count as the v1 independence proxy. The codebase's only reviewer signal is the deduplicated Reviewers []string on each finding, used by confidenceFor() (internal/reconcile/merge.go:209-214) to produce a two-tier HIGH/MEDIUM label. Applying that count as the independence factor keeps the scoring formula internally consistent with the existing confidence system and introduces zero new infrastructure. Document it as a v1 proxy in the cross-exam handoff schema; a future epic can replace it with a real independence map when the data exists. A model-weight config map is out-of-scope; dropping independence entirely loses the signal the scoring formula is designed to capture.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/merge.go
