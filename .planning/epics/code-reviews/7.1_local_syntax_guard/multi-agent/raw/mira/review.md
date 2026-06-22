<think>
Let me analyze the diff carefully against the sprint plan scope.

The sprint plan is for Epic 7.1: Local Syntax/Compile Guard for Fixes. The in-scope files are:
- `internal/verify/executor.go` (syntax guard integration)
- `internal/verify/syntaxguard.go` (new file)
- `internal/verify/syntaxguard_test.go` (new file)
- `internal/verify/executor_test.go` (tests)
- `internal/reconcile/emit.go` (FixWarning field doc update)
- `internal/report/render.go` (showing FixWarning in markdown)
- `internal/report/render_test.go` (tests)

All these are in scope. Let me look for issues:

1. **executor.go line ~138-147**: The new logic calls `validateGoFixSyntax(fix)` and sets `FixWarning`. But the comment says "prose change-instructions and valid code clear any warning a prior failed/empty/invalid run left". Looking at the code, the condition is: if syntax error -> set warning; else -> clear warning. This seems correct.

   However, there's a subtle issue: `f.Fix = fix` is set BEFORE the syntax guard. The comment in the test says "the attempted fix stays visible". This is correct per the sprint plan clarification.

2. **executor.go**: The `logPipelineWarning` call - need to check if this function exists and if `log.FromContext(ctx)` returns a logger. Since this isn't shown in the diff, I can't verify, but let me check if there might be an issue. Actually, this is consistent with the pattern used elsewhere in the file (line 128, 134 mentioned in the plan). So probably fine.

