# Task 02: Per-Agent Effective Input Budget (Reserve Output Tokens, Size Per Model)

**Source:** Plan 19.10 – Debt Item #2
**Priority:** P1 | **Effort:** M | **Type:** Fix

## Problem Statement
`internal/fanout/review.go` sizes every reviewer's payload against one global byte budget (`cfg.Settings.PayloadByteBudget`, default 524288 bytes / 512 KiB), applied identically at two call sites — `buildPayloads` (`internal/fanout/review.go:726`) and `PrepareReviewFromDiff` (`internal/fanout/review.go:464`) — via `payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)`. Neither call site knows or cares which model will consume the payload, and neither reserves any room for the model's own output.

`defaultMaxTokens = 8192` (`internal/fanout/review.go:954`) is applied to every reviewer call via `maxTokensPtr()` (`internal/fanout/review.go:958`) but is never subtracted from the input-sizing calculation. This is the confirmed root cause of the `dax` failure in the 19.6 run: `dax`'s model has a 32768-token window; litellm reported the boundary as 24577 input tokens, i.e. `32768 − 8192 + 1` — the payload sizer filled the input budget as if the full window were available for input alone, then the output cap pushed the call one token past the window on every single call.

Because `buildPayloads` builds one `modePayload` per payload *mode* (line 719-739) and shares its already-budgeted `Text` across every agent using that mode (`buildSlots`'s `add` closure reads `payloads[mode]` at line 852), a heterogeneous roster spanning a 32k-window model and a 144k-window model is sized identically today — the 32k model overflows and the 144k model is starved of context it could safely use.

## Solution Overview
Add an effective-budget helper in a new file `internal/payload/sizing.go` that computes, for a given model, the input-token budget that leaves room for the model's output cap and a fixed prompt-rendering overhead, then converts that token count to a byte count using the conservative ~3.5 B/token ratio (not the codebase's existing, more optimistic ~4.1 B/token comment at `internal/registry/project.go:89`). Wire this helper into the two `ApplyByteBudget` call sites in `internal/fanout/review.go` so each agent's payload is shed to fit its own model's window instead of one payload-mode-wide global budget.

This task supplies only the effective-budget *number* (bytes) per agent and the byte-shedding wiring — it explicitly does not touch chunk-plan derivation (feeding `chunkDiff` a window-aware `maxLines`), which is a separate task building on this one's output.

