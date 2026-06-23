<think>
Let me carefully review this diff against the sprint plan and look for issues.

The sprint plan is for Epic 7.5: Syntax Guard Refinements. The scope is:
- `internal/verify/syntaxguard.go` - Add non-Go brace-content detection ahead of block-brace signal in `looksLikeGoCode`
- `internal/verify/syntaxguard_test.go` - Add unfenced JSON/config characterization tests

Let me analyze the diff:

1. **New regex `jsonKeyLineRe`**: `^\s*"[^"]*"\s*:` - matches lines starting with optional whitespace, then a double-quoted key, then a colon.

2. **`looksLikeGoCode` modification**: Now checks for declaration keyword first (always true), then checks for non-Go braces (suppress), then checks block braces.

3. **New function `looksLikeNonGoBraces`**: Returns true if `jsonKeyLineRe` matches AND no declaration keyword.

Let me think about potential issues:

**Issue 1: `looksLikeNonGoBraces` ordering logic**
The function `looksLikeGoCode` first checks `declKeywordRe`. If that matches, it returns true. So `looksLikeNonGoBraces` is only called when `declKeywordRe` does NOT match. Therefore, the check `!declKeywordRe.MatchString(s)` inside `looksLikeNonGoBraces` is redundant — the caller already ensures `declKeywordRe` doesn't match before calling. This is a minor inefficiency, not a correctness bug, but it's worth noting.

Wait, actually, is `looksLikeNonGoBraces` exported or called from anywhere else? Looking at the test:
```go
func TestLooksLikeNonGoBraces(t *testing.T) {
    assert.True(t, looksLikeNonGoBraces("{\n  \"k\": 1\n}"), "quoted-key object is non-Go")
    assert.False(t, looksLikeNonGoBraces("func f() {\n\treturn 1\n}"), "a Go func is not non-Go braces")
    ...
}
```

The test calls `looksLikeNonGoBraces` directly with Go code like `func f() { ... }`. The `!declKeywordRe.MatchString(s)` check ensures this returns false for Go code with decl keywords. So the redundant check in `looksLikeNonGoBraces` is actually needed for the test to pass and for correctness when called directly.

But when called from `looksLikeGoCode`, the decl keyword check is already done. So it's not incorrect, just slightly redundant in one call path. This is a LOW severity issue at best - more of a maintainability note.

**Issue 2: The regex `jsonKeyLineRe`**
The regex is `^\s*"[^"]*"\s*:` with multiline flag. 

Wait, let me check - in Go, `(?m)` makes `^` and `$` match line boundaries. So `^\s*"[^"]*"\s*:` should match a line that starts with optional whitespace, then `"..."`, then optional whitespace, then `:`.

But what about lines like `"key" :`? The `\s*` allows whitespace before the colon. That seems fine.

What about `// "key":` (a comment with a quoted key)? The regex would match this. Hmm, but this is in the context of `looksLikeNonGoBraces` which is only called when parse has already failed. A Go file with a `// "key":` comment that also has block braces... this is an edge case. Let me think...

Actually, if the content is valid Go with a comment, it would parse successfully and never reach `looksLikeGoCode`. So this is only a concern for broken Go that happens to have a comment with a quoted key. That's a very narrow case and not really an issue.

**Issue 3: Test coverage for `switch s { case "foo": ... }`**
The test `TestValidateGoFixSyntax_BrokenGoSwitchStringCaseStillFlagged` checks that a broken Go switch with `case "foo":` is still flagged. Let me trace through:

Source: `switch s {\ncase "foo":\n\treturn 1 +\n}`

1. `parseGoFix` - this would fail because `return 1 +` is incomplete
2. `looksLikeGoCode` is called
3. `declKeywordRe` - does it match? `declKeywordRe` is `(?m)^\s*(func|type|var|const|import|package|return|if|for|switch|select|defer|go|chan|map|struct|interface)\b`
   - `switch` is a decl keyword! So `declKeywordRe` matches, and `looksLikeGoCode` returns `true`.
4. So the fix is flagged as invalid.

The test expects `require.Error`, which matches. Good.

But wait - if `switch` is a declaration keyword, then a valid Go switch statement would also match `declKeywordRe`, but it would have parsed successfully and never reached this path. So the concern is only for broken Go. This is fine.

**Issue 4: `jsonKeyLineRe` regex with escaped quotes**
The regex `"[^"]*"` doesn't handle escaped quotes within the key (e.g., `"key\"sub":`). But JSON keys can have escaped quotes. This is a minor edge case. For the conservative-recall policy, missing some JSON shapes is acceptable (just means those shapes still get flagged as Go, which is the old behavior - no regression).

Actually wait, this could be a real issue. Consider a JSON object with a key containing escaped quotes:
```
{
  "weird\"key": "value"
}
```

