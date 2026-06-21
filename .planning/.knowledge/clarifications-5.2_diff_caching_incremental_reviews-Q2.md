---
id: mem-2026-06-20-6e5b61
question: "What fields should be included in the diff cache key for atcr's review fan-out caching?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/postprocess.go]
tags: [clarifications, epic-5.2_diff_caching_incremental_reviews, architecture, caching, cache-key]
retrievals: 0
status: active
type: clarifications
---

# What fields should be included in the diff cache key for atc

## Decision

The correct key is: sha256(payload_bytes) + model_id + sha256(persona_text). Agent name is redundant once persona content and model are hashed. byte-budget/truncation is already captured implicitly — a different budget produces different payload text and therefore a different hash (ApplyByteBudget runs before payload serialization at review.go:498). min-severity and max-findings must NOT be in the key — they are deterministic post-LLM filters applied after the API call returns (internal/fanout/postprocess.go:19, enforceConstraints), so the same LLM response is valid regardless of filter settings. Keying on them causes unnecessary cache misses when only a filter threshold changes. Persona content is rendered directly into the LLM prompt (review.go:608-625) so a content hash is required, not just the name. Temperature should also be included if configurable.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/postprocess.go
