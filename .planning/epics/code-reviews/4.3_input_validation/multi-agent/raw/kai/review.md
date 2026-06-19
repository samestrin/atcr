The user wants me to review a diff against a sprint plan scope. Let me analyze the diff carefully.

Files changed:
1. cmd/atcr/report.go - adds validation.FilePath on --output post-Abs
2. cmd/atcr/report_test.go - adds test for system dir output rejection
3. cmd/atcr/review.go - adds validation.FilePath on --output-dir post-Abs
4. cmd/atcr/review_test.go - adds test for system dir rejection on output-dir
5. internal/boundaries_test.go - adds validation package to allowed imports
6. internal/validation/validation.go - new validation package
7. internal/validation/validation_test.go - tests for validation package

Scope constraints:
- IN scope: internal/validation package, FilePath wiring on review --output-dir and report --output, errors wrapped in usageError
- OUT of scope: wiring GitRef onto --base/--head, replacing existing severity/format validation, wiring ReviewID onto --id, config/persona/model/API-key validation

Let me look for issues in the diff:

1. In internal/validation/validation.go:
   - `reviewIDPattern` uses a compiled regex but in GitRef there's `regexp.MatchString` in the design doc, but in actual code they use `strings.Contains` for GitRef and compiled regex for ReviewID. That's fine.
   - Wait, looking at the code: GitRef uses `strings.Contains` and `strings.ContainsAny`, not regex. That's fine.
   - FilePath checks `strings.Contains(path, "..")` - this might reject paths that contain ".." even if they're not traversing? Actually the comment says "no path traversal" but the check is `strings.Contains(path, "..")`. This means a legitimate directory named `foo..bar` or file named `..gitignore` or path with `...` would be rejected. However, looking at the sprint plan, the design explicitly says `strings.Contains(path, "..")`. And the scope says this is the validation to ship. But wait - is this an issue? The sprint plan explicitly designed it this way. But as a code reviewer, is this a real issue? A path like `foo..bar/baz` would be rejected. However, the sprint plan says "No path traversal" and uses `strings.Contains(path, "..")`. Is this a correctness issue? It might reject valid paths. But given the sprint plan explicitly specifies this implementation, and the review scope says to review the diff against the sprint plan... Hmm. Actually, the user says "Review the following diff and identify any issues." and "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)." Wait, no - it says if it's in a file not mentioned or addresses concerns outside goals, mark as OUT-OF-SCOPE (do not include). So I should NOT include out-of-scope findings. But I should flag issues in scope.

Let me think about real issues in the diff:

**Issue 1: FilePath contains ".." check is overly broad**
`strings.Contains(path, "..")` will reject paths like `foo..bar`, `...`, etc. This is a correctness issue. The proper check would be to check for path traversal specifically, e.g., using `strings.Contains(path, "../")` or `path == ".."`, or better yet, checking if after filepath.Clean there are ".." components. But wait - the sprint plan explicitly says:
```go
// No path traversal
if strings.Contains(path, "..") {
    return &ValidationError{"file path", path, "must not contain .."}
}
```
So this is implementing the sprint plan as specified. But is it an issue? The sprint plan itself has this. However, the instructions say "No false positives: Only report real issues". Is this a real issue? Yes, it rejects legitimate paths containing ".." as a substring. But it matches the sprint plan spec. Should I flag it? The scope constraint says to apply the scope. The sprint plan defines what is in scope. The FilePath validator is in scope. If the implementation matches the sprint plan but has a bug, I should still flag the bug because the sprint plan doesn't override correctness. But wait, the sprint plan explicitly called for `strings.Contains(path, "..")`. However, the user instruction says "Be precise: Reference specific line numbers. Be actionable: Every issue must have a concrete fix. Be prioritized: HIGH = must fix, MEDIUM = should fix, LOW = nice to have. No false positives: Only report real issues."

Actually, looking more carefully at the scope constraint: "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

This means I should NOT include findings about things outside the scope. But a correctness bug in a file that IS in scope should still be reported. The sprint plan defines the scope boundaries (which files, which features). It doesn't mean I should ignore bugs in those files. The scope is to limit where I look, not to approve the implementation details. So if `strings.Contains(path, "..")` is overly broad, it's a correctness issue in `internal/validation/validation.go` which is in scope.

