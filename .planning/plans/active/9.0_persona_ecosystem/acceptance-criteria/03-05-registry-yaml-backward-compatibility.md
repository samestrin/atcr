# Acceptance Criteria: Registry YAML Backward Compatibility

**Related User Story:** [03: Language-Aware Skeptic Routing](../user-stories/03-language-aware-skeptic-routing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Config loading | Go package (`internal/registry`) | `yaml:"language,omitempty"` ensures omitted key loads cleanly |
| Test fixtures | YAML files in `personas/testdata/` or `internal/registry/testdata/` | Existing fixtures without `language` key |
| Test Framework | go test / testify | Load existing fixtures and assert behavior |
| Key Dependencies | `gopkg.in/yaml.v3`, `internal/registry/config.go` | |

## Related Files
- `internal/registry/config.go:267` - modify: `Language` field tagged `yaml:"language,omitempty"` ensures missing key in YAML unmarshals to nil without error
- `internal/registry/config_test.go` - modify: add regression test loading an existing fixture registry file that lacks `language` keys and asserting that `SelectEligibleSkeptics` output matches the pre-routing alphabetical baseline
- `personas/testdata/` - reference: existing YAML fixtures that do not contain `language` key; must continue to parse without errors after the struct change

## Happy Path Scenarios

**Scenario 1: Existing registry file without `language` key loads without error**
- **Given** a `registry.yaml` agent entry that has no `language` key (e.g., an existing built-in persona definition)
- **When** the registry is loaded via `loadRegistry` or equivalent
- **Then** the agent loads successfully, `AgentConfig.Language` is nil, and no error is returned

**Scenario 2: Skeptic selection from a legacy registry produces the same alphabetical result as before routing**
- **Given** a registry loaded from a fixture file with no `language` keys and a pool of skeptics `["charlie", "alpha", "bravo"]`
- **When** `SelectEligibleSkeptics` is called with any `finding.File` and `n=2`
- **Then** the returned slice is `["alpha", "bravo"]` — identical to pre-routing alphabetical behavior

**Scenario 3: Mixed registry (some agents with `language`, some without) loads cleanly**
- **Given** a `registry.yaml` containing both agents with `language: ["go"]` and agents without the `language` key
- **When** the registry loads
- **Then** all agents load without error; agents without `language` have `Language = nil`

## Edge Cases

**Edge Case 1: YAML `null` value for `language` key**
- **Given** a `registry.yaml` agent entry containing `language: null`
- **When** the registry loads
- **Then** `AgentConfig.Language` is nil and the agent loads without error

**Edge Case 2: YAML explicit empty list for `language` key**
- **Given** a `registry.yaml` agent entry containing `language: []`
- **When** the registry loads
- **Then** `AgentConfig.Language` is an empty (non-nil) slice; agent loads without error; behavior is identical to nil for routing purposes

**Edge Case 3: Registry file written before `Language` field was added to struct**
- **Given** a YAML file that was valid before the `AgentConfig.Language` field existed
- **When** `yaml.Unmarshal` processes the file with the updated struct
- **Then** `omitempty` ensures no error; the field is nil; the file remains valid

## Error Conditions

**Error Scenario 1: Unrelated YAML parse error is not masked by new field**
- **Given** a malformed `registry.yaml` (e.g., invalid indentation unrelated to `language`)
- **When** the registry loads
- **Then** the original YAML parse error is returned unchanged; the `Language` field addition does not suppress or alter unrelated errors

## Performance Requirements
- **Response Time:** Registry load time must not increase measurably from the addition of the `Language` field; YAML unmarshaling of a nil/absent field is a no-op.
- **Throughput:** No throughput impact; registry is loaded once at startup.

## Security Considerations
- **Input Validation:** `omitempty` prevents the field from being required in YAML; no change to the YAML parser's security posture. Validation of `Language` entries (rejecting control characters) applies only when the field is present and non-empty.
- **Authentication/Authorization:** No auth impact.

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** At least one existing fixture YAML file from `personas/testdata/` or `internal/registry/testdata/` that has no `language` key. A new fixture with a mixed registry (some agents with `language`, some without).
**Mock/Stub Requirements:** No mocks; load actual YAML files through the real registry loader. After loading, call `SelectEligibleSkeptics` with a controlled pool and assert alphabetical output matches the pre-routing baseline.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/registry/... ./internal/verify/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Regression test confirms existing fixture YAML files (without `language` key) load without error after `AgentConfig` struct change
- [ ] Regression test confirms `SelectEligibleSkeptics` output for a no-language-field registry matches pre-routing alphabetical baseline exactly
- [ ] Mixed registry (some with `language`, some without) loads cleanly and both agent types function correctly

**Manual Review:**
- [ ] Code reviewed and approved