The regex `^\s*"[^"]*"\s*:` would match the first `"` at position 0, then `[^"]*` would match `weird\` (no quote), then `"` at position 7 (the escaped quote). Wait, `\"` in the source is a single character `"`. So the string is `"weird"key": ...` — let me re-think.

In actual text: `"weird\"key": "value"`
Characters: `"`, `w`, `e`, `i`, `r`, `d`, `\`, `"`, `k`, `e`, `y`, `"`, `:`, ...

The regex `"[^"]*"` starts matching at position 0 with `"`, then `[^"]*` matches `weird\` (0 or more non-quote chars), then needs `"` - but the next char is `"` (the escaped quote). So the regex matches `"weird\"` as the "key". Then `\s*:` needs to match - but next is `k`, not whitespace or colon. So the regex doesn't match this line.

So a JSON key with an escaped quote would not be detected, and the JSON would still be flagged. This is a false positive (JSON being flagged as Go) - but the sprint plan says "AC2: Detection only suppresses flagging — no input that was previously unflagged becomes flagged". This is actually a missed suppression, not a new flag, so AC2 is still satisfied. It's just incomplete coverage of the JSON false positive. The conservative-recall policy tolerates this.

This is a LOW severity issue - incomplete coverage of an edge case, but doesn't violate any acceptance criteria.

**Issue 5: Trailing comma in JSON**
JSON allows trailing commas in some dialects. The regex `^\s*"[^"]*"\s*:` doesn't care about commas - it just looks for the key-value pattern. So this is fine.

**Issue 6: Single-quoted keys**
Some config formats use single quotes. The regex only matches double quotes. But the sprint plan specifically says "quoted keys" and the examples are all JSON with double quotes. This is acceptable per scope.

**Issue 7: JSON without braces**
What about a JSON array of primitives? `["a", "b", "c"]` - this has `[` and `]` but no `{` or `}`. The `blockOpenRe` and `blockCloseRe` only match `{` and `}`. So a JSON array wouldn't trigger the block-brace signal in the first place and wouldn't be flagged. So this is fine - no change needed.

**Issue 8: The `looksLikeNonGoBraces` redundancy**
As noted in Issue 1, the `!declKeywordRe.MatchString(s)` check inside `looksLikeNonGoBraces` is redundant when called from `looksLikeGoCode` (which already checks `declKeywordRe` first). But it's needed for the test `TestLooksLikeNonGoBraces` which calls it directly with Go code. So it's actually necessary for the function to be correct as a standalone predicate. Not really an issue.

**Issue 9: Regex compilation**
`jsonKeyLineRe` is a package-level variable compiled once. Good practice.

**Issue 10: The comment says "A `case "x":` label does NOT match"**
Let me verify: `case "x":` - the regex `^\s*"[^"]*"\s*:` would need to match starting at the line beginning. The line starts with `case`, not `"`, so it wouldn't match. Correct.

**Issue 11: Test `TestValidateGoFixSyntax_BrokenStringKeyedMapSuppressed_AcceptedFalseNegative`**
The comment is very thorough. The test expects `assert.NoError` for a broken JSON-shaped fix. This is the accepted false negative per the conservative-recall policy. This is documented and intentional.

**Issue 12: What about YAML/config with quoted keys?**
YAML can have shapes like `"key": value` which would match the regex. That's fine - it's non-Go content and should be suppressed.

**Issue 13: What about Go string constants?**
```go
var s = "timeout"
```
This has `"timeout"` but no colon after it on the same line at the start. The regex needs `"...":` with a colon. So a simple string declaration wouldn't match. Good.

What about:
```go
const m = "key": value
```
This is invalid Go and would fail to parse. Would the regex match? The line is `const m = "key": value`. The regex `^\s*"[^"]*"\s*:` - the line starts with `const`, not `"`. So it wouldn't match. And `declKeywordRe` would match `const`, so `looksLikeGoCode` returns true and it's flagged. Correct.

**Issue 14: `looksLikeNonGoBraces` is not truly a Go signal check**
The function is called from `looksLikeGoCode` only when `declKeywordRe` doesn't match. But the function itself includes a `!declKeywordRe.MatchString(s)` check. This is a minor code quality issue - the function could be simplified to just `return jsonKeyLineRe.MatchString(s)` since the caller already filters out decl keywords. But keeping it makes the function correct as a standalone predicate (as tested). This is a maintainability concern, not a bug.

**Issue 15: Line with just `"key":` (no value)**
The test `TestValidateGoFixSyntax_BrokenStringKeyedMapSuppressed_AcceptedFalseNegative` uses:
```
{
  "a": 1,
  "b":
}
```

The line `"b":` matches the regex (quoted key, colon, no value). The line `"a": 1,` also matches. So `jsonKeyLineRe` matches, and `looksLikeNonGoBraces` returns true. The fix is suppressed. This is the intended behavior.

But wait - would `parseGoFix` actually fail on this? Let me think... `{"a": 1, "b":}` - this is not valid Go (missing value for "b"). `go/parser` would fail. So yes, it reaches `looksLikeGoCode`, and it's suppressed. Correct.

**Summary of issues found:**

1. LOW: The `!declKeywordRe.MatchString(s)` check in `looksLikeNonGoBraces` is redundant when called from `looksLikeGoCode` (caller already filters). But it's needed for the standalone test. This is more of a design note.

2. LOW: The `jsonKeyLineRe` regex doesn't handle escaped quotes in JSON keys (`\"`). This means some JSON objects with escaped quote keys won't be suppressed. But this doesn't violate AC2 (no new flagging), it's just incomplete coverage. Conservative-recall tolerant.

3. LOW: The regex could potentially match Go code that has a comment with a quoted key on its own line, like:
```go
func f() {
    // "commented": out
    return 1 +
}
```
Wait, would this parse? `func f() { // "commented": out return 1 + }` - actually the `//` makes everything to end of line a comment. Let me think of a proper example:
```go
func f() {
    x := 1
    // "key": value
}
```
This is valid Go and would parse. Never reaches the guard.

What about broken Go with a comment containing a quoted key?
```go
func f() {
    // "key": value
    return 1 +
}
```
This would fail to parse. `declKeywordRe` matches `func`, so `looksLikeGoCode` returns true. Flagged. Correct.

What about:
```
{
    // "key": value
    x := 1
}
```
This would fail to parse (mixed braces and Go). `declKeywordRe` matches `:=`? No, `declKeywordRe` is `^\s*(func|type|var|const|import|package|return|if|for|switch|select|defer|go|chan|map|struct|interface)\b`. `x` doesn't match. So it falls to the non-Go check. `jsonKeyLineRe` matches the comment line `"key": value` (wait, the comment line starts with `//`, not `"`). Let me re-read: `    // "key": value`. The regex `^\s*"[^"]*"\s*:` - after the optional whitespace, the next char is `/`, not `"`. So it doesn't match. The braces trigger the block-brace signal and it's flagged. This is a narrow edge case and acceptable.

Actually, what about:
```
// "key": value
{
    x := 1
}
```
The first line `// "key": value` - the regex `^\s*"[^"]*"\s*:` - after whitespace, next char is `/`. Doesn't match. The braces trigger flagging. Correct.

What about a comment on its own line that starts with a quote? Like:
```
"key": value
{
    x
}
```
The first line matches the regex! And there's no decl keyword. So `looksLikeNonGoBraces` returns true, and the fix is suppressed. But this is broken Go that should be flagged. Hmm, is this a real concern?

This would be a false negative (broken Go not flagged). But the conservative-recall policy explicitly accepts false negatives. And this is a very contrived case. Not a real issue.

OK, let me think about what the most important findings are:

1. The `looksLikeNonGoBraces` function has a redundant `!declKeywordRe.MatchString(s)` check when called from `looksLikeGoCode` - this is a minor maintainability issue. Actually, on reflection, this is needed for the standalone test, so it's not really an issue. I'll skip this.

2. The regex doesn't handle escaped quotes in JSON keys - LOW severity, conservative-recall tolerant.

3. Let me look more carefully at the regex itself for correctness issues.

`jsonKeyLineRe = regexp.MustCompile(`(?m)^\s*"[^"]*"\s*:`)`

