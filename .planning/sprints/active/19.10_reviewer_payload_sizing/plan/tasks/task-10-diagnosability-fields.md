# Task 10: Diagnosability Fields (F8)

**Source:** Plan 19.10 – Debt Item #8
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
When a reviewer's payload degrades — chunked, truncated, fallen back to a substitute model, or hard-failed — nothing in `status.json`/`summary.json` records what actually happened for that specific agent. `AgentStatus` (`internal/fanout/status.go:286`) already tracks `PayloadMode`, `Truncated`/`FilesDropped`, `FallbackUsed`/`FallbackFrom`, `CacheHit`, and `UnreviewedChunks`, but has no field for the per-model sizing math that produced those outcomes: what effective input budget the agent was sized against, what context window was resolved for its model, how many output tokens were reserved, how many chunks its diff was split into, or which `on_overflow` action (`chunk`/`truncate`/`fallback`/`fail`) actually fired. Post-hoc, an operator investigating a degraded run (e.g. the `dax`/`otto` 19.6 boundary-overflow incident this plan traces back to) has no artifact-level way to see why a given agent's payload was sized the way it was — only stderr logs, if those were even captured.

## Solution Overview
Extend `AgentStatus` in `internal/fanout/status.go` with five new fields — `effective_budget`, `resolved_window`, `reserved_output_tokens`, `chunk_count`, `degradation_action` — following the struct's existing additive/omitempty discipline (see the `CacheHit`/`UnreviewedChunks`/tool-loop-pointer precedents immediately above the insertion point) so that an unaffected run's `status.json`/`summary.json` stays byte-identical to today's output. Populate them in `statusFor` (`internal/fanout/artifacts.go:275`) by reading the sizing/chunk-plan/overflow-dispatch values Tasks 02, 03, and 04 attach to `Result` (`internal/fanout/engine.go:171`), and the fallback provenance Task 06 already threads through `FallbackUsed`/`FallbackFrom`. This task is a pure aggregation/wiring step — it introduces no new sizing logic, only records values other tasks already compute. `PoolSummary` (`internal/fanout/artifacts.go:31`) requires no change: it already embeds `[]AgentStatus` by value, so the extended struct flows through `writePool` (`internal/fanout/artifacts.go:93-121`) automatically once `statusFor` populates it.

## Technical Implementation
### Steps
1. In `internal/fanout/status.go`, add five new fields to `AgentStatus` (after the `UnreviewedChunks` field at line 347, following its comment style — cite the epic/plan and explain the omitempty rationale):
   ```go
   // Diagnosability (Epic 19.10 F8): per-agent payload-sizing and degradation
   // record, populated whenever the sizing/chunk-plan/overflow-dispatch path
   // (Tasks 02-04) produced a value for this agent. omitempty/pointer so a
   // pre-19.10 run (or an agent that never entered per-model sizing, e.g. a
   // cache-hit replay) keeps status.json byte-identical to the pre-F8 shape.
   EffectiveBudget       int64  `json:"effective_budget,omitempty"`
   ResolvedWindow        int    `json:"resolved_window,omitempty"`
   ReservedOutputTokens  int    `json:"reserved_output_tokens,omitempty"`
   ChunkCount            int    `json:"chunk_count,omitempty"`
   DegradationAction     string `json:"degradation_action,omitempty"`
   ```
   Use plain `omitempty` scalars (not pointers) unless a genuine "explicit zero vs. absent" ambiguity exists for a field — `chunk_count: 0` and "chunking never ran" are the same observable state here (unlike `UnreviewedChunks`, where an explicit 0 must be distinguishable from "field predates this run"), so match whichever convention Tasks 02/03/04 actually need; if a reviewer that WAS sized still legitimately has `chunk_count == 0` (single-chunk, non-degraded), confirm this is indistinguishable from "not sized" is acceptable — it is, since `effective_budget`/`resolved_window` being non-zero is the actual "was this agent sized" signal.
