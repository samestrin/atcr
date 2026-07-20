# Acceptance Criteria: Two-Tier Workflow Is Test-Verified and Reproducible from Documentation Alone

**Related User Story:** [03: Run a Second Tier Over Skipped Findings](../user-stories/03-run-second-tier-over-skipped-findings.md)

## Acceptance Criteria
The two-tier partition contract is proven by an automated integration test over a mixed-complexity fixture (fixed-by-tier-1 XOR fixed-by-tier-2 XOR skip-logged-by-both — never both, never neither), and the same workflow is reproducible by an operator from `docs/registry.md` plus the example configs alone, without reading executor source code.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go integration test (`internal/verify`) plus documentation (`docs/registry.md`, `examples/registry-with-executor.yaml`) | Combines an automated proof with an operator-facing worked example; both are required, neither substitutes for the other |
| Test Framework | go test | Fixture-driven, mixed-complexity finding set |
| Key Dependencies | `internal/registry`, `internal/reconcile`, existing YAML example/config loader | No new dependencies |

### Related Files (from codebase-discovery.json)
- `internal/verify/executor_test.go` - create/modify: the automated integration test proving the two-tier partition using a fixture findings set with mixed complexity (below-ceiling, above-ceiling, boundary)
- `examples/registry-with-executor.yaml` - modify: add a second example profile (or a clearly-labeled sibling block) showing a "cheap-tier" `ExecutorConfig` (low `max_estimated_minutes`) and a "frontier-tier" `ExecutorConfig` (high/no ceiling) intended to run back-to-back against the same `findings.json`
- `docs/registry.md` - modify: add a worked-example section walking through running atcr twice (tier 1 then tier 2) against the same `findings.json`, including expected outcome (what gets fixed by which tier, how to verify nothing was double-processed or dropped)
- `internal/reconcile/emit.go` - reference only: `ReadReconciledFindings`/`RenderJSON` — the documented workflow relies on `findings.json` being the shared, re-readable handoff artifact between the two runs

**Minimum 2 files per AC**

## Happy Path Scenarios
**Scenario 1: Automated test proves the partition using a fixture with mixed complexity**
- **Given** a fixture `findings.json`-equivalent set (in-memory `[]reconcile.JSONFinding` or an actual test-data file) containing LOW, MEDIUM, and HIGH `EstMinutes` findings
- **When** the integration test runs tier 1 then tier 2 against it and inspects the resulting finding set
- **Then** the test asserts, in one place, the full outcome-level contract: every finding is fixed-by-tier-1 XOR fixed-by-tier-2 XOR explicitly skip-logged-by-both, with zero findings in more than one of these buckets and zero findings in none of them

**Scenario 2: An operator reproduces the workflow from documentation alone**
- **Given** `docs/registry.md`'s worked example and the two example profiles in `examples/registry-with-executor.yaml`, and no access to (or need to read) `internal/verify/executor.go` source
- **When** an operator follows the documented steps: run atcr with the cheap-tier config against a findings set, then run atcr again with the frontier-tier config pointed at the same `findings.json`
- **Then** the operator ends up with a `findings.json` where cheap findings are fixed by tier 1 and the remainder is fixed by tier 2, matching the documented expected outcome — verified by including a concrete before/after `findings.json` excerpt (or equivalent) in the documentation so the expected state is checkable, not just described in prose

## Edge Cases
**Edge Case 1: Documentation example config is not itself valid against the registry schema**
- **Given** the two example profiles added to `examples/registry-with-executor.yaml`
- **When** the existing registry config validation/loading path parses them
- **Then** both profiles load without validation errors — the example is not just illustrative prose but an actually-loadable config, so documentation drift (e.g., a renamed field) is caught by existing config tests rather than only discovered by a confused operator later

**Edge Case 2: Worked example omits the open design-question caveat**
- **Given** the story's Assumptions section flags an open, unresolved design question (two independent runs vs. an in-process ordered chain)
- **When** the worked example is written
- **Then** the documentation explicitly states it demonstrates the "two independent runs" interpretation currently implemented, without asserting this is the only valid interpretation of "multi-tier" — so a future reader is not misled into thinking the design question was resolved by this story

## Error Conditions
**Error Scenario 1: Fixture test data becomes stale relative to `JSONFinding` schema changes**
- **Given** a future schema change to `internal/reconcile.JSONFinding` (new required field, renamed field)
- **When** the two-tier integration test's fixture is built via struct literals (not raw JSON strings)
- **Then** the test fails to compile rather than silently passing against a stale shape — this is a design constraint on how the test fixture must be authored, not a runtime error message
- Error message: Go compiler error on the fixture struct literal (no runtime error string applicable)
- HTTP status / error code: N/A

**Error Scenario 2: Documentation worked example and the automated test diverge**
- **Given** the worked example in `docs/registry.md` and the fixture in `internal/verify/executor_test.go` are maintained independently
- **When** either changes without the other being updated
- **Then** this is flagged as a review-time risk in the Definition of Done (manual check) rather than assumed to be automatically caught — there is no automated doc-vs-test consistency check in scope for this story

## Performance Requirements
- **Response Time:** The integration test completes within the existing `internal/verify` package test-run budget (no live network calls, mock completer only)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — documentation and test fixtures carry no live credentials; example YAML uses placeholder `APIKeyEnv` names consistent with existing examples in `examples/registry-with-executor.yaml`
- **Input Validation:** The example config additions must pass the same registry validation (`internal/registry/config.go`) as any hand-authored operator config — no relaxed/example-only validation path

## Test Implementation Guidance
**Test Type:** E2E (documentation-reproducibility check) backed by an INTEGRATION test (partition proof)
**Test Data Requirements:** A fixture findings set with at least one LOW, one MEDIUM, and one HIGH complexity finding (by `EstMinutes` and/or severity) to exercise the full mixed-complexity partition matrix; the same shape should inform (not necessarily be byte-identical to) the worked example in the docs so the two stay conceptually aligned
**Mock/Stub Requirements:** Same stub `executorCompleter`/`Registry` fixtures as AC 03-01/03-02; no live provider calls in the automated test — the "reproducible from documentation" half of this AC is verified by review/read-through of the docs, not by automating an operator's manual CLI steps

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Integration test with a mixed-complexity fixture proves the full partition contract (fixed-by-tier-1 XOR fixed-by-tier-2 XOR skip-logged, never both/neither)
- [ ] `examples/registry-with-executor.yaml` contains a loadable cheap-tier + frontier-tier profile pair validated by existing config-loading tests
- [ ] `docs/registry.md` contains a worked example with concrete expected `findings.json` state (not prose-only) and explicitly labels which design interpretation ((a) two independent runs) it demonstrates
- [ ] Documentation explicitly notes the open design question is not resolved by this story, per the story's Assumptions section

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] A reviewer who has not read `internal/verify/executor.go` confirms they can follow `docs/registry.md` alone and correctly predict the two-tier outcome