The `[^"]*` means zero or more non-quote characters. So an empty key `"":` would match. That's fine for JSON (empty string is a valid JSON key).

What about a key with spaces? `"key with spaces":` - the regex matches `"key with spaces":` (the `[^"]*` includes spaces). Fine.

What about Unicode in keys? `"ключ":` - the regex matches. Fine.

What about numeric-looking keys? `"123":` - matches. Fine.

I think the regex is correct for its intended purpose.

Let me also check: is there any issue with the `looksLikeNonGoBraces` being called on content that hasn't been fenced-extracted? Looking at the call site in `looksLikeGoCode`, it's called on the raw `s` parameter. `looksLikeGoCode` is called from `validateGoFixSyntax` on the full content. The `extractFencedCode` is called first in `validateGoFixSyntax` to extract fenced content. So `looksLikeGoCode` receives the extracted (or full) content. The non-Go detection works on the full extracted content, which is correct.

Wait, let me re-check. Looking at the original code (not in the diff), `validateGoFixSyntax` likely calls `extractFencedCode` first, then `parseGoFix`, then `looksLikeGoCode`. If fenced content is found, only that is checked. If not, the full content is checked. The `looksLikeNonGoBraces` detection works on whatever content is passed to `looksLikeGoCode`, which is correct.

One more thing: the test `TestValidateGoFixSyntax_UnfencedJSONArrayNotFlagged`:
```go
src := "[\n  {\n    \"name\": \"a\",\n    \"on\": true\n  }\n]"
```

