# Acceptance Criteria: Empty Selection and Unverifiable Verdict Contract

**Related User Story:** [01: Skeptic Selection & Role Plumbing](../user-stories/01-skeptic-selection-role-plumbing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Selection function | `SelectEligibleSkeptics` in `internal/verify` | Returns empty slice when no eligible skeptics |
| Verdict contract | `Verification` struct in `internal/reconcile` | `verdict="unverifiable"`, `notes="no_eligible_skeptic"` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests |
| Key Dependencies | `internal/registry`, `internal/reconcile` | Empty-slice return is caller-mapped to verdict |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/registry/config.go:37` - reference: `RoleSkeptic` constant

- `internal/verify/select.go` - modify: document empty-selection return contract
- `internal/verify/select_test.go` - modify: test cases for all no-eligible-skeptic conditions
- `internal/reconcile/emit.go:36` - reference: `Verification` struct (`Verdict`, `Skeptic`, `Notes` fields)
- `internal/reconcile/emit.go:59` - reference: `JSONFinding` struct with `*Verification` field

## Happy Path Scenarios

**Scenario 1: No skeptics registered returns empty slice**
- **Given** a `Registry` with no agents having `role: skeptic`
- **When** `SelectEligibleSkeptics(finding, 2)` is called
- **Then** the returned slice is empty (len == 0, non-nil)

**Scenario 2: All skeptics share models with reviewers returns empty slice**
- **Given** a registry with skeptics `s1` (model: "gpt-4o"), `s2` (model: "claude-sonnet-4-20250514") and a finding with `Reviewers: ["alice", "bob"]` where both reviewers use models "gpt-4o" and "claude-sonnet-4-20250514" respectively
- **When** `SelectEligibleSkeptics(finding, 2)` is called
- **Then** the returned slice is empty (all skeptics excluded by different-model rule)

**Scenario 3: Callers map empty selection to unverifiable verdict**
- **Given** `SelectEligibleSkeptics` returns an empty slice for a finding
- **When** the caller (later story: verification pipeline) processes the empty result
- **Then** the caller produces `Verification{Verdict: "unverifiable", Notes: "no_eligible_skeptic"}` on the finding's `*Verification` field

## Edge Cases

**Edge Case 1: Single skeptic excluded by single reviewer model**
- **Given** a registry with exactly one skeptic `s1` (model: "gpt-4o") and a finding with `Reviewers: ["alice"]` where alice has model "gpt-4o"
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** the returned slice is empty

**Edge Case 2: Reviewers list contains duplicate names**
- **Given** a finding with `Reviewers: ["alice", "alice"]` where alice has model "gpt-4o"
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** the duplicate reviewer does not cause errors; each unique reviewer model appears exactly once in the exclusion set and no duplicate causes a panic or incorrect exclusion

**Edge Case 3: Registry has agents but none with RoleSkeptic**
- **Given** a registry with agents all having `role: reviewer` or `role: judge`
- **When** `SelectEligibleSkeptics(finding, 5)` is called
- **Then** the returned slice is empty

## Error Conditions

**Error Scenario 1: Finding with nil Reviewers slice**
- **Given** a `JSONFinding` with `Reviewers: nil`
- **When** `SelectEligibleSkeptics` is called
- **Then** the function treats nil the same as empty slice — no reviewers to match against, all skeptics eligible

## Performance Requirements
- **Response Time:** Empty-selection path must complete in < 1ms (early return after building model set)
- **Throughput:** No allocation amplification on empty result

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** Nil/empty reviewer lists handled gracefully; no panics on nil slices
- **Data Integrity:** Empty slice return is non-nil so callers can distinguish "no selection" from "not called"

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Registry with skeptics all sharing models with reviewers
- Registry with no skeptics at all
- Findings with nil, empty, and duplicate reviewer lists

**Mock/Stub Requirements:**
- Construct `Registry` and `JSONFinding` directly
- No YAML parsing needed

**Test Pattern:**
```go
func TestSelectEligibleSkeptics_EmptySelection(t *testing.T) {
    tests := []struct {
        name      string
        skeptics  map[string]registry.AgentConfig
        reviewers map[string]registry.AgentConfig
        finding   reconcile.JSONFinding
        n         int
    }{
        // cases: no skeptics, all excluded, nil reviewers...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := SelectEligibleSkeptics(tt.reg, tt.finding, tt.n)
            assert.Empty(t, got)
            assert.NotNil(t, got) // non-nil empty slice
        })
    }
}
```

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/verify/...` passes
- [x] `go vet ./internal/verify/...` clean
- [x] Test coverage >= 95% on empty-selection code paths

**Story-Specific:**
- [x] Empty slice returned when no skeptics are registered
- [x] Empty slice returned when all skeptics share models with reviewers
- [x] Empty slice is non-nil (distinguishable from "not called")
- [x] Nil `Reviewers` slice handled without panic
- [x] Contract documented: empty selection maps to `verdict="unverifiable"`, `notes="no_eligible_skeptic"`

**Manual Review:**
- [x] Code reviewed and approved
- [x] Verdict contract documented in function godoc for future callers (this story defines the contract; later stories implement the caller)
