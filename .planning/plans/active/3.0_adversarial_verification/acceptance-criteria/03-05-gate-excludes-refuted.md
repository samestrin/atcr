# Acceptance Criteria: Gate Counter Excludes Refuted Findings

**Related User Story:** [03: Confidence v2 & Re-emit](../user-stories/03-confidence-v2-re-emit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Function | Go function modification in `internal/reconcile/gate.go` | Filter before counting |
| Test Framework | go test + testify | Table-driven tests |
| Key Dependencies | `internal/reconcile` (Merged, Verification, CountAtOrAbove) | In-package modification |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:36` - reference: `Verification` struct shape
- `internal/reconcile/merge.go:48` - reference: `Merge` (produces `Merged` input to gate)

- `internal/reconcile/gate.go` - modify: `CountAtOrAbove` (line 57) to exclude refuted findings
- `internal/reconcile/gate_test.go` - modify: add test cases for refuted finding exclusion
- `internal/reconcile/merge.go` - reference: `Merged` struct, confidence constants

## Happy Path Scenarios

**Scenario 1: Refuted findings excluded from gate count**
- **Given** 5 findings: 3 with `Severity: "HIGH"` (no verdict), 1 with `Severity: "HIGH"` and `Verification.Verdict: "refuted"`, 1 with `Severity: "LOW"`
- **When** `CountAtOrAbove(findings, "HIGH")` is called
- **Then** the count is 3 (the refuted HIGH finding is excluded)

**Scenario 2: Non-refuted LOW confidence findings still counted at LOW threshold**
- **Given** 2 findings: 1 with `Severity: "LOW"`, `Confidence: "LOW"`, no verdict; 1 with `Severity: "LOW"`, `Confidence: "LOW"`, `Verification.Verdict: "refuted"`
- **When** `CountAtOrAbove(findings, "LOW")` is called
- **Then** the count is 1 (only the non-refuted finding counts)

**Scenario 3: Unverifiable findings are NOT excluded (only refuted)**
- **Given** 2 findings: 1 with `Severity: "HIGH"`, `Verification.Verdict: "unverifiable"`; 1 with `Severity: "HIGH"`, `Verification.Verdict: "refuted"`
- **When** `CountAtOrAbove(findings, "HIGH")` is called
- **Then** the count is 1 (unverifiable counts, refuted does not)

## Edge Cases

**Edge Case 1: Finding with no Verification field (v1 finding, nil pointer)**
- **Given** a finding with `Verification: nil` and `Severity: "HIGH"`
- **When** `CountAtOrAbove` evaluates this finding
- **Then** The finding IS counted (nil Verification means no verdict, not refuted)

**Edge Case 2: Finding with Verification.Verdict = "" (empty string)**
- **Given** a finding with `Verification: &Verification{Verdict: ""}` and `Severity: "HIGH"`
- **When** `CountAtOrAbove` evaluates this finding
- **Then** The finding IS counted (empty verdict is not "refuted")

**Edge Case 3: Out-of-scope findings still excluded (existing behavior preserved)**
- **Given** a finding with `Category: "out-of-scope"`, `Severity: "CRITICAL"`, no verdict
- **When** `CountAtOrAbove(findings, "CRITICAL")` is called
- **Then** The finding is NOT counted (existing out-of-scope exclusion preserved)

**Edge Case 4: All findings refuted**
- **Given** 3 findings all with `Verification.Verdict: "refuted"`
- **When** `CountAtOrAbove(findings, "LOW")` is called
- **Then** the count is 0

## Error Conditions

No new error conditions. `CountAtOrAbove` is a pure function that takes a slice and returns an int. The filter logic is internal and never errors.

## Performance Requirements
- **Response Time:** < 10 microseconds for 500 findings (single pass filter + count)
- **Throughput:** N/A (called once per gate check)

## Security Considerations
- **No external input:** Pure function operating on in-memory data.
- **Filter correctness:** Critical for CI gate — a bug that excludes non-refuted findings would let real issues pass CI. Mitigated by exhaustive table-driven tests.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `[]Merged` slices with various combinations of severity, confidence, category, and verification verdict.
**Mock/Stub Requirements:** None — pure function, no I/O.

```go
func TestCountAtOrAbove_ExcludesRefuted(t *testing.T) {
    findings := []Merged{
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "HIGH"},},
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "HIGH"},},
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "VERIFIED"},},
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "LOW"},
         // refuted: should NOT count
        },
    }
    // Set verification on last finding
    // Note: Merged embeds stream.Finding which has no Verification field;
    // the gate operates on reconcile.Merged which may need a Verification
    // field added, OR the gate checks confidence="LOW" + a separate verdict source.
    // Implementation must ensure refuted findings are identifiable at gate time.

    got := CountAtOrAbove(findings, "HIGH")
    assert.Equal(t, 3, got) // 3 non-refuted HIGH findings
}
```

**Important implementation note:** The `Merged` struct (which embeds `stream.Finding`) may need a `Verification` field added, or the gate must access verification data from a separate source. The simplest approach is to add `Verification *Verification` to `Merged` (matching the field already on `JSONFinding`), populated during re-emit before the gate runs.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/reconcile/...`)
- [ ] No linting errors (`go vet ./internal/reconcile/...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `CountAtOrAbove` excludes findings with `Verification.Verdict == "refuted"`
- [ ] Existing out-of-scope exclusion preserved
- [ ] Findings with nil/empty verdict are NOT excluded
- [ ] Unverifiable findings are NOT excluded
- [ ] Table-driven tests cover: refuted excluded, unverifiable included, nil verification included, empty verdict included, out-of-scope still excluded, all-refuted returns 0

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Gate logic reviewed for CI-blocking correctness (incorrect exclusion = real findings pass CI)
