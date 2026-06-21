<think>
Let me analyze this diff carefully for the Epic 5.0 file path validation work.

The scope is:
- File path validation in `internal/stream/parser.go` and related files
- Path warning metadata
- Report generation updates
- Unit tests for validation logic

Let me look at the changes:

1. **`cmd/atcr/reconcile.go`, `cmd/atcr/resume.go`, `cmd/atcr/review.go`, `internal/mcp/handlers.go`**: All add `Root: "."` to `reconcile.Options`. These are in scope.

2. **`internal/reconcile/emit.go`**: Adds `PathValid` and `PathWarning` fields to `JSONFinding`, propagates them from `Merged`, and adds a warning line in `writeFindingsList`. In scope.

3. **`internal/reconcile/gate.go`**: Calls `validateFindingPaths` after merge. In scope.

4. **`internal/reconcile/reconcile.go`**: Adds `Root` to `Options`. In scope.

5. **`internal/reconcile/validate.go`**: New file with `validateFindingPaths`. In scope.

6. **`internal/reconcile/validate_test.go`**: Tests. In scope.

7. **`internal/report/render.go`**: Adds `writePathWarning` to markdown, checklist, and refuted section. In scope.

8. **`internal/report/validate_test.go`**: Tests. In scope.

9. **`internal/stream/parser.go`**: Adds `PathValid` and `PathWarning` fields. In scope.

10. **`internal/stream/validate.go`**: New file with `ValidatePath`. In scope.

11. **`internal/stream/validate_test.go`**: Tests. In scope.

Now let me look for issues:

### Issue 1: `TestValidatePath_EmptyRootDefaultsToCwd` is a fragile test

```go
func TestValidatePath_EmptyRootDefaultsToCwd(t *testing.T) {
	f := Finding{File: "parser.go"}
	ValidatePath(&f, "")

	assert.True(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}
```

This test relies on `parser.go` existing in the current working directory. `go test` is typically run from the package directory (`internal/stream`), where `parser.go` does exist. But this is a fragile assumption - it could break if the test is run from a different directory or if the file is renamed. However, this is just a test issue, not a code issue.

### Issue 2: The `rel == ".."` check for path traversal

