# Acceptance Criteria: Fix Attribution Prevents Double-Processing Across Tiers

**Related User Story:** [03: Run a Second Tier Over Skipped Findings](../user-stories/03-run-second-tier-over-skipped-findings.md)

## Acceptance Criteria
Fix attribution keyed on the executor's `Name` prevents a second tier from re-attempting a finding tier 1 already fixed (verified for the shared default-`Name` case, and explicitly characterized by test for distinct-`Name` tier configs), while never blocking a tier-2 attempt on a finding tier 1 ceiling-skipped — and attribution survives the `findings.json` write/read round-trip intact.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go integration test / `internal/verify` package | Exercises the existing `hasFixAttribution`/`appendFixAttribution` mechanism across two sequential `generateFixes` passes |
| Test Framework | go test | Follows existing patterns in `internal/verify/executor_test.go` |
| Key Dependencies | `internal/registry.ExecutorConfig.Name` (attribution key), `internal/reconcile.JSONFinding.Evidence` (attribution carrier) | No new dependencies |

### Related Files (from codebase-discovery.json)
- `internal/verify/executor.go` - reference/verify (no functional change expected): `hasFixAttribution` (line ~363) and `appendFixAttribution` (line ~376) key attribution on `ex.Name` via a delimited `"fix by <name>"` token in `Evidence`; the skip check at line ~147 (`hasFixAttribution(f.Evidence, ex.Name)`) runs before ceiling logic
- `internal/verify/executor_test.go` - create/modify: add attribution-across-tiers test cases, including the same-`Name` and distinct-`Name` scenarios below
- `internal/registry/config.go` - reference: `ExecutorConfig.Name` (line ~207) and its default value `RoleExecutor = "executor"` (line 65, applied when `Name` is left unset) — the attribution key both tiers must agree on for cross-tier recognition to work
- `docs/registry.md` - modify: document that tier-1 and tier-2 configs sharing (or deliberately not sharing) `Name` changes cross-tier attribution behavior, so operators configure this deliberately rather than by accident

**Minimum 2 files per AC**

## Happy Path Scenarios
**Scenario 1: Tier 2 does not re-attempt a finding tier 1 already fixed (same executor `Name`)**
- **Given** a finding tier 1 fixed and attributed with `Name = "executor"` (the default, used by both tier configs), leaving `Evidence` containing `"fix by executor"`
- **When** tier 2 (also `Name = "executor"`, different model/ceiling) runs `generateFixes` against the same finding
- **Then** `hasFixAttribution` returns true, the finding is skipped before any executor call is made, and its `Fix`/`Evidence` are left byte-identical to tier 1's output

