# Acceptance Criteria: Different-Model Exclusion Rule

**Related User Story:** [01: Skeptic Selection & Role Plumbing](../user-stories/01-skeptic-selection-role-plumbing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Selection function | Go package-level function | `SelectEligibleSkeptics(finding, n) []AgentConfig` |
| Package | `internal/verify` (new) | Created in this story; imports `internal/registry` and `internal/reconcile` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests |
| Key Dependencies | `internal/registry` (Registry, AgentConfig, RoleSkeptic), `internal/reconcile` (JSONFinding) | No reverse import (reconcile must NOT import verify) |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/registry/config.go:37` - reference: `RoleSkeptic` constant
- `internal/registry/config.go:56` - reference: `AgentConfig` struct with `Model` field
- `internal/reconcile/emit.go:36` - reference: `Verification` struct shape

- `internal/verify/select.go` - create: `SelectEligibleSkeptics` function and package scaffolding
- `internal/verify/select_test.go` - create: table-driven tests for different-model exclusion
- `internal/registry/config.go` - reference: `Registry`, `AgentConfig`, `AgentsByRole` (added in AC 01-01)
- `internal/reconcile/emit.go:59` - reference: `JSONFinding` struct with `Reviewers []string`

## Happy Path Scenarios

**Scenario 1: Skeptic with different model is eligible**
- **Given** a registry with skeptic `s1` (model: "gpt-4o") and a finding with `Reviewers: ["alice"]` where `alice` has model "claude-sonnet-4-20250514"
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** the returned slice contains `s1`

**Scenario 2: Multiple eligible skeptics, request n=2**
- **Given** a registry with skeptics `s1` (model: "gpt-4o"), `s2` (model: "gemini-2.5-pro"), `s3` (model: "claude-sonnet-4-20250514") and a finding with `Reviewers: ["alice"]` where `alice` has model "claude-sonnet-4-20250514"
- **When** `SelectEligibleSkeptics(finding, 2)` is called
- **Then** the returned slice contains `s1` and `s2` (not `s3`), ordered by agent name for determinism

**Scenario 3: Model comparison is exact string match**
- **Given** a registry with skeptic `s1` (model: "gpt-4o") and a finding with `Reviewers: ["alice"]` where `alice` has model "gpt-4o-mini"
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** `s1` IS eligible (exact match required; "gpt-4o" != "gpt-4o-mini")

## Edge Cases

**Edge Case 1: Finding has empty Reviewers list**
- **Given** a registry with skeptic `s1` (model: "gpt-4o") and a finding with `Reviewers: []`
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** `s1` is eligible (no reviewer models to exclude against)

**Edge Case 2: Reviewer name not found in registry**
- **Given** a finding with `Reviewers: ["ghost"]` where "ghost" is not a registered agent
- **When** `SelectEligibleSkeptics(finding, 1)` is called
- **Then** the unresolvable reviewer is skipped silently; skeptics are NOT excluded by the missing reviewer's model (defensive: agent may have been removed from registry)

**Edge Case 3: n is larger than eligible candidates**
- **Given** a registry with 1 eligible skeptic and `n=5`
- **When** `SelectEligibleSkeptics(finding, 5)` is called
- **Then** the returned slice contains exactly 1 element (the eligible skeptic)

**Edge Case 4: n is 0**
- **Given** a registry with eligible skeptics
- **When** `SelectEligibleSkeptics(finding, 0)` is called
- **Then** the returned slice is empty (no selection requested)

**Edge Case 5: Multiple reviewers with different models**
- **Given** a finding with `Reviewers: ["alice", "bob"]` where `alice` has model "claude-sonnet-4-20250514" and `bob` has model "gpt-4o"
- **When** `SelectEligibleSkeptics(finding, 10)` is called
- **Then** any skeptic with model "claude-sonnet-4-20250514" OR "gpt-4o" is excluded

## Error Conditions

**Error Scenario 1: Nil registry passed to selection**
- **Given** a nil `*Registry` argument (or uninitialised verify context)
- **When** `SelectEligibleSkeptics` is called
- **Then** the function returns nil or empty slice (defensive); no panic

## Performance Requirements
- **Response Time:** O(S * R) where S = number of skeptics, R = number of reviewers per finding; typical case < 1ms for < 50 agents
- **Throughput:** Must handle 100+ findings with 10+ reviewers each in < 10ms total

## Security Considerations
- **Authentication/Authorization:** N/A — purely in-memory selection
- **Input Validation:** Model strings are compared with `==`; no shell injection or path traversal possible
- **Data Integrity:** Selection is read-only; no mutation of registry or finding state

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Registry with skeptics on various models
- Findings with varying reviewer lists (empty, single, multiple, unresolvable names)
- Registry with mixed reviewer models to test set-building

**Mock/Stub Requirements:**
- Construct `Registry` directly (no YAML parsing needed for unit tests)
- Construct `JSONFinding` directly with test `Reviewers` slices

**Test Pattern:**
```go
func TestSelectEligibleSkeptics_DifferentModelExclusion(t *testing.T) {
    tests := []struct {
        name         string
        skeptics     map[string]registry.AgentConfig  // role: skeptic only
        reviewers    map[string]registry.AgentConfig  // all reviewer agents
        finding      reconcile.JSONFinding
        n            int
        wantNames    []string
    }{
        // table cases covering scenarios above...
    }
    // ...
}
```

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/verify/...` passes
- [x] `go vet ./internal/verify/...` clean
- [x] `go build ./...` succeeds — no import cycle between verify and reconcile
- [x] Test coverage >= 95% on `SelectEligibleSkeptics` code path

**Story-Specific:**
- [x] Skeptic sharing a model with any reviewer is excluded
- [x] Model comparison is exact string match (no aliasing)
- [x] Unresolvable reviewer names are skipped silently
- [x] Result is deterministically ordered by agent name
- [x] Result slice length is min(n, eligible_count)

**Manual Review:**
- [x] Code reviewed and approved
- [x] No import cycle: `verify` imports `reconcile`, but `reconcile` does NOT import `verify`
