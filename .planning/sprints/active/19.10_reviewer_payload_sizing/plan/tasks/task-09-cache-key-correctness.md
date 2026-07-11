# Task 09: Fold Per-Agent Effective Budget / Chunk Plan Into the Diff-Cache Key

**Source:** Plan 19.10 – Debt Item #7
**Priority:** P1 | **Effort:** S | **Type:** Fix

## Problem Statement
Task 02 makes the reviewer payload budget per-agent (`payload.EffectiveByteBudget(ac.Model, defaultMaxTokens)`, sized by the agent's model context window) and Task 03 makes the chunk plan per-agent (`ml`, a model-derived `maxLines` fed into `chunkDiff`). Neither value is folded into `diffCacheKey` (`internal/fanout/review.go:975`), which today keys only on `prompt, model, baseURL, temperature`.

Because `diffCacheKey` hashes the rendered prompt, a change in payload content (fewer bytes retained, or a different chunk split) already changes the prompt text most of the time — but it is not guaranteed in every case: two runs against the SAME diff, SAME model, and SAME prompt-template can still differ only in how many bytes/lines of context were retained if the effective budget is recomputed with a different `defaultMaxTokens`/context-window resolution between runs (e.g. a context-window override change, or a future config knob), while the *retained* bytes happen to render identical prompt text (e.g. a diff small enough that both budgets keep it whole). In that boundary case the cache key would silently collide, and a per-agent-sized payload's review could be served a stale cache entry produced under a different sizing regime. Folding the effective-budget/chunk-plan identifier directly into the tuning token closes this gap deterministically, the same way `baseURL` was folded in to prevent a cross-provider collision (F5.2 MEDIUM finding), rather than relying on prompt-text divergence as an incidental guarantee.

## Solution Overview
Extend `diffCacheKey`'s tuning-token composition (`internal/fanout/review.go:975-989`) to additionally fold in a `sizingToken` value that identifies the per-agent effective budget / chunk-plan the payload was sized under — following the exact NUL-separated pattern already used for `baseURL`. Thread the sizing value from its computation point (Task 02's `buildSlots`/`add` closure and Task 03's chunked-strategy branch) through `renderAgent` and `buildFallbackAgent` into the two `diffCacheKey` call sites (`internal/fanout/review.go:1053` and `:1140`), so both the bulk path and the chunked path produce a key that changes whenever the sizing regime for that agent/chunk changes. An empty/zero sizing token (e.g. direct `Agent{}` construction in existing tests, or an agent whose model has no resolvable context window) must collapse to the pre-existing `baseURL`+`temperature`-only token, preserving every current cache key and existing test assertion.

## Technical Implementation
### Steps
1. In `internal/fanout/review.go`, change `diffCacheKey`'s signature to accept the sizing identifier, e.g.:
   ```go
   func diffCacheKey(prompt, model, baseURL string, temperature *float64, sizingToken string) string
   ```
   Build `sizingToken` at each call site as a short deterministic string that captures both Task 02's effective byte budget and Task 03's chunk-plan `maxLines` for that specific agent/chunk, e.g. `fmt.Sprintf("%d:%d", effectiveByteBudget, maxLines)` — use `0` for either component when it does not apply (e.g. bulk/non-chunked path has no `maxLines`, so pass `0` for that half) so the token stays stable and comparable across paths.
2. Extend the tuning composition inside `diffCacheKey` to fold `sizingToken` in NUL-separated, same as `baseURL`:
   ```go
   tuning := temp
   if baseURL != "" {
       tuning = baseURL + "\x00" + temp
   }
   if sizingToken != "" && sizingToken != "0:0" {
       tuning = tuning + "\x00" + sizingToken
   }
   ```
   (Adjust the "empty" sentinel to whatever Task 02/03 actually produce for "no per-agent sizing applied" — the goal is that an agent with no effective-budget/chunk-plan restriction reduces to today's exact key so no unrelated existing cache entries are invalidated.)
3. Update `renderAgent` (`internal/fanout/review.go:998`) to accept the effective-budget value (from Task 02) and, when on the chunked branch, the chunk's `maxLines` (from Task 03) — either as new parameters or via a small struct — and pass the composed `sizingToken` into its `diffCacheKey` call at line 1053.
4. Update `buildFallbackAgent` (`internal/fanout/review.go:1088`) similarly: a fallback answers in the primary's place with the SAME prompt/payload but its OWN model, so its OWN effective budget (re-derived for its own model via Task 02's helper) must be used when composing its `sizingToken` for the `diffCacheKey` call at line 1140 — mirror the existing "fallback uses its OWN model/temperature" comment pattern already at that call site.
5. Update the `diffCacheKey` doc comment (`internal/fanout/review.go:960-974`) to document the new deliberately-included sizing component: state that the per-agent effective budget / chunk-plan is now folded into the tuning token so a payload sized under one budget/chunk regime is never served a cache hit produced under a different one, mirroring how `baseURL` is documented as folded in for the same cross-collision reason.
6. Update every existing `diffCacheKey(...)` call site in `internal/fanout/cache_test.go` (lines 27, 114, 136) to pass the new parameter — use the empty/zero sentinel so these existing tests keep asserting the pre-F7 key shape unchanged.
7. Add a new regression test in `internal/fanout/cache_test.go` (see Test Strategy) asserting the F7 behavior.
8. Run `go build ./...` and `go test ./internal/fanout/...` to confirm the new parameter threads cleanly and no existing cache test regresses.

## Files to Create/Modify
- `internal/fanout/review.go` – modify (`diffCacheKey` signature + tuning composition at line 975-989; doc comment at 960-974; `renderAgent` at 998 and its call site at 1053; `buildFallbackAgent` at 1088 and its call site at 1140)
- `internal/fanout/cache_test.go` – modify (update existing `diffCacheKey` call sites for the new parameter; add new sizing-collision regression test)

## Documentation Links
- [Cache-Key Correctness](../documentation/cache-key-correctness.md)

## Related Files (from codebase-discovery.json)
- `internal/cache/key.go` — `Key` (line 26): unchanged; still hashes `promptHash, model, tuning` — F7 only changes what callers fold into `tuning`
- `internal/fanout/engine.go` — `invokeCachedSingleShot` (line 691): unchanged; consumes `Agent.CacheKey` as an opaque string, so no change needed once `CacheKey` is derived correctly upstream

## Success Criteria
- [ ] `diffCacheKey` folds a per-agent effective-budget/chunk-plan identifier into its tuning token, NUL-separated, following the existing `baseURL` pattern
- [ ] Two calls with identical `prompt, model, baseURL, temperature` but different effective-budget/chunk-plan values produce DIFFERENT keys — verified by test (AC7)
- [ ] Two calls with identical everything INCLUDING an unset/zero sizing value produce the SAME key as before this change — verified by test (backward compatibility for non-sized agents)
- [ ] `renderAgent` and `buildFallbackAgent` both compose and pass a correct `sizingToken` reflecting their OWN agent's/model's effective budget (a fallback's token reflects its own model, not the primary's)
- [ ] `diffCacheKey`'s doc comment documents the new deliberately-included sizing component
- [ ] All existing `cache_test.go` assertions (temperature, baseURL/provider, no-cache, tool-agent-never-cached) still pass unmodified in behavior

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `diffCacheKey(prompt, model, baseURL, temp, sizingA) != diffCacheKey(prompt, model, baseURL, temp, sizingB)` for `sizingA != sizingB` (mirrors `TestEngine_DifferentProviderMissesCache`'s structure)
- `diffCacheKey(prompt, model, baseURL, temp, "")` (or the chosen empty sentinel) equals the pre-change key computed by the old 4-arg shape, for backward-compat verification
- A chunked-strategy agent and a bulk-strategy agent for the SAME persona/model that happen to render identical prompt text still produce distinct keys when their chunk-plan/effective-budget values differ

**Integration Tests:**
- `TestEngine_DifferentSizingMissesCache` (new, in `internal/fanout/cache_test.go`, following the shape of `TestEngine_DifferentProviderMissesCache` at line 129): build two `Slot`s with the same prompt/model/baseURL/temperature but different sizing tokens, run both through `Engine.invokeCachedSingleShot` (or the existing `runReview`-level test harness), and assert the second is NOT served from cache (`CacheHit == false`) — i.e. a per-agent-sized payload is never served a stale full-payload cache hit (AC7)

**Test Files:**
- `internal/fanout/cache_test.go`

## Risk Mitigation
- Cache serves a stale full-payload result for a per-agent-sized request because the sizing value was silently dropped somewhere in the `renderAgent`/`buildFallbackAgent` plumbing — mitigation: the explicit `TestEngine_DifferentSizingMissesCache` regression test, plus threading the sizing value as an explicit required argument (not an optional/defaultable one) through both `renderAgent` and `buildFallbackAgent` so a missed call site fails to compile rather than silently defaulting.
- Changing `diffCacheKey`'s signature invalidates ALL pre-existing cached entries on disk for sized agents (any agent whose effective budget previously produced a non-empty token) even when nothing else changed — mitigation: this is the intended, correct behavior for F7 (a cache regime change SHOULD invalidate); document it in the `diffCacheKey` comment update (Step 5) so it is not mistaken for a bug during review.

## Dependencies
- Task-02 (Per-Agent Effective Budget) — supplies the effective byte-budget value folded into the key
- Task-03 (Window-Aware Chunking) — supplies the chunk-plan `maxLines` value folded into the key

## Definition of Done
- `diffCacheKey` accepts and folds in the per-agent effective-budget/chunk-plan identifier
- `renderAgent` and `buildFallbackAgent` both compose and pass their own agent's sizing token correctly
- `diffCacheKey`'s doc comment updated to document the new deliberately-included component
- New regression test `TestEngine_DifferentSizingMissesCache` (or equivalent) passes and demonstrably fails against the pre-F7 code (verified by temporarily reverting the fold to confirm the test catches the regression)
- All existing `internal/fanout/cache_test.go` tests updated for the new signature and passing
- `go build ./...` and `go test ./...` pass
