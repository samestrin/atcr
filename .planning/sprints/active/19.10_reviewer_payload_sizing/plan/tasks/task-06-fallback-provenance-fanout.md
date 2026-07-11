# Task 06: Fallback Provenance — Run-Level Surfacing in summary.json

**Source:** Plan 19.10 – Debt Item #5 (fanout-side provenance)
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
`internal/fanout/status.go`'s `AgentStatus` already carries `FallbackUsed`/`FallbackFrom` (`status.go:286-296`), and `internal/fanout/artifacts.go`'s `statusFor` (`artifacts.go:275-319`) already copies both fields unconditionally from the `Result` it is given — lines 284-285:

```go
FallbackUsed:           r.FallbackUsed,
FallbackFrom:           r.FallbackFrom,
```

Because `writePool` (`artifacts.go:93-159`) always receives results that have already passed through `mergeChunkResults`/`mergeResultGroup` (`internal/fanout/chunker.go:154-178`, `188-295` — called at `internal/fanout/review.go:591` before `writePool` runs at `review.go:646`), this per-agent plumbing is identical for the bulk (single-call, one chunk per persona) and chunked (N-chunk-per-persona, merged) paths. A slot served via the existing `Slot.Fallbacks` chain (`internal/fanout/engine.go:160-227`, `invokeSlot` at `engine.go:541-593`) already lands `fallback_used`/`fallback_from` in that persona's per-agent `status.json` today, regardless of strategy.

What is genuinely missing is a **run-level** signal. `PoolSummary` (`artifacts.go:31-72`) already aggregates other per-agent facts into an always-present run tally — `TruncatedZeroFindings` (`artifacts.go:38-51`, computed at `artifacts.go:138-143`) is the precedent — but nothing analogous exists for fallback substitutions: a caller (or a human) currently has to walk every entry in `PoolSummary.Agents` to learn whether *any* slot in the run was served by a fallback model. `internal/fanout/outcome.go`'s `Summary`/`summarize()` (`outcome.go:23-63`) also has no fallback tally, even though `summarize()` already receives the same post-merge `results` slice `writePool` does.

This gap blocks AC5 ("summary.json records the substitution ... verified by test/fixture") at the run level, and blocks Task 07's reconcile-side de-weighting from having a cheap way to know a run contains any substitution worth reconciling against, without a confirming test that the bulk (non-chunked) path's provenance actually survives to disk end-to-end.

## Solution Overview
Add a `FallbackCount` tally to both `Summary` (outcome.go) and `PoolSummary` (artifacts.go), following the exact always-present-field discipline `TruncatedZeroFindings` already established, so a `0` is distinguishable from an older summary.json that predates the field. Compute it once in `summarize()` (a single loop over `results`, mirroring the `Failed`/`Succeeded` tallying already there) so both `Outcome()` and `writePool` get it for free without re-deriving it from `statuses`. No change is required to `AgentStatus`, `statusFor`, or `mergeResultGroup` — their existing fallback plumbing is already correct for both paths; only their doc comments need a small clarification so the next reader does not re-diagnose this as missing. Close the loop with a fixture test that runs a fallback-serving slot through `WritePool` (and, for the strongest signal, through the real `Engine`) and asserts `fallback_used`/`fallback_from` land in the per-agent record and `fallback_count` lands in the run-level record.

