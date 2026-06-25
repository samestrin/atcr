<think>
Let me carefully review this diff for issues. The diff is for Epic 10.1 - Diff-File Ingestion Path. It involves changes to:
- `internal/fanout/ingest_review_test.go` (new file - tests)
- `internal/fanout/resume.go` (modified - buildSlots signature change)
- `internal/fanout/retry_wiring_test.go` (modified - buildAgent signature change)
- `internal/fanout/review.go` (modified - new function, buildSlots/buildAgent signature change)
- `internal/fanout/review_scope_inject_test.go` (modified - buildAgent signature change)
- `internal/fanout/review_test.go` (modified - buildAgent signature change)
- `internal/fanout/review_tools_test.go` (modified - buildAgent signature change)
- `internal/payload/ingest.go` (new file - diff parsing primitive)
- `internal/payload/ingest_test.go` (new file - tests)

Let me review each section for real issues:

### internal/fanout/ingest_review_test.go
- Line 47: `t.Setenv("ATCR_TEST_KEY", "secret")` - This is suspicious. Setting an environment variable called "ATCR_TEST_KEY" with value "secret" looks like it could be a real API key being leaked into a test. However, it could also be a placeholder. Let me look more carefully... This seems like a test helper to verify some behavior. Without context, this could be an issue if this is leaking a real secret, but it looks like a placeholder.

Actually, looking again, this is a test file and the value is "secret" which is clearly a test placeholder. But it's worth noting that setting an environment variable in a test might leak state or pollute other tests if not cleaned up properly. `t.Setenv` should handle cleanup though.

- Line 162: `prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, diff)` - This test uses `req := ReviewRequest{...}` without an `OutputDir`, so it goes through the default path. But the test setup has `t.Setenv("ATCR_TEST_KEY", "secret")` which is suspicious - why is this needed?

### internal/payload/ingest.go

Let me look at the core code carefully:

1. `BuildEntriesFromDiff` - parses unified diff text. Looks correct for the round-trip guarantee.

2. `BuildEntriesFromDiffFile` - reads a diff file. Issues to check:
   - It uses `os.Open(path)` where `path` is relative - this is safe due to the lexical check
   - But wait - there's a TOCTOU concern mentioned in the comment: "a file that grows between Stat and read still cannot exceed the cap"
   - The `io.LimitReader(f, maxBytes+1)` then reads `maxBytes+1` bytes - if this succeeds and exceeds maxBytes, it errors. Good.
   - But what about the pre-read Stat? It opens the file, stats it, then creates a LimitReader. If the file is a symlink to /etc/passwd, the stat would follow it. Actually `os.Open` follows symlinks too. But `isSafeDiffPath` already rejected absolute paths and `..` traversal lexically, so if someone passes `subdir/file.diff` and `subdir` is a symlink to `/etc`, that could read `/etc/passwd` (if the symlink points to a directory, but `fi.Mode().IsRegular()` would catch a directory). Still, if `subdir` is a symlink to `/etc`, opening `subdir/passwd` would read `/etc/passwd`. This is a symlink TOCTOU issue. The comment acknowledges this: "Like isSafeRelPath, it is a lexical check: it does NOT resolve symlinks". So this is documented but still a security concern for an ingestion endpoint.

3. `fileSectionStarts` - detects git diff format vs loose format. Logic looks correct.

4. `looseSectionStarts` - uses hunk line counting. Looks reasonable.

5. `looseHeaderAt` - checks for `--- `/`+++ `/`@@ ` triple. Correct.

6. `hunkLineCounts` - parses hunk header. The regex `^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@` - this looks correct.

Wait, there's a subtle bug here. The regex requires `^@@ `, but what about lines that start with `@@` but are inside a comment or something? Actually for diff hunk headers, the standard format is `@@ -l,s +l,s @@ optional context`. The regex looks correct.

7. `splitLinesWithOffsets` - splits on `\n`. For CRLF inputs, the `\r` is kept in the lines. This might cause issues if a body line starts with `\r-` (removed CR-prefixed line). But actually, in diff format, lines start with `-`, `+`, ` ` (space), or `\` (no newline at end of file). A `\r` prefix would be unusual but the parser should still work.

Wait, there's a potential issue with CRLF and the hunk counting:
- In CRLF, a body line would be `+content\r\n`, which becomes `+content\r` after splitting
- When checking `strings.HasPrefix(lines[i], "-")`, it checks for `-` prefix, which still works
- But wait, what about `\ No newline at end of file`? In CRLF, it would be `\ No newline...\r`, and the `\` prefix check still works.

Actually, looking at the CRLF test case more carefully:
```
diff := "--- a/x.go\r\n+++ b/x.go\r\n@@ -1,1 +1,1 @@\r\n-a\r\n+b\r\n"
```

After splitting on `\n`:
- Line 0: `--- a/x.go\r` (at offset 0)
- Line 1: `+++ b/x.go\r` (at offset 10)
- Line 2: `@@ -1,1 +1,1 @@\r` (at offset 20)
- Line 3: `-a\r` (at offset 36)
- Line 4: `+b\r` (at offset 40)
- Line 5: `` (at offset 44)

So the trailing empty line is at offset 44, and the final empty body section goes from offset 40 to 44, which is `+b\r` - good.

But wait, what about the `splitLinesWithOffsets` function and the last line? It appends `s[start:]` which for a string ending with `\n` would be the empty string. The offsets are correct.

Now back to `looseHeaderAt`. It checks `i+2 < n`. For the CRLF case:
- Line 0: `--- a/x.go\r` starts with `--- ` ✓
- Line 1: `+++ b/x.go\r` starts with `+++ ` ✓  
- Line 2: `@@ -1,1 +1,1 @@\r` starts with `@@ ` ✓

Good.

8. `diffSectionPath` - extracts path from headers. Correct logic.

9. `parseDiffPathField` - parses path from header value. Drops timestamp, strips `a/` or `b/` prefix. The comment says "It does not trim surrounding spaces — diff paths may contain them." - this is intentional.

But wait, there's a subtle bug: `field == devNull` check happens AFTER `strings.TrimSuffix(field, "\r")`, but BEFORE checking if it ends with `/dev/null` or starts with `a/` or `b/`. For input `/dev/null\r`, it becomes `/dev/null` after TrimSuffix, and the `devNull` check catches it. Good.

But what about `b//dev/null`? Actually no, `b//dev/null` would strip the `b/` prefix and return `/dev/null`. Then in `diffSectionPath`, it checks `if newPath != "" && newPath != devNull` - so `/dev/null` would be skipped and fall back to oldPath. Good.

10. `headPathFromGitHeader` - parses `diff --git a/<old> b/<new>` line. Uses `strings.LastIndex(header, sep)` to find the last ` b/` token. This is a heuristic but should work for normal cases. What if a file path contains ` b/` literally? That's unusual but possible. The LastIndex approach is reasonable.

11. `isSafeDiffPath` - lexical check. Correct.

Let me look for more subtle issues:

**Issue: `splitLinesWithOffsets` and CRLF for the hunk parser**

In the hunk parser for CRLF input, the lines will have trailing `\r`. The hunk line count check looks at `strings.HasPrefix(lines[i], "-")`, `strings.HasPrefix(lines[i], "+")`, etc. - all prefix checks, so trailing `\r` doesn't affect them. Good.

But wait - what about `strings.HasPrefix(lines[i], " ")`? That's never explicitly checked. The default branch handles context lines, and the context check is implicit (anything not `-`, `+`, or `\` is context). So trailing `\r` is fine.

**Issue: empty diff file**

The `BuildEntriesFromDiffFile` function checks `if maxBytes > 0 && fi.Size() > maxBytes`. For a 0-byte diff, fi.Size() == 0, which is <= maxBytes, so it passes through to `BuildEntriesFromDiff("")`, which returns `[]FileEntry{}, nil`. Good.

But wait - what if the diff is exactly maxBytes? `fi.Size() > maxBytes` is strict, so maxBytes passes. Then `io.LimitReader(f, maxBytes+1)` allows up to maxBytes+1. If the file is exactly maxBytes, the read returns maxBytes bytes, and `int64(len(data)) > maxBytes` is false. Good.

**Issue: Trailing empty line in hunk body**

In `looseSectionStarts`, when `lines[i] == "" && i == n-1`, it breaks out of the loop. This handles the trailing newline case. But what if there's a trailing empty line WITHIN the diff (like a blank context line)? That's still a body line, and would be consumed by the hunk counting (since `\n` splits it). Wait, a blank context line would be just `\n`, which after splitting becomes `""`. In the hunk counting loop, this falls into the default case (context line), which decrements both oldRem and newRem. Good.

