---
id: mem-2026-06-15-ba5179
question: "Should cost_usd be persisted in status.json or computed at scorecard emit time from stored token counts?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/llmclient/rates.go, internal/fanout/status.go, internal/scorecard/scorecard.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, scorecard, cost-computation, rates, schema]
retrievals: 0
status: active
type: clarifications
---

# Should cost_usd be persisted in status.json or computed at s

## Decision

Persist model + tokens_in + tokens_out only; compute cost_usd at emit time via llmclient.ComputeCostUSD. Rationale: the rate table in rates.go is hardcoded and known to drift (TD-003: no override path, model-id normalization gap). Computing at emit time means a rate table correction retroactively re-prices all historical JSONL records without re-running reviews; persisting cost would freeze potentially wrong values from a stale rate table permanently. Token counts are stable raw measurements; cost is a derived value that should follow the current rate table at read time. The JSONL scorecard record still carries cost_usd as a computed field — it is not absent, just not pre-stored in status.json.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/rates.go
- internal/fanout/status.go
- internal/scorecard/scorecard.go
