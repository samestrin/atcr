---
id: mem-2026-06-20-e1f45d
question: "In atcr's fan-out architecture, can diff caching be done at per-file granularity or only at payload level?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go]
tags: [clarifications, epic-5.2_diff_caching_incremental_reviews, architecture, scope, caching, fan-out]
retrievals: 0
status: active
type: clarifications
---

# In atcr's fan-out architecture, can diff caching be done at 

## Decision

Only at payload level. buildPayloads (internal/fanout/review.go:491-511) concatenates all changed files into a single text blob per payload mode before any agent is called. There is no per-file granularity downstream of this point — each agent slot receives one pre-rendered prompt built from the full payload blob (review.go:590-653). A cache key of sha256(payload_bytes) + agent_identity is both correct and sufficient: an identical diff produces an identical blob and a full cache hit. A one-file change invalidates the entire blob and is a full miss. True per-file incremental caching would require re-architecting buildPayloads and the engine into per-file API calls — out of scope for Epic 5.2.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
