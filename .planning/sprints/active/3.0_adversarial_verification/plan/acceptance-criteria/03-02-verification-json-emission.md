# Acceptance Criteria: Verification JSON Emission

**Related User Story:** [03: Confidence v2 & Re-emit](../user-stories/03-confidence-v2-re-emit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Function | Go function in `internal/verify/emit_verification.go` | Writes JSON artifact |
| Test Framework | go test + testify | Round-trip write/read test |
| Key Dependencies | `encoding/json`, `os`, `time`, `path/filepath` | Standard library |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:145` - reference: `ReadReconciledFindings` (input loader pattern)

- `internal/verify/emit_verification.go` - create: `WriteVerification(reviewDir string, results []VerificationResult) error`, `VerificationResult` struct, `VerdictCounts` struct, atomic write helper
- `internal/verify/emit_verification_test.go` - create: round-trip and schema validation tests
- `internal/reconcile/emit.go` - modify: none (reference for `Verification` struct and `writeFileAtomic` pattern)

## Happy Path Scenarios

**Scenario 1: Write verification.json with valid results**
- **Given** a review directory with `reconciled/` subdir and 2 verification results (one `confirmed`, one `refuted`)
- **When** `WriteVerification(reviewDir, results)` is called
- **Then** `reconciled/verification.json` is created containing: `verifiedAt` (ISO 8601 UTC), `minSeverity`, `fresh`, `thorough`, `findings[]` array with per-finding verdict objects, and `verdictCounts` with `{confirmed: 1, refuted: 1, unverifiable: 0}`

**Scenario 2: Empty results list writes valid schema**
- **Given** a review directory with `reconciled/` subdir and an empty results slice
- **When** `WriteVerification(reviewDir, []VerificationResult{})` is called
- **Then** `reconciled/verification.json` is created with `findings: []` and `verdictCounts: {confirmed: 0, refuted: 0, unverifiable: 0}`

## Edge Cases

**Edge Case 1: Results with empty reasoning and no tripped budgets**
- **Given** a verification result with `Reasoning: ""` and `TrippedBudgets: nil`
- **When** `WriteVerification` serializes the result
- **Then** `reasoning` field is `""` and `trippedBudgets` field is `[]` (not null)

**Edge Case 2: Reconciled directory does not exist**
- **Given** a review directory without `reconciled/` subdir
- **When** `WriteVerification` is called
- **Then** the function creates the `reconciled/` directory before writing (or returns an error if creation fails)

## Error Conditions

**Error Scenario 1: Invalid review directory path**
- Error message: wrapped `os.PathError` from `os.MkdirAll` or temp file creation
- Behavior: Returns error, no partial file written

## Performance Requirements
- **Response Time:** < 100ms for up to 500 verification results
- **Throughput:** Single write per pipeline run

## Security Considerations
- **Input Validation:** Verdict values in results should be one of `confirmed`, `refuted`, `unverifiable`. Function does not reject invalid values (writer responsibility per emit.go:36 contract) but logs a warning if configured.
- **Atomic write:** Uses temp file + `os.Rename` pattern (matching `writeFileAtomic` in emit.go) to prevent partial writes.

## Test Implementation Guidance
**Test Type:** UNIT (with filesystem I/O via `t.TempDir()`)
**Test Data Requirements:** Sample `VerificationResult` structs covering all verdict types. Expected JSON schema.
**Mock/Stub Requirements:** None — uses real filesystem via `t.TempDir()`.

```go
func TestWriteVerification_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
    results := []VerificationResult{
        {File: "main.go", Line: 10, Problem: "nil deref", Verdict: "confirmed",
         Skeptic: "agent-b", Model: "gpt-4", Reasoning: "confirmed path", DurationMs: 500},
        {File: "util.go", Line: 42, Problem: "race", Verdict: "refuted",
         Skeptic: "agent-c", Model: "claude-3", Reasoning: "mutex held", DurationMs: 300},
    }
    require.NoError(t, WriteVerification(dir, results))

    // Read back and verify schema
    data, err := os.ReadFile(filepath.Join(dir, "reconciled", "verification.json"))
    require.NoError(t, err)
    var got VerificationFile
    require.NoError(t, json.Unmarshal(data, &got))
    assert.Equal(t, 2, len(got.Findings))
    assert.Equal(t, 1, got.VerdictCounts.Confirmed)
    assert.Equal(t, 1, got.VerdictCounts.Refuted)
    assert.Equal(t, 0, got.VerdictCounts.Unverifiable)
    assert.NotEmpty(t, got.VerifiedAt)
}
```

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...`)
- [x] No linting errors (`go vet ./internal/verify/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `WriteVerification` writes `reconciled/verification.json` atomically
- [x] `VerificationResult` struct contains all required fields (File, Line, Problem, Verdict, Skeptic, Model, Reasoning, DurationMs, TrippedBudgets)
- [x] `VerdictCounts` struct with Confirmed/Refuted/Unverifiable int fields
- [x] Round-trip test passes: write then read back, schema matches
- [x] `verdictCounts` equals the observed counts of confirmed/refuted/unverifiable entries in the results slice
- [x] `trippedBudgets` serializes as `[]` not `null` when empty

**Manual Review:**
- [x] Code reviewed and approved
