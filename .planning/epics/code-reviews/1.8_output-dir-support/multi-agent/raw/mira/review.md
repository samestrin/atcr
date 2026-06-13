<think>
Let me analyze the diff carefully against the sprint plan scope. The sprint plan is about adding `--output-dir` flag to `atcr review` command. Let me focus on the files that are in scope and the changes related to this feature.

In-scope files (based on sprint plan):
- `cmd/atcr/review.go` - new flag, wiring into review directory creation
- `cmd/atcr/review_test.go` - tests for flag parsing
- `cmd/atcr/reconcile_test.go` - tests for reconcile inheriting output dir
- `internal/fanout/review.go` - OutputDir field, PrepareReview changes
- `internal/fanout/review_test.go` - tests
- `internal/fanout/reviewdir.go` - ScaffoldOutputDir
- `internal/fanout/reviewdir_test.go` - tests
- `README.md` - docs

Out-of-scope files (not mentioned in sprint plan):
- `docs/registry.md` - changes to payload_byte_budget docs
- `internal/fanout/outcome.go` - change to summarize for unknown status
- `internal/fanout/outcome_test.go` - test for unknown status
- `internal/payload/budget.go` - AllDropped field
- `internal/payload/budget_test.go` - tests for AllDropped
- `internal/reconcile/ambiguous.go` - change from "" return to panic
- `internal/registry/project.go` - default config changes

These out-of-scope changes should be marked as such in my review.

Let me analyze the in-scope changes:

### `cmd/atcr/review.go`

1. Added `errors` and `path/filepath` imports - OK
2. Added `--output-dir` flag - OK
3. `outputDirFromFlags` function - good separation
4. Calls `outputDirFromFlags` early in `runReview` - OK
5. Sets `OutputDir` on `ReviewRequest` - OK

Let me check for issues:

1. **The `runReview` function** - I should look at the original code. The ID override is also being checked. Let me look at the logic:
   - `idOverride, _ := cmd.Flags().GetString("id")` - gets id
   - `outputDir, err := outputDirFromFlags(cmd)` - resolves output dir
   - These are checked for mutual exclusion in `outputDirFromFlags`
   
   Wait, the order matters. In `outputDirFromFlags`, it first checks if `cmd.Flags().Changed("output-dir")` and then checks if `cmd.Flags().Changed("id")`. The order is fine because cobra can tell us which flags were set.
   
   But wait - what if `--id` is set but `--output-dir` is NOT? Then `outputDirFromFlags` returns "", nil. But `idOverride` is already set. This is fine - it's the existing behavior.

2. **`outputDirFromFlags` checks mutual exclusion in the wrong direction?** Let me re-read:
   ```go
   if !cmd.Flags().Changed("output-dir") {
       return "", nil
   }
   if cmd.Flags().Changed("id") {
       return "", usageError(errors.New("--output-dir and --id are mutually exclusive"))
   }
   ```
   This looks correct - if both are set, error. If only `--output-dir` is set, proceed. If only `--id` is set, no error here.

3. **ID Override is used later in `review.go` but not passed to the request**: Looking at the code, the `IDOverride` is passed as `idOverride` to `ReviewRequest.IDOverride`. That's correct.

4. **The OutputDir is set after IDOverride** - this is fine.

### `internal/fanout/review.go`

The PrepareReview function now has a switch:
```go
switch {
case req.OutputDir != "":
    dir, err = ScaffoldOutputDir(req.OutputDir)
case req.IDOverride != "":
    dir, err = ScaffoldReviewDir(req.Root, id)
default:
    // ... derived ids claim their directory atomically
}
```

This is good. The OutputDir takes precedence over IDOverride.

But wait - if both `OutputDir` and `IDOverride` are set, OutputDir wins. The CLI prevents this, but defensively, the order is OutputDir first, then IDOverride. This means a user setting both would silently get OutputDir behavior. The CLI prevents this, but it's worth noting.

### `internal/fanout/reviewdir.go`

`ScaffoldOutputDir`:
```go
switch entries, err := os.ReadDir(dir); {
case err == nil && len(entries) > 0:
    return "", fmt.Errorf("output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)", dir)
case err != nil && !errors.Is(err, fs.ErrNotExist):
    return "", fmt.Errorf("failed to create review directory: %w", err)
}
if err := os.MkdirAll(dir, 0o755); err != nil {
    return "", fmt.Errorf("failed to create review directory: %w", err)
}
```

This handles:
- Existing empty dir - proceeds to MkdirAll (idempotent)
- Existing non-empty dir - returns error
- Non-existent path - falls through to MkdirAll
- Other errors (permission, file at path) - returns error

This is good. But what about a symlink? `os.ReadDir` follows symlinks. If the path is a symlink to a non-empty dir, the check would pass through correctly.

Let me think about this... `os.ReadDir(dir)` will follow symlinks at `dir`. If `dir` is a symlink, ReadDir reads the target. Then MkdirAll on `dir` (a symlink) - what happens? `os.MkdirAll` will create the dir if it doesn't exist, or do nothing if it does. If `dir` is a symlink to a real directory, MkdirAll succeeds and the symlink is preserved. But the subdirs would be created inside the target. Hmm, this might be OK behavior.

### `internal/fanout/review_test.go`

The test `TestRunReview_OutputDirSkipsLatest` looks reasonable. It checks that:
- `res.Dir == out`
- subdirs exist
- `ReadLatest` returns error

### `cmd/atcr/reconcile_test.go`

`TestReconcileCmd_InheritsExternalOutputDir` - sets up an external review dir and runs reconcile on it. Looks good.

### Now for out-of-scope changes - these shouldn't be flagged

Wait, let me re-read the scope constraint: "if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

So I should only include IN-SCOPE issues. Let me focus.

Looking at the in-scope code more carefully for issues:

### `cmd/atcr/review.go` line 49-50:

```go
dir, _ := cmd.Flags().GetString("output-dir")
if strings.TrimSpace(dir) == "" {
```

Wait, `GetString` returns a value and an error. The error is being ignored. For cobra flag getters, the error is typically only set if the flag isn't registered. Since we just registered it, this should never fail. But this is a minor code smell.

### Error message format check:

`"--output-dir and --id are mutually exclusive"` - this is a bit terse. The CLI could provide more context. But this is minor.

### `internal/fanout/reviewdir.go` line 235-240:

