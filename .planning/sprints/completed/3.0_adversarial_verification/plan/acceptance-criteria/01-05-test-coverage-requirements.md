# Acceptance Criteria: Comprehensive Table-Driven Test Coverage

**Related User Story:** [01: Skeptic Selection & Role Plumbing](../user-stories/01-skeptic-selection-role-plumbing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test framework | `go test` + `testify/assert` | Table-driven subtests matching existing registry patterns |
| Coverage tool | `go test -cover` | Must achieve >= 95% on new code paths |
| Build verification | `go build ./...` | Confirms no import cycles (verify → reconcile, not reverse) |
| Key Dependencies | `internal/registry`, `internal/verify`, `internal/reconcile` | Cross-package tests exercise integration seams |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/registry/config.go:37` - reference: `RoleSkeptic` constant
- `internal/registry/config.go:56` - reference: `AgentConfig` struct with `Role` field
- `internal/registry/config.go:111` - reference: `roleValid()` validates role values
- `internal/registry/config.go:262` - reference: `applyDefaults()` — intentionally does NOT mutate `Role`
- `internal/reconcile/emit.go:36` - reference: `Verification` struct shape
- `internal/reconcile/emit.go:59` - reference: `JSONFinding` struct with `Reviewers` field

- `internal/registry/config_test.go` - modify: add `TestAgentsByRole` table-driven tests
- `internal/verify/select_test.go` - create: add `TestSelectEligibleSkeptics` table-driven tests covering all scenarios
- `internal/verify/select.go` - reference: function under test
- `internal/registry/config.go` - reference: types and constants used in test fixtures

## Happy Path Scenarios

**Scenario 1: All new code paths have >= 95% test coverage**
- **Given** the `internal/registry` and `internal/verify` packages with new code from AC 01-01 through 01-04
- **When** `go test -cover ./internal/registry/... ./internal/verify/...` is run
- **Then** coverage on new code paths (`AgentsByRole`, `SelectEligibleSkeptics`, empty-role normalization, model set building) is >= 95%

**Scenario 2: CI passes with all new tests**
- **Given** all test files from AC 01-01 through 01-04
- **When** `go test ./internal/registry/... ./internal/verify/...` is run
- **Then** all tests pass, no panics, no race conditions (`-race` flag clean)

**Scenario 3: No import cycle between verify and reconcile**
- **Given** the new `internal/verify` package imports `internal/reconcile`
- **When** `go build ./...` is run
- **Then** the build succeeds with no import cycle errors

## Edge Cases

**Edge Case 1: n-selection with fewer candidates than requested**
- **Given** a registry with 2 eligible skeptics and `n=10`
- **When** `SelectEligibleSkeptics(finding, 10)` is called
- **Then** the returned slice contains exactly 2 elements

**Edge Case 2: Deterministic ordering**
- **Given** a registry with skeptics `zebra`, `alpha`, `mango` (all eligible)
- **When** `SelectEligibleSkeptics(finding, 3)` is called multiple times
- **Then** every call returns [`alpha`, `mango`, `zebra`] (alphabetical by agent name)

**Edge Case 3: Combined test — mixed registry, multiple findings**
- **Given** a registry with 3 skeptics, 5 reviewers, and a judge; and 3 findings with different reviewer compositions
- **When** `SelectEligibleSkeptics` is called for each finding
- **Then** each finding gets the correct eligible set per its reviewer models

## Error Conditions

**Error Scenario 1: go vet detects issues in new code**
- **Given** new code in `internal/verify` and modifications in `internal/registry`
- **When** `go vet ./internal/registry/... ./internal/verify/...` is run
- **Then** no issues detected

## Performance Requirements
- **Test Execution:** All new unit tests complete in < 5 seconds total
- **CI Integration:** Tests must pass under `go test -race` with no data races

## Security Considerations
- **Authentication/Authorization:** N/A — test-only code
- **Input Validation:** Tests cover nil, empty, and adversarial inputs (duplicate reviewers, unresolvable names)
- **Data Integrity:** Tests verify no mutation of registry or finding state during selection

## Test Implementation Guidance
**Test Type:** UNIT (all AC 01-01 through 01-04 test scenarios consolidated)

**Test Data Requirements:**
- Shared test fixtures for registry construction (helper function `testRegistry(agents map[string]AgentConfig) *Registry`)
- Shared helper for constructing `JSONFinding` with reviewer lists
- Table-driven subtests organized by scenario category

**Mock/Stub Requirements:**
- No mocks needed — pure types
- Helper functions to reduce boilerplate in table test definitions

**Test File Organization:**
```
internal/registry/
  config_test.go      — TestAgentsByRole (covers AC 01-01, 01-04)
internal/verify/
  select_test.go      — TestSelectEligibleSkeptics (covers AC 01-02, 01-03)
                        Sub-tests: DifferentModelExclusion, EmptySelection,
                                   Ordering, EdgeCases
```

**Required Test Cases (minimum):**

| # | Test Case | Package | AC |
|---|-----------|---------|-----|
| 1 | Mixed-role filtering: skeptic | registry | 01-01 |
| 2 | Mixed-role filtering: reviewer | registry | 01-01 |
| 3 | Unknown role returns empty map | registry | 01-01 |
| 4 | Empty registry returns empty map | registry | 01-01 |
| 5 | Different-model exclusion | verify | 01-02 |
| 6 | Exact string match (no aliasing) | verify | 01-02 |
| 7 | Unresolvable reviewer skipped | verify | 01-02 |
| 8 | n > candidates returns all | verify | 01-02 |
| 9 | n = 0 returns empty | verify | 01-02 |
| 10 | No skeptics registered → empty | verify | 01-03 |
| 11 | All skeptics excluded → empty | verify | 01-03 |
| 12 | Nil reviewers → all eligible | verify | 01-03 |
| 13 | Empty role → reviewer filter | registry | 01-04 |
| 14 | Empty role → skeptic excluded | registry | 01-04 |
| 15 | 1.x config (all empty) → all reviewers | registry | 01-04 |
| 16 | Deterministic ordering by name | verify | 01-05 |
| 17 | Duplicate reviewer names handled | verify | 01-05 |
| 18 | Multiple findings different results | verify | 01-05 |

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/registry/... ./internal/verify/...` — all pass
- [x] `go test -race ./internal/registry/... ./internal/verify/...` — no races
- [x] `go test -cover ./internal/registry/... ./internal/verify/...` — >= 95% on new code
- [x] `go vet ./internal/registry/... ./internal/verify/...` — clean
- [x] `go build ./...` — no import cycles
- [x] All 18 minimum test cases present and passing

**Story-Specific:**
- [x] Table-driven test pattern used consistently (matching existing registry test style)
- [x] Test fixtures are clear and self-documenting
- [x] Each sub-test has a descriptive name matching the scenario it validates
- [x] Test helper functions reduce duplication without hiding behavior

**Manual Review:**
- [x] Code reviewed and approved
- [x] Test names and table case names are readable without reading implementation
- [x] No test depends on map iteration order (ordering tested explicitly via sorting)
