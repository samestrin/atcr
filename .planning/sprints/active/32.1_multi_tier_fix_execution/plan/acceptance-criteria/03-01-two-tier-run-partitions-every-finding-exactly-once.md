# Acceptance Criteria: Two-Tier Run Partitions Every Finding Exactly Once

**Related User Story:** [03: Run a Second Tier Over Skipped Findings](../user-stories/03-run-second-tier-over-skipped-findings.md)

## Acceptance Criteria
Running `generateFixes` twice — a tier-1 low-ceiling pass followed by a tier-2 high/no-ceiling pass over the same finding set — leaves every finding in exactly one terminal state: fixed-by-tier-1, fixed-by-tier-2, or explicitly skip-logged by both. No finding is double-processed, and no finding is silently dropped (empty `Fix` with no warning).

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go integration test / `internal/verify` package | Exercises `generateFixes` twice against the same finding set at the outcome level; does not assume an in-process ordered-executor chain |
| Test Framework | go test (table-driven + fixture-driven integration test) | Follows existing patterns in `internal/verify/executor_test.go` |
| Key Dependencies | `internal/registry` (`ExecutorConfig`, ceiling fields from Story 1), `internal/reconcile` (`JSONFinding`, `ReadReconciledFindings`/`RenderJSON`) | No new packages required |

### Related Files (from codebase-discovery.json)
- `internal/verify/executor.go` - modify (Story 2 dependency): `generateFixes` is the function under test; this AC does not change its skip logic, only exercises it twice in sequence
- `internal/verify/executor_test.go` - create/modify: add the two-tier partition integration test (fixture findings, tier-1 pass, tier-2 pass, partition assertions)
- `internal/registry/config.go` - modify (Story 1 dependency): `ExecutorConfig` ceiling fields (`max_estimated_minutes`, optional `max_severity_for_fix`) configured differently for the tier-1 and tier-2 fixtures in this test
- `internal/reconcile/emit.go` - reference only: `JSONFinding` schema (`EstMinutes`, `Severity`, `Evidence`, `Fix`, `FixWarning`) used to build the fixture and to assert the final partitioned state

**Minimum 2 files per AC**

## Happy Path Scenarios
**Scenario 1: Tier 1 fixes below-ceiling findings, tier 2 fixes the rest**
- **Given** a fixture finding set containing findings with `EstMinutes` values spanning below tier 1's ceiling, above tier 1's ceiling but below tier 2's ceiling, and above both ceilings (or no ceiling on tier 2)
- **When** `generateFixes` is run once with the tier-1 (low-ceiling) `ExecutorConfig` against the full set, and then run a second time with the tier-2 (high/no-ceiling) `ExecutorConfig` against the resulting (tier-1-processed) set
- **Then** every finding below tier 1's ceiling has `Fix` populated and carries tier 1's fix attribution after the first pass, every remaining finding (skipped by tier 1) has `Fix` populated and carries tier 2's fix attribution after the second pass, and no finding is left with both an empty `Fix` and no skip/warning record

**Scenario 2: Tier 2 run over an empty remainder is a no-op**
- **Given** a fixture where every finding is within tier 1's ceiling (tier 1 fixes 100% of findings)
- **When** tier 2 is run against the tier-1-processed set
- **Then** tier 2 makes zero executor calls (nothing eligible remains) and the finding set is unchanged by the second pass

## Edge Cases
**Edge Case 1: Finding above both tiers' ceilings**
- **Given** a finding whose `EstMinutes`/severity exceeds both tier 1's and tier 2's ceilings
- **When** both tiers run in sequence
- **Then** the finding is left unfixed by both tiers but is explicitly logged as skipped (e.g., via the existing ceiling-skip logging path from Story 2) rather than silently absent from the output — its `Fix` field stays empty and no fabricated attribution is added

