# Acceptance Criteria: AgentsByRole Filtering

**Related User Story:** [01: Skeptic Selection & Role Plumbing](../user-stories/01-skeptic-selection-role-plumbing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Method receiver | Go method on `*Registry` | `AgentsByRole(role string) map[string]AgentConfig` |
| Package | `internal/registry` | Lives in existing `config.go` or new file in same package |
| Test Framework | `go test` + `testify/assert` | Table-driven tests matching existing registry patterns |
| Key Dependencies | None (standard library only) | Uses existing `RoleReviewer`, `RoleSkeptic`, `RoleJudge` constants |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/registry/config.go:111` - reference: `roleValid()` validates role values
- `internal/registry/config.go:262` - reference: `applyDefaults()` — intentionally does NOT mutate `Role`

- `internal/registry/config.go` - modify: add `AgentsByRole(role string) map[string]AgentConfig` method on `*Registry`
- `internal/registry/config_test.go` - modify: add table-driven tests for `AgentsByRole`
- `internal/registry/config.go:37` - reference: `RoleReviewer`, `RoleSkeptic`, `RoleJudge` constants
- `internal/registry/config.go:56` - reference: `AgentConfig` struct with `Role` field

## Happy Path Scenarios

**Scenario 1: Filter skeptics from a mixed-role registry**
- **Given** a `Registry` with agents: `alice` (role:reviewer), `bob` (role:skeptic), `carol` (role:skeptic), `dave` (role:judge)
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** the returned map contains exactly `bob` and `carol` with their full `AgentConfig` values, and does not contain `alice` or `dave`

**Scenario 2: Filter reviewers from a mixed-role registry**
- **Given** a `Registry` with agents: `alice` (role:reviewer), `bob` (role:skeptic), `carol` (role:"")
- **When** `AgentsByRole(RoleReviewer)` is called
- **Then** the returned map contains `alice` and `carol` (empty-role defaults to reviewer), but not `bob`

**Scenario 3: Filter judges**
- **Given** a `Registry` with agents: `alice` (role:judge), `bob` (role:reviewer)
- **When** `AgentsByRole(RoleJudge)` is called
- **Then** the returned map contains exactly `alice`

## Edge Cases

**Edge Case 1: Unknown role string returns empty map**
- **Given** a `Registry` with any agents
- **When** `AgentsByRole("unknown_role")` is called
- **Then** the returned map is non-nil and empty (len == 0)

**Edge Case 2: Empty registry returns empty map**
- **Given** a `Registry` with `Agents: map[string]AgentConfig{}`
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** the returned map is non-nil and empty

**Edge Case 3: No agents match the requested role**
- **Given** a `Registry` where all agents have `role: reviewer`
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** the returned map is non-nil and empty

## Error Conditions

**Error Scenario 1: Nil registry receiver**
- **Given** a nil `*Registry` pointer
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** the method returns a non-nil empty map (defensive nil-guard), or the test documents that callers must not call on nil

## Performance Requirements
- **Response Time:** O(n) where n is the number of agents — single pass over the map
- **Throughput:** Must handle registries with 100+ agents in < 1ms (no allocation amplification)

## Security Considerations
- **Authentication/Authorization:** N/A — purely in-memory config query, no external I/O
- **Input Validation:** Role parameter is a plain string; no injection risk since it is compared with `==` against known constants
- **Data Integrity:** Returns a shallow copy of matching agents; caller mutations do not affect the registry's internal map

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Registry fixtures with mixed roles (reviewer, skeptic, judge, empty)
- Registry with only one role
- Empty registry

**Mock/Stub Requirements:** None — pure in-memory operation on existing types

**Test Pattern:**
```go
func TestAgentsByRole(t *testing.T) {
    tests := []struct {
        name     string
        agents   map[string]AgentConfig
        role     string
        wantKeys []string // sorted agent names expected in result
    }{
        // table cases...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            reg := &Registry{Agents: tt.agents}
            got := reg.AgentsByRole(tt.role)
            // assert keys match wantKeys
        })
    }
}
```

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/registry/...` passes
- [ ] `go vet ./internal/registry/...` clean
- [ ] `go build ./...` succeeds (no import cycles)
- [ ] Test coverage >= 95% on `AgentsByRole` code path

**Story-Specific:**
- [ ] `AgentsByRole(RoleSkeptic)` returns only agents with `role: skeptic`
- [ ] `AgentsByRole(RoleReviewer)` includes empty-role agents (backward compatible)
- [ ] Returns non-nil empty map for unknown roles and empty registries
- [ ] Returned map values are usable `AgentConfig` structs (not zeroed)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Method signature matches the contract in the story (returns `map[string]AgentConfig`)
