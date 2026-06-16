---
id: mem-2026-06-15-d1ef91
question: "Is LeaderboardRow.CostPerCorroborated (total_cost / corroborated) correct, or should it divide by corroborated + refuted?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/aggregate.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, metrics, leaderboard, scorecard]
retrievals: 0
status: active
type: clarifications
---

# Is LeaderboardRow.CostPerCorroborated (total_cost / corrobor

## Decision

The current formula is correct. "Cost per corroborated finding" is by definition TotalCostUSD / FindingsCorroborated (internal/scorecard/aggregate.go:151), and the metric name matches the formula. Dividing by corroborated + refuted describes a different metric ("cost per verified finding") and is not computable from the current row: LeaderboardRow (aggregate.go:24-35) tracks no refuted count and Aggregate never sums one. Adding a cost-per-verified-finding metric would be new scope (add a refuted field to LeaderboardRow, sum FindingsRefuted in Aggregate, add a column), not a correction to this formula. Close as not-a-bug.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/aggregate.go