```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

This checks if the joined path is outside root. But what about a path that after cleaning contains `..` in the middle? Actually, `filepath.Join` calls `filepath.Clean`, so `../` segments are handled. The check seems correct: if `Rel` returns `..` (meaning the path is the parent of root, which shouldn't happen since root is absolute or relative) or starts with `../`, then it's escaping.

Wait, but there's a subtle issue: `filepath.Join` will clean the path. So if `f.File = "../foo"`, `filepath.Join(root, "../foo")` becomes `filepath.Dir(root) + "/foo"`. Then `filepath.Rel(root, ...)` would return `"../foo"`, which starts with `..` + separator. Good.

But what if `root` itself is relative? If `root = "internal"` and `f.File = "../foo"`, then `filepath.Join("internal", "../foo")` = `"foo"`. Then `filepath.Rel("internal", "foo")` could return `".."` (if "foo" is the parent of "internal", which it's not in the filesystem). Actually, `filepath.Rel` computes the relative path from root to target. If root is `"internal"` and target is `"foo"`, then:
- If both are relative to CWD, then `Rel("internal", "foo")` = `"../foo"`. This starts with `".." + separator`, so it would be caught. Good.

But wait, if `root` is relative and `f.File` is relative, the resolution depends on the CWD. This is a potential issue but not a bug per se - the CWD is the repo root for atcr.

### Issue 3: The `rel == ".."` case

When would `rel == ".."`? If the target is the parent directory of root. This shouldn't happen with normal paths from atcr (where root is the repo root). But it's a valid check.

### Issue 4: Absolute path handling

```go
f := Finding{File: "/etc/hosts"}
ValidatePath(&f, root)
```

The test `TestValidatePath_AbsolutePathNeutralized` expects this to be flagged invalid. Let's trace:
- `root = t.TempDir()` (something like `/tmp/xxx`)
- `filepath.Join(root, "/etc/hosts")` = `filepath.Join("/tmp/xxx", "/etc/hosts")` = `/tmp/xxx/etc/hosts`
- `filepath.Rel("/tmp/xxx", "/tmp/xxx/etc/hosts")` = `"etc/hosts"`
- This doesn't start with `".."`, so the escape check passes.
- `os.Stat("/tmp/xxx/etc/hosts")` returns `os.IsNotExist` true.
- So `f.PathValid = false`, `f.PathWarning = PathNotFoundWarning`.

Good, the test should pass. But is this the right behavior? An absolute path in a finding is almost certainly a bug. It would be better to flag it as invalid directly. But the current behavior is acceptable - it just won't find the file.

Wait, but what if someone runs atcr from `/` and has a file `/etc/hosts`? Then `filepath.Join("/", "/etc/hosts")` = `/etc/hosts`, and `filepath.Rel("/", "/etc/hosts")` = `"etc/hosts"`, and `os.Stat` succeeds. The finding would be marked valid! But this is an edge case that doesn't matter in practice.

### Issue 5: `writeFindingsList` in `internal/reconcile/emit.go` only adds warning in the findings list section

The `writeFindingsList` function renders the list of findings in the report. It adds the path warning after the finding line. But the refuted findings section in `report/render.go` has its own warning via `writePathWarning`. The reconcile emit's `writeFindingsList` is for the reconciled findings, and the report's `writeRefutedSection` handles refuted findings. These are different code paths.

Wait, let me re-read the reconcile emit code:

```go
func writeFindingsList(b *bytes.Buffer, findings []Merged) {
	...
	fmt.Fprintf(b, "- %s — confidence %s, reviewers: %s\n",
		codeSpan(m.File, m.Line), esc(m.Confidence), esc(joinOrNone(m.Reviewers)))
	if m.PathWarning != "" {
		fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
	}
	...
}
```

This is in `internal/reconcile/emit.go`. Where is this function called from? Let me check the diff... It's not shown, so I need to check if there's a call to this function in the new tests. The test `TestRunReconcile_FlagsHallucinatedPathEndToEnd` checks `report.md` content, not the findings list specifically.

### Issue 6: Report rendering order

In `internal/report/render.go`, the `writePathWarning` is called in:
1. `renderMarkdown` - after the finding line, before disagreement
2. `renderChecklist` - after the finding line
3. `writeRefutedSection` - after the finding line

This seems consistent.

### Issue 7: `writeFindingsList` in reconcile/emit.go vs `renderMarkdown` in report/render.go

Both render similar output. The `writeFindingsList` is for the reconciled findings section, and `renderMarkdown` in report is for the `atcr report` command. The question is: which one produces the `report.md` file?

Looking at the test:
```go
reportMD, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
```

This reads from the `reconciled` directory, which is produced by the reconcile pipeline. So `ReportMD` is likely produced by `internal/reconcile/emit.go`, not by `internal/report/render.go`.

But `internal/report/render.go` also renders markdown. Is there a duplication? Let me check... The report package's `Render` function is used by the `atcr report` command. The reconcile package's `writeFindingsList` is used during reconciliation.

The test `TestRender_Markdown_ShowsPathWarning` tests the report package's `Render` function with `FormatMarkdown`. This is the `atcr report` output.

The test `TestRunReconcile_FlagsHallucinatedPathEndToEnd` tests the reconcile pipeline's output, which includes the `reconciled/report.md` file.

Both need the warning. The diff adds `writePathWarning` in both places. Good.

### Issue 8: The `writeFindingsList` function is in `internal/reconcile/emit.go` but the test `TestRenderMarkdown_ShowsPathWarning` in `validate_test.go` calls `RenderMarkdown`

Let me check if `RenderMarkdown` is the same as `writeFindingsList` or if it's a wrapper. The test does:
```go
require.NoError(t, RenderMarkdown(&b, r))
```

And expects:
```go
assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
```

If `RenderMarkdown` calls `writeFindingsList` (or similar), then the warning would appear. The diff adds the warning in `writeFindingsList`. So this should work.

### Issue 9: The `Rel` check for path traversal

```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

The `rel == ".."` case: when would this happen? If `joined` is the parent directory of `root`. For example, if `root = "a/b"` and `joined = "a"`, then `Rel("a/b", "a")` = `".."`. But `filepath.Join` would never produce a path that is the parent of `root` from a normal `f.File` value. This case is theoretically unreachable but doesn't hurt.

