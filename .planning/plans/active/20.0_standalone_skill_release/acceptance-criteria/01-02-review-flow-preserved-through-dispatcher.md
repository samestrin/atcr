# Acceptance Criteria: Review Orchestration Flow Preserved Through the Dispatcher

**Related User Story:** [01: Dispatcher Skill Rewrite](../user-stories/01-dispatcher-skill-rewrite.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown Agent Skill body (Orchestration Steps section) | `skill/SKILL.md` |
| Test Framework | Go `testing` + `testify` | `skill/skill_test.go` (`TestSkill_OrchestrationSequence` already exists and must keep passing) |
| Key Dependencies | `atcr` CLI (`range`, `review`, `status`, `reconcile`, `report`) | ordering contract unchanged from current SKILL.md lines 33-51 |

## Related Files
- `skill/SKILL.md` - modify: the existing "Orchestration Steps" section (range → review → status → host findings → reconcile → report) must remain reachable as the routed behavior for the `review` command path, unchanged in substance
- `skill/skill_test.go` - reference/modify: `TestSkill_OrchestrationSequence` (lines 47-60) asserts the ordered substrings `` `atcr range ``, `` `atcr review ``, `` `atcr status ``, `sources/host/findings.txt`, `` `atcr reconcile ``, `` `atcr report `` appear in that order — this must continue to pass after the rewrite
- `docs/skill-usage.md` - reference only: describes the same 6-step flow (pre-flight range, start+poll, host review, reconcile, optional adjudicate, render+present) that must stay consistent with the dispatcher's routed review flow

## Happy Path Scenarios
**Scenario 1: `/atcr review` still runs the full orchestration**
- **Given** an agent invokes the dispatcher with a review-shaped request (e.g. "review branch feature-x" or `/atcr review`)
- **When** the dispatcher routes to the review command path
- **Then** the agent performs, in order: pre-flight range (`atcr range`) → start background review (`atcr review`) → poll (`atcr status <id>`) → host review write (`sources/host/findings.txt`) → reconcile (`atcr reconcile <id>`) → render/present (`atcr report <id>`) — identical in sequence and content to the pre-rewrite SKILL.md

**Scenario 2: Polling and timeout parameters unchanged**
- **Given** the routed review flow reaches the status-polling step
- **When** the agent polls
- **Then** it polls every 10 seconds up to 60 times (10-minute default timeout), both configurable, exactly as specified pre-rewrite

**Scenario 3: Non-review commands bypass the review orchestration entirely**
- **Given** an agent invokes the dispatcher for a non-review command (e.g. `/atcr doctor`, `/atcr status <id>` standalone, `/atcr trust`)
- **When** the dispatcher routes
- **Then** it issues only the single corresponding `atcr <command>` invocation and does not run the range/review/host/reconcile/report sequence

## Edge Cases
**Edge Case 1: Partial pool failure**
- **Given** some pool agents error but at least one succeeds during the routed review flow
- **When** reconciliation proceeds
- **Then** the dispatcher still reconciles and notes `partial: true`, matching current SKILL.md behavior (lines 51)

**Edge Case 2: Stale or missing `.atcr/latest` pointer**
- **Given** `.atcr/latest` is missing or stale after the routed review flow starts
- **When** the agent calls `reconcile`/`report`/`status`
- **Then** it passes the explicit review id captured from `atcr review` output rather than relying on the pointer

**Edge Case 3: Review command invoked with an explicit range vs. no input**
- **Given** the user supplies a git range, branch name, PR URL, or nothing
- **Then** the dispatcher's review path resolves the range exactly as the four input forms in the current "Input Format" section describe (unchanged)

## Error Conditions
**Error Scenario 1: Empty range**
- Error message: `Range is empty: no changes between <base> and <head>. Nothing to review.`
- Trigger: `atcr range` reports an empty range during the routed pre-flight step

**Error Scenario 2: Review timeout**
- Error message: `Review timed out after <N> seconds. Check 'atcr status' for details.`
- Trigger: polling exceeds the configured timeout without reaching `completed`/`failed`

**Error Scenario 3: No reconcile sources**
- Error message: `no reconcile sources found under sources/`
- Trigger: `atcr reconcile <id>` finds zero sources under `sources/`

## Performance Requirements
- **Response Time:** Polling interval fixed at 10s, max 60 polls (10-minute ceiling), both configurable — unchanged from current behavior; the dispatcher rewrite must not alter these defaults.
- **Throughput:** N/A — single review flow at a time per invocation, consistent with the current single-threaded orchestration.

## Security Considerations
- **Authentication/Authorization:** Unchanged — `gh` CLI must be authenticated for PR-reference input; skill halts with a clear message if missing rather than crashing.
- **Input Validation:** Review id must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` and all host-review writes must stay within `.atcr/reviews/<id>/` — this existing constraint (current SKILL.md line 59) must be preserved when the Host Review content is relocated per AC 01-03.

## Test Implementation Guidance
**Test Type:** UNIT (string-ordering assertions over embedded `SkillMD`) + manual trace
**Test Data Requirements:** None beyond the embedded constant; `TestSkill_OrchestrationSequence` already encodes the required ordered substrings.
**Mock/Stub Requirements:** None for the Markdown-level test. Manual verification requires tracing at least one full review-flow invocation end-to-end against a real or sample `.atcr/reviews/<id>/` directory structure (per the story's risk-mitigation note).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`, including `TestSkill_OrchestrationSequence`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Routed review path preserves the exact range → review → status → host → reconcile → report sequence
- [ ] Polling interval/timeout values (10s / 60 polls / 10-minute default) are unchanged
- [ ] All three error messages (empty range, timeout, no reconcile sources) remain verbatim
- [ ] Non-review commands do not trigger the review orchestration sequence

**Manual Review:**
- [ ] Code reviewed and approved
