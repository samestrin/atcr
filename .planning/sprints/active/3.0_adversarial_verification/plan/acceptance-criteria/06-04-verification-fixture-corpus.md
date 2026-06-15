# Acceptance Criteria: Verification Fixture Corpus

**Related User Story:** [[06]: Report Updates & Documentation](../user-stories/06-report-updates-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test Fixtures | JSON and text files | Static, self-contained; no network calls or LLM invocations |
| Schema Validation | `go test` + `reconcile.JSONFinding` unmarshal | Fixtures must parse as valid `reconcile.JSONFinding` |
| Key Dependencies | `internal/reconcile` (JSONFinding struct) | Schema source for fixture validation |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:59` - reference: `JSONFinding` struct (schema for fixtures)

- `internal/verify/testdata/true-finding.json` - create: planted true finding (realistic, parseable as `reconcile.JSONFinding`)
- `internal/verify/testdata/false-finding.json` - create: planted false finding (plausible but incorrect, parseable as `reconcile.JSONFinding`)
- `internal/verify/testdata/malformed-response.txt` - create: non-parseable skeptic response text (invalid JSON)
- `internal/verify/` - create: directory for verification test data
- `internal/verify/verify_e2e_test.go` - create: end-to-end test driving the planted true/false fixtures through the pipeline (scripted mock skeptic → invoke → aggregate → confidence v2) asserting confirmed/refuted
- `internal/reconcile/emit.go` - read: `JSONFinding` struct definition for schema compliance

## Happy Path Scenarios
**Scenario 1: true-finding.json is parseable as reconcile.JSONFinding**
- **Given** the fixture file `internal/verify/testdata/true-finding.json`
- **When** it is unmarshaled via `json.Unmarshal` into `reconcile.JSONFinding`
- **Then** no error occurs, and all required fields are populated: severity (valid enum), file (non-empty path), line (> 0), problem (non-empty text)

**Scenario 2: false-finding.json is parseable as reconcile.JSONFinding**
- **Given** the fixture file `internal/verify/testdata/false-finding.json`
- **When** it is unmarshaled via `json.Unmarshal` into `reconcile.JSONFinding`
- **Then** no error occurs, and all required fields are populated with the same constraints as Scenario 1

**Scenario 3: malformed-response.txt is non-empty invalid JSON**
- **Given** the fixture file `internal/verify/testdata/malformed-response.txt`
- **When** its content is read and inspected
- **Then** the file is non-empty, contains text resembling a skeptic response (e.g., partial JSON with verdict/reasoning fields), and fails `json.Unmarshal` into a valid verdict struct

**Scenario 4: true-finding.json describes a realistic code issue**
- **Given** the content of `true-finding.json`
- **When** a developer reads it
- **Then** it describes a plausible, correct finding (e.g., "JWT signature not verified before claims are read") pointing to a realistic code pattern with appropriate severity, category, and reviewer attribution

**Scenario 5: false-finding.json describes a plausible but incorrect finding**
- **Given** the content of `false-finding.json`
- **When** a developer reads it
- **Then** it describes a plausible but deliberately incorrect finding (e.g., "nil pointer dereference on line 42" where the code actually checks for nil) — useful for testing that skeptics produce a `refuted` verdict for the false finding in end-to-end tests

**Scenario 6: End-to-end — planted false finding is refuted, planted true finding is confirmed**
- **Given** the fixtures `true-finding.json` and `false-finding.json`, and a scripted mock skeptic (`fakeChatCompleter`) configured to return a `confirmed` verdict for the true finding and a `refuted` verdict for the false finding
- **When** each finding is driven through the verification pipeline end-to-end (`invokeSkeptic` → `aggregateVerdicts` → `confidenceV2`) with the mock skeptic
- **Then** the true finding yields verdict `confirmed` and confidence `VERIFIED`; the false finding yields verdict `refuted` and confidence `LOW` (demoted, retained). This is the executing test for the "deliberately false finding gets refuted / true finding gets confirmed" success criterion — `06-04` no longer only validates that the fixtures parse.

## Edge Cases
**Edge Case 1: Fixture files are self-contained**
- **Given** any fixture file in `internal/verify/testdata/`
- **When** it is read in isolation
- **Then** it requires no external files, network calls, or environment variables to be valid

**Edge Case 2: Fixture schema compliance with future fields**
- **Given** the fixture JSON files
- **When** the `reconcile.JSONFinding` struct gains new optional fields in a future epic
- **Then** the fixtures remain valid (they use `omitempty`-compatible fields and do not set unknown keys)

**Edge Case 3: malformed-response.txt resembles a real skeptic response**
- **Given** the content of `malformed-response.txt`
- **When** inspected
- **Then** it contains text that looks like it could be a model response (has fields like "verdict" or "reasoning") but is structurally invalid JSON (e.g., missing closing brace, unescaped quotes, trailing comma)

## Error Conditions
**Error Scenario 1: Fixture fails schema validation**
- Error message: `json: cannot unmarshal ...` (standard Go JSON unmarshal error)
- Behavior: If a fixture file fails to parse as `reconcile.JSONFinding`, the test that depends on it must fail with a clear message naming the fixture file and the parse error

## Performance Requirements
- **N/A:** Fixture files are static and small (< 1 KB each); no performance requirements

## Security Considerations
- **No secrets:** Fixture files contain no API keys, credentials, or real code snippets from production systems
- **No executable content:** JSON and text files only; no scripts or binaries
- **Realistic but synthetic:** Findings reference plausible but fictional file paths and code patterns (not real codebase paths that could leak internal structure)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** The fixture files themselves are the test data; no additional data needed
**Mock/Stub Requirements:** None — fixtures are validated directly against `reconcile.JSONFinding` schema

Validation tests:
```go
func TestFixture_TrueFindingParses(t *testing.T) {
    data, err := os.ReadFile("testdata/true-finding.json")
    require.NoError(t, err)
    var f reconcile.JSONFinding
    require.NoError(t, json.Unmarshal(data, &f))
    assert.NotEmpty(t, f.File)
    assert.NotEmpty(t, f.Problem)
    assert.NotEmpty(t, f.Severity)
}
```

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...` or equivalent)
- [x] No linting errors (`go vet ./...`)
- [x] Build succeeds (`go build ./...`)
- [x] Fixture files parse without errors

**Story-Specific:**
- [x] `internal/verify/testdata/true-finding.json` exists and parses as `reconcile.JSONFinding` with all required fields populated
- [x] `internal/verify/testdata/false-finding.json` exists and parses as `reconcile.JSONFinding` with all required fields populated
- [x] `internal/verify/testdata/malformed-response.txt` exists, is non-empty, and contains invalid JSON resembling a skeptic response
- [x] Fixtures are self-contained (no external dependencies)
- [x] `true-finding.json` describes a realistic correct finding; `false-finding.json` describes a plausible but incorrect finding
- [x] End-to-end test (`verify_e2e_test.go`) drives both planted fixtures through `invokeSkeptic` → `aggregateVerdicts` → `confidenceV2` with a scripted mock skeptic and asserts: true → `confirmed`/`VERIFIED`, false → `refuted`/`LOW`

**Manual Review:**
- [x] Code reviewed and approved
- [x] Fixture content reviewed for realism and appropriateness
- [x] No secrets or real codebase paths in fixture files
