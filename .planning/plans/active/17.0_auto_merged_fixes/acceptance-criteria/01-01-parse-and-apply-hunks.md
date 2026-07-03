# Acceptance Criteria: Parse and Apply Hunks for Modify, Create, and Delete Entries

**Related User Story:** [01: Apply a Parsed Patch to the Working Tree Without Corruption](../user-stories/01-apply-patch-to-working-tree-without-corruption.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/autofix`, file `apply.go`) | Thin orchestrator over `go-gitdiff` + `atomicfs`; no hand-rolled hunk matching |
| Test Framework | Go standard `testing` (table-driven), `t.TempDir()` fixtures | Matches convention in `internal/atomicfs/atomic_test.go` |
| Key Dependencies | `github.com/bluekeyes/go-gitdiff/gitdiff` (`gitdiff.Parse`, `gitdiff.Apply`), `internal/payload.FileEntry` (input shape, reference only) | New module dependency — add to `go.mod`/`go.sum` |

## Related Files
- `internal/autofix/apply.go` - create: core `Apply(entries []payload.FileEntry) (...)` orchestration; parses each entry's `Body` via `gitdiff.Parse`, reads current on-disk content (or treats missing target as empty for a new-file entry), and calls `gitdiff.Apply` to produce patched bytes in memory.
- `internal/autofix/apply_test.go` - create: table-driven tests covering modification, new-file creation (`--- /dev/null`), and deletion entries.
- `internal/payload/ingest.go` - reference only: `BuildEntriesFromDiff` (line 42) is the upstream producer of `[]payload.FileEntry`; not modified by this AC.
- `go.mod` - modify: add `github.com/bluekeyes/go-gitdiff` as a direct dependency.

## Happy Path Scenarios
**Scenario 1: Modify an existing file**
- **Given** a working-tree file `internal/foo/bar.go` whose content matches the diff's `--- a/internal/foo/bar.go` old side, and a `payload.FileEntry{Path: "internal/foo/bar.go", Body: <unified diff modifying two lines>}`
- **When** `autofix.Apply` processes the entry
- **Then** `gitdiff.Parse` successfully parses `Body` into a `*gitdiff.File`, `gitdiff.Apply` produces patched bytes that byte-for-byte match the diff's expected post-patch content, and the in-memory result is handed off for writing (AC 01-02)

**Scenario 2: Create a new file**
- **Given** a `payload.FileEntry` whose diff body has `--- /dev/null` on the old side and `+++ b/<new path>` on the new side, and no file currently exists at `<new path>`
- **When** `autofix.Apply` processes the entry
- **Then** the current content is treated as empty (no read error for a missing target), `gitdiff.Apply` produces the full new-file content, and the result is passed on for writing as a new file

**Scenario 3: Delete an existing file**
- **Given** a `payload.FileEntry` whose diff body has `+++ /dev/null` on the new side for a file that currently exists on disk
- **When** `autofix.Apply` processes the entry
- **Then** the deletion is recognized explicitly (branched on the `/dev/null` new-side marker, not inferred from an empty `gitdiff.Apply` result) and the entry is routed to file removal rather than a zero-byte atomic write

**Scenario 4: Multi-entry batch processes each file independently**
- **Given** a `[]payload.FileEntry` containing three entries: one modification, one creation, one deletion
- **When** `autofix.Apply` processes the batch
- **Then** each entry's parse/apply is performed independently (no shared mutable parse state between entries) and all three produce their correct respective results

## Edge Cases
**Edge Case 1: Empty entries slice**
- **Given** `autofix.Apply` is called with `entries == nil` or `len(entries) == 0`
- **When** the function runs
- **Then** it returns immediately with no error and no filesystem side effects (mirrors `BuildEntriesFromDiff`'s empty-diff behavior)

**Edge Case 2: Diff with multiple hunks in one file**
- **Given** a `payload.FileEntry.Body` containing more than one `@@ ` hunk against the same file
- **When** `gitdiff.Apply` runs
- **Then** all hunks in the file are applied in sequence to the same in-memory content, producing a single correctly patched result

**Edge Case 3: Target file's current content differs slightly (whitespace/line-ending drift) from the diff's expected old-side context**
- **Given** a target file whose content has minor drift within `go-gitdiff`'s fuzzy-match tolerance
- **When** `gitdiff.Apply` runs
- **Then** the hunk applies successfully via `go-gitdiff`'s built-in fuzzy matching (no custom leniency layer added in `internal/autofix`)

## Error Conditions
**Error Scenario 1: Malformed diff body fails to parse**
- **Given** a `payload.FileEntry.Body` that is not valid unified-diff syntax
- **When** `gitdiff.Parse` is called
- **Then** `autofix.Apply` returns a per-file error identifying the offending `Path` and wraps the underlying `gitdiff.Parse` error; no file on disk is touched for this entry
- Error message: `"autofix: parsing diff for %q: %w"` (path, wrapped `gitdiff.Parse` error)
- HTTP status / error code: N/A (library function; caller returns process exit code 1 per plan's CLI conventions)

**Error Scenario 2: Hunk context drifted beyond fuzzy-match tolerance**
- **Given** a target file whose on-disk content has diverged from the diff's expected old-side context beyond what `go-gitdiff` can fuzzy-match
- **When** `gitdiff.Apply` returns a non-nil error
- **Then** that error is treated as a hard failure for the file — no partial-confidence apply is accepted — and surfaced as a per-file error identifying `Path`
- Error message: `"autofix: applying patch to %q: %w"` (path, wrapped `gitdiff.Apply` error)
- HTTP status / error code: N/A

**Error Scenario 3: Target path missing when diff expects a modification (not a creation)**
- **Given** a `payload.FileEntry` whose diff has a non-`/dev/null` old-side path, but no file exists at `Path` on disk
- **When** `autofix.Apply` attempts to read the current content
- **Then** the entry fails with a clear error distinguishing "expected existing file, found none" from a generic read error, naming `Path`
- Error message: `"autofix: target %q does not exist but diff expects a modification (old side is not /dev/null)"`
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Applying a single typical technical-debt-sized diff (under ~50 changed lines across 1-5 files) completes in well under 1 second of CPU time; parse/apply cost is dominated by `go-gitdiff`, not `internal/autofix` orchestration overhead.
- **Throughput:** No batch-size limit imposed by this AC beyond what `payload.BuildEntriesFromDiff`'s existing `DefaultMaxDiffBytes` (10 MiB) already bounds upstream.

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operation, no network/auth surface in this AC.
- **Input Validation:** `Path` values are already validated for traversal/absolute-path safety by `payload.BuildEntriesFromDiff` (`isSafeDiffContentPath`) before reaching this package; `internal/autofix` does not need to re-implement that check but must not weaken it by resolving `Path` through anything other than a direct join against the working-tree root.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture unified-diff strings for: a simple single-hunk modification, a multi-hunk modification, a new-file creation (`/dev/null` old side), a deletion (`/dev/null` new side), a malformed/unparseable diff body, and a diff whose old-side context does not match any on-disk content. Use `t.TempDir()` to create the on-disk "before" state for modification/deletion cases.
**Mock/Stub Requirements:** None required — `gitdiff.Parse`/`gitdiff.Apply` and real filesystem reads via `t.TempDir()` are fast and deterministic enough to run directly in unit tests; no mocking layer needed for this AC.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Modification, new-file creation, and deletion diff entries each produce the correct in-memory patched result via `gitdiff.Parse`/`gitdiff.Apply`
- [ ] A malformed diff body and a hunk that fails to apply each return a clear per-file error naming the path, without panicking or touching disk
- [ ] `go.mod`/`go.sum` include `github.com/bluekeyes/go-gitdiff` as a direct dependency

**Manual Review:**
- [ ] Code reviewed and approved