```go
case err == nil && len(entries) > 0:
    return "", fmt.Errorf("output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)", dir)
case err != nil && !errors.Is(err, fs.ErrNotExist):
    return "", fmt.Errorf("failed to create review directory: %w", err)
```

What if `os.ReadDir(dir)` returns `fs.ErrNotExist`? The switch falls through to MkdirAll. That's correct.

What if there's a permission error opening the directory? `os.ReadDir` returns an error that's not `fs.ErrNotExist`. The second case catches it. Good.

What if the path exists but is a regular file? `os.ReadDir(file)` returns an error. The second case catches it. Good - and the test confirms this.

But wait - let me think about race conditions. Between `os.ReadDir` and `os.MkdirAll`, someone could create a file. The MkdirAll on an existing dir is a no-op, so the review would proceed to write to a non-empty directory. This is a TOCTOU race. However, this is a local filesystem operation and the window is microseconds, so it's probably acceptable. Most filesystems don't have this kind of atomic guarantee anyway. The CLI also documents this in risks: "Non-empty directory check misses edge cases (symlinks, hidden files)".

Actually, looking more carefully - what about the order of `os.ReadDir` and `os.MkdirAll`? If dir doesn't exist, we go to MkdirAll which creates it. If it exists and is empty, MkdirAll is a no-op (returns nil). If it exists and is non-empty, we error out.

Wait, there's actually a subtle issue. What if the directory exists but is a "hidden" directory like `.`? Or what about `os.MkdirAll` on an empty dir? It returns nil (no-op). What about on a non-empty dir? It returns nil. So the logic is correct: the readdir check happens first, then mkdirall is called.

But what about a symlink pointing to an empty dir? ReadDir would read the target, find it empty, return nil. MkdirAll on a symlink to a dir would do nothing. Then we'd write into the symlink target, which is fine.

What about a broken symlink? ReadDir would error. The error would be... I'm not sure if it'd be ErrNotExist or some other error. If it's ErrNotExist, we'd fall through to MkdirAll. But MkdirAll on a broken symlink to a dir would either fail or... actually, MkdirAll doesn't follow symlinks the same way. If the symlink points to a non-existent target, MkdirAll would create the target. This is a corner case.

These are all edge cases. Let me not flag them unless they're clear bugs.

### Looking at the test `TestRunReview_OutputDirSkipsLatest`:

```go
out := filepath.Join(t.TempDir(), "ext-review")
req := reviewReq(repo, repo, base, head)
req.OutputDir = out
```

The test uses `t.TempDir()` which creates a unique temp dir. This should be safe.

```go
_, lerr := ReadLatest(repo)
assert.Error(t, lerr, "--output-dir must not update .atcr/latest")
```

This checks that `ReadLatest` errors when `.atcr/latest` doesn't exist. But what if the test framework has already created a `.atcr/latest` for some reason? Let me think... The test uses a fresh `t.TempDir()` for the repo. Looking at `initRepo` - it likely creates a fresh git repo. So `.atcr/` should not exist.

Actually, the `reviewReq` function might initialize some state. Let me check... without seeing the full code, I have to trust the existing test patterns.

### Now let me think about potential issues more broadly

1. **Documentation alignment with sprint plan**: The sprint plan says "fail with exit 2 if it contains files". The implementation uses `os.ReadDir` and checks for "any entry". This is consistent.

2. **Path resolution**: The sprint plan says "Relative --output-dir resolved to absolute via filepath.Abs at flag-parse time." This is implemented.

3. **Mutual exclusion**: "Mutual exclusion (`--output-dir` + `--id`) enforced in `runReview` via `cmd.Flags().Changed()` and returned through `usageError()` so it maps to exit 2." This is implemented correctly.

4. **Skip .atcr/latest**: The sprint plan says "SKIP WriteLatest" when OutputDir is set. This is implemented.

5. **Test coverage**: The sprint plan requires "Unit tests cover the flag parsing, path validation, and the `.atcr/latest` skip logic." The tests are added.

### Let me look at `runReview` more carefully:

Looking at the original code I can infer:
```go
func runReview(cmd *cobra.Command, _ []string) error {
    // ... resolve range, load config
    idOverride, _ := cmd.Flags().GetString("id")
    
    // ... new code
    outputDir, err := outputDirFromFlags(cmd)
    if err != nil {
        return err
    }
    
    // ... validate --fail-on
    // ... build request
    req := fanout.ReviewRequest{
        // ...
        IDOverride: idOverride,
        OutputDir:  outputDir,
    }
}
```

Wait, what if `idOverride` is set AND `outputDir` is also set? The CLI prevents this via mutual exclusion. But defensively, what happens?
- `outputDirFromFlags` returns error if both are set, so the function returns early.
- This is fine.

### One potential issue: ordering of `outputDirFromFlags` vs `idOverride`

In the new code:
```go
idOverride, _ := cmd.Flags().GetString("id")

// Resolve --output-dir (mutually exclusive with --id, relative→absolute)
outputDir, err := outputDirFromFlags(cmd)
```

The mutual exclusion check is in `outputDirFromFlags`. If the user passes both flags, the error is returned, but the function still reads `idOverride` from the flags. This is harmless since we return early.

### Looking at the error message handling:

The error from `outputDirFromFlags` is returned directly. If it's a `usageError`, it should map to exit 2. The test confirms this:
```go
require.Equal(t, 2, exitCode(err))
```

### Let me look for potential security issues:

The `ScaffoldOutputDir` function takes an arbitrary path from the user. The CLI resolves it to absolute. Then `os.ReadDir` and `os.MkdirAll` are called. If the path is `/` or some critical system directory, this could be a problem. But this is a CLI tool, and the user has already chosen the path. Not a security issue per se.

What about path traversal? If the user passes `../../etc/`, this resolves to an absolute path. The tool would try to create `/etc/...` which would likely fail with permission denied. Not really an issue.

What about TOCTOU on the directory check? Already discussed.

### Now for the out-of-scope items - I should not flag them, but I notice some concerns:

- `outcome.go` - the `default` case in summarize. This is actually a defensive change. Let me see... Before, the switch would silently ignore unknown statuses (no default case). After, unknown statuses are counted as Failed. The test `TestSummarize_UnknownStatusCountsAsFailed` confirms this. This is a correctness fix, but it's out of scope.

- `budget.go` - adding `AllDropped` field. This is a new feature, not in scope.