2. Confirm what field names/types Tasks 02, 03, 04, and 06 actually landed on `Result` (`internal/fanout/engine.go:171`) — this task depends on their output and must not invent a parallel representation. Expect (adjust to match reality if these tasks used different names):
   - Task 02 (`internal/payload/sizing.go` `EffectiveByteBudget`) — the per-agent effective budget in bytes and the resolved context-window token count for the agent's model (likely threaded onto `Result` as something like `EffectiveBudget int64` / `ResolvedWindow int`, or recoverable via `payload.ContextWindowTokens(r.Model)` if `Result` doesn't carry it directly).
   - Task 03 (window-aware chunking) — the chunk count for a chunked agent (from `len(chunkDiff(...))` at the `internal/fanout/review.go:865-876` call site, likely threaded onto `Result` as `ChunkCount int` or derivable from an existing chunk-related field if one already exists post-Task-03).
   - Task 04 (`internal/fanout/overflow.go` `OverflowResult.Action`) — the degradation action string (`"chunk"`, `"truncate"`, `"fallback"`, `"fail"`, or empty when no overflow policy fired), likely threaded onto `Result` as `DegradationAction string` or read directly off the `OverflowResult` returned at the dispatch call site.
   - `defaultMaxTokens` (`internal/fanout/review.go:954`) — the reserved-output-tokens value is already a package constant; `reserved_output_tokens` records this same constant per agent (or the agent-specific override if one exists post-Task-02) rather than requiring new plumbing.
   If any of Tasks 02/03/04/06 did not surface a needed value on `Result`, add the minimal additional field to `Result` in `internal/fanout/engine.go` needed to carry it through — do not recompute sizing math independently in `statusFor`.
3. In `internal/fanout/artifacts.go`, extend `statusFor` (line 275-319) to populate the five new fields from `r Result`, following the existing pattern used for `Model`/`TokensIn`/`TokensOut` (only set when a real value exists, e.g. guard `resolved_window`/`effective_budget` population behind "this agent went through per-model sizing" the same way token usage is guarded behind `r.TokensIn > 0 || r.TokensOut > 0`) and for `Turns`/`ToolCalls` (tool-enabled-only fields). A minimal direct assignment is likely sufficient since all five are now plain (non-pointer) omitempty scalars:
   ```go
   EffectiveBudget:      r.EffectiveBudget,
   ResolvedWindow:       r.ResolvedWindow,
   ReservedOutputTokens: r.ReservedOutputTokens,
   ChunkCount:           r.ChunkCount,
   DegradationAction:    r.DegradationAction,
   ```
4. Do not modify `PoolSummary` (`internal/fanout/artifacts.go:31`) or `writePool` (line 93-121) — both already carry `[]AgentStatus` through unchanged; the extended struct shape flows to `summary.json` for free once `statusFor` populates it. Verify this by inspection only (no code change expected here).
5. Confirm `WriteStatus` (`internal/fanout/status.go:354`, the `FilesDropped` nil-normalization function) needs no change — it only special-cases `FilesDropped`; the five new scalar fields need no analogous nil-normalization since they are not slices/maps.
6. Run `go build ./...` then `go test ./internal/fanout/... ./internal/payload/...` to confirm the extension compiles and no existing status/summary fixture comparison breaks.

## Files to Create/Modify
- `internal/fanout/status.go` – modify (`AgentStatus` struct, line 286-348: add 5 new fields)
- `internal/fanout/artifacts.go` – modify (`statusFor`, line 275-319: populate the 5 new fields from `Result`)
- `internal/fanout/engine.go` – modify only if Tasks 02/03/04/06 did not already surface a needed value on `Result` (line 171-onward); prefer reusing whatever fields those tasks landed over adding new ones

## Documentation Links
- [Diagnosability Fields](../documentation/diagnosability-fields.md)
- [Per-Agent Budget & Chunking](../documentation/per-agent-budget-and-chunking.md)
- [on_overflow Policy](../documentation/on-overflow-policy.md)
- [Fallback Provenance](../documentation/fallback-provenance.md)

