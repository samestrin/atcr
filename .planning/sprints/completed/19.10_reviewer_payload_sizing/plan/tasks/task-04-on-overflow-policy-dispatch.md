# Task 04: on_overflow Policy Dispatch

**Source:** Plan 19.10 – Debt Item #4
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
Overflow handling is currently hardcoded: there is no policy switch, so an oversized payload either gets silently byte-shed via `ApplyByteBudget` or, when litellm's `context_window_fallbacks` is unset, kills the agent outright ("No fallback model group found"). `otto` (144,941-token model) and `dax` (32,768-token model) both fail on a 506 KB diff because nothing routes the oversize condition to a deliberate degradation strategy. There is no single place in the code that answers "given this policy string, what should happen to this payload?" — that dispatch point does not exist yet.

## Solution Overview
Add `internal/fanout/overflow.go` implementing a single dispatch function that maps an already-resolved `on_overflow` policy string (`chunk`, `truncate`, `fallback`, `fail`) onto the correct code path: `chunk` calls the existing `chunkDiff` (internal/fanout/chunker.go:111) with the per-model `maxLines` computed in Task 03; `truncate` calls the existing `ApplyByteBudget` (internal/payload/budget.go:46) primitive, now gated explicitly behind this policy value instead of being the unconditional default; `fallback` and `fail` are recognized as valid strings but return a clear, typed error — `fallback` because its provenance-recording prerequisite (F5) is out of scope for this task, `fail` because hard-failing loudly IS the correct behavior for that arm. This task is dispatch-only: it assumes the policy string has already been validated/defaulted by config parsing (Task 05) and consumes it as a plain string parameter, not a config struct.

## Technical Implementation
### Steps
1. Create `internal/fanout/overflow.go`. Define an `OverflowPolicy` type (or reuse `string`) with named constants `OverflowChunk = "chunk"`, `OverflowTruncate = "truncate"`, `OverflowFallback = "fallback"`, `OverflowFail = "fail"`.
2. Implement a dispatch function, e.g. `applyOverflowPolicy(policy string, diff string, maxLines int, entries []payload.FileEntry, budget int64) (result OverflowResult, err error)` — the exact input signature should be adapted to whatever payload representation is passed in by the caller at the fan-out call site (diff string for the `chunk`/agent-prompt path, `[]payload.FileEntry` for the `truncate` path). Keep the two content representations distinct rather than forcing one artificial shared shape, since `chunkDiff` operates on a rendered diff string and `ApplyByteBudget` operates on `payload.FileEntry` slices.
3. Define an `OverflowResult` (or per-arm return) struct that always records the degradation action taken (e.g. `Action string` — `"chunk"`, `"truncate"`, `"none"`) plus arm-specific data (`Chunks []string` for chunk, `Truncation payload.Truncation` for truncate) — this becomes the diagnosability record consumed later by F8 in `summary.json`. Never rely on stderr-only signaling as the sole record, matching the existing `Truncation` struct's "always returned, never silent" pattern (internal/payload/budget.go:17-27).
4. `chunk` arm: call `chunkDiff(diff, maxLines)` and return the chunks as the result.
5. `truncate` arm: call `payload.ApplyByteBudget(entries, budget)` and return the kept entries plus the `Truncation` record.
6. `fallback` arm: return a typed/sentinel error (e.g. `ErrFallbackUnavailable`) with a message stating that fallback requires provenance-recording plumbing (F5) that is not present, so callers cannot silently proceed as if nothing happened.
7. `fail` arm: return a typed/sentinel error (e.g. `ErrOverflowPolicyFail`) immediately with a clear message that the payload exceeded budget and the configured policy is `fail`.
8. Unknown/unrecognized policy string: return a clear "unrecognized on_overflow policy %q" error rather than silently defaulting — defaulting-on-unknown belongs to Task 05's config parsing, not this dispatch function; this function should only accept the four known values.
9. Write `internal/fanout/overflow_test.go` covering: `chunk` default path produces the same chunks as calling `chunkDiff` directly; `truncate` path produces the same kept/dropped result as calling `ApplyByteBudget` directly; `fallback` returns a non-nil, clearly-worded error and performs no model swap; `fail` returns a non-nil, clearly-worded error; an unrecognized policy string returns a clear error.
10. Run `go build ./...` and `go test ./internal/fanout/...` to confirm the new file compiles and integrates with the existing `chunker.go`/`chunker_test.go` package without breaking any existing test.

