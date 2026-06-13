# Acceptance Criteria: Mixed Roster Reconciler Compatibility

**Related User Story:** [04: Graceful Degradation](../user-stories/04-graceful-degradation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Reconciler | `internal/reconcile` package | Consumes `raw/<agent>/*.json` uniformly; no tool-vs-single-shot branching |
| Status Schema | `AgentStatus` in `status.go` | `tools_degraded` is metadata only; does not affect reconcile input |
| WritePool | `internal/fanout/artifacts.go` | Writes per-agent results identically regardless of execution mode |
| Integration Test | `go test` + `httptest` | End-to-end mixed-roster review with reconcile |


### Related Files (from codebase-discovery.json)
- `internal/fanout/status.go:225` - modify: `AgentStatus` schema with `tools_degraded` field; backward-compatible omitempty
- `internal/reconcile/reconcile.go` - verify: no changes needed; consumes results uniformly (regression test added)
- `internal/fanout/review_test.go` - create: integration test with mixed roster (tool-loop + degraded agents)
- `internal/fanout/artifacts.go:160` - reference: `writeAgentArtifacts` writes per-agent results identically regardless of execution mode

## Spec Alignment Notes

- **Reconciler requires NO changes** — the existing reconcile path already reads `raw/<agent>/*.json` uniformly. A degraded agent's output file has the same shape as a single-shot agent's output. A tool-loop agent's output also has the same shape (the final response is serialized identically).
- **`status.json` carries the signal, not the reconcile input** — `tools_degraded` is operator-facing metadata in the per-agent status. The reconciler's input (findings JSON) is identical regardless of execution mode.
- **Mixed roster is the normative case** — Epic 2.0 expects operators to run heterogeneous rosters. The reconciler must produce identical output whether all agents ran as tool-loops, all degraded, or any mix.

## Happy Path Scenarios

**Scenario 1: Mixed roster review completes and reconciler consumes both result shapes**
- **Given** a review roster with at least one tool-loop agent and at least one degraded single-shot agent
- **When** the fan-out completes and the reconciler runs
- **Then** the reconciler processes both agents' results identically
- **And** no reconcile-path errors are attributable to tool-vs-single-shot heterogeneity
- **And** the reconcile output (verdict, findings) is correct

**Scenario 2: `status.json` carries per-agent degrade signals**
- **Given** a mixed roster review has completed
- **When** the operator reads `status.json` for each agent
- **Then** tool-loop agents have `tools_degraded: false` (or absent)
- **And** degraded agents have `tools_degraded: true`
- **And** the `tools_requested` field preserves the original request for each agent

**Scenario 3: Per-agent status records degradation for operator visibility**
- **Given** a mixed roster review with 2 degraded agents out of 5 total
- **When** `status.json` is written for each agent
- **Then** the 2 degraded agents have `tools_degraded: true`
- **And** the remaining 3 agents have `tools_degraded: false` (or the field is absent)
- **And** the operator can see degradation by inspecting per-agent status

**Scenario 4: Reconcile output identical for tool-loop vs single-shot with same input**
- **Given** two reviews: one where agent X ran as a tool-loop, one where agent X ran degraded single-shot
- **And** both agents received the same logical input (same prompt, same payload)
- **When** the reconciler processes each review
- **Then** the reconcile input shape (findings JSON) is identical for agent X in both reviews
- **And** any difference in reconcile output is due to content differences, not shape differences

## Edge Cases

**Edge Case 1: All agents in the roster degrade (no tool-loop agents at all)**
- **Given** a roster of 5 agents, all non-tool-capable, all with `tools: true`
- **When** the review completes
- **Then** all agents have `tools_degraded: true`
- **And** `degraded_count` equals the roster size (5)
- **And** the reconciler still processes all results successfully
- **And** the review completes without reconcile errors

**Edge Case 2: All agents run as tool-loops (no degradation)**
- **Given** a roster of 5 agents, all tool-capable, all with `tools: true`
- **When** the review completes
- **Then** all agents have `tools_degraded: false`
- **And** `degraded_count` is 0
- **And** the reconciler processes all results successfully

**Edge Case 3: 1.x review (no tool code path) has no `tools_degraded` field**
- **Given** a review produced by the 1.x code path (no Epic 2.0 changes)
- **When** the per-agent `status.json` is read
- **Then** `tools_degraded` is absent (omitempty; field was not evaluated)
- **And** `tools_requested` is absent
- **And** `degraded_count` in summary.json is 0
- **And** the reconciler processes the review without errors (backward compat)

**Edge Case 4: No agents degrade**
- **Given** a review where every agent ran as configured (no degrade events)
- **When** `status.json` is written for each agent
- **Then** no agent has `tools_degraded: true`
- **And** 1.x agents do not emit the `tools_degraded` field (omitempty)

## Error Conditions

**Error Scenario 1: Reconciler panics on mixed roster**
- **Detection:** Integration test with mixed roster; zero panics across all test runs
- **Mitigation:** The reconciler's input contract is `raw/<agent>/*.json` which is shape-identical regardless of execution mode. No branching on `tools_degraded`.
- **Regression:** A CI-pinned integration test exercises mixed rosters on every commit.



## Performance Requirements
- **Reconcile Latency:** No measurable change; the reconciler does not read `tools_degraded` and processes the same input shape
- **Status Read:** Reading `tools_degraded` from status.json adds < 100 bytes per agent; negligible for operator-facing tooling

## Security Considerations
- **No Reconciler Changes:** The reconciler does not branch on `tools_degraded`; there is no new attack surface in the reconcile path

- **Backward Compatible:** Old status.json files (without `tools_degraded`) parse without error in new code; the field's absence means "not evaluated" (1.x compat)

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:**
- Mixed roster: 2+ tool-capable agents, 2+ non-tool-capable agents, all with `tools: true`
- Fake completers: some returning tool-loop sequences, some returning single-shot responses
- Expected reconcile output for comparison
- 1.x-compatible status.json fixtures (no `tools_degraded` field)

**Mock/Stub Requirements:**
- Fake `Completer` per agent (tool-loop or single-shot behavior)
- `httptest.Server` for end-to-end review execution
- Reconcile input/output fixtures

**Test Cases:**
1. `TestMixedRoster_ReconcileSucceeds` — end-to-end mixed roster review + reconcile
2. `TestMixedRoster_StatusSignals` — per-agent `tools_degraded` correct for each agent
3. `TestMixedRoster_DegradedSignals` — per-agent `tools_degraded` correct for each agent
4. `TestMixedRoster_ReconcileIdempotent` — reconcile output same for tool-loop and degraded agents with same input
5. `TestBackwardCompat_NoDegradedField` — 1.x status.json without `tools_degraded` parses without error
6. `TestAllDegraded_ReconcileSucceeds` — every agent degrades; reconcile still works
7. `TestNoDegradation_DegradedCountZero` — no degrade events; `degraded_count: 0`

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/fanout/... ./internal/reconcile/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)
- [x] Mixed-roster integration test passes in CI (TestMixedRoster_BothShapesReconcileIdentically)

**Story-Specific:**
- [x] Mixed roster review completes end-to-end with no reconcile errors
- [x] Reconciler produces correct output for mixed rosters without code changes (no reconcile code touched)
- [x] 1.x status.json (no `tools_degraded` field) remains backward-compatible (TestInvokeAgent_SingleShotStatusOmitsToolFields)
- [x] Regression test for mixed roster is pinned in CI

**Manual Review:**
- [x] Code reviewed and approved (4.2.A adversarial subagent)
- [x] Reconciler confirmed to have no tool-vs-single-shot branching (statusFor gates tool fields behind r.Tools; reconcile unchanged)