- `ambiguous.go` - changing from "return empty string" to "panic" on JSON render error. This is a defensiveness change. The comment says "unreachable: AmbiguousCluster marshals without error" - so a panic is acceptable. Out of scope.

- `project.go` and `registry.md` - changes to payload_byte_budget docs. Out of scope.

### Let me also check if there are any issues with the test `TestReconcileCmd_InheritsExternalOutputDir`:

```go
ext := filepath.Join(t.TempDir(), "ext-review")
require.NoError(t, os.MkdirAll(filepath.Join(ext, "sources", "host"), 0o755))
require.NoError(t, os.MkdirAll(filepath.Join(ext, "reconciled"), 0o755))
require.NoError(t, os.WriteFile(filepath.Join(ext, "sources", "host", "findings.txt"),
    []byte("# atcr-findings/v1\nHIGH|a.go:1|boom|fix|security|10|ev|host\n"), 0o644))

require.Equal(t, 0, execCmd(t, "reconcile", ext))
require.FileExists(t, filepath.Join(ext, "reconciled", "findings.txt"))
```

This test sets up a manual review dir and runs reconcile on it. It should work, but it depends on `execCmd` and the `reconcile` command's handling of the path argument. The reconcile command already accepts paths.

But wait - does reconcile expect a manifest.json? Looking at the test setup, there's no manifest.json created. Will reconcile fail? Hmm, this could be an issue. The test might be incomplete or there's something else going on that doesn't require a manifest. Without seeing the reconcile code, I can't be sure. This might be a legitimate concern.

Actually, let me reconsider. The test is verifying that reconcile "operates on" an external dir. If reconcile requires a manifest.json, the test would fail. Since the test is in the diff, presumably the developer tested it. But it might be a fragile test if it doesn't actually exercise the full reconcile flow.

Looking at `execCmd` and the test pattern - it returns 0, which is success. So reconcile did succeed. But this might be because reconcile doesn't strictly require a manifest.json, or the test setup is missing some pieces.

Hmm, this is a potential issue but I'm not sure without more context. Let me note it as a potential concern.

### Another potential issue: the `reviewSubdirs` variable

The new code uses `reviewSubdirs` in `ScaffoldOutputDir`. Let me check that this is the correct variable name... yes, based on the existing code, `reviewSubdirs` is the package-level variable listing "payload", "sources", "reconciled". This is consistent with `ScaffoldReviewDir`.

### Let me check the test for the success path of `runReview`:

`TestRunReview_OutputDirSkipsLatest`:
- Creates a fresh temp dir
- Runs a review with `OutputDir` set
- Verifies the output is at the expected path
- Verifies subdirs exist
- Verifies manifest.json exists
- Verifies `.atcr/latest` is not written

This looks comprehensive. But I notice it uses `t.Setenv("ATCR_TEST_KEY", "secret")` - this is a common pattern for setting test env vars. And it uses `mockProvider(t)` and `twoAgentConfig(srv.URL)` - these are existing test helpers.

The test should work. But there's a potential issue: it requires running the full review, which means actually calling LLM providers (mocked). This might be slow. But the existing test `TestRunReview_EndToEnd` already does this, so it's consistent.

### Now let me finalize my findings

For in-scope issues:

1. **`TestReconcileCmd_InheritsExternalOutputDir` may be incomplete**: The test doesn't set up a manifest.json, which reconcile might require. But this might be OK if reconcile doesn't require it. Without seeing the full reconcile code, I can't be certain. I'll mark this as a low-severity concern.

Actually, looking at this more carefully, the test might be checking that reconcile can run on any directory with findings files, not necessarily one that has been created by `atcr review`. This is a more relaxed test. If reconcile reads `findings.txt` from `sources/` and writes to `reconciled/`, then it doesn't need a manifest. Let me check this hypothesis by looking at the test's assertions:
- `require.Equal(t, 0, execCmd(t, "reconcile", ext))` - success exit
- `require.FileExists(t, filepath.Join(ext, "reconciled", "findings.txt"))` - output file exists

If reconcile creates the `reconciled/findings.txt` from `sources/host/findings.txt`, then the test is valid. The test name says "InheritsExternalOutputDir" which suggests it's just checking that reconcile works on an external dir.

I'll mark this as a minor concern but not a blocker.

2. **`runReview` reads `idOverride` before checking mutual exclusion**: This is a code style issue. The order could be cleaner. But it's not a real bug.

3. **The `outputDirFromFlags` function checks `Changed("output-dir")` first, then `Changed("id")`**: The order of checks doesn't matter for correctness because both are required for the error. But conceptually, it's checking if the user provided --output-dir at all, then if they ALSO provided --id. This is fine.

4. **The `ScaffoldOutputDir` function might have a TOCTOU race**: Between the readdir check and mkdirall, someone could create files. This is a theoretical concern but unlikely in practice. The sprint plan acknowledges this risk and accepts it.

5. **What if `--output-dir` is set to a relative path that resolves to a path that's already been used by another test?** This is a test isolation issue, not a code bug. The tests use `t.TempDir()` which provides unique paths.

6. **The `outputDirFromFlags` function discards the error from `GetString`**: This is a minor code smell. The error is virtually impossible to occur (we just registered the flag), but it's not idiomatic Go.

7. **The `ScaffoldOutputDir` function's error message is informative but doesn't tell the user how to resolve it**: The message says "refusing to overwrite (point --output-dir at a new or empty path)". This is actually helpful. Good.

Let me look at the review.go more carefully. There's something that might be a concern:

```go
// Resolve --output-dir (mutually exclusive with --id, relative→absolute)
// before any review work, so a bad flag combination is a usage error (exit 2)
// with no wasted API calls.
outputDir, err := outputDirFromFlags(cmd)
if err != nil {
    return err
}
```

What about validation of `--output-dir` requiring the parent directory to be writable? `MkdirAll` will fail if it can't create parents, but the error might not be user-friendly. This is acceptable for an MVP.

Now let me think about any high-severity issues:

1. **In `PrepareReview`, the OutputDir branch is checked first, but `ScaffoldOutputDir` doesn't use the `id`**: The id is derived earlier. If OutputDir is set, the id is not used for the path. This is by design. No issue.

2. **The `WriteLatest` skip is correct**: It's inside the `if req.OutputDir == ""` block. Good.