Wait, actually, if `f.File` is something like `..`, then `filepath.Join(root, "..")` = `filepath.Dir(root)`. And `filepath.Rel(root, filepath.Dir(root))` = `".."`. This would be caught by the `rel == ".."` check. Good.

But what if `f.File` is empty? The check `strings.TrimSpace(f.File) == ""` returns early, so this is handled.

### Issue 10: The `Finding` struct has both `PathValid` and `PathWarning`

The comment in the diff explicitly addresses this:
> CONTRACT: path_warning is the authoritative signal — a non-empty value means the path did not resolve under the reviewed repo root. path_valid is auxiliary and MUST NOT be read in isolation: under omitempty, "validated but missing" and "never validated" both serialize as an absent path_valid (false), so the two states are indistinguishable from path_valid alone.

This is a well-documented design decision. The `PathValid` field is essentially redundant given the `omitempty` JSON tag, but it's kept for in-memory use. The `PathWarning` is the authoritative signal.

### Issue 11: The `PathValid` field is never read

Wait, is `PathValid` ever read? Let me check the diff... In `internal/reconcile/emit.go`, it's set in `JSONFindings`. In `internal/report/render.go`, only `f.PathWarning` is checked in `writePathWarning`. So `PathValid` is set but never read for display purposes. It's only used in tests.

This is not a bug per se - the field is there for potential future use and for in-memory state. The comment explains the design. But it could be considered a minor design issue (dead state).

### Issue 12: The `TestValidateFindingPaths_SkipsWhenRootEmpty` test

```go
func TestValidateFindingPaths_SkipsWhenRootEmpty(t *testing.T) {
	findings := []Merged{{Finding: stream.Finding{File: "does/not/exist.go"}}}
	validateFindingPaths(findings, "")

	assert.False(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
}
```

This test verifies that when `root` is empty, the finding is left untouched. The `PathValid` is `false` (zero value) and `PathWarning` is `""` (zero value). This is correct behavior per the design.

### Issue 13: The `TestRenderMarkdown_NoWarningWhenValid` test

```go
func TestRenderMarkdown_NoWarningWhenValid(t *testing.T) {
	r := Result{Findings: []Merged{
		{Finding: stream.Finding{
			Severity: "HIGH", File: "a.go", Line: 1,
			Problem: "x", Confidence: "MEDIUM", PathValid: true,
		}},
	}}

	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, r))
	assert.NotContains(t, b.String(), "File not found")
}
```

This test sets `PathValid: true` but doesn't set `PathWarning` (so it's empty). The `writeFindingsList` function only adds the warning when `m.PathWarning != ""`, so no warning is added. Good.

### Issue 14: Potential issue with `filepath.Rel` on Windows