**Edge Case 2: Finding at exactly the ceiling boundary**
- **Given** a finding whose `EstMinutes` equals tier 1's ceiling exactly
- **When** tier 1 runs
- **Then** the finding's boundary-inclusive/exclusive behavior matches Story 2's documented ceiling semantics consistently between tier 1 and tier 2 (no off-by-one gap or overlap at the shared boundary between "cheap" and "frontier" ranges)

**Edge Case 3: Zero findings in the input set**
- **Given** an empty `[]JSONFinding` slice
- **When** both tiers run against it
- **Then** both passes complete without error and the output remains an empty set (trivially satisfies "every finding partitioned")

## Error Conditions
**Error Scenario 1: Tier 2 config references an undefined provider**
- **Given** a tier-2 `ExecutorConfig` whose `Provider` is not present in `Registry.Providers`
- **When** tier 2's `generateFixes` runs
- **Then** the run logs `executor_unknown_provider` and returns without processing any finding — this must not be misreported as "all findings partitioned"; the test asserts the pipeline warning fires and the finding set is left exactly as tier 1 produced it
- Error message: `"executor_unknown_provider"` (pipeline warning code, per `internal/verify/executor.go:114`)
- HTTP status / error code: N/A (internal warning, not an HTTP path)

**Error Scenario 2: Tier 2 executor call fails mid-run for one finding**
- **Given** a fixture where the mock completer returns an error for one eligible finding
- **When** tier 2 runs
- **Then** that finding's `FixWarning` is set to a non-empty failure reason and it is not silently dropped from the output set — the partition assertion (every finding fixed-or-skip-logged) still accounts for it as "attempted and logged failed" rather than counting it as a clean skip

## Performance Requirements
- **Response Time:** The two-tier integration test uses a mock/fake `executorCompleter` (no live network calls) and completes in well under the existing `internal/verify` package's test suite time budget (sub-second per test case)
- **Throughput:** N/A — test exercises correctness of partitioning, not throughput; existing `MaxParallel` bounded worker pool behavior (`internal/verify/executor.go:122`) is unaffected by running twice

## Security Considerations
- **Authentication/Authorization:** N/A — no new credential or auth surface; tier 1 and tier 2 use the existing per-provider `APIKeyEnv` resolution unchanged
- **Input Validation:** The fixture findings set is test-authored, but the test should include at least one finding with adversarial/edge `EstMinutes` (e.g., 0, negative-if-representable, very large) to confirm ceiling comparison logic does not panic or silently mis-partition on unusual values

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** A fixture `[]reconcile.JSONFinding` (in-memory, not necessarily a file) with at least 4 findings spanning: below-tier-1-ceiling, above-tier-1-but-below-tier-2-ceiling, above-both-ceilings, and a boundary-exact case. Two `registry.ExecutorConfig` fixtures (tier-1 low ceiling, tier-2 high/no ceiling) pointing at a shared fake provider.
**Mock/Stub Requirements:** A stub `executorCompleter` (already used in `internal/verify/executor_test.go`) that returns a deterministic, syntactically valid fix string per call so `validateGoFixSyntax` does not interfere with partition assertions; a stub `Registry` with one provider satisfying both `ExecutorConfig`s.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Integration test proves every finding in the fixture ends in exactly one of: fixed-by-tier-1, fixed-by-tier-2, or explicitly skip-logged-by-both — never two of these states at once, never none (`TestGenerateFixes_TwoTierPartitionsFindingsExactlyOnce`; never-both/never-neither/XOR asserted separately)
- [x] Test explicitly asserts zero double-processing (no finding is re-submitted to a completer after it already carries a `Fix`+attribution from the other tier's pass, scoped per Story 2's skip semantics) (`rec2.calls==1`; tier-1-fixed files asserted absent from tier-2 prompts)
- [x] Test covers the above-both-ceilings edge case and confirms it is logged, not silently dropped (`both.go`, EstMinutes 100000)
- [x] Test covers the error path (unknown provider / failed completer call) without misclassifying it as a clean partition (`TestGenerateFixes_TwoTierUnknownProviderLeavesTier1State`)

**Manual Review:**
- [x] Code reviewed and approved
