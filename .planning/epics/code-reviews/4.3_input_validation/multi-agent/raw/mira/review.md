<think>
Let me carefully review this diff in the context of the sprint plan for Epic 4.3: Input Validation Layer.

The scope from the sprint plan clarifications:
- IN scope: `internal/validation` package, `FilePath` wiring on `review --output-dir` and `report --output`, all such errors wrapped in `usageError`
- OUT of scope: wiring `GitRef` onto `--base`/`--head`, replacing existing severity/format validation, wiring `ReviewID` onto `--id`, config/persona/model/API-key validation

Let me review the diff carefully:

1. **`cmd/atcr/report.go`** - adds `FilePath` validation on `--output` after `filepath.Abs`. This is in scope.

2. **`cmd/atcr/report_test.go`** - adds test for system dir output. In scope.

3. **`cmd/atcr/review.go`** - adds `FilePath` validation on `--output-dir` after `filepath.Abs`. In scope.

4. **`cmd/atcr/review_test.go`** - adds test for system dir rejection. In scope.

5. **`internal/boundaries_test.go`** - adds `validation` to allowed imports. In scope.

6. **`internal/validation/validation.go`** - new package. In scope.

7. **`internal/validation/validation_test.go`** - tests for validators. In scope.

Now let me look for actual issues:

**Issue 1: `cmd/atcr/report.go` - `output` is not declared**
Looking at the diff for `cmd/atcr/report.go`:
```go
if output, _ := cmd.Flags().GetString("output"); output != "" {
    abs, err := filepath.Abs(output)
    ...
}
```

Wait, is `output` actually declared as a flag on the report command? The diff shows the wiring but I should check if the `--output` flag exists. But the existing test `TestReportCmd_OutputWriteFailureIsUsageError` uses `--output`, so it must exist. This is fine.

**Issue 2: `validation.FilePath` - what about symlinks?**
The `FilePath` function checks for `..` in the path string, but after `filepath.Abs`, the path should be cleaned (no `..`). So that's fine.

**Issue 3: `validation.FilePath` - Windows system directories**
The comment in the code acknowledges this is Unix-only. That's noted as technical debt. Not a current issue.

**Issue 4: `validation.GitRef` - The `^` and `~` characters**
The `GitRef` function rejects `^` and `~`, but these are used in git revisions like `HEAD^` and `HEAD~3`. The sprint plan explicitly says this is NOT wired onto `--base`/`--head`, so this is acceptable. But wait - the function is in the package and exported. If someone uses it elsewhere, it could be problematic. The comment in the code acknowledges this. Not a bug per se, but worth noting as a design decision.

**Issue 5: `validation.FilePath` - bare `/` prefix check could miss Windows**
Already noted in the code comments.

**Issue 6: Test coverage for `Enum` function**
The test for `Enum` covers the success case and a single failure case. Looks adequate.

**Issue 7: `validation.FilePath` - what about a path like `/etcd/`?**
The code checks `path == sysDir || strings.HasPrefix(path, sysDir+"/")`. So `/etcd/` would NOT match `/etc/` because `/etcd/` doesn't start with `/etc/`. Wait, let me check: `/etcd/` starts with `/e`, then `t`, then `c`, then `d`, then `/`. `/etc/` is `/e`, `t`, `c`, then `/`. So `/etcd/` does NOT start with `/etc/`. Good.

But what about `/etc`? The code checks `path == sysDir`, so `/etc` (exact) is rejected. And `/etc/passwd` starts with `/etc/` so it's rejected. Good.

