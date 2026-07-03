# Acceptance Criteria: Restore All Touched Files on Validation Failure

**Related User Story:** [03: Automatic Revert on Validation Failure](../user-stories/03-automatic-revert-on-validation-failure.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`), function `RevertPatch` (or equivalent name) in `revert.go` | Synchronous, blocking restore loop |
| Test Framework | `go test` (standard library `testing`) | Includes fault-injection via a package-level `copyPathFn`/`renameFn` seam, mirroring `internal/fanout/reviewdir.go`'s pattern |
| Key Dependencies | `internal/atomicfs.CopyPath`, `os.Rename` | Restore-side primitives reused verbatim, no new low-level I/O |

### Related Files (from codebase-discovery.json)
- `internal/autofix/revert.go` - create: implements the restore loop that iterates the `{originalPath -> backupPath}` map and calls `atomicfs.CopyPath(backupPath, originalPath)` for every entry, modeled on `internal/fanout/reviewdir.go`'s `restorePriorBackup` but generalized to N per-file backups instead of one directory-wide `.bak`.
- `internal/fanout/reviewdir.go` - reference only (not modified): `restorePriorBackup` (line ~373) is the existing single-`.bak` restore-on-failure precedent this story's per-file loop generalizes from.
- `cmd/atcr/*.go` - modify: the `--auto-fix` orchestrator calls this story's revert function synchronously and only proceeds toward `internal/ghaction` calls (Stories 4/5) after receiving a "validated + cleaned up" terminal result — never after a pending or failed revert.
- `internal/autofix/revert_test.go` - create: unit tests asserting full-restore, partial-apply-restore, and pre-ghaction-call sequencing.

## Happy Path Scenarios
**Scenario 1: Single-file patch reverted on validation failure**
- **Given** a patch applied to 1 file with a `.bak` backup present, and the configured validation command exits non-zero
- **When** `--auto-fix`'s orchestrator invokes this story's revert function with the backup map and the failed validation signal
- **Then** the file's on-disk content is restored to be byte-for-byte identical to its pre-patch `.bak` content, verified via checksum comparison

**Scenario 2: Multi-file patch fully reverted on validation failure**
- **Given** a patch applied to 3 files, each with its own `.bak` backup, and validation exits non-zero
- **When** the revert function runs
- **Then** all 3 files are restored to their exact pre-patch byte content, and the function returns only after every restore has been attempted

## Edge Cases
**Edge Case 1: Partial-apply failure — revert covers only files that were actually backed up**
- **Given** a patch that touched 3 files but only 2 were successfully backed up and written before an unrelated apply-time error occurred (the third file's write never happened)
- **When** validation subsequently fails (or the orchestrator short-circuits straight to revert on the apply error)
- **Then** only the 2 backed-up files are restored; the third file (never written) is left untouched, since its "true prior state" is simply its current, unmodified state

**Edge Case 2: One file's restore fails, others must still be attempted**
- **Given** a 3-file revert where file B's `.bak` is missing at restore time
- **When** the revert loop processes A, B, and C
- **Then** the loop does not stop at B's failure — it still attempts C's restore, collects B's error alongside any others, and returns all collected errors rather than only the first one, so failure is localized to the smallest possible set

**Edge Case 3: Restore-of-CREATE — revert deletes a patch-created file instead of copying a backup**
- **Given** a patch whose diff created a brand-new file (a `/dev/null` old-side entry, per Story 1's apply behavior), so the backup-map entry for that path records "no prior original" (i.e. the file did not exist before the patch) rather than pointing at a `.bak` of prior content — contrast with a restore-of-MODIFY entry, whose backup-map value points at a `.bak` capturing the overwritten file's pre-patch bytes (see sibling AC [03-01](./03-01-backup-map-tracking.md) for how the backup map records each entry's origin)
- **When** validation fails and the revert loop processes this entry
- **Then** revert routes on the "no prior original" marker to DELETE the created file (leaving the tree as if the patch never applied) rather than attempting a `.bak` copy-back that has no source, while restore-of-MODIFY entries in the same map are still restored via `atomicfs.CopyPath(backupPath, originalPath)`

## Error Conditions
**Error Scenario 1: Restore attempted before validation signal is available**
- Error message: `"revert called without a validation result"` (or equivalent guard-clause error)
- HTTP status / error code: not applicable (internal Go `error`); function must reject a call that lacks a definitive pass/fail signal rather than guessing

**Error Scenario 2: Sequencing violation — a GitHub-mutating call attempted before revert completes**
- Error message: not user-facing at this layer; enforced structurally, not by a runtime error message
- HTTP status / error code: not applicable — this is a compile-time/structural guarantee (see Performance/Security note below), not a runtime-checked error path

## Performance Requirements
- **Response Time:** Restore loop runtime is dominated by per-file I/O (`atomicfs.CopyPath`) and scales linearly with the number of touched files; no additional overhead beyond the copy itself.
- **Throughput:** Synchronous, single-threaded restore is sufficient for the target use case (small technical-debt patches); no concurrency requirement, and concurrency would complicate the "collect all errors" contract without a measured need.

## Security Considerations
- **Authentication/Authorization:** Not applicable — purely local filesystem operation, no network or credential surface.
- **Input Validation:** The revert function must operate only on paths present in the backup map it was handed (never a caller-supplied arbitrary path), preventing an unrelated file from being clobbered by a mismatched restore call.

## Test Implementation Guidance
**Test Type:** UNIT and INTEGRATION
**Test Data Requirements:** `t.TempDir()`-based fixtures with real files and `.bak` siblings created via `atomicfs.BackupToDotBak`; single-file, multi-file, and partial-apply scenarios each need dedicated fixtures. Add a restore-of-CREATE fixture: a patch-created file with a backup-map entry marked "no prior original" (no `.bak`), asserting revert DELETES it, paired against a restore-of-MODIFY entry (with a `.bak`) asserting copy-back — a single map may mix both routing kinds.
**Mock/Stub Requirements:** A package-level `copyPathFn` seam (mirroring `atomicfs`'s and `fanout`'s existing `renameFn`/`copyPathFn` fault-injection pattern) to deterministically simulate a restore failure without relying on real disk-pressure conditions. An integration test asserting the orchestrator's control-flow gate (no `internal/ghaction` call reachable without a terminal revert/cleanup result) should use a stub `ghaction` client that fails the test if invoked out of sequence.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] 100% of files in the backup map are bit-for-bit identical to pre-patch state after a failed-validation revert, across single-file, multi-file, and partial-apply scenarios
- [ ] A single file's restore failure does not prevent remaining files' restores from being attempted
- [ ] A restore-of-CREATE entry (backup-map "no prior original" marker) is DELETED on revert, while restore-of-MODIFY entries in the same map are copied back from their `.bak`
- [ ] The `--auto-fix` orchestrator cannot reach any `internal/ghaction` call without first receiving this function's terminal result

**Manual Review:**
- [ ] Code reviewed and approved