## Related Files (from codebase-discovery.json)
- `internal/fanout/status.go` (`AgentStatus`, line 286)
- `internal/fanout/artifacts.go` (`PoolSummary`/`writePool`, line 31/93; `statusFor`, line 275)
- `internal/fanout/engine.go` (`Result`, line 171 — the source struct `statusFor` reads from)
- `internal/payload/budget.go` (`Truncation`, line 17-27 — the non-silent-degradation-record pattern this task's fields must match)

## Success Criteria
- [ ] `AgentStatus` carries all 5 new fields (`effective_budget`, `resolved_window`, `reserved_output_tokens`, `chunk_count`, `degradation_action`) with `omitempty` JSON tags
- [ ] `statusFor` populates all 5 fields from the values Tasks 02/03/04 attach to `Result`, with no independent recomputation of sizing/chunk/overflow logic in `artifacts.go`
- [ ] `summary.json` records all 5 new fields per agent for a run that goes through per-model sizing/chunking/overflow dispatch (AC8)
- [ ] A run/agent that predates or bypasses per-model sizing (e.g. a cache-hit replay, or a test fixture built before this task) produces a `status.json`/`summary.json` with the 5 new fields entirely absent (`omitempty` fires), keeping the artifact byte-identical to the pre-F8 shape
- [ ] `degradation_action` reflects the actual `OverflowResult.Action` value from Task 04's dispatch (`chunk`/`truncate`/`fallback`/`fail`), or is absent when no overflow policy fired for that agent

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `internal/fanout/status_test.go`: marshal an `AgentStatus` with all 5 new fields set to non-zero/non-empty values — assert each field name/value round-trips through JSON correctly.
- `internal/fanout/status_test.go`: marshal an `AgentStatus` with the 5 new fields left at their zero values (unset) — assert the resulting JSON contains none of the 5 new keys (`omitempty` verified).
- `internal/fanout/artifacts_test.go`: existing byte-identical/fixture-comparison test(s) for `status.json`/`summary.json` (search for any golden-file or exact-JSON assertion in `artifacts_test.go`) continue to pass unmodified for a `Result` that never goes through per-model sizing — proves the additive discipline holds.
- `internal/fanout/artifacts_test.go`: `statusFor` given a `Result` with the sizing/chunk/overflow fields populated produces an `AgentStatus` with the matching 5 diagnosability fields set (direct pass-through, no transformation).

**Integration Tests:**
- `internal/fanout/artifacts_test.go` (or a `writePool`-level test): a `WritePool`/`writePool` run over a roster that includes at least one chunked agent and one overflow-degraded agent produces a `summary.json` whose `agents[]` entries carry the expected `chunk_count` and `degradation_action` values per agent, confirming end-to-end wiring from dispatch through to the written artifact (AC8).

**Test Files:**
- `internal/fanout/artifacts_test.go`
- `internal/fanout/status_test.go`

## Risk Mitigation
- **Risk:** Breaking existing `summary.json`/`status.json` consumers (`reconcile`, `scorecard`) by making a new field required or non-omitempty. **Mitigation:** strict additive/omitempty discipline — every new field is a plain omitempty scalar, matching the struct's established convention; verified by the zero-value JSON-absence unit test above.
- **Risk:** `statusFor` silently recomputing sizing/chunk/overflow values independently (drifting from what Tasks 02/03/04 actually computed) instead of reading them off `Result`. **Mitigation:** this task only reads existing `Result` fields; if a needed value is missing from `Result`, add the minimal carrier field in `engine.go` rather than deriving it a second time in `artifacts.go`.
- **Risk:** `degradation_action` landing as a silent/absent signal when an overflow policy DID fire (defeating F8's purpose). **Mitigation:** match the `Truncation{Truncated, FilesDropped}` "always returned, never silent" convention — Task 04's `OverflowResult.Action` is always populated by the dispatcher, so `statusFor` should assign it unconditionally whenever the agent went through overflow dispatch, not only on a happy path.

## Dependencies
- Task-02 (Per-Agent Effective Budget) — supplies `effective_budget`/`resolved_window` values
- Task-03 (Window-Aware Chunking) — supplies `chunk_count`
- Task-04 (on_overflow Policy Dispatch) — supplies `degradation_action`
- Task-06 (Fallback Provenance) — the existing `FallbackUsed`/`FallbackFrom` fields this task's `degradation_action` complements (fallback substitution already recorded separately; no duplication needed)

## Definition of Done
- [ ] `AgentStatus` extended with the 5 new fields, all `omitempty`, matching the existing struct's doc-comment and field-grouping style
- [ ] `statusFor` populates the 5 fields from `Result`, reusing Task 02/03/04/06 output with no independent recomputation
- [ ] Unit tests confirm JSON round-trip for populated fields and JSON-absence for unset fields
- [ ] Integration test confirms `summary.json` records the 5 fields per agent end-to-end for a sizing/chunking/overflow-degraded run (AC8)
- [ ] Existing `status.json`/`summary.json` fixture/golden tests in `internal/fanout` continue to pass unmodified for pre-F8-shaped runs
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