Wait, but the sprint plan literally says:
```go
if strings.Contains(path, "..") {
    return &ValidationError{"file path", path, "must not contain .."}
}
```
And the diff implements exactly that. But as a code reviewer, I should point out that this is buggy because it rejects `foo..bar`. However, is this a false positive in my review? The code does exactly what the spec says. But the spec itself might be flawed. The instructions say "Review the following diff and identify any issues." It doesn't say "only identify deviations from the sprint plan". It says identify issues in the diff, scoped to the sprint plan. So if the diff introduces a bug (even if the spec calls for it), I should flag it? Hmm.

Actually, I think the intent is: don't nitpick unrelated code. But for code that is in scope, apply normal code review standards. The fact that the sprint plan contains pseudocode with the bug doesn't mean I shouldn't flag the bug in the actual implementation. The sprint plan is a plan, not a specification that overrides correctness.

So: `strings.Contains(path, "..")` is too broad. It should be something like `strings.Contains(path, "/../") || strings.HasPrefix(path, "../") || strings.HasSuffix(path, "/..") || path == ".."` or use `filepath.Clean` and check for `..` components. Actually, since the path is already absolute when FilePath is called (in the integration points), `filepath.Clean` on an absolute path will strip `..` if they try to escape, but if it stays within the tree, it might be okay? Wait, `filepath.Clean` resolves `..` but for absolute paths, `..` can be valid if it's not escaping root. Actually on Unix `/foo/../bar` cleans to `/bar`. But `/foo/..` cleans to `/`. The issue is detecting traversal out of a base directory. But here there's no base directory - it's just system paths that are forbidden. The `..` check is meant to prevent traversal. But `strings.Contains(path, "..")` catches `foo..bar`.

