# Acceptance Criteria: Hard Error Surfacing on Restore Failure

**Related User Story:** [03: Automatic Revert on Validation Failure](../user-stories/03-automatic-revert-on-validation-failure.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`), error aggregation in `revert.go` | Extends the restore loop (AC 03-02) with a named, multi-error return type |
| Test Framework | `go test` (standard library `testing`) | Asserts error message content names the diverged file(s) and `.bak` path(s) |
| Key Dependencies | `errors.Join` (Go stdlib, 1.20+) or an equivalent aggregation type | No new external dependency; matches `atomicfs`/`fanout`'s existing `fmt.Errorf("...: %w", err)` wrapping style |

### Related Files (from codebase-discovery.json)
- `internal/autofix/revert.go` - create: the restore loop (AC 03-02) collects a per-file error for any restore that fails, and this AC's scope is turning that collection into a hard, named error surfaced to the `--auto-fix` orchestrator rather than a swallowed log line.
- `internal/fanout/reviewdir.go` - reference only (not modified): `restorePriorBackup`'s existing precedent of logging (via `log.FromContext(ctx).Warn`) — not swallowing — an unrecoverable restore is the model this story's hard-error path extends from best-effort logging to a run-failing error, since AC4's job is safety-critical (unlike `restorePriorBackup`'s best-effort crash-recovery role).
- `cmd/atcr/*.go` - modify: the `--auto-fix` orchestrator treats a non-nil error from this story's revert function as a fatal, non-zero-exit condition for the CLI invocation, printed with enough detail (file + `.bak` path) for the maintainer to intervene manually.
- `internal/autofix/revert_test.go` - create: unit tests asserting the returned error names every file that failed to restore, not just the first.

## Happy Path Scenarios
**Scenario 1: All restores succeed — no error returned**
- **Given** a 3-file revert where every `.bak` is present and restorable
- **When** the restore loop completes
- **Then** the function returns a nil error and `--auto-fix` reports the validation-failure-and-revert outcome as the terminal (non-fatal-to-the-tool) result

**Scenario 2: Single restore failure is reported by name**
- **Given** a 1-file revert where the `.bak` is missing
- **When** the restore loop completes
- **Then** the returned error names the specific original file path and its expected `.bak` path, and `--auto-fix` exits non-zero with that message surfaced to the user

## Edge Cases
**Edge Case 1: Multiple restore failures in one run are all named, not just the first**
- **Given** a 3-file revert where files A and C fail to restore (missing `.bak`) but B restores successfully
- **When** the restore loop completes
- **Then** the returned error is an aggregate naming both A and C (and their respective `.bak` paths), not only whichever failed first — matching the story's "collecting and returning any restore errors rather than stopping at the first one" requirement

**Edge Case 2: A restore failure must never be silently downgraded to a log line only**
- **Given** any restore failure occurs during the AC4 revert path (as opposed to `fanout`'s best-effort `restorePriorBackup` crash-recovery case)
- **When** the failure is handled
- **Then** it is both logged (for operational visibility, consistent with `restorePriorBackup`'s existing Warn-level precedent) and returned as a hard error that fails the `--auto-fix` invocation — never one without the other, since a silent restore failure would leave a broken file in the tree with no indication anything went wrong

## Error Conditions
**Error Scenario 1: Single-file restore failure**
- Error message: `"failed to restore %s from backup %s: %w"` naming the diverged original path, its expected `.bak` path, and the wrapped underlying OS/copy error
- HTTP status / error code: not applicable (internal Go `error`); CLI-level, this maps to a non-zero process exit code for the `--auto-fix` invocation

**Error Scenario 2: Multi-file aggregate restore failure**
- Error message: an `errors.Join`-composed error whose `Error()` output enumerates each failed file on its own line/segment (e.g. `"revert failed for 2 file(s): failed to restore fileA.go from backup fileA.go.bak: ...; failed to restore fileC.go from backup fileC.go.bak: ..."`)
- HTTP status / error code: not applicable; same non-zero exit contract as the single-file case, but the message must be exhaustive rather than truncated to the first failure

## Performance Requirements
- **Response Time:** Error aggregation is O(n) in the number of failed files and adds no measurable overhead beyond the restore loop itself.
- **Throughput:** Not applicable — this is error-path bookkeeping, not a throughput-sensitive operation.

## Security Considerations
- **Authentication/Authorization:** Not applicable — purely local error handling with no external access.
- **Input Validation:** Error messages must include only the file paths already present in the backup map (no path traversal or injection risk from patch-controlled filenames being echoed verbatim into a shell or template context — this is a plain `fmt.Errorf`/log field, not executed).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `t.TempDir()` fixtures with one or more `.bak` files deliberately missing or made unreadable (e.g. via a fault-injection seam) to force restore failures deterministically.
**Mock/Stub Requirements:** The same `copyPathFn`/removal fault-injection seam as AC 03-02, driven to fail for a specific subset of files so the aggregate-error test can assert exact multi-file coverage.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] A single restore failure produces a named, non-nil error identifying the file and its `.bak` path
- [x] A multi-file restore failure names every failed file, not only the first
- [x] `--auto-fix` exits non-zero and prints the restore-failure detail when this error path fires; no restore failure is ever logged-only with a zero exit

**Manual Review:**
- [x] Code reviewed and approved
