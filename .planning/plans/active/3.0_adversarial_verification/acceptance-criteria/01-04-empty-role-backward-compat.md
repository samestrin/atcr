# Acceptance Criteria: Empty-Role Backward Compatibility

**Related User Story:** [01: Skeptic Selection & Role Plumbing](../user-stories/01-skeptic-selection-role-plumbing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Role normalization | Go logic in `AgentsByRole` method | Empty `Role` treated as `RoleReviewer` before comparison |
| Package | `internal/registry` | Existing package; no schema changes |
| Test Framework | `go test` + `testify/assert` | Table-driven tests |
| Key Dependencies | `RoleReviewer`, `RoleSkeptic`, `roleValid` existing constants and validation | `roleValid("")` already returns true |

## Related Files
- `internal/registry/config.go` - modify: add empty-role normalization in `AgentsByRole` (empty → `RoleReviewer`)
- `internal/registry/config_test.go` - modify: add backward-compatibility test cases for empty-role agents
- `internal/registry/config.go:111` - reference: `roleValid` function (empty string already passes validation)
- `internal/registry/config.go:262` - reference: `applyDefaults` function (does NOT set default role — intentional per option-a decision)

## Happy Path Scenarios

**Scenario 1: Empty-role agent included when filtering reviewers**
- **Given** a `Registry` with agent `legacy-agent` having `Role: ""` (empty string) and agent `explicit-reviewer` having `Role: "reviewer"`
- **When** `AgentsByRole(RoleReviewer)` is called
- **Then** the returned map contains BOTH `legacy-agent` and `explicit-reviewer`

**Scenario 2: Empty-role agent excluded when filtering skeptics**
- **Given** a `Registry` with agent `legacy-agent` having `Role: ""`
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** the returned map does NOT contain `legacy-agent`

**Scenario 3: Empty-role agent excluded when filtering judges**
- **Given** a `Registry` with agent `legacy-agent` having `Role: ""`
- **When** `AgentsByRole(RoleJudge)` is called
- **Then** the returned map does NOT contain `legacy-agent`

## Edge Cases

**Edge Case 1: All agents have empty roles (1.x config)**
- **Given** a `Registry` where every agent has `Role: ""` (simulating a 1.x config with no role fields)
- **When** `AgentsByRole(RoleReviewer)` is called
- **Then** the returned map contains all agents

**Edge Case 2: Mix of empty-role, explicit reviewer, skeptic, and judge**
- **Given** agents: `a1` (role:""), `a2` (role:"reviewer"), `a3` (role:"skeptic"), `a4` (role:"judge")
- **When** `AgentsByRole(RoleReviewer)` is called
- **Then** result contains `a1` and `a2` only
- **When** `AgentsByRole(RoleSkeptic)` is called
- **Then** result contains `a3` only
- **When** `AgentsByRole(RoleJudge)` is called
- **Then** result contains `a4` only

**Edge Case 3: `applyDefaults` does NOT set Role to reviewer**
- **Given** a `Registry` with agent `legacy-agent` having `Role: ""`
- **When** `LoadRegistry` is called (which calls `applyDefaults`)
- **Then** `legacy-agent.Role` remains `""` after loading (the defaulting to reviewer happens ONLY in `AgentsByRole`, not in the loaded config)

## Error Conditions

No error conditions — empty role is valid per `roleValid()` and this AC only affects filtering behavior.

## Performance Requirements
- **Response Time:** Role normalization adds O(1) per agent during filtering; negligible impact
- **Throughput:** No performance regression on existing `LoadRegistry` path

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** Empty role is already validated as acceptable by `roleValid`; no new validation needed
- **Data Integrity:** `AgentsByRole` does not mutate the `AgentConfig.Role` field — normalization is local to the method, preserving the option-a decision (distinguishing "explicitly set" from "inherited default")

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Registry with empty-role agents
- Registry with mixed explicit and empty roles
- Registry simulating a 1.x config (all empty roles)

**Mock/Stub Requirements:** None — uses existing `Registry` and `AgentConfig` types

**Test Pattern:**
```go
func TestAgentsByRole_EmptyRoleBackwardCompat(t *testing.T) {
    tests := []struct {
        name     string
        agents   map[string]AgentConfig
        role     string
        wantKeys []string
    }{
        {
            name: "empty role included as reviewer",
            agents: map[string]AgentConfig{
                "legacy": {Provider: "p", Model: "m", Role: ""},
            },
            role:     RoleReviewer,
            wantKeys: []string{"legacy"},
        },
        {
            name: "empty role excluded from skeptic",
            agents: map[string]AgentConfig{
                "legacy": {Provider: "p", Model: "m", Role: ""},
            },
            role:     RoleSkeptic,
            wantKeys: nil,
        },
        // more cases...
    }
    // ...
}
```

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/registry/...` passes
- [ ] `go vet ./internal/registry/...` clean
- [ ] Existing 1.x/2.0 test fixtures still pass (no regressions)
- [ ] Test coverage >= 95% on empty-role normalization path

**Story-Specific:**
- [ ] Empty-role agents are returned by `AgentsByRole(RoleReviewer)`
- [ ] Empty-role agents are NOT returned by `AgentsByRole(RoleSkeptic)` or `AgentsByRole(RoleJudge)`
- [ ] `applyDefaults` does NOT mutate `Role` field (remains empty after load)
- [ ] 1.x config simulation (all empty roles) returns all agents as reviewers

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Verified that `AgentsByRole` normalization does not modify the underlying `AgentConfig` in the registry map
