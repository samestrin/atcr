# Acceptance Criteria: Existing Skip Chain and Skip/Failure Branches Remain Unaffected

**Related User Story:** [02: Skip Over-Ceiling Findings Safely](../user-stories/02-skip-over-ceiling-findings-safely.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function / `internal/verify` package | Regression coverage across the full `generateFixes` skip/failure surface |
| Test Framework | go test | Full `internal/verify` test suite plus targeted regression cases |
| Key Dependencies | `reclib.ConfidenceAtOrAbove`, `meetsSeverityFloor`, `hasFixAttribution`, `logPipelineWarning`, `reconcile.JSONFinding.FixWarning` | All pre-existing, unchanged in signature |

## Related Files
- `internal/verify/executor.go` - modify: verify the new ceiling check and self-decline branch are inserted additively (order preserved: confidence → severity → attribution → ceiling → dispatch) without altering the existing `continue` semantics of the confidence/severity/attribution checks (lines ~136-149) or the existing `executor_fix_failed`/`executor_truncated_fix`/`executor_empty_fix`/`executor_invalid_syntax` branches (lines ~189-228).
- `internal/verify/executor_test.go` - modify: existing test cases for confidence-below-floor, severity-below-floor, already-attributed, fix-failed, truncated, empty-fix, and invalid-syntax must continue to pass unmodified (or with only additive new cases, never edited assertions on existing cases).
- `internal/reconcile/emit.go` - reference only: `JSONFinding.FixWarning` (line 136) — verifies the field's end-to-end visibility contract into `findings.json` is unchanged for both old and new skip reasons.
- `internal/verify/pipeline.go` - reference only: `logPipelineWarning` (line 41) signature is unchanged; only a new class string value is introduced by this story.

## Happy Path Scenarios
**Scenario 1: Full pre-existing executor test suite passes unmodified**
- **Given** the complete pre-existing `internal/verify/executor_test.go` suite (confidence floor, severity floor, attribution idempotency, fix-failed, truncated, empty-fix, invalid-syntax cases)
- **When** this story's ceiling check and self-decline branch are added
- **Then** every pre-existing test continues to pass with no changes to its assertions (only new test cases are additive)

**Scenario 2: Skip-chain ordering is preserved**
- **Given** a finding that fails multiple gates simultaneously (e.g. below confidence floor AND above the ceiling)
- **When** `generateFixes` evaluates it
- **Then** it is skipped at the FIRST applicable gate in the existing order (confidence, then severity, then attribution, then the new ceiling check) via a bare `continue` with no `FixWarning`/log side effect from the earlier (pre-existing) gates — confirming the new ceiling check did not get inserted ahead of, or interleaved with, the existing silent skips

**Scenario 3: Existing failure branches are unaffected by the new self-decline branch**
- **Given** findings that trigger `executor_fix_failed` (provider error), `executor_truncated_fix` (finish_reason=length), `executor_empty_fix` (empty completion), and `executor_invalid_syntax` (unparseable Go) respectively
- **When** `generateFixes` processes each
- **Then** each still produces its pre-existing `FixWarning` text and `logPipelineWarning` class exactly as before — none is redirected into the new ceiling/decline class, and the new branch does not shadow or intercept them

## Edge Cases
**Edge Case 1: Ceiling-skipped finding never silently reverts to a "clean" run**
- **Given** a run where every finding is skipped via the new ceiling check (no finding qualifies for dispatch)
- **When** the run completes and `findings.json`/report output is generated
- **Then** every skipped finding carries a non-empty `FixWarning`, so the run's output is distinguishable from a genuinely clean run with zero findings needing fixes — a downstream consumer (report renderer, CI gate) cannot misread "all skipped" as "all fine"

**Edge Case 2: Mixed run — some findings skip via existing gates, some via the new ceiling gate, some succeed**
- **Given** a single `generateFixes` invocation over a finding set covering all skip categories plus at least one successful fix
- **When** the run completes
- **Then** each finding's outcome (silent `continue` for pre-existing confidence/severity/attribution skips; `FixWarning`-set for ceiling/decline/failed/truncated/empty/invalid-syntax; `Fix`-populated for success) is independently correct with no cross-finding interference — consistent with the documented per-index-write invariant (no mutex needed because each goroutine touches only its own finding)

**Edge Case 3: Concurrent execution under the worker pool preserves per-finding isolation**
- **Given** `reg.Verify.MaxParallel` findings dispatched concurrently, a mix including ceiling-skips (evaluated on the calling goroutine before dispatch) and in-flight fix attempts (in worker goroutines)
- **When** `wg.Wait()` returns
- **Then** every finding's final state (`Fix`, `Evidence`, `FixWarning`) reflects exactly its own outcome, with the pre-dispatch ceiling filter never racing with or blocking the goroutine dispatch of other, eligible findings

## Error Conditions
**Error Scenario 1: A regression in gate ordering must fail the test suite loudly, not silently**
- If the new ceiling check is accidentally inserted before the confidence/severity/attribution checks, or replaces a `continue` with a warning where one was not previously expected, the existing test suite (asserting silent skip for those cases) must fail — this AC requires that failure mode be exercised and confirmed to trip in a deliberately-broken build (as part of test-implementation validation), not merely assumed.
- Error message / test failure signal: an existing confidence/severity/attribution-skip test case asserting `f.FixWarning == ""` must fail if the new code path incorrectly sets a warning for that finding.

## Performance Requirements
- **Response Time:** Adding the ceiling check and self-decline branch must not measurably change the per-finding dispatch latency or total `generateFixes` wall-clock time for a run with no ceiling configured (the zero-ceiling case must be a true no-op on the hot path).
- **Throughput:** Worker-pool concurrency (`reg.Verify.MaxParallel`, default 4) and semaphore-bounded dispatch behavior are unchanged; the new pre-dispatch check runs on the calling goroutine exactly like the existing confidence/severity/attribution checks, adding no new goroutines or synchronization primitives.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no new external surface.
- **Input Validation:** No change to how `reclib.ConfidenceAtOrAbove`, `meetsSeverityFloor`, or `hasFixAttribution` validate/normalize their inputs; this AC guards against the new code accidentally weakening any of those existing validations (e.g. by short-circuiting the loop before they run).

## Test Implementation Guidance
**Test Type:** INTEGRATION (full `internal/verify` package test run: `go test ./internal/verify/...`) supplemented by targeted UNIT regression cases isolating gate-ordering and per-branch class-string assertions
**Test Data Requirements:** The complete existing `executor_test.go` fixture set (confidence/severity/attribution/fix-failed/truncated/empty-fix/invalid-syntax cases) run as-is, plus new mixed-scenario fixtures combining multiple simultaneous skip conditions on one finding and multiple findings with different outcomes in one `generateFixes` call.
**Mock/Stub Requirements:** Existing scripted `executorCompleter` fakes and fake `Dispatcher`/`fanout.ChatCompleter` already used by the suite; an observable/capturable logger to assert exact class strings per branch across the full matrix.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Full pre-existing `internal/verify/executor_test.go` suite passes with zero edits to existing assertions
- [ ] Skip-chain order (confidence → severity → attribution → ceiling) is verified by a test combining multiple simultaneous failing gates
- [ ] `executor_fix_failed`, `executor_truncated_fix`, `executor_empty_fix`, `executor_invalid_syntax` classes and their `FixWarning` text are unchanged and verified by existing/regression tests
- [ ] A run where every finding is ceiling-skipped or self-declined still yields non-empty `FixWarning` on every such finding (no false "clean" run)

**Manual Review:**
- [ ] Code reviewed and approved