## Technical Implementation
### Steps
1. Create `internal/payload/sizing.go` (same package as `internal/payload/budget.go`, follow its doc-comment style: what/why/caveats). Depends on Task 01's `internal/payload/contextwindow.go` exposing `ContextWindowTokens(model string) int`.
2. In `sizing.go`, define the conservative byte→token ratio as a named constant, e.g. `const conservativeBytesPerToken = 3.5` (or an equivalent fixed-point form), with a doc comment explicitly contrasting it against the optimistic ~4.1 B/token assumption implied by the `payload_byte_budget` comment in `internal/registry/project.go:89` (`512 KiB ≈ 128k tokens`) and stating that a safety margin is intentional — over-reserving is acceptable, overflow is not.
3. In `sizing.go`, define `promptOverhead` as a conservative fixed token constant (persona/instruction wrapper text rendered around every payload by `renderAgent` — estimate generously since no such concept exists in the codebase yet; document the estimate's basis in a comment).
4. In `sizing.go`, add the exported effective-budget function, e.g.:
   ```go
   // EffectiveByteBudget returns the byte budget a model's payload must fit
   // within so that estimated input tokens ≤ contextWindow - defaultMaxTokens -
   // promptOverhead. It converts the resulting token budget to bytes using a
   // conservative ~3.5 B/token ratio (over-reserving on purpose — see
   // conservativeBytesPerToken). Returns 0 (meaning "no budget available") if
   // the reservation leaves zero or negative input tokens for a given model,
   // never a negative byte count.
   func EffectiveByteBudget(model string, outputTokens int) int64
   ```
   Internally: `effectiveTokens := max(0, ContextWindowTokens(model) - outputTokens - promptOverhead)`, then convert to bytes via the conservative ratio, rounding down (floor) so the byte budget never overshoots the token reservation.
5. In `internal/fanout/review.go`, thread the per-agent model into budget derivation:
   - At `buildPayloads` (line 719-739): this function currently builds one shared `modePayload` per mode before any agent is known, which is structurally incompatible with a true per-agent budget. Restructure so `ApplyByteBudget` is no longer called inside `buildPayloads` for the shared `Text` — instead, retain the unbudgeted `[]payload.FileEntry` per mode (extend `modePayload` or introduce a small unexported holder) and move the `ApplyByteBudget` call into `buildSlots`'s `add` closure (`internal/fanout/review.go:843-933`), where `ac` (the agent's `AgentConfig`, in scope at line 844) and its `ac.Model` are available. Compute the per-agent budget as `min(cfg.Settings.PayloadByteBudget, payload.EffectiveByteBudget(ac.Model, defaultMaxTokens))` when `cfg.Settings.PayloadByteBudget > 0` (0 continues to mean "unlimited" per `ApplyByteBudget`'s existing contract in `internal/payload/budget.go:48`) — the configured global cap still acts as a ceiling, but a small-window model's budget is never inflated past what its window can actually hold.
   - At `PrepareReviewFromDiff` (line 441-464): apply the same per-agent derivation. Since this entry point builds one shared diff-mode payload for every agent in the roster regardless of `EffectivePayloadMode` (line 431-433, "every agent reviews it regardless of its configured payload mode"), the per-agent budget must be applied at the same point it flows into `buildSlots` (`forceMode="diff"`, line 497) rather than once at line 464 — mirror the `buildPayloads` restructuring so the raw entries survive into `buildSlots` and get shed per agent there too.
   - Keep `payload.ApplyByteBudget` itself untouched (`internal/payload/budget.go:46`) — it remains the shed-to-fit mechanism; only its budget *argument* becomes per-agent-derived instead of the single global `cfg.Settings.PayloadByteBudget`.
6. Update `Truncation`/`FileCount` bookkeeping so it still reflects what each specific agent actually saw (post-per-agent-budget), consistent with the existing "FileCount reflects what the reviewer actually saw" comment at `internal/fanout/review.go:734-735` — this may now vary per agent within the same payload mode, which is expected and correct.
7. Add `go doc`-quality comments at each modified call site explaining why the budget is now per-agent (cite the `dax` boundary-overflow arithmetic) so a future reader does not "simplify" it back to one shared value.

## Files to Create/Modify
- `internal/payload/sizing.go` – create
- `internal/fanout/review.go` – modify (`buildPayloads` ~719-739, `PrepareReviewFromDiff` ~441-464, `buildSlots`'s `add` closure ~843-933, `defaultMaxTokens`/`maxTokensPtr` ~948-958 as the output-reservation input)

## Documentation Links
- [Per-Agent Budget & Chunking](../documentation/per-agent-budget-and-chunking.md)
- [Context-Window Resolver](../documentation/context-window-resolver.md)

## Related Files (from codebase-discovery.json)
- `internal/payload/budget.go` (`ApplyByteBudget` line 46, `FileEntry` line 11, `Truncation` line 23 — untouched, reused as-is)
- `internal/payload/contextwindow.go` (Task 01 deliverable — `ContextWindowTokens(model string) int`)
- `internal/fanout/review.go` (`buildPayloads`, `PrepareReviewFromDiff`, `buildSlots`, `defaultMaxTokens`, `maxTokensPtr`)
- `internal/registry/project.go:89` (the optimistic ~4.1 B/token comment this task's ratio deliberately rejects)
- `internal/registry/precedence.go:97` (`Settings.PayloadByteBudget int64`, the resolved concrete value read by `review.go`)

## Success Criteria
- [ ] `internal/payload/sizing.go` exposes an effective-byte-budget function keyed on model id and output-token reservation, using `ContextWindowTokens` from Task 01.
- [ ] The byte→token ratio used is ~3.5 B/token (or more conservative), never the ~4.1 B/token implied by `internal/registry/project.go:89`.
- [ ] Both `ApplyByteBudget` call sites in `internal/fanout/review.go` (the `buildPayloads` path and the `PrepareReviewFromDiff` path) derive their budget argument from the specific agent's model rather than one shared `cfg.Settings.PayloadByteBudget` value across the whole payload mode.
- [ ] The configured `cfg.Settings.PayloadByteBudget` (when > 0) still acts as an upper ceiling on the per-agent budget, preserving existing operator-configured caps.
- [ ] For a 32,768-token-window model with `defaultMaxTokens = 8192` reserved, the computed effective input budget in tokens is strictly less than `32768 - 8192 = 24576`, and the exact `dax` arithmetic (`24577 input tokens + 8192 output > 32768`) cannot recur.
- [ ] `payload.ApplyByteBudget` itself is unmodified — this task only changes what budget value callers pass in.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `internal/payload/sizing_test.go`: 32,768-token-window model — assert `EffectiveByteBudget` (or equivalent) yields an input-token budget ≤ `32768 - 8192 - promptOverhead`, converted to bytes at the conservative ratio.
- `internal/payload/sizing_test.go`: 144,941-token-window model (`otto`'s window per the F1 quick-reference table) — assert the input-token budget scales up proportionally and remains ≤ `window - 8192 - promptOverhead`.
- `internal/payload/sizing_test.go`: regression test explicitly named/commented to reference the `dax` boundary overflow — assert that for a 32768-token window, `effectiveInputTokens + defaultMaxTokens <= 32768` always holds (the `24577 + 8192 > 32768` class cannot recur) (AC2).
- `internal/payload/sizing_test.go`: unknown-model fallback — assert the function does not panic and uses `ContextWindowTokens`'s conservative default window.
- `internal/payload/sizing_test.go`: degenerate case where a model's window is smaller than `defaultMaxTokens + promptOverhead` — assert the function returns 0 (no crash, no negative budget) rather than a negative or wrapped value.

**Integration Tests:**
- `internal/fanout/review_test.go` (or a new sizing-focused test file in the same package): build a roster mix (a small-window agent and a large-window agent sharing the same payload mode) against an oversized diff and assert the small-window agent's rendered payload is smaller/differently truncated than the large-window agent's, proving the per-agent budget is actually applied rather than shared.
- Existing `buildPayloads`/`PrepareReviewFromDiff`/`buildSlots` tests continue to pass with the restructured (per-agent) budget plumbing — verify `FileCount`/`Truncation` bookkeeping stays internally consistent per agent.

**Test Files:**
- `internal/payload/sizing_test.go`
- `internal/fanout/review_test.go` (extend existing table-driven tests where the two call sites are exercised)

## Risk Mitigation
- Byte→token ratio too optimistic → residual overflow; mitigation: use the conservative ~3.5 B/token ratio plus a safety margin (never the optimistic ~4.1 B/token comment at `internal/registry/project.go:89`), and floor (round down) the byte conversion so the budget never overshoots the token reservation.
- Restructuring `buildPayloads`/`PrepareReviewFromDiff` to defer `ApplyByteBudget` into per-agent scope risks silently reverting to the old shared-budget behavior if a future edit re-hoists the call; mitigation: comment the call sites explaining why the budget must stay per-agent, and cover with the roster-mix integration test above.
- `promptOverhead` is a net-new, currently unmeasured concept; mitigation: pick a deliberately generous fixed constant and document its basis so it errs toward under-filling rather than overflow.

## Dependencies
- Task-01 (Context-Window Resolver) — supplies `internal/payload/contextwindow.go`'s `ContextWindowTokens(model string) int`, which this task's `EffectiveByteBudget` consumes directly.

## Definition of Done
- [ ] `internal/payload/sizing.go` created with the effective-budget helper and documented constants (ratio, promptOverhead).
- [ ] Both `internal/fanout/review.go` call sites (`buildPayloads`, `PrepareReviewFromDiff`) derive per-agent budgets via the new helper instead of the single global `cfg.Settings.PayloadByteBudget`.
- [ ] Unit tests cover the 32k-window and 144k-window cases plus the `dax`-boundary regression (AC1, AC2).
- [ ] `go test ./...` passes.
- [ ] No changes to `payload.ApplyByteBudget`'s signature or behavior.
