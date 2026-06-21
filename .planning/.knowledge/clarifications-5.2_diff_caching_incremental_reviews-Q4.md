---
id: mem-2026-06-20-479610
question: "What eviction policy and default size cap should atcr's diff cache use, and how should it be configured?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/registry/project.go, .atcr/config.yaml]
tags: [clarifications, epic-5.2_diff_caching_incremental_reviews, architecture, caching, config, eviction]
retrievals: 0
status: active
type: clarifications
---

# What eviction policy and default size cap should atcr's diff

## Decision

50 MB total-size cap with LRU eviction, overridable via .atcr/config.yaml as an optional pointer field cache_max_bytes *int64. This matches the established pattern: all existing runtime tunables (payload_byte_budget, timeout_secs, max_parallel) are optional pointer fields in internal/registry/project.go:32-44 with embedded defaults. Size-based LRU directly addresses "unbounded growth" (AC #4), is simpler than TTL or entry-count policies, and is easier for users to reason about. The .atcr/config.yaml is the active project config file loaded via LoadProjectConfig at internal/registry/project.go:80.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/project.go
- .atcr/config.yaml