## Files to Create/Modify
- `internal/fanout/overflow.go` – create
- `internal/fanout/overflow_test.go` – create

## Documentation Links
- [on_overflow Policy](../documentation/on-overflow-policy.md)

## Related Files (from codebase-discovery.json)
- `internal/fanout/chunker.go` — `chunkDiff` (line 111), the `chunk` arm's primitive
- `internal/payload/budget.go` — `ApplyByteBudget` (line 46) and `Truncation` (line 17), the `truncate` arm's primitive and its non-silent degradation record pattern

## Success Criteria
- [x] `internal/fanout/overflow.go` exposes a dispatch function/type that accepts a resolved `on_overflow` policy string and routes to the correct arm
- [x] `chunk` (default) arm calls `chunkDiff` with the per-model `maxLines` and returns the resulting chunks
- [x] `truncate` arm calls `ApplyByteBudget` and returns its `Truncation` record unmodified (no re-implementation of the shed logic)
- [x] `fallback` and `fail` arms are recognized as valid policy values and return clear, typed errors rather than silently no-op-ing or falling through to another arm
- [x] An unrecognized policy string produces a clear error distinct from the `fallback`/`fail` errors
- [x] Every dispatch outcome carries an explicit degradation-action record (`OverflowResult.Action`, never stderr-only signaling), matching the `Truncation` "always returned" convention

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `chunk` policy with a multi-file diff and a small `maxLines` produces the same chunk boundaries `chunkDiff` alone would produce
- `chunk` policy with `maxLines <= 0` returns the whole diff as a single chunk (matches `chunkDiff`'s documented unlimited case)
- `truncate` policy with entries exceeding `budget` returns `Truncation.Truncated == true` and the same `FilesDropped` set `ApplyByteBudget` alone would produce
- `truncate` policy with entries under `budget` returns `Truncation.Truncated == false`
- `fallback` policy returns a non-nil error whose message references the missing provenance prerequisite; no chunks/truncation are produced as a side effect
- `fail` policy returns a non-nil error immediately, with no chunking or truncation attempted first
- An unrecognized/empty policy string returns a clear "unrecognized policy" error

**Integration Tests:**
- Fan-out engine invokes the dispatch function once per oversized agent slot and receives back a well-formed `OverflowResult`/error that can be attached to the per-agent record consumed later by F8's `summary.json` diagnosability fields (verified at the unit level in this task; full `summary.json` wiring is out of scope here)

**Test Files:**
- `internal/fanout/overflow_test.go`

## Risk Mitigation
- **Risk:** Duplicating `chunkDiff`/`ApplyByteBudget` logic instead of delegating, causing the two implementations to drift. **Mitigation:** the dispatch function must call the existing primitives directly, not reimplement chunking or byte-shedding.
- **Risk:** `fallback`/`fail` silently no-op or fall through to `chunk`/`truncate` by accident (e.g. a missing `case` in a switch defaulting to the next arm). **Mitigation:** explicit `switch` with no fallthrough, each arm's test asserts a non-nil error and asserts no chunks/truncation were produced.
- **Risk:** Task 05 lands with different config defaulting assumptions than this task expects (e.g. defaulting unknown strings to `chunk` before calling dispatch, vs. expecting dispatch to reject unknowns). **Mitigation:** this task treats any non-exact-match string as an error at the dispatch layer, which is compatible with Task 05 either pre-validating or passing raw config through — dispatch remains the single source of truth for "what is a valid policy string."

## Dependencies
- Task-03 (Window-Aware Chunking) — supplies the per-model maxLines for the chunk arm

## Definition of Done
- [x] `internal/fanout/overflow.go` created and compiles cleanly
- [x] `internal/fanout/overflow_test.go` created and covers all four policy arms plus the unrecognized-policy case
- [x] `go build ./...` passes
- [x] `go test ./...` passes
- [x] `chunk` and `truncate` arms verified to delegate to `chunkDiff`/`ApplyByteBudget` without reimplementing their logic
- [x] `fallback` and `fail` arms verified to error clearly and never silently no-op
