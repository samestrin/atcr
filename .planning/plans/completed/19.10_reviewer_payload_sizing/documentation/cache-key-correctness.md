# Cache-Key Correctness (F7)

**Priority: IMPORTANT**

## Overview

The Epic 5.2 diff cache keys on the rendered prompt, model, and a tuning token. Once per-agent sizing exists, a full-payload cache entry served to a per-agent-sized request would be a silent failure. Plan 19.10 folds the per-agent effective budget / chunk plan into the cache key so a sized payload never replays a stale full-payload result.

> Source: [plan.md](../plan.md):Objectives:F7, [original-requirements.md](../original-requirements.md):Requirements:F7

## Key Concepts

- **Existing key structure.** `cache.Key(promptHash, model, tuning string)` in `internal/cache/key.go:42` joins three NUL-separated components before hashing. NUL cannot appear in a hex digest, model id, or numeric tuning token, so no boundary-ambiguity collision is possible.

  > Source: `internal/cache/key.go:26-50`, codebase-discovery.json:existing_patterns:NUL-separated composite cache/hash keys

- **Backend already folded into tuning.** `diffCacheKey` in `internal/fanout/review.go:975` currently folds `baseURL` into the tuning token with a NUL separator, preserving old keys when `baseURL` is empty.

  > Source: `internal/fanout/review.go:975-989`

- **Fold budget/chunk plan into the same tuning token.** F7 extends the tuning-token composition to include the effective budget / chunk-plan identifier, using the same NUL-join pattern so distinct sizing inputs produce distinct keys.

  > Source: codebase-discovery.json:reusable_components:cache.Key / diffCacheKey NUL-joined tuning token

- **Cache integration lives in `engine.go` and `review.go`.** The fan-out cache is **not** in a separate `internal/fanout/cache.go` file (that path is deprecated/does not exist). The lookup happens in `internal/fanout/engine.go:invokeCachedSingleShot` and the key derivation in `internal/fanout/review.go:diffCacheKey`.

  > Source: codebase-discovery.json:architecture_notes:6, codebase-discovery.json:files_to_modify:internal/fanout/cache.go

## Implementation Guidance

- Extend `diffCacheKey(prompt, model, baseURL string, temperature *float64)` to accept or derive the effective-budget/chunk-plan value and append it to the tuning token.

- Keep the NUL-separator convention so boundary collisions remain impossible.

- Update the `diffCacheKey` comment block to document that the budget/chunk plan is now deliberately included.

- Add a regression test in `internal/fanout/cache_test.go` asserting that two identical prompts with different per-agent sizing produce different keys.

  > Source: codebase-discovery.json:files_to_modify:internal/fanout/cache_test.go

## Related Documentation

- [Per-Agent Budget & Chunking](per-agent-budget-and-chunking.md) — produces the effective budget and chunk plan to fold into the key (F2/F3)
- `internal/cache/key.go` — `cache.Key` primitive
- `internal/fanout/review.go` — `diffCacheKey`
- `internal/fanout/engine.go` — `invokeCachedSingleShot`
- `codebase-discovery.json` — discovery findings for F7