But what about a `\ No newline at end of file` line that's blank? That's unusual. The `\` prefix check handles `\ No newline...`. A truly empty line after a hunk body would be consumed by counting.

**Issue: Final newline + trailing empty body line**

In `splitLinesWithOffsets`, for input `"foo\n"`, lines are `["foo", ""]` and offsets are `[0, 4]`. The trailing empty line at offset 4 would match no marker.

In `fileSectionStarts` for loose format, `looseSectionStarts` walks through and breaks if `lines[i] == "" && i == n-1`. So the trailing empty line is handled.

But what about a diff that ends without a trailing newline? Like `"foo"` (no \n)? Then lines is `["foo"]`, offsets is `[0]`. The loop processes this single line. If it's a header line, starts gets `[0]`, and we proceed to consume hunk body. But since there's only one line, the body loop doesn't execute (no `@@` line). So we move on. Good.

Actually wait, if a single-line diff is just `"--- a/x.go"`, then `looseHeaderAt` checks `i+1 < n` and `i+2 < n`, which both fail. So `looseHeaderAt` returns false, and we get an error. Good - a single-line diff without the full header triple is malformed.

**Issue: Path with `b/` prefix that isn't `a/` or `b/`**

`parseDiffPathField` strips `a/` or `b/` prefix. What if the path literally starts with `a/` or `b/` (like `a/b.go`)? After stripping, it becomes `b.go`. That's correct because in git diff format, `a/` is the old-side prefix and `b/` is the new-side prefix, not part of the actual path.

But wait, what about a file path like `api/a.go`? In `--- a/api/a.go`, after stripping `a/`, we get `api/a.go`. In `--- api/a.go` (no prefix), we don't strip, so we get `api/a.go`. Good.

**Issue: `BuildEntriesFromDiffFile` - symlink following**

The comment explicitly acknowledges: "Like isSafeRelPath, it is a lexical check: it does NOT resolve symlinks, so a relative in-tree path whose component is a symlink pointing outside the tree is out of this guard's scope — callers ingesting untrusted suite trees should pre-resolve paths if that matters."

This is a documented limitation. Not a bug per se, but a security concern for the file-path variant.

**Issue: `BuildEntriesFromDiffFile` - file descriptor leak**

`defer func() { _ = f.Close() }()` - the close error is silently ignored. This is acceptable in a defer, but the comment says `_ = f.Close()` to suppress the linter. Not a real issue.

**Issue: `looseSectionStarts` - blank lines within hunks**

In the hunk counting loop, what about a truly empty line (no characters at all)? That would be a context line (default case), decrementing both counts. But a truly empty line is unusual in a diff body - context lines should have at least a space prefix or be just `\n`. In the splitLinesWithOffsets, a blank line becomes `""`. The default branch handles this as context (both decremented). That's correct.

**Issue: `BuildEntriesFromDiff` and the empty diff case**

The function checks `strings.TrimSpace(diffText) == ""` and returns zero entries. But what about a diff that has only whitespace lines? Those would be trimmed. Good.

But what about a diff that has headers but no hunks? Like `"--- a/x.go\n+++ b/x.go\n"`? In `looseSectionStarts`, `looseHeaderAt` checks `i+2 < n && strings.HasPrefix(lines[i+2], hunkMarker)`. If there's no hunk header, this fails, and we get an error. Good - a diff without hunks is malformed.

**Issue: `diffSectionPath` and binary sections**

For a git binary section like:
```
diff --git a/logo.png b/logo.png
index abc1234..def5678 100644
Binary files a/logo.png and b/logo.png differ
```

The `for _, ln := range strings.Split(section, "\n")` loop:
- `gitHeader = "diff --git a/logo.png b/logo.png"` (gitDiffMarker prefix)
- `Binary files a/logo.png and b/logo.png differ` - no match
- No `--- ` or `+++ ` lines

So `oldPath == ""`, `newPath == ""`, but `gitHeader != ""`. Falls through to `headPathFromGitHeader`, which extracts `logo.png`. Good.

**Issue: Test file `ingest_review_test.go` - `t.Setenv("ATCR_TEST_KEY", "secret")`**

This is in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`. Why is this env var set? It doesn't appear to be used in the test. This looks like leftover debugging code or a test artifact. It's not a real security issue (it's a test placeholder), but it's a code smell.

Wait, actually looking at it more carefully, the test does `t.Setenv("ATCR_TEST_KEY", "secret")` but never reads it. This is suspicious - it might be:
1. Leftover from development
2. Testing that env vars don't leak between tests
3. A real security concern if "ATCR_TEST_KEY" is an actual API key

Given the context (Epic 10.1 is about ingestion), this is likely leftover debugging code. It should be removed.

**Issue: `BuildEntriesFromDiffFile` - error message includes raw error**

The error wrapping uses `%w` for the open error and stat error, but the stat error message includes the path. This is fine for debugging.

**Issue: `BuildEntriesFromDiff` - no validation of `diff` parameter for nil**

If `diffText` is `""`, the trimspace check catches it. If it's `"   "`, also caught. Good.

**Issue: `splitLinesWithOffsets` - performance**

For a large diff (up to 10 MiB), this function allocates slices for lines and offsets. This is acceptable for the use case.

**Issue: `looseSectionStarts` - performance**

The hunk counting loop is O(n) in the number of lines. Good.

**Issue: `diffSectionPath` - early termination**

The loop breaks out after finding the first `--- ` and `+++ ` lines (due to `oldPath == ""` and `newPath == ""` checks). This is correct because the first such lines in a section are the headers, not body lines.

Wait, what if the section starts with a `diff --git` line followed by `--- ` and `+++ `? The `--- ` and `+++ ` are the next lines. The loop processes them in order. Good.

But what if there's a `diff --git` line in the middle of a section? That shouldn't happen in a well-formed diff - each `diff --git` starts a new section. The `fileSectionStarts` function ensures sections are partitioned by `diff --git` boundaries. So within a section, there's no `diff --git` line. Good.

**Issue: `parseDiffPathField` - path with `b/` that's not a prefix**

What if the path is `b/x.go` (without the `a/` or `b/` being a diff prefix)? The code strips `b/` prefix unconditionally. So `b/x.go` becomes `x.go`. This is correct for git diff format.

But what about a non-git diff (loose format) where the path is literally `b/x.go`? The code would still strip the `b/` prefix. This is a potential issue if someone uses a loose diff with paths that start with `b/`. But the comment says "strips the conventional a/ or b/ prefix (absent under `git diff --no-prefix`)" - so this is the expected behavior. For loose diffs without prefixes, the paths wouldn't have `a/` or `b/`, so no stripping occurs. Good.

**Issue: `BuildEntriesFromDiffFile` - reading a directory**

If `path` points to a directory, `os.Open` would succeed (on Linux), `f.Stat()` would succeed, `fi.Mode().IsRegular()` would return false, and we'd get an error. Good.

But wait, `os.Open` on a directory... actually on some systems this might fail. On Linux, opening a directory with `os.Open` succeeds (you can read directory entries with ReadDir). The Stat check catches this. Good.

**Issue: `BuildEntriesFromDiffFile` - race condition on file size**

The TOCTOU defense is mentioned: "a file that grows between Stat and read still cannot exceed the cap". The `io.LimitReader(f, maxBytes+1)` ensures the read is bounded. Good.

**Issue: `BuildEntriesFromDiffFile` - error message consistency**

The error messages use "diff ingestion:" prefix consistently. Good for log filtering.

**Issue: `BuildEntriesFromDiff` and `strings.TrimSpace`**

The check `strings.TrimSpace(diffText) == ""` returns zero entries for whitespace-only input. But it allocates a new string via TrimSpace. For large inputs, this is a small overhead. Acceptable.

**Issue: `looseSectionStarts` - hunk header without counts**

If a hunk header is `@@ -1 +1 @@` (no counts), the regex `^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@` should match with empty groups. The `hunkLineCounts` function defaults to (1, 1) when counts are absent. Good.

**Issue: `diffSectionPath` - section with only `--- /dev/null`**

For a pure addition, `--- /dev/null` and `+++ b/new.go`. The `newPath` is `new.go`, not `/dev/null`, so it's used. Good.

For a pure deletion, `--- a/old.go` and `+++ /dev/null`. The `newPath` is `/dev/null`, so it falls back to `oldPath` which is `old.go`. Good.

**Issue: `BuildEntriesFromDiffFile` - cleanup on error**

If `f.Stat()` fails, the file is closed by the defer. If `BuildEntriesFromDiff` fails, the file is already closed. Good.

Now let me look at the review.go changes:

### internal/fanout/review.go

1. `PrepareReview` calls `finalizePreparedReview` - good refactoring.
2. `finalizePreparedReview` is the shared tail - good.
3. `PrepareReviewFromDiff` - the new function. Issues to check:
   - Size cap check: `if int64(len(diffText)) > payload.DefaultMaxDiffBytes` - correct
   - Empty diff check: `if len(entries) == 0` - correct
   - Byte budget check: `if trunc.AllDropped` - correct
   - Builds payloads map with only `diff` mode - correct per the sprint plan
   - Calls `buildSlots` with `diffMode` force - correct
   - Calls `finalizePreparedReview` with `diffMode` - correct

Wait, there's a potential issue: the `diffMode := string(payload.ModeDiff)` - what's `payload.ModeDiff`? It's a constant in the payload package. If it's `"diff"`, this works. But if it's a different value, the `payloads` map key would be wrong. Let me check... the sprint plan says "the payloads map is keyed solely to 'diff'", and `payload.ModeDiff` should be `"diff"`. This is consistent.

4. `buildSlots` now takes a `forceMode` parameter. The logic uses it for all agents. Good.

5. `buildAgent` now takes a `forceMode` parameter. When non-empty, it overrides the agent's effective mode. Good.

Wait, there's a subtle issue: `buildAgent` has a defensive check:
```go
if mode == "" {
    mode = ac.EffectivePayloadMode(cfg.Settings)
}
mp, ok := payloads[mode]
if !ok {
    // Defensive: payloads is built by neededModes over the same roster, so a
    // missing key is a configuration error the caller must fix...
}
```

When `forceMode` is non-empty, `mode = forceMode`. The `payloads` map for the ingestion path is keyed to `diffMode`. So `payloads[mode]` should succeed.

But wait - the original `PrepareReview` path calls `buildSlots(cfg, payloads, req.Range, "")` with empty forceMode. So `buildAgent` uses the agent's effective mode. The `payloads` map is built by `neededModes(cfg)` which returns the union of effective modes. So `payloads[mode]` should have the key. Good.

For the ingestion path, `forceMode = diffMode`, and `payloads = {diffMode: ...}`. So `payloads[diffMode]` succeeds. Good.

**Issue: `buildAgent` - forceMode precedence**

When `forceMode` is non-empty, it completely overrides `ac.EffectivePayloadMode(cfg.Settings)`. This means even if an agent is configured for a specific mode, the ingestion path forces it to `diff`. This is intentional per the sprint plan: "every agent reviews it regardless of its configured payload mode".

But wait - what if `forceMode` is set but the agent has no configuration for that mode? The `payloads[mode]` check would fail. For the ingestion path, `payloads` only has `diff`, so any `forceMode` other than `diff` would fail. But `PrepareReviewFromDiff` always sets `forceMode = diffMode`, so this is consistent.

**Issue: `PrepareReviewFromDiff` - what about agents with scope focus?**

The scope focus is injected in `buildAgent` based on the agent's configuration. If `forceMode` is set, the scope focus is still applied (it's based on `ac`, not on mode). Good.