3. **What if `req.OutputDir != ""` but `req.IDOverride != ""`?**: This is prevented by the CLI. The code's switch order means OutputDir wins, but since CLI prevents this, it doesn't matter.

4. **`ScaffoldOutputDir` error message for non-empty dir**: Could be a bit more specific about how to recover. But it's clear enough.

Let me also check: does the `idOverride` get ignored when OutputDir is set? Yes - the id is derived from the date+branch, and used for provenance only. This matches the sprint plan clarification: "The generated `<id>` is NOT persisted into `manifest.json`."

Wait - if `idOverride` is set (but OutputDir prevents this), what would happen? The CLI prevents it. But if it got through, the OutputDir branch wins, and the idOverride would be ignored. The `id` is computed before the switch. Let me check:

Looking at the code:
```go
id, err := deriveID(req)
if err != nil {
    return nil, err
}
```

I'd assume `deriveID` uses IDOverride if set, else derives from date+branch. So if both OutputDir and IDOverride were set (hypothetically), the id would use IDOverride but the path would use OutputDir. The CLI prevents this scenario.

OK let me think about my actual findings for in-scope:

**Findings:**

1. The `runReview` function calls `idOverride, _ := cmd.Flags().GetString("id")` BEFORE the mutual exclusion check in `outputDirFromFlags`. The order is logically fine but could be cleaner. This is a low-severity maintainability issue.

2. The `outputDirFromFlags` function discards the error from `GetString` (using `_`). This is a minor code smell. Low severity.

3. The `ScaffoldOutputDir` function has a TOCTOU race between the directory empty check and `MkdirAll`. The sprint plan acknowledges this. Low severity.

4. The `TestReconcileCmd_InheritsExternalOutputDir` test doesn't set up a `manifest.json`, but reconcile might require it. Without seeing the full code, this could be a fragile test. Low severity.

Actually, let me reconsider #4. Looking at the test setup:
- Creates `ext/sources/host/findings.txt` with proper atcr-findings format
- Creates `ext/reconciled/` directory
- Runs `atcr reconcile ext`
- Verifies exit 0 and `ext/reconciled/findings.txt` exists

For reconcile to read from `sources/host/findings.txt` and write to `reconciled/findings.txt`, it needs to:
1. Identify the review dir (uses the path)
2. Find the sources
3. Reconcile findings
4. Write the output

It might or might not require a manifest.json. The test was written by the developer and presumably works. But the test setup looks minimal - if reconcile requires more than just findings files, the test would fail.

Without more context on the reconcile command, I'll note this as a potential concern but not a blocker.

5. **`ScaffoldOutputDir` allows empty `.atcr` files inside the target dir**: If the user runs `atcr review --output-dir /tmp/some-empty-dir` and the dir has a `.gitkeep` file, the readdir check would see the `.gitkeep` and reject. This might be surprising. But this is by design - the check is meant to prevent clobbering.

6. **The `outputDirFromFlags` function could log which path was rejected**: For better debugging. But this is a CLI tool, so errors are shown to the user. No issue.

Hmm, let me look for any actual issues in the in-scope code...

Actually, looking at the review.go change more carefully:

```go
func runReview(cmd *cobra.Command, _ []string) error {
    // ...
    mergeCommit, _ := cmd.Flags().GetString("merge-commit")
    idOverride, _ := cmd.Flags().GetString("id")
    
    // Resolve --output-dir (mutually exclusive with --id, relative→absolute)
    // before any review work, so a bad flag combination is a usage error (exit 2)
    // with no wasted API calls.
    outputDir, err := outputDirFromFlags(cmd)
    if err != nil {
        return err
    }
```

Wait, what about the consistency of where the resolution happens? Why is the resolution in a separate function? Is this clean code? Yes, it's clean - extracting the flag resolution to a helper makes the function more testable.

But there's an interesting point: `outputDirFromFlags` is called BEFORE `failOnThreshold(cmd)` which is the original code. So the ordering is:
1. Get idOverride
2. Resolve outputDir
3. Validate --fail-on
4. ... rest of review

The placement of #2 vs #3 doesn't matter much. Both happen before any expensive work.

Let me also verify: the new code's `outputDirFromFlags` is placed right after the existing flag reads. This is a reasonable location.

I think the in-scope code is generally well-written. Let me look for any subtle issues...

Actually, I notice one thing. In `ScaffoldOutputDir`:

```go
switch entries, err := os.ReadDir(dir); {
case err == nil && len(entries) > 0:
    return "", fmt.Errorf("output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)", dir)
case err != nil && !errors.Is(err, fs.ErrNotExist):
    return "", fmt.Errorf("failed to create review directory: %w", err)
}
if err := os.MkdirAll(dir, 0o755); err != nil {
    return "", fmt.Errorf("failed to create review directory: %w", err)
}
```

What if `os.ReadDir(dir)` returns a non-NotExist error, e.g., a permission error? The switch case returns the error. But the error is wrapped in "failed to create review directory" which is misleading - the readdir call failed, not the creation. The error message could be more specific.

For example, if the user runs `--output-dir /root/something` and they don't have permission, the error would say "failed to create review directory" which is technically wrong. The readdir failed first.

This is a minor error message accuracy issue.

Let me also check: when readdir returns `ErrNotExist`, the code falls through to MkdirAll. MkdirAll will then create the directory. Good.

When readdir returns nil (empty dir), the code falls through to MkdirAll. MkdirAll on an existing dir is a no-op. Then subdirs are created. Good.

OK, I think I have enough findings. Let me also note one more thing:

The `outputDirFromFlags` function checks `cmd.Flags().Changed("output-dir")` but doesn't check if the user passed `--output-dir=""` vs not passing it. The `Changed` method returns true if the flag was explicitly set, even to an empty string. So:
- `--output-dir` not passed: Changed returns false, return "", nil
- `--output-dir=""` passed: Changed returns true, but TrimSpace("") is "", return error
- `--output-dir=" "` passed: Changed returns true, TrimSpace(" ") is "", return error
- `--output-dir="/tmp/x"` passed: Changed returns true, proceed

This is correct. The empty-value check via TrimSpace handles whitespace-only inputs.

Actually, wait. The test `TestOutputDirFromFlags_EmptyValueIsUsageError` does:
```go
require.NoError(t, cmd.ParseFlags([]string{"--output-dir", ""}))
```

