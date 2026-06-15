# Acceptance Criteria: Confidence V2 Recomputation

**Related User Story:** [03: Confidence v2 & Re-emit](../user-stories/03-confidence-v2-re-emit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Function | Go pure function in `internal/verify/confidence_v2.go` | No I/O; pure mapping |
| Test Framework | go test + testify | Table-driven tests |
| Key Dependencies | `internal/reconcile` constants (ConfHigh, ConfMedium, ConfLow) | Import for constant values |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:36` - reference: `Verification` struct shape
- `internal/reconcile/gate.go:57` - reference: `CountAtOrAbove` (consumer of confidence v2)

- `internal/verify/confidence_v2.go` - create: `confidenceV2(v1Confidence, verdict string) string` pure function and `ConfidenceVerified` constant
- `internal/verify/confidence_v2_test.go` - create: table-driven tests covering all verdict x v1-confidence combinations
- `internal/reconcile/merge.go` - modify: none (read-only reference for ConfHigh/ConfMedium/ConfLow constants)

## Happy Path Scenarios

**Scenario 1: Confirmed verdict promotes to VERIFIED**
- **Given** a finding with v1 confidence `"HIGH"` and skeptic verdict `"confirmed"`
- **When** `confidenceV2("HIGH", "confirmed")` is called
- **Then** the result is `"VERIFIED"`

**Scenario 2: Refuted verdict demotes to LOW**
- **Given** a finding with v1 confidence `"HIGH"` and skeptic verdict `"refuted"`
- **When** `confidenceV2("HIGH", "refuted")` is called
- **Then** the result is `"LOW"`

**Scenario 3: Unverifiable verdict retains v1 confidence**
- **Given** a finding with v1 confidence `"MEDIUM"` and skeptic verdict `"unverifiable"`
- **When** `confidenceV2("MEDIUM", "unverifiable")` is called
- **Then** the result is `"MEDIUM"`

**Scenario 4: Empty verdict (no skeptic ran) retains v1 confidence**
- **Given** a finding with v1 confidence `"LOW"` and no verdict (`""`)
- **When** `confidenceV2("LOW", "")` is called
- **Then** the result is `"LOW"`

## Edge Cases

**Edge Case 1: Confirmed verdict on LOW confidence finding promotes to VERIFIED**
- **Given** a finding with v1 confidence `"LOW"` and verdict `"confirmed"`
- **When** `confidenceV2("LOW", "confirmed")` is called
- **Then** the result is `"VERIFIED"` (confirmation overrides regardless of v1 level)

**Edge Case 2: Refuted verdict on already LOW confidence stays LOW**
- **Given** a finding with v1 confidence `"LOW"` and verdict `"refuted"`
- **When** `confidenceV2("LOW", "refuted")` is called
- **Then** the result is `"LOW"`

**Edge Case 3: Unknown verdict string treated as no-op**
- **Given** a finding with v1 confidence `"HIGH"` and verdict `"unknown_value"`
- **When** `confidenceV2("HIGH", "unknown_value")` is called
- **Then** the result is `"HIGH"` (unknown verdicts pass through unchanged)

## Error Conditions

No error conditions — `confidenceV2` is a pure function that never returns an error. Invalid inputs (unknown verdict, unknown v1 confidence) are handled by pass-through.

## Performance Requirements
- **Response Time:** < 1 microsecond per call (pure function, no allocation)
- **Throughput:** N/A (called once per finding during re-emit)

## Security Considerations
- **Input Validation:** Verdict values are not validated here — writer responsibility (per `emit.go:36` contract comment).
- **No external input:** Pure function with no I/O or side effects.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table of all verdict values (`confirmed`, `refuted`, `unverifiable`, `""`, unknown) crossed with all v1 confidence levels (`HIGH`, `MEDIUM`, `LOW`).
**Mock/Stub Requirements:** None — pure function, no dependencies to mock.

```go
func TestConfidenceV2(t *testing.T) {
    tests := []struct {
        name     string
        v1       string
        verdict  string
        expected string
    }{
        {"confirmed/HIGH", "HIGH", "confirmed", "VERIFIED"},
        {"confirmed/MEDIUM", "MEDIUM", "confirmed", "VERIFIED"},
        {"confirmed/LOW", "LOW", "confirmed", "VERIFIED"},
        {"refuted/HIGH", "HIGH", "refuted", "LOW"},
        {"refuted/MEDIUM", "MEDIUM", "refuted", "LOW"},
        {"refuted/LOW", "LOW", "refuted", "LOW"},
        {"unverifiable/HIGH", "HIGH", "unverifiable", "HIGH"},
        {"unverifiable/MEDIUM", "MEDIUM", "unverifiable", "MEDIUM"},
        {"unverifiable/LOW", "LOW", "unverifiable", "LOW"},
        {"empty/HIGH", "HIGH", "", "HIGH"},
        {"empty/MEDIUM", "MEDIUM", "", "MEDIUM"},
        {"empty/LOW", "LOW", "", "LOW"},
        {"unknown/HIGH", "HIGH", "garbage", "HIGH"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := confidenceV2(tt.v1, tt.verdict)
            assert.Equal(t, tt.expected, got)
        })
    }
}
```

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...`)
- [x] No linting errors (`go vet ./internal/verify/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `confidenceV2` function exists and is exported or unexported per convention
- [x] `ConfidenceVerified = "VERIFIED"` constant defined
- [x] Table-driven test covers all 13 verdict x v1-confidence combinations
- [x] No import cycle between `internal/verify` and `internal/reconcile`

**Manual Review:**
- [x] Code reviewed and approved
