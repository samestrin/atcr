# Acceptance Criteria: `atcr review --verify` Chaining

**Related User Story:** [04: CLI Command & MCP Tool](../user-stories/04-cli-command-mcp-tool.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Framework | Cobra (`github.com/spf13/cobra`) | Flag added to existing `reviewCmd` |
| Package | `cmd/atcr` | Modify `review.go`, add flag and chaining logic |
| Test Framework | `go test` + `testify/assert` | Table-driven tests for flag combinations |
| Key Dependencies | `internal/verify`, `internal/reconcile` | Pipeline stages called in sequence |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `cmd/atcr/review.go` - modify: add `--verify` flag and chaining logic
- [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) — defines `atcr review --verify` chaining behavior and exit-code semantics

- `cmd/atcr/review.go` - modify: add `--verify` bool flag, chaining logic after review and reconcile stages
- `cmd/atcr/review_test.go` - modify: add tests for `--verify` flag behavior and flag combinations
- `cmd/atcr/verify.go` - reference: shared verify logic called by both `atcr verify` and `atcr review --verify`

## Happy Path Scenarios

**Scenario 1: `atcr review --verify` chains all three stages**
- **Given** a valid diff input and a configured registry
- **When** `atcr review --verify <diff>` is executed
- **Then** the command runs review, then reconcile, then verify in sequence; produces all artifacts: review findings, reconciled findings, `verification.json`, re-emitted `findings.json` with verification blocks, `manifest.json` with `"verify"` stage, `summary.json` with `verdictCounts`

**Scenario 2: `--verify` implies `--reconcile`**
- **Given** a valid diff input
- **When** `atcr review --verify <diff>` is executed (without explicitly setting `--reconcile`)
- **Then** the reconcile stage runs automatically before verification, even though `--reconcile` was not explicitly passed

**Scenario 3: `--verify` and `--reconcile` both set explicitly**
- **Given** a valid diff input
- **When** `atcr review --verify --reconcile <diff>` is executed
- **Then** the reconcile stage runs exactly once (no double reconciliation), followed by the verify stage; output is identical to Scenario 2

**Scenario 4: `atcr review --reconcile` without `--verify` (backward compatibility)**
- **Given** a valid diff input
- **When** `atcr review --reconcile <diff>` is executed
- **Then** the command runs review then reconcile only; no verification stage; output matches pre-existing behavior

## Edge Cases

**Edge Case 1: `--verify` with `--fresh` or `--thorough` flags**
- **Given** a valid diff input
- **When** `atcr review --verify --fresh <diff>` is executed
- **Then** the verify stage receives `Fresh: true` in its options; the chaining works the same as without the extra flags

**Edge Case 2: Review stage produces zero findings**
- **Given** a diff that produces no review findings
- **When** `atcr review --verify <diff>` is executed
- **Then** the reconcile stage processes zero findings, the verify stage processes zero findings, and all artifacts are emitted with zero counts

**Edge Case 3: Verify stage is skipped when review fails**
- **Given** a diff input that causes the review stage to fail
- **When** `atcr review --verify <diff>` is executed
- **Then** the command exits with the review stage's error; reconcile and verify stages are not reached

## Error Conditions

**Error Scenario 1: Reconcile stage fails during chaining**
- **Given** a review stage that succeeds but produces malformed output
- **When** `atcr review --verify <diff>` is executed
- **Then** the reconcile stage error is propagated; verify stage is not reached; error message identifies the failing stage

**Error Scenario 2: Registry load failure during verify stage**
- **Given** a review and reconcile that succeed, but registry is missing
- **When** `atcr review --verify <diff>` is executed
- **Then** the verify stage exits with a registry load error; review and reconcile artifacts remain on disk

## Performance Requirements
- **Response Time:** Chaining overhead (stage transitions) adds < 100ms beyond individual stage durations
- **Throughput:** No redundant I/O; each stage reads the previous stage's output exactly once

## Security Considerations
- **Authentication/Authorization:** N/A — CLI runs with user's permissions
- **Input Validation:** `--verify` is a boolean flag; no injection risk. Flag interaction validated in Cobra `PreRunE` or `RunE`

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Fixture diffs for review input
- Expected review output feeding reconcile
- Expected reconcile output feeding verify

**Mock/Stub Requirements:**
- Mock review stage to produce deterministic findings
- Mock reconcile stage to produce deterministic reconciled findings
- Mock verify stage to verify it receives correct options and input
- Verify flag state (`--verify` implies `--reconcile`, both set, neither set)

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go vet ./cmd/atcr/...` clean
- [ ] `go build ./cmd/atcr/...` succeeds

**Story-Specific:**
- [ ] `--verify` flag defined on `reviewCmd` with default `false`
- [ ] `--verify` implies `--reconcile` when reconcile is not explicitly set
- [ ] Both flags set runs reconcile exactly once (no double reconcile)
- [ ] `--reconcile` without `--verify` remains unchanged (backward compatible)
- [ ] Artifacts from `atcr review --verify` match running stages separately

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Chaining order (review → reconcile → verify) is clear and documented in code comments
