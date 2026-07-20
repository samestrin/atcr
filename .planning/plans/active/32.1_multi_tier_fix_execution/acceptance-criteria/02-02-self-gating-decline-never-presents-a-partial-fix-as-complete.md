# Acceptance Criteria: Self-Gating Decline Never Presents a Partial Fix as Complete

**Related User Story:** [02: Skip Over-Ceiling Findings Safely](../user-stories/02-skip-over-ceiling-findings-safely.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function / `internal/verify` package | New branch inside `generateFixes`'s per-finding goroutine, parallel to the existing `warn`/`truncated`/empty-fix branches |
| Test Framework | go test | Table-driven + scripted-completer tests in `internal/verify/executor_test.go` |
| Key Dependencies | `internal/llmclient` (`executorCompleter`/`metaCompleter`), `internal/verify` (`invokeExecutor` for Agent Mode), `internal/log` (`logPipelineWarning`) | No new subsystem |

## Related Files
- `internal/verify/executor.go` - modify: add a self-decline detection branch inside the per-finding goroutine of `generateFixes` (around lines 158-228), parallel to (not overloading) the existing `warn`/`truncated`/empty-string handling; on decline, call `logPipelineWarning(log.FromContext(ctx), "executor_ceiling_skip", "<file>:<line>: self-declined: <reason>")` (or an equally distinct, story-approved class/format) and set `f.FixWarning`, returning before any `f.Fix` assignment.
- `internal/verify/executor.go` - modify: `invokeExecutor` (Agent Mode path) and/or `callExecutor`/prompt-building (`buildFixPrompt`) as needed to surface a self-decline signal distinct from an empty/truncated/error response.
- `internal/verify/executor_test.go` - modify: add a scripted-completer/agent-mode test case where the executor's response indicates a decline, asserting the finding lands as a skip, not as `Fix` content.
- `internal/reconcile/emit.go` - reference only: `JSONFinding.FixWarning` (line 136) and `Fix` are the two fields whose mutual-exclusivity this AC protects; no schema change.

## Happy Path Scenarios
**Scenario 1: Executor declines mid-dispatch (Agent Mode tool loop) due to self-assessed complexity**
- **Given** Agent Mode is enabled (`ex.AgentMode`, `cc` and `disp` both non-nil) and the executor's own tool-loop reasoning (via `invokeExecutor`) determines mid-loop that the fix is too complex to complete safely
- **When** `generateFixes` processes that finding
- **Then** the finding is recorded as declined: `f.FixWarning` is set to an explicit, human-readable reason (e.g. `"executor declined: fix exceeds safe complexity for this model"`), `f.Fix` is left unset/unchanged, and `logPipelineWarning` is called with a class distinguishable from `executor_fix_failed` (a decline is deliberate, not a provider/transport error)

**Scenario 2: Executor declines post-dispatch (single-shot snippet path) after generating a response**
- **Given** the non-Agent-Mode snippet path (`callExecutor`) returns a response that the post-processing logic recognizes as a self-decline (e.g. a recognized decline marker/sentinel in the completion, per the story's self-gating design) rather than a usable fix
- **When** `generateFixes` post-processes that response
- **Then** the same skip-and-log contract applies: `f.FixWarning` set, `f.Fix` never populated from the declined content, and the decline is logged non-silently

## Edge Cases
**Edge Case 1: Decline branch runs before any `f.Fix` assignment**
- **Given** a self-decline is detected at any point in the per-finding goroutine
- **When** the branch executes
- **Then** it returns (early-return pattern, matching the existing `warn`/`truncated`/empty-fix branches) strictly before the `f.Fix = fix` / `f.Evidence = appendFixAttribution(...)` lines, so a finding can never end up with both a stale `Fix` and a decline warning (the risk explicitly flagged in the story's Potential Risks table)

**Edge Case 2: Decline vs. truncation vs. empty completion are mutually distinguishable**
- **Given** three separate findings — one truncated (`finish_reason=length`), one with an empty completion, and one self-declined
- **When** all three are processed by `generateFixes`
- **Then** each produces a distinct `FixWarning` reason string and a distinct/correctly-attributed `logPipelineWarning` class (`executor_truncated_fix`, `executor_empty_fix`, and the new decline class respectively) — no cross-contamination of reason text or class

**Edge Case 3: Decline does not overload `executor_fix_failed`**
- **Given** a self-decline occurs
- **When** the warning is logged
- **Then** the class is NOT `executor_fix_failed` (that class must remain reserved for provider/transport errors per the story's Implementation Notes) — verified by an explicit assertion on the logged class string

## Error Conditions
**Error Scenario 1: Decline reason must never be empty**
- Error message: `f.FixWarning` for a self-decline must be a non-empty, human-readable string; an implementation that declines with an empty reason fails this AC (indistinguishable from a silent drop, the exact failure mode the story forbids)
**Error Scenario 2: A malformed/ambiguous "maybe-decline" response defaults to safe handling**
- Given a response that is ambiguous (neither a clean fix, nor a recognized decline marker, nor empty/truncated)
- Then it must fall through to existing handling (e.g. treated as a candidate fix subject to the existing syntax guard, or as `executor_fix_failed`/empty per existing rules) rather than being silently swallowed by an overly broad decline detector — decline detection must be conservative, not a catch-all

## Performance Requirements
- **Response Time:** Decline detection is a post-processing check on an already-returned completion (or an in-loop signal in Agent Mode); it must not add a second round-trip or extra provider call solely to confirm a decline.
- **Throughput:** No change to worker-pool concurrency (`reg.Verify.MaxParallel`) or semaphore behavior — the decline branch is purely a classification of an already-obtained result.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no new external call or credential surface is introduced.
- **Input Validation:** Decline-marker detection (if implemented via a sentinel/prefix in the completion text) must not be spoofable by attacker-controlled code/finding content in a way that suppresses a real fix or, conversely, forces a decline for a legitimate fix; the detector should operate on the executor's own structured/documented decline signal, not on arbitrary substring matching against untrusted finding text.

## Test Implementation Guidance
**Test Type:** UNIT (scripted `executorCompleter`/`metaCompleter` and a fake Agent Mode tool-loop driver in `internal/verify/executor_test.go`)
**Test Data Requirements:** A scripted completion representing a clean fix, one representing an empty completion, one representing truncation, and one representing a self-decline (distinct sentinel/shape); for Agent Mode, a fake `fanoutRunner`/`ChatCompleter` sequence that yields a decline signal mid-loop.
**Mock/Stub Requirements:** `executorCompleter`/`metaCompleter` fakes (already the seam per `newExecutorClient`/`swapExecutorClient`); a fake `Dispatcher`/`fanout.ChatCompleter` for the Agent Mode path; an observable logger to assert the decline's class string and non-`executor_fix_failed` distinction.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A self-declined fix (both snippet-path and Agent-Mode-path where applicable) sets `f.FixWarning` to a non-empty, human-readable reason and never sets `f.Fix`
- [ ] The decline's logged class is distinct from `executor_fix_failed`, `executor_truncated_fix`, and `executor_empty_fix`
- [ ] The decline branch returns before any `f.Fix`/`f.Evidence` mutation (no stale-Fix-plus-warning state possible)
- [ ] An ambiguous, non-decline response is never misclassified as a decline (conservative-detector test case passes)

**Manual Review:**
- [ ] Code reviewed and approved
