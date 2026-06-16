---
id: mem-2026-06-16-b3d56e
question: "Should statusFor record the model on a successful zero-token run for better $0-cost auditability, overturning the byte-identical status.json contract?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/fanout/artifacts.go, internal/llmclient/rates.go, internal/scorecard/scorecard.go, internal/scorecard/aggregate.go, internal/fanout/usage_test.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, scorecard, status-json, usage-wiring]
retrievals: 0
status: active
type: clarifications
---

# Should statusFor record the model on a successful zero-token

## Decision

No. The auditability gain is illusory: a zero-token run prices to $0 regardless of model because ComputeCostUSD zeroes both rate terms when tokensIn/tokensOut are 0 (internal/llmclient/rates.go:83). The byte-identical status.json contract is intentional and Model is deliberately recorded only alongside the non-zero tokens it priced (internal/fanout/artifacts.go:221-229). Emitting Model-without-tokens would create phantom (reviewer, model) leaderboard rows carrying 0 findings/0 cost, since the leaderboard groups by (Reviewer, Model) (internal/scorecard/aggregate.go:132). TestStatusFor_OmitsUsageWhenZero (internal/fanout/usage_test.go:66-78) pins this all-or-nothing usage block by design and should NOT be changed. Design invariant: in the scorecard pipeline, Model travels with tokens as a unit; cost is derived at emit time, never persisted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/artifacts.go
- internal/llmclient/rates.go
- internal/scorecard/scorecard.go
- internal/scorecard/aggregate.go
- internal/fanout/usage_test.go