**Scenario 2: Tier 2 correctly attempts a finding tier 1 ceiling-skipped**
- **Given** a finding tier 1 skipped (its `EstMinutes` exceeded tier 1's ceiling; it carries no fix attribution and no `Fix`)
- **When** tier 2 (higher/no ceiling) runs `generateFixes` against it
- **Then** the finding passes tier 2's severity/confidence/ceiling eligibility checks, is submitted to tier 2's executor, and on success has `Fix` populated and `Evidence` carrying `"fix by <tier-2 Name>"`

## Edge Cases
**Edge Case 1: Tier 1 and tier 2 configured with distinct `Name` values**
- **Given** an operator configures tier 1 with `Name = "cheap-tier"` and tier 2 with `Name = "frontier-tier"` (both non-default, distinct identifiers — a plausible operator choice for readable logs/evidence)
- **When** tier 2 runs against a finding tier 1 already fixed and attributed as `"fix by cheap-tier"`
- **Then** `hasFixAttribution(evidence, "frontier-tier")` returns false — tier 2 is NOT prevented from re-attempting the finding by the name-scoped attribution check alone. This is a real gap in the "fix attribution already exists" assumption from the story's Assumptions section under distinct-name configuration; the test must surface this behavior explicitly (assert the actual current behavior, do not assume it prevents re-processing) so the gap is documented rather than silently relied upon. If Story 2 or this story's implementation adds an additional guard (e.g., skip when `f.Fix != ""` regardless of attribution `Name`), that guard is what this edge case validates instead — write the assertion against whichever mechanism is actually shipped, not against an assumed one.

**Edge Case 2: Finding already fixed, then re-submitted with a corrupted/prefix-colliding `Name`**
- **Given** two executors named `"op"` and `"opus"` (prefix relationship)
- **When** `"op"` checks attribution against evidence containing `"fix by opus"`
- **Then** `hasFixAttribution` does not falsely match on the substring — the delimited `"; "`-token match (already implemented, per the existing doc comment on `hasFixAttribution`) is confirmed by an explicit test case, since a false match here would cause tier 2 to wrongly skip a finding it should fix, i.e. a silent drop

**Edge Case 3: Evidence field empty or missing on the finding tier 2 receives**
- **Given** a finding with `Evidence == ""` (no prior attribution) that also has no ceiling-skip reason
- **When** tier 2 evaluates it
- **Then** `hasFixAttribution` returns false (not a panic on empty string) and the finding passes all of tier 2's gates and is dispatched to tier 2's executor

## Error Conditions
**Error Scenario 1: Attribution check silently short-circuits due to Evidence field being overwritten by an unrelated pipeline stage between tier 1 and tier 2**
- **Given** some other stage of the findings.json round-trip (e.g., re-reconciliation) were to overwrite/blank `Evidence` between the two runs
- **When** tier 2 runs
- **Then** the test must assert that `Evidence` (and thus attribution) survives the `findings.json` write/read round-trip (`internal/reconcile/emit.go`'s `RenderJSON`/`ReadReconciledFindings`) unchanged — a regression here would silently defeat cross-tier double-processing prevention
- Error message: N/A (this is a data-integrity assertion, not a runtime error path)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Attribution checks are pure string operations (`strings.Split`/`TrimSpace`) with no I/O; negligible cost per finding, unaffected by running two tiers instead of one
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — attribution is an internal bookkeeping string, not a trust boundary
- **Input Validation:** `Name` values are already sanitized at config-load time (control characters rejected, length-capped per `internal/verify/executor.go`'s prompt-injection notes); this AC does not add new validation but the test should confirm a `Name` containing the `"; "` delimiter itself cannot be crafted to forge or defeat another executor's attribution token

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Findings pre-seeded with `Evidence` values representing "already fixed by tier 1" (`"fix by executor"` or `"fix by cheap-tier"` depending on the scenario) and "ceiling-skipped by tier 1" (no attribution, `EstMinutes` above tier 1's ceiling).
**Mock/Stub Requirements:** Same stub `executorCompleter`/`Registry` fixtures as AC 03-01; a call-count spy on the completer to assert zero calls for already-attributed findings and exactly one call for eligible ones.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Test proves tier 2 skips a tier-1-fixed finding when both tiers share the same executor `Name` (including the default `"executor"` value) (partition test + `TestGenerateFixes_AttributionSurvivesFindingsJSONRoundTrip`, both using `registry.RoleExecutor` = `"executor"`)
- [x] Test proves tier 2 attempts and fixes a tier-1-ceiling-skipped finding (`tier2.go` in the partition test)
- [x] Test explicitly documents (via an assertion, not a comment) the actual behavior when tier 1 and tier 2 use distinct `Name` values, so the assumption in the story's Assumptions section is verified rather than taken on faith (`TestGenerateFixes_TwoTierDistinctNamesReprocesses`)
- [x] `docs/registry.md` updated to tell operators whether to keep `Name` consistent (or what alternate guard exists) across tier configs for correct cross-tier attribution *(Phase 4 / Story 5 — docs scheduled)* — done: two-tier prose states both tiers must share the executor `name`, that attribution is name-scoped, and that distinct names break the partition (no separate cross-tier lock).

**Manual Review:**
- [ ] Code reviewed and approved