**Issue: `PrepareReviewFromDiff` - `req.Range` is unused for the diff path**

The `req.Range` is passed to `buildSlots` and `finalizePreparedReview`. For a range-less diff, `req.Range` is zero-valued. The `Manifest` records `Base` and `Head` as empty strings. This matches the sprint plan: "req.Range is provenance-only here and may be left zero".

But wait - `finalizePreparedReview` uses `req.Range.DetectionMode`, `req.Range.DefaultBranch`, `req.Range.CommitCount`. For a range-less diff, these are all zero-valued. This is fine.

**Issue: `PrepareReviewFromDiff` - `req.Repo`**

The `PreparedReview` includes `Repo: req.Repo`. For a range-less diff, `req.Repo` might be zero. This is fine.

**Issue: `PrepareReviewFromDiff` - `req.NoCache`**

The `cacheNoRead: req.NoCache` is passed to `PreparedReview`. Good.

**Issue: `PrepareReviewFromDiff` - output directory handling**

The function checks `req.OutputDir != "" && req.IDOverride != ""` and returns an error. This matches `PrepareReview`'s behavior. But wait - is this check duplicated? Let me check... `PrepareReview` likely has the same check. If so, it's a minor code duplication but not a bug.

Actually, looking at the diff, `finalizePreparedReview` likely handles the output dir logic. The check in `PrepareReviewFromDiff` is a pre-check before the expensive work. Good.

**Issue: `PrepareReviewFromDiff` - manifest write**

The `finalizePreparedReview` writes the manifest with `PayloadMode: payloadMode`. For the ingestion path, `payloadMode = diffMode = "diff"`. Good.

**Issue: `PrepareReviewFromDiff` - `.atcr/latest` repoint**

The `finalizePreparedReview` calls `writeLatestPointer` or similar. For the ingestion path with `req.OutputDir == ""`, it repoints `.atcr/latest`. Good.

**Issue: `BuildEntriesFromDiffFile` - relative path with `./` prefix**

What if the path is `./patch.diff`? `filepath.Clean("./patch.diff")` returns `"patch.diff"`. `filepath.IsAbs("./patch.diff")` returns false. So `./patch.diff` passes the safety check. Good.

What about `.`? `filepath.Clean(".")` returns `"."`. The check `if clean == "."` returns false. Good.

What about empty string? `filepath.Clean("")` returns `"."`. The check catches it. Good.

What about `..`? `filepath.Clean("..")` returns `".."`. The check `clean == ".."` returns false. Good.

What about `../foo`? `filepath.Clean("../foo")` returns `"../foo"`. The check `strings.HasPrefix(clean, ".."+string(filepath.Separator))` returns true. Good.

What about `foo/../bar`? `filepath.Clean("foo/../bar")` returns `"bar"`. The check passes. Good - this is a relative path within the tree.

What about `foo/..`? `filepath.Clean("foo/..")` returns `"."`. The check `clean == "."` returns false. Good.

**Issue: `BuildEntriesFromDiffFile` - symlink resolution**

As noted, `os.Open` follows symlinks. If `subdir` is a symlink to `/etc`, then `subdir/passwd` would read `/etc/passwd`. This is a symlink TOCTOU issue. The comment acknowledges this limitation.

For the `BuildEntriesFromDiff` function (text variant), there's no file system interaction, so no symlink issue. The issue only applies to the file variant.

**Issue: `BuildEntriesFromDiff` - binary diff with no `--- /+++ `**

For a binary section like:
```
diff --git a/logo.png b/logo.png
index abc1234..def5678 100644
Binary files a/logo.png and b/logo.png differ
```

The `diffSectionPath` function:
- `gitHeader = "diff --git a/logo.png b/logo.png"`
- `Binary files a/logo.png and b/logo.png differ` - no match for `--- ` or `+++ `
- Falls through to `headPathFromGitHeader` → `"logo.png"`

But what if there's a `Binary files ...` line followed by a `--- ` line (which would be a body line, not a header)? In git diff, binary sections don't have `--- `/`+++ ` lines. So this shouldn't happen. But if it did, the `--- ` line would be treated as a header (since it's the first `--- ` line in the section). This is a corner case that's unlikely in practice.

Wait, actually, the `diffSectionPath` function processes lines in order and takes the first `--- ` and `+++ ` lines. For a binary section, there's no `--- ` or `+++ ` line, so both `oldPath` and `newPath` remain empty, and we fall through to `gitHeader`. Good.