**Issue 8: `cmd/atcr/report.go` - error handling for `filepath.Abs`**
The code wraps the error with `usageError`. But `filepath.Abs` errors are quite rare (they'd only happen on systems with weird current directory states). Using `usageError` (exit 2) for this seems fine since it's a user-facing error.

**Issue 9: Potential issue - `outputDirFromFlags` already validates**
Let me look at the `outputDirFromFlags` function. The diff shows it already does `filepath.Abs` and then adds `validation.FilePath`. But wait - if `outputDirFromFlags` is called from `runReview`, and it returns a `usageError`... the function returns `(string, error)`. Let me check the existing code more carefully.

Looking at the diff:
```go
abs, err := filepath.Abs(outputDir)
if err != nil {
    return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
}
if err := validation.FilePath(abs); err != nil {
    return "", usageError(err)
}
```

This looks correct. The error from `validation.FilePath` is wrapped in `usageError`, which is the correct pattern per AC6.

**Issue 10: Test for `TestReportCmd_SystemDirOutputIsUsageError`**
The test uses `/etc/atcr-report.md` which is an absolute path. After `filepath.Abs`, it stays `/etc/atcr-report.md`. The `FilePath` function checks `strings.HasPrefix(path, "/etc/")` which matches. Good.

**Issue 11: Missing test case for the `outputDirFromFlags` error**
Actually wait - the test `TestOutputDirFromFlags_SystemDirRejected` tests the function directly. Good.

**Issue 12: Potential edge case - what if the system dir check matches Windows paths?**
On Windows, the path might be `C:\etc\passwd`. The function checks for `/etc`, `/proc`, `/sys` which are Unix-style. On Windows, this would not match. The code comment acknowledges this. Not a bug, just a limitation.

**Issue 13: `outputDirFromFlags` - is the validation in the right place?**
The sprint plan says: "`FilePath` is applied to `review --output-dir` AFTER `filepath.Abs` resolution (inside `outputDirFromFlags`)". The code does exactly this. Good.

**Issue 14: What about relative paths that resolve to a system dir?**
For example, if cwd is `/etc` and user passes `--output-dir foo`, then `filepath.Abs("foo")` would return `/etc/foo`. The `FilePath` function would reject this. Good.

**Issue 15: `outputDirFromFlags` - does it handle empty input correctly?**
Looking at the diff, the `FilePath` function checks for empty path and returns an error. But what if `outputDir` is empty? The function would call `filepath.Abs("")` which returns the current directory. Then `FilePath` would check that... and if cwd is not a system dir, it passes. This seems reasonable.

Wait, let me look at the existing `outputDirFromFlags` function. The diff shows it being modified, but I don't see the full function. Let me check if there's a check for empty `outputDir` before calling `filepath.Abs`. Looking at the diff:

```go
func outputDirFromFlags(cmd *cobra.Command) (string, error) {
    ...
    abs, err := filepath.Abs(outputDir)
    if err != nil {
        return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
    }
    if err := validation.FilePath(abs); err != nil {
        return "", usageError(err)
    }
    return abs, nil
}
```

I don't have the full function, but the diff only shows the addition of the `FilePath` check. The existing code presumably handles the empty case (e.g., by using a default). This is fine.

**Issue 16: Test `TestReportCmd_SystemDirOutputIsUsageError` - missing cleanup?**
The test calls `isolate(t)` and `fixtureReconciled(t, "r", oneFinding)`. These should handle cleanup. The test doesn't need to create the file since validation happens before any I/O. Good.

**Issue 17: `FilePath` function - what about null bytes?**
The function doesn't check for null bytes in paths. On Unix, null bytes in paths are invalid. This could be a potential issue, but it's not in the original spec. Low priority.

**Issue 18: `FilePath` function - what about `\\` on Windows?**
On Windows, backslashes are path separators. The function only checks for `..` and Unix-style system dirs. This is a known limitation acknowledged in the code comments.

**Issue 19: `GitRef` function - what about `\` (backslash)?**
The function checks for `~^: \t\n` but not `\`. On Windows, backslashes might be used. But git refs don't typically use backslashes. Not a real issue.

**Issue 20: `ReviewID` function - what about the length check before pattern?**
The function checks for empty first, then pattern, then length. The pattern `^[a-zA-Z0-9_-]+$` would reject paths with `..` or `/`, so the pattern check is sufficient for security. The length check is a separate concern. This is fine.

**Issue 21: `Severity` function - case-insensitive but error message shows original**
The function uses `strings.ToUpper(s)` to check, but the error message uses the original value. This is fine - the error shows what the user actually typed.

**Issue 22: `Enum` function - what about empty `allowed` slice?**
If `allowed` is empty, the function always returns an error. This is correct behavior.

**Issue 23: `Enum` function - what about nil `allowed` slice?**
If `allowed` is nil, the `for _, a := range allowed` loop doesn't execute, and the function returns an error. This is correct.

**Issue 24: `ValidationError` - is it comparable?**
The struct has three string fields, so it's comparable. But errors should typically not be compared directly. The test uses `assert.Equal` which compares the error string. This is fine.

**Issue 25: `outputDirFromFlags` - the existing function might have other validation**
I don't have the full context, but the diff shows the addition. The existing function might have other checks. This is fine.

**Issue 26: Test for `outputDirFromFlags` - does it use the correct exit code?**
The test checks `require.Equal(t, 2, exitCode(err))`. This assumes `exitCode` is a function that extracts the exit code from a `usageError`. This is consistent with the rest of the codebase.

**Issue 27: Potential issue - `TestOutputDirFromFlags_SystemDirRejected` doesn't test the full integration**
This test only tests `outputDirFromFlags` directly, not the full `runReview` command. But the integration is simple enough that this is adequate.

**Issue 28: `report.go` - the validation happens after format validation**
Looking at the diff, the format validation happens first, then the output path validation. This is fine - format is a simpler check.

**Issue 29: `review.go` - the validation happens in `outputDirFromFlags`**
The function is called from `runReview`. The validation happens before any review work. Good.

**Issue 30: Missing test for `outputDirFromFlags` with a relative path that resolves to a system dir**
The test only tests an absolute system dir path. A test for a relative path that resolves to a system dir (e.g., if cwd is `/etc`) would be more thorough. But this is a minor test coverage issue.

**Issue 31: Missing test for `outputDirFromFlags` with a valid relative path**
The sprint plan says: "a legitimate relative path (e.g. `../reviews`) resolves to a clean absolute path and pass". There's no test for this. But there might be other tests that cover this. Low priority.

**Issue 32: `FilePath` function - what about a path that's exactly `..`?**
The function checks for `strings.Contains(path, "..")`. So `..` would match. Good.

**Issue 33: `FilePath` function - what about a path like `...`?**
The function checks for `strings.Contains(path, "..")`. So `...` would match. But `...` is not a path traversal in the traditional sense. This is a minor false positive. Low priority.

**Issue 34: `GitRef` function - what about Unicode characters?**
Git refs can contain Unicode characters in newer versions of git. The function only rejects specific characters (`~^: \t\n`). This is fine for the current scope.

**Issue 35: `ReviewID` function - what about Unicode characters?**
The pattern `^[a-zA-Z0-9_-]+$` only allows ASCII. This is consistent with the spec.

**Issue 36: `Severity` function - what about leading/trailing whitespace?**
The function doesn't trim whitespace. If the user passes `" HIGH "`, it would be rejected. This might be too strict, but it's consistent with the spec.

**Issue 37: `Enum` function - case sensitivity**
The function does exact string matching. If the user passes `"MD"` and the allowed values are `["md", "json"]`, it would be rejected. This is consistent with the spec.

**Issue 38: `outputDirFromFlags` - the error from `validation.FilePath` is wrapped in `usageError`**
The diff shows:
```go
if err := validation.FilePath(abs); err != nil {
    return "", usageError(err)
}
```
This correctly wraps the error. Good.

**Issue 39: `report.go` - the error from `validation.FilePath` is wrapped in `usageError`**
The diff shows:
```go
if err := validation.FilePath(abs); err != nil {
    return usageError(err)
}
```
This correctly wraps the error. Good.

**Issue 40: Potential issue - `outputDirFromFlags` might be called with a path that starts with `~` (home directory)**
The `FilePath` function doesn't check for `~`. This is a Unix convention, not a git convention. Not a real issue.

**Issue 41: Test `TestReportCmd_SystemDirOutputIsUsageError` - the test doesn't check that no file is created**
The test checks the exit code and error message, but doesn't verify that no file was created at `/etc/atcr-report.md`. This is a minor test coverage issue, but the validation happens before any I/O, so it's implicitly tested.

**Issue 42: `validation.FilePath` - the error message for `..` is clear**
The error message "must not contain .." is clear and actionable. Good.

**Issue 43: `validation.FilePath` - the error message for system dirs is clear**
The error message "must not reference system directories" is clear. Good.

**Issue 44: `outputDirFromFlags` - the function signature is `(string, error)`**
The function returns an empty string and an error when validation fails. This is consistent with the existing pattern.

**Issue 45: Potential issue - the `output` flag in `report.go` might not exist**
I assumed it exists based on the existing test. If it doesn't exist, `cmd.Flags().GetString("output")` would return an empty string, and the validation would be skipped. But the existing test uses `--output`, so it must exist.

**Issue 46: `outputDirFromFlags` - what if the user passes a path with a trailing slash?**
`filepath.Abs` handles trailing slashes correctly. The `FilePath` function checks for `..` which would be found regardless of trailing slashes. This is fine.

**Issue 47: `outputDirFromFlags` - what if the user passes a path with multiple slashes?**
`filepath.Abs` cleans the path. This is fine.

**Issue 48: `outputDirFromFlags` - what if the user passes a path with `.`?**
`filepath.Abs` cleans the path. The `FilePath` function checks for `..` not `.`. So `./foo` would pass `FilePath` after `filepath.Abs` cleans it. This is correct behavior.

**Issue 49: `validation.FilePath` - the `..` check is too strict for some legitimate paths**
For example, a file named `file..txt` would be rejected. But the sprint plan says to apply `FilePath` after `filepath.Abs`, and `filepath.Abs` doesn't add or remove `..` from the middle of a path. So a file named `file..txt` would still be rejected. This is a minor false positive. Low priority.

Wait, actually, `filepath.Clean` (which is called by `filepath.Abs`) doesn't add or remove `..` from the middle of a path. It only resolves `..` that refer to parent directories. So `file..txt` would remain `file..txt` after cleaning. And `FilePath` would reject it because it contains `..`. This is a known limitation.

**Issue 50: `validation.FilePath` - what about a path like `a/../b`?**
After `filepath.Abs`, this would become `b` (the `..` is resolved). So `FilePath` would see `b` and pass. This is correct behavior.

**Issue 51: `validation.FilePath` - what about a path like `a/..`?**
After `filepath.Abs`, this would become the current directory. `FilePath` would see the current directory path and check if it's a system dir. If cwd is not a system dir, it passes. This is correct behavior.

**Issue 52: `outputDirFromFlags` - the existing function might have a check for empty `outputDir`**
I don't have the full context, but the diff shows the addition. The existing function might handle the empty case by using a default. This is fine.

OK, let me think about what are the most significant issues to report:

1. The `FilePath` function rejects any path containing `..`, which could cause false positives for legitimate filenames containing `..` (like `file..txt`). However, this is a design decision and the sprint plan doesn't specify otherwise.

2. The `FilePath` function's `..` check happens AFTER `filepath.Abs`, which resolves `..` in path components. But `..` in the middle of a filename (like `file..txt`) is not resolved by `filepath.Abs`. So the check would still reject such files. This is a potential false positive.

3. The `outputDirFromFlags` test doesn't cover the case of a relative path that resolves to a system dir. But this is a test coverage issue, not a bug.

4. The `outputDirFromFlags` test doesn't cover the case of a valid relative path (like `../reviews`). But the sprint plan says this should work, and there might be other tests that cover it.

5. The `FilePath` function doesn't check for null bytes, which are invalid in Unix paths. This is a minor security concern.

6. The `outputDirFromFlags` function might be called with a path that, after `filepath.Abs`, still contains `..` in a filename (like `/home/user/file..txt`). The `FilePath` function would reject this. This is a potential false positive.

Let me also check if there are any issues with the test code:

7. The test `TestOutputDirFromFlags_SystemDirRejected` uses `exitCode(err)` to check the exit code. This assumes `exitCode` is a function that extracts the exit code from a `usageError`. This is consistent with the rest of the codebase.

8. The test `TestReportCmd_SystemDirOutputIsUsageError` uses `execCmdCapture` to capture the output. This is consistent with the rest of the codebase.

Let me also check the boundaries test:

9. The `allowedInternalImports` map now includes `validation`. This is correct.

10. The `validation` package is listed as having no dependencies (empty slice). This is correct - the package only uses stdlib.

Now, let me think about what's most important to report:

The most significant issue I see is the `..` check in `FilePath` that could cause false positives for legitimate filenames. But this is a design decision and the sprint plan doesn't specify otherwise.

Actually, wait. Let me re-read the `FilePath` function:

```go
// No path traversal.
if strings.Contains(path, "..") {
    return &ValidationError{"file path", path, "must not contain .."}
}
```

The comment says "No path traversal", but the check is for any `..` in the path. After `filepath.Abs`, path traversal sequences like `../foo` are resolved, so any remaining `..` would be in a filename (like `file..txt`). The check is too strict.

But wait, the sprint plan says: "No path traversal" in the spec, and the function is supposed to reject `..`. The sprint plan doesn't distinguish between path traversal sequences and `..` in filenames. So this is consistent with the spec.

Let me think about other issues:

11. The `outputDirFromFlags` function's validation happens after `filepath.Abs`. If `filepath.Abs` fails (e.g., due to a very long path), the error is wrapped in `usageError`. This is correct.

12. The `report.go` validation happens after the format validation. If the format is invalid, the path validation is skipped. This is fine - format is a simpler check.

13. The `outputDirFromFlags` test uses `newReviewCmd()` to create a new command. This is consistent with the rest of the codebase.

OK, I think the most significant issues are:

1. The `FilePath` function's `..` check is too strict and could reject legitimate filenames containing `..`. This is a potential false positive.

2. The `outputDirFromFlags` test doesn't cover the case of a relative path that resolves to a system dir.

3. The `outputDirFromFlags` test doesn't cover the case of a valid relative path (like `../reviews`).

Actually, let me look more carefully at the `outputDirFromFlags` function. I don't have the full function, but the diff shows:

```go
abs, err := filepath.Abs(outputDir)
if err != nil {
    return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
}
if err := validation.FilePath(abs); err != nil {
    return "", usageError(err)
}
```

The `outputDir` variable is the raw value from the flag. After `filepath.Abs`, it's the absolute, cleaned path. Then `FilePath` is called on the absolute path.

If `outputDir` is empty, `filepath.Abs("")` returns the current directory. Then `FilePath` would check if the current directory is a system dir. If it is, the validation would fail. This could be unexpected behavior.

Wait, but the existing function probably handles the empty case before calling `filepath.Abs`. Let me check the diff again... The diff only shows the addition of the `FilePath` check. The existing code presumably handles the empty case.

Actually, I don't have the full function. Let me look at the existing test `TestOutputDirFromFlags_WhitespacePaddedResolvesTrimmed` to understand the function better. The test uses `cmd.ParseFlags([]string{"--output-dir", " "})` and expects `filepath.Join(cwd, "out")`. This suggests the function trims whitespace and uses a default if empty.

OK, I think the main issues to report are:

1. The `FilePath` function's `..` check is too strict (MEDIUM - potential false positive for legitimate filenames).

2. Missing test coverage for relative path that resolves to system dir (LOW).

3. Missing test coverage for valid relative path (LOW).

4. `FilePath` doesn't check for null bytes (LOW - minor security concern).

Actually, wait. Let me re-examine the `..` check issue more carefully.

The function is called after `filepath.Abs`. `filepath.Abs` calls `filepath.Clean` internally. `filepath.Clean` resolves `..` path components. So after `filepath.Abs`:
- `../foo` → `/parent/foo` (if cwd is `/parent`)
- `foo/../bar` → `bar`
- `foo..bar` → `foo..bar` (unchanged, since `..` is not a path component)
- `file..txt` → `file..txt` (unchanged)

So the `..` check would only catch filenames containing `..` (like `file..txt`). This is a potential false positive.

But the sprint plan says: "No path traversal" in the spec. The function is supposed to reject `..`. The sprint plan doesn't distinguish between path traversal sequences and `..` in filenames.

However, in practice, this could cause issues. For example, if a user wants to save a report to `report..md`, it would be rejected. This seems like an overly strict check.

I think this is worth reporting as a MEDIUM issue.

Let me also think about the `FilePath` function's behavior with `..` in the middle of a path:

- `foo/..bar` → after `filepath.Abs`, this is `cwd/foo/..bar`. The `..` check would find `..` in `..bar` and reject it. This is a false positive.

So the `..` check is definitely too strict.

Actually, wait. Let me re-read the sprint plan:

> No path traversal
> if strings.Contains(path, "..") {
>     return &ValidationError{"file path", path, "must not contain .."}
> }

The sprint plan explicitly says `strings.Contains(path, "..")`. So the function is consistent with the spec. But the spec itself is potentially too strict.

I think this is worth reporting as a MEDIUM issue with a suggestion to use a more precise check (like checking for `..` as a path component rather than a substring).

Let me also check if there are any other issues:

15. The `outputDirFromFlags` function might have a check for empty `outputDir` that I'm not seeing. If it doesn't, then `filepath.Abs("")` would return the current directory, and `FilePath` would check if it's a system dir. This could be unexpected.

Actually, I just realized I don't have the full `outputDirFromFlags` function. The diff only shows the addition. Let me check if there's a check for empty `outputDir` in the existing code...

Looking at the test `TestOutputDirFromFlags_WhitespacePaddedResolvesTrimmed`, it uses `cmd.ParseFlags([]string{"--output-dir", " "})` and expects `filepath.Join(cwd, "out")`. This suggests the function trims whitespace. But what about empty input?

I don't have enough context to determine if the existing function handles the empty case. But this is outside the scope of the diff (the diff only adds the `FilePath` check).

OK, let me finalize my findings:

1. MEDIUM: `FilePath` function's `..` check is too strict - it rejects any path containing `..`, including legitimate filenames like `file..txt` or `..bar`. After `filepath.Abs`, path traversal sequences are resolved, so any remaining `..` is in a filename. The check should use a more precise pattern (like checking for `..` as a path component).

2. LOW: Missing test coverage for relative path that resolves to a system dir (e.g., if cwd is `/etc`).

3. LOW: Missing test coverage for valid relative path (like `../reviews`).

4. LOW: `FilePath` doesn't check for null bytes, which are invalid in Unix paths.

Actually, let me reconsider. The sprint plan explicitly says:

> if strings.Contains(path, "..") {
>     return &ValidationError{"file path", path, "must not contain .."}
> }

So the function is consistent with the spec. The spec itself is potentially too strict, but that's a design decision made in the sprint plan. I should note this as a potential issue but acknowledge that it's consistent with the spec.

Let me also check if there are any issues with the `outputDirFromFlags` test:

The test `TestOutputDirFromFlags_SystemDirRejected`:
```go
cmd := newReviewCmd()
require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "/etc/atcr"}))
_, err := outputDirFromFlags(cmd)
require.Error(t, err)
require.Equal(t, 2, exitCode(err))
require.Contains(t, err.Error(), "must not reference system directories")
```

This test looks correct. It creates a new command, parses the flags, calls the function, and checks the error.

The test `TestReportCmd_SystemDirOutputIsUsageError`:
```go
isolate(t)
fixtureReconciled(t, "r", oneFinding)
code, out := execCmdCapture(t, "report", "--output", "/etc/atcr-report.md", "r")
require.Equal(t, 2, code)
require.Contains(t, out, "must not reference system directories")
```

This test looks correct. It uses the existing test helpers.

OK, let me also check the `validation_test.go` file:

The tests cover:
- `TestValidationError_Error` - tests the error message format
- `TestGitRef` - tests valid refs, `..`, invalid characters, empty, over-length
- `TestFilePath` - tests valid paths, empty, `..`, system dirs, directory-boundary match
- `TestReviewID` - tests valid IDs, empty, path traversal, over-length
- `TestSeverity` - tests valid severities, invalid, case-insensitive
- `TestEnum` - tests valid values, invalid

The tests look comprehensive. The `TestFilePath` test covers the directory-boundary match (siblings of system dirs are not rejected).

Wait, the test covers `/etcd/data` and `/etc-backup` as valid paths. This confirms that the `..` check is applied to the path string, and the directory-boundary check is correct.

Let me also check if there are any issues with the `outputDirFromFlags` function's behavior with whitespace:

The existing test `TestOutputDirFromFlags_WhitespacePaddedResolvesTrimmed` uses `" "` (space) as input. After trimming, it becomes `out`. After `filepath.Abs`, it becomes `cwd/out`. The `FilePath` function would check for `..` (not found) and system dirs (not found). So the validation passes. Good.

But what about `" .."` (space followed by `..`)? After trimming, it becomes `..`. After `filepath.Abs`, it becomes the parent directory. If the parent directory is a system dir, the validation would fail. If not, it would pass. This is correct behavior.

OK, I think I've covered all the major issues. Let me finalize my findings:

1. MEDIUM: `FilePath` function's `..` check is too strict - it rejects any path containing `..`, including legitimate filenames like `file..txt` or `..bar`. This is consistent with the sprint plan spec, but could cause false positives in practice.

Actually, I realize I should also check if there are any issues with the `GitRef` function:

The `GitRef` function checks for `..`, `~`, `^`, `:`, space, tab, newline. It does NOT check for `\`, `[`, `]`, or other special characters that git might reject. But this is a subset of git-check-ref-format rules, which is acceptable per the sprint plan.

Also, the `GitRef` function does NOT check for `@{` which is used in reflogs. But this is a revision, not a ref name. The sprint plan says `GitRef` is for ref names, not revisions. So this is acceptable.

OK, let me also check if there are any issues with the `ReviewID` function:

The `ReviewID` function checks for alphanumeric, dash, underscore. It does NOT check for `.` (dot). The sprint plan says "alphanumeric, dash, underscore" in the spec, so this is correct.

But wait, the `fanout.ValidateReviewID` function (mentioned in the sprint plan clarifications) uses `^[A-Za-z0-9][A-Za-z0-9._-]*$` which allows dots. The `validation.ReviewID` function is NOT wired onto `--id`, so this difference doesn't matter in practice.

OK, I think I've covered all the major issues. Let me write up my findings.

Actually, one more thing. Let me check the `outputDirFromFlags` function more carefully. I don't have the full function, but the diff shows:

```go
func outputDirFromFlags(cmd *cobra.Command) (string, error) {
    ...
    abs, err := filepath.Abs(outputDir)
    if err != nil {
        return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
    }
    if err := validation.FilePath(abs); err != nil {
        return "", usageError(err)
    }
    return abs, nil
}
```

The `...` is the existing code. I don't know what it does. But the addition looks correct.

Let me also check the `report.go` function:

```go
if output, _ := cmd.Flags().GetString("output"); output != "" {
    abs, err := filepath.Abs(output)
    if err != nil {
        return usageError(fmt.Errorf("resolving --output: %w", err))
    }
    if err := validation.FilePath(abs); err != nil {
        return usageError(err)
    }
}
```

This looks correct. The `output` flag is checked only when non-empty.

OK, I think the main issues are:

1. MEDIUM: `FilePath` function's `..` check is too strict.

Let me also think about whether the `..` check is actually a problem in practice. The `FilePath` function is applied to:
- `review --output-dir`: this is a directory path, not a file path. Directories with `..` in the name are extremely rare.
- `report --output`: this is a file path. Files with `..` in the name are also rare (though possible).

In practice, the `..` check is unlikely to cause false positives. But it's still worth noting as a potential issue.

Actually, I just realized that the `..` check is specifically designed to prevent path traversal. After `filepath.Abs`, path traversal sequences are resolved, so any remaining `..` is in a filename. The check is too broad for its intended purpose.

A more precise check would be to look for `..` as a path component (i.e., preceded and followed by `/` or at the start/end of the path). But this is a design decision, not a bug.

OK, let me finalize my findings. I'll report the `..` check as a MEDIUM issue, and note that it's consistent with the sprint plan spec.

Actually, wait. Let me re-read the sprint plan spec one more time:

> No path traversal
> if strings.Contains(path, "..") {
>     return &ValidationError{"file path", path, "must not contain .."}
> }

The spec explicitly says `strings.Contains(path, "..")`. So the function is consistent with the spec. The spec itself is potentially too strict, but that's a design decision made in the sprint plan.

I think the most honest finding is to note that the `..` check is consistent with the spec but could cause false positives in edge cases. This is a LOW or MEDIUM issue depending on how likely it is to occur in practice.

Let me also think about whether there are any other issues I've missed:

- The `outputDirFromFlags` test doesn't cover the case of a valid relative path (like `../reviews`). This is mentioned in the sprint plan as a key behavior. But there might be other tests that cover it.

- The `outputDirFromFlags` test doesn't cover the case of a relative path that resolves to a system dir.

- The `FilePath` function doesn't check for null bytes.

- The `GitRef` function doesn't check for `@{` (reflog syntax).

- The `ReviewID` function doesn't allow dots, while `fanout.ValidateReviewID` does. This is intentional per the sprint plan clarifications.

OK, I think I have a good list of findings. Let me write them up.

Wait, I should also check if there are any issues with the `outputDirFromFlags` function's behavior when the input is empty. If `outputDir` is empty, `filepath.Abs("")` returns the current directory. Then `FilePath` would check if the current directory is a system dir. If it is, the validation would fail. This could be unexpected.

But the existing function probably handles the empty case before calling `filepath.Abs`. I don't have the full function, so I can't be sure.

Let me look at the diff one more time:

```
@@ -66,6 +67,13 @@ func outputDirFromFlags(cmd *cobra.Command) (string, error) {
 	if err != nil {
 		return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
 	}
+	// Validate the resolved (absolute, cleaned) path, not the raw value: a
+	// legitimate relative path like ../reviews resolves clean and passes, while
+	// a path under a system directory (/etc, /proc, /sys) is rejected by the
+	// input-validation layer (exit 2) before any review work begins.
+	if err := validation.FilePath(abs); err != nil {
+		return "", usageError(err)
+	}
 	return abs, nil
 }
```

The diff shows the function context. The line before the `filepath.Abs` call is `if err != nil` which suggests there's a preceding block. I don't have the full function, but the addition looks correct.

OK, I think I've covered all the major issues. Let me write up my findings.

Actually, I want to double-check one more thing. The `outputDirFromFlags` function is called from `run
</think>
