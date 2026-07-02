---
id: mem-2026-07-01-aefd94
question: "Is deduplication user-configurable in .atcr/config.yaml, and what are the actual configurable keys?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, reconcile/distance.go, reconcile/dedupe.go]
tags: []
retrievals: 0
status: active
type: clarifications skill (epic 15.0)
---

# Is deduplication user-configurable in .atcr/config.yaml, and

## Decision

Deduplication is NOT user-configurable. NCD was falsified and never adopted (reconcile/distance.go:6); live merge cutoffs are hardcoded constants MergeThreshold=0.7, GrayLow=0.4 (reconcile/dedupe.go:17-18); the reconcile/ package has zero YAML tags. There is no yaml block named `reconcile` — the fields triggers/max_items/allow_single_model/max_parallel belong to the `debate:` block (DebateConfig, internal/registry/config.go:153-158, yaml:"debate" at :428). Real user-configurable surface: `persona` (AgentConfig:282, ExecutorConfig:193) plus top-level Registry keys (providers, agents, payload_mode, review_strategy, fail_on, max_parallel, etc. config.go:390-441) and the verify:/debate:/executor: blocks. Dedup behavior is fixed internal: AST-isomorphism + token Jaccard, merge>=0.7 / gray-zone 0.4-0.7.</answer>
<parameter name="tags">clarifications, epic-15.0_documentation_audit, config, reconcile, dedup, documentation

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- reconcile/distance.go
- reconcile/dedupe.go
