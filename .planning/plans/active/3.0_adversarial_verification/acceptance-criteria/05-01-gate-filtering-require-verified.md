# Acceptance Criteria: Gate Filtering with `--fail-on` and `--require-verified`

**Related User Story:** [05: Gate Semantics](../user-stories/05-gate-semantics.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Gate Logic | Go function modification in `internal/reconcile/gate.go` | `CountAtOrAbove` gains `requireVerified bool` parameter or new wrapper `CountFailing` |
| CLI Flag | Go `flag` / `cobra` flag in `cmd/atcr/reconcile.go` | `--require-verified` (bool, default false) |
| Test Framework | go test + testify | Table-driven matrix tests |
| Key Dependencies | `internal/reconcile` (Merged, JSONFinding, Verification, AtOrAbove) | In-package modification |

## Related Files
- `internal/reconcile/gate.go` - modify: add `requireVerified` filtering to `CountAtOrAbove` (line 57) or add new `CountFailing` wrapper
- `internal/reconcile/gate_test.go` - modify: add matrix tests for all verdict/severity/flag combinations
- `cmd/atcr/reconcile.go` - modify: add `--require-verified` flag, pass through to gate logic
- `internal/reconcile/emit.go` - reference: `Verification` struct (line 36), `JSONFinding` struct (line 47)
- `internal/reconcile/merge.go` - reference: severity constants, `Merged` struct

## Happy Path Scenarios

**Scenario 1: `--fail-on` excludes refuted findings regardless of v1 severity**
- **Given** 3 findings: (A) `Severity: "HIGH"`, `Verification.Verdict: "refuted"`; (B) `Severity: "HIGH"`, `Verification.Verdict: "confirmed"`; (C) `Severity: "HIGH"`, `Verification.Verdict: "unverifiable"`
- **When** `CountAtOrAbove(findings, "HIGH")` is called (no `requireVerified`)
- **Then** the count is 2 (B and C; refuted finding A is excluded)

**Scenario 2: `--require-verified` counts only VERIFIED findings at threshold**
- **Given** Same 3 findings as Scenario 1
- **When** gate is called with `requireVerified=true` and `threshold="HIGH"`
- **Then** the count is 1 (only finding B with verdict=confirmed / confidence=VERIFIED)

**Scenario 3: `--fail-on` without `--require-verified` preserves backward-compatible semantics**
- **Given** 4 findings: (A) `Severity: "HIGH"`, no Verification (v1 finding); (B) `Severity: "MEDIUM"`, `Verification.Verdict: "confirmed"`; (C) `Severity: "HIGH"`, `Verification.Verdict: "unverifiable"`; (D) `Severity: "HIGH"`, `Verification.Verdict: "refuted"`
- **When** `CountAtOrAbove(findings, "HIGH")` is called with `requireVerified=false`
- **Then** the count is 2 (A and C; D excluded as refuted, B is below threshold)

**Scenario 4: CLI flag `--require-verified` wired through `atcr reconcile`**
- **Given** A reconciled review with findings including refuted, confirmed, and unverifiable verdicts
- **When** `atcr reconcile --fail-on HIGH --require-verified <review>` is executed
- **Then** exit code 0 if only refuted/unverifiable findings exist above HIGH (no VERIFIED findings), exit code 1 if any VERIFIED finding at HIGH or above exists

## Edge Cases

**Edge Case 1: All findings refuted with `--require-verified`**
- **Given** 3 findings all with `Verification.Verdict: "refuted"`, all `Severity: "HIGH"`
- **When** gate called with `requireVerified=true` and `threshold="HIGH"`
- **Then** the count is 0 (no finding survived scrutiny; gate passes — correct semantic)

**Edge Case 2: Finding with nil Verification and `--require-verified`**
- **Given** A finding with `Severity: "HIGH"`, `Verification: nil` (v1 finding)
- **When** gate called with `requireVerified=true` and `threshold="HIGH"`
- **Then** the finding is NOT counted (nil Verification means not VERIFIED; only refuted exclusion applies without `requireVerified`)

**Edge Case 3: `--require-verified` without `--fail-on`**
- **Given** `--require-verified` is set but `--fail-on` is not specified (no threshold)
- **When** The gate evaluation runs
- **Then** `--require-verified` has no effect (documented: requires `--fail-on` to take effect); alternatively, returns a usage error

**Edge Case 4: Out-of-scope findings still excluded regardless of verdict**
- **Given** A finding with `Category: "out-of-scope"`, `Severity: "CRITICAL"`, `Verification.Verdict: "confirmed"`
- **When** gate called with `requireVerified=true` and `threshold="CRITICAL"`
- **Then** the finding is NOT counted (out-of-scope exclusion takes precedence)

**Edge Case 5: Empty verdict string**
- **Given** A finding with `Severity: "HIGH"`, `Verification.Verdict: ""` (empty)
- **When** `CountAtOrAbove(findings, "HIGH")` is called
- **Then** the finding IS counted (empty verdict is not "refuted"; not VERIFIED either)

## Error Conditions

**Error Scenario 1: Invalid severity threshold with `--require-verified`**
- Error message: `invalid severity threshold: "BLOCKER" (must be CRITICAL, HIGH, MEDIUM, or LOW)`
- Behavior: exit code 2 (usage error); `--require-verified` does not affect threshold validation

## Performance Requirements
- **Response Time:** < 10 microseconds for 500 findings (single-pass filter + count, same as current `CountAtOrAbove`)
- **Throughput:** N/A (called once per gate check)

## Security Considerations
- **No external input:** Pure function operating on in-memory data. No new injection surface.
- **Filter correctness:** Critical for CI gate — incorrect `requireVerified` logic could cause VERIFIED findings to be skipped (real issues pass CI) or non-VERIFIED findings to count (false blocks). Mitigated by exhaustive matrix tests.
- **Flag validation:** `--require-verified` is a boolean; no parsing risk.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `[]Merged` (or `[]JSONFinding`) slices with combinations of:
- Verdicts: confirmed, refuted, unverifiable, nil (no Verification), empty string
- Severities: CRITICAL, HIGH, MEDIUM, LOW
- Categories: normal, out-of-scope
- Flag states: `requireVerified=true`, `requireVerified=false`

**Mock/Stub Requirements:** None — pure function, no I/O.

```go
// Matrix test structure (>= 12 cases)
func TestCountFailing_Matrix(t *testing.T) {
    tests := []struct {
        name             string
        findings         []Merged // with Verification set
        threshold        string
        requireVerified  bool
        want             int
    }{
        // 3 verdicts x 3 severities x 2 flag states = 18 minimum
        // Plus: nil verification, empty verdict, out-of-scope
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CountFailing(tt.findings, tt.threshold, tt.requireVerified)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/reconcile/... ./cmd/atcr/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `CountAtOrAbove` (or wrapper) excludes findings with `Verification.Verdict == "refuted"` before counting
- [ ] `requireVerified=true` restricts count to findings with `confidence == "VERIFIED"` at or above threshold
- [ ] `--require-verified` CLI flag added to `atcr reconcile` (and `atcr review` if applicable)
- [ ] Out-of-scope exclusion preserved regardless of `requireVerified` state
- [ ] Matrix tests cover >= 12 distinct scenarios (3 verdicts x 3 severities x 2 flag states minimum)
- [ ] Findings with nil/empty verdict are not excluded by `--fail-on`, not counted by `--require-verified`

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Gate logic reviewed for CI-blocking correctness (refuted never blocks; VERIFIED is strictest gate)
- [ ] `--require-verified` without `--fail-on` behavior documented (no-op or usage error)
