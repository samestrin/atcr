# Acceptance Criteria: Every Target Write Goes Through Atomic Sibling-Temp-Then-Rename

**Related User Story:** [01: Apply a Parsed Patch to the Working Tree Without Corruption](../user-stories/01-apply-patch-to-working-tree-without-corruption.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`, file `apply.go`) | Consumes `atomicfs.WriteFileAtomic` as-is; no new write primitive |
| Test Framework | Go standard `testing`, `t.TempDir()`, `os.ReadDir` for `.tmp-*` leftover checks | Matches `internal/atomicfs/atomic_test.go` conventions (e.g. `TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget`) |
| Key Dependencies | `internal/atomicfs.WriteFileAtomic` (line 24 of `internal/atomicfs/atomic.go`) | Reused as-is — no direct `os.WriteFile`/`os.Create`/`os.OpenFile` on any target path in `internal/autofix` |

### Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go` - create: after `gitdiff.Apply` produces patched bytes for a modification or creation entry, the write to `FileEntry.Path` happens exclusively via `atomicfs.WriteFileAtomic(path, patchedBytes)`.
- `internal/autofix/apply_test.go` - create: tests asserting no direct `os.WriteFile`/`os.Create` call path exists for target writes, and that a concurrent reader never observes partial content.
- `internal/atomicfs/atomic.go` - reference only: `WriteFileAtomic` (line 24, sibling-temp-then-rename) is reused unmodified; this AC does not alter its crash-durability tradeoffs (no fsync, documented at `internal/atomicfs/atomic.go:18-23`).

## Happy Path Scenarios
**Scenario 1: Modification write lands atomically**
- **Given** `gitdiff.Apply` has produced patched bytes for an existing target file `foo.go`
- **When** `internal/autofix` writes the result
- **Then** `atomicfs.WriteFileAtomic("foo.go", patchedBytes)` is called, which creates a sibling temp file, writes the full patched content, and renames it over `foo.go`, so `foo.go` is either the pre-patch content or the full post-patch content at every point observable from outside the call

**Scenario 2: New-file creation write lands atomically**
- **Given** `gitdiff.Apply` has produced full content for a new-file entry (`--- /dev/null` old side) whose target path does not yet exist
- **When** `internal/autofix` writes the result
- **Then** `atomicfs.WriteFileAtomic` is called with the new path (its parent directory must already exist, matching `WriteFileAtomic`'s existing sibling-temp requirement — see Error Scenario 2), producing the file via the same temp-then-rename mechanism, never via `os.Create`

**Scenario 3: Concurrent reader never observes a partial write**
- **Given** a target file mid-patch under `internal/autofix`
- **When** a concurrent goroutine repeatedly opens and reads the target path while the write is in flight
- **Then** every observed read returns either the complete pre-patch content or the complete post-patch content — never a truncated, empty (for a pre-existing file), or partially-overwritten byte sequence

## Edge Cases
**Edge Case 1: Patched content byte-identical to current content**
- **Given** a diff whose hunks resolve to content identical to what is already on disk (a no-op patch)
- **When** `internal/autofix` writes the result
- **Then** the write still goes through `atomicfs.WriteFileAtomic` unconditionally (no special-cased skip-if-unchanged optimization that would bypass the atomic path)

**Edge Case 2: Large patched file**
- **Given** a patched file whose content is several MB (within the diff-ingestion `DefaultMaxDiffBytes` bound upstream)
- **When** `atomicfs.WriteFileAtomic` writes it
- **Then** the full content is written to the temp file before the rename occurs — partial writes to the temp file itself are invisible to readers of the target path, since the temp file has a different name until rename

## Error Conditions
**Error Scenario 1: Write to temp sibling fails (e.g. disk full)**
- **Given** `atomicfs.WriteFileAtomic`'s internal `tmp.Write` fails
- **When** `internal/autofix` calls `WriteFileAtomic`
- **Then** the target file at `Path` is left completely untouched (the failure occurs before any rename), the temp file is cleaned up by `WriteFileAtomic`'s deferred removal, and `internal/autofix` surfaces the wrapped error identifying `Path`
- Error message: `"autofix: writing %q: %w"` (path, wrapped `atomicfs.WriteFileAtomic` error)
- HTTP status / error code: N/A

**Error Scenario 2: Target's parent directory does not exist (new-file creation into a new subdirectory)**
- **Given** a new-file entry whose `Path` has a parent directory that does not yet exist on disk
- **When** `atomicfs.WriteFileAtomic`'s `os.CreateTemp(dir, ...)` is called
- **Then** the call fails (no implicit `os.MkdirAll`), and `internal/autofix` surfaces a clear per-file error rather than silently dropping the entry; this AC does not add directory-creation logic — that is out of scope per the story's constraint that `internal/autofix` is "not a new atomic-write primitive" (creating missing parent directories for genuinely new file trees, if needed, is deferred to a later story/decision, and this AC's Definition of Done includes an explicit test pinning the current no-mkdir behavior)
- Error message: `"autofix: writing %q: %w"` (path, wrapped `os.CreateTemp` error via `atomicfs.WriteFileAtomic`)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Atomic write overhead per file is bounded by one `os.CreateTemp`, one `Write`, one `Chmod`, one `Close`, and one `Rename` — no additional buffering layer added by `internal/autofix` beyond what `WriteFileAtomic` already does.
- **Throughput:** No batching/parallelism required for this AC; each file in a multi-file patch is written sequentially and independently (parallel writes are out of scope — see AC 01-04 for per-file isolation semantics).

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operation.
- **Input Validation:** `internal/autofix` must not construct a write path by any means other than the already-validated `FileEntry.Path` (see AC 01-01's note on `isSafeDiffContentPath`); no path concatenation logic is introduced in this AC that could reintroduce a traversal risk `atomicfs.WriteFileAtomic` itself does not guard against (it trusts its caller's path).

## Test Implementation Guidance
**Test Type:** UNIT (with one concurrency-flavored test)
**Test Data Requirements:** A `t.TempDir()` fixture file and its expected patched content; a scenario with a missing parent directory to pin the no-mkdir error behavior; a large (several-hundred-KB) content fixture to sanity-check the write path scales.
**Mock/Stub Requirements:** None for the happy path — real `atomicfs.WriteFileAtomic` against `t.TempDir()`. The concurrent-reader test spawns a real goroutine polling `os.ReadFile` against the target path during the write (following the pattern already exercised in `internal/atomicfs/atomic_test.go`'s `TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget`) rather than mocking the filesystem.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Every target-path write in `internal/autofix/apply.go` traces to a call to `atomicfs.WriteFileAtomic` — no direct `os.WriteFile`/`os.Create`/`os.OpenFile` on a `FileEntry.Path` anywhere in the package (verified by test and code inspection)
- [x] A concurrent-reader test confirms no partial/truncated read is ever observed during a write
- [x] A disk-full/write-failure scenario leaves the pre-existing target file byte-for-byte unchanged

**Manual Review:**
- [x] Code reviewed and approved
