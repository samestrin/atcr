# Acceptance Criteria: Per-File Backup via BackupToDotBak Before Any Overwrite

**Related User Story:** [01: Apply a Parsed Patch to the Working Tree Without Corruption](../user-stories/01-apply-patch-to-working-tree-without-corruption.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`, file `apply.go`) | Consumes `atomicfs.BackupToDotBak` as-is; no new backup primitive |
| Test Framework | Go standard `testing`, `t.TempDir()` | Matches `internal/atomicfs/atomic_test.go`'s `TestBackupToDotBak_*` conventions |
| Key Dependencies | `internal/atomicfs.BackupToDotBak` (line 77 of `internal/atomicfs/atomic.go`) | Reused as-is; crash-safe `<src>.bak`/`<src>.bak.old` swap semantics already implemented there |

### Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go` - create: before writing patched bytes to an existing target (`atomicfs.WriteFileAtomic`) or removing a target (deletion entry), call `atomicfs.BackupToDotBak(FileEntry.Path)` first, once per file.
- `internal/autofix/apply_test.go` - create: tests asserting a `.bak` file exists with the pre-patch content immediately before the target is overwritten, and that a new-file entry produces no `.bak` (no-op backup of a non-existent source).
- `internal/atomicfs/atomic.go` - reference only: `BackupToDotBak` (line 77) documents that a missing `src` is a no-op returning `("", nil)` (line 79-81), which is exactly the new-file-creation case this AC relies on.

## Happy Path Scenarios
**Scenario 1: Modification backs up the pre-patch file before overwrite**
- **Given** an existing target file `foo.go` about to be patched by a modification entry
- **When** `internal/autofix` processes the entry
- **Then** `atomicfs.BackupToDotBak("foo.go")` is called and completes (producing `foo.go.bak` with the original pre-patch content) before `atomicfs.WriteFileAtomic` overwrites `foo.go`

**Scenario 2: Deletion backs up the file before removal**
- **Given** an existing target file `bar.go` targeted by a deletion entry (`+++ /dev/null`)
- **When** `internal/autofix` processes the entry
- **Then** `atomicfs.BackupToDotBak("bar.go")` is called and completes (producing `bar.go.bak`) before the file is removed from the working tree, so a later revert story can restore it from the backup

**Scenario 3: New-file creation is a backup no-op**
- **Given** a new-file entry whose target path does not yet exist
- **When** `internal/autofix` processes the entry
- **Then** `atomicfs.BackupToDotBak` is still called (uniform per-file flow, no special-cased skip) but returns `("", nil)` per its documented missing-source no-op behavior, and no `.bak` file is created

**Scenario 4: Multi-file patch backs up each touched file individually**
- **Given** a patch touching three existing files
- **When** `internal/autofix` processes the batch
- **Then** three separate `.bak` files are produced (one per touched file), not a single directory-level or tree-level backup, matching the story's stated constraint that AC4's revert needs file-by-file granularity

## Edge Cases
**Edge Case 1: A prior `.bak` already exists from an earlier auto-fix run**
- **Given** `foo.go.bak` already exists from a previous invocation
- **When** `internal/autofix` calls `atomicfs.BackupToDotBak("foo.go")` again
- **Then** the existing `.bak` is replaced (per `BackupToDotBak`'s documented "replacing any existing backup" behavior), using its own crash-safe `.bak.old` staging swap — `internal/autofix` does not attempt to preserve multiple backup generations itself

**Edge Case 2: Backup runs before write even when the patch is a no-op (identical content)**
- **Given** a modification entry whose patched result is byte-identical to current content
- **When** `internal/autofix` processes the entry
- **Then** the backup still runs unconditionally before the write, consistent with AC 01-02's Edge Case 1 (no skip-if-unchanged shortcut anywhere in the apply flow)

## Error Conditions
**Error Scenario 1: Backup fails (e.g. permission denied, disk full during staging copy)**
- **Given** `atomicfs.BackupToDotBak` returns a non-nil error for a target about to be modified or deleted
- **When** `internal/autofix` observes that error
- **Then** the file is NOT overwritten or removed — the write/delete step is skipped entirely for that file, the error is surfaced as a per-file failure naming `Path`, and processing continues to the next independent entry in the batch (per AC 01-04's isolation contract)
- Error message: `"autofix: backing up %q before apply: %w"` (path, wrapped `atomicfs.BackupToDotBak` error)
- HTTP status / error code: N/A

**Error Scenario 2: Symlinked target is skipped by BackupToDotBak**
- **Given** a target path that is a symlink (per `BackupToDotBak`'s documented symlink-skip behavior, line 85-90 of `internal/atomicfs/atomic.go`)
- **When** `internal/autofix` calls `atomicfs.BackupToDotBak`
- **Then** the backup call returns `("", nil)` (no error, no backup produced) and `internal/autofix` treats this the same as the new-file no-op case — it proceeds to write/delete without a backup, since `BackupToDotBak` intentionally does not follow symlinks; this is documented, expected behavior rather than a failure requiring a distinct error path

## Performance Requirements
- **Response Time:** Backup overhead per file is one filesystem copy plus one rename-based swap (already implemented in `BackupToDotBak`); for the target technical-debt-fix use case (small files, single-digit file counts per patch) this is not expected to be measurable against process startup time.
- **Throughput:** Backups are performed sequentially, one per file, in the same per-file loop as the write — no separate batch-backup pass is introduced.

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operation.
- **Input Validation:** No new path construction is introduced; `internal/autofix` passes the already-validated `FileEntry.Path` straight to `atomicfs.BackupToDotBak`, the same path used for the subsequent write, so backup and write always target the identical resolved path.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `t.TempDir()` fixtures for: an existing file about to be modified (assert `.bak` content equals pre-patch content), an existing file about to be deleted (assert `.bak` exists post-removal), a new-file entry (assert no `.bak` created), and a pre-existing `.bak` from a simulated prior run (assert it is replaced, not appended).
**Mock/Stub Requirements:** None required for the happy path — real `atomicfs.BackupToDotBak` against `t.TempDir()`. A permission-denied backup-failure case can use a read-only parent directory (platform-permitting) or the package's existing `renameFn`/`removeAllFn` fault-injection seams (`internal/atomicfs/atomic.go:157,171,201,207`) if a deterministic failure is needed cross-platform.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Every existing-file overwrite or deletion is preceded by a completed `atomicfs.BackupToDotBak` call for that exact path
- [ ] A multi-file patch produces one `.bak` per touched existing file, not a single tree-level backup
- [ ] A new-file creation entry produces no `.bak` file (no-op backup verified)
- [ ] A backup failure aborts the write/delete for that file without corrupting the target and without blocking other entries in the same batch

**Manual Review:**
- [ ] Code reviewed and approved