`cmd.ParseFlags([]string{"--output-dir", ""})` - does this set Changed to true? Let me think... cobra's behavior is that `--output-dir ""` does set Changed to true. So the test would pass.

But there's a subtle issue: in some cobra versions, `--output-dir=` (with `=`) vs `--output-dir ""` (with space) might behave differently. The test uses the latter. Let me not worry about this - the developer tested it.

OK, let me also consider: the function name `outputDirFromFlags` is a bit awkward. `resolveOutputDir` or `getOutputDir` might be cleaner. But this is a naming nit.

Let me write up my findings.

Actually, let me also look at this:

```go
dir, _ := cmd.Flags().GetString("output-dir")
if strings.TrimSpace(dir) == "" {
    return "", usageError(errors.New("--output-dir must not be empty"))
}
```

The error message says "--output-dir must not be empty" but the check actually rejects empty AND whitespace-only. The error message is technically accurate (whitespace-only is effectively empty), but a user passing a string of spaces might be confused. Minor.

OK, final findings:

For in-scope code, I have these concerns:

1. **Minor: `GetString` error ignored in `outputDirFromFlags`** (review.go:51) - low severity, maintainability
2. **Minor: Error message inaccuracy in `ScaffoldOutputDir`** (reviewdir.go:235) - low severity, error-handling
3. **Minor: TOCTOU race in `ScaffoldOutputDir`** - acknowledged in sprint plan risks, low severity
4. **Minor: `TestReconcileCmd_InheritsExternalOutputDir` setup is minimal** - low severity, correctness (test fragility)
5. **Minor: Order of operations in `runReview`** - idOverride is read before mutual exclusion check, low severity maintainability

Actually, for the test, let me look at it more carefully. The test sets up a minimal review directory and runs reconcile on it. If reconcile requires more than just findings files, the test would fail. But the test was committed, so presumably it works.

Looking at the test, the only "scaffolding" is:
- `ext/sources/host/findings.txt` - the source data
- `ext/reconciled/` - the output dir

No `manifest.json`, no `status.json`, no `payload/`. If reconcile reads the manifest, the test would fail.

But wait, the test expects exit 0. If reconcile failed to find the manifest, it might exit 1 or 2. So either:
a) The test was actually run and works (reconcile doesn't require a manifest for this minimal case)
b) The test was added without being run, and would fail

Given that the developer committed this, I'll assume (a). The reconcile command might be more lenient than I think.

But there's a related concern: the test name says "InheritsExternalOutputDir" and the comment says "no new flag". This suggests the test is just verifying that reconcile works on an arbitrary path. The minimal setup is intentional.

OK, I'll note this as a low-severity concern.

Let me also look for any issue I might have missed...

One thing I notice: the test `TestScaffoldOutputDir_RejectsNonEmpty` creates a hidden file:
```go
require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644))
```

This verifies that hidden files are caught. Good.

But what about directories inside? The test only creates a file. What if the directory contains a subdirectory? The check uses `os.ReadDir` which lists entries, not just files. So a subdirectory would also be caught. Good.

Another potential issue: the `os.ReadDir` lists "directory entries", not all files. If there's a broken symlink, it might or might not show up. Let me not worry about this.

Let me also look at: does `os.ReadDir` return `.` and `..`? No, it doesn't. Only the actual entries. Good.

OK, I think I've covered everything. Let me write up my findings.

Wait, I want to double-check one more thing. The sprint plan says:
"Mutual exclusion (`--output-dir` + `--id`) enforced in `runReview` via `cmd.Flags().Changed()` and returned through `usageError()` so it maps to exit 2."

The implementation puts the mutual exclusion check in `outputDirFromFlags`, not directly in `runReview`. This is a slight deviation from the plan, but it's a cleaner abstraction. The plan said "enforced in `runReview`" but the actual implementation factors it out. This is fine - the plan is a high-level description.

OK, final list of findings:

In scope:
1. LOW: `GetString` error ignored in `outputDirFromFlags` - review.go:51
2. LOW: Error message slightly misleading in `ScaffoldOutputDir` - reviewdir.go:235
3. LOW: TOCTOU race in `ScaffoldOutputDir` - acknowledged
4. LOW: Test `TestReconcileCmd_InheritsExternalOutputDir` may be fragile - reconcile_test.go:64-77
5. LOW: Order of `idOverride` read vs mutual exclusion check - review.go:43-50

Actually, finding #5 is very minor and might not be worth flagging. Let me think about it...

The order is:
```go
idOverride, _ := cmd.Flags().GetString("id")

outputDir, err := outputDirFromFlags(cmd)
if err != nil {
    return err
}
```

If both flags are set:
1. `idOverride` is read (no validation)
2. `outputDirFromFlags` returns an error
3. We return early

The `idOverride` is read but never used (we return early). This is harmless. The order could be swapped for clarity, but it's not a bug.

I'll skip this finding.

Let me also think about: the `outputDirFromFlags` function might be misnamed. It's not just "getting" the flag, it's also validating it. A name like `resolveOutputDir` or `validateOutputDir` might be more accurate. But "FromFlags" is a common Go pattern. I'll skip this.

OK, let me write up the findings.

Actually, I realize I should also look at the `runReview` function to see if there are any other potential issues...

The function:
1. Gets various flag values
2. Calls `outputDirFromFlags` for validation
3. Calls `failOnThreshold` for validation
4. Loads config
5. Resolves range
6. Builds ReviewRequest
7. Runs review

The new code is well-placed. Validation happens before any expensive work. Good.

Let me also look at the test for the success path:
- `TestOutputDirFromFlags_Unset` - tests that unset returns ""
- `TestOutputDirFromFlags_MutuallyExclusiveWithID` - tests mutual exclusion
- `TestOutputDirFromFlags_RelativeResolvedToAbs` - tests relative path resolution
- `TestOutputDirFromFlags_EmptyValueIsUsageError` - tests empty value rejection

All four scenarios are covered. Good test coverage.

What about the case where the user passes a valid relative path that happens to point to the current directory? E.g., `--output-dir .`. This would resolve to CWD. The function would return the CWD. The `ScaffoldOutputDir` would then check if CWD is non-empty (it almost certainly is) and return an error. The user would get a clear error. Good.

What about `--output-dir /`? This would resolve to root. ReadDir would return many entries. Error. Good.

OK, I'm confident in my findings. Let me write them up.

