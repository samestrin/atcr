# Acceptance Criteria: A Failed Hunk Reports a Clear Per-File Error Without Corrupting Prior Successes

**Related User Story:** [01: Apply a Parsed Patch to the Working Tree Without Corruption](../user-stories/01-apply-patch-to-working-tree-without-corruption.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`, file `apply.go`) | Batch-orchestration/error-aggregation logic layered over AC 01-01/01-02/01-03's per-file steps |
| Test Framework | Go standard `testing`, table-driven multi-entry batch tests | Matches repo convention |
| Key Dependencies | `errors.Join` (Go stdlib, for aggregating multiple per-file failures into one returned error) or an equivalent per-file result slice; `gitdiff.Apply`, `atomicfs.WriteFileAtomic`, `atomicfs.BackupToDotBak` (all reused, error paths already characterized in AC 01-01/01-02/01-03) | No new external dependency for this AC |

### Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go` - create: the top-level `Apply(entries []payload.FileEntry) (Result, error)` (or equivalent) function that loops over entries independently, capturing and aggregating per-file outcomes instead of stopping at the first failure.
- `internal/autofix/apply_test.go` - create: multi-entry batch tests asserting that a failing entry does not roll back or otherwise touch files from entries that already succeeded earlier in the same call.
- `internal/atomicfs/atomic.go` - reference only: this AC relies on `WriteFileAtomic`'s rename-based atomicity (line 24) and `BackupToDotBak`'s no-op-on-missing-source behavior (line 79-81) to guarantee each file's outcome is self-contained.

## Happy Path Scenarios
**Scenario 1: One failing entry among several succeeding entries**
- **Given** a batch of four `payload.FileEntry` values where the first, second, and fourth apply cleanly and the third has a hunk that fails to apply (context drifted beyond fuzzy-match tolerance)
- **When** `internal/autofix.Apply` processes the batch
- **Then** entries 1, 2, and 4 are fully applied (backed up and written, per AC 01-02/01-03) on disk, entry 3 is left completely untouched at its pre-patch content (no backup created, no partial write), and the overall call returns a per-file error identifying entry 3's path and reason while still reporting entries 1/2/4 as successes

**Scenario 2: Processing order does not depend on success of prior entries**
- **Given** a batch where entry 1 fails and entries 2-4 would succeed
- **When** `internal/autofix.Apply` processes the batch
- **Then** entries 2-4 are still attempted and applied (entry 1's failure does not short-circuit the batch), demonstrating each entry is processed independently rather than the batch aborting on first error

## Edge Cases
**Edge Case 1: All entries in a batch fail**
- **Given** every entry in a batch has an unparseable or unapplyable diff body
- **When** `internal/autofix.Apply` processes the batch
- **Then** no file on disk is modified, and the returned error/result aggregates all per-file failures (not just the first) so the caller can report every problem in one pass

**Edge Case 2: Same target path appears in two entries within one batch**
- **Given** a malformed or duplicate-producing upstream diff yields two `FileEntry` values with the same `Path`
- **When** `internal/autofix.Apply` processes the batch
- **Then** both entries are applied in the order they appear in the slice (second entry's backup captures the first entry's already-patched content, matching the file's real-time on-disk state at that point) — this AC does not add duplicate-path detection/rejection, since `BuildEntriesFromDiff`'s per-file-section parsing does not itself produce duplicates for a well-formed diff; sequential per-entry processing naturally handles it without special-casing

**Edge Case 3: Failure occurs during the backup or write step, not the parse/apply step**
- **Given** an entry whose `gitdiff.Parse`/`gitdiff.Apply` succeed but whose subsequent `atomicfs.BackupToDotBak` or `atomicfs.WriteFileAtomic` call fails (per AC 01-02 Error Scenario 1, AC 01-03 Error Scenario 1)
- **When** `internal/autofix.Apply` processes the batch
- **Then** that entry is reported as a failure using the same aggregation/isolation mechanism as a parse/apply failure — failure isolation is uniform across every stage of the per-file pipeline, not just the `gitdiff` stage

## Error Conditions
**Error Scenario 1: Aggregated batch error surfaces every failing file, not just the first**
- **Given** a batch with two failing entries among five total
- **When** `internal/autofix.Apply` returns
- **Then** the returned error (via `errors.Join` or an equivalent per-file result collection) includes both failing paths and their individual reasons, and `errors.Is`/`errors.As` can still unwrap to inspect an individual per-file error if the caller needs to distinguish failure types
- Error message: aggregated form embeds each per-file message from AC 01-01 Error Scenarios 1-3, AC 01-02 Error Scenario 1, and AC 01-03 Error Scenario 1, e.g. `"autofix: 2 of 5 entries failed: [path1: ...; path2: ...]"`
- HTTP status / error code: N/A (caller maps a non-nil aggregate error to the CLI's non-zero exit code per plan conventions)

**Error Scenario 2: A successful entry's write must not be visible as "in progress" to the caller when a later entry fails**
- **Given** entry 1 completes its full backup+write cycle successfully before entry 2 fails
- **When** `internal/autofix.Apply` returns with a non-nil aggregate error
- **Then** the caller can still determine, from the returned result value, which specific files were successfully applied versus which failed — the function does not simply return `error` with no success detail, since the later validation/revert stories (AC3/AC4) need to know exactly which files landed
- Error message: N/A (this scenario specifies the return shape, not a message)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** A single failing entry adds no meaningful overhead beyond the cost of the failing `gitdiff.Apply`/`BackupToDotBak`/`WriteFileAtomic` call itself — no retry loop or backoff is introduced for this AC (retries, if ever needed, are out of scope).
- **Throughput:** Batch processing remains O(n) in the number of entries; error aggregation must not turn a per-file check into an O(n²) scan (e.g. avoid re-scanning all prior results per entry — accumulate into a slice/map as the loop runs).

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operation.
- **Input Validation:** Aggregated error messages must not leak absolute filesystem paths beyond what `FileEntry.Path` (already a validated relative path) already contains — no additional path resolution is performed solely for error-message construction.

## Test Implementation Guidance
**Test Type:** UNIT (batch-level, building on the single-entry fixtures from AC 01-01/01-02/01-03)
**Test Data Requirements:** Multi-entry `[]payload.FileEntry` fixtures mixing succeeding and failing entries in different orders (fail-first, fail-middle, fail-last, fail-all), plus assertions on both the on-disk end state (successful files patched, failed files untouched at original content) and the returned error/result's per-file detail.
**Mock/Stub Requirements:** None required — real `t.TempDir()` fixtures with a mix of valid and deliberately malformed/unapplyable diff bodies exercise every branch without mocking.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A batch with a mix of succeeding and failing entries leaves every succeeding file correctly patched on disk and every failing file byte-for-byte unchanged from its pre-patch state
- [ ] The aggregate error/result identifies every failing entry (not just the first) with its path and reason
- [ ] The returned result distinguishes successful entries from failed ones so later stories (validation, revert) know exactly which files were touched

**Manual Review:**
- [ ] Code reviewed and approved
