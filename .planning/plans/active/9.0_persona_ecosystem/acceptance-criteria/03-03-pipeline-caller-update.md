# Acceptance Criteria: Pipeline Caller Signature Update

**Related User Story:** [03: Language-Aware Skeptic Routing](../user-stories/03-language-aware-skeptic-routing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Production caller | Go package (`internal/verify`) | Update `pipeline.go:162` to pass scores map |
| Score map source | `map[string]float64` | Derived from corroboration metrics or nil when unavailable |
| Test Framework | go test / `go build` | Build-green verification |
| Key Dependencies | `internal/verify/select.go`, `internal/registry` | |

## Related Files
- `internal/verify/pipeline.go:162` - modify: update the single call-site of `SelectEligibleSkeptics` to pass a corroboration scores map (or `nil` when scores are not yet available in this pipeline phase)
- `internal/verify/select.go` - modify: updated function signature (4th `scores` parameter) that `pipeline.go` must satisfy
- `internal/verify/pipeline_test.go` - modify: verify the pipeline integration compiles and routes correctly with the new signature

## Happy Path Scenarios

**Scenario 1: Pipeline passes a populated scores map**
- **Given** the pipeline has corroboration scores available as `map[string]float64{"idiomatic": 0.85, "sentinel": 0.72}`
- **When** `SelectEligibleSkeptics` is called at `pipeline.go:162`
- **Then** the call compiles, the scores are forwarded, and language-matched skeptics with higher scores are preferred

**Scenario 2: Pipeline passes nil when scores are unavailable**
- **Given** corroboration scores are not yet computed at the point of the `SelectEligibleSkeptics` call
- **When** `SelectEligibleSkeptics` is called with `scores = nil`
- **Then** the call compiles cleanly, nil is handled safely inside `SelectEligibleSkeptics`, and alphabetical ordering within the matched partition is used

**Scenario 3: `go build ./...` succeeds after the update**
- **Given** the signature of `SelectEligibleSkeptics` has been updated and `pipeline.go:162` has been updated to match
- **When** `go build ./...` is run
- **Then** the build exits with code 0 and no compiler errors

## Edge Cases

**Edge Case 1: No additional callers exist**
- **Given** the task assumption that `pipeline.go:162` is the only production caller
- **When** `grep -r "SelectEligibleSkeptics" ./internal` is run before implementation
- **Then** only one call-site is found; if additional callers are discovered, they must be updated in the same commit

**Edge Case 2: Scores map contains entries for skeptics not in the current pool**
- **Given** `scores = {"unknown-persona": 0.99}` and the pool does not include `"unknown-persona"`
- **When** `SelectEligibleSkeptics` uses the scores map
- **Then** the extra entry is ignored silently; no panic or error

## Error Conditions

**Error Scenario 1: Missing scores argument at call-site (compile-time)**
- Error message: Go compiler error — `"not enough arguments in call to SelectEligibleSkeptics"`
- HTTP status / error code: non-zero `go build` exit

**Error Scenario 2: Wrong argument type passed for scores (compile-time)**
- Error message: Go compiler error — `"cannot use X as type map[string]float64"`
- HTTP status / error code: non-zero `go build` exit

## Performance Requirements
- **Response Time:** The update at `pipeline.go:162` is a call-site change only; no new computation is added at the pipeline level beyond what is already present for building the scores map.
- **Throughput:** No throughput impact; this is a single call-site change.

## Security Considerations
- **Input Validation:** The scores map originates from internal corroboration metrics, not from external user input. No sanitization required at the call-site.
- **Authentication/Authorization:** No auth impact.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** A minimal pipeline test that invokes the updated call-site with both a populated scores map and a nil scores map, asserting that the returned skeptic slice is non-nil and length-bounded by `n`.
**Mock/Stub Requirements:** Mock or stub the registry and finding inputs; use a lightweight `AgentConfig` set with at least one entry having a `Language` field set.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`) — zero compiler errors

**Story-Specific:**
- [ ] `internal/verify/pipeline.go:162` passes a scores map (or `nil`) as the 4th argument to `SelectEligibleSkeptics`
- [ ] No other callers of `SelectEligibleSkeptics` exist with the old 3-argument signature (verified by grep before and after)

**Manual Review:**
- [ ] Code reviewed and approved
