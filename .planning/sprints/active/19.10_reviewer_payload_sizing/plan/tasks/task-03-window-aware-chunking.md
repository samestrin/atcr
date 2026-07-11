# Task 03: Window-Aware Chunking (F3)

**Source:** Plan 19.10 – Debt Item #3
**Priority:** P1 | **Effort:** M | **Type:** Refactor

## Problem Statement
The Epic 14.3 chunker (`internal/fanout/chunker.go:111` `chunkDiff(diff string, maxLines int) []string`) bin-packs an over-window diff by raw diff-line count, gated on `AgentConfig.EffectiveMaxContextLines()` (`internal/registry/config.go:984`, default `DefaultMaxContextLines = 1500`). That line budget is identical for every agent regardless of the target model's actual context window. It does not lose content — files are split, never dropped — but it is miscalibrated in both directions: 1500 dense lines can still overflow a 32k-token model, and the same 1500-line ceiling wastes most of a 144k-token model's capacity by opening far more chunks (and provider calls) than necessary. `internal/fanout/review.go:875-876` is the sole call site that reads `ac.EffectiveMaxContextLines()` and feeds it into `chunkDiff` on the `chunked`-strategy branch (`internal/fanout/review.go:865-876`).

## Solution Overview
Reuse `chunkDiff` completely unchanged — it already does exactly what F3 needs (split on file boundaries, respect `maxChunksPerAgent = 64`, never drop a file). The only change is what value feeds its `maxLines` parameter. Add a chunk-plan helper to `internal/payload/sizing.go` (created by Task 02) that converts the per-model effective input token budget (produced by Task 02's effective-budget helper, itself built on Task 01's `ContextWindowTokens` resolver) into a `maxLines` figure using the same conservative byte→token ratio Task 02 established, then divide by an average-bytes-per-line estimate to get a line count `chunkDiff` can consume. Wire `internal/fanout/review.go:875` to call this new helper instead of `ac.EffectiveMaxContextLines()` when `review_strategy: chunked` and `on_overflow: chunk` are in effect, so a 32k-window model gets a smaller `maxLines` (more chunks) and a 144k-window model gets a larger `maxLines` (fewer chunks) — both computed from the SAME diff and the SAME chunker, with zero files ever dropped.

## Technical Implementation
### Steps
1. In `internal/payload/sizing.go` (created by Task 02), add a chunk-plan helper, e.g. `ChunkMaxLines(model string, cfg ...)` (or `(b EffectiveBudget) MaxLines()` if Task 02's effective-budget type is a struct — align the signature with whatever Task 02 actually lands), that takes the per-model effective input byte/token budget already computed by Task 02 and converts it to an integer `maxLines` value: `maxLines = effectiveInputBytes / avgBytesPerLine`, using a conservative average-bytes-per-line constant (document the anchor — do not invent a new ratio unrelated to Task 02's ~3.5 B/token conversion; derive `avgBytesPerLine` from the same conservative-estimate spirit, e.g. reuse the existing `internal/registry/config.go` `DefaultMaxContextLines` doc comment's own line/token relationship as a sanity cross-check without importing `internal/registry`). Clamp the result to a sane positive minimum (e.g. never return 0 or negative — fall back to a small positive floor) so `chunkDiff`'s `maxLines <= 0` disables-chunking branch (`internal/fanout/chunker.go:116`) is never triggered unintentionally by a pathologically small model window.
2. In `internal/fanout/review.go`, at the chunked-strategy branch (currently lines 865-876), replace `ml := ac.EffectiveMaxContextLines()` with a call into the new `internal/payload` chunk-plan helper, passing the agent's resolved model (via `ac.Model` or whatever field Task 01's resolver reads) and the already-computed Task 02 effective budget for this agent. Keep the resulting local variable named `ml` (or equivalent) so the rest of the branch (`chunkDiff(mp.Text, ml)` at line 876, the oversized-chunk warning loop at lines 884-896 that also reads `ml`) needs no further changes — the wiring change is isolated to how `ml` is computed, not how it is consumed.
3. Verify the two warning call sites in `internal/fanout/review.go:884-896` that reference `ml` (the "single file's diff exceeds max_context_lines" warning) still read correctly against the new per-model `ml` — update the warning message only if it still hardcodes `max_context_lines` phrasing that would now be misleading against a model-derived value (keep the message accurate to what actually gates the split).
4. Do NOT modify `internal/registry/config.go`'s `EffectiveMaxContextLines`/`MaxContextLines`/`DefaultMaxContextLines` — those stay as the config-explicit override path (an operator can still hand-set `max_context_lines` per agent); this task only changes what `review.go`'s chunked branch feeds `chunkDiff` when no explicit per-agent override is more authoritative. Confirm with Task 02's resolver precedence whether an explicit `AgentConfig.MaxContextLines` should still win over the derived value — if Task 02 does not already specify this, resolve it here: an explicit operator-set `max_context_lines` takes precedence over the model-derived `maxLines` (least-surprise: explicit config wins).
5. Extend `internal/fanout/chunker_test.go`'s `TestChunkDiff` table-driven pattern (line 58) is NOT the right place for the new window-aware assertions since `chunkDiff` itself is unchanged — instead add new test cases to `internal/payload/sizing_test.go` asserting the chunk-plan helper returns a smaller `maxLines` for a 32k-window model than for a 144k-window model, and add an integration-style test (either in `internal/fanout/chunker_test.go` or a new focused test in `internal/fanout/review_test.go` if one exists) asserting that calling `chunkDiff` with the two respective derived `maxLines` values against the SAME diff produces MORE chunks for the 32k case and FEWER for the 144k case, with `strings.Join(chunks, "")` reproducing the original diff exactly (zero files dropped) in both cases.
6. Run `go build ./...` and `go test ./internal/payload/... ./internal/fanout/...` to confirm the wiring compiles and existing chunker/review tests still pass unmodified in behavior (same `maxLines` semantics, different value source).