One more thought: the `ScaffoldOutputDir` function is in the `fanout` package. Is this the right place? The function creates directories on the filesystem, which is a low-level operation. The fanout package orchestrates reviews. Putting `ScaffoldOutputDir` here is reasonable - it parallels `ScaffoldReviewDir`. The naming is consistent.

OK, writing up now.

Final list:

In scope:
1. LOW: `GetString` error ignored in `outputDirFromFlags` - review.go:51 (maintainability)
2. LOW: Error message slightly misleading in `ScaffoldOutputDir` - reviewdir.go:235 (error-handling)
3. LOW: Test `TestReconcileCmd_InheritsExternalOutputDir` may be fragile - reconcile_test.go:64-77 (correctness)

Out of scope (mentioned for transparency but not in TD_STREAM):
- `outcome.go` change to default case
- `budget.go` AllDropped field
- `ambiguous.go` panic change
- `project.go` and `registry.md` payload_byte_budget docs
- `outcome_test.go`, `budget_test.go` tests

Hmm, actually thinking about it again. Let me look for HIGH/MEDIUM issues I might have missed.

Let me re-read the diff more carefully...

The `ScaffoldOutputDir` function:
```go
switch entries, err := os.ReadDir(dir); {
case err == nil && len(entries) > 0:
    return "", fmt.Errorf("output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)", dir)
case err != nil && !errors.Is(err, fs.ErrNotExist):
    return "", fmt.Errorf("failed to create review directory: %w", err)
}
```

Wait. I see a potential issue. The error wrapping says "failed to create review directory" but the error might come from `os.ReadDir`, not from any creation operation. The user might see "failed to create review directory: permission denied" when actually the readdir failed.

But this is a minor naming issue. The next operation is `MkdirAll`, which is the creation. The error message could be interpreted as "failed to create" referring to the upcoming MkdirAll, but actually it's the readdir that failed.

I'll note this as a minor concern.

Actually, let me think about a real bug. What if `os.ReadDir` returns `ErrNotExist` (path doesn't exist), and then `os.MkdirAll` succeeds? The function continues and creates subdirs. Good.

What if `os.ReadDir` returns `ErrNotExist`, and `os.MkdirAll` fails? The error is returned. Good.

What if `os.ReadDir` returns nil and entries is empty, and `os.MkdirAll` fails? MkdirAll on an existing dir should not fail (unless there's a permission issue or it's a file). If it's a file, ReadDir would have returned an error. If it's a directory but with restricted permissions for writing, MkdirAll would fail. Edge case.

OK, no major bugs found. Let me finalize.

Actually, one more thing. Let me look at the `ScaffoldOutputDir` documentation comment:
```go
// ScaffoldOutputDir creates the review tree at an explicit --output-dir path
// (used verbatim — it is the orchestrator's own output target, not under
// .atcr/reviews/). Parent directories are created as needed; the path may be
// non-existent or an empty directory. It returns the path so callers mirror the
// ScaffoldReviewDir signature.
```

The comment says "Parent directories are created as needed". This is implemented via `os.MkdirAll`. Good.

The comment says "the path may be non-existent or an empty directory". This is implemented. Good.

The comment says "It returns the path so callers mirror the ScaffoldReviewDir signature." This is a bit unclear - it returns the path that was passed in, after validation. Good.

Let me look at the test for relative path resolution:
```go
func TestOutputDirFromFlags_RelativeResolvedToAbs(t *testing.T) {
    isolate(t)
    cmd := newReviewCmd()
    require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "out/review"}))
    dir, err := outputDirFromFlags(cmd)
    require.NoError(t, err)
    require.True(t, filepath.IsAbs(dir), "relative path must resolve to absolute, got %q", dir)
    cwd, err := os.Getwd()
    require.NoError(t, err)
    require.Equal(t, filepath.Join(cwd, "out", "review"), dir)
}
```

This test uses `isolate(t)` to set up a clean CWD. Then it resolves a relative path. The test verifies the result is an absolute path matching `cwd/out/review`. Good.

But wait - the test uses `isolate(t)`. What does this do? Probably changes to a temp directory. The test then uses `os.Getwd()` to verify the result. This is correct.

But there's a subtle issue: the test might be affected by parallel test execution. If another test changes CWD, this test might break. The `isolate(t)` function probably handles this. Let me trust the existing test pattern.

OK, finalizing my findings. I have 3 low-severity in-scope issues. None are critical. The code is well-written overall.

Let me also think about test isolation. The test `TestRunReview_OutputDirSkipsLatest` uses `t.TempDir()` which provides unique paths. The test `TestReconcileCmd_InheritsExternalOutputDir` also uses `t.TempDir()`. Good.

Let me also check: are there any other tests that might be affected by the changes?

The `runReview` function now resolves outputDir early. This could affect other tests that call `runReview`. Let me look at the test file...

Actually, I don't have access to the full test file. But the existing tests should not set `--output-dir` so they should be unaffected.

OK, I'm done. Let me write up the findings.

Actually, wait. Let me look at the test file diff one more time:

```go
+func TestOutputDirFromFlags_Unset(t *testing.T) {
+	cmd := newReviewCmd()
+	require.NoError(t, cmd.ParseFlags(nil))
+	dir, err := outputDirFromFlags(cmd)
+	require.NoError(t, err)
+	require.Equal(t, "", dir, "unset --output-dir must yield empty (default review dir)")
+}
```

Wait, `cmd.ParseFlags(nil)` - is this valid cobra? Let me think... Yes, `ParseFlags(nil)` is valid. It parses no flags, leaving all flags at their default values. The `Changed` method should return false for all flags. Good.

But there's a subtle thing: `cmd.Flags().Changed("output-dir")` returns true if the flag was set, even if to the default value. After `ParseFlags(nil)`, the flag was not set, so `Changed` returns false. Good.

What about `cmd.ParseFlags([]string{})`? Same as `nil`. Good.

What about `cmd.ParseFlags([]string{"--output-dir", ""})`? This sets the flag to "" but `Changed` returns true. Then the function would check `TrimSpace("") == ""` and return an error. The test `TestOutputDirFromFlags_EmptyValueIsUsageError` verifies this.

But wait - when you do `cmd.ParseFlags([]string{"--output-dir", ""})`, does the flag get set to "" or to the default? Let me think... cobra's behavior: if you pass `--output-dir ""`, the flag is set to "". `Changed` returns true. `GetString` returns "". The default value of the flag is "" (set in the code), so this is consistent.

OK, the tests are correct.

Let me also consider: what if the user passes `--output-dir ""` and the default is also ""? The `Changed` check correctly distinguishes "not set" from "set to default value". Good.

OK, I'm done analyzing. Let me write up the final findings.

For the in-scope code, my findings are:

1. LOW - `GetString` error ignored at review.go:51
2. LOW - Error message inaccuracy at reviewdir.go:235
3. LOW - Test fragility at reconcile_test.go:64-77

Hmm, let me think about whether finding #3 is really an issue. The test:
- Creates a directory structure
- Runs `atcr reconcile ext`
- Expects exit 0
- Verifies `ext/reconciled/findings.txt` exists

If this test was actually run and passes, then reconcile doesn't require a manifest.json for this minimal case. The test is valid. So #3 might not be an issue.

But there's a concern: what if reconcile is supposed to require a manifest.json (e.g., to verify the review was created by atcr), and this test is bypassing that check? The test name says "InheritsExternalOutputDir" and the comment says "operates on it via its existing [id-or-path] argument with NO new flag." This suggests the test is verifying the inheritance, not the full reconcile functionality.

If reconcile has a guard like "manifest.json must exist", this test would fail. Since it's in the diff, presumably it passes. So either:
a) Reconcile doesn't require a manifest.json (acceptable)
b) The test was added without being run (concerning)