The array has `{` and `}` (from the inner object). `blockOpenRe` matches `  {` (opening brace with optional non-brace prefix). `blockCloseRe` matches `  }`. So `looksLikeGoCode` would return true without the suppression. With the suppression: `jsonKeyLineRe` matches `"name": "a"` and `"on": true`. No decl keywords. So `looksLikeNonGoBraces` returns true, and `looksLikeGoCode` returns false. Fix is not flagged. Correct.

But wait - `parseGoFix` would fail on this (it's not valid Go). So it reaches `looksLikeGoCode`. Correct.

Actually, I want to double-check: does `parseGoFix` fail on a JSON array? `[\n  {\n    "name": "a",\n    "on": true\n  }\n]` - this is not valid Go. `go/parser` would fail. So yes, `validateGoFixSyntax` would flag it without the suppression. With the suppression, it's not flagged. The test expects `NoError`. Correct.

Let me think about whether there are any other issues...

**Issue: The `looksLikeNonGoBraces` function could match Go content**
Consider this Go code (which is valid and would never reach the guard, but hypothetically):
```go
var m = map[string]int{
    "key": 1,
}
```
This is valid Go. It would parse and never reach the guard. Fine.

What about broken Go:
```go
var m = map[string]int{
    "key":
}
```
Missing value. Fails to parse. `declKeywordRe` matches `var` and `map`. So `looksLikeGoCode` returns true (decl keyword check comes first). Flagged. Correct.

What about:
```go
m := map[string]int{
    "key":
}
```
`declKeywordRe` - does it match? The pattern is `^\s*(func|type|var|const|import|package|return|if|for|switch|select|defer|go|chan|map|struct|interface)\b`. `m` is not in the list. `map` is. But the line starts with `m`, not `map`. So `declKeywordRe` doesn't match. Falls to non-Go check. `jsonKeyLineRe` matches `"key":`. So `looksLikeNonGoBraces` returns true, and the fix is suppressed. This is a false negative (broken Go not flagged). But the sprint plan explicitly accepts this: "A broken Go map/struct literal with string keys that fails to parse would be suppressed (a false negative). Explicitly acceptable under the 7.1 conservative-recall policy."

So this is documented and intentional. Not an issue.

I think the diff is well-implemented and well-tested. The only minor issues I can find are:

1. LOW: Regex doesn't handle escaped quotes in JSON keys (incomplete coverage, conservative-recall tolerant)
2. LOW: The `!declKeywordRe` check in `looksLikeNonGoBraces` is partially redundant (needed for standalone use, so not really an issue)

Actually, I realize I should be more careful. Let me re-read the scope constraint:

"Only flag issues in files/areas directly related to the work items below."

The files are `internal/verify/syntaxguard.go` and `internal/verify/syntaxguard_test.go`. All my findings are in these files. Good.

"Do NOT flag issues in unrelated code that happens to appear in the diff"

All the changes are related to the work items. Good.

Let me finalize my findings. I think the most notable issue is:

1. The regex `jsonKeyLineRe` `(?m)^\s*"[^"]*"\s*:` - the `[^"]*` in the key doesn't account for escaped quotes (`\"`). JSON allows escaped quotes in keys (e.g., `"a\"b": 1`). Such keys would not be detected, leaving a residual false positive. This is LOW severity because:
   - It's a narrow edge case (escaped quotes in JSON keys are uncommon in config)
   - It doesn't violate AC2 (no new flagging, just missed suppression)
   - Conservative-recall policy tolerates incomplete detection

Actually, wait. Let me reconsider. Is this really worth flagging? The sprint plan says "keep detection narrow" and the conservative-recall policy explicitly accepts false negatives. A JSON key with an escaped quote is extremely rare in practice. And the fix would only mean a residual false positive (JSON still flagged as Go), which is the pre-existing behavior - no regression.

I think this is worth a LOW finding.

Are there any other issues? Let me think once more...

Actually, I want to check: does the `jsonKeyLineRe` regex have any issues with the `\s*` before the colon? Consider:
```
"key" : 1
```
The `\s*` allows whitespace before the colon. This is fine for JSON (whitespace is allowed).

What about:
```
"key":value
```
No space after colon. The regex `\s*:` matches with zero whitespace. Fine.

What about a Go struct tag? Like:
```go
type T struct {
    Field `json:"key":value`
}
```
Hmm, this is getting contrived. The regex would match `"key":value` on the struct tag line. But this is broken Go (the backtick syntax is wrong). `declKeywordRe` matches `type`. So `looksLikeGoCode` returns true. Flagged. Correct.

I think the diff is solid. Let me also check if there are any issues with the test file.

**Test issue: `TestValidateGoFixSyntax_BrokenGoWithBracesStillFlagged_75`**
```go
src := "func add(a, b int) int {\n\treturn a +\n}"
```
This is broken Go (incomplete return). `declKeywordRe` matches `func` and `return`. So `looksLikeGoCode` returns true. Flagged. Test expects error. Correct.

**Test issue: `TestValidateGoFixSyntax_BrokenGoSwitchStringCaseStillFlagged`**
```go
src := "switch s {\ncase \"foo\":\n\treturn 1 +\n}"
```
Broken Go. `declKeywordRe` matches `switch` and `return`. Flagged. Test expects error. Correct.

**Test issue: `TestValidateGoFixSyntax_BrokenStringKeyedMapSuppressed_AcceptedFalseNegative`**
```go
src := "{\n  \"a\": 1,\n  \"b\":\n}"
```
Broken (missing value for "b"). `declKeywordRe` doesn't match (no Go keywords). `jsonKeyLineRe` matches `"a": 1,` and `"b":`. So `looksLikeNonGoBraces` returns true. Suppressed. Test expects NoError. Correct.

But wait - does `parseGoFix` actually fail on this? `{ "a": 1, "b": }` - this is not valid Go. `go/parser` would fail. But `parseGoFix` has three OR-strategies. Let me think about what those might be...

The comment says "parseGoFix's three OR-strategies — recorded WONTFIX (conservative-recall by design)". So `parseGoFix` tries multiple strategies. If any succeeds, the fix is considered parseable. For `{ "a": 1, "b": }`, all three strategies would fail. So it reaches `looksLikeGoCode`. Correct.

Actually, I realize I don't have the full `parseGoFix` code. But based on the test expectations, it must fail on this input. I'll trust the tests.

**Test issue: `TestLooksLikeNonGoBraces`**
```go
assert.True(t, looksLikeNonGoBraces("{\n  \"k\": 1\n}"), "quoted-key object is non-Go")
```
`jsonKeyLineRe` matches `"k": 1`. `declKeywordRe` doesn't match. Returns true. Correct.

```go
assert.False(t, looksLikeNonGoBraces("func f() {\n\treturn 1\n}"), "a Go func is not non-Go braces")
```
`jsonKeyLineRe` - does it match? The lines are `func f() {`, `	return 1`, `}`. None start with `"`. Doesn't match. So `looksLikeNonGoBraces` returns... wait, the function is `jsonKeyLineRe.MatchString(s) && !declKeywordRe.MatchString(s)`. If `jsonKeyLineRe` doesn't match, the AND is false, returns false. Correct.

But wait - what if `jsonKeyLineRe` doesn't match but `declKeywordRe` does? Then the function returns false because the first condition is false. That's correct - if there's no JSON shape, it's not non-Go braces.

```go
assert.False(t, looksLikeNonGoBraces("type T struct {\n\tX int\n}"), "a Go type decl is not non-Go braces")
```
`jsonKeyLineRe` doesn't match (no line starts with `"`). Returns false. Correct.

```go
assert.False(t, looksLikeNonGoBraces("switch s {\ncase \"x\":\n}"), "a quoted case label does not start the line")
```
`jsonKeyLineRe` - the line `case "x":` starts with `case`, not `"`. Doesn't match. Returns false. Correct.

All tests look correct.

Let me think about whether there are any correctness issues I'm missing...

Actually, I want to reconsider the regex more carefully. The `jsonKeyLineRe` is `(?m)^\s*"[^"]*"\s*:`.

The `(?m)` flag makes `^` match at the beginning of each line (after a newline). The `\s*` matches optional whitespace at the start of the line. Then `"` opens the key. `[^"]*` matches the key content (any non-quote characters). Then `"` closes the key. Then `\s*:` matches optional whitespace and a colon.

This is correct for JSON object members at the start of a line.

But what about JSON where the key is NOT at the start of a line? Like:
```json
{ "timeout": 30 }
```
The key is not at the start of a line (there's `{ ` before it). The regex `^\s*"[^"]*"\s*:` with multiline mode would look for lines starting with `"`. The line `{ "timeout": 30 }` starts with `{`, not `"`. So it wouldn't match.

But the multi-line version:
```json
{
  "timeout": 30
}
```
The line `  "timeout": 30` starts with whitespace, then `"`. The regex matches. Correct.

So the regex only works for multi-line JSON where the key is on its own line (indented). Single-line JSON `{ "key": 1 }` would not be detected. This is another incomplete coverage case.

Is this a real issue? Single-line JSON with braces:
- `{"key": 1}` - has `{` and `}`, so `blockOpenRe`/`blockCloseRe` would match. Without the suppression, it would be flagged. With the suppression, `jsonKeyLineRe` doesn't match (key not at line start in a single-line format), so `looksLikeNonGoBraces` returns false, and `looksLikeGoCode` returns true. Still flagged.

So single-line JSON is still a residual false positive. This is a LOW severity issue - incomplete coverage but no regression.

Actually, wait. The test `TestValidateGoFixSyntax_UnfencedJSONObjectNotFlagged` uses multi-line JSON. The test `TestValidateGoFixSyntax_UnfencedNestedJSONNotFlagged` also uses multi-line. There's no test for single-line JSON. This means single-line JSON `{ "key": 1 }` is not covered by the suppression. This is a gap in the implementation relative to the stated goal (AC1: "an unfenced multi-line JSON/config snippet with block braces is NOT flagged"). AC1 specifically says "multi-line", so single-line is technically out of scope. The implementation matches the AC.

But it's worth noting as a LOW finding that the regex only matches keys at the start of a line, so single-line JSON objects with braces would still be flagged.

Hmm, but the AC says "multi-line" explicitly. So this is by design. I won't flag it.

Let me think about one more thing: the `jsonKeyLineRe` regex with the `(?m)` flag. In Go, `(?m)` is the same as `(?s)`? No, `(?m)` is the multiline flag (Perl-compatible). In Go's regexp, the flags are:
- `(?i)` - case-insensitive
- `(?m)` - multi-line mode (`^` and `$` match line boundaries)
- `(?s)` - let `.` match `\n`

So `(?m)^\s*"[^"]*"\s*:` is correct for matching quoted keys at the start of any line.

I think the implementation is correct. Let me finalize my findings.

Given the scope and the quality of the implementation, I have one LOW finding:

1. The `jsonKeyLineRe` regex `(?m)^\s*"[^"]*"\s*:` doesn't handle escaped quotes in JSON keys (e.g., `"a\"b": 1`). Such keys would not be detected, leaving a residual false positive. However, this is:
   - Acceptable under conservative-recall (no regression, just incomplete coverage)
   - A narrow edge case (escaped quotes in JSON keys are uncommon)
   - Not a violation of any AC

This is a LOW severity finding.

Actually, I'm now second-guessing whether this is worth flagging. The implementation is deliberately narrow, and the escaped quote case is extremely rare. The conservative-recall policy explicitly tolerates false negatives (which this would be - a JSON that should be suppressed but isn't). 

But from a code review perspective, it's worth noting that the regex has this limitation, especially since the comment claims it's for "the canonical JSON object member shape" - a key with escaped quotes is still canonical JSON.

I think a LOW finding is appropriate.

Let me also consider: is there any issue with the `looksLikeNonGoBraces` function name? It's called from `looksLikeGoCode` and returns true for non-Go content. The name is a bit misleading - it's more like "is this non-Go brace content?" which is the inverse of "looks like Go code". But the name is clear enough. Not an issue.

One more thing: the comment on `jsonKeyLineRe` says "Go source almost never starts a line this way: a quoted string appears at line start only in a map/struct literal with string keys". This is a good comment. It documents the known limitation (map/struct literal false negative) upfront.

OK, I'll go with one LOW finding about the escaped quote limitation.

Wait, actually, I want to reconsider whether there are any more significant issues. Let me re-read the diff one more time...

The `looksLikeGoCode` function:
```go
func looksLikeGoCode(s string) bool {
	if declKeywordRe.MatchString(s) {
		return true
	}
	if looksLikeNonGoBraces(s) {
		return false
	}
	return blockOpenRe.MatchString(s) || blockCloseRe.MatchString(s)
}
```

The logic is:
1. If any Go declaration keyword → Go
2. If non-Go braces → not Go
3. If block braces → Go
4. Otherwise → not Go

This is correct. The non-Go check only suppresses, never adds. The decl keyword check is a strong Go signal that's never suppressed. The block brace check is the weaker signal that can be suppressed by non-Go detection.

Everything looks correct. I'll go with my one LOW finding.

Actually, let me reconsider one more time. The `jsonKeyLineRe` regex `(?m)^\s*"[^"]*"\s*:` - what if there's a line like:
```
"key": // some comment
```
The regex would match (the line starts with `"key":`). But this could be Go code. Let me think of a realistic example:

```go
var s = "key": // this is a Go syntax error
    value
```
This is broken Go. `declKeywordRe` matches `var`. Flagged. Correct.

What about:
```go
m := map[string]int{
    "key": // comment
    value,
}
```
This is valid Go (the comment is fine). It would parse. Never reaches the guard.

What about broken Go:
```go
m := map[string]int{
    "key": // comment
}
```
Missing value. Fails to parse. `declKeywordRe` - does it match? `m` is not a keyword. `map` is a keyword but the line starts with `m`, not `map`. So `declKeywordRe` doesn't match. Falls to non-Go check. `jsonKeyLineRe` matches `"key":` on the second line. `looksLikeNonGoBraces` returns true. Suppressed. False negative. But this is the accepted cost per the sprint plan.

OK, I'm satisfied. The implementation is correct and well-tested. My only finding is the LOW severity regex limitation with escaped quotes.

Let me also check: is the `looksLikeNonGoBraces` function properly tested for the case where `jsonKeyLineRe` matches but `declKeywordRe` also matches? Looking at the test:

```go
func TestLooksLikeNonGoBraces(t *testing.T) {
    assert.True(t, looksLikeNonGoBraces("{\n  \"k\": 1\n}"), "quoted-key object is non-Go")
    assert.False(t, looksLikeNonGoBraces("func f() {\n\treturn 1\n}"), "a Go func is not non-Go braces")
    ...
}
```

The second assertion tests that Go code (with decl keywords) returns false. But it doesn't have a JSON-shaped line, so `jsonKeyLineRe` doesn't match, and the function returns false from the first condition. The `!declKeywordRe` check is not actually exercised by this test.

To properly test the `!declKeywordRe` check, you'd need content that has both a JSON-shaped line AND a Go declaration keyword. Like:
```
func f() {
    "key": 1
}
```
This is broken Go. `jsonKeyLineRe` matches `"key": 1`. `declKeywordRe` matches `func`. So `looksLikeNonGoBraces` returns false (because of the `!declKeywordRe` check). This ensures the suppression doesn't apply to Go code that happens to have a JSON-shaped line.

Is this tested? Let me check... No, there's no test for this case. The test `TestValidateGoFixSyntax_BrokenGoWithBracesStillFlagged_75` uses `func add(a, b int) int { return a + }` which doesn't have a JSON-shaped line. So the `!declKeywordRe` check in `looksLikeNonGoBraces` is not directly tested.

This is a minor test coverage gap. The logic is correct (if there's a Go keyword, it's Go, not non-Go), but it's not explicitly tested. I'll note this as a LOW finding.

Actually, wait - is this really worth flagging? The logic is clearly correct, and the test for `looksLikeGoCode` indirectly covers it (if `looksLikeNonGoBraces` incorrectly returned true for Go code with decl keywords, `looksLikeGoCode` would return false, and Go code would not be flagged). The existing tests for broken Go (like `TestValidateGoFixSyntax_BrokenGoWithBracesStillFlagged_75`) verify that broken Go with braces is still flagged, which implicitly tests the `!declKeywordRe` check.

Hmm, but those tests don't have JSON-shaped lines. So they don't actually exercise the `!declKeywordRe` check. The `looksLikeNonGoBraces` function could have a bug where it returns true for content with both JSON shape and Go keywords, and the existing tests wouldn't catch it.

Let me think about whether this is a real risk. The function is:
```go
func looksLikeNonGoBraces(s string) bool {
	return jsonKeyLineRe.MatchString(s) && !declKeywordRe.MatchString(s)
}
```

If `!declKeywordRe.MatchString(s)` were removed (bug), then:
- A broken Go function with a JSON-shaped line (like `func f() { "key": 1 }`) would be suppressed
- This would be a false negative (broken Go not flagged)
- The conservative-recall policy accepts false negatives

So even with the bug, it wouldn't violate any AC. It's a degraded case but not a failure. And the test for `looksLikeNonGoBraces` with Go code doesn't have JSON-shaped lines, so the test would still pass.

Is this worth flagging? It's a test coverage gap for a function that has a specific safety check (`!declKeywordRe`). A test like:
```go
assert.False(t, looksLikeNonGoBraces("func f() {\n\t\"key\": 1\n}"), "Go code with JSON-shaped line is not non-Go braces")
```
would explicitly test this case.

I'll flag this as a LOW finding for test coverage.

OK, let me finalize. My findings:

1. LOW: `jsonKeyLineRe` regex doesn't handle escaped quotes in JSON keys - incomplete coverage, conservative-recall tolerant
2. LOW: Missing test for `looksLikeNonGoBraces` when content has both JSON-shaped lines and Go declaration keywords (the `!declKeywordRe` check is not directly exercised)

Actually, finding #2 is more of a test coverage suggestion than a code issue. The code is correct. I'll include it as a LOW finding for test coverage.

Hmm, but finding #1 is also more of a limitation note than a bug. The conservative-recall policy explicitly accepts incomplete detection. Both are LOW severity.

Let me also reconsider: is there any issue with the `looksLikeNonGoBraces` function being called on content that has already been determined to not have a declaration keyword? As I noted, the `!declKeywordRe` check is redundant in the call from `looksLikeGoCode` but needed for standalone use. This is a minor maintainability note. I won't flag this.

Final findings:
1. LOW: Regex limitation with escaped quotes
2. LOW: Missing test coverage for `!declKeywordRe` check in `looksLikeNonGoBraces`

Actually, I'm overthinking this. Let me just flag the most notable issues. The implementation is solid, well-documented, and well-tested. The only real concerns are the regex limitation and the minor test gap.

Let me write up the findings.

Wait, I want to double-check one more thing. The `jsonKeyLineRe` regex uses `(?m)` which is Go's multiline mode. In Go, `(?m)` makes `^` and `$` match at the beginning and end of each line (after `\n`). This is correct for matching keys at the start of any line.

But what about `\r\n` line endings? The `^` in multiline mode matches after `\n` in Go. If the content has `\r\n` line endings, the `\r` before `\n` means `^` would match after `\r`, not at the actual start of the line. The `\s*` at the beginning would consume the `\r`. So this should still work correctly.

Actually, let me think more carefully. In Go's regexp with `(?m)`:
- `^` matches at the beginning of the text and after each `\n`
- If the text is `"line1\r\nline2"`, then `^` matches at position 0 and after `\n` (position 7)
- But position 7 is `\n` itself? No, `^` matches the position after `\n`, which is the start of `line2`
- Wait, `\r\n` - the `\n` is at position 6, so `^` matches at position 7 (start of `line2`)

So for `\r\n` line endings, `^` correctly matches at the start of each line. The `\s*` would match any leading whitespace, including `\r` if present. This is fine.

OK, I'm satisfied. Let me write the findings.

Actually, one more thought. The `jsonKeyLineRe` regex `(?m)^\s*"[^"]*"\s*:` - what if the line has leading whitespace that includes tabs? `\s` matches tabs in Go. So `^\s*` matches any combination of spaces and tabs. Fine.

What about Unicode whitespace? `\s` in Go matches Unicode whitespace by default. This is fine for most cases.

OK, final findings. I'll go with:

1. LOW: `jsonKeyLineRe` doesn't handle escaped quotes in JSON keys - residual false positive for a narrow edge case, acceptable under conservative-recall
2. LOW: `looksLikeNonGoBraces` test doesn't cover the case where content has both a JSON-shaped line and a Go declaration keyword (the `!declKeywordRe` guard is not directly exercised)

Let me write them up in the required format.

Hmm, actually, for finding #2, let me reconsider. The test `TestValidateGoFixSyntax_BrokenGoWithBracesStillFlagged_75` uses `func add(a, b int) int {\n\treturn a +\n}`. This has Go keywords. The `looksLikeGoCode` function would return true at the first check (`declKeywordRe`), so `looksLikeNonGoBraces` is never called. The test verifies that broken Go with braces is flagged, but it doesn't exercise the `!declKeywordRe` check in `looksLikeNonGoBraces`.

To exercise that check, you'd need content where:
- `parseGoFix` fails
- `declKeywordRe` matches (so `looksLikeGoCode` returns true at the first check)
- `looksLikeNonGoBraces` is never called

Wait, that's the point - if `declKeywordRe` matches, `looksLikeNonGoBraces` is never called. So the `!declKeywordRe` check in `looksLikeNonGoBraces` is only relevant when the function is called directly (like in the test). The test `TestLooksLikeNonGoBraces` tests the function directly, but with inputs that don't have both JSON shape and Go keywords.

So the `!declKeywordRe` check is a safety net for direct calls. It's not exercised by the integration tests. This is a test coverage gap.

But is it worth flagging? The logic is obviously correct (if there's a Go keyword, it's Go). The test gap is minor. I'll flag it as LOW.

OK, writing up findings now.

One more consideration: the sprint plan says "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

My findings are both in `internal/verify/syntaxguard.go` and `internal/verify/syntaxguard_test.go`, which are the files in scope. So both findings are in-scope.

Final output:
</think>

LOW|internal/verify/syntaxguard.go:60|jsonKeyLineRe regex does not handle escaped quotes in JSON keys|Use `"(?:[^"\\]|\\.)*"` to match keys containing escaped chars|correctness|5|Residual FP for `{"a\"b":1}`; conservative-recall tolerant|bruce
LOW|internal/verify/syntaxguard_test.go:271|looksLikeNonGoBraces not tested with content having both JSON key line and Go decl keyword|Add case like `func f() {\n\t"k": 1\n}` asserting False|maintainability|5|!declKeywordRe guard is only reachable via direct calls; no test exercises it|bruce