Given that the path is validated post-Abs, and `filepath.Abs` also cleans the path (resolves `.` and `..` and symlinks? No, Abs doesn't resolve symlinks, but it does clean). Wait, `filepath.Abs` calls `filepath.Clean` internally (or similar). Actually in Go, `filepath.Abs` returns the absolute representation of the path, and it calls `Clean` on the result. So after `filepath.Abs`, there should be no `..` remaining unless it's a valid filename? No, `Clean` removes `..` by collapsing them. So if you pass `foo..bar`, Abs returns `/cwd/foo..bar` which does NOT contain `..` as a substring? Wait, `foo..bar` contains `..` as a substring. `strings.Contains("/cwd/foo..bar", "..")` is true!

So a file named `foo..bar` would be rejected. That's definitely a bug.

But wait - `filepath.Abs` does clean the path, so `../reviews` becomes `/reviews` or whatever. But `foo..bar` stays `foo..bar`. Yes, this is a real correctness issue.

However, looking at the sprint plan's recorded clarification: "Q2: FilePath on --output-dir — validate raw value or post-Abs? A: Option A — apply FilePath after filepath.Abs". The path is absolute and cleaned. The `..` substring check is still there from the original design. But now that it's post-Abs, a path like `/home/user/foo..bar` would be rejected. That's incorrect.

Fix: Use a proper path traversal check, e.g., use `strings.Contains(path, "/../")` or check path components. Or since it's post-Abs, check if the original relative path contained `..`? No, the point of post-Abs is to preserve relative paths. But the `..` check is now problematic.

Actually, looking at the code in review.go:
```go
abs, err := filepath.Abs(outputDir)
if err != nil {
    return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
}
if err := validation.FilePath(abs); err != nil {
    return "", usageError(err)
}
```

If user passes `--output-dir ../reviews`, after Abs it becomes `/home/user/reviews` (assuming cwd is /home/user/project). That resolved path is fine. But if user passes `--output-dir /etc/passwd`, it's caught by the sysdir check. The `..` check is meant to catch traversal attempts, but after Abs, traversal is already resolved! So the `..` check in FilePath is both:
1. Overly broad (rejects `foo..bar`)
2. Redundant post-Abs (since Abs resolves `..`)

Wait, does `filepath.Abs` resolve `..`? Let me think. `filepath.Abs` calls `filepath.join()` with the working directory and the path. It might not fully clean it on all platforms? Actually, `filepath.Abs` does return an absolute path, and the docs say it calls `Clean` on the result. Yes, `filepath.Abs` returns the absolute path, and it is equivalent to `join(cwd, path)` then `Clean`. So `..` is resolved.

So post-Abs, a path containing `..` as a substring can only mean `..` is part of a filename. Thus the check is wrong.

But is this in scope? Yes, `internal/validation/validation.go` is in scope. The FilePath validator is in scope.

**Issue 2: FilePath system directory check has a prefix issue?**
The code checks:
```go
for _, sysDir := range []string{"/etc", "/proc", "/sys"} {
    if path == sysDir || strings.HasPrefix(path, sysDir+"/") {
        return &ValidationError{"file path", path, "must not reference system directories"}
    }
}
```
This looks correct. `/etc` matches exactly, `/etc/passwd` matches prefix, `/etcd` does not match, `/etc-backup` does not match. Good.

**Issue 3: GitRef regex?**
No regex used. `strings.ContainsAny(ref, "~^: \t\n")`. Wait, what about carriage return `\r`? And other control characters? The sprint plan says "no control chars" but only checks `\t\n`. What about `\r`? The comment says "no control chars" but the code doesn't check them all. However, the sprint plan explicitly lists only those. Is this an issue? It's a minor discrepancy between comment and code. But the sprint plan spec says that exact code. I think this is not a real security issue given the scope.

Actually, looking at the code:
```go
// Git ref rules: no .., no ~, no ^, no :, no space, no control chars
if strings.Contains(ref, "..") || strings.ContainsAny(ref, "~^: \t\n") {
```
The comment says "no control chars" but only checks tab and newline. Carriage return `\r` and other control characters are not checked. However, this is a minor issue. But wait - is GitRef even wired up? The scope says GitRef ships in the package but is NOT wired onto --base/--head. So it's only used by tests. Is it worth flagging? Since it's not wired, the impact is low. But the acceptance criteria say `validation.GitRef` must work. AC1 and AC2 are about GitRef. The package is in scope. I could flag it as LOW or not flag it. Given that the sprint plan explicitly shows this code, and it's not wired, I'll skip it to avoid false positives/scope creep.

**Issue 4: ReviewID regex**
```go
var reviewIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
```
This is fine. Pre-compiled.

**Issue 5: Severity map**
```go
valid := map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true}
```
Created every time. Not a big deal for a tiny map.

**Issue 6: Enum efficiency**
Loops over allowed. Fine.

**Issue 7: report.go integration**
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
This wraps validation.FilePath error in usageError. But validation.FilePath returns a ValidationError which already has a nice message. Wrapping it in usageError is fine.

But wait - `usageError` is a function that presumably marks the error as a usage error (exit code 2). The sprint plan says "Wrap validation errors in usageError for exit code 2." This looks correct.

**Issue 8: review.go integration**
```go
func outputDirFromFlags(cmd *cobra.Command) (string, error) {
    // ... existing code ...
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
This is correct per scope.

**Issue 9: tests**
The tests look fine.

**Issue 10: boundaries_test.go**
Added `validation` with empty deps. Good.

**Issue 11: `cmd/atcr/report.go` missing `filepath` import?**
Wait, the diff shows:
```diff
 import (
 	"errors"
 	"fmt"
 	"os"
+	"path/filepath"
 
 	"github.com/samestrin/atcr/internal/reconcile"
 	"github.com/samestrin/atcr/internal/report"
+	"github.com/samestrin/atcr/internal/validation"
 	"github.com/spf13/cobra"
 )
```
Yes, `filepath` is imported.

**Issue 12: report.go - `abs` variable but not used?**
Wait, in report.go:
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
The `abs` is passed to `validation.FilePath`. But then the function continues using `output` (the original relative path?) for the actual file operation? Let me check the rest of report.go. The diff doesn't show the rest. But presumably the report logic uses `cmd.Flags().GetString("output")` again later. That's okay, the validation ensures the path is safe. But wait - if the path is relative, they validate the absolute form, but then write to the relative form. Is that an issue? Not really, since the filesystem will resolve it the same way (assuming cwd hasn't changed). However, there might be a TOCTOU issue, but that's not in scope.

Actually, looking more carefully at report.go - the diff only shows adding the validation block. The actual output file writing presumably happens later using the flag value. Since `filepath.Abs` was used for validation, and the actual write uses the original path, they might write to a different location if cwd changes between validation and write (unlikely in a CLI). Not worth flagging.

**Issue 13: `validation.FilePath` rejects empty string**
`FilePath` returns error on empty. But in both integration points, empty is checked before calling FilePath (`output != ""`, and in review.go, the default is handled). So that's fine.

**Issue 14: `review_test.go` uses `exitCode(err)`**
Is `exitCode` a helper that extracts the exit code? Presumably yes.

**Issue 15: FilePath allows absolute paths not in system dirs**
What about `C:\Windows\System32` on Windows? The doc says it's inert on Windows. But the sprint plan says "The system-directory guard is oriented at Unix absolute paths. On Windows... it is inert; callers targeting Windows need volume-aware system-path detection (tracked as technical debt)." This is a known limitation documented in the code. Not an issue to flag.

**Issue 16: `report.go` order of validation**
In `runReport`, the format is validated first:
```go
if !report.ValidFormat(format) {
    return usageError(fmt.Errorf("unknown format %q: supported formats are %s", format, report.Formats()))
}
```
Then output path is validated. This is fine.

**Issue 17: `GitRef` length check**
`len(ref) > 255` - for Unicode/UTF-8, `len` counts bytes, not runes. Git ref names are byte strings, so this is correct.

**Issue 18: `ReviewID` length check**
`len(id) > 100` - same, counts bytes. The pattern is ASCII-only, so bytes == runes. Fine.

**Issue 19: `strings.Contains(path, "..")` in FilePath**
I think this is the main real issue. Let me decide whether to flag it.

If a user tries to write to `--output foo..bar.md`, it will be rejected. But `foo..bar.md` is a perfectly valid filename. This is a false positive in the validator. The sprint plan explicitly wrote this code, but it's a bug. A code reviewer should flag it. The scope says the `internal/validation` package is in scope. So I will flag it.

How to fix? Since the callers resolve to absolute paths first, the `..` components are already resolved by `filepath.Abs`. So the `strings.Contains(path, "..")` check is redundant and harmful. It should be removed, or changed to only check raw relative inputs. But since the validator is documented as "Callers that accept relative paths should resolve them to an absolute, cleaned form (filepath.Abs) before validating", the `..` check inside FilePath is unnecessary for resolved paths and harmful for legitimate filenames.

Wait, but what if someone calls FilePath directly with a raw path that contains `../`? The function is public. If a caller doesn't abs first, they might want traversal caught. But the doc says callers should abs first. The check `strings.Contains(path, "..")` catches traversal attempts in raw paths but over-rejects.

Better fix: Remove the `..` check entirely since post-Abs paths are cleaned and `..` is resolved, or use a more precise check like checking if the cleaned path still escapes a base directory (but there is no base directory here). Since the function checks system directories and empty path, and `filepath.Abs` is documented as the caller's responsibility, the `..` substring check can just be removed. Or changed to: if strings.Contains(path, "/../") || strings.HasPrefix(path, "../") || strings.HasSuffix(path, "/..") || path == "..".

But actually, since the path is already absolute, `filepath.Clean` (called by Abs) resolves all `..`. So any `..` remaining must be part of a filename. Thus the check should be removed.

Wait, what if caller passes `../etc/passwd`? Abs resolves it to `/etc/passwd` (assuming cwd is /something). Then the sysdir check catches it! So `..` is redundant. If caller passes `foo/../../etc/passwd`, Abs resolves to `/etc/passwd`, caught by sysdir. If caller passes `../reviews`, Abs resolves to a normal path, which is fine. So indeed, for absolute paths, the `..` check is redundant for security (sysdir catches the bad ones) and harmful for legitimate names.

I'll flag this as MEDIUM correctness issue.

**Issue 20: `GitRef` control characters**
The comment says "no control chars" but code only checks `\t\n`. This is a discrepancy. But since GitRef is not wired up per scope, and the sprint plan explicitly shows that code, I will not flag it to avoid false positives / out-of-scope concerns. Actually, the package is in scope, but the specific function is shipped per AC1/AC2/AC7. So it is in scope. Is it a real issue? It's a minor inaccuracy. I'll skip it because the sprint plan explicitly requested that implementation and it's not wired to user input.

**Issue 21: `cmd/atcr/report.go` and `cmd/atcr/review.go` error wrapping**
In report.go:
```go
if err := validation.FilePath(abs); err != nil {
    return usageError(err)
}
```
In review.go:
```go
if err := validation.FilePath(abs); err != nil {
    return "", usageError(err)
}
```
This wraps the ValidationError in usageError. That's correct per AC6.

**Issue 22: `report_test.go` and `review_test.go`**
Tests look good.

**Issue 23: `internal/validation/validation.go` package comment**
Package comment says "Each validator performs no I/O". Good.

**Issue 24: `FilePath` and `path == sysDir`**
What about trailing slash? `/etc/`? `filepath.Abs` cleans trailing slashes (except root). So `/etc` is the form. But if someone passes `/etc/` on Unix, Abs returns `/etc`. So it's fine.

Wait, what if `path` is `/etc`? It matches `path == sysDir`. What if it's `/etc/`? Abs cleans it to `/etc`. Good.

**Issue 25: `FilePath` on Windows**
On Windows, `filepath.Abs` might return `C:\etc` for `\etc`? Actually `filepath.Abs` on Windows with a path like `\etc` would return `C:\etc` (or whatever the current drive is). The system dir check only checks Unix paths starting with `/etc`, `/proc`, `/sys`. On Windows, these checks won't trigger because absolute paths start with `C:\` etc. So `C:\Windows\System32` passes. This is documented technical debt. Not flagging.

**Issue 26: `review.go` outputDirFromFlags returns `usageError` for filepath.Abs error**
The existing code already returned `usageError` for Abs error. The diff preserves this.

**Issue 27: `cmd/atcr/report.go` line 42**
```go
if output, _ := cmd.Flags().GetString("output"); output != "" {
```
Discarding the error from GetString. Is this okay? `GetString` on a registered flag shouldn't error. This is idiomatic in Cobra. Not an issue.

**Issue 28: `validation.FilePath` allows symlinks to system directories**
If path is `/tmp/link-to-etc`, it passes because it's not under `/etc` prefix. But that's a symlink. The validator performs no I/O (as documented), so it doesn't resolve symlinks. This is a known limitation of input validation without I/O. Not flagging.

**Issue 29: `reviewIDPattern` is package level**
`var reviewIDPattern = regexp.MustCompile(...)` - this panics on invalid regex at init. The regex is hardcoded and valid. Fine.

**Issue 30: `strings.Contains(path, "..")` false positive example**
`foo..bar` - valid filename, rejected. This is the main issue.

Let me reconsider if there are any other issues.

Wait, looking at `cmd/atcr/report.go`:
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
The variable `abs` is computed but never stored back to `output` or used in the actual write. The actual report rendering likely uses the `output` flag value again later. If the working directory changes between this validation and the actual write (unlikely but possible in concurrent code?), it could differ. More importantly, if `output` is relative, the actual write will use the relative path, which is fine because the process hasn't changed directory. However, if the code later does its own Abs or evaluates relative to a different base, there could be inconsistency. But I don't see evidence of that in the diff. Not flagging.

Wait, actually there is a subtle issue: the `abs` is validated, but the original `output` string is used for the actual file operation. If `output` contains `.` or `..` or is relative, it might be resolved differently if the cwd changes. But in a CLI command handler, cwd doesn't change. This is not a real issue.

Another thing: In `report.go`, what if `output` is a directory? `filepath.Abs` works. `validation.FilePath` allows directories (no check for file vs dir). That's fine.

What about `cmd/atcr/review.go` - `outputDirFromFlags` returns `abs` which is validated. The caller uses the returned `abs`. That's good.

Are there any security issues?
- The FilePath validator is meant to prevent path traversal. But `strings.Contains(path, "..")` is insufficient and overbroad. As discussed, post-Abs makes it overbroad.
- The system directory guard only checks `/etc`, `/proc`, `/sys`. What about `/bin`, `/usr`, `/var`? The sprint plan specifies only those. Not flagging.

Are there any error handling issues?
- `validation.FilePath` returns error. The callers wrap in `usageError`. Correct.

Are there any performance issues?
- The map in `Severity` is created every call. Could be a package var. But trivial. Not flagging.

Are there any maintainability issues?
- The `FilePath` function's `..` check is incorrect. That's a correctness issue.

Let me check if there's an issue with `filepath.Abs` error handling. On Windows, `filepath.Abs` can fail if the working directory is deleted or permissions issues. The error is wrapped in `usageError`. Fine.

Wait, looking at `cmd/atcr/report.go`:
```go
abs, err := filepath.Abs(output)
if err != nil {
    return usageError(fmt.Errorf("resolving --output: %w", err))
}
```
But then if validation fails, it returns `usageError(err)`. The `err` from `filepath.Abs` is wrapped with a message. The validation err is passed directly. This is consistent.

One more thing: the `boundaries_test.go` diff. It adds `"validation": {}`. But does it preserve the comment alignment? The diff shows reformatted alignment for all entries. That's just formatting.

Is there an issue with the `review_test.go` test?
```go
func TestOutputDirFromFlags_SystemDirRejected(t *testing.T) {
    cmd := newReviewCmd()
    require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "/etc/atcr"}))
    _, err := outputDirFromFlags(cmd)
    require.Error(t, err)
    require.Equal(t, 2, exitCode(err))
    require.Contains(t, err.Error(), "must not reference system directories")
}
```
This tests the function correctly. `exitCode` is presumably a test helper.

Similarly `report_test.go`:
```go
func TestReportCmd_SystemDirOutputIsUsageError(t *testing.T) {
    isolate(t)
    fixtureReconciled(t, "r", oneFinding)
    code, out := execCmdCapture(t, "report", "--output", "/etc/atcr-report.md", "r")
    require.Equal(t, 2, code)
    require.Contains(t, out, "must not reference system directories")
}
```
Looks good.

Wait, I see `TestReportCmd_SystemDirOutputIsUsageError` uses `execCmdCapture` and checks output string. But it doesn't check that no file is created. Not an issue.

Let me think about whether I should flag the `strings.Contains(path, "..")` issue. Yes, I believe I should. It's a real bug in in-scope code.

SEVERITY: MEDIUM
FILE:LINE: `internal/validation/validation.go:63` (or around there)
Let me check exact line numbers in the diff.

In `internal/validation/validation.go`:
Line 63 is probably:
```go
	if strings.Contains(path, "..") {
```
Looking at the diff:
```go
+	// No path traversal.
+	if strings.Contains(path, "..") {
+		return &ValidationError{"file path", path, "must not contain .."}
+	}
```
The line number would be around 63. Let me count from the diff.

The file starts at line 1. Let's count:
1: package comment (multi-line)
...
Around line 60-63. I'll say `internal/validation/validation.go:63` as an approximate line number? Wait, the diff doesn't show line numbers for the new file. Since it's a new file, line numbers start from 1. Let me count more carefully.

```go
 1 // Package validation centralizes user-input validation for the atcr CLI: git
 2 // refs, file paths, review IDs, severities, and arbitrary enums. Each validator
 3 // performs no I/O and returns a *ValidationError so callers can wrap it in a
 4 // usage error (exit 2) for a consistent, field-aware message across commands.
 5 package validation
 6 
 7 import (
 8 	"fmt"
 9 	"regexp"
10 	"strings"
11 )
12 
13 // ValidationError is a user-facing input validation error. It names the field,
14 // echoes the offending value, and explains the expected format.
15 type ValidationError struct {
16 	Field   string
17 	Value   string
18 	Message string
19 }
20 
21 func (e *ValidationError) Error() string {
22 	return fmt.Sprintf("invalid %s %q: %s", e.Field, e.Value, e.Message)
23 }
24 
25 // GitRef validates a git ref name (branch, tag, or SHA). It applies a subset of
26 // git-check-ref-format rules — no "..", no "~^: " or control chars, and a length
27 // bound. It is intentionally NOT applied to the --base/--head flags, which take
28 // git *revisions* (HEAD^, HEAD~3, @{...}) that legitimately use these characters.
29 func GitRef(ref string) error {
30 	if ref == "" {
31 		return &ValidationError{"git ref", ref, "must not be empty"}
32 	}
33 	// Git ref rules: no .., no ~, no ^, no :, no space, no control chars.
34 	if strings.Contains(ref, "..") || strings.ContainsAny(ref, "~^: \t\n") {
35 		return &ValidationError{"git ref", ref, "contains invalid characters"}
36 	}
37 	if len(ref) > 255 {
38 		return &ValidationError{"git ref", ref, "must be <= 255 characters"}
39 	}
40 	return nil
41 }
42 
43 // FilePath validates a file path: non-empty, no path traversal (".."), and no
44 // reference to the system directories /etc, /proc, or /sys. Callers that accept
45 // relative paths should resolve them to an absolute, cleaned form (filepath.Abs)
46 // before validating, so a legitimate relative path is not rejected for "..".
47 //
48 // The system-directory guard is oriented at Unix absolute paths. On Windows
49 // (drive-letter/volume paths such as C:\Windows) it is inert; callers targeting
50 // Windows need volume-aware system-path detection (tracked as technical debt).
51 func FilePath(path string) error {
52 	if path == "" {
53 		return &ValidationError{"file path", path, "must not be empty"}
54 	}
55 	// No path traversal.
56 	if strings.Contains(path, "..") {
57 		return &ValidationError{"file path", path, "must not contain .."}
58 	}
59 	// No paths under the system directories /etc, /proc, /sys. Match a directory
60 	// boundary (the exact dir or a "<dir>/" prefix) so siblings like /etcd or
61 	// /system are not falsely rejected by a bare prefix check.
62 	for _, sysDir := range []string{"/etc", "/proc", "/sys"} {
63 		if path == sysDir || strings.HasPrefix(path, sysDir+"/") {
64 			return &ValidationError{"file path", path, "must not reference system directories"}
65 		}
66 	}
67 	return nil
68 }
```
So line 56 is the issue.

But wait, looking at the callers again:
- review.go calls `filepath.Abs` first, then `FilePath(abs)`.
- report.go calls `filepath.Abs` first, then `FilePath(abs)`.

Given that `filepath.Abs` resolves and cleans the path, any `..` sequences are collapsed. So after Abs, a path like `/home/user/foo..bar` still contains `..` as a substring but is a perfectly valid absolute path. The check on line 56 rejects it.

This is a correctness bug. I'll report it.

Is there anything else?

What about `GitRef` at line 34: `strings.ContainsAny(ref, "~^: \t\n")`. This checks for space, tab, newline. But not carriage return. However, the comment says control chars. This is a minor inconsistency, but the sprint plan explicitly has this. I won't flag it.

What about `reviewIDPattern`? It's a package-level var. If `regexp.MustCompile` panics, it happens at init. The pattern is valid. Fine.

What about `TestFilePath` in `validation_test.go`:
```go
for _, p := range []string{"/etcd/data", "/etc-backup", "/system/x", "/procession"} {
    require.NoError(t, FilePath(p), p)
}
```
This tests the sibling paths. But `/system/x` and `/procession` are tested against `/sys` prefix? Yes, `/system` does not start with `/sys/`, `/procession` does not start with `/proc/`. Good.

Wait, the test `TestFilePath` also tests:
```go
assert.EqualError(t, FilePath("../escape"), `invalid file path "../escape": must not contain ..`)
```
But per the sprint plan clarifications, callers should use `filepath.Abs` before calling `FilePath`. So the validator might receive relative paths only if callers don't follow the doc. The test is testing the validator in isolation. That's fine.

Actually, looking at the test, it tests `FilePath("../escape")` directly. This is fine.

Another thing: In `cmd/atcr/report.go`, the import order. The diff shows:
```go
 import (
 	"errors"
 	"fmt"
 	"os"
+	"path/filepath"
 
 	"github.com/samestrin/atcr/internal/reconcile"
 	"github.com/samestrin/atcr/internal/report"
+	"github.com/samestrin/atcr/internal/validation"
 	"github.com/spf13/cobra"
 )
```
This is standard Go formatting. Not an issue.

Wait, I should check if there's a missing blank line or something. No.

Is there a race condition or anything? No.

What about the `validation` package having no I/O but the tests are in the same package. Fine.

Alright, I think the only real issue is the `strings.Contains(path, "..")` false positive.

Wait, one more thought: `cmd/atcr/report.go` adds validation for `--output`. But what if `output` is `-` (stdout)? The flag is a string. Typically `-` means stdout in many CLI tools. Does the code handle that? The diff shows `if output != ""`. If output is `-`, it would try to validate `filepath.Abs("-")` which returns something like `/cwd/-`. That would pass FilePath. But then the report code might treat `-` as stdout. Is this an issue? The sprint plan says "empty --output = stdout". It doesn't mention `-`. The existing code might already handle `-`. But since the diff only adds validation and empty check is already there (`output != ""`), `-` would be validated. Unless the existing code treats `-` specially before this validation? The validation is added early in `runReport`. If the existing code later treats `-` as stdout, then this validation might break that. But I don't see `-` handling in the diff. The sprint plan says empty = stdout. So `-` would be treated as a filename unless existing code handles it. But that's outside the diff shown. Not flagging.

Actually, looking at the diff for report.go, the validation is added