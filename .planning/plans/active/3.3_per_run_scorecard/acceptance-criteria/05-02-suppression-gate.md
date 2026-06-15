# Acceptance Criteria: Suppression Gate — Zero Records Written

**Related User Story:** [05: Suppress Emission](../user-stories/05-suppress-emission.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Scorecard Package | `internal/scorecard` | `Emit()` function checks `suppress` boolean as first condition |
| File I/O | `os.OpenFile` with `O_APPEND` | Never opened when suppression is active |
| Test Framework | `go test` + `testify/assert` | Assert zero new lines in JSONL file after suppressed run |

### Related Files

- `internal/scorecard/scorecard.go` - modify: `Emit()` accepts a `suppress bool` parameter; returns immediately if `true`
- `cmd/atcr/reconcile.go` - modify: pass `--no-scorecard` flag value to `Emit()` call
- `internal/scorecard/scorecard_test.go` - modify: add test asserting zero file I/O when suppress is `true`

## Happy Path Scenarios

**Scenario 1: `atcr reconcile --no-scorecard` writes zero records**
- **Given** a fresh or existing scorecard store at `~/.config/atcr/scorecard/`
- **When** `atcr reconcile --no-scorecard` is executed against a test fixture
- **Then** zero new lines are appended to any `.jsonl` file in the store; the file modification times are unchanged

**Scenario 2: JSONL file is not opened during suppressed run**
- **Given** the scorecard emission hook is called with `suppress=true`
- **When** the `Emit()` function is invoked
- **Then** the function returns immediately without calling `os.OpenFile` or any other file I/O operation

**Scenario 3: Suppression is silent — no scorecard-related output**
- **Given** `atcr reconcile --no-scorecard` is executed
- **When** reconcile completes successfully
- **Then** stdout and stderr contain no scorecard-related messages (no "scorecard suppressed", no "scorecard skipped", no "writing scorecard" text); only standard reconcile summary output is printed

**Scenario 4: Suppression does not affect reconcile exit code**
- **Given** a valid test fixture that reconcile processes successfully
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** the exit code is identical to a run without the flag (exit code 0 for success, non-zero for reconcile failure)

## Edge Cases

**Edge Case 1: Scorecard store directory does not exist yet**
- **Given** `~/.config/atcr/scorecard/` does not exist (first run ever)
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** the directory is NOT created; no file or directory artifacts are left behind

**Edge Case 2: Monthly JSONL file does not exist yet for current month**
- **Given** the scorecard directory exists but the current month's `.jsonl` file has not been created
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** the monthly `.jsonl` file is NOT created; no new files appear in the store

**Edge Case 3: Multiple reviewers in a single reconcile run — all suppressed**
- **Given** a reconcile run produces eval records for 3 reviewers
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** zero records are written for all 3 reviewers; not even partial writes occur

**Edge Case 4: Reconcile fails before reaching emission step**
- **Given** reconcile encounters an analysis error before reaching the post-completion hook
- **When** the run exits with a non-zero exit code
- **Then** no scorecard records are written (same as without `--no-scorecard` — the emission step was never reached); `--no-scorecard` has no effect on this path

## Error Conditions

**Error Scenario 1: Scorecard store is unwritable (permissions) — suppressed run does not error**
- **Given** `~/.config/atcr/scorecard/` exists with permissions `000`
- **When** `atcr reconcile --no-scorecard` is executed
- **Then** reconcile completes successfully (exit code 0) because no file I/O is attempted; no permissions error is surfaced

## Performance Requirements
- **Suppression overhead:** The early-return guard adds < 1µs to reconcile execution time (single boolean comparison)

## Security Considerations
- **No information leakage:** The suppression path does not log, print, or expose any information about the scorecard store state (existence, contents, or path)
- **No partial writes:** The early return occurs before any file handle is opened, eliminating the possibility of partial or empty records being written

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- A test fixture repo that reconcile can process successfully
- A temporary directory simulating `~/.config/atcr/scorecard/` (via `t.TempDir()` + path override)

**Mock/Stub Requirements:**
- For unit test: call `Emit(records, true)` directly, assert no file I/O occurs (use an `io.Writer` spy or verify the function returns before file operations)
- For integration test: run full reconcile with `--no-scorecard`, count lines in the monthly JSONL before and after, assert delta is 0
- Regression test: run the same reconcile without `--no-scorecard`, assert delta is > 0

## Definition of Done

### Automated Tests
- [ ] Unit test: `Emit()` with `suppress=true` returns immediately without file I/O
- [ ] Integration test: `atcr reconcile --no-scorecard` produces zero new JSONL lines
- [ ] Regression test: `atcr reconcile` (without flag) produces > 0 new JSONL lines
- [ ] Integration test: `--no-scorecard` run produces no scorecard text in stdout/stderr

### Story-Specific
- [ ] Suppression gate is the FIRST condition checked in `Emit()` — before any file path resolution
- [ ] No directory creation occurs during suppressed runs
- [ ] No partial writes are possible (early return before file handle open)

### Manual Verification
- [ ] Run `atcr reconcile --no-scorecard` manually and confirm no artifacts appear in `~/.config/atcr/scorecard/`