I'll note this as a potential concern but with low severity.

Actually, let me think about this from a different angle. The reconcile command's job is to aggregate findings from sources. It reads `findings.txt` from `sources/<name>/` and writes to `reconciled/findings.txt`. It might or might not need a manifest.json to identify the review. If it takes a path directly, it might just operate on whatever it finds.

Looking at the test: it uses `reconcile ext` (a relative path). Reconcile discovers the path, looks for sources, aggregates. No manifest.json needed. This is a sensible test.

OK, I'll downgrade this finding. The test is reasonable. No issue.

So my final findings are:

1. LOW - `GetString` error ignored at review.go:51
2. LOW - Error message inaccuracy at reviewdir.go:235

Actually, these are both very minor. Let me consider whether to include them at all. The instructions say "No false positives: Only report real issues". These are real but very minor.

I'll include them with low severity.

Let me also consider: is there any potential security issue I'm missing?

The `--output-dir` accepts an arbitrary path. There's no validation that the path is safe. But the user has chosen the path, so this is not really a security issue. The tool does the right thing with whatever path is given.

What if the path is `/etc/passwd`? The function would try to ReadDir, which would return entries. The function would reject. The user gets a clear error. Good.

What if the path is `/dev/null`? ReadDir on a special file would return an error (not ErrNotExist). The function would reject. Good.

No security issues.

What about performance? The `ScaffoldOutputDir` function does a ReadDir followed by MkdirAll. This is two filesystem operations. Acceptable.

OK, finalizing.

Actually, one more thought. The TOCTOU race in `ScaffoldOutputDir` is acknowledged in the sprint plan's risk table. The mitigation is "Use os.ReadDir and check for any entry; document the check". The implementation does this. The race exists but is theoretical. I'll skip this finding since it's acknowledged.

OK, my final findings:

1. LOW - `GetString` error ignored at review.go:51 (maintainability)
2. LOW - Error message inaccuracy at reviewdir.go:235 (error-handling)

These are very minor. Let me check if there are any other issues I should flag.

Hmm, one more thing. Let me check the error message wording for clarity:

`"--output-dir must not be empty"` - the user might wonder if "empty" means "no files" or "zero-length string". The implementation actually checks `strings.TrimSpace(dir) == ""`, so whitespace-only is also rejected. The error message is accurate enough.

`"--output-dir and --id are mutually exclusive"` - clear and concise.

`"output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)"` - clear and actionable.

All error messages are reasonable.

OK, finalizing my findings. I'll output them in TD_STREAM format.

Wait, let me also note: the `runReview` function doesn't pass the resolved outputDir to the request in a way that logs the path. The user might want to see where the output went. But this is shown in stdout by the existing review code (`cmd/atcr/review.go:100` per the sprint plan). So no issue.

OK, writing up.

Actually, I realize I should also look for issues in the new tests to make sure they're well-written.

`TestScaffoldOutputDir_RejectsFileAtPath`:
```go
file := filepath.Join(t.TempDir(), "afile")
require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
_, err := ScaffoldOutputDir(file)
require.Error(t, err)
assert.Contains(t, err.Error(), "failed to create review directory")
```

This test creates a file and tries to scaffold at that path. The expected error is "failed to create review directory". The test passes because:
1. `os.ReadDir(file)` returns an error (not ErrNotExist, since the file exists)
2. The switch case `err != nil && !errors.Is(err, fs.ErrNotExist)` triggers
3. Returns the wrapped error

But wait, what error does `os.ReadDir` return for a file? On Linux, it would be `ENOTDIR` ("not a directory"). On other systems, it might be different. The Go error would be `syscall.ENOTDIR` or similar, not `fs.ErrNotExist`. So the switch case triggers and returns the error.

Good, the test is correct.

`TestScaffoldOutputDir_AllowsEmptyExisting`:
```go
dir := t.TempDir()
got, err := ScaffoldOutputDir(dir)
require.NoError(t, err)
require.Equal(t, dir, got)
assert.DirExists(t, filepath.Join(dir, "sources"))
```

`t.TempDir()` creates a fresh empty directory. The test:
1. Calls ScaffoldOutputDir with the empty dir
2. Verifies no error
3. Verifies the returned path matches
4. Verifies "sources" subdir exists

This is correct. The empty dir is allowed.

`TestScaffoldOutputDir_RejectsNonEmpty`:
```go
dir := t.TempDir()
require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644))
_, err := ScaffoldOutputDir(dir)
require.Error(t, err)
assert.Contains(t, err.Error(), "not empty")
```

Creates a hidden file in the dir. ScaffoldOutputDir should reject. The test verifies the error message contains "not empty". Correct.

`TestScaffoldOutputDir_CreatesTreeWhenAbsent`:
```go
dir := filepath.Join(t.TempDir(), "nested", "ext-review")
got, err := ScaffoldOutputDir(dir)
require.NoError(t, err)
require.Equal(t, dir, got)
for _, sub := range reviewSubdirs {
    assert.DirExists(t, filepath.Join(dir, sub))
}
```

