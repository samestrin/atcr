# Acceptance Criteria: Skip Already-Verified Findings Unless `--fresh`

**Related User Story:** [04: CLI Command & MCP Tool](../user-stories/04-cli-command-mcp-tool.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Pipeline Orchestration | Go logic in `internal/verify` | Reads existing `reconciled/findings.json` verification blocks before invoking skeptics |
| CLI Flag | Cobra bool flag `--fresh` | Already defined in AC 04-01; this AC defines the behavior it controls |
| Test Framework | `go test` + `testify` | Table-driven tests with pre-populated verification blocks |
| Key Dependencies | `internal/reconcile` (JSONFinding.Verification), `internal/verify` (Verify options) | Existing verification block reused |

### Related Files

- `internal/verify/pipeline.go` — top-level `Verify` orchestration
- `internal/verify/emit_findings.go` — `ReEmitFindings` writes verification blocks
- `cmd/atcr/verify.go` — `--fresh` flag definition

## Happy Path Scenarios

**Scenario 1: Confirmed findings are skipped on re-run**
- **Given** a review directory whose `reconciled/findings.json` contains a finding with `Verification.Verdict: "confirmed"`
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** no skeptic is invoked for that finding; the existing verdict and confidence are preserved

**Scenario 2: Refuted findings are skipped on re-run**
- **Given** a review directory whose `reconciled/findings.json` contains a finding with `Verification.Verdict: "refuted"`
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** no skeptic is invoked; the refuted verdict, LOW confidence, and skeptic reasoning are preserved

**Scenario 3: `--fresh` re-verifies all findings**
- **Given** the same review directory as Scenario 1
- **When** `atcr verify <dir> --fresh` runs
- **Then** skeptics are invoked for the confirmed finding as if it had no prior verdict

**Scenario 4: New findings added after a previous verify run**
- **Given** a review directory with a previous `verification.json`; the reconciler added one new finding and preserved one existing confirmed finding
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** only the new finding is verified; the existing confirmed finding is skipped

## Edge Cases

**Edge Case 1: Unverifiable findings are also skipped**
- **Given** a finding with `Verification.Verdict: "unverifiable"`
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** the finding is skipped; no skeptic is re-invoked

**Edge Case 2: Finding with empty or missing verdict is always verified**
- **Given** a finding with `Verification: nil` or `Verification.Verdict: ""`
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** the finding is treated as unverified and skeptics are invoked

**Edge Case 3: Mixed findings with and without prior verdicts**
- **Given** 3 findings: one confirmed, one refuted, one with no verification block
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** only the finding without a verification block is processed by skeptics

## Error Conditions

**Error Scenario 1: Corrupt verification block prevents skip optimization**
- **Given** a finding with a verification block containing an unknown verdict value
- **When** `atcr verify <dir>` runs without `--fresh`
- **Then** the finding is treated as unverified and re-processed (unknown verdict is not trusted as a cached result)

## Performance Requirements
- **Response Time:** Skip decision is O(1) per finding; no LLM calls for skipped findings
- **Throughput:** Re-running verify on an already-verified review completes in < 1s (excluding I/O)

## Security Considerations
- **Input Validation:** Existing verification blocks are read from `findings.json` produced by the same tool; verdict values validated before trust
- **No data loss:** `--fresh` re-invokes skeptics but still writes new artifacts atomically

## Test Implementation Guidance
**Test Type:** UNIT / INTEGRATION
**Test Data Requirements:**
- `findings.json` fixtures with pre-populated verification blocks
- Mock skeptic invocations to assert they are not called for skipped findings

```go
func TestVerify_SkipsAlreadyVerified(t *testing.T) {
    findings := []reconcile.JSONFinding{
        {File: "a.go", Line: 1, Problem: "x", Verification: &reconcile.Verification{Verdict: "confirmed"}},
        {File: "b.go", Line: 2, Problem: "y", Verification: &reconcile.Verification{Verdict: "refuted"}},
        {File: "c.go", Line: 3, Problem: "z"},
    }
    // Assert invokeSkeptic called exactly once (for c.go) when Fresh=false
}
```

## Definition of Done
**Auto-Verified:**
- [x] `go test ./internal/verify/... ./cmd/atcr/...` passes
- [x] `go vet ./...` clean
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] Findings with `Verification.Verdict` of `confirmed`, `refuted`, or `unverifiable` are skipped by default
- [x] `--fresh` forces re-verification of all findings
- [x] Findings without a verdict (`nil` or empty) are always verified
- [x] Unknown verdict values are not trusted and trigger re-verification
- [x] `verification.json` and `findings.json` are still re-emitted on every run

**Manual Review:**
- [x] Code reviewed and approved
