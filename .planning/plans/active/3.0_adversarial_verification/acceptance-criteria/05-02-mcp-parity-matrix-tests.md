# Acceptance Criteria: MCP Handler Parity and Fixture Matrix Tests

**Related User Story:** [05: Gate Semantics](../user-stories/05-gate-semantics.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP Handler | Go function modification in `internal/mcp/handlers.go` | `failingFindings` (line 339) updated with same filter logic |
| MCP Tool Args | Go struct in `internal/mcp/tools.go` or handler args | `require_verified` field added to ReconcileArgs |
| Test Framework | go test + testify | Shared fixture tests for CLI and MCP paths |
| Key Dependencies | `internal/reconcile` (CountAtOrAbove/CountFailing, AtOrAbove, JSONFinding, Verification) | Shared gate function called by both paths |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/reconcile/gate.go:57` - reference: `CountAtOrAbove` (shared gate function)
- `internal/mcp/handlers.go:339` - reference: `failingFindings` (MCP parity target)
- [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) — defines gate semantics that must be identical across CLI and MCP

- `internal/mcp/handlers.go` - modify: `failingFindings` (line 339) and `handleReconcile` to apply refuted/requireVerified filtering
- `internal/mcp/handlers_test.go` - modify: add MCP-specific gate tests with verification fixtures
- `internal/mcp/tools.go` - modify: add `require_verified` field to ReconcileArgs struct (if separate)
- `internal/reconcile/gate_test.go` - modify: shared matrix test fixtures reusable by MCP tests
- `internal/reconcile/gate.go` - reference: shared gate function both paths call

## Happy Path Scenarios

**Scenario 1: MCP `atcr_reconcile` excludes refuted findings from failing list**
- **Given** A reconciled review with findings: (A) `Severity: "HIGH"`, `Verification.Verdict: "refuted"`; (B) `Severity: "HIGH"`, `Verification.Verdict: "unverifiable"`
- **When** MCP `atcr_reconcile` is called with `fail_on: "HIGH"` (no `require_verified`)
- **Then** `Pass: false`, `Findings` contains only finding B (refuted finding A excluded)

**Scenario 2: MCP `atcr_reconcile` with `require_verified` counts only VERIFIED findings**
- **Given** Same findings as Scenario 1, plus (C) `Severity: "HIGH"`, `Verification.Verdict: "confirmed"`
- **When** MCP `atcr_reconcile` is called with `fail_on: "HIGH"`, `require_verified: true`
- **Then** `Pass: false`, `Findings` contains only finding C (only VERIFIED at threshold)

**Scenario 3: MCP and CLI produce identical gate results for same input**
- **Given** A fixture with 5 findings at mixed verdicts and severities
- **When** Both CLI `atcr reconcile --fail-on HIGH` and MCP `atcr_reconcile(fail_on: "HIGH")` are run
- **Then** Both produce the same pass/fail verdict and the same failing findings list

**Scenario 4: MCP `atcr_reconcile` with `require_verified: true` and no VERIFIED findings passes gate**
- **Given** 3 findings: 2 refuted, 1 unverifiable, none confirmed
- **When** MCP `atcr_reconcile` called with `fail_on: "HIGH"`, `require_verified: true`
- **Then** `Pass: true`, `Findings: []` (no VERIFIED findings to trigger failure)

## Edge Cases

**Edge Case 1: MCP finding with nil Verification**
- **Given** A finding with `Severity: "HIGH"`, `Verification: nil` (v1 finding, no adversarial verification run)
- **When** MCP `atcr_reconcile` called with `fail_on: "HIGH"` (no `require_verified`)
- **Then** The finding IS counted as failing (nil Verification = not refuted, backward compatible)

**Edge Case 2: MCP `require_verified` with nil Verification findings**
- **Given** Same finding as Edge Case 1
- **When** MCP `atcr_reconcile` called with `fail_on: "HIGH"`, `require_verified: true`
- **Then** The finding is NOT counted (nil Verification is not VERIFIED)

**Edge Case 3: Empty findings list**
- **Given** A reconciled review with 0 findings
- **When** MCP `atcr_reconcile` called with `fail_on: "HIGH"`, `require_verified: true`
- **Then** `Pass: true`, `Findings: []`, `TotalFindings: 0`

## Error Conditions

**Error Scenario 1: Invalid `fail_on` with `require_verified`**
- Error message: `invalid severity threshold: "BLOCKER" (must be CRITICAL, HIGH, MEDIUM, or LOW)`
- Behavior: MCP returns error result; `require_verified` does not affect threshold validation order

**Error Scenario 2: MCP `require_verified` field type mismatch**
- Error: JSON schema validation rejects non-boolean `require_verified` values before handler invocation
- Behavior: MCP tool returns structured error (type mismatch), handler never called

## Performance Requirements
- **Response Time:** `failingFindings` filter adds < 5 microseconds for 500 findings (single-pass)
- **Throughput:** N/A (called once per MCP reconcile request)

## Security Considerations
- **Input validation:** `require_verified` is a boolean in MCP tool args schema; no injection risk
- **Path containment:** Existing `resolveReviewDir` path validation is unaffected by the new flag
- **Consistency guarantee:** Both CLI and MCP paths MUST call the same gate function. Divergence would create inconsistent CI behavior — mitigated by shared `CountAtOrAbove` / `CountFailing` function.

## Test Implementation Guidance
**Test Type:** INTEGRATION (MCP handler) + UNIT (shared gate function)
**Test Data Requirements:**
- Shared fixture: `[]reconcile.Merged` or `reconcile.Result` with mixed verdicts/severities
- Fixture for scenario in story: 3 findings (v1=HIGH+refuted, v1=MEDIUM+confirmed, v1=HIGH+unverifiable) with `--fail-on high` → count=1 without `require_verified`, count=0 with `require_verified`

**Mock/Stub Requirements:**
- MCP handler tests: use in-memory `engine` with temp directory containing fixture findings.json
- Shared gate function tests: none (pure function)

```go
// Shared fixture test (reused by both CLI and MCP tests)
func TestGateMatrix_StoryFixture(t *testing.T) {
    findings := []Merged{
        // v1=HIGH + refuted → confidence=LOW
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "LOW"},
         Verification: &Verification{Verdict: "refuted"}},
        // v1=MEDIUM + confirmed → confidence=VERIFIED
        {Finding: stream.Finding{Severity: "MEDIUM", Confidence: "VERIFIED"},
         Verification: &Verification{Verdict: "confirmed"}},
        // v1=HIGH + unverifiable → confidence stays
        {Finding: stream.Finding{Severity: "HIGH", Confidence: "MEDIUM"},
         Verification: &Verification{Verdict: "unverifiable"}},
    }

    // --fail-on high, no --require-verified → count=1 (unverifiable only; refuted excluded)
    assert.Equal(t, 1, CountFailing(findings, "HIGH", false))

    // --fail-on high, --require-verified → count=0 (no VERIFIED at HIGH)
    assert.Equal(t, 0, CountFailing(findings, "HIGH", true))
}

