# Acceptance Criteria: Cleanup Backup Files on Validation Success

**Related User Story:** [03: Automatic Revert on Validation Failure](../user-stories/03-automatic-revert-on-validation-failure.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`), function `CleanupBackups` (or equivalent) in `revert.go` | Runs in the same synchronous path as the failure-triggered restore |
| Test Framework | `go test` (standard library `testing`) | Asserts `.bak` absence post-cleanup via `os.Stat` |
| Key Dependencies | `os.Remove` (or `os.RemoveAll` for directory-shaped backups) | No new dependency; matches `atomicfs`'s existing best-effort removal precedent (e.g. `removeAllFn(bakOld)`) |

## Related Files
- `internal/autofix/revert.go` - create: implements the success-path cleanup that iterates the same `{originalPath -> backupPath}` map used by the failure-path restore (AC 03-02) and removes every `.bak` file once validation has passed.
- `internal/atomicfs/atomic.go` - reference only (not modified): `BackupToDotBak`'s doc comment establishes "garbage-collecting older .bak state is the caller's job" — this AC is that caller-side obligation for the auto-fix flow specifically.
- `internal/autofix/revert_test.go` - create: unit tests asserting `.bak` files are removed after a simulated validation-success call and are absent from the filesystem afterward.

## Happy Path Scenarios
**Scenario 1: All backups removed after validation passes**
- **Given** a patch applied to 3 files, each with a `.bak` backup, and the configured validation command exits zero (success)
- **When** the orchestrator invokes this story's cleanup path with the backup map and the passing validation signal
- **Then** all 3 `.bak` files are removed from disk, and the 3 live (patched) files remain untouched

**Scenario 2: Empty backup map cleanup is a no-op**
- **Given** a patch that touched zero files (empty backup map, see AC 03-01)
- **When** validation succeeds
- **Then** the cleanup path returns immediately without error and performs no filesystem operations

## Edge Cases
**Edge Case 1: A `.bak` file is already absent at cleanup time**
- **Given** a `.bak` file for one of the touched files was already removed out-of-band (e.g. by a concurrent process or a prior partial run)
- **When** cleanup attempts to remove it
- **Then** the missing-file case is treated as already-clean (not an error) — cleanup proceeds to the remaining entries without failing the overall `--auto-fix` run, mirroring `atomicfs`'s existing `errors.Is(err, fs.ErrNotExist)` tolerance pattern

**Edge Case 2: Repeated `--auto-fix` runs do not accumulate stale `.bak` state**
- **Given** a prior `--auto-fix` run completed successfully and its cleanup ran to completion
- **When** a subsequent `--auto-fix` run patches an overlapping set of files
- **Then** no leftover `.bak` from the prior run is mistaken for the current run's backup — Story 1's `BackupToDotBak` call for the new run creates a fresh backup that this story's cleanup will remove on the new run's own success path

## Error Conditions
**Error Scenario 1: Cleanup itself fails for a reason other than "already absent" (e.g. permission denied)**
- Error message: `"failed to remove backup %s: %w"` naming the specific `.bak` path and wrapping the underlying OS error
- HTTP status / error code: not applicable (internal Go `error`); logged at Warn (best-effort, matching `atomicfs.swapStagedBackup`'s precedent for non-critical post-success cleanup) rather than failing the overall successful `--auto-fix` run, since the live file is already correct and validated — only the backup artifact is stranded

## Performance Requirements
- **Response Time:** Cleanup runtime scales linearly with the number of touched files; negligible relative to the validation command's own runtime.
- **Throughput:** Runs once per successful `--auto-fix` invocation; no batching or concurrency requirement.

## Security Considerations
- **Authentication/Authorization:** Not applicable — purely local filesystem operation.
- **Input Validation:** Cleanup must only ever remove paths present in the backup map it was handed (the same map produced by AC 03-01), never an arbitrary `.bak`-suffixed path discovered via a directory glob — this prevents cleanup from deleting an unrelated `.bak` a user created manually.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `t.TempDir()` fixtures with live files and their `.bak` siblings; a variant fixture with one `.bak` pre-deleted to exercise the already-absent edge case.
**Mock/Stub Requirements:** A package-level removal seam (e.g. `removeFn`) to fault-inject a non-`ErrNotExist` removal failure deterministically, mirroring `atomicfs`'s `removeAllFn` pattern.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] All `.bak` files for a successfully validated patch are removed from disk after cleanup
- [ ] An already-absent `.bak` is tolerated, not treated as an error
- [ ] A non-`ErrNotExist` cleanup failure is logged at Warn and does not fail an otherwise-successful `--auto-fix` run

**Manual Review:**
- [ ] Code reviewed and approved
