# Acceptance Criteria: Sort Ordering

**Related User Story:** [05: Corroboration Feedback via Persona Scores](../user-stories/05-corroboration-feedback.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Sort logic | Go stdlib `sort` or `slices` package | `sort.Slice` over joined `[]scoredPersona` struct |
| Joined type | `internal/personas` package (local struct) | `scoredPersona{PersonaMeta, rate *float64}` — nil pointer = n/a |
| Test Framework | `go test` / `testify` | Deterministic ordering assertions |
| Key Dependencies | `sort` (stdlib), `strings` (stdlib) | No new external deps |

## Related Files
- `internal/personas/list.go` - modify: after building the joined slice, apply `sort.Slice`: numeric rates descending by value, then `n/a` entries ascending by `strings.ToLower(name)`
- `internal/personas/list_test.go` - modify: add `TestPersonasListScoresSortOrder` with a fixture covering descending numeric order and alphabetical `n/a` tail

### Related Files (from codebase-discovery.json)

- `internal/personas/list.go` — modify: sort logic for `--scores` output
- `internal/personas/list_test.go` — modify: add sort-order tests

## Happy Path Scenarios

**Scenario 1: Mixed numeric and n/a rows sort by rate descending then alphabetically**
- **Given** four personas installed with rates: `sentinel=0.72`, `tracer=n/a`, `idiomatic=0.50`, `guardian=n/a`
- **When** the user runs `atcr personas list --scores`
- **Then** the output rows appear in this order: `sentinel (72.0%)`, `idiomatic (50.0%)`, `guardian (n/a)`, `tracer (n/a)` — numeric rows descending, then `n/a` rows alphabetically

**Scenario 2: All personas have numeric rates**
- **Given** all installed personas have at least one scorecard entry
- **When** the user runs `atcr personas list --scores`
- **Then** rows are sorted by corroboration rate descending; ties are broken alphabetically by name ascending

**Scenario 3: All personas show n/a**
- **Given** no scorecard data exists for any installed persona
- **When** the user runs `atcr personas list --scores`
- **Then** all rows show `n/a` and are sorted alphabetically by persona name ascending

## Edge Cases

**Edge Case 1: Two personas with identical rates**
- **Given** `sentinel` and `idiomatic` both have a `CorroborationRate` of `0.60`
- **When** the user runs `atcr personas list --scores`
- **Then** `idiomatic` appears before `sentinel` (alphabetical tiebreak, ascending)

**Edge Case 2: Single persona installed**
- **Given** only one persona (`sentinel`) is installed
- **When** the user runs `atcr personas list --scores`
- **Then** the single row is displayed; no sort error occurs; exit code is 0

**Edge Case 3: Rate of exactly 1.0 (100%)**
- **Given** `sentinel` has a `CorroborationRate` of `1.0`
- **When** the user runs `atcr personas list --scores`
- **Then** `sentinel` appears first with `100.0%` and sorts above all lower-rate personas

## Error Conditions

**Error Scenario 1: Sort is applied after a failed Aggregate call**
- If `scorecard.Aggregate()` errors, the sort is never reached; the command exits 1 before any table is rendered (covered by AC 05-02 error conditions — no additional sort-specific error path)

## Performance Requirements
- **Response Time:** Sort of up to 100 joined persona rows completes in under 1 ms (in-memory `sort.Slice` on small slice)
- **Throughput:** N/A — single interactive CLI invocation

## Security Considerations
- **Authentication/Authorization:** Sort operates on already-loaded in-memory data; no additional I/O or privilege concerns
- **Input Validation:** Rate values sourced from `scorecard.Aggregate()` which already parses JSONL; no additional validation needed in sort path

## Test Implementation Guidance
**Test Type:** UNIT  
**Test Data Requirements:**  
- Fixture slice of 4+ `scoredPersona` entries with mixed numeric rates and `n/a` values  
- Tie fixture: two entries with identical rates to verify alphabetical tiebreak  
- All-`n/a` fixture to verify purely alphabetical output  

**Mock/Stub Requirements:** No mocks needed for sort logic; test operates directly on the `sortScoredPersonas([]scoredPersona)` helper function (or equivalent exported/unexported sort function)

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `TestPersonasListScoresSortOrder` passes: numeric rows descending, `n/a` rows alphabetical tail
- [ ] Tie-breaking test passes: identical rates produce alphabetical ordering
- [ ] All-`n/a` fixture sorts alphabetically (no panic or empty output)

**Manual Review:**
- [ ] Code reviewed and approved