## Files to Create/Modify
- `internal/payload/sizing.go` – modify (extend with chunk-plan helper, created by Task 02)
- `internal/fanout/review.go` – modify (lines 865-876, chunked-strategy branch: replace `EffectiveMaxContextLines()` source with model-derived `maxLines`)
- `internal/fanout/chunker_test.go` – modify (add/extend window-aware chunk-count assertions if the integration test lands here rather than in review_test.go)

## Documentation Links
- [Per-Agent Budget & Chunking](../documentation/per-agent-budget-and-chunking.md)

## Related Files (from codebase-discovery.json)
- `internal/fanout/chunker.go` — `chunkDiff` (line 111, unchanged), `maxChunksPerAgent` (line 99, unchanged ceiling), `mergeChunkResults`/`mergeResultGroup` (lines 154/188, unchanged downstream merge)
- `internal/fanout/review.go` — chunked-strategy branch (lines 865-876) and the oversized-chunk warning loop (lines 884-896)
- `internal/registry/config.go` — `EffectiveMaxContextLines`/`DefaultMaxContextLines` (lines 974-988, reference only, NOT modified per plan Constraints)

## Success Criteria
- [x] `internal/payload/sizing.go` exposes a chunk-plan helper that converts a per-model effective token budget into a `maxLines` value, consuming the Task 02 effective-budget output (no parallel budget type introduced)
- [x] `internal/fanout/review.go:865-876`'s chunked-strategy branch feeds `chunkDiff` a model-derived `maxLines` instead of the global `ac.EffectiveMaxContextLines()`, unless an explicit per-agent `max_context_lines` override is set (explicit config still wins)
- [x] A 32k-context-window model produces MORE chunks than a 144k-context-window model for the identical input diff — verified by test
- [x] Zero files dropped across the chunk plan for both the 32k and 144k cases — verified by `strings.Join(chunks, "") == diff` (or equivalent lossless-reassembly assertion) in the test
- [x] The existing `maxChunksPerAgent = 64` ceiling in `internal/fanout/chunker.go` is respected unchanged — no new ceiling logic introduced
- [x] `chunkDiff` itself (`internal/fanout/chunker.go:111`) is not modified — only its caller's `maxLines` argument source changes

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `internal/payload/sizing_test.go`: chunk-plan helper returns a smaller `maxLines` for a 32k-token-window model than for a 144k-token-window model given the same effective-budget inputs
- `internal/payload/sizing_test.go`: chunk-plan helper never returns a non-positive `maxLines` (clamped floor), even for a pathologically small resolved window
- `internal/fanout/chunker_test.go` (or `review_test.go`): calling `chunkDiff` with the 32k-derived `maxLines` vs the 144k-derived `maxLines` against the same multi-file diff produces a greater chunk count for the smaller window
- `internal/fanout/chunker_test.go` (or `review_test.go`): zero files dropped — `strings.Join(chunks, "")` reproduces the original diff exactly for both window sizes
- `internal/fanout/chunker_test.go`: existing `TestChunkDiff`/`TestChunkDiffBoundsChunkCount` continue to pass unmodified, confirming `chunkDiff`'s own behavior (file-boundary splitting, 64-chunk ceiling) is untouched

**Integration Tests:**
- `internal/fanout/review.go`'s chunked-strategy dispatch path (`review_strategy: chunked`), exercised end-to-end with two synthetic agents pointed at a 32k-window and a 144k-window model respectively against the same oversized diff, asserting the resulting slot/chunk counts differ as expected and both personas' merged results (via `mergeChunkResults`) cover every file in the original diff

**Test Files:**
- `internal/payload/sizing_test.go`
- `internal/fanout/chunker_test.go`

## Risk Mitigation
- Chunking a 32k model on a slow backend re-triggers more, smaller-timeout-window requests than before — this task does not change per-request timeouts; flag the interaction as input to Task-08 (F6 timeout scaling), which must design timeout scaling *with* this chunking behavior, not after.
- An overly conservative `avgBytesPerLine` estimate could under-fill a chunk (more chunks than strictly necessary) — acceptable per the plan's Conservatism NFR (under-filling is acceptable; overflow is not), but avoid stacking Task 02's already-conservative byte/token margin with a second independent conservative margin here without documenting why both are needed.
- If Task 02's effective-budget type/signature differs from what this task assumes, adjust the helper's parameter shape to match Task 02's actual output rather than introducing a second, parallel budget representation.

## Dependencies
- Task-01 (Context-Window Resolver) — supplies `ContextWindowTokens(model string) int`
- Task-02 (Per-Agent Effective Budget) — supplies the per-model effective input token/byte budget this task converts into `maxLines`

## Definition of Done
- [x] `internal/payload/sizing.go` chunk-plan helper implemented and exported for use by `internal/fanout`
- [x] `internal/fanout/review.go:865-876` wired to the new per-model `maxLines` source
- [x] AC3 verified: with `on_overflow: chunk`, an over-window payload is delivered whole across multiple appropriately-sized chunks — more chunks for a 32k model than a 144k model — with zero files dropped, confirmed by test
- [x] `chunkDiff` and `maxChunksPerAgent` in `internal/fanout/chunker.go` remain unmodified (wiring-only change per plan Constraints)
- [x] `go build ./...` succeeds
- [x] `go test ./...` passes
- [x] No changes to `internal/registry/config.go`'s `EffectiveMaxContextLines`/`DefaultMaxContextLines`/`MaxContextLines` resolvers