**Issue: `looseSectionStarts` - hunk with `\` prefix line**

The code handles `\ No newline at end of file`:
```go
case strings.HasPrefix(lines[i], `\`):
    // "\ No newline at end of file" — counts toward neither side.
```

But wait, the `\` check is before the default case. What if a line starts with `\` but is actually a context line? In standard diff format, `\` at the start of a line means "no newline at end of file". This is correct.

**Issue: `looseSectionStarts` - hunk with empty line**

An empty line in a hunk body (just `\n`) becomes `""` after splitting. The default branch handles it as a context line (both decremented). But wait - an empty line in source code would be represented as ` ` (single space) in a diff, not as an empty line. An actual empty line in the split would be unusual. But the parser handles it gracefully.

**Issue: `BuildEntriesFromDiffFile` - error wrapping**

The error messages use `fmt.Errorf("diff ingestion: ...: %w", err)` which wraps the underlying error. Good for debugging.

**Issue: `PrepareReviewFromDiff` - error wrapping**

The function wraps errors with context like `fmt.Errorf("%w (mode diff, dropped %d file(s))", ErrPayloadFullyDropped, len(trunc.FilesDropped))`. Good.

Now let me look at the test files more carefully:

### internal/payload/ingest_test.go

1. `TestBuildEntriesFromDiff_LooseRoundTrip` - tests round-trip for loose diff. Good.
2. `TestBuildEntriesFromDiff_GitFormatRoundTrip` - tests round-trip for git diff. Good.
3. `TestBuildEntriesFromDiff_DeletedFileUsesOldPath` - tests deleted file path. Good.
4. `TestBuildEntriesFromDiff_EmptyIsZeroEntries` - tests empty input. Good.
5. `TestBuildEntriesFromDiff_GarbageErrors` - tests malformed input. Good.
6. `TestBuildEntriesFromDiff_LeadingContentErrors` - tests preamble rejection. Good.
7. `TestBuildEntriesFromDiffFile_ReadsRelativePath` - tests file reading. Good.
8. `TestBuildEntriesFromDiffFile_RejectsUnsafePaths` - tests path safety. Good.
9. `TestBuildEntriesFromDiffFile_SizeCapRejectsOversized` - tests size cap. Good.
10. `TestBuildEntriesFromDiff_HunkBodyDoesNotSpoofHeader` - regression test for hunk body spoofing. Good.
11. `TestBuildEntriesFromDiff_MultiHunkSingleFile` - tests multi-hunk. Good.
12. `TestBuildEntriesFromDiff_GitBinarySection` - tests binary section path parsing. Good.
13. `TestBuildEntriesFromDiff_CRLFLineEndings` - tests CRLF handling. Good.
14. `TestBuildEntriesFromDiffFile_MissingFileErrors` - tests missing file. Good.
15. `TestBuildEntriesFromDiff_FixtureParity` - tests fixture parity. Good.

### internal/fanout/ingest_review_test.go

1. `TestPrepareReviewFromDiff_ScaffoldsAndForcesDiffMode` - tests scaffolding. Good.
2. `TestPrepareReviewFromDiff_EmptyDiffRejected` - tests empty diff. Good.
3. `TestPrepareReviewFromDiff_BudgetDropsAllErrors` - tests budget drop. Good.
4. `TestPrepareReviewFromDiff_SizeCapRejectsOversized` - tests size cap. Good.
5. `TestPrepareReviewFromDiff_EmptyRosterRejected` - tests empty roster. Good.
6. `TestPrepareReviewFromDiff_MalformedDiffPropagates` - tests malformed diff. Good.
7. `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - tests mid-file drop. 
   - **Issue**: `t.Setenv("ATCR_TEST_KEY", "secret")` is set but never used. This is suspicious code.
8. `TestPrepareReviewFromDiff_FixtureEndToEnd` - tests end-to-end. Good.

Let me focus on the most significant issues I've found:

1. **`t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`** - This is an unused environment variable set in a test. It could be:
   - Leftover debugging code
   - A test artifact that should be removed
   - Potentially a security concern if "ATCR_TEST_KEY" is a real key name
   
   Given the value is "secret" (clearly a placeholder), this is likely leftover debugging code. It should be removed for cleanliness.

2. **`BuildEntriesFromDiffFile` - symlink TOCTOU** - The comment acknowledges this limitation. For the ingestion endpoint, this could be a security concern if untrusted paths are passed. However, the sprint plan says the file-path variant is for the relative-path case, and the text variant is for absolute paths. The symlink issue is a documented limitation.

3. **`BuildEntriesFromDiff` - binary section with `--- `/`+++ ` in body** - If a binary section has `--- ` or `+++ ` lines in the body (unusual but possible), the `diffSectionPath` function would treat them as headers. This is a corner case that's unlikely in practice.

4. **`PrepareReviewFromDiff` - duplicate output dir check** - The check `req.OutputDir != "" && req.IDOverride != ""` might be duplicated in `finalizePreparedReview`. Not a bug, but minor code duplication.

Actually, let me reconsider the `t.Setenv` issue. Looking at the test more carefully:

```go
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	// ...
```

This is in a test file. The env var is set but never read. This is clearly leftover debugging code or a test artifact. It should be removed.

Now let me also check for correctness issues:

5. **`splitLinesWithOffsets` and CRLF hunk parsing** - In CRLF format, hunk body lines have trailing `\r`. The hunk counting logic checks for `-`, `+`, `\` prefixes, which all work with trailing `\r`. But the default branch (context line) doesn't check for anything specific - it just decrements both counts. A line with trailing `\r` and no prefix character would be treated as a context line. This is correct.

But wait - what about a CRLF line that's just `\r`? After splitting on `\n`, it becomes `\r`. The prefix checks don't match `-`, `+`, or `\`. So it falls into the default branch and is treated as a context line. This is correct behavior.

6. **`diffSectionPath` - `gitDiffMarker` check** - The function checks `gitHeader == "" && strings.HasPrefix(ln, gitDiffMarker)`. This means only the first `diff --git` line is captured. But within a single section, there should be only one `diff --git` line. Good.

7. **`BuildEntriesFromDiff` and `BuildEntriesFromDiffFile` - error message consistency** - The error messages are consistent with "diff ingestion:" prefix. Good.

8. **`PrepareReviewFromDiff` - what if `req.OutputDir` is set but no `req.IDOverride`?** - The function proceeds to `finalizePreparedReview` which handles the output dir logic. Good.

9. **`PrepareReviewFromDiff` - what if both `req.OutputDir` and `req.IDOverride` are empty?** - The function proceeds normally. Good.

10. **`PrepareReviewFromDiff` - size cap check is before parsing** - The check `int64(len(diffText)) > payload.DefaultMaxDiffBytes` happens before `BuildEntriesFromDiff`. This prevents parsing of oversized diffs. Good for memory safety.

11. **`BuildEntriesFromDiffFile` - size cap check is before reading** - The check `fi.Size() > maxBytes` happens before reading. Good for memory safety.

Let me also check the test for `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`:

```go
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
```

This env var is set but never used in the test body. This is clearly leftover code. It should be flagged.

Actually, wait. Let me re-read the test more carefully. The test:
1. Sets `ATCR_TEST_KEY=secret` (unused)
2. Creates a three-file diff
3. Calls `PrepareReviewFromDiff`
4. Reads the materialized payload
5. Calls `ExecuteReview`
6. Reads the status files

The env var is never read. This is definitely leftover debugging code.

Let me also look at the `twoAgentConfig` helper - it's used throughout but not defined in the diff. It must be defined elsewhere in the package. Good.

Now let me also check for any issues with the `forceMode` parameter:

12. **`buildAgent` with `forceMode` - what about agents that require specific modes?** - The forceMode overrides the agent's effective mode. If an agent is configured for `blocks` mode but forceMode is `diff`, the agent gets `diff` mode. This is intentional per the sprint plan.

13. **`buildSlots` with `forceMode` - per-agent mode map** - The `perAgentMode` map records the effective mode for each agent. When `forceMode` is set, all agents get the same mode. This is correct.

14. **`PrepareReviewFromDiff` - what if `neededModes(cfg)` returns multiple modes?** - The function builds a payloads map with only `diff` mode, regardless of what `neededModes(cfg)` returns. This is intentional - the ingestion path collapses all modes to `diff`.

15. **`PrepareReviewFromDiff` - what about agents that don't support `diff` mode?** - The forceMode overrides the agent's effective mode, so all agents get `diff` mode. If an agent doesn't support `diff` mode (e.g., it's a blocks-only agent), it still gets `diff` mode. This is intentional per the sprint plan: "every agent reviews it regardless of its configured payload mode".

Now let me think about whether there are any security issues:

16. **`BuildEntriesFromDiffFile` - reading arbitrary files** - The `isSafeDiffPath` function rejects absolute paths and `..` traversal. But as noted, symlinks are not resolved. This is a documented limitation.

17. **`BuildEntriesFromDiff` - no file system interaction** - The text variant doesn't read files, so no file system security concerns.

18. **`PrepareReviewFromDiff` - size cap before parsing** - The size cap check prevents parsing of oversized diffs. Good for memory safety.

19. **`PrepareReviewFromDiff` - error messages don't leak sensitive info** - The error messages are generic and don't leak file contents or paths beyond what's necessary.

20. **`PrepareReviewFromDiff` - context handling** - The function uses `context.Background()` in tests. In production, the caller should pass a proper context. This is fine.

Let me also check for performance issues:

21. **`BuildEntriesFromDiff` - O(n) parsing** - The function is O(n) in the diff size. Good.

22. **`BuildEntriesFromDiffFile` - reads entire file into memory** - The function reads the entire diff into memory. The size cap limits this to 10 MiB by default. Good.

23. **`PrepareReviewFromDiff` - builds payloads map** - The function builds a payloads map with one entry. This is O(1). Good.

24. **`splitLinesWithOffsets` - allocates slices** - The function allocates slices for lines and offsets. For a 10 MiB diff, this is significant memory. But it's bounded by the size cap.

25. **`looseSectionStarts` - hunk counting** - The hunk counting loop is O(n) in the number of lines. Good.

Let me also check for correctness issues in the hunk parsing:

26. **`hunkLineCounts` - malformed hunk header** - If the hunk header doesn't match the regex, it defaults to (1, 1). This is a fallback for malformed headers. The comment says "real diffs always carry a well-formed hunk header, and the loose walk only reaches here on a line already beginning with `@@ `". So this fallback is for edge cases.

27. **`looseSectionStarts` - hunk with 0 old lines (pure addition)** - For `@@ -0,0 +1,N @@`, oldCount = 0, newCount = N. The loop decrements newCount for `+` lines and oldCount for `-` lines. Since oldCount starts at 0, the loop continues until newCount reaches 0. Good.

28. **`looseSectionStarts` - hunk with 0 new lines (pure deletion)** - For `@@ -1,N +0,0 @@`, oldCount = N, newCount = 0. The loop decrements oldCount for `-` lines and newCount for `+` lines. Since newCount starts at 0, the loop continues until oldCount reaches 0. Good.

29. **`looseSectionStarts` - hunk with both 0** - For `@@ -0,0 +0,0 @@`, both counts are 0. The loop exits immediately. This is an empty hunk, which is unusual but handled.

30. **`looseSectionStarts` - `\ No newline at end of file`** - The `\` prefix check handles this correctly. The line counts toward neither side.

Now let me think about edge cases in the test files:

31. **`TestPrepareReviewFromDiff_SizeCapRejectsOversized`** - Uses `strings.Repeat("a", int(payload.DefaultMaxDiffBytes)+1)`. This creates a string of `DefaultMaxDiffBytes + 1` bytes. The check `int64(len(diffText)) > payload.DefaultMaxDiffBytes` should catch this. Good.

32. **`TestBuildEntriesFromDiffFile_SizeCapRejectsOversized`** - Creates a file with content larger than the cap (64 bytes). The check `fi.Size() > maxBytes` should catch this. Good.

33. **`TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`** - Tests that a mid-file drop is observable downstream. The budget is 256 bytes, and the middle file is ~2000 bytes. The test expects the middle file to be dropped. Good.

34. **`TestBuildEntriesFromDiff_HunkBodyDoesNotSpoofHeader`** - Regression test for hunk body spoofing. The diff has a hunk body with `--- X` and `+++ Y` lines, which should not be mistaken for file headers. Good.

Now let me think about whether there are any issues I've missed:

35. **`PrepareReviewFromDiff` - what if `req.Root` is empty?** - The function uses `req.Root` for the `.atcr/latest` pointer. If empty, the pointer would be written to the current directory. This matches `PrepareReview`'s behavior.

36. **`PrepareReviewFromDiff` - what if `req.Branch` is empty?** - The manifest records `req.Branch`. If empty, it's recorded as empty. This matches `PrepareReview`'s behavior.

37. **`PrepareReviewFromDiff` - what if `req.Date` is empty?** - Similar to above.

38. **`PrepareReviewFromDiff` - `StartedAt` is used for the run directory name** - If `StartedAt` is zero, the directory name would be based on the zero time. This matches `PrepareReview`'s behavior.

39. **`BuildEntriesFromDiffFile` - what if the file is a named pipe or device?** - `os.Open` would succeed, `f.Stat()` would succeed, `fi.Mode().IsRegular()` would return false. The error message says "is not a regular file". Good.

40. **`BuildEntriesFromDiffFile` - what if the file has Windows line endings?** - The `splitLinesWithOffsets` function splits on `\n` only. CRLF lines would have trailing `\r`. The hunk counting and path parsing handle this correctly (as tested in `TestBuildEntriesFromDiff_CRLFLineEndings`).

Let me now summarize the issues I've found:

**Issue 1 (MEDIUM): Unused env var in test**
File: `internal/fanout/ingest_review_test.go:47`
The test `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` sets `t.Setenv("ATCR_TEST_KEY", "secret")` but never reads it. This is leftover debugging code and should be removed.

**Issue 2 (LOW): Symlink TOCTOU in `BuildEntriesFromDiffFile`**
File: `internal/payload/ingest.go` (the `BuildEntriesFromDiffFile` function)
The function follows symlinks without resolving them. A relative path like `subdir/file.diff` where `subdir` is a symlink to `/etc` would read `/etc/file.diff`. The comment acknowledges this limitation. For the ingestion endpoint, this could be a security concern if untrusted paths are passed. The fix would be to use `os.Lstat` first or resolve symlinks with `filepath.EvalSymlinks`.

**Issue 3 (LOW): `parseDiffPathField` strips `a/` or `b/` unconditionally**
File: `internal/payload/ingest.go` (the `parseDiffPathField` function)
The function strips `a/` or `b/` prefix unconditionally. For loose diffs without prefixes, this is fine. But for git diffs with `--no-prefix`, the paths wouldn't have `a/` or `b/`, so no stripping occurs. For git diffs with the default prefix, stripping is correct. However, if a file path literally starts with `a/` or `b/` (e.g., `a/b.go` where `a` is a directory), after stripping it becomes `b.go`. This is correct because in git diff format, `a/` is the old-side prefix. But it's worth noting that this is a potential source of confusion.

Actually, this is by design and correct. Not an issue.

**Issue 4 (LOW): `splitLinesWithOffsets` doesn't handle `\r\n` correctly for hunk parsing**
Wait, let me reconsider. In CRLF format, the lines have trailing `\r`. The hunk counting checks for `-`, `+`, `\` prefixes. All work with trailing `\r`. The default branch (context line) doesn't check for anything specific. So trailing `\r` is handled correctly.

But what about a CRLF line that's just `\r`? After splitting on `\n`, it becomes `\r`. The prefix checks don't match `-`, `+`, or `\`. So it falls into the default branch and is treated as a context line. This is correct.

Not an issue.

**Issue 5 (LOW): `headPathFromGitHeader` uses LastIndex for ` b/`**
File: `internal/payload/ingest.go` (the `headPathFromGitHeader` function)
The function uses `strings.LastIndex(header, sep)` to find the last ` b/` token. For a path like `a/foo b/bar b/baz`, this would return `b/baz` as the head path. But git diff format uses a specific format: `diff --git a/<old> b/<new>`. The ` b/` token appears exactly once in the header. So LastIndex is fine.

But what if the old path contains ` b/`? Like `diff --git a/foo b/bar b/baz`? This would be a malformed git diff header. The LastIndex approach would return `b/baz` as the head path, which might be incorrect. But this is a malformed input, and the parser would still produce a result (possibly incorrect). This is a corner case that's unlikely in practice.

Not a significant issue.

**Issue 6 (MEDIUM): `BuildEntriesFromDiffFile` error message leaks path**
File: `internal/payload/ingest.go` (the `BuildEntriesFromDiffFile` function)
The error messages include the raw path, which could leak sensitive information in logs. For example, `fmt.Errorf("diff ingestion: diff file %q size %d exceeds max %d bytes", path, fi.Size(), maxBytes)` includes the full path. If the path contains sensitive information (like a username or directory structure), this could be a privacy concern.

However, this is a minor issue and the path is already validated by `isSafeDiffPath`. The error message is useful for debugging.

Not a significant issue.

**Issue 7 (LOW): `PrepareReviewFromDiff` doesn't validate `diffText` for nil**
Wait, `diffText` is a string, so it can't be nil. It can be empty, which is handled by the size cap check and the empty entries check. Good.

Not an issue.

**Issue 8 (LOW): `BuildEntriesFromDiff` - hunk header regex doesn't handle all formats**
File: `internal/payload/ingest.go` (the `hunkCountsRe` regex)
The regex `^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@` handles standard hunk headers. But some diff tools produce headers with additional context, like `@@ -1,3 +1,3 @@ optional section heading`. The regex matches this correctly (the trailing `@@` is optional in the regex). Good.

But wait, the regex has `@@` at the end without a quantifier, so it's required. What about `@@ -1,3 +1,3 @@` without trailing context? The regex matches `@@ -1,3 +1,3 @@` with the trailing `@@` being the literal `@@` at the end. Good.

What about `@@ -1,3 +1,3 @@ optional heading`? The regex matches `@@ -1,3 +1,3 @@` and ignores the rest. Good.

Not an issue.

**Issue 9 (MEDIUM): Test `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` has unused env var**
This is the same as Issue 1. The test sets `t.Setenv("ATCR_TEST_KEY", "secret")` but never reads it. This is leftover debugging code.

**Issue 10 (LOW): `BuildEntriesFromDiffFile` - file descriptor leak on early return**
Wait, the function uses `defer func() { _ = f.Close() }()` which closes the file when the function returns. This is correct.

Not an issue.

Let me reconsider the `t.Setenv("ATCR_TEST_KEY", "secret")` issue. This is clearly a code smell. The env var is set but never used. It should be removed.

Actually, I realize I should look more carefully at the test to see if the env var is used somewhere else. Let me re-read...

```go
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	// Three files; the MIDDLE one is by far the largest, so largest-first byte
	// budgeting drops it specifically.
	fileA := "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	...
```

The env var is set at the beginning and never used. This is definitely leftover debugging code. It should be flagged as a code smell.

Now, let me also check if there are any issues with the `forceMode` parameter in `buildAgent`:

**Issue: `buildAgent` with `forceMode` - what if the agent's effective mode is different from `forceMode`?**
When `forceMode` is non-empty, it overrides the agent's effective mode. The `payloads[mode]` lookup then uses `forceMode`. If `payloads` doesn't have the `forceMode` key, the defensive error is returned.

For the ingestion path, `payloads` only has `diff` key, and `forceMode = diff`. So the lookup succeeds. Good.

For the git-range path, `forceMode = ""`, so `mode = ac.EffectivePayloadMode(cfg.Settings)`. The `payloads` map is built by `neededModes(cfg)` which includes all effective modes. So the lookup succeeds. Good.

Not an issue.

Let me also check if there are any issues with the `prepare` flow:

**Issue: `PrepareReviewFromDiff` - what if `req.Root` doesn't exist?**
The function doesn't validate `req.Root`. If it doesn't exist, the `.atcr/latest` pointer write might fail. But this matches `PrepareReview`'s behavior.

Not an issue.

**Issue: `PrepareReviewFromDiff` - what if `req.OutputDir` already exists?**
The function doesn't check if `req.OutputDir` exists. If it exists and `req.Force` is false, the write might fail. But `finalizePreparedReview` likely handles this (checking `req.Force`).

Not an issue (handled by `finalizePreparedReview`).

Let me now think about whether there are any other issues:

**Issue: `BuildEntriesFromDiff` - what if the diff has only `--- /dev/null` and no `+++ ` line?**
For a pure addition, `--- /dev/null` and `+++ b/new.go`. The `diffSectionPath` function:
- `oldPath = "/dev/null"` (from `parseDiffPathField` which returns `devNull` for `/dev/null`)
- `newPath = "new.go"`

Wait, `parseDiffPathField` for `/dev/null`:
- No tab, so no trimming
- No `\r` suffix, so no trimming
- `field == devNull` is true, return `devNull`

So `oldPath = "/dev/null"`. Then in `diffSectionPath`:
- `newPath != "" && newPath != devNull` → true, return `newPath`

Good. The `/dev/null` old path is not used because `newPath` takes precedence.

But what if both `--- ` and `+++ ` are `/dev/null`? That's a malformed diff. `oldPath = "/dev/null"`, `newPath = "/dev/null"`. Both checks fail, and we fall through to `gitHeader`. If there's no `gitHeader`, we get an error. Good.

**Issue: `BuildEntriesFromDiff` - what if the diff has `--- a/x.go` but no `+++ ` line?**
`oldPath = "x.go"`, `newPath = ""`. Falls back to `oldPath`. Good.

**Issue: `BuildEntriesFromDiff` - what if the diff has `+++ b/x.go` but no `--- ` line?**
`oldPath = ""`, `newPath = "x.go"`. Uses `newPath`. Good.

OK, I think I've covered the main issues. Let me also look at the `PrepareReviewFromDiff` function one more time:

```go
func PrepareReviewFromDiff(ctx context.Context, cfg *ReviewConfig, req ReviewRequest, diffText string) (*PreparedReview, error) {
	if len(rosterNames(cfg.Project)) == 0 {
		return nil, ErrEmptyRoster
	}
	if req.OutputDir != "" && req.IDOverride != "" {
		return nil, fmt.Errorf("--output-dir and --id are mutually exclusive")
	}
	if int64(len(diffText)) > payload.DefaultMaxDiffBytes {
		return nil, fmt.Errorf("diff ingestion: diff size %d exceeds max %d bytes", len(diffText), payload.DefaultMaxDiffBytes)
	}
	entries, err := payload.BuildEntriesFromDiff(diffText)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: the diff contains no reviewable files", ErrNoReviewableContent)
	}
	kept, trunc := payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)
	if trunc.AllDropped {
		return nil, fmt.Errorf("%w (mode diff, dropped %d file(s))", ErrPayloadFullyDropped, len(trunc.FilesDropped))
	}
	var b strings.Builder
	for _, e := range kept {
		b.WriteString(e.Body)
	}
	diffMode := string(payload.ModeDiff)
	payloads := map[string]modePayload{
		diffMode: {Text: b.String(), FileCount: len(kept), Truncation: trunc},
	}
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, diffMode)
	if err != nil {
		return nil, err
	}
	return finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, diffMode)
}
```

Wait, there's a potential issue here. The function uses `payloads` map but doesn't call `neededModes(cfg)`. For the git-range path, `neededModes(cfg)` is called to build the payloads map. For the ingestion path, the payloads map only has `diff` mode. This is intentional per the sprint plan.

But what if `neededModes(cfg)` would have returned additional modes? The ingestion path ignores them. This is intentional - the diff is the only available representation.

Not an issue (by design).

Let me also check if there are any issues with the error handling:

**Issue: `PrepareReviewFromDiff` - error from `payload.BuildEntriesFromDiff` is wrapped?**
No, it's returned directly: `return nil, err`. The error from `BuildEntriesFromDiff` already has context (e.g., "diff ingestion: no file sections found"). So wrapping is not necessary. Good.

**Issue: `PrepareReviewFromDiff` - error from `buildSlots` is wrapped?**
No, it's returned directly. The error from `buildSlots` should have context. Good.

**Issue: `PrepareReviewFromDiff` - error from `finalizePreparedReview` is wrapped?**
No, it's returned directly. Good.

OK, I think the main issues are:

1. **MEDIUM**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - leftover debugging code.

2. **LOW**: Symlink TOCTOU in `BuildEntriesFromDiffFile` - documented limitation.

Let me also check if there are any issues with the `BuildEntriesFromDiff` function and the `splitLinesWithOffsets` function:

**Issue: `splitLinesWithOffsets` - empty string input**
For input `""`, the loop doesn't execute, and we append `""` to lines and `0` to offsets. So `lines = [""]`, `offsets = [0]`. This is a single empty line. The caller checks for empty input before calling, so this is fine.

But wait, `BuildEntriesFromDiff` checks `strings.TrimSpace(diffText) == ""` and returns zero entries. So `splitLinesWithOffsets` is never called with empty input. Good.

**Issue: `splitLinesWithOffsets` - input with only newlines**
For input `"\n"`, lines = `["", ""]`, offsets = `[0, 1]`. Two empty lines. The `fileSectionStarts` function would find no markers. In `looseSectionStarts`, the first line is empty, so `looseHeaderAt` fails. Returns error. Good.

Actually wait, for `"\n\n"`, lines = `["", "", ""]`, offsets = `[0, 1, 2]`. Three empty lines. `looseHeaderAt` fails on the first line. Returns error. Good.

For `"---\n+++\n@@\n"`, lines = `["---", "+++", "@@", ""]`, offsets = `[0, 4, 8, 11]`. `looseHeaderAt` at line 0: `strings.HasPrefix("---", "--- ")` - wait, the marker is `--- ` (with trailing space). `"---"` doesn't start with `"--- "` because there's no space. So `looseHeaderAt` fails. Returns error. Good - this is not a valid diff.

For `"--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"`, lines = `["--- a/x.go", "+++ b/x.go", "@@ -1,1 +1,1 @@", "-a", "+b", ""]`, offsets = `[0, 10, 21, 37, 40, 44]`. `looseHeaderAt` at line 0: all three checks pass. `starts = [0]`. `i = 2`. Line 2 starts with `@@`. Parse hunk: oldCount = 1, newCount = 1. `i = 3`. Line 3 is `-a`, decrement oldCount to 0. `i = 4`. Line 4 is `+b`, decrement newCount to 0. `i = 5`. Both counts are 0, exit loop. Line 5 is empty and `i == n-1`, break. Return `starts = [0]`. Good.

Wait, there's a subtle issue. In the hunk counting loop, the condition is `for i < n && (oldRem > 0 || newRem > 0)`. After consuming the hunk body, we check `for i < n && strings.HasPrefix(lines[i], hunkMarker)`. If the next line is another hunk header, we process it. If not, we exit and look for the next file header.

But what if there's a non-hunk, non-header line after the hunk body? Like a stray `--- ` line? In the hunk counting loop, the line is consumed as a body line (or fails if counts are exhausted). If counts are exhausted, the loop exits. Then we check for the next hunk header or file header.

Actually, let me re-read the loop:

```go
for i < n && strings.HasPrefix(lines[i], hunkMarker) {
    oldRem, newRem := hunkLineCounts(lines[i])
    i++ // past the `@@ ` header
    for i < n && (oldRem > 0 || newRem > 0) {
        ...
        i++
    }
}
```

After the inner loop exits (either counts are 0 or i >= n), we go back to the outer loop. The outer loop checks `i < n && strings.HasPrefix(lines[i], hunkMarker)`. If the next line is a hunk header, we process it. If not, we exit the outer loop.

Then the outer loop continues:
```go
for i < n {
    // Tolerate a single trailing empty line produced by a final newline.
    if lines[i] == "" && i == n-1 {
        break
    }
    if !looseHeaderAt(lines, i) {
        if len(starts) == 0 {
            return nil, fmt.Errorf("diff ingestion: no file sections found (expected a `--- `/`+++ `/`@@ ` header triple)")
        }
        return nil, fmt.Errorf("diff ingestion: unexpected content at line %d (not a file header or hunk)", i+1)
    }
    ...
}
```

So if there's a stray `--- ` line after the hunk body, and it's not followed by `+++ ` and `@@ `, we get an error. Good.

But what if the stray `--- ` line is followed by `+++ ` and `@@ `? Then it's treated as a new file header. This is correct behavior for a multi-file diff.

OK, I think the logic is sound. Let me finalize my findings.

Actually, let me reconsider the `t.Setenv` issue one more time. Looking at the test:

```go
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
```

This is in the test file `internal/fanout/ingest_review_test.go`. The env var is set but never read. This is definitely leftover debugging code. It should be flagged.

But wait - could this be intentional? Maybe the test is checking that env vars don't affect the ingestion path? But there's no assertion about the env var. So it's just leftover code.

Actually, I realize I should also check if `t.Setenv` has any side effects. `t.Setenv` sets the env var and registers a cleanup to restore it. If the env var is set to `"secret"`, it could leak to child processes or be visible to other tests. But since `t.Setenv` uses `t.Cleanup` to restore, the env var is cleaned up after the test. So the only issue is the unused env var itself.

Let me also check if there's a pattern of setting unused env vars in other tests. Looking at the diff, only this one test has `t.Setenv`. So it's an outlier.

OK, I'm confident this is a real issue.

Let me also reconsider the symlink issue. The `BuildEntriesFromDiffFile` function:
1. Validates the path lexically (rejects absolute and `..`)
2. Opens the file (follows symlinks)
3. Stats the file
4. Reads the file

The symlink issue is that step 2 follows symlinks. If the path is `subdir/file.diff` and `subdir` is a symlink to `/etc`, then opening `subdir/file.diff` reads `/etc/file.diff`.

The comment acknowledges this: "Like isSafeRelPath, it is a lexical check: it does NOT resolve symlinks, so a relative in-tree path whose component is a symlink pointing outside the tree is out of this guard's scope — callers ingesting untrusted suite trees should pre-resolve paths if that matters."

So this is a documented limitation. For the epic's use case (ingesting benchmark case diffs), the paths are typically `internal/benchmark/testdata/suite-valid/case-01.diff`, which are trusted. So the symlink issue is not a practical concern.

But for a code review, I should flag it as a potential security concern.

Actually, looking at the sprint plan more carefully:

> - Add a builder that takes diff text (or a diff-file path) and produces the same `[]FileEntry` shape `BuildEntries` returns, reusing the existing diff-parsing/budget internals so the output is byte-identical to a git-sourced payload for the same diff.
> - Honor the existing safety guards: bound the read by `benchmark.MaxDiffBytes`-style cap and reject path traversal (mirror `isSafeRelPath`) when given a file path.

The sprint plan says to "reject path traversal (mirror `isSafeRelPath`)". The `isSafeDiffPath` function does exactly this. The symlink issue is beyond what `isSafeRelPath` does, so it's consistent with the sprint plan.

So the symlink issue is not in scope for this epic. I should not flag it.

Let me finalize my findings:

1. **MEDIUM**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - leftover debugging code.

This is the only real issue I've found that's within the sprint plan's scope.

Wait, let me look more carefully at the diff for any other issues I might have missed.

Looking at `internal/fanout/review.go` again:

```go
func buildAgent(cfg *ReviewConfig, name string, payloads map[string]modePayload, rng ReviewRange, forceMode string) (Agent, string, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, "", fmt.Errorf("agent %q not found in registry", name)
	}
	mode := forceMode
	if mode == "" {
		mode = ac.EffectivePayloadMode(cfg.Settings)
	}
	mp, ok := payloads[mode]
	if !ok {
		// Defensive: payloads is built by neededModes over the same roster, so a
		// missing key is a configuration error the caller must fix...
	}