3. **syntaxguard.go**: Let me look at the regexes and logic:
   - `fenceRe`: `(?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?```` - This uses `(?s)` which is not a standard Go regex flag. Go's regexp package uses `(?s)` for dot-all mode actually... let me check. Yes, Go regexp does support `(?s)` for single-line mode (dot matches newline). OK.
   - `declKeywordRe`: matches `package|import|func|type|var|const` at line start. Good.
   - `blockOpenRe`: `(?m)\{[ \t]*$` - matches a line ending with `{` and optional whitespace. Good.
   - `blockCloseRe`: `(?m)^[ \t]*\}` - matches a line starting with `}`. Good.
   - `packageClauseRe`: matches `package xxx`. Good.

4. **syntaxguard.go potential issues**:
   - In `parseGoFix`, the strategies used: full file, prefixed with package, wrapped in function. The `mode = parser.SkipObjectResolution` is used, which is good for syntax-only check.
   - When the file has a `package` clause, `parser.ParseFile` might not be the best choice for the synthetic package wrap... actually it doesn't matter since we try all three.
   - Edge case: What if the code has a `package` clause but is broken? The `fileErr` will be non-nil, `declErr` will be the same or different. The code returns `fileErr` for package clause case. Good.
   - But wait - if the source has a `package` clause, then wrapping it in `package p\n` + src will create TWO package clauses, which is itself a syntax error. So `declErr` will definitely be non-nil in that case. That's fine because we check `fileErr` first.

5. **executor.go line 144**: `fmt.Sprintf("%s:%d: %v", f.File, f.Line, synErr)` - if `f.File` is empty, this still produces a valid string like ":42: error". Minor.

6. **Potential issue with `f.Fix = fix` before validation**: If the executor returns something that later fails syntax check, the Fix is set, FixWarning is set. This is per spec.

7. **Report rendering**: `escTrunc(f.FixWarning)` - the fix warning is HTML-escaped. The comment in the test says "The message text is HTML-escaped by the renderer; assert the unescaped portion." This is fine.

8. **Test file - `TestRenderMarkdown_ShowsFixWarning`**: Tests check the output contains "Fix warning:" and the message. The test uses an emoji "⚠️". This should be fine.

9. **Performance**: `parser.ParseFile` is called up to 3 times per fix. For a large batch of findings, this could add up. But Go's parser is fast and this is reasonable for an MVP guard.

10. **Correctness of the heuristic**: The comment in `looksLikeGoCode` says "an inline brace or `:=` embedded mid-sentence" is NOT treated as code. The test `TestValidateGoFixSyntax_ProseWithInlineBracesNotFlagged` tests this. Good.

11. **Looking at `TestValidateGoFixSyntax_InvalidShortAssign`**: `x := func( {` - this has `:=` which is NOT in `looksLikeGoCode`'s criteria. So why does this test expect an error? Let me re-read... 
    - `x := func( {` has a `:=` (short variable declaration) - wait, `looksLikeGoCode` doesn't check for `:=`. 
    - The content `x := func( {` has a `{` at the end of the line! So `blockOpenRe` matches.
    - Actually, let me re-read: `x := func( {` - the `{` is at the end, so `blockOpenRe` (`\{[ \t]*$`) matches. Yes, it has block structure, so it would be flagged. Good.

12. **Potential issue with `blockOpenRe`**: The regex `\{[ \t]*$` - this would also match inside a string literal or comment. But for the purpose of the heuristic (prose vs code), this is acceptable.

13. **Test `TestRenderMarkdown_ShowsFixWarning` line 38-39**: The assertion checks that the message text is HTML-escaped. But actually, the renderer uses `escTrunc` which escapes. So the actual output would have something like `invalid_syntax: 2:1: expected '&#39;}`. The test asserts on "invalid_syntax: 2:1: expected" without `'`, which should work since HTML escaping doesn't affect those characters.

14. **render.go line 164-167**: The new code adds "⚠️ Fix warning: %s\n" to the markdown. This is in scope per the sprint plan.

15. **emit.go doc change**: This is just a doc comment update, in scope.

Now, let me look for actual issues that should be reported:

**Issue 1: `looksLikeGoCode` heuristic for `:=` is inconsistent with the test**
- The test `TestValidateGoFixSyntax_InvalidShortAssign` passes `"x := func( {"` which has `:=`. The comment in `looksLikeGoCode` says `:=` is NOT treated as code signal, but this test expects an error. Looking at the content again: `"x := func( {"` - the `{` is at end of line, so `blockOpenRe` matches. So it's the `{` at end-of-line that triggers the flag, not the `:=`. OK, this is fine.

**Issue 2: `parseGoFix` may return `declErr` for content with `package` clause**
- If `src` has a `package` clause, the second strategy (`"package p\n" + src`) will have TWO package clauses, which is itself a syntax error. So `declErr` will definitely be non-nil. The code still works because it checks `fileErr` first. The `switch` correctly returns `fileErr` for package-clause content. OK.

**Issue 3: `blockOpenRe` could match inside strings/comments**
- A prose line ending with `{` (in a string) would be falsely flagged. But this is rare. The heuristic is conservative. LOW severity.

**Issue 4: TestRenderMarkdown_ShowsFixWarning test assertion**
- The fix warning contains a single quote `'`. With HTML escaping, `'` becomes `&#39;`. The test asserts `"invalid_syntax: 2:1: expected"` which is before the `'`. OK, fine.

**Issue 5: `executor.go` - logPipelineWarning error message format**
- `fmt.Sprintf("%s:%d: %v", f.File, f.Line, synErr)` - if `f.File` is empty (shouldn't happen but theoretically), this is ":<line>: error". Minor cosmetic.

**Issue 6: Potential race condition in `generateFixes`**
- The original code uses `wg` and goroutines. The new code modifies `f.Fix` and `f.FixWarning` in the goroutine, so this is per-finding, no race. OK.

**Issue 7: Performance - regex compilation**
- All regexes are package-level variables, so they're compiled once. Good.

**Issue 8: `parser.SkipObjectResolution`**
- This skips object resolution, making parsing faster and avoiding issues with unresolved identifiers. Good for syntax-only check.

**Issue 9: `extractFencedCode` only finds the first fence**
- This is documented behavior. If a fix has multiple fences (unusual), only the first is checked. Minor.

**Issue 10: The `declKeywordRe` doesn't include `if`, `for`, `switch`, etc.**
- These are statement keywords, not declaration keywords. A statement fragment like `if err != nil {` would be caught by `blockOpenRe` (ends in `{`). So OK.

**Issue 11: test for `TestValidateGoFixSyntax_ValidStatementSnippet`**
- `src := "if err != nil {\n\treturn err\n}"` - the first line ends in `{` so `blockOpenRe` matches. Wait, but the test expects NO error. The content IS valid Go. `parser.ParseFile` with `"package p\nfunc _() {\n" + src + "\n}\n"` would parse it as a function body. So `stmtErr` is nil, returns nil. Good.

Wait, but `parser.ParseFile` requires a package clause at the top level. So `"package p\nfunc _() {\n" + src + "\n}\n"` - this is a valid Go file: `package p`, then a function declaration. The body contains `if err != nil { return err }`. That's valid. OK.

**Issue 12: When file has a `package` clause and broken syntax, what error is returned?**
- The `fileErr` from `parser.ParseFile` will be non-nil. The synthetic package wrap will have two package clauses and also fail. The synthetic function wrap will have a function with package + broken code, which would also fail. All three errors are non-nil. The code returns `fileErr` because `packageClauseRe` matches. Good.

**Issue 13: `fenceRe` issue - the `(?s)` flag**
- Go regex supports `(?s)` for single-line mode (dot matches newline). The regex `(?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?```` - the `.*?` is non-greedy, so it matches up to the first `\n?``` `. But with `(?s)`, `.` matches newlines. So `(.*?)` matches across newlines until the first occurrence of `\n?``` `. Good.

But wait, there's a subtle issue: `fenceRe = regexp.MustCompile("(?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?```")`. The opening ``` is followed by a language tag (possibly empty), then `\n`, then the body, then optional `\n`, then closing ```. What if the language tag is empty? Like ```` ```\n...``` ````? The regex would match: opening ```, empty language tag, `\n`, body, optional `\n`, closing ```. OK.

What if the language tag has a space? Like ```` ``` go\n...``` ````? The regex `[A-Za-z0-9_+-]*` would match empty (since the space is not in the class), so it would match ```` ``` ```` as opening with empty tag, then expect `\n` but find ` `. The match would fail. Actually, no - the regex would try other positions. Hmm, this could be a minor edge case. But most LLM output uses ` ```go ` not ` ``` go `. LOW severity.

**Issue 14: The fix could be applied to a file but the guard is on the raw fix string**
- The sprint plan says: "Apply the generated fix to an in-memory copy of the file. Run a fast language-specific syntax check." But the implementation only checks the fix string itself, not the resulting file after applying the fix. The sprint plan also says: "IN scope: Go syntax validation (via stdlib go/parser) of generated fixes inside generateFixes, run immediately after the fix string is produced and before f.Fix is committed". So the implementation matches the actual in-scope definition. The "apply to in-memory copy" was an AC item that was de-scoped per the clarifications.

**Issue 15: `f.Evidence = appendFixAttribution(f.Evidence, ex.Name)` runs even for invalid syntax**
- This is correct per the test `TestGenerateFixes_FlagsInvalidSyntax` which asserts `Evidence` contains "fix by opus" even for invalid syntax. Good.

**Issue 16: In `parseGoFix`, when the content has `package` but is broken, the returned `fileErr` is the file-shaped error. Good.**

**Issue 17: A potential issue with `executor.go` line 144** - The `logPipelineWarning` call uses `f.File` and `f.Line`. If these are zero/empty, the log message is ":<line>: error" or "::<error>". Minor.

**Issue 18: The `FixWarning` field is set to `""` when valid, but the doc says "omitempty".**
- Setting to `""` is consistent with `omitempty`. Good.

**Issue 19: `escTrunc` in render.go - does it exist?**
- It's used at lines 161 (`escTrunc(f.Fix)`), so it exists. Good.

**Issue 20: A subtle issue with the regex `fenceRe`**
- The regex uses `(?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?``` `. The `\n?` is a single optional newline. What if the closing fence has a blank line before it? Like:
  ```
  ```go
  code
  
  ```
  ```
- The regex `(.*?)\n?``` ` would match the code, then `\n?` matches the newline, then ` ``` `. But there's an empty line between. Let me trace: `(.*?)` is non-greedy, so it tries to match the shortest. It would match "code", then need `\n?``` ` - next is `\n\n``` `. `\n?` matches `\n`, then need ` ``` ` but we have `\n``` `. So the regex backtracks. `.*?` matches "code\n", then need `\n?``` ` - next is `\n``` `. `\n?` matches `\n`, then need ` ``` ` and we have ` ``` `. Yes! So it matches with body "code\n" and then the closing. But what about the trailing empty line? It's outside the match. The test `TestValidateGoFixSyntax_ClosingFenceSameLineValid` handles the case where there's no newline before closing ```. Good.

But wait, what about a fix that ends with a blank line after the code fence? Like:
```
```go
code
```

more text after
```
- The regex `(.*?)\n?``` ` - the `(.*?)` is non-greedy. It matches "code", then `\n?` matches `\n`, then need ` ``` `. Next is `\nmore...`. Doesn't match. Backtrack: `.*?` matches "code\n", then `\n?` matches `\n`, need ` ``` `, have `\nmore...`. Doesn't match. Continue: `.*?` matches "code\n\n", need ` ``` `, have `more text`. Doesn't match. So this fix would not be recognized as fenced! But actually, the closing ``` is in the middle, and after it there's "more text". The regex would fail to find the closing ```. Hmm, but actually the regex is non-greedy `(.*?)` and looks for the first `\n?``` `. Let me retrace:
  - Position after opening: "code\n\n```\n\nmore text after"
  - `.*?` tries empty: need `\n?``` `, have "code\n\n```\n..." - `\n?` matches empty, need ` ``` `, have `code`. No.
  - `.*?` tries "c": need `\n?``` `, have "ode\n\n```\n..." - no.
  - ... eventually `.*?` tries "code\n": need `\n?``` `, have "\n```\n..." - `\n?` matches `\n`, need ` ``` `, have ` ``` ` - YES! Match found with body "code\n".

OK so the regex finds the closing fence even with blank lines. Good.

Actually wait, I made an error. Let me re-trace with the actual content:
`Here is the fix:\n\n\`\`\`go\ncode\n\n\`\`\`\n\nmore text after\n`
- After matching opening ` ```go\n `, the remaining is: `code\n\n\`\`\`\n\nmore text after\n`
- `.*?` starts empty, needs `\n?``` `.
- It will try `.*?` = "code": needs `\n?``` `, have `\n\n\`\`\`\n\nmore...`. `\n?` matches `\n`, then needs ` ``` `, have `\n\`\`\`...`. No.
- `.*?` = "code\n": needs `\n?``` `, have `\n\`\`\`\n\nmore...`. `\n?` matches `\n`, needs ` ``` `, have ` ``` `. YES!
- So match body = "code\n", which is valid Go.

Good. The regex works.

**Issue 21: Potential issue with `declKeywordRe` matching prose like "Package the binary"**
- The regex `(?m)^\s*(package|import|func|type|var|const)\b` would match a line starting with "package" (case-sensitive). The sprint plan says Go is case-sensitive. But "Package the binary for distribution" starts with "Package" (capital P), so it wouldn't match (regex is case-sensitive). Good.

But what about "import" used as a verb? "Import the data into the table" - this would match! The line starts with "import", and `\b` is a word boundary. This would be a false positive for the heuristic. The fix would be flagged as code and parsed, but "import the data into the table" would fail to parse as Go. So it would be incorrectly flagged as invalid syntax.

Wait, but the test `TestValidateGoFixSyntax_ProseInstructionNotFlagged` uses "Add a nil check before calling Connect() so a closed pool does not panic." - this doesn't start with a keyword. The test `TestValidateGoFixSyntax_ProseWithKeywordsNotFlagged` uses "If the input is nil, return an error..." - this starts with "If" which is not in the regex. So the tests don't cover this case.

Actually, this is a real potential false positive. "Import the data from the CSV file" would start with "import" and match `declKeywordRe`, then fail to parse as Go, and be incorrectly flagged.

Hmm, but is this realistic? An LLM generating a "precise change instruction" would be unlikely to start a sentence with "import" in this way. The more common case is "import" as a Go keyword. So this is a low-severity edge case.

Similarly: "Type the result into the field" - starts with "type", would match.
"Function foo should be renamed" - starts with "function" (lowercase 'f' but "func" not "function"), wouldn't match. "Func foo should..." - would match but is unusual.
"Variable names should follow conventions" - starts with "var", would match! "Var names..." - would match.
"Constant values should be defined" - starts with "const", would match!

So there are potential false positives. But these are uncommon in practice. LOW severity.

**Issue 22: `blockOpenRe` false positive**
- A prose line ending with `{` is unusual but possible. E.g., "The output is a JSON object {". LOW severity.

**Issue 23: The `executor.go` change - `f.Fix = fix` is unconditional**
- If the fix is empty (which the existing code checks for earlier in the function), the Fix is set to empty. Then `validateGoFixSyntax("")` returns nil, and FixWarning is cleared. This is correct.

Let me check the earlier code path: there's likely a check for empty fix before this point. Looking at the diff, the original code had `f.Fix = fix` followed by `f.FixWarning = ""`. The new code replaces the warning clear with the syntax guard. So the fix assignment is still unconditional. If fix is empty, the syntax guard returns nil, and FixWarning is set to "". That's the same behavior as before. Good.

**Issue 24: Missing test for empty fix path**
- The new code path for empty fix (FixWarning cleared) isn't explicitly tested in the new tests. But it's covered by the existing flow. Minor.

**Issue 25: `TestGenerateFixes_ClearsStaleInvalidSyntaxOnValidFix`**
- This test starts with a finding that has a stale `FixWarning: "invalid_syntax: 2:11: expected '}'"`. The new code clears it when the fix is valid. Good test.

**Issue 26: The `parser.ParseFile` call with `parser.SkipObjectResolution`**
- This is the correct mode for syntax-only validation. Good.

**Issue 27: The `fenceRe` doesn't handle nested fences**
- Like ```` ```go\n```python\n...```\n``` ````. This would be confusing. The outer fence would be detected. LOW severity.

**Issue 28: The `extractFencedCode` uses `FindStringSubmatch` which returns the first match. If the fix has prose before and after a fence, the prose after is ignored. The fix could be partially validated. But this is a reasonable simplification.**

**Issue 29: `TestRenderMarkdown_ShowsFixWarning` - the fix string `"func x() {"` itself is syntactically invalid Go. But since the test is for the renderer, not the syntax guard, the FixWarning is set directly. Good.**

**Issue 30: The `executor.go` log message**
- `fmt.Sprintf("%s:%d: %v", f.File, f.Line, synErr)` - if `f.Line` is 0, the message is "file.go:0: error". Minor.

**Issue 31: Potential nil pointer / type issue**
- The `validateGoFixSyntax` function returns an error. If `synErr` is nil, no log. If non-nil, log and set FixWarning. Good.

**Issue 32: The `logPipelineWarning` function signature**
- Not visible in the diff, but the call is `logPipelineWarning(log.FromContext(ctx), "executor_invalid_syntax", ...)`. This pattern is used elsewhere in the file (lines 128, 134). So it's the existing pattern. Good.

Now let me decide which issues to report. The sprint plan scope is:
- Go syntax validation
- In-memory copy NOT applied (per clarifications)
- Flag-only (no retry)
- Go-only

The issues I've identified are all within scope. Let me pick the most significant ones:

1. **Potential false positive in `looksLikeGoCode`**: Prose starting with `import`, `type`, `var`, `const` would be incorrectly flagged as code and then fail to parse, causing a false invalid_syntax flag. This is a correctness issue. MEDIUM severity.

2. **No test for `import`/`type`/`var`/`const` as prose**: The tests don't cover this edge case. But the issue is the heuristic itself. MEDIUM.

3. **`blockOpenRe` matches prose ending in `{`**: Edge case, LOW.

4. **`fenceRe` doesn't handle language tag with space**: LOW.

5. **`executor.go` log message format**: Cosmetic, LOW.

6. **The heuristic is conservative - unfenced broken fragments may not be flagged**: This is documented as intentional. Not an issue.

Let me also check if there are any issues in the test files or render.go:

7. **`render.go` line 166-168**: The fix warning is rendered after the fix. If both Fix and FixWarning are present, both are shown. Good.

8. **`render.go` - the emoji `⚠️` in the output**: This is fine, it's UTF-8.

9. **`emit.go` doc change**: Just a comment update, no functional change. Good.

10. **`TestRenderMarkdown_ShowsFixWarning` line 38**: The test creates a finding with `Fix: "func x() {"` and `FixWarning: "invalid_syntax: 2:1: expected '}'"`. It then asserts the output contains "Fix warning:" and "invalid_syntax: 2:1: expected". The `'` in the warning would be HTML-escaped to `&#39;`, but the test asserts on text before the `'`. Good.

Let me think about whether the false positive issue is significant enough to flag.

The heuristic: if prose starts with "import the data", it matches `declKeywordRe` ("import" at line start), then `looksLikeGoCode` returns true, then `parseGoFix` is called, and it fails (not valid Go), so the error is returned, and the fix is flagged as invalid_syntax.

This is a real false positive risk. But how likely is an LLM to produce a change instruction starting with "Import the data" or "Type the result" or "Var x should be..."? It's possible but uncommon. MEDIUM severity.

Actually, looking at the code more carefully:

```go
func looksLikeGoCode(s string) bool {
    return declKeywordRe.MatchString(s) || blockOpenRe.MatchString(s) || blockCloseRe.MatchString(s)
}
```

And `declKeywordRe = regexp.MustCompile(`(?m)^\s*(package|import|func|type|var|const)\b`)`

So any prose starting (after optional whitespace) with "package", "import", "func", "type", "var", or "const" would be treated as code. This is a known trade-off documented in the code comments, but it's still a correctness concern.

I'll flag this as MEDIUM.

Let me also consider: the `TestValidateGoFixSyntax_ProseWithKeywordsNotFlagged` test uses "If the input is nil, return an error..." - "If" and "return" are not in the decl keyword list, so this test doesn't cover the false positive case. "if" and "for" and "switch" and "return" are statement keywords, not declaration keywords. So prose starting with "if" wouldn't match `declKeywordRe`. Good.

But prose starting with "import" (as a verb) would. This is a real concern.

Also: what about a prose line that has a `}` at the start? Like "} End of fix". `blockCloseRe` matches `^[ \t]*\}`, so a line starting with `}` would match. But this is uncommon in prose. LOW.

What about a prose line ending with `{`? Like "The set is {1, 2, 3}". The `{` is mid-line, not at the end. `blockOpenRe` is `\{[ \t]*$` - the `{` must be at end of line (possibly followed by spaces/tabs). So "The set is {1, 2, 3}" wouldn't match because `{1, 2, 3}` is mid-line. But "Create a map { }" - if `{` is at the end of a line, it would match. Uncommon. LOW.

I think the main issue to flag is the false positive risk for prose starting with decl keywords. Let me write it up.

Actually, I want to double-check one more thing. The `parser.ParseFile` with `parser.SkipObjectResolution` - when the content is "import the data from the CSV file" wrapped as `"package p\nfunc _() {\nimport the data from the CSV file\n}\n"`, the `import` at the start of a line (inside a function) is invalid (import is a top-level declaration). So the parser would fail with a syntax error like "import must appear before other declarations" or similar. The error would be returned, and the fix would be flagged as invalid_syntax.

But wait, the first strategy `parser.ParseFile(fset, "", "import the data from the CSV file", mode)` - "import" is a keyword and the parser would expect specific syntax. The error would be something like "expected 'package', found 'import'" or "expected declaration, found 'import'". So `fileErr` is non-nil.

Second strategy: `"package p\nimport the data from the CSV file"` - this is a valid Go file structure (package, then import statement). But "import the data from the CSV file" is not a valid import declaration (import must be `"path"` or a parenthesized group). So `declErr` would be non-nil with a syntax error.

Third strategy: `"package p\nfunc _() {\nimport the data from the CSV file\n}\n"` - inside a function, "import" is not a valid statement. So `stmtErr` is non-nil.

All three fail. The content has "import" matching `declKeywordRe`, so the switch returns `declErr`. The fix is flagged as invalid_syntax.

This is a real false positive. The fix prose "import the data from the CSV file" is a legitimate English instruction, but it would be incorrectly flagged as invalid Go.

However, I should note: an LLM generating a fix for a Go codebase is unlikely to output a prose instruction starting with "import" in this way. The more common shape is "Add an import statement for the foo package" or similar. So the false positive is unlikely in practice.

Still, it's a correctness concern in the heuristic. I'll flag it as MEDIUM.

Let me also think about whether there are any other issues I missed:

- **Thread safety**: The `f.Fix` and `f.FixWarning` modifications are inside the goroutine, per-finding. The `wg.Wait()` ensures all complete before returning. No race. Good.

- **Error from `logPipelineWarning`**: Not handled, but the existing pattern (lines 128, 134) likely doesn't handle it either. Consistent.

- **`appendFixAttribution` called even for invalid syntax**: This is intentional per the test. Good.

- **The `package` keyword in the synthetic wrap**: When the content already has a `package` clause, wrapping with `"package p\n"` creates a duplicate. This is a syntax error, but the code handles it (declErr is non-nil, and the switch returns fileErr). Good.

- **`parser.SkipObjectResolution`**: This skips resolving identifiers, which is what we want for syntax-only check. Good.

- **The `mode` variable is `const`**: Good, it's a constant.

OK, I think I have enough. Let me write the findings.

Actually, let me reconsider the false positive issue. The heuristic is documented as conservative: "The cost is conservative recall: a one-line broken expression with no block structure is not flagged, since it is indistinguishable from prose." But the case of prose starting with "import" or "type" or "var" or "const" is distinguishable from code? No, it's not - the word "import" at the start of a line is ambiguous. The heuristic chose to treat it as code, which is a reasonable default but introduces this false positive.

Given the trade-off and the documentation, I think this is a MEDIUM issue worth flagging. The fix would be to make the heuristic more sophisticated, but that's out of scope for this review (the implementation is per the sprint plan).

Actually, I should focus on real issues that have concrete fixes within the scope. The false positive is a known trade-off, not really a bug. Let me reconsider.

Looking at the sprint plan: "Code-aware, conservative. Detect plausible Go source before running go/parser; never flag prose change-instructions as invalid."

The heuristic aims to never flag prose. But the current heuristic CAN flag prose (if it starts with a decl keyword). So this contradicts the stated goal. MEDIUM.

But the fix is non-trivial - making the heuristic more robust would require additional logic. Let me flag it and suggest a concrete fix.

Concrete fix: Add a check for the context around the keyword. E.g., if the line is "import the data" (followed by English words), it's more likely prose than code. But this is heuristic and error-prone.

Alternatively: Check if the keyword is followed by a Go-valid construct (e.g., `import "path"`, `var x int`, `const y = 1`). If the keyword is followed by non-Go syntax, it's more likely prose.

Or: Only treat `package` as a code signal (since `package xxx` is unambiguous), and require block structure (`{` at end of line) for other keywords. But this would miss valid decl fragments without braces.

This is a design decision, not a simple fix. I'll flag it as MEDIUM with a note that it's a known trade-off.

Hmm, let me also look at whether there are simpler issues:

- **The `executor.go` change moves the `f.Fix = fix` assignment before the syntax guard. Previously, the order was: assign Fix, clear FixWarning. Now: assign Fix, validate, set/clear FixWarning. The fix is committed even if it's invalid. This is per the sprint plan clarification ("the attempted fix stays visible"). Good.**

- **What if `fix` is nil or causes a panic? Unlikely since it's a string. OK.**

- **The `validateGoFixSyntax` function is in `syntaxguard.go` and is unexported. Good (lowercase).**

- **The `nonGoFenceLangs` map is defined at package level. The `:= true` syntax inside a map literal is valid Go. Good.**

Let me also check: is there a potential issue with the `fenceRe` regex not matching if the language tag contains a space? Like ```` ``` go\n...``` ````? The regex `[A-Za-z0-9_+-]*` doesn't match space, so the language tag capture would be empty, and the regex would need to match ```` ``` \n...``` ```` - but there's a space after ```, not a `\n`. So the match would fail.

This is a real edge case. Some markdown renderers allow a space after the opening fence. LOW severity.

OK, I think I have enough findings. Let me also consider:

- **The `TestValidateGoFixSyntax_ValidStatementSnippet` test**: `src := "if err != nil {\n\treturn err\n}"`. This is valid Go when wrapped in a function. The test expects no error. The `blockOpenRe` would match "if err != nil {" (ends in `{`). So `looksLikeGoCode` returns true. Then `parseGoFix` is called. The first strategy (full file) fails (no package). The second strategy (prefixed package) - `"package p\nif err != nil {\n\treturn err\n}"` - this is invalid because `if` is a statement, not a top-level decl. So `declErr` is non-nil. The third strategy (wrapped in function) - `"package p\nfunc _() {\nif err != nil {\n\treturn err\n}\n}\n"` - this is valid! So `stmtErr` is nil, returns nil. Good, test passes.

- **The `TestValidateGoFixSyntax_InvalidUnfencedCode` test**: `src := "func add(a, b int) int {\n\treturn a +\n}"`. The content has `func` at start, matching `declKeywordRe`. Also `{` at end of line, matching `blockOpenRe`. So `looksLikeGoCode` returns true. `parseGoFix` - full file fails (no package). Prefixed package - `"package p\nfunc add(a, b int) int {\n\treturn a +\n}"` - this is invalid because `return a +` is incomplete. So `declErr` is non-nil. Wrapped - also invalid. Returns the error. Good.

OK, the implementation is correct for the tested cases. The main concern is the false positive for prose starting with decl keywords.

Let me finalize my findings. I'll report:

1. MEDIUM: False positive risk in `looksLikeGoCode` for prose starting with decl keywords (import, type, var, const).
2. LOW: `fenceRe` doesn't match fences with a space after the opening backticks.
3. LOW: `executor.go` log message format doesn't handle empty `f.File`.

Actually, let me reconsider #3. If `f.File` is empty, the message is ":<line>: error" which is still parseable. And `f.File` is documented to be non-empty for findings. So this is very minor. I'll skip it or make it LOW.

Let me also check: is there a potential issue with the `parser.ParseFile` call when the content is empty after trimming? The code checks `if code == "" { return nil }` before `parseGoFix`. Good.

What about the `fenceRe` matching a fence with no closing fence? Like just ```` ```go\ncode ```` (no closing)? The regex requires `\n?``` ` at the end. If there's no closing, no match. The code returns the full fix as `code` with `hadFence = false`. Then `parseGoFix` is called on the full content (including the opening ```go), which would fail to parse. If the content has braces (unlikely with the stray ```go), it might be flagged. But this is an edge case. LOW.

OK, I'll write up the findings. Let me be concise.

Actually, I want to reconsider whether the false positive issue is worth flagging. The sprint plan says "never flag prose change-instructions as invalid". The current heuristic can flag some prose. This is a gap between the stated goal and the implementation. It's a real issue. MEDIUM.

But the fix is non-trivial. I should suggest a concrete approach. For example: only use `declKeywordRe` as a signal if the keyword is followed by Go-valid syntax (e.g., `import` followed by a string literal, `var` followed by an identifier and type). Or: require the presence of `{` or `}` alongside the decl keyword. Or: check if the content parses as Go code more broadly.

A simpler fix: only treat `package` as a decl signal (since `package xxx` is unambiguous), and for other keywords, require additional code signals (braces, `:=`, semicolons, etc.). But this would miss valid decl fragments like `var x int` without braces.

Actually, `var x int` is a valid top-level decl. The heuristic currently matches it via `declKeywordRe`. The false positive is for prose that happens to start with the word "var" or "import" etc. In practice, LLM-generated change instructions for Go code are unlikely to start with "Var names should..." or "Import the data...". So the false positive is unlikely.

I'll flag it as MEDIUM but note the trade-off.

Let me write the final output.

Wait, I should also consider: is the `looksLikeGoCode` function supposed to be conservative (prefer false negatives over false positives) or aggressive (catch more invalid code)? The comment says "conservative recall" - meaning it's OK to miss some invalid code to avoid false positives. The false positive for "import the data..." would be a violation of the conservative principle. MEDIUM.

OK, final findings:

1. MEDIUM|syntaxguard.go:99-103|False positive: prose starting with decl keywords (import/type/var/const/func/package) is incorrectly flagged as code|Require an additional code signal (e.g., braces or `:=`) alongside decl keywords, or validate that the keyword is followed by Go-valid syntax|correctness|15|declKeywordRe matches prose like "Import the data from CSV" which then fails to parse and is incorrectly flagged|code-reviewer

2. LOW|syntaxguard.go:30|fenceRe doesn't match fences with a space after opening backticks (e.g., ``` ` go`)|Trim leading whitespace from the language tag capture or allow optional space after opening fence|maintainability|5|``` go\n...``` would not be recognized as a fenced block|code-reviewer

Actually, let me reconsider #2. The regex is ```` (?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?``` ````. If the input is ```` ``` go\ncode\n``` ````:
- Opening ``` matches at position 0-2.
- Language tag `[A-Za-z0-9_+-]*` matches empty string at position 3.
- Then need `\n`, but have ` ` (space). No match at this position.
- The regex engine would try other positions, but there's no other opening ```. So the match fails.
- `extractFencedCode` returns the full input as code, `hadFence = false`.
- The content "``` go\ncode\n```" would be parsed as Go. "``` go" is not valid Go. It would fail to parse.
- `looksLikeGoCode`: does "``` go\ncode\n```" match any pattern? `declKeywordRe` doesn't match (no decl keyword at line start). `blockOpenRe`: lines are "``` go", "code", "```". None end with `{`. `blockCloseRe`: no line starts with `}`. So `looksLikeGoCode` returns false.
- So the fix passes through unflagged. The false negative (not catching the invalid code) is the outcome, not a false positive.

So this is actually a false negative case, not a false positive. LOW severity, and the outcome is acceptable (the fix is not incorrectly flagged, it's just not caught). I'll keep it as LOW.

Hmm, but is this a real issue? The CommonMark spec says the language tag is the text after the opening fence up to the end of the line. So ```` ``` go ```` would have language tag " go" (with leading space). Most implementations trim this. But Go's regex with `[A-Za-z0-9_+-]*` doesn't allow space. So this is a minor limitation. LOW.

Let me also check: what about ```` ```\ncode\n``` ```` (no language tag)?
- Opening ``` matches.
- Language tag `[A-Za-z0-9_+-]*` matches empty.
- Need `\n`, have `\n`. Match.
- Body `(.*?)\n?``` ` matches "code\n``` ".
- So the full match is ```` ```\ncode\n``` ````, body is "code", lang is "", fenced is true.
- If "code" is valid Go, passes. If invalid, would be flagged (since hadFence is true).
- This is correct behavior.

OK, my findings are solid. Let me also add:

3. LOW|syntaxguard.go:106-115|conservative heuristic may miss unfenced broken fragments without block structure or decl keywords|documented as intentional trade-off, but consider adding a third check for `:=` at start of line (already a signal) or semicolons|maintainability|10|unfenced one-liner like "x := " would not be flagged even if broken|code-reviewer

Actually, the comment already documents this: "an unfenced fragment with no block structure (no braces, no `:=`, no leading declaration keyword) is not flagged even if malformed". Wait, it says "no `:=`" - but `looksLikeGoCode` doesn't check for `:=`. The comment says `:=` is not checked, and these are treated as prose. This is consistent. So the heuristic intentionally doesn't use `:=` as a signal to avoid false positives on inline `:=` in prose. This is documented. Not an issue.

Let me finalize. I'll report the MEDIUM and the LOW.

Actually, one more thing. Let me check the `TestRenderMarkdown_ShowsFixWarning` test more carefully:

```go
{FixWarning: "invalid_syntax: 2:1: expected '}"},
```

The single quote in the message. The renderer uses `escTrunc`. What does `escTrunc` do? It likely HTML-escapes. So `'` becomes `&#39;`. The test asserts on `"invalid_syntax: 2:1: expected"` which is before the `'`. This should work.

But wait, the test comment says: "The message text is HTML-escaped by the renderer; assert the unescaped portion." So the test acknowledges HTML escaping. Good.

OK, final output. Let me write 2-3 findings.

Actually, I realize I should double-check: is the false positive for "import the data..." really a concern? Let me think about realistic LLM output.

An LLM generating a fix for a Go file might output:
- "Add error handling to the Connect function"
- "Use a parameterized query instead of string concatenation"
- "Import the sync package" (as a prose instruction)
- "Add an import statement for the foo package"

"Import the sync package" - starts with "import" and has a space. The regex matches "import" at line start. Then `parseGoFix` is called. "import the sync package" as Go - "import" is a keyword, "the" is not a valid import path. So it fails. Flagged as invalid.

But "import the sync package" is a valid prose change instruction! The LLM is saying "add an import for the sync package". This is a legitimate fix instruction, not code. And it would be incorrectly flagged.

This is a real, plausible false positive. MEDIUM.

Similarly: "Var declarations should use the new naming convention" - starts with "var", flagged. But it's prose.

"Type the result into the output buffer" - starts with "type", flagged. But it's prose.

"Const values should be defined at the package level" - starts with "const", flagged. But it's prose.

These are all realistic prose instructions that would be falsely flagged. The heuristic has a clear gap.

I'll report this as MEDIUM.

Let me write the final output.

One more check: the `looksLikeGoCode` function is called only when `hadFence` is false (unfenced content). From the code:
```go
parseErr := parseGoFix(code)
if parseErr == nil {
    return nil
}
if hadFence || looksLikeGoCode(code) {
    return parseErr
}
return nil
```

So `looksLikeGoCode` is only called for unfenced content. For fenced content, the fence itself is the signal, and any parse error is reported. This means:
- "```go\nimport the data from CSV\n```" - fenced, parse fails, flagged. False positive!
- "Import the data from CSV" - unfenced, looksLikeGoCode returns true (import matches), parse fails, flagged. False positive!

So the false positive exists for both fenced and unfenced prose instructions starting with decl keywords. The fence doesn't help because the fence language tag is "go" (explicit Go), and the parser fails on the prose.

Wait, for the fenced case: the fence is ```go, so `nonGoFenceLangs` doesn't include it. The content "import the data from CSV" is parsed as Go and fails. Flagged. False positive.

This is a real issue. MEDIUM.

OK, final findings. Let me write them out.

Let me also consider: should I flag the lack of a test for this false positive case? The test `TestValidateGoFixSyntax_ProseWithKeywordsNotFlagged` uses "If the input is nil, return an error..." which doesn't trigger `declKeywordRe`. A test for "Import the sync package" would expose the false positive. But the issue is in the heuristic itself, not the tests. I'll flag the heuristic issue.

Alright, I'm confident in my findings. Let me write the output.

Final list:
1. MEDIUM - False positive in looksLikeGoCode for prose starting with decl keywords
2. LOW - fenceRe doesn't match fences with space after opening backticks

That's it. Let me format them.
</think>

MEDIUM|internal/verify/syntaxguard.go:99-103|looksLikeGoCode false-positives on prose change-instructions starting with decl keywords (e.g. "Import the sync package", "Type the result into the buffer", "Const values should be defined at package level")|Require an additional code signal alongside the decl keyword, or only treat `package` as a code signal and require braces/`:=` for the others|correctness|15|declKeywordRe matches prose lines like `Import the sync package`; parseGoFix then fails and the legitimate prose fix is incorrectly flagged `invalid_syntax`|code-reviewer
LOW|internal/verify/syntaxguard.go:30|fenceRe requires `\n` immediately after the opening fence and rejects the CommonMark-permitted space (e.g. ` ``` go\n...`) — such fences fall through as unfenced, so a malformed fix in a spaced fence is never flagged|Allow optional whitespace between the opening fence and language tag, or trim a leading space in the capture|maintainability|5|`(?s)```([A-Za-z0-9_+-]*)\n` does not match ` ``` go\n`; the content is treated as unfenced and may bypass validation|code-reviewer