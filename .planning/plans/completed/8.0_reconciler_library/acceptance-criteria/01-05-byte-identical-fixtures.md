# Acceptance Criteria: Byte-Identical Fixtures Against Pre-Extraction Baseline

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go output artifacts (deterministic fixtures) | `findings.json`, `ambiguous.json`, `disagreements.json` sidecars |
| Test Framework | go test + testify + byte-diff against baseline | Diff-based verification, not new RED tests |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` | `sortMerged` total-order + `Verification` pointer identity must be preserved |

### Related Files (from codebase-discovery.json)
- `internal/reconcile/merge.go` - modify: moves to `reconcile/merge.go`; `Merged`, `mergeVerification` (`internal/reconcile/merge.go:418`), `verdictRank`, finding-merge rules; `SeverityRank` copy (`internal/reconcile/merge.go:30`) collapses to library canonical `NormalizeSeverity`/`SeverityRank`
- `reconcile/merge.go` - create: moved merge logic; `sortMerged` total-order (severity desc, then file, then line) preserved exactly
- `internal/reconcile/dedupe.go` - modify: moves to `reconcile/dedupe.go`; token-set Jaccard dedupe, `AmbiguousCluster`, integer-cross-multiply 0.7/0.4 thresholds (`internal/reconcile/dedupe.go:53`)
- `internal/reconcile/disagree.go` - modify: moves to `reconcile/disagree.go`; `BuildDisagreements` radar projection (`internal/reconcile/disagree.go:102`)
- `internal/reconcile/cluster.go` - modify: moves to `reconcile/cluster.go`; deterministic `(FILE, LINE±3)` location clustering (`internal/reconcile/cluster.go:29`)
- `internal/reconcile/ambiguous.go` - modify: moves to `reconcile/ambiguous.go`; `AmbiguousID`/`AmbiguousHash` sidecar (`internal/reconcile/ambiguous.go:60`)
- `internal/reconcile/confidence.go` - modify: moves to `reconcile/confidence.go`; `ConfidenceForVerdict`/`ConfidenceAtOrAbove` (`internal/reconcile/confidence.go:22`)
- `internal/reconcile/attribution.go` - modify: moves to `reconcile/attribution.go`; `FixAttribution`/`EvidenceSep` constants (`internal/reconcile/attribution.go:10`)
- `docs/findings-format.md` - reference: wire format spec (`atcr-findings/v1`); no schema change permitted
- `internal/reconcile/cluster_merge_test.go` - reference: `MergeJSONFindings_VerificationPrecedence` validates merge ordering
- `internal/reconcile/disagree_test.go` - reference: `BuildDisagreements` validates disagreement output

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — confidence v2 ordering (`VERIFIED` > `HIGH` > `MEDIUM` > `LOW`) and gate semantics preserved in byte-identical output.

## Happy Path Scenarios
**Scenario 1: findings.json byte-identical to pre-extraction baseline**
- **Given** a pre-extraction baseline `findings.json` is captured from the current `internal/reconcile` implementation
- **When** the reconciler is extracted into the library and ATCR runs the same input corpus through the adapter
- **Then** the regenerated `findings.json` is byte-for-byte identical (`diff` exit 0) to the baseline, including field order, severity ranking, and `Verification` stamping

**Scenario 2: ambiguous.json sidecar byte-identical**
- **Given** the baseline `ambiguous.json` sidecar (produced by `AmbiguousID`/`AmbiguousHash`)
- **When** the extraction lands and the same corpus runs
- **Then** the regenerated `ambiguous.json` is byte-identical (`diff` exit 0) to the baseline

**Scenario 3: disagreements.json sidecar byte-identical**
- **Given** the baseline `disagreements.json` sidecar (produced by `BuildDisagreements` radar projection)
- **When** the extraction lands and the same corpus runs
- **Then** the regenerated `disagreements.json` is byte-identical (`diff` exit 0) to the baseline

**Scenario 4: sortMerged total-order preserved**
- **Given** `sortMerged` orders merged findings by severity desc, then file, then line
- **When** the merge logic moves to `reconcile/merge.go`
- **Then** the same input yields the same ordered output, so the JSON array order is byte-identical (deterministic output is the no-regression guarantee)

## Edge Cases
**Edge Case 1: SeverityRank copy collapses to canonical library owner**
- **Given** `merge.go:30` has a package-local `SeverityRank` copy and `internal/stream/severity.go:33` has the canonical `NormalizeSeverity`/`SeverityRank`
- **When** the copy collapses to the library's canonical `NormalizeSeverity`/`SeverityRank`
- **Then** the severity ranking values are identical (the copy was already a mirror), so fixture output is unchanged

**Edge Case 2: Verification pointer-identity affects output**
- **Given** `Merged.Verification` is a `*Verification` shared with `gate.go` and `internal/debate`
- **When** `Verification` becomes library API and the adapter preserves pointer identity
- **Then** mutations by `internal/debate/emit.go:107` (`applyRulings`) are visible in the serialized `findings.json` (the shared pointer is serialized after all mutations)

**Edge Case 3: Dedupe thresholds (0.7/0.4) unchanged**
- **Given** `dedupe.go` uses integer-cross-multiply thresholds 0.7 and 0.4 to avoid float drift
- **When** the dedupe logic moves to the library
- **Then** the same integer-cross-multiply arithmetic is used (no float conversion), so cluster membership is byte-identical

## Error Conditions
**Error Scenario 1: Fixture diff detected**
- Error message: `diff baseline/findings.json actual/findings.json` produces output (non-empty diff)
- HTTP status / error code: CI fixture-diff check exit code 1

**Error Scenario 2: sortMerged order changed**
- Error message: `TestMergeJSONFindings_VerificationPrecedence` fails: merged finding order differs from baseline
- HTTP status / error code: go test exit code 1

**Error Scenario 3: Wire format schema drift**
- Error message: `docs/findings-format.md` spec no longer matches the serialized output (a field was added/removed/renamed)
- HTTP status / error code: schema-diff check exit code 1 (the `atcr-findings/v1` wire format must not change)

## Performance Requirements
- **Response Time:** Fixture generation latency must not regress against the pre-extraction baseline (the library adds no computation, only a package boundary).
- **Throughput:** N/A (correctness-focused; throughput unchanged by definition since output is byte-identical)

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** The `atcr-findings/v1` wire format is a stability contract. No field may be added, removed, renamed, or reordered in the serialized output. The fixture diff is the enforcement mechanism. Any drift is a behavioral change and blocks merge.

## Test Implementation Guidance
**Test Type:** INTEGRATION (diff-based; no new RED tests — this is the central no-regression guarantee)
**Test Data Requirements:** A pre-extraction baseline fixture set (`findings.json`, `ambiguous.json`, `disagreements.json`) captured from the current `main` branch before extraction. A representative input corpus (the existing test corpus inputs).
**Mock/Stub Requirements:** None. Run the full reconciler on the corpus inputs through the adapter, capture outputs, and `diff` against the baseline. Assert `diff` exit code 0 for each artifact. The existing `cluster_merge_test.go` and `disagree_test.go` corpus tests must also pass unchanged.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (existing corpus: `cluster_merge_test.go`, `disagree_test.go`, `emit_test.go`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `findings.json`, `ambiguous.json`, `disagreements.json` byte-identical (`diff` exit 0) against pre-extraction baseline
- [ ] `sortMerged` total-order (severity desc, then file, then line) preserved exactly
- [ ] `atcr-findings/v1` wire format unchanged (no schema drift in `docs/findings-format.md`)
- [ ] `SeverityRank` copy in `merge.go` collapsed to library canonical `NormalizeSeverity`/`SeverityRank` with zero value change

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm baseline fixtures were captured from pre-extraction `main` (not regenerated post-extraction)
