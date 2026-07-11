# Per-Agent Budget & Window-Aware Chunking (F2/F3)

**Priority: CRITICAL**

## Overview

Once each model's full context window is known (F1), the reviewer must size the input payload so the estimated input tokens fit inside `window − output cap − overhead`, then convert that token budget into a per-model chunk line-count for the Epic 14.3 chunker.

> Source: [plan.md](../plan.md):Objectives:F2-F3, [original-requirements.md](../original-requirements.md):Requirements:F2-F3

This closes the confirmed `dax` boundary overflow: `24577 input tokens + 8192 output cap > 32768 window`. Reserving the output cap in the input sizing calculation prevents the exact-boundary class from recurring.

> Source: [original-requirements.md](../original-requirements.md):Problem Statement:2, codebase-discovery.json:semantic_matches:defaultMaxTokens / maxTokensPtr

## Key Concepts

- **Reserve the output cap.** The effective per-agent input budget is derived from:

  ```
  effectiveInputTokens = contextWindow - defaultMaxTokens - promptOverhead
  ```

  where `defaultMaxTokens = 8192` is defined in `internal/fanout/review.go:954` and applied to every reviewer call via `maxTokensPtr()`.

  > Source: `internal/fanout/review.go:948-958`, codebase-discovery.json:semantic_matches:defaultMaxTokens / maxTokensPtr

- **Conservative byte→token ratio.** The plan requires ~3.5 bytes/token plus a safety margin, explicitly rejecting the more optimistic ~4.1 B/token comment in `internal/registry/project.go:89`. Under-filling is acceptable; overflow is not.

  > Source: [original-requirements.md](../original-requirements.md):Non-Functional:Conservatism, [plan.md](../plan.md):Technical Planning Notes:Byte→token ratio anchor

- **Convert effective tokens to `maxLines`.** The Epic 14.3 chunker (`internal/fanout/chunker.go:111` `chunkDiff(diff string, maxLines int) []string`) splits only on file boundaries and respects the 64-chunk/agent ceiling. F3 replaces the current line-gated `EffectiveMaxContextLines()` value with a `maxLines` derived from the per-model effective budget.

  > Source: `internal/fanout/chunker.go:99-142`, codebase-discovery.json:semantic_matches:chunkDiff, codebase-discovery.json:semantic_matches:maxChunksPerAgent

- **In-memory per-agent sizing at dispatch.** Build the full `FileEntry` set once, then compute the effective budget and chunk plan per agent at dispatch time. Sizing is deterministic from `(entries, model, config)`, so resume works without per-agent artifacts.

  > Source: [original-requirements.md](../original-requirements.md):Proposed Solution:In-memory per-agent sizing, [plan.md](../plan.md):Success Criteria

## Implementation Guidance

- Add `internal/payload/sizing.go` exposing helpers for effective budget and chunk-plan derivation, consuming `[]payload.FileEntry` directly rather than introducing a parallel type.

  > Source: codebase-discovery.json:files_to_create:internal/payload/sizing.go, codebase-discovery.json:reusable_components:FileEntry

- The two global-budget call sites in `internal/fanout/review.go:464` and `:726` become per-agent effective-budget/chunk-plan derivation hooks.

  > Source: codebase-discovery.json:integration_points:internal/fanout/review.go:464 and :726

- The chunked-strategy branch at `internal/fanout/review.go:865-876` currently feeds `chunkDiff` a line count from `EffectiveMaxContextLines()`; replace or supplement that value with the per-model token-derived `maxLines`.

  > Source: codebase-discovery.json:integration_points:internal/fanout/review.go:865-876

- Respect the existing 64-chunk ceiling (`maxChunksPerAgent = 64`); do not reimplement chunking.

  > Source: codebase-discovery.json:related_files:internal/fanout/chunker.go, [original-requirements.md](../original-requirements.md):Constraints

## Quick Reference

| Concept | Location | Relevance |
|---------|----------|-----------|
| `defaultMaxTokens` | `internal/fanout/review.go:954` | Output cap to reserve |
| `chunkDiff` | `internal/fanout/chunker.go:111` | Epic 14.3 chunker; feed it per-model `maxLines` |
| `maxChunksPerAgent` | `internal/fanout/chunker.go:99` | 64-chunk ceiling, unchanged |
| `ApplyByteBudget` | `internal/payload/budget.go:46` | Remains the `on_overflow=truncate` mechanism only |
| `EffectiveMaxContextLines` | `internal/registry/config.go` | Old line-gated input; replaced/supplemented for F3 |

## Related Documentation

- [Context-Window Resolver](context-window-resolver.md) — provides `ContextWindowTokens` (F1)
- [on_overflow Policy](on-overflow-policy.md) — routes overflow through chunk/truncate/fallback/fail (F4)
- [Cache-Key Correctness](cache-key-correctness.md) — folds the chunk plan into the cache key (F7)
- [Diagnosability Fields](diagnosability-fields.md) — records effective budget, chunk count, etc. (F8)
- `internal/fanout/chunker_test.go` — pattern for AC3 window-aware chunk-count tests
- `codebase-discovery.json` — discovery findings for F2/F3
