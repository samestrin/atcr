---
id: mem-2026-06-21-cbcbcb
question: "Does FallbackFrom need to be set inside the synthesized cache-hit Result in engine.go invokeAgent?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/fanout/engine.go]
tags: [td-clarification, td-only, correctness, engine, cache, FallbackFrom, invokeSlot]
retrievals: 0
status: active
type: clarifications skill, td-only mode, 2026-06-21
---

# Does FallbackFrom need to be set inside the synthesized cach

## Decision

No. FallbackFrom is stamped by invokeSlot (internal/fanout/engine.go:476) uniformly on the returned Result for both cached and live paths. The synthesized cache-hit Result returned from invokeCachedSingleShot intentionally mirrors invokeSingleShot, which also omits FallbackFrom from the struct literal. Any TD finding that suggests adding FallbackFrom inside the cache-hit Result is a false positive — the Agent struct has no FallbackFrom field and the field is set at the invokeSlot level, not inside individual result constructors.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/engine.go