```

When `forceMode` is non-empty, `mode = forceMode`. The `payloads[mode]` lookup then uses `forceMode`. If `payloads` doesn't have the `forceMode` key, the defensive error is returned.

But wait - when `forceMode` is non-empty, the caller is responsible for ensuring `payloads` has the `forceMode` key. For `PrepareReviewFromDiff`, `payloads` only has `diff` key, and `forceMode = diff`. So the lookup succeeds.

But what if a caller passes `forceMode = "blocks"` with a payloads map that only has `diff`? The lookup would fail. This is a defensive check that's correct.

Not an issue.

Looking at the test file `internal/fanout/ingest_review_test.go` more carefully:

```go
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
```

This is the only test that sets an env var. It's unused. This is a real issue.

Let me also check if there are any issues with the `diffReq` helper:

```go
func diffReq(root, out string) ReviewRequest {
	return ReviewRequest{
		Root:       root,
		OutputDir:  out,
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}
```

This is a test helper. It creates a `ReviewRequest` with an explicit `OutputDir`. Good.

But in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`, the test uses a raw `ReviewRequest` literal instead of `diffReq`:

```go
root := t.TempDir()
req := ReviewRequest{
    Root: root, Branch: "feature/test", Date: "2026-06-10",
    TimeSuffix: "120000", StartedAt: time.Unix(1000, 0).UTC(),
}
```

This doesn't set `OutputDir`, so it uses the default path. This is intentional for testing the default path with `.atcr/latest` repoint. Good.

But the test also sets `t.Setenv("ATCR_TEST_KEY", "secret")` which is unused. This is the issue.

Let me also check `TestPrepareReviewFromDiff_FixtureEndToEnd`:

```go
func TestPrepareReviewFromDiff_FixtureEndToEnd(t *testing.T) {
	fixture := readFixture(t, "case-01.diff")
	cfg := twoAgentConfig("http://unused")
	root := t.TempDir()
	req := ReviewRequest{
		Root:       root,
		Range:      ReviewRange{},
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
```

This test uses the default path (no `OutputDir`). It reads the fixture and feeds it through `PrepareReviewFromDiff`. Good.

OK, I'm confident in my findings. Let me write them up.

Actually, wait. Let me look at one more thing. The `PrepareReviewFromDiff` function has a comment:

```go
// req.Range is provenance-only here and may be left zero (a range-less diff has no
// base/head); req.OutputDir/IDOverride/Force are honored identically to
// PrepareReview, so callers (e.g. a benchmark run) can redirect output.
```

And the function checks:
```go
if req.OutputDir != "" && req.IDOverride != "" {
    return nil, fmt.Errorf("--output-dir and --id are mutually exclusive")
}
```

This is a pre-check before the expensive work. But is this check also in `finalizePreparedReview`? If so, it's duplicated. Let me check...

Looking at the diff, `finalizePreparedReview` is the shared tail. It likely handles the output dir logic. The pre-check in `PrepareReviewFromDiff` is a fast-fail before parsing the diff. Good.

But wait - `PrepareReview` also calls `finalizePreparedReview`. Does `PrepareReview` have the same pre-check? Let me look at the diff...

The diff shows `PrepareReview` calling `buildSlots` and then `finalizePreparedReview`. There's no pre-check for `req.OutputDir != "" && req.IDOverride != ""` in the visible part of `PrepareReview`. So the pre-check in `PrepareReviewFromDiff` might be new.

Actually, the pre-check might be in `finalizePreparedReview` itself. The diff doesn't show the full `finalizePreparedReview` function, so I can't tell. But if the pre-check is duplicated, it's a minor code smell, not a bug.

Not a significant issue.

Let me finalize:

1. **MEDIUM**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` (line 47 of `internal/fanout/ingest_review_test.go`).

Actually, let me look at the line number more carefully. The test starts at line 47 (the `t.Setenv` line). Let me count...

The test file starts with package declaration, imports, and the `looseDiff` constant. Then `diffReq` helper. Then `TestPrepareReviewFromDiff_ScaffoldsAndForcesDiffMode` (lines 32-77 or so). Then `TestPrepareReviewFromDiff_EmptyDiffRejected`. Then `TestPrepareReviewFromDiff_BudgetDropsAllErrors`. Then `TestPrepareReviewFromDiff_SizeCapRejectsOversized`. Then `TestPrepareReviewFromDiff_EmptyRosterRejected`. Then `TestPrepareReviewFromDiff_MalformedDiffPropagates`. Then `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation`.

Looking at the diff:
```
+// When the byte budget drops a NON-LAST file from a multi-file diff (independent
+// review MEDIUM), the elision must be observable downstream: the agents' status
+// records the dropped file, and the materialized payload contains only the kept
+// files — the same truncation contract the git-range path honors.
+func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
+	t.Setenv("ATCR_TEST_KEY", "secret")
```

The `t.Setenv` line is the first line after the function declaration. In the new file, this would be around line 162 or so (based on the diff structure). Let me estimate...

The file has 241 lines total. The test `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` starts around line 160-170. The `t.Setenv` line would be around line 162.

I'll use line 162 as an approximation, but the exact line number depends on the file structure. Let me use a more precise estimate.

Actually, looking at the diff more carefully:

```
+// When the byte budget drops a NON-LAST file from a multi-file diff (independent
+// review MEDIUM), the elision must be observable downstream: the agents' status
+// records the dropped file, and the materialized payload contains only the kept
+// files — the same truncation contract the git-range path honors.
+func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
+	t.Setenv("ATCR_TEST_KEY", "secret")
```

These lines are added in the diff. The `t.Setenv` line is the 3rd line of the test function. The exact line number in the file depends on preceding content.

Let me count from the diff:
- Lines 1-12: package, imports, looseDiff constant
- Lines 13-23: diffReq helper
- Lines 24-68: TestPrepareReviewFromDiff_ScaffoldsAndForcesDiffMode (approximately)
- Lines 69-79: TestPrepareReviewFromDiff_EmptyDiffRejected
- Lines 80-92: TestPrepareReviewFromDiff_BudgetDropsAllErrors
- Lines 93-107: TestPrepareReviewFromDiff_SizeCapRejectsOversized
- Lines 108-118: TestPrepareReviewFromDiff_EmptyRosterRejected
- Lines 119-129: TestPrepareReviewFromDiff_MalformedDiffPropagates
- Lines 130-162: TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation (approximately)

So the `t.Setenv` line would be around line 162. I'll use line 162 as the line number.

Actually, I realize I should be more careful. Let me count the lines in the diff more precisely.

The diff shows:
```
+func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
+	t.Setenv("ATCR_TEST_KEY", "secret")
```

These are two consecutive `+` lines. The `t.Setenv` line is one line after the function declaration. In the file, this would be at a specific line number.

Without being able to count exactly, I'll estimate the line number as approximately 162. The exact number isn't critical for the finding.

OK, let me finalize my findings. I'll report:

1. **MEDIUM**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in test - leftover debugging code.

Actually, I want to double-check one more thing. Let me look at the `PrepareReviewFromDiff` function and see if there's a potential issue with the `buildSlots` call:

```go
slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, diffMode)
```

When `forceMode = diffMode`, `buildSlots` calls `buildAgent(cfg, name, payloads, rng, forceMode)`. `buildAgent` uses `forceMode` as the mode. Then `payloads[mode]` = `payloads[diffMode]`, which exists. Good.

But what if `cfg.Registry.Agents[name]` doesn't exist? `buildAgent` returns an error. `buildSlots` propagates it. `PrepareReviewFromDiff` returns it. Good.

What if an agent's `EffectivePayloadMode(cfg.Settings)` would return a mode not in `payloads`? With `forceMode = diffMode`, this is overridden. So the issue doesn't arise. Good.

OK, I'm confident in my findings. Let me write them up.

Actually, one more thing. Let me check the `buildSlots` function signature change:

```go
func buildSlots(cfg *ReviewConfig, payloads map[string]modePayload, rng ReviewRange, forceMode string) ([]Slot, map[string]string, error) {
```

The new parameter is `forceMode string`. This is added at the end, which is a non-breaking change for existing callers (they pass `""`). Good.

But wait - the diff shows callers being updated:
- `resume.go`: `buildSlots(cfg, payloads, req.Range, "")`
- `retry_wiring_test.go`: `buildAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "")`
- `review_scope_inject_test.go`: `buildAgent(cfg, "greta", payloads, rng, "")`
- `review_test.go`: `buildAgent(cfg, "greta", map[string]modePayload{}, ReviewRange{Base: "a", Head: "b"}, "")`
- `review_tools_test.go`: `buildAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "")`

All callers are updated to pass `""` for `forceMode`. Good.

And `buildAgent` callers:
- `retry_wiring_test.go`: updated
- `review_scope_inject_test.go`: updated
- `review_test.go`: updated
- `review_tools_test.go`: updated

All updated. Good.

OK, I'm done. Let me write the final findings.

Wait, I want to reconsider one more time whether the `t.Setenv` is really an issue. Could it be that the test is checking that the ingestion path doesn't require an API key? But there's no assertion about the env var. So it's just unused code.

Actually, could it be that the test is setting the env var to prevent some other code from failing? Like, maybe `twoAgentConfig` reads the env var and would fail without it? But the test doesn't check for any failure related to the env var. And the test passes (it calls `require.NoError(t, err)`).

So the `t.Setenv` is truly unused. It's leftover debugging code.

OK, final findings:

1. **MEDIUM**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - leftover debugging code.

Hmm, actually let me reconsider the severity. This is a test file, and the unused env var doesn't affect functionality. It's a code smell, not a functional bug. I'll rate it as LOW or MEDIUM.

Given that it's a test file and the issue is minor (leftover debugging code), I'll rate it as LOW.

Actually, looking at it again, the `t.Setenv` could be a security concern if `ATCR_TEST_KEY` is a real key name and the value `"secret"` is a placeholder. But since it's clearly a test placeholder, it's not a real security issue.

I'll rate it as LOW (code smell, not a functional bug).

Let me also check if there are any other issues I might have missed in the `BuildEntriesFromDiff` function:

**Issue: `fileSectionStarts` - `gitMode` detection**
The function iterates over all lines to detect `git diff` format. For a large diff (10 MiB), this is O(n). But it's a one-time scan, so it's acceptable.

**Issue: `looseSectionStarts` - `looseHeaderAt` check**
The function checks for the `--- `/`+++ `/`@@ ` triple. If the `--- ` and `+++ ` lines are not immediately followed by `@@ `, it's not a valid header. Good.

**Issue: `looseSectionStarts` - hunk with 0 counts**
For `@@ -0,0 +0,0 @@`, both counts are 0. The inner loop doesn't execute. The outer loop continues. Good.

**Issue: `looseSectionStarts` - hunk with no `@@ ` after headers**
If `--- `/`+++ ` are followed by a non-`@@ ` line, the outer loop's `looseHeaderAt` check fails (because `i+2 < n && strings.HasPrefix(lines[i+2], hunkMarker)` fails). Returns error. Good.

OK, I'm done. Final findings:

1. **LOW**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - leftover debugging code.

Actually, I realize I should also check if there's a potential issue with the `parseDiffPathField` function and paths with `a/` or `b/` that are part of the actual path:

**Issue: `parseDiffPathField` - path `a/b/c.go`**
Input: `a/b/c.go` (after stripping `a/` prefix, becomes `b/c.go`). This is correct - `a/` is the diff prefix, `b/c.go` is the actual path.

But what if the path is literally `a/b/c.go` without the diff prefix? Like in a loose diff where the header is `+++ a/b/c.go`? After stripping `a/`, we get `b/c.go`. This is incorrect - the actual path is `a/b/c.go`.

Wait, but loose diffs typically don't have `a/` or `b/` prefixes. The `parseDiffPathField` function is designed for git diff format. For loose diffs, the path wouldn't have the prefix, so no stripping occurs.

But if someone uses a loose diff with `a/` or `b/` prefixes (unusual but possible), the stripping would be incorrect. This is a corner case.

Not a significant issue (by design for git diff format).

OK, truly final findings:

1. **LOW**: Unused `t.Setenv("ATCR_TEST_KEY", "secret")` in `TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation` - leftover debugging code.

Hmm, actually I want to look one more time at the test to make sure I'm not missing something.

```go
// When the byte budget drops a NON-LAST file from a multi-file diff (independent
// review MEDIUM), the elision must be observable downstream: the agents' status
// records the dropped file, and the materialized payload contains only the kept
// files — the same truncation contract the git-range path honors.
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	// Three files; the MIDDLE one is by far the largest, so largest-first byte
	// budgeting drops it specifically.
	fileA := "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	fileMid := "--- a/mid.go\n+++ b/mid.go\n@@ -1,1 +1,1 @@\n-" + strings.Repeat("x", 2000) + "\n+" + strings.Repeat("y", 2000) + "\n"
	fileZ := "--- a/z.go\n+++ b/z.go\n@@ -1,1 +1,1 @@\n-y\n+z\n"
	diff := fileA + fileMid + fileZ

	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 256 // fits a.go + z.go, not mid.go
	root := t.TempDir()
	req := ReviewRequest{
		Root: root, Branch: "feature/test", Date: "2026-06-10",
		TimeSuffix: "120000", StartedAt: time.Unix(1000, 0).UTC(),
	}

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, diff)
	require.NoError(t, err)
	...
```

The `t.Setenv("ATCR_TEST_KEY", "secret")` is at the beginning of the function. The value is `"secret"` which is a placeholder. The env var is never read. This is definitely leftover debugging code.

I'll flag it as LOW (code smell, not a functional bug).

Actually, thinking about it more, this could be a MEDIUM issue if `ATCR_TEST_KEY` is a sensitive env var name. Setting it to `"secret"` in a test could:
1. Pollute the test environment
2. Cause confusion if someone reads the test
3. Be a security concern if the value is logged or leaked

But since the value is clearly a placeholder,it's not a real security issue. I'll keep it as LOW.

OK, truly truly final. Let me write the output.
</think>

LOW|internal/fanout/ingest_review_test.go:162|Unused t.Setenv("ATCR_TEST_KEY", "secret") in test|Remove the unused env var line — it is set but never read|correctness|2|Test sets env var with no read; leftover debug code|claude