// MCP handler test
func TestHandleReconcile_RequireVerified(t *testing.T) {
    // Setup engine with temp dir containing fixture findings.json
    // Call handleReconcile with RequireVerified: true, FailOn: "HIGH"
    // Assert Pass: true (no VERIFIED findings at HIGH)
}
```

**Matrix test coverage requirement:** >= 12 distinct scenarios:
- 3 verdicts (confirmed/refuted/unverifiable) × 3 severities (HIGH/MEDIUM/LOW) × 2 flag states (on/off) = 18
- Plus: nil verification, empty verdict, out-of-scope, empty findings

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/mcp/... ./internal/reconcile/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `failingFindings` at `internal/mcp/handlers.go:339` applies refuted exclusion and `requireVerified` filtering
- [ ] MCP `atcr_reconcile` accepts `require_verified` parameter (boolean, default false)
- [ ] MCP and CLI paths call the same gate function (no divergence)
- [ ] Fixture matrix tests cover >= 12 scenarios (verdict × severity × flag state)
- [ ] Story fixture test passes: 3 findings (HIGH+refuted, MEDIUM+confirmed, HIGH+unverifiable) → count=1 without `require_verified`, count=0 with `require_verified` at `--fail-on high`
- [ ] Test coverage >= 95% on gate logic (`go test -cover ./internal/reconcile/... ./internal/mcp/...`)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] MCP handler gate logic reviewed for parity with CLI path
- [ ] Matrix test completeness verified (all verdict × severity × flag combinations covered)