## Technical Implementation
### Steps
1. In `internal/fanout/outcome.go`, add a `FallbackCount int` field to the `Summary` struct (`outcome.go:23-28`) with a doc comment explaining it counts results where `FallbackUsed` is true, siblings to `Total`/`Succeeded`/`Failed`. In `summarize()` (`outcome.go:49-63`), tally it inside the existing `for _, r := range results` loop (`if r.FallbackUsed { s.FallbackCount++ }`) — do not add a second loop.
2. In `internal/fanout/artifacts.go`, add `FallbackCount int \`json:"fallback_count"\`` to `PoolSummary` (`artifacts.go:31-72`), placed near `TruncatedZeroFindings` with a doc comment following that field's style (always present, not omitempty, counts run-level fallback substitutions across both bulk and chunked-merged results, unaffected by grounding/post-processing since fallback state is fixed before `findingsFor` runs). In `writePool` (`artifacts.go:93-159`), set `FallbackCount: sum.FallbackCount` on the `ps := PoolSummary{...}` literal (`artifacts.go:144-154`) — reuse the `sum` already computed at `artifacts.go:133`, do not recompute from `statuses`.
3. In `internal/fanout/status.go`, extend the `AgentStatus` doc comment above `FallbackUsed`/`FallbackFrom` (`status.go:281-296`) to state explicitly that these fields are populated identically for the bulk (single-chunk) and chunked-merged (`mergeResultGroup`) paths via `statusFor`, so a future reader does not re-open this as a gap. No field or logic change in this file.
4. In `internal/fanout/chunker.go`, add one sentence to the `mergeResultGroup` doc comment (`chunker.go:180-188`) cross-referencing the new `Summary.FallbackCount`/`PoolSummary.FallbackCount` run-level tally, so the existing per-chunk `fallbackFromSet` union (`chunker.go:212, 233-238, 261-268`) is documented as feeding the same run-level count. No logic change.
5. Add a unit test in `internal/fanout/outcome_test.go` asserting `summarize()`/`Outcome()` produce the correct `FallbackCount` for a mixed slice of fallback and non-fallback results (model after the existing `TestOutcome_PartialWhenSomeFail`-style tests at `outcome_test.go:73+`).
6. Add a fixture test in `internal/fanout/artifacts_test.go` (or a new `internal/fanout/fallback_provenance_test.go` if a dedicated file reads more cleanly — follow the precedent of `response_truncation_test.go:181` `TestWritePool_CountsTruncatedZeroFindings`) that calls `WritePool` directly with a bulk-shaped `Result` slice (no chunking involved) containing one `FallbackUsed: true, FallbackFrom: "<primary>"` entry and one clean entry, then unmarshal `summary.json` and assert: `ps.FallbackCount == 1`; the fallback agent's `AgentStatus.FallbackUsed`/`FallbackFrom` are correctly serialized; and a companion no-fallback run's raw JSON bytes contain `"fallback_count":0` (not an omitted key), locking the always-present discipline the same way `TruncatedZeroFindings` is locked.
7. Add an end-to-end test mirroring `response_truncation_e2e_test.go`'s style (`TestE2E_TruncatedZeroFindings_FailsOverAndRecords` at `response_truncation_e2e_test.go:23`): run the real `Engine` against a `slotWithFallback`-style primary-fails/fallback-succeeds slot (pattern already established at `response_truncation_test.go:15-21`, `outcome_test.go:23-39`), then call `WritePool` on the results and assert the on-disk `summary.json`/`status.json` carry the substitution — this is the concrete "bulk path" proof AC5 asks for, closing the gap that no such test currently exists (confirmed: no `Fallback` assertions exist today in `artifacts_test.go` or `status_test.go`).

## Files to Create/Modify
- `internal/fanout/outcome.go` – modify (`Summary` struct `outcome.go:23-28`, `summarize()` `outcome.go:49-63`: add `FallbackCount`)
- `internal/fanout/artifacts.go` – modify (`PoolSummary` struct `artifacts.go:31-72`, `writePool` `artifacts.go:93-159`: add and populate `FallbackCount`)
- `internal/fanout/status.go` – modify (doc comment only, `AgentStatus.FallbackUsed`/`FallbackFrom` at `status.go:281-296`; no field/logic change — already correct)
- `internal/fanout/chunker.go` – modify (doc comment only, `mergeResultGroup` at `chunker.go:180-188`; no logic change)
- `internal/fanout/outcome_test.go` – modify (add `FallbackCount` unit test)
- `internal/fanout/artifacts_test.go` – modify (add `WritePool` fallback-provenance fixture test), or new `internal/fanout/fallback_provenance_test.go`
- `internal/fanout/response_truncation_e2e_test.go`-style new test – add (end-to-end Engine → WritePool → summary.json fallback assertion), in a new or existing e2e test file

## Documentation Links
- [Fallback Provenance](../documentation/fallback-provenance.md)
- [on_overflow Policy](../documentation/on-overflow-policy.md) — the `fallback` arm (F4, Task 04) is gated on this work landing
- [Diagnosability Fields](../documentation/diagnosability-fields.md) — F8's per-agent field additions share the same `statusFor`/`writePool` attachment points