Creates a non-existent path. ScaffoldOutputDir should create it. The test verifies the path and subdirs. Correct.

All tests look well-written.

OK, finalizing my findings. Two low-severity issues for in-scope code.

Actually, let me reconsider the `GetString` error ignoring. The error from `cmd.Flags().GetString` is virtually never set in practice - cobra's flag system doesn't return errors for valid registered flags. The error is only set if the flag pointer is nil. So ignoring it is safe.

But the Go idiom is to check errors. The code could use:
```go
dir, err := cmd.Flags().GetString("output-dir")
if err != nil {
    return "", usageError(fmt.Errorf("invalid --output-dir: %w", err))
}
```

This is more defensive. But since the error is virtually never set, it's a minor style issue.

I'll flag this as a low-severity maintainability issue.

OK, writing up.

Hmm, also let me look at one more thing. The `ScaffoldOutputDir` function doesn't have a test for the case where ReadDir returns ErrNotExist and MkdirAll fails. This is a rare edge case but could be tested. Not a blocker.

Also, the `ScaffoldOutputDir` function doesn't have a test for the case where MkdirAll succeeds but creating a subdir fails. This is also a rare edge case. Not a blocker.

OK, done analyzing. Writing up the findings.

Wait, I want to also verify: are there any places where the new code might conflict with existing code?

The new flag `--output-dir` is added to the review command. The flag name is unlikely to conflict with anything. Good.

The new struct field `OutputDir` is added to `ReviewRequest`. Let me check the existing struct fields... The diff shows the struct has these fields:
- Root
- Base
- Head
- MergeCommit
- TimeSuffix
- StartedAt
- IDOverride
- OutputDir (new)

No conflicts. Good.

The new function `ScaffoldOutputDir` is added. No conflicts with `ScaffoldReviewDir`. Good.

The new test functions are added. No conflicts. Good.

The new `usageError` is used. Let me check if it exists... The diff shows `usageError(errors.New(...))` which implies it exists. Good.

OK, no conflicts. Finalizing.

Let me also check the `cmd/atcr/review.go` change one more time. The new code adds an `errors` and `path/filepath` import. Let me check the existing imports...

The diff shows:
```go
import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"
    ...
)
```

The new imports are `errors` and `path/filepath`. Both are used. Good.

OK, done. Writing up.

One more thought: the test `TestReconcileCmd_InheritsExternalOutputDir` is a good test for the reconcile command working on external paths. But the test name is a bit awkward - "InheritsExternalOutputDir" suggests the reconcile command inherits the output dir from the review command. The reconcile command doesn't actually inherit anything - it takes a path argument. The test verifies that the same path can be used for both review and reconcile. The name is a bit misleading.

But this is a minor naming nit, not a real issue.

OK, final findings. Writing up.

Actually, let me think one more time about the high/medium severity issues.

Looking at the code:
- Flag parsing: clean
- Path validation: clean
- Skipping .atcr/latest: clean
- Error handling: clean (with minor issues)
- Test coverage: comprehensive

I don't see any high or medium severity issues. Just low-severity nits.

OK, finalizing. I'll output 2 low-severity findings.

Hmm, actually thinking about it more, both of these are so minor that I wonder if I should include them. The instructions say "No false positives: Only report real issues". These are real but very minor.

Let me include them since they're real issues, just with low severity.

Actually, on reflection, the error message inaccuracy in ScaffoldOutputDir is a real issue. When the user sees "failed to create review directory: permission denied", they might think the tool tried to create the directory but failed, when actually the tool tried to read it and failed. This could lead to confusion.

The GetString error ignoring is more of a style issue. The error is virtually never set, so this is safe. I'll include it as a low-severity issue.

OK, finalizing for real now.

Let me also check the test `TestRunReview_OutputDirSkipsLatest` more carefully. It uses `t.Setenv("ATCR_TEST_KEY", "secret")` and `mockProvider(t)`. This is a heavyweight test that requires a mock LLM provider. The test is consistent with the existing `TestRunReview_EndToEnd` test.

But there's a concern: if the test fails, it might be hard to debug because the error is in the LLM response. However, this is consistent with the existing test pattern.

OK, no additional findings. Writing up.

One more thought: the `runReview` function is long. The new code adds a few more lines. This is a maintainability concern but not new - the function was already long before this change.

OK, FINAL findings:

1. LOW - review.go:51 - GetString error ignored
2. LOW - reviewdir.go:235 - Error message inaccuracy

Writing up in TD_STREAM format.
</think>

Looking at this diff, I need to focus on the in-scope items per the sprint plan: `--output-dir` flag on `atcr review`, path validation, skipping `.atcr/latest`, and tests. The other changes (outcome.go default case, budget.go AllDropped, ambiguous.go panic, registry.md/project.go payload_byte_budget docs) are OUT-OF-SCOPE per the sprint plan.

Let me analyze the in-scope code carefully for issues:

**`cmd/atcr/review.go`**:
- `outputDirFromFlags` extracts flag resolution cleanly
- `runReview` resolves `--output-dir` early, before any expensive work
- Mutual exclusion check is correct

**`internal/fanout/review.go`**:
- `OutputDir` added to `ReviewRequest` with clear documentation
- `PrepareReview` switch correctly prioritizes OutputDir and skips `WriteLatest`

**`internal/fanout/reviewdir.go`**:
- `ScaffoldOutputDir` uses `os.ReadDir` to check emptiness, handles all error cases

**Tests**: Coverage includes unset, mutual exclusion, relative path, empty value, scaffolding, latest-pointer skip.

```
LOW|cmd/atcr/review.go:51|GetString error silently discarded on registered flag|Use the returned error to surface an invalid flag state defensively|maintainability|2|dir, _ := cmd.Flags().GetString("output-dir") ignores an err that is virtually never set but is non-idiomatic and would mask a misconfigured flag set|bruce
LOW|internal/fanout/reviewdir.go:235|Error message says "failed to create review directory" but wraps an os.ReadDir failure (e.g. permission denied) before MkdirAll runs|Distinguish read vs. create failures in the wrapped error so users can diagnose permission issues on the target path|error-handling|3|fmt.Errorf("failed to create review directory: %w", err) is returned when os.ReadDir hits ENOTDIR/EACCES, not when creation itself failed|bruce
```