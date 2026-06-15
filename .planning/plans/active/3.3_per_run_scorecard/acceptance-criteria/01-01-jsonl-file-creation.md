# Acceptance Criteria: JSONL File Creation and Update

**Related User Story:** [01: Auto-emit Scorecard](../user-stories/01-auto-emit-scorecard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Scorecard Store | Go Package (`internal/scorecard`) | Append-mode JSONL writer |
| File Paths | Go `os.UserConfigDir` + `atcr/scorecard/` | Cross-platform config dir |
| Test Framework | `go test` + `testify` | Unit + integration |

## Related Files
- `internal/scorecard/store.go` - create: JSONL append writer with dir creation
- `internal/scorecard/paths.go` - create: monthly file path resolution (`YYYY-MM.jsonl`)
- `cmd/atcr/reconcile.go` - modify: hook scorecard.Emit after reconcile completion

## Happy Path Scenarios
**Scenario 1: First run creates directory and file**
- **Given** `~/.config/atcr/scorecard/` does not exist
- **When** `atcr reconcile` completes successfully
- **Then** directory `~/.config/atcr/scorecard/` is created and file `YYYY-MM.jsonl` contains one JSON object per line

**Scenario 2: Subsequent run appends to existing file**
- **Given** `~/.config/atcr/scorecard/2026-06.jsonl` already exists with prior records
- **When** `atcr reconcile` completes in June 2026
- **Then** new records are appended to the same file without modifying existing lines

**Scenario 3: Month boundary creates new file**
- **Given** `2026-06.jsonl` exists from prior month
- **When** reconcile runs on 2026-07-01
- **Then** new file `2026-07.jsonl` is created; `2026-06.jsonl` is untouched

## Edge Cases
**Edge Case 1: Config directory parent missing**
- **Given** `~/.config/atcr/` does not exist
- **When** scorecard write is triggered
- **Then** `os.MkdirAll` creates full path hierarchy; write succeeds

**Edge Case 2: File exists but is empty**
- **Given** `2026-06.jsonl` exists with zero bytes
- **When** records are appended
- **Then** file contains valid JSONL (no leading newline artifacts)

## Error Conditions
**Error Scenario 1: Permission denied on config directory**
- Error message: logged warning `scorecard: write failed: permission denied`; reconcile run continues without failure

**Error Scenario 2: Disk full**
- Error message: logged warning `scorecard: write failed: no space left on device`; reconcile summary notes scorecard emission failure

## Performance Requirements
- **Response Time:** File I/O adds < 10ms to reconcile completion
- **Throughput:** Single append per run; no contention under normal usage

## Security Considerations
- **Input Validation:** Reviewer names and model strings are written as-is but must be valid JSON-encodable strings (escape control characters)
- **Data Protection:** File written to user-local config dir with default permissions (0600); no world-readable scores

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Mock reconcile Result with N reviewers; temp dir as config root
**Mock/Stub Requirements:** Override config dir via env var or option; no real LLM calls needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Directory auto-created on first write
- [ ] Monthly file naming correct (YYYY-MM.jsonl)
- [ ] Append mode preserves existing records
- [ ] Write failure logs warning but does not fail reconcile

**Manual Review:**
- [ ] Code reviewed and approved