## Related Files (from codebase-discovery.json)
- `internal/fanout/chunker.go` (`mergeResultGroup` fallbackFromSet aggregation, `chunker.go:188-295`)
- `internal/fanout/status.go` (`AgentStatus`, `status.go:286`)
- `internal/fanout/engine.go` (`invokeSlot`, `engine.go:541-593`; `Result.FallbackUsed`/`FallbackFrom`, `engine.go:177-178`)
- `internal/fanout/review.go` (`mergeChunkResults` call at `review.go:591`, `writePool` call at `review.go:646`)

## Success Criteria
- [x] `Summary` (outcome.go) and `PoolSummary` (artifacts.go) both carry an always-present `FallbackCount`/`fallback_count` field, computed once in `summarize()` and never re-derived elsewhere
- [x] A bulk-path (non-chunked) run with a fallback-served slot produces a `status.json` with `fallback_used:true`/`fallback_from:"<primary>"` and a `summary.json` with `fallback_count` reflecting it — proven by a fixture test, not just code inspection
- [x] A run with zero fallback substitutions serializes `"fallback_count":0` explicitly (never an omitted key), matching the `TruncatedZeroFindings` always-present discipline
- [x] No behavior change to `AgentStatus`/`statusFor`/`mergeResultGroup` — their existing fallback plumbing is verified correct via tests, not rewritten (doc comments only)
- [x] `PoolSummary`'s doc comment documents `FallbackCount` the same way it documents `TruncatedZeroFindings`

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `summarize()`/`Outcome()` return the correct `FallbackCount` for a mixed fallback/non-fallback `[]Result` slice
- `PoolSummary.FallbackCount` equals `Summary.FallbackCount` after `writePool` (no drift between the two tallies)
- A zero-fallback run's raw `summary.json` bytes contain the literal `"fallback_count":0`

**Integration Tests:**
- Fixture: `WritePool` given a bulk-shaped `Result` slice with one `FallbackUsed`/`FallbackFrom` entry writes a `status.json` and `summary.json` that both correctly record the substitution (`internal/fanout/artifacts_test.go` or new `fallback_provenance_test.go`)
- End-to-end: a real `Engine.Run` against a primary-fails/fallback-succeeds `Slot` (bulk, un-chunked), piped through `WritePool`, produces on-disk artifacts recording the substitution — the direct AC5 "verified by test/fixture" proof for the bulk path

**Test Files:**
- `internal/fanout/outcome_test.go`
- `internal/fanout/artifacts_test.go`
- `internal/fanout/fallback_provenance_test.go` (new, if not folded into `artifacts_test.go`)

## Risk Mitigation
- Fallback silently corrupts reconcile CONFIDENCE — mitigation: F5 records every substitution at both the per-agent (`status.json`) and run (`summary.json`) levels, and this task adds the fixture/e2e tests that make a future regression (e.g. a refactor of `writePool` or `mergeChunkResults` that drops the fields) fail CI instead of shipping silently
- Risk of duplicating scope with Task 07: this task stops at `internal/fanout` artifacts; it does not touch `internal/reconcile/disagree.go`, `internal/reconcile/emit.go`, `internal/stream.Finding`, or `JSONFinding` — those are Task 07's exclusive scope
- Risk of mis-diagnosing already-working code as broken: Steps 1-2 are additive (a new run-level tally); Steps 3-4 are comment-only clarifications of code confirmed correct by reading `artifacts.go:275-319` and `chunker.go:188-295` during grounding — no `AgentStatus`/`statusFor`/`mergeResultGroup` logic is touched

## Dependencies
- None hard — can run in parallel with Task-04/Task-05

## Definition of Done
- `Summary.FallbackCount` and `PoolSummary.FallbackCount` implemented and populated from a single `summarize()` computation
- Fixture test proves the bulk (non-chunked) path's fallback substitution survives to `status.json` and `summary.json`
- End-to-end test proves the real `Engine` → `WritePool` pipeline records a fallback substitution
- `AgentStatus`/`PoolSummary`/`mergeResultGroup` doc comments updated to reflect the confirmed bulk-and-chunked coverage
- `go test ./...` passes
- `go vet ./...` and the project's lint gate pass
- No changes outside `internal/fanout` (reconcile-side de-weighting is Task 07)
