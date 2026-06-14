# Acceptance Criteria: Findings Re-Emit with Verification Blocks

**Related User Story:** [03: Confidence v2 & Re-emit](../user-stories/03-confidence-v2-re-emit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Function | Go function in `internal/verify/emit_findings.go` | Loads, modifies, re-writes findings.json |
| Test Framework | go test + testify | Table-driven with temp dir |
| Key Dependencies | `internal/reconcile` (ReadReconciledFindings, JSONFinding, Verification), `encoding/json` | Must not create import cycle |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/emit.go:145` - reference: `ReadReconciledFindings` (input loader)

- `internal/verify/emit_findings.go` - create: `ReEmitFindings(reviewDir string, verdicts map[FindingKey]*reconcile.Verification) error`, `FindingKey` struct
- `internal/verify/emit_findings_test.go` - create: re-emit tests with verification block validation
- `internal/reconcile/emit.go` - modify: none (reference for `JSONFinding`, `Verification`, `ReadReconciledFindings`, `writeFileAtomic`)

## Happy Path Scenarios

**Scenario 1: Re-emit with confirmed verdicts updates confidence and populates verification**
- **Given** a `reconciled/findings.json` with 2 findings (v1 confidence `HIGH`), and a verdict map with `FindingKey{file, line, problem}` -> `&Verification{Verdict: "confirmed", Skeptic: "agent-b", Notes: "path valid"}`
- **When** `ReEmitFindings(reviewDir, verdicts)` is called
- **Then** the re-emitted `findings.json` has both findings with `Confidence: "VERIFIED"` and `Verification` field populated with the verdict data

**Scenario 2: Re-emit with refuted verdict demotes to LOW but retains finding**
- **Given** a `reconciled/findings.json` with a finding at v1 confidence `HIGH` and a verdict map entry with `Verdict: "refuted"`
- **When** `ReEmitFindings` is called
- **Then** the re-emitted finding has `Confidence: "LOW"`, `Verification.Verdict: "refuted"`, and the finding is still present in the file (not deleted)

**Scenario 3: Findings without verdicts remain unchanged**
- **Given** a `reconciled/findings.json` with 3 findings and a verdict map covering only 1
- **When** `ReEmitFindings` is called
- **Then** the 2 unmatched findings retain their v1 confidence and `Verification` remains nil (omitempty excludes the field)

## Edge Cases

**Edge Case 1: Empty verdict map (no skeptics ran)**
- **Given** a valid `reconciled/findings.json` and an empty verdict map
- **When** `ReEmitFindings` is called
- **Then** findings.json is re-written unchanged (all findings retain v1 confidence, no verification blocks)

**Edge Case 2: FindingKey collision — same file+line+problem for distinct findings**
- **Given** two findings with identical file, line, and problem text
- **When** `ReEmitFindings` matches verdicts by `FindingKey`
- **Then** both findings receive the same verdict (documented assumption: reconciler deduplicates before this stage)

**Edge Case 3: Findings file does not exist**
- **Given** a review directory without `reconciled/findings.json`
- **When** `ReEmitFindings` is called
- **Then** Returns `os.ErrNotExist` (propagated from `ReadReconciledFindings`)

## Error Conditions

**Error Scenario 1: Malformed findings.json**
- Error message: `"parsing reconciled findings: ..."` (from `ReadReconciledFindings`)
- Behavior: Returns error, no file written

**Error Scenario 2: Findings.json is empty**
- Error message: `"reconciled findings.json is empty"` (from `ReadReconciledFindings`)
- Behavior: Returns error, no file written

## Performance Requirements
- **Response Time:** < 50ms for up to 500 findings
- **Throughput:** Single re-emit per pipeline run

## Security Considerations
- **Input Validation:** Verdict values are validated by the caller (Story 2). This function trusts the `*Verification` values it receives.
- **Atomic write:** Uses temp file + `os.Rename` pattern to prevent partial writes on crash.
- **No injection:** Finding text fields (problem, reasoning) are serialized via `json.Marshal` which handles escaping.

## Test Implementation Guidance
**Test Type:** UNIT (with filesystem I/O via `t.TempDir()`)
**Test Data Requirements:** Sample `findings.json` files with various v1 confidence levels. Verdict maps covering subsets of findings.
**Mock/Stub Requirements:** None — uses real `ReadReconciledFindings` and filesystem.

```go
func TestReEmitFindings_RefutedDemoted(t *testing.T) {
    dir := t.TempDir()
    reconDir := filepath.Join(dir, "reconciled")
    require.NoError(t, os.MkdirAll(reconDir, 0o755))

    // Write initial findings.json
    findings := []reconcile.JSONFinding{
        {Severity: "HIGH", File: "main.go", Line: 10, Problem: "nil deref",
         Confidence: "HIGH", Reviewers: []string{"agent-a"}},
    }
    writeFindings(t, reconDir, findings)

    key := FindingKey{File: "main.go", Line: 10, Problem: "nil deref"}
    verdicts := map[FindingKey]*reconcile.Verification{
        key: {Verdict: "refuted", Skeptic: "agent-b", Notes: "false positive"},
    }
    require.NoError(t, ReEmitFindings(dir, verdicts))

    updated, err := reconcile.ReadReconciledFindings(dir)
    require.NoError(t, err)
    require.Len(t, updated, 1)
    assert.Equal(t, "LOW", updated[0].Confidence)
    assert.NotNil(t, updated[0].Verification)
    assert.Equal(t, "refuted", updated[0].Verification.Verdict)
}
```

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors (`go vet ./internal/verify/...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] No import cycle (`go build ./internal/verify/... ./internal/reconcile/...`)

**Story-Specific:**
- [ ] `ReEmitFindings` loads findings, applies verdicts, recomputes confidence, writes atomically
- [ ] `FindingKey` struct defined with File/Line/Problem fields
- [ ] Refuted findings demoted to LOW but retained in file
- [ ] Findings without verdicts remain unchanged (Verification=nil, confidence unchanged)
- [ ] Empty verdict map produces no changes

**Manual Review:**
- [ ] Code reviewed and approved
