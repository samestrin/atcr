# Acceptance Criteria: Per-File Backup Map Precondition and Tracking

**Related User Story:** [03: Automatic Revert on Validation Failure](../user-stories/03-automatic-revert-on-validation-failure.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`) | In-memory orchestration type, no new file I/O primitives |
| Test Framework | `go test` (standard library `testing`) | Table-driven tests over map contents |
| Key Dependencies | `internal/atomicfs` (`BackupToDotBak` return value shape) | Consumes the return value produced by User Story 1's apply step; introduces no new dependency |

### Related Files (from codebase-discovery.json)
- `internal/autofix/revert.go` - create: defines the `BackupMap` (or equivalent `map[string]string` of `originalPath -> backupPath`) type this story's revert/cleanup functions operate on, and the constructor/accumulator helper the apply step (Story 1) calls after each successful `atomicfs.BackupToDotBak`.
- `internal/autofix/apply.go` - modify (Story 1 owns creation; this AC's scope is the contract it must satisfy): each successful `BackupToDotBak` call during apply appends `{originalPath: backupPath}` to the map passed into this story's revert function — a file whose write never happened must not appear in the map.
- `internal/autofix/revert_test.go` - create: unit tests asserting map coverage exactly matches write coverage across full-success, partial-apply, and zero-touch scenarios.

## Happy Path Scenarios
**Scenario 1: Full-success map matches every touched file**
- **Given** a patch that touches 3 files and every file is successfully backed up and written by Story 1's apply step
- **When** the apply step completes and hands its accumulated map to this story's revert entry point
- **Then** the map contains exactly 3 entries, one per touched file, each mapping the original path to its `.bak` path returned by `atomicfs.BackupToDotBak`

**Scenario 2: Empty patch produces an empty map**
- **Given** a patch that touches zero files (a no-op diff)
- **When** the apply step completes
- **Then** the map is empty and the revert/cleanup path is a no-op that returns immediately without error

## Edge Cases
**Edge Case 1: Partial apply — map coverage exactly matches write coverage, never wider**
- **Given** a patch touches 3 files, files A and B are backed up and written successfully, and file C's apply fails before any write occurs
- **When** control reaches this story's revert/cleanup entry point
- **Then** the map contains entries only for A and B; file C is absent from the map because it was never backed up (per Story 1's ordering: backup happens before write, so a file whose write never happened needs no restore)

**Edge Case 2: Duplicate path entries within a single patch**
- **Given** a patch entry list that (erroneously or via a legitimate multi-hunk diff) references the same target path twice
- **When** the apply step processes both entries
- **Then** the map holds a single entry for that path pointing at the most recent backup, so restore does not attempt to double-restore or leak a stale `.bak` reference

## Error Conditions
**Error Scenario 1: `BackupToDotBak` itself fails during apply (not this story's direct responsibility, but the map must reflect it)**
- **Given** `atomicfs.BackupToDotBak` returns a non-nil error for one file mid-patch
- **When** the apply step aborts that file's write
- **Then** that file is absent from the map handed to this story — Error message: not applicable here (Story 1 surfaces the backup error); this AC only asserts the map never contains an entry for a file whose backup did not succeed

## Performance Requirements
- **Response Time:** Map construction/lookup is O(1) per entry and negligible relative to file I/O; no measurable overhead for patches touching up to hundreds of files.
- **Throughput:** No batching constraints — map is held entirely in memory for the duration of a single `--auto-fix` invocation.

## Security Considerations
- **Authentication/Authorization:** Not applicable — purely local, in-process data structure with no external access.
- **Input Validation:** Paths in the map must be the same absolute/relative form used consistently by both the apply step (producer) and the revert step (consumer) — a mismatched path representation (e.g. relative vs. absolute) would cause a silent restore miss; tests must assert path form consistency.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Synthetic `payload.FileEntry` batches simulating full-success, partial-apply (2-of-3), zero-touch, and duplicate-path patches; no real GitHub or network fixtures needed.
**Mock/Stub Requirements:** None required — `atomicfs.BackupToDotBak` can run against real temp-directory fixtures (`t.TempDir()`) since it is a pure local filesystem call.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Map contains exactly one entry per successfully backed-up file, never more, never fewer
- [ ] Partial-apply scenario (2-of-3 files) produces a map with exactly 2 entries
- [ ] Zero-touch patch produces an empty map and a no-op revert/cleanup call

**Manual Review:**
- [ ] Code reviewed and approved