On Windows, `filepath.Separator` is `\`. The check `strings.HasPrefix(rel, ".."+string(filepath.Separator))` handles this correctly. But `filepath.Rel` on Windows can return paths with mixed separators. However, since we're using `filepath.Separator`, this should be fine.

### Issue 15: The `writeFindingsList` function in `internal/reconcile/emit.go` - is it called for refuted findings?

Looking at the diff, `writeFindingsList` is called for the list of reconciled findings. The refuted findings are handled separately in `internal/report/render.go` via `writeRefutedSection`. But wait, does the reconcile pipeline produce a `report.md` that includes refuted findings? Let me check...

Actually, I don't have the full context of the codebase. The test reads `filepath.Join(reviewDir, "reconciled", ReportMD)`, which suggests the reconcile pipeline produces a `report.md` in the `reconciled` directory. The report package's `Render` function is for the `atcr report` command, which may produce a different output.

If the reconcile pipeline's `report.md` doesn't include refuted findings, then the `writeRefutedSection` in the report package is for a different output. The warning is added in both places, which is correct.

### Issue 16: The `Merged` struct - does it have `PathValid` and `PathWarning`?

The diff shows that `Merged` embeds `stream.Finding` (based on `findings[i].Finding` in the validate function). So `Merged` inherits the `PathValid` and `PathWarning` fields. The `JSONFinding` struct also has these fields. The propagation in `JSONFindings()` is correct.

Wait, let me re-check. The `validateFindingPaths` function does:
```go
for i := range findings {
    stream.ValidatePath(&findings[i].Finding, root)
}
```

This takes a pointer to the embedded `Finding` field. Since `Finding` is embedded in `Merged`, `&findings[i].Finding` gives a pointer to the embedded struct. The `ValidatePath` function modifies the `PathValid` and `PathWarning` fields of the embedded `Finding`, which are accessible through the `Merged` struct. This is correct.

### Issue 17: The `writeFindingsList` function - what about findings that are not in the "refuted" or "findings" sections?

Looking at the diff for `writeFindingsList`:
```go
func writeFindingsList(b *bytes.Buffer, findings []Merged) {
	...
	for _, m := range findings {
		// ... severity emoji, code span ...
		fmt.Fprintf(b, "- %s — confidence %s, reviewers: %s\n",
			codeSpan(m.File, m.Line), esc(m.Confidence), esc(joinOrNone(m.Reviewers)))
		if m.PathWarning != "" {
			fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
		}
		if m.Disagreement != "" {
			fmt.Fprintf(b, "  - Severity disagreement: %s\n", esc(m.Disagreement))
		}
	}
}
```

The order is: finding line, path warning, disagreement. This seems fine.

### Issue 18: The `writePathWarning` in `internal/report/render.go` is called in three places

1. `renderMarkdown` - in the main findings list
2. `renderChecklist` - in the checklist
3. `writeRefutedSection` - in the refuted section

All three use the same `writePathWarning` helper. Good.

### Issue 19: The `TestRender_Markdown_RefutedShowsPathWarning` test

This test sets up a finding with a hallucinated path and a refuted verdict. It expects the warning to appear in the "Refuted Findings" section. The `writeRefutedSection` function calls `writePathWarning`, so this should work.

### Issue 20: The `writePathWarning` function in `internal/report/render.go` uses `esc(f.File)`

```go
func writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
	if f.PathWarning != "" {
		fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(f.File))
	}
}
```

The `esc` function HTML-escapes the file path. This is correct for markdown rendering (to prevent injection). But wait, in `internal/reconcile/emit.go`, the `writeFindingsList` also uses `esc(m.File)`:
```go
fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
```

Good, both are escaped.

### Issue 21: The `codeSpan` function in the finding line

The finding line uses `codeSpan(m.File, m.Line)` which likely wraps the file path in backticks or similar. The path warning uses `esc(m.File)` which escapes HTML. These are different formatting, but both are safe.

### Issue 22: The `Finding` struct in `internal/stream/parser.go` - is the `PathValid` field's zero value misleading?

The comment addresses this:
> PathValid and PathWarning record the result of file-existence validation (Epic 5.0). They are NOT part of the wire format — no findings.txt column carries them — and stay at their zero values until ValidatePath stamps a finding. PathWarning is the authoritative display signal: an empty PathWarning means "no warning" regardless of PathValid, so an unvalidated finding (PathValid defaults to false) is never falsely flagged.

This is well-documented. The zero value of `PathValid` is `false`, but an empty `PathWarning` means "no warning." This is the correct design.

### Issue 23: The `writeFindingsList` function - is it the right place for the warning?

The `writeFindingsList` is in `internal/reconcile/emit.go`. This function is called when emitting the reconciled findings to the `reconciled/report.md` file. The warning is added here, which is correct.

But wait, does the reconcile pipeline also call the report package's `Render` function? If so, the warning would be added twice. But looking at the test, the `reconciled/report.md` is read from the file system, not from the report package's `Render` output. So they are separate.

### Issue 24: The `writeRefutedSection` in `internal/report/render.go` - is refuted findings output included in the reconcile pipeline's `report.md`?

Looking at the test `TestRunReconcile_FlagsHallucinatedPathEndToEnd`, it checks that the warning appears in `reconciled/report.md`. But it doesn't check for refuted findings. If the reconcile pipeline's `report.md` includes refuted findings (via `writeRefutedSection` from the report package), then the warning would be added there too. But the diff doesn't show this.

Actually, looking more carefully, the `writeFindingsList` in `internal/reconcile/emit.go` is for the reconcile pipeline's output. The `writeRefutedSection` in `internal/report/render.go` is for the `atcr report` command's output. These are separate code paths.

### Issue 25: The `writePathWarning` function uses `esc` for the file path

In `internal/report/render.go`:
```go
fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(f.File))
```

In `internal/reconcile/emit.go`:
```go
fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
```

Both use `esc`. Good.

### Issue 26: The `Rel` check in `ValidatePath` - what if `root` and `joined` are both absolute?

`filepath.Rel` works with both absolute and relative paths. If both are absolute, it returns a relative path from root to joined. If both are relative, it also returns a relative path. The check `rel == ".."` or `strings.HasPrefix(rel, ".."+string(filepath.Separator))` works in both cases.

### Issue 27: The `TestValidatePath_AbsolutePathNeutralized` test

```go
func TestValidatePath_AbsolutePathNeutralized(t *testing.T) {
	root := t.TempDir()
	f := Finding{File: "/etc/hosts"}
	ValidatePath(&f, root)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}
