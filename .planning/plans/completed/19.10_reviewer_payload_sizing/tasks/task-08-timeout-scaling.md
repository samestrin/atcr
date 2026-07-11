# Task 08: Timeout Scaling with Chunk Count / Payload Load (F6)

**Source:** Plan 19.10 – Debt Item #6
**Priority:** P1 | **Effort:** S | **Type:** Fix

## Problem Statement
`greta`, `vera`, and `brad` hit `context deadline exceeded` against the local litellm endpoint — `brad` at exactly 599,998 ms, i.e. the flat `timeout_secs: 600` wall (`.atcr/config.yaml:20`, `DefaultTimeoutSecs = 600` at `internal/registry/config.go:25`). This wall is applied identically regardless of how much work a given run actually has to do. Two deadline seams currently apply this flat value unmodified:
- `internal/fanout/engine.go:604-610` (`invokeAgent`) — bounds a single agent call to `a.TimeoutSecs` (set from `AgentConfig.EffectiveTimeoutSecs()` by `renderAgent`/`buildFallbackAgent` in `internal/fanout/review.go:1039` / `internal/fanout/review.go:1113`).
- `internal/fanout/review.go:512-518` (`runEngine`) — bounds the ENTIRE fan-out (every slot, including a serial lane's sequential chunk calls) to `p.TimeoutSec` (`cfg.Settings.TimeoutSecs`, set at `internal/fanout/review.go:369`/`398`).

Task 03's window-aware chunking (`internal/fanout/review.go:865-916`) fans a small-context-window model's diff into multiple chunk-Slots for the SAME persona (`chunks := chunkDiff(mp.Text, ml)`, one `renderAgent` call per chunk at `review.go:907`, one `Slot` appended per chunk at `review.go:915`). When that persona is a `SerialAgents` entry, its chunk-Slots run one at a time in the serial lane, so the aggregate wall-clock for that persona is roughly `chunkCount x per-call duration` — even though each individual chunk call fits comfortably inside its own per-agent timeout, the SUM across chunks can exceed `runEngine`'s single flat deadline. This is exactly the interaction Task 03 flagged as input to this task (`task-03-window-aware-chunking.md`:Risk Mitigation): "this task does not change per-request timeouts; flag the interaction as input to Task-08 (F6 timeout scaling), which must design timeout scaling *with* this chunking behavior, not after."

## Solution Overview
Scale both deadline seams from the already-resolved `AgentConfig.EffectiveTimeoutSecs()` base, using the chunk count Task 03 already computes — without touching `internal/registry`'s resolvers or adding a new config-schema field:

1. Carry the chunk count onto the `Agent`/`Slot` so `invokeAgent`'s per-call deadline (`engine.go:610`) can apply a bounded per-chunk floor for slow local backends.
2. Carry the PER-LANE (per-persona) chunk count into `runEngine`'s overall deadline (`review.go:516`) so a serial lane doing N sequential chunk calls gets `N x base` (clamped), not a single flat `base`.

Both scale factors are derived deterministically from `(chunk count, resolved base timeout)` — no live/network inputs — and both clamp to the existing schema-validated upper bound (`registry.MaxTimeoutSecs = 86400`, `internal/registry/precedence.go:11`) so a pathological chunk count cannot produce an unbounded deadline.

## Technical Implementation
### Steps
1. In `internal/fanout/engine.go`, add a `ChunkTotal int` field to the `Agent` struct (near `TimeoutSecs`, `engine.go:103-105`), documented as: "the number of chunk-Slots this persona's diff was split into by the chunked strategy (Task 03); 0 or 1 means unchunked — a single call." Default zero value (unset) preserves current behavior for every non-chunked caller (bulk path, doctor/direct construction, fallback agents).

2. In `internal/fanout/review.go`'s chunked-strategy branch (`review.go:897-916`), after `chunks := chunkDiff(mp.Text, ml)` (`review.go:876`) and inside the `if len(chunks) > 1` block, set `primary.ChunkTotal = len(chunks)` on the `Agent` returned by `renderAgent` (`review.go:907`) before appending the `Slot` (`review.go:915`). This threads the already-known chunk count onto the Agent with no new derivation — `len(chunks)` is computed once per persona and reused for every chunk-Slot of that persona.

3. Add a small, deterministic scaling helper in `internal/fanout` (e.g. a new unexported function `scaledTimeoutSecs(baseSecs, chunkTotal int) int` placed near `invokeAgent` in `engine.go`, or a new `internal/fanout/timeout.go` if that keeps `engine.go` readable). Formula:
   - `chunkTotal <= 1` (or `0`, unset): return `baseSecs` unchanged (current behavior, zero regression for non-chunked agents).
   - `chunkTotal > 1`: return `baseSecs` scaled by chunk count with a sub-linear or capped multiplier (e.g. `baseSecs * min(chunkTotal, chunkTimeoutCeilingFactor)` or a documented additive-per-extra-chunk term) — the exact curve is an implementation choice, but it MUST (a) be monotonically non-decreasing in `chunkTotal`, (b) be deterministic given `(baseSecs, chunkTotal)` only, and (c) clamp the result to `registry.MaxTimeoutSecs` (`internal/registry/precedence.go:11`, currently `86400`) as the ceiling so a pathological chunk count cannot produce an unbounded deadline. Document the chosen constant(s) inline with the same "conservative estimate, not a live measurement" framing Task 01/02/03 used for their byte/token ratios.

4. Apply the helper at the per-call seam: in `internal/fanout/engine.go`'s `invokeAgent` (`engine.go:604-612`), replace the guard `if a.TimeoutSecs > 0 { ctx, cancel = context.WithTimeout(ctx, time.Duration(a.TimeoutSecs)*time.Second) }` with a scaled variant: compute `scaled := scaledTimeoutSecs(a.TimeoutSecs, a.ChunkTotal)` and apply `context.WithTimeout(ctx, time.Duration(scaled)*time.Second)` under the same `a.TimeoutSecs > 0` guard (an unset base timeout still means "global deadline only," unaffected by chunking). This gives each individual chunk call a raised ceiling proportional to how fragmented its persona's diff is, covering the "slow local backend" half of F6.

5. Apply the helper at the aggregate seam: in `internal/fanout/review.go`'s `runEngine` (`review.go:512-518`), before `if p.TimeoutSec > 0 { ... }`, compute the maximum per-lane chunk total across `p.Slots` — group `Serial: true` slots by `Primary.Name` (persona) and take the largest `Primary.ChunkTotal` observed for any single persona (this bounds the worst-case sequential lane; parallel lanes do not accumulate wall-clock the same way and are already covered per-call by Step 4). Feed that count into `scaledTimeoutSecs(p.TimeoutSec, maxLaneChunkTotal)` and use the result as the `context.WithTimeout` duration instead of the raw `p.TimeoutSec`. This is the seam that actually fixes the reported failure: a serial persona fanned into 6+ sequential chunk calls now gets an overall deadline scaled to its lane's real chunk count instead of the flat 600s wall.

6. Verify `PreparedReview.Slots` (`review.go:173-177`) already carries everything Step 5 needs (`[]Slot`, each with `Primary.Name`, `Primary.ChunkTotal`, `Serial`) — no new field on `PreparedReview` itself should be necessary; the grouping in Step 5 is computed inline in `runEngine` from `p.Slots`.

7. Run `go build ./...` and `go test ./internal/fanout/...` to confirm both seams compile and existing timeout/deadline tests still pass unmodified for the non-chunked (chunkTotal <= 1) case.

## Files to Create/Modify
- `internal/fanout/engine.go` – modify (`Agent` struct: add `ChunkTotal int`; `invokeAgent`, lines 604-612: scale the per-call deadline; add `scaledTimeoutSecs` helper)
- `internal/fanout/review.go` – modify (chunked-strategy branch ~897-916: set `ChunkTotal` on chunk Agents; `runEngine`, lines 512-518: scale the aggregate deadline by max per-lane chunk total)

## Documentation Links
- [Timeout Scaling](../documentation/timeout-scaling.md)

## Related Files (from codebase-discovery.json)
- `internal/registry/config.go` — `AgentConfig.EffectiveTimeoutSecs()` (line 948, resolved base; NOT modified), `DefaultTimeoutSecs = 600` (line 25)
- `internal/registry/precedence.go` — `MaxTimeoutSecs = 86400` (line 11, reused as the clamp ceiling; NOT modified)

## Success Criteria
- [ ] `Agent.ChunkTotal` is threaded from the chunked-strategy branch (`review.go:897-916`) onto every chunk-Slot's `Primary` Agent, reflecting the true `len(chunks)` for that persona
- [ ] `invokeAgent` (`engine.go:604-612`) applies a scaled per-call deadline derived from `(a.TimeoutSecs, a.ChunkTotal)`, unchanged (`a.TimeoutSecs` as-is) when `ChunkTotal <= 1`
- [ ] `runEngine` (`review.go:512-518`) applies a scaled aggregate deadline derived from `(p.TimeoutSec, max per-serial-lane chunk total)` instead of the flat `p.TimeoutSec`
- [ ] The scaled timeout at both seams clamps to `registry.MaxTimeoutSecs` (86400) and is deterministic from `(base, chunkTotal)` alone — no live/network inputs
- [ ] `internal/registry`'s `EffectiveTimeoutSecs`, `DefaultTimeoutSecs`, and `MaxTimeoutSecs` are unmodified — F6 reads the resolved value and clamp constant only
- [ ] AC6: `greta`, `vera`, and `brad` (the three previously-timed-out agents) complete on a large-but-valid multi-chunk payload without hitting the wall, verified by test

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `internal/fanout/engine_test.go`: `scaledTimeoutSecs(base, chunkTotal)` returns `base` unchanged for `chunkTotal` of `0` and `1`
- `internal/fanout/engine_test.go`: `scaledTimeoutSecs(base, chunkTotal)` returns a strictly larger value for `chunkTotal = 6` than for `chunkTotal = 1`, and is monotonically non-decreasing across increasing `chunkTotal`
- `internal/fanout/engine_test.go`: `scaledTimeoutSecs` clamps its result to `registry.MaxTimeoutSecs` for a pathologically large `chunkTotal`
- `internal/fanout/engine_test.go`: `invokeAgent`'s context deadline reflects the scaled value when `Agent.ChunkTotal > 1` (assert via a short base timeout + a slow stub completer that succeeds only under the scaled deadline, fails under the unscaled one)

**Integration Tests:**
- `internal/fanout/review_test.go` (or wherever `runEngine`/`ExecuteReview` is exercised): a synthetic serial-lane persona fanned into 6+ chunk-Slots against a stub completer with a per-call latency that would exceed the flat `p.TimeoutSec` in aggregate but completes within the scaled deadline — assert the run finishes with `StatusOK` for every chunk rather than `StatusTimeout`
- Reproduce the reported failure mode: a large-but-valid multi-chunk payload routed through `greta`/`vera`/`brad`-equivalent agent configs (small-context-window model, slow-backend-shaped stub latency) completes without `context deadline exceeded` (AC6)

**Test Files:**
- `internal/fanout/engine_test.go`
- `internal/fanout/review_test.go`

## Risk Mitigation
- Chunking a 32k model on a slow backend re-triggers timeouts if scaling is bolted on after chunking lands instead of co-designed with it — mitigated by Step 5 reading Task 03's actual `len(chunks)` output directly rather than an independent estimate, and by Step 2 threading that exact count onto the Agent the same call that creates it.
- An overly aggressive multiplier could mask a genuinely hung/broken backend behind a very long deadline — mitigated by clamping to the existing schema-validated `registry.MaxTimeoutSecs` ceiling (Step 3c) rather than leaving the scaled value unbounded.
- Scaling only the per-call seam (Step 4) without the aggregate seam (Step 5) would still leave a serial lane's cumulative wall-clock exposed to the flat `runEngine` deadline — both seams are required together for AC6 to hold for serial agents.

## Dependencies
- Task-03 (Window-Aware Chunking) — supplies the chunk count (`len(chunks)`) driving the timeout scaling; this task must land after Task-03's chunked-strategy branch exists in `review.go`

## Definition of Done
- [ ] `Agent.ChunkTotal` field added and populated from the chunked-strategy branch
- [ ] `scaledTimeoutSecs` helper implemented, deterministic, and clamped to `registry.MaxTimeoutSecs`
- [ ] `invokeAgent` (per-call seam, `engine.go:610`) and `runEngine` (aggregate seam, `review.go:516`) both apply the scaled timeout
- [ ] AC6 verified: `greta`, `vera`, `brad` complete on a large multi-chunk payload without hitting the wall
- [ ] No changes to `internal/registry`'s `EffectiveTimeoutSecs`, `DefaultTimeoutSecs`, `MaxTimeoutSecs`, or any config-schema timeout field
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
