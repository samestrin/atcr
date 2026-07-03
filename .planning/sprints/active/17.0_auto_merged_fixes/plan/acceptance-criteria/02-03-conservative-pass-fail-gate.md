# Acceptance Criteria: Conservative Pass/Fail Gate (No Mutation, No Partial Success)

**Related User Story:** [02: Configurable Local Validation](../user-stories/02-configurable-local-validation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go decision logic (`internal/verify`), consumed by `internal/autofix` orchestration | Pure function of `ValidationResult` from AC 02-02 to a pass/fail decision |
| Test Framework | Go `testing` package, table-driven tests | Follows existing `internal/verify/*_test.go` conventions |
| Key Dependencies | None beyond the `ValidationResult` type from AC 02-02 | No new dependencies |

### Related Files (from codebase-discovery.json)
- `internal/verify/localvalidate.go` - modify: add the pass/fail decision function (e.g. `(r ValidationResult) Passed() bool`) that treats any non-zero exit code or timeout as failure with no stdout/stderr content inspection
- `internal/verify/localvalidate_test.go` - modify: unit tests asserting exit code `0` -> pass, any non-zero exit code -> fail, timeout -> fail, regardless of captured output content
- `internal/autofix/revert.go` - reference (Story 3 consumer): documents that this AC's pass/fail signal is the sole input to the automatic revert decision; this AC does not implement revert itself
- `internal/verify/syntaxguard.go` - reference only: shared conservative-failure design rationale (no output-content heuristics) this AC extends to the post-apply gate

## Happy Path Scenarios
**Scenario 1: Exit code 0 is always a pass**
- **Given** a `ValidationResult` with `ExitCode == 0` and no timeout
- **When** the pass/fail decision is evaluated
- **Then** the result is `Passed() == true`, independent of stdout/stderr content (e.g. even if stderr contains the word "error" as part of normal build output)

**Scenario 2: Non-zero exit code is always a fail**
- **Given** a `ValidationResult` with `ExitCode == 1` (or any non-zero value)
- **When** the pass/fail decision is evaluated
- **Then** the result is `Passed() == false`, with no attempt to parse stdout/stderr for a "actually it's fine" partial-success override

**Scenario 3: Decision is handed off without mutating the working tree**
- **Given** a computed pass/fail decision (either outcome)
- **When** the validation step returns control to the `internal/autofix` orchestration
- **Then** no file under the working tree has been created, modified, or deleted by the validation step itself — the `.bak` backups from Story 1 remain untouched and available for Story 3's revert

## Edge Cases
**Edge Case 1: Timeout counts as failure**
- **Given** a `ValidationResult` with `TimedOut == true` (command exceeded the configured timeout per AC 02-01/02-02)
- **When** the pass/fail decision is evaluated
- **Then** the result is `Passed() == false`, treated identically to a non-zero exit for downstream revert purposes

**Edge Case 2: Command-not-found is a distinct refusal, not a silent fail-and-continue**
- **Given** a `StartError` (command could not even start, per AC 02-01/02-02)
- **When** the `--auto-fix` flow encounters this before touching any files
- **Then** the flow refuses to proceed with `--auto-fix` entirely rather than treating it as a per-fix failure that reverts one patch and continues to the next

**Edge Case 3: Zero exit code with non-empty stderr**
- **Given** a validation command that exits `0` but writes warnings to stderr (e.g. a linter reporting non-fatal notices)
- **When** the pass/fail decision is evaluated
- **Then** the result is still `Passed() == true` — stderr content is captured for diagnostics only (AC 02-02) and never inspected by the decision logic

## Error Conditions
**Error Scenario 1: Attempting to call the revert or commit path directly from this AC's code**
- Error message: N/A — architectural boundary enforced by code review/import structure: `internal/verify`'s validation package must not import `internal/ghaction`, and this AC's code performs no file mutation or Git/GitHub call of any kind
- HTTP status / error code: N/A

**Error Scenario 2: Fail decision reached with an already-corrupted `.bak` backup state**
- Error message: `"validation failed but no backup available to revert"` — surfaced by Story 3, not this AC, but this AC must never delete or overwrite `.bak` files, guaranteeing Story 3 always has a clean input
- HTTP status / error code: N/A (Go `error`, propagated by Story 3's revert)

## Performance Requirements
- **Response Time:** Pass/fail decision evaluation is O(1) over the `ValidationResult` fields (exit code, timeout flag) — no content scanning, so effectively instantaneous relative to the command execution time it follows
- **Throughput:** One decision per validation run; no batching requirement

## Security Considerations
- **Authentication/Authorization:** N/A — decision logic has no access to GitHub credentials or network calls (explicitly out of scope per the story's Constraints)
- **Input Validation:** Decision logic trusts only `ExitCode` and `TimedOut`/`StartError` fields from AC 02-02's result struct; it must not be influenced by attacker/model-controlled stdout/stderr content, closing off any injection vector via crafted validation-command output

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven cases: exit 0 (pass), exit 1/2/127 (fail), timed-out (fail), exit 0 with stderr content containing "error"/"fail" strings (still pass, proving no content heuristics), command-not-found (distinct refusal path)
**Mock/Stub Requirements:** None beyond constructing `ValidationResult` values directly; also add an integration-style test in `internal/autofix` (once Story 1/3 land) verifying no filesystem writes occur during this step, using a temp dir with `os.Stat` mtime checks before/after

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Any non-zero exit code or timeout yields `Passed() == false`; only `ExitCode == 0` and no timeout yields `Passed() == true`
- [x] stdout/stderr content never influences the pass/fail decision (verified by a test asserting a passing exit code with "error"-laden stderr still passes)
- [x] No test or code path in this AC creates, modifies, or deletes any file, and `internal/verify`'s validation code has zero import of `internal/ghaction`

**Manual Review:**
- [x] Code reviewed and approved