```

Let's trace:
- `root = /tmp/xxx`
- `f.File = "/etc/hosts"`
- `joined = filepath.Join("/tmp/xxx", "/etc/hosts")` = `/tmp/xxx/etc/hosts`
- `rel = filepath.Rel("/tmp/xxx", "/tmp/xxx/etc/hosts")` = `"etc/hosts"`
- `rel` doesn't equal `".."` and doesn't start with `"../"`, so the escape check passes.
- `os.Stat("/tmp/xxx/etc/hosts")` returns `os.IsNotExist` true.
- So `f.PathValid = false`, `f.PathWarning = PathNotFoundWarning`.

The test passes. But is this the right behavior? An absolute path in a finding is almost certainly a bug. It would be better to flag it as invalid directly (e.g., "absolute paths are not allowed"). But the current behavior is acceptable - it just won't find the file.

### Issue 28: The `writeFindingsList` function - what about the `codeSpan` for the file path in the warning?

The finding line uses `codeSpan(m.File, m.Line)` which wraps the file:line in backticks or similar. The path warning uses `esc(m.File)` which just escapes HTML. This means the file path in the warning is not wrapped in a code span. This is a minor formatting inconsistency but not a bug.

### Issue 29: The `writePathWarning` in `internal/report/render.go` - is it called in the right places?

Looking at the diff:
1. `renderMarkdown` (line 142) - after the finding line, before disagreement
2. `renderChecklist` (line 186) - after the finding line
3. `writeRefutedSection` (line 353) - after the finding line

All three places call `writePathWarning`. Good.

### Issue 30: The `writeFindingsList` in `internal/reconcile/emit.go` - is the warning in the right place?

```go
fmt.Fprintf(b, "- %s — confidence %s, reviewers: %s\n",
    codeSpan(m.File, m.Line), esc(m.Confidence), esc(joinOrNone(m.Reviewers)))
if m.PathWarning != "" {
    fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
}
if m.Disagreement != "" {
    fmt.Fprintf(b, "  - Severity disagreement: %s\n", esc(m.Disagreement))
}
```

The warning is added after the finding line and before the disagreement. This seems fine.

### Issue 31: The `Finding` struct in `internal/stream/parser.go` - are the new fields after the last existing field?

The diff shows:
```go
Reviewer   string   // per-source 8th column
Reviewers  []string // reconciled 8th column
Confidence string   // reconciled 9th column

// PathValid and PathWarning record the result of file-existence validation
// (Epic 5.0). ...
PathValid   bool   // true once validated and the file exists
PathWarning string // e.g. "file not found"; empty when valid or unvalidated
```

The new fields are at the end of the struct, with a blank line separator. This is fine.

### Issue 32: The `writeFindingsList` function - is it called for the refuted findings too?

Looking at the diff, `writeFindingsList` is called for the main findings list. The refuted findings are handled in `writeRefutedSection` in the report package. But does the reconcile pipeline also have a refuted findings section? I don't have the full context.

### Issue 33: The `validateFindingPaths` function - is it called at the right time?

The function is called after `Reconcile` and before `Emit`:
```go
res := Reconcile(sources, opts)

// Flag findings whose file paths do not exist under the reviewed repo root
// (Epic 5.0). Stamped after merge, before Emit, so the warning rides into
// findings.json and the reports. No-op when opts.Root is empty.
validateFindingPaths(res.Findings, opts.Root)
```

This is the correct timing - after merge (so `Merged` objects are created) and before emit (so the warning is included in the output).

### Issue 34: The `writeFindingsList` function - what about the `codeSpan` for the file path in the warning line?

The finding line uses `codeSpan(m.File, m.Line)` which wraps the file:line in backticks. The path warning uses `esc(m.File)` which just escapes HTML. This means the file path in the warning is not wrapped in a code span. This is a minor formatting inconsistency.

Wait, but the warning is a separate line. It might be intentional to not wrap it in a code span. Let me check the report package's `writePathWarning`:

```go
func writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
	if f.PathWarning != "" {
		fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(f.File))
	}
}
```

Same pattern - `esc(f.File)` without `codeSpan`. Consistent.

### Issue 35: The `TestValidateFindingPaths_StampsWhenRootSet` test

```go
func TestValidateFindingPaths_StampsWhenRootSet(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "exists.go"), []byte("package x\n"), 0o644))

	findings := []Merged{
		{Finding: stream.Finding{File: "exists.go"}},
		{Finding: stream.Finding{File: "missing.go"}},
	}
	validateFindingPaths(findings, root)

	assert.True(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
	assert.False(t, findings[1].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, findings[1].PathWarning)
}
```

This test verifies the basic functionality. The `exists.go` file is created in `root`, and the test checks that the first finding is valid and the second is invalid. Good.

### Issue 36: The `TestJSONFindings_CarriesPathValidation` test

This test verifies that `PathValid` and `PathWarning` are carried into the JSON output. Good.

### Issue 37: The `TestRunReconcile_FlagsHallucinatedPathEndToEnd` test

This is the AC6 acceptance test. It sets up a real review directory with a hallucinated path, runs the full reconcile pipeline, and checks that:
1. The real path finding is valid
2. The hallucinated path finding is invalid with the warning
3. `findings.json` carries the warning
4. `report.md` shows the warning and preserves the finding

This is a comprehensive end-to-end test. Good.

### Issue 38: The `writePathWarning` function - what about the `⚠️` emoji?

The warning uses the `⚠️` emoji (warning sign + variation selector). This is a standard emoji for warnings. It's used consistently in both `internal/reconcile/emit.go` and `internal/report/render.go`. Good.

### Issue 39: The `TestRender_Markdown_NoWarningWhenValid` test in `internal/report/validate_test.go`

```go
func TestRender_Markdown_NoWarningWhenValid(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", PathValid: true,
	}}

	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatMarkdown))

	assert.NotContains(t, b.String(), "File not found")
}
```

This test verifies that a valid path doesn't add a warning. But it only checks for "File not found" - what if the finding text or other content contains "File not found"? Unlikely, but the assertion is specific enough.

### Issue 40: The `writeFindingsList` function in `internal/reconcile/emit.go` - is it called from the right places?

I don't have the full context, but the diff doesn't change the call sites of `writeFindingsList`. The function is called for the main findings list in the reconcile pipeline's output.

### Summary of findings:

1. The `PathValid` field is set but never read for display purposes (only `PathWarning` is checked). This is a minor design issue, but the comment explicitly addresses it.

2. The `TestValidatePath_EmptyRootDefaultsToCwd` test is fragile - it relies on `parser.go` existing in the CWD. This could break if the test is run from a different directory or if the file is renamed.

3. The absolute path handling in `ValidatePath` is not ideal - an absolute path is re-rooted under the validation root rather than being flagged as invalid directly. This is a minor issue.

4. The `writePathWarning` function doesn't wrap the file path in a code span (unlike the finding line). This is a minor formatting inconsistency.

5. The `Rel` check in `ValidatePath` handles the `rel == ".."` case, but this is theoretically unreachable with normal paths from atcr.

Let me also check for any issues I might have missed:

### Issue: The `Merged` struct - does it embed `stream.Finding` or have it as a named field?

The diff shows `findings[i].Finding` being used, which suggests `Finding` is an embedded field (since you can access embedded fields directly with the type name). The `stream.ValidatePath(&findings[i].Finding, root)` takes a pointer to the embedded `Finding`. This is correct.

### Issue: The `writeFindingsList` function - what about findings with `m.File == ""`?

If `m.File` is empty, the `codeSpan` and `esc` functions would receive an empty string. The `writePathWarning` would not be called (since `m.PathWarning` would be empty for an unvalidated finding). The finding would still be rendered with an empty file path. This is acceptable behavior.

### Issue: The `writeFindingsList` function - what about the `codeSpan` for an invalid path?

The finding line uses `codeSpan(m.File, m.Line)` which wraps the file:line in backticks. If the path is invalid, it would be wrapped in backticks. The warning line also shows the path (without backticks). This is consistent with the design.

### Issue: The `writePathWarning` function in `internal/report/render.go` - is it called for refuted findings only?

The function is called in `writeRefutedSection` for refuted findings, and in `renderMarkdown`/`renderChecklist` for the main findings. The refuted findings are a subset of the main findings (they have a refuted verdict). The warning is added in both places, which is correct.

### Issue: The `validateFindingPaths` function - does it handle nil findings?

The function iterates over `findings []Merged` and calls `stream.ValidatePath(&findings[i].Finding, root)`. If `findings` is nil, the loop doesn't execute. If `findings[i].Finding` is a zero-value `stream.Finding`, the `ValidatePath` function handles it (empty `f.File` returns early). Good.

### Issue: The `writePathWarning` function in `internal/report/render.go` - what about the `f.PathWarning` check?

```go
func writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
	if f.PathWarning != "" {
		fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(f.File))
	}
}
```

This checks `f.PathWarning != ""`. The `PathWarning` is the authoritative signal. If it's empty, no warning is added. This is correct per the design.

### Issue: The `writeFindingsList` function in `internal/reconcile/emit.go` - what about the `m.PathWarning` check?

```go
if m.PathWarning != "" {
    fmt.Fprintf(b, "  - ⚠️ File not found: %s\n", esc(m.File))
}
```

Same pattern - checks `m.PathWarning != ""`. Good.

### Issue: The `TestRender_Markdown_RefutedShowsPathWarning` test

This test sets up a finding with a hallucinated path and a refuted verdict. It expects the warning to appear in the "Refuted Findings" section. The `writeRefutedSection` function calls `writePathWarning`, so this should work.

But wait, the test does:
```go
require.Contains(t, out, "Refuted Findings")
assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
```

It first checks that the "Refuted Findings" section exists, then checks that the warning appears. This is a good test.

### Issue: The `TestRunReconcile_FlagsHallucinatedPathEndToEnd` test

This test reads `reportMD` from `filepath.Join(reviewDir, "reconciled", ReportMD)`. The `ReportMD` constant is likely defined elsewhere. The test checks that the warning appears in the `report.md` file. This is the AC6 acceptance test.

But wait, the test only checks for the warning in the main findings list, not in the refuted section. The hallucinated finding is not refuted, so it should appear in the main list. The warning is added in `writeFindingsList`, so it should appear. Good.

### Issue: The `writeFindingsList` function in `internal/reconcile/emit.go` - what about the `codeSpan` for the file path in the warning line?

Actually, I just realized that the warning line uses `esc(m.File)` which escapes HTML. The `codeSpan` function likely wraps the file:line in backticks. The warning line is a separate line, so it makes sense to not wrap it in backticks. This is consistent with the report package's `writePathWarning`.

### Final assessment:

The diff is well-designed and well-tested. The main issues I've identified are:

1. The `TestValidatePath_EmptyRootDefaultsToCwd` test is fragile (relies on CWD).
2. The `PathValid` field is set but never read for display (minor design issue, explicitly documented).
3. The absolute path handling is not ideal (re-rooted rather than flagged directly).
4. The `writePathWarning` function doesn't wrap the file path in a code span (minor formatting).

These are all minor issues. The core functionality is correct and well-tested.

Let me also check if there are any issues with the `writeFindingsList` function being called for refuted findings. Looking at the diff again, the function is in `internal/reconcile/emit.go` and is called for the main findings list. The refuted findings are handled in `internal/report/render.go`. But does the reconcile pipeline also produce a `report.md` that includes refuted findings? I don't have the full context, but the test `TestRunReconcile_FlagsHallucinatedPathEndToEnd` checks the `reconciled/report.md` file and expects the warning to appear. The test passes, so the function is working correctly.

Wait, I should check if the `writeFindingsList` function in `internal/reconcile/emit.go` is the one that produces the `reconciled/report.md` file. The test reads from `filepath.Join(reviewDir,
</think>
