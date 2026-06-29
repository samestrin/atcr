<think>
Let me analyze this diff carefully. The changes are about adding brace-language AST parsers for TS/JS, PHP, Rust, and Bash. Let me look for issues in the relevant files.

Key areas to review:
1. `internal/astgroup/embed.go` - registry entries
2. `internal/astgroup/host_test.go` - test changes
3. `internal/astgroup/parsers/SHA256SUMS` - checksums
4. `internal/astgroup/parsers/build.sh` - build script
5. `internal/astgroup/parsers/src/braceparser/` - new parser source
6. `internal/astgroup/testdata/corpus.json` - test corpus

Let me identify potential issues:

1. **build.sh: Using `${tagflag[@]+"${tagflag[@]}"}` pattern**: This looks suspicious. The `${var+...}` syntax expands to "..." if the var is set (even if empty). This is bash array expansion idiom. Let me look more carefully. The `${tagflag[@]+"${tagflag[@]}"}` is a way to handle the case where an array is empty - it expands to the array if it's set (including empty), but actually with `+` it doesn't check for emptiness, just whether it's set. This might work but is unusual. Actually this idiom is for handling empty arrays in older bash. With Bash 4.3+, you could just use `"${tagflag[@]}"` directly. But it might still be a valid approach.

Wait, actually there might be a bug. Let me think... when `tags` is empty, `tagflag=()` is set (empty array). The `${tagflag[@]+...}` would expand to the array contents (which is empty), but that means the command would be `go build ... -o output .` with nothing between `-trimpath` and `-o`. That might actually work fine.

But wait, there's a potential issue with word splitting. When `${tagflag[@]+"${tagflag[@]}"}` expands, the inner `"${tagflag[@]}"` is quoted, so if there are tags, they pass as a single argument. But if the array is empty, the whole `${tagflag[@]+...}` produces nothing. So it works.

Actually I notice the bigger issue: the `tagflag` array is declared with `local tagflag=()`. When tags is empty, the array is set but empty. The `${tagflag[@]+...}` will expand to nothing because the array is empty. When tags is set, it expands to `-tags bash` for example. OK this seems intentional.

But wait - the issue is the `${tagflag[@]+"${tagflag[@]}"}` is a bit weird. Let me think again. The standard idiom for bash empty arrays before 4.4 is `${arr[@]+"${arr[@]}"}`. This is correct.

2. **Sprint plan says acceptance criterion**: AC1 says "A single shared brace-block parser plugin parses all four languages". But the diff creates 4 separate .wasm binaries (`ts.wasm`, `php.wasm`, `rust.wasm`, `bash.wasm`). This contradicts AC1 but the clarifications explicitly noted: "'Single shared binary' / '~3–4 MB' appears only in the Proposed Solution / NFR sections, not the binding Technical Constraints; it is consciously relaxed." So this is acceptable per the clarifications. The acceptance criterion should probably be updated to reflect this, but it's a docs issue.

3. **embed.go - Extension case sensitivity**: The LanguageForExt has a comment saying "lower-cased" but the .mts, .cts, .mjs are lowercased already. The case `.PHP` wouldn't be handled. But this matches the original `.go` and `.py` handling, so it's consistent.

4. **parse_core.go - lineCommentStarts comment**: The comment says "For the '#' marker under commentWordBoundary (Bash), the '#' must sit at a word boundary". Let me check the implementation. The `lineCommentStarts` function loops through line comments, and for `#` checks word boundary. Looks correct.

5. **parse_core.go - charLiteralLen handling '\u{7f}'**: The comment says "Multi-byte escapes like '\u{7f}' are not length-matched here; returning 0 leaves the lone quote as ordinary text, which is safe (it cannot open a string in Rust mode)." Let me check: In Rust, `'` is a char literal delimiter if followed by a char and closing `'`. If we see `'\u{7f}'`, the next char after `\` is `u`, not `'`, so `charLiteralLen` returns 0, leaving the `'` as text. That's fine.

But wait - `'\u{7f}'` contains `{` and `}` which are inside a char literal. Since we don't recognize this as a char literal, we'd treat `'` as text and `{` and `}` as braces. This could affect brace counting. The comment acknowledges this trade-off and says it's "safe (it cannot open a string in Rust mode)". This is true - the worst case is a false block detection that would be a minor grouping issue, but a misparse degrades to line proximity. OK this is acknowledged.

6. **parse_core.go - state management in loop**: After a `case stLineComment:`, we don't update `lineStart`. Wait, let me check. After case `c == '\n'` we update `lineStart = i + 1`. So if we're in `stLineComment` and hit `\n`, state becomes Normal, then we fall through to the `if c == '\n'` block which updates line. OK.

7. **parse_core.go - rawStringStart logic**: When we see `r#"...`, we set `rawHashes = countHashes(src, i+1)`. Then `i += 1 + rawHashes`. But the body iteration starts after this, and at position `i+1` after the increment, we'd be at the opening `"`. The string starts there. Wait, the iteration loop will then check `c == src[i]` which is `"`. So `state` is `stRawString` and we'd consume that. But there's a subtle issue: the body would process the `"` and look for the closing `"` followed by hashes. That looks correct.

Actually wait, there's a subtle issue. After the `i += 1 + rawHashes`, the loop increments `i++` at the end (no, it doesn't - the loop has `for i := 0; i < len(src); i++`). So after `i += 1 + rawHashes`, the next iteration increments `i++` to point past the hashes to `"`. So `c = '"'` and we enter `stRawString`. Good.

But wait, in `stRawString`, when we see `"`, we check `hasHashes(src, i+1, rawHashes)`. At position `i+1` after the closing `"`, we'd be at the hashes. OK.

8. **parse_core.go - `escape` state in strings**: In `stString`, when `escape` is true, we set `escape = false`. But we never increment past the escaped char. The `i++` at the top of the loop handles that. So `\\n` (newline escape) would be: see `\`, set escape. Next iter `c = 'n'`, escape was true, so escape=false. The newline character wouldn't trigger line counting. Good.

But what about `\\`? See `\`, set escape. Next iter `c = '\'`, escape was true, so escape=false. Then we process `\` again - but we're back in stString without escape, so `c == '\\'` would set escape again. OK.

9. **parse_core.go - heredoc handling**: For PHP heredocs, `<<<EOT`, then `EOT` on its own line. But PHP actually requires `EOT;` (with semicolon) or `EOT,` etc. on its own line. The `heredocLineMatches` function checks `s == tag` OR `strings.HasPrefix(s, tag)` followed by non-ident byte. So `EOT;` would match because `;` is not an ident byte. Good.

10. **parse_core.go - heredoc body braces**: The state `stHeredoc` only updates line counting on `\n`. It doesn't check braces. Good.

11. **TestParseSource_SimpleFunction** in parse_core_test.go: Uses `tsConfig`. But the test isn't behind a build tag, so it's available in all builds. The tests reference `tsConfig` which is in `configs.go`. OK.

12. **braceparser/go.mod**: `go 1.26` - this seems unusual. Most projects are on 1.22, 1.23 etc. But this is the project's choice.

13. **embed.go - Four additional .wasm files embedded**: The four new .wasm files add to the binary size. The sprint plan acknowledges this trade-off.

14. **braceparser/main.go - pin map**: The `pins` map is a global that's never cleared except via `free`. If `free` is never called by the host, memory leaks. But this matches the existing pattern.

15. **braceparser/main.go - emit function**: It allocates a new buffer with `alloc()` but doesn't check if the allocation succeeded. Actually `alloc()` always succeeds (returns a non-negative int).

16. **build.sh - tags flag expansion**: When `tags` is empty, the command is:
    ```
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath ${tagflag[@]+"${tagflag[@]}"} -o ...
    ```
    With empty array, `${tagflag[@]+...}` produces nothing, so command is:
    ```
    go build -buildmode=c-shared -trimpath -o output .
    ```
    That's fine.

    When tags is "bash", `tagflag=(-tags bash)`, the expansion is `"${tagflag[@]}"` = `-tags bash`. So command becomes:
    ```
    go build -buildmode=c-shared -trimpath -tags bash -o output .
    ```
    That looks fine.

17. **Sprint plan naming convention issue**: The plan calls out "Build FOUR separate .wasm binaries (ts/php/rust/bash) from ONE parameterized Go source (braceparser)". The actual diff does this. Good.

18. **Missing test for corpus benchmark**: The corpus.json is extended but I should check if there's a test that runs through it. The plan references `TestBenchmark_ASTGroupingAccuracy`.

19. **Potential issue with `isHeredocStart` for PHP**: For PHP `<<<EOT`, after `<<<` we have `EOT`. The function skips spaces, then optional `'` or `"` (PHP allows quoted heredoc identifiers), then ident. So for `<<<EOT`, after `<<<`, the first char is `E` (ident byte, not digit). Good.

    But for `<<<EOT\n`, the function reads `EOT` then... the function doesn't actually consume the newline. It just checks that the position is at an ident byte. The `parseHeredoc` then reads the ident bytes. So it reads `EOT` as the tag. Then the rest of the loop continues. Good.

20. **braceparser/go.mod - go 1.26**: This is a future version. As of early 2026, go 1.26 might exist. But this is suspicious. Let me check the original goparser or pyparser go.mod. The diff doesn't include them, but the original `parsers/go.wasm` and `parsers/python.wasm` already exist, so they have a go.mod already. If those use a different version, there'd be a discrepancy. But this might just be the project upgrading.

    Wait, the existing files presumably have their own go.mod. Adding a new one with `go 1.26` might require it. But this is a minor concern.

21. **Missing src/braceparser/README or build instructions**: Not strictly an issue.

22. **Possible issue: `main.go` is `//go:build wasip1`** - this means it's only built when targeting wasip1. The other files (`parse_core.go`, `configs.go`, tests) don't have this tag. So `go test` (non-wasm) builds all of them. But `main.go` is only included in wasm builds. So when you `go test` in the braceparser dir, main.go is excluded. The tests reference `tsConfig`, `phpConfig`, etc. and `parseSource`. The `main()` function is empty anyway. OK.

23. **Bash heredoc test**: `TestParseSource_BashHeredocBracesIgnored` uses inline bash config (not `bashConfig`). The local config doesn't have `paramExpand: true`. So `${...}` inside the heredoc isn't relevant.

24. **Potential security issue**: The wazero host loads and executes the .wasm files. If someone could replace the .wasm files, they could run arbitrary code. The SHA256SUMS guard against tampering. Good.

25. **braceparser/configs.go - PHP `funcParen: false` but comment says "so funcParen stays off to avoid mislabeling bare calls"** - This is correct because PHP `function` is a keyword, not a funcParen pattern.

26. **braceparser/configs.go - TS has `funcParen: true`** - This handles method shorthand `name(...) { ... }`. Good.

27. **braceparser/configs.go - Bash has `funcParen: true`** - Handles `name() { ... }`. Good.

28. **braceparser/parse_core.go - `stParamExp` state**: When we see `${`, we increment `i` once (to consume `{`) and set `paramDepth = 1`. Then we enter `stParamExp`. Inside, `{` increments depth, `}` decrements. When depth reaches 0, we go back to normal. But the `${` consumes `{` already (i incremented once) and we set depth to 1. So when we see the matching `}`, depth becomes 0 and state returns to normal. Then the next byte is processed normally. That looks right.

    Wait, there's a subtle issue. After consuming `${`, we're inside `stParamExp` and `i` points to the char after `${`. The loop then iterates. When we see `}` matching the outer one, paramDepth becomes 0 and state returns to normal. But the next iteration, `i` is incremented to point past `}`. So we'd then process whatever comes after the `${...}` as normal. Good.

    For nested `${a:-${b:-c}}`:
    - See `${`, i += 1 (consumes `{`), paramDepth = 1, state = stParamExp
    - See `a`, normal processing
    - See `:`, normal processing  
    - See `-`, normal processing
    - See `${`, ... wait, `${` is `$` followed by `{`. In stParamExp, `$` doesn't matter. We just see `{`, paramDepth becomes 2.
    - Then `b`, `:`, `-`, `c`, then `}`, paramDepth becomes 1.
    - Then `}`, paramDepth becomes 0, state = normal.
    - Good, nested handled correctly.

29. **braceparser/configs.go - `paramExpand: true` for bash**: This is critical because `${var}` in bash should not affect brace depth. The state machine handles it correctly.

30. **braceparser/configs.go - PHP heredocs**: PHP heredocs use `<<<` and the tag can be in quotes. `<<<"EOT"` is valid PHP. The `isHeredocStart` allows optional `'` or `"` after the operator. Then `parseHeredoc` reads optional quotes and ident bytes.

    But wait, the `parseHeredoc` function reads the quotes but doesn't include them in the tag. So for `<<<"EOT"`, the tag is `EOT`. When the heredoc closes, we look for `EOT` on its own line. But the closing tag in PHP `EOT;` should be `EOT;` (the semicolon is required). The closing detection works because `heredocLineMatches` checks if the line starts with `EOT` followed by a non-ident byte. So `EOT;` matches because `;` is not an ident byte. Good.

31. **braceparser/configs.go - Rust `paramExpand` not set**: Rust doesn't use `$` for parameter expansion in the same way, so not setting it is correct.

32. **braceparser/configs.go - `rawStrings: true` for Rust only**: Correct.

33. **Potential issue: `arrowFunc: true` for TS only**: Correct.

34. **Potential issue: `lineComments` for TS uses `["//"]`**: TS also supports `<!--` and `-->` for JSX, but those aren't real line comments. They appear inside JSX text. This is an edge case not covered but probably fine for a heuristic parser.

35. **Potential issue: PHP `heredocOp: "<<<"`**: PHP has multiple heredoc styles: `<<<EOT` (regular), `<<<'EOT'` (nowdoc, no interpolation). The `heredocs: true` with `heredocOp: "<<<"` would handle both. The interpolation in regular heredocs is handled by the string state machine... wait, no. PHP heredocs aren't strings in the regular sense. Inside a heredoc, `$var` is interpolated. The current implementation enters `stHeredoc` which ignores everything except newlines. This means `$var` inside a heredoc isn't tracked. For grouping purposes, the braces inside `${var}` are ignored. But PHP heredocs use `$var` and `{$var}`, not `${var}`. So `{$var}` would have braces that the parser would try to track. Hmm.

    Wait, in PHP heredoc state, we just look for newlines and check if the line matches the terminator. Other chars including `{` and `}` are ignored. So `{$var}` inside a heredoc wouldn't affect brace depth. Good.

    But what about `$arr[0]` inside heredoc? `[]` aren't tracked anyway. Fine.

36. **braceparser/parse_core.go - state variable `escape` and `escape = false` reset only inside string state**: After `escape = false` we don't reset escape to false on next string entry. But `state = stString` is followed by `escape = false`. OK.

37. **braceparser/parse_core.go - `stParamExp` doesn't reset `escape` or other state**: We're already in stParamExp, not string. OK.

38. **braceparser/parse_core.go - `stRawString` checks for `"` and `hasHashes(src, i+1, rawHashes)`**: After `"`, check if there are exactly `rawHashes` `#` chars. If yes, consume them and exit. The `i += rawHashes` skips the hashes. Then loop increments i++ to next char. Good.

39. **braceparser/parse_core.go - `isHeredocStart` after bit-shift**: For `a << b`, `isHeredocStart` checks after `<<`. The position after `<<` is `b`. Is `b` an ident byte? Yes. But is the previous byte to `<<` at a word boundary? Wait, the function only checks after `<<`, not the context. So `a << b` would be detected as a heredoc start with tag `b`. That's a bug!

    Let me re-check: `matchAt(src, i, cfg.heredocOp)` checks if `<<` is at position i. For `a << b`, `i` points to the first `<`. The previous byte ` ` (space) is fine. Then `isHeredocStart(src, i+len(cfg.heredocOp))` is called with `i+2`, pointing to `b`. It skips spaces (none), checks optional `'` or `"` (no), then checks `isIdentByte(b[2]) && !(b[2] >= '0' && b[2] <= '9')`. `b` is an ident byte and not a digit, so true. So we enter heredoc mode! That's a bug.

    Wait, but the bash config has `heredocs: true` and `heredocOp: "<<"`. So bash would have this bug. But `isHeredocStart` would say "yes, this looks like a heredoc". Then `parseHeredoc` would read `b` as the tag. So when `b` is encountered on its own line, the heredoc would close. That's wrong for `a << b`.

    Hmm, actually `parseHeredoc` reads from `i + len(cfg.heredocOp)` which is `i + 2`. For `a << b`, `i+2` points to `b`. It skips spaces (none), checks optional `-` or `~` (no), checks optional `'` or `"` (no), then reads ident bytes: `b`. So tag is `b`, strip is false, consumed is 1.

    Then we'd enter heredoc state with tag `b`. The next newline triggers state transition. Then we look for `b` on its own line. If `b` is on its own line later in the source, we'd close the heredoc there. That's wrong.

    This is a real correctness bug. However, the corpus might not exercise this case, so it might not be caught by tests. And the comment in the code says "Guards against treating a bit-shift `a << b` as a heredoc" - but it doesn't actually guard against it!

    Wait, let me re-read the comment: `// isHeredocStart reports whether a heredoc tag plausibly begins at src[j] (after the heredoc operator): an optional -/~ then optional quote then an identifier char. Guards against treating a bit-shift a << b as a heredoc.`

    The comment claims it guards against this. But the code doesn't actually check for the context (e.g., that we're at the start of a line, or that there's no expression before `<<`). So the comment is misleading.

    For PHP, `<<` isn't a heredoc operator - only `<<<` is. So `isHeredocStart` with `cfg.heredocOp = "<<<"` only matches `<<<`. For `a << b`, this is `<<` not `<<<`, so it's not matched. Good for PHP.

    For Bash, `<<` is the operator. The issue exists. But: bash heredocs typically appear at the start of a line (after pipe or other operator). Common bash patterns like `cat <<EOF`, `cat <<-EOF`, `cmd <<<word`, etc. The pattern `a << b` (without newline before `b`) wouldn't normally be a heredoc. But the code doesn't check this context.

    This is a real issue. Let me flag it.

    Actually, looking more carefully - in `a << b`, the previous char to `<<` is ` ` (space) - but actually if it's `a << b`, the chars are `a`, ` `, `<`, `<`, ` `, `b`. The function checks if `i` has `<<`. The function `isHeredocStart(src, i+2)` checks the char at position `i+2`. For `a << b`, that's `b`. It returns true.

    But for a real heredoc like `cat <<EOF`, the chars are `cat <<EOF`. Position of first `<` is at index 4. Then `i+2 = 6`, which is `E`. So `EOF` would be the tag. Same as expected.

    The difference is that for `a << b`, the `<<` is in the middle of an expression, but the code doesn't check that. This is a real bug.

    The fix would be to require that the heredoc operator is at the start of a line (after optional whitespace) or after a command separator (`;`, `|`, `&`, etc.).

40. **braceparser/parse_core.go - Inconsistent state handling**: Let me check. In the `stHeredoc` case, we only handle `\n`. But what if the heredoc terminator is on the last line without a trailing `\n`? Then the heredoc never closes, and subsequent code would be in stHeredoc mode. The end-of-loop would close blocks but the heredoc state isn't explicitly closed. The end result: a heredoc with no closing tag swallows the rest of the source. That's a minor edge case but matches typical heuristic behavior.

41. **braceparser/parse_core.go - `matchAnyPrefix` is defined but unused**: Let me check. I see `func matchAnyPrefix(src []byte, i int, prefixes []string) bool`. It's not called anywhere in the diff. This is dead code. Should be flagged as maintainability.

42. **braceparser/parse_core.go - Unused `addHeader` calls for `(` and `)`**: Inside `case c == '('`, we increment `parenDepth` and call `addHeader(c)`. Same for `)`. This is intentional - we want to include parens in the header for parsing. OK.

43. **braceparser/parse_core.go - `header` might have leading spaces trimmed**: The `addHeader` function skips initial spaces but adds subsequent spaces. So `function f()` becomes `function f()`. OK.

44. **braceparser/parse_core.go - `headerLine` for blocks with keywords on previous lines**: For:
    ```
    function
    f() {
    ```
    The header would accumulate `function\nf()`. The line counter increments when `\n` is added. But the `\n` is replaced with space in the header. So `function f()` would be the header. `classifyHeader` would find `function` keyword and `f` as the name. Good.

    For:
    ```
    function
    {
    ```
    The header is `function`. classifyHeader finds `function`, name = "" (no name after). Returns func with no name. OK.

45. **braceparser/parse_core.go - `parenDepth` handling for `for (let i = 0; ...)`**: The `(`, `;`, `;`, `)` characters are all added to header. The `;` inside parens doesn't reset the header. Good.

46. **braceparser/parse_core.go - For TS arrow functions**: `const handler = (e) => { ... }`. The `=>` is detected via `cfg.arrowFunc`. The `classifyHeader` returns `"func", ""`. Good.

47. **braceparser/parse_core.go - Object literal after `const cfg = { ... }`**: The header is `const cfg = `. No keyword matches. `cfg.arrowFunc` is false (no `=>`). `cfg.funcParen` is true for TS, but the header doesn't end with `)`. So returns `"block", ""`. Good.

48. **braceparser/parse_core.go - Issue with `matchAt` returning false if `s == ""`**: `cfg.blockOpen` is `""` for bash. So `matchAt` returns false for empty blockOpen. The case `cfg.blockOpen != "" && matchAt(src, i, cfg.blockOpen)` short-circuits on `cfg.blockOpen != ""`. Good.

49. **braceparser/parse_core.go - TestParseSource_TemplateLiteralIgnored**: Uses `tsConfig`. Template literal `${x}` inside backtick string. In `stString` with delim `'`, `{` and `}` are ignored. Good. But wait - the backtick string isn't actually entered. Let me check. In `stNormal`, we see `'`, enter `stString` with strDelim `'`. So we process the backtick string content as if it were a single-quoted string. The single-quote handling means: any `'` ends the string. But the source has backticks around `${x} { } b`. So we enter stString on first `'`, and exit on matching `'`. The content includes backticks, `${`, `}`, `{`, `}`. None of those trigger brace tracking. Good.

50. **Potential issue: braceparser/configs.go - `funcParen: true` for TS could misclassify method calls**: For example, `someFunction(x, y, z)` followed by `{ ... }` on next line. But this is unusual in TS. The code checks if the trimmed header ends with `)`. If `someFunction(x, y, z)` is followed by `{`, the header would be `someFunction(x, y, z)`. classifyHeader would return `"func", "someFunction"`. That might be a false positive, but it's consistent with treating method definitions similarly.

    Actually, looking at the code more carefully:
    ```go
    if cfg.funcParen {
        if id, ok := funcParenName(h); ok {
            return "func", id
        }
    }
    ```
    This is a fallback after no keyword matches. So `someFunction() { ... }` would be classified as func. That's the intended behavior for methods and bash functions.

51. **braceparser/parse_core.go - `headerLine` initialization**: Initially `headerLine = 1`. The first byte added to header sets `headerLine = line`. So if the file starts with `function f() {`, headerLine = 1, line = 1, start = 1. OK.

52. **braceparser/parse_core.go - empty header for opening brace**: If a `{` appears at the start with no preceding text, header is empty. `classifyHeader` finds no keyword, no arrow, no funcParen. Returns `"block", ""`. Good.

53. **braceparser/parse_core.go - global variables `pins`**: This is the same pattern as goparser/pyparser. OK.

54. **braceparser/main.go - `if n <= 0` in alloc**: If host calls alloc(0), we allocate 1 byte. OK.

55. **braceparser/main.go - `pins[ptr]` for freed pointer**: If host calls free then uses the pointer, `pins[ptr]` returns the zero value (nil) and `ok` is false. The parse function handles this with `buf, ok := pins[ptr]; if !ok { return emit(node{Kind: "error", Name: "bad pointer"}) }`. Good.

56. **braceparser/main.go - `parse(ptr, n)` checking `int(n) > len(buf)`**: Good bounds check.

57. **braceparser/main.go - `if len(src) == 0` early return**: Empty input returns a bare file node. The comment says "matching the goparser/pyparser empty-source contract". OK.

58. **braceparser/configs_test.go - test functions**: All use `parseSource(src, config)` directly. No issues.

59. **braceparser/parse_core_test.go - `TestParseSource_CStyleForKeepsForKind`**: Uses `tsConfig`. Verifies `for (let i = 0; i < 3; i++)` is classified as `for`. The header accumulates `function f() for (let i = 0; i < 3; i++)`. classifyHeader finds `for` as the last keyword. Returns `for` kind. Good.

60. **Test for `ts_block_comment_drift_same_func`**: Block comment contains 4 lines of content. The lines in the comment don't update `lineStart` correctly... wait, let me check. In `stBlockComment`, we only look for `cfg.blockClose`. We don't update `lineStart` directly. But the outer `if c == '\n' { line++; lineStart = i + 1 }` handles line counting regardless of state. So line counting is correct.

    Wait, the test expects a_line=2 and b_line=8 to be in the same block. Line 8 is after the block comment ends. The block is `handler()` from line 1 to line 10 (closing `}`). So lines 2 and 8 should both resolve to the func. Yes.

61. **braceparser/parse_core.go - `stParamExp` and `escape` are not used together**: They're in different states. OK.

62. **braceparser/parse_core.go - `case c == '\n'` outside switch states**: Looking at the structure:
    ```go
    switch state {
    case stLineComment:
        if c == '\n' {
            state = stNormal
        }
    ...
    default: // stNormal
        switch { ... }
    }
    
    if c == '\n' {
        line++
        lineStart = i + 1
        if heredocPending {
            heredocPending = false
            state = stHeredoc
        }
    }
    ```
    
    So line counting happens regardless of state. Good.

63. **braceparser/parse_core.go - heredocPending logic**: When we detect `<<` followed by tag, we set `heredocPending = true`. Then when we hit `\n`, we enter stHeredoc. This is to handle: `cat <<EOF\n` where `<<EOF` is on the first line, and the heredoc body starts on the next line.

    But what about `cat <<EOF; echo "before"` on one line? In PHP, you can have `<<<EOT` then more code. After `;` would reset the header but the heredoc state is set. Hmm, but PHP heredocs usually start a new line. And bash heredocs typically also start a new line.

    What if `<<EOF` is at the very end of input (no newline after)? `heredocPending` stays true, but we never enter stHeredoc because no `\n`. The heredoc is incomplete. End-of-loop closes blocks. Fine.

64. **braceparser/parse_core.go - `heredocLineMatches` for tab-stripped**: For `<<-EOF`, strip=true. The terminator line can have leading tabs stripped. The function does `strings.TrimLeft(s, "\t")`. Good.

65. **braceparser/parse_core.go - `isHeredocStart` - identifier can't start with digit**: `isIdentByte` allows digits. But the function checks `isIdentByte(src[j]) && !(src[j] >= '0' && src[j] <= '9')`. So digits are excluded as starting chars. Good.

66. **braceparser/parse_core.go - parsing accuracy**: This is a heuristic parser, not grammar-accurate. The sprint plan acknowledges this.

67. **braceparser/configs.go - PHP `elseif` mapped to "if"**: PHP uses `elseif` (single keyword) instead of `else if`. Mapping to `if` is unusual but works for grouping purposes. Could be `else` instead, but the current mapping groups `elseif` body with the conditional check. OK.

68. **braceparser/configs.go - Rust `loop` mapped to "while"**: `loop` is infinite loop in Rust. Mapping to "while" is reasonable. OK.

69. **braceparser/configs.go - Rust `match` mapped to "switch"**: Reasonable mapping. OK.

70. **braceparser/parse_core.go - Inside `stString` state for `\\` (backslash)**: 
    ```go
    case stString:
        if escape {
            escape = false
        } else if c == '\\' {
            escape = true
        } else if c == strDelim {
            state = stNormal
        }
    ```
    
    What about `\\\"` (escaped backslash followed by escaped quote)? Sequence:
    - See `\`, set escape
    - See `\`, escape was true, set escape=false. Don't process the second `\` as escape.
    - See `\`, set escape (we processed `\` as normal because escape was false)
    - See `"`, escape was true, set escape=false. Don't exit string.
    
    Actually wait, let me redo:
    - `\` (escape=true)
    - `\` (escape was true, so escape=false, don't process)
    - `\` (escape was false, so escape=true)
    - `"` (escape was true, so escape=false, don't exit)
    
    Hmm, that doesn't seem right. In a string `"\\\""` (backslash, backslash, backslash, quote), we want the `\\` to be one escape sequence and `\"` to be another. Let me trace:
    - chars: `\`, `\`, `\`, `"`, end of string
    - i=0: c=`\`, escape was false, set escape=true
    - i=1: c=`\`, escape was true, set escape=false
    - i=2: c=`\`, escape was false, set escape=true
    - i=3: c=`"`, escape was true, set escape=false
    
    So we don't exit the string on the final `"`. The string is `"\\\""` which in actual string content is `\\"` (backslash, backslash, quote - wait that's 3 chars).
    
    Actually `\"` is `"`. `\\` is `\`. So `\\\"` = `\` + `"` = `\"`. So the source `"\\\""` represents the string `\"` (backslash followed by quote). The closing `"` is unescaped.
    
    Let me retrace with chars `"`, `\`, `\`, `\`, `"`:
    - We start at i=0 with c=`"`, state=stNormal, enter stString, strDelim=`"`, escape=false.
    - i=1: c=`\`, escape was false, set escape=true
    - i=2: c=`\`, escape was true, set escape=false
    - i=3: c=`\`, escape was false, set escape=true
    - i=4: c=`"`, escape was true, set escape=false
    
    So we don't exit. That's wrong. The string should be `\"` (with closing quote on i=4).
    
    Wait, `"\\\""` has 6 chars: `"`, `\`, `\`, `\`, `"`, `"`. Let me recount.
    
    In Go string literal: `"\\\""` = the characters `"`, `\`, `\`, `\`, `"`. Wait no. `\"` in a Go string literal is the char `"`. So `"\\\""` = `"` + `\` + `"` (4 chars... no wait).
    
    Let me use the Go convention: `"\\\""`. In Go:
    - `"` opens the string
    - `\\` is escaped backslash = `\` char
    - `\"` is escaped quote = `"` char
    - `"` closes the string
    
    So `"\\\""` in Go is the string content `\"` (2 chars: `\` and `"`).
    
    But in the source code we're parsing, the bytes are: `"`, `\`, `\`, `\`, `"`, `"` (6 bytes including both quotes that delimit the string).
    
    OK so let's trace parsing `"\\\""`:
    - i=0: c=`"`, state=stNormal, enter stString with strDelim=`"`, escape=false
    - i=1: c=`\`, escape=false, set escape=true
    - i=2: c=`\`, escape=true, set escape=false
    - i=3: c=`\`, escape=false, set escape=true
    - i=4: c=`"`, escape=true, set escape=false. Don't exit.
    - End of string.
    
    So we never exit stString. The string is never closed. That's a bug, but it's an edge case (triple-escaped quote).
    
    Actually, this is the standard C-style escape handling. Let me think again:
    - `\"` in a string literal means an embedded `"`. So `"\""` is a string containing `"`.
    - The escape sequence is 2 chars: `\` and `"`.
    - The escape applies to the NEXT character, not the current one.
    
    So `\` consumes itself and sets escape=true for the next char. The next char (`"`) is the escaped char, so it's part of the string content.
    
    Trace `"\\\""`:
    - `"` opens string
    - `\` sets escape=true (this `\` is part of escape sequence)
    - `\` has escape=true, so it's the escaped char (part of string content), set escape=false
    - `\` sets escape=true (new escape sequence)
    - `"` has escape=true, so it's the escaped char (part of string content), set escape=false
    - End of source: we're still in stString
    
    So the parser thinks the string is unclosed. But the source `"\\\""` IS unclosed from a parsing standpoint (the closing `"` is escaped).
    
    Actually wait, the source has 5 chars: `"`, `\`, `\`, `\`, `"`. That's `"\\\""`. In bash, this is a string containing `\\"` (3 chars... no).
    
    Let me try once more. In a bash string literal `"\\\""`:
    - `"` opens string
    - `\\` is escaped backslash (single `\` char in string)
    - `\"` is escaped quote (single `"` char in string)
    - `"` closes string
    
    So `"\\\""` is the bash source with 5 chars representing the string `\"` (2 chars: `\` and `"`).
    
    But that's 5 chars total. Let me count again: `"`, `\`, `\`, `\`, `"`. Yes 5 chars.
    
    In our parser:
    - i=0: `"`, enter stString, strDelim=`"`, escape=false
    - i=1: `\`, escape=false, set escape=true
    - i=2: `\`, escape=true, set escape=false (this `\` is the escaped char, part of string)
    - i=3: `\`, escape=false, set escape=true (new escape sequence)
    - i=4: `"`, escape=true, set escape=false (this `"` is the escaped char, part of string)
    
    End of source. We're still in stString. The string is unclosed from parser's view.
    
    But in the actual source, the `"` on i=4 is escaped (it's the escaped `"`), so the string IS open. The closing `"` would be on i=5. But there's no i=5.
    
    So the source `"\\\""` is malformed bash - it never closes the string. The parser correctly identifies it as unclosed.
    
    OK so the parser is correct for this edge case. Good.

71. **braceparser/parse_core.go - `c == '\n'` updating lineStart**: The `lineStart` is the position of the first byte of the current line. After `\n`, `lineStart = i + 1`. Good.

72. **braceparser/parse_core.go - `isHeredocStart` for `cmd <<< word`** in bash: This is a here-string, not a heredoc. The operator is `<<<` not `<<`. So `matchAt(src, i, "<<")` would match the first `<<`. But we have `<<<` - `matchAt` matches `<<` at position 0. The function then calls `isHeredocStart(src, i+2)` which is the third `<`. The function skips spaces (none), checks optional `-`/`~` (no), checks optional `'`/`"` (no), then checks `isIdentByte('<')`. `<` is not an ident byte. So returns false. Good.

    But wait, `matchAt(src, i, "<<")` at position i=3 (first `<` of `<<<`). Then `isHeredocStart(src, i+2)` is position 5 (the third `<`). The function checks `isIdentByte('<')` - false. So `isHeredocStart` returns false. So we don't enter heredoc mode. Good.

    But there's a subtle issue: `matchAt` matches the prefix `<<`. For `<<<`, we match the first `<<`. Then `isHeredocStart` fails. So we don't treat it as a heredoc. Good.

73. **braceparser/parse_core.go - Issue: `matchAt` for `<<<` is matched as `<<` then the `<<` is followed by `<`**. Wait, the case is `cfg.heredocs && matchAt(src, i, cfg.heredocOp) && isHeredocStart(...)`. For PHP `<<<`, `cfg.heredocOp = "<<<"` matches `<<<`. For bash `<<`, `cfg.heredocOp = "<<"` matches `<<`. So bash `<<<` would be matched as `<<` followed by `<`. Then `isHeredocStart(src, i+2)` checks position after the matched `<<`. Position has `<`. `<` is not an ident byte. So `isHeredocStart` returns false. Good.

74. **braceparser/parse_core.go - Potential issue with `for (;;) {}` for Rust**: Rust doesn't have C-style for loops. So this isn't a real Rust pattern. OK.

75. **braceparser/parse_core.go - `braceparser/main.go` file**: Has `//go:build wasip1`. When building for host testing, this file is excluded. So `main()` is not defined when running tests. But Go allows multiple `main` packages in different build configurations. The tests are also in `package main`. So when testing, only `parse_core.go`, `configs.go`, etc. are included. No `main()` function defined, but `package main` is fine.

    Wait, but `package main` requires a `main()` function for `go build`. Since `main.go` has the build tag `wasip1`, when building/testing normally, `main()` is excluded. So `go test ./...` would fail because there's no main function.

    Let me check: tests are in `package main`. To run `go test`, the package needs to compile. A `package main` without `main()` function fails to compile.

    But `main.go` has `//go:build wasip1`. So when NOT building for wasip1, `main.go` is excluded, and there's no `main()`. The package fails to compile.

    Hmm, but the test files import nothing extra. They'd just need `package main` to compile, but without `main()`, the package can't build as an executable.

    Actually, `go test` on a `package main` works fine if there are test files - the test binary is built with a generated `main()`. So `go test ./...` should work.

    Let me double-check by looking at the existing goparser or pyparser modules. They presumably have the same structure.

    Actually, I think `go test` on `package main` works because the test framework generates a `main()` that calls the tests. So this should be fine.

    But there's a subtle issue: when `go test ./...` is run in the root reconcile module, the braceparser directory has its own `go.mod` (nested module). So it's a separate module. The root module's `go build ./...` would not include it. Good.

76. **Potential issue: braceparser go.mod has `go 1.26`**: As of my knowledge, Go 1.26 doesn't exist (latest stable is 1.23 or 1.24). This might cause build issues. Let me check - actually, my knowledge cutoff is January 2026, and Go releases happen in Feb and Aug each year. So Go 1.24 (Feb 2025) and Go 1.25 (Aug 2025) and Go 1.26 (Feb 2026) might exist by then. So `go 1.26` is plausible.

77. **braceparser/main.go - allocation and copy**: `alloc` allocates a buffer and pins it. `emit` calls `alloc` then `copy(pins[p], b)`. If `alloc` returns a different pointer than expected, copy would write to wrong location. But `alloc` returns a valid pointer. OK.

78. **braceparser/main.go - JSON marshal error fallback**: If JSON marshal fails, returns `{"kind":"error","name":"marshal"}`. The `emit` function always returns a valid int64. OK.

79. **braceparser/main.go - empty source returns file node**: `if len(src) == 0 { return emit(node{Kind: "file", StartLine: 1, EndLine: 1}) }`. Good.

80. **braceparser/main.go - parse function**: Returns `(int64(p) << 32) | int64(len(b))`. This packs the pointer (high 32 bits) and length (low 32 bits) into a single int64. The host unpacks it. OK.

81. **braceparser/parse_core.go - `addHeader` for `(` and `)` always**: Even when `parenDepth == 0` and we see `(`, we increment and add to header. Same for `)`. So `(x)` outside of any keyword context would be added to header. OK.

82. **braceparser/parse_core.go - `closeBlock` when stack has only file**: `if len(stack) <= 1 { resetHeader(); return }`. So extra `}` is ignored. Good.

83. **braceparser/parse_core.go - end-of-loop block cleanup**: `for len(stack) > 1 { closeBlock() }`. Closes any unclosed blocks. The file node's EndLine is updated to current line. Good.

84. **braceparser/parse_core.go - `deepest` in test file**: Returns the deepest node covering the line. The host probably has similar logic. OK.

85. **Potential issue: braceparser/parse_core.go - `line` counter and `lineStart` consistency**: After a newline, both `line` and `lineStart` are updated. The `lineStart` is used by `heredocLineMatches` to compare the line content. Let me verify:
    - At start of line 2, `line=2`, `lineStart` is the byte position of the start of line 2.
    - `i` iterates through bytes. When `i == lineStart`, we're at the start of the line.
    - For heredoc matching: we hit `\n` at end of line 1, set state=stHeredoc. Then `line=2`, `lineStart = position of line 2 start`.
    - We iterate through line 2 bytes. At `\n` (end of line 2), we call `heredocLineMatches(src[lineStart:i], ...)`. `lineStart` is the position of line 2 start. `i` is the position of the `\n`. So `src[lineStart:i]` is the content of line 2 (without the newline). Good.

86. **braceparser/configs.go - `funcParen: true` for TS but the parser comment says "TS methods"**: Methods use `name(...) {`. But arrow functions also use `(...) => {`. So the arrow func check handles arrows, funcParen handles methods. Good.

87. **braceparser/configs.go - PHP doesn't have funcParen**: PHP functions use `function name() {`, not `name() {`. So funcParen isn't needed. Good.

88. **braceparser/configs.go - bash `funcParen: true`**: Bash has `name() {` form. Good.

89. **braceparser/configs.go - bash `paramExpand: true`**: Bash has `${var}`. Good.

90. **braceparser/configs.go - `commentWordBoundary: true` for bash**: Bash uses `#` for comments. But `$#` (number of args) and `${#var}` (length) use `#` non-comment. The boundary check prevents these from being treated as comments. Good.

91. **braceparser/configs.go - bash `heredocs: true, heredocOp: "<<"`**: Bash heredocs use `<<EOF`. Good.

92. **braceparser/configs.go - PHP `heredocs: true, heredocOp: "<<<"`**: PHP heredocs use `<<<EOT`. Good.

93. **braceparser/configs.go - Rust `rawStrings: true`**: Rust raw strings `r"..."` and `r#"..."#`. Good.

94. **braceparser/configs.go - Rust `charLiterals: true`**: Rust char literals `'x'`. Good.

95. **braceparser/configs.go - TS `arrowFunc: true`**: TS arrow functions `=>`. Good.

96. **braceparser/parse_core.go - `cfg.arrowFunc` check**: When arrowFunc is true, we check `strings.LastIndex(h, "=>")`. If `=>` is present, return "func" with no name. So `const handler = (e) => { ... }` becomes a func block (anonymous). Good.

97. **braceparser/parse_core.go - `cfg.funcParen` check**: If funcParen is true, we check if header ends with `)` and has a single identifier before `(`. Good.

98. **braceparser/parse_core.go - `funcParenName` edge case**: `strings.HasSuffix(t, ")")` - if header is `()` only, returns false because `name` is empty after trimming. But what about `a()`? `t = "a()"`, suffix is `)`. Index of `(` is 1. `name = "a"`. All ident bytes. Returns "a". Good.

99. **braceparser/parse_core.go - `funcParenName` for `(x) => { ... }`**: Header is `=>`. Wait, no. The header accumulates from the start. So for `const handler = (e) => {`, header is `const handler = (e) => `. Then classifyHeader is called when `{` is seen. The arrow func check matches `=>`. Returns "func". Good.

    But what if arrowFunc is true and the header has `=>` but no keyword? For `(e) => {`, header is `(e) => `. Arrow func matches. Good.

100. **braceparser/parse_core.go - `classifyHeader` with `elseif` for PHP**: PHP keywords include `elseif`. So `elseif { ... }` would be matched. Returns "if". Good.

101. **braceparser/parse_core.go - `lastWholeWord` for partial keywords**: For header `ifelse`, would it match `if`? `lastWholeWord` checks boundaries. At position 0, `if` matches but after `f` is `e` which is an ident byte. So no match. Returns -1. So `ifelse` doesn't match `if`. Good.

102. **braceparser/parse_core.go - `lastWholeWord` for `if (x) { ... }`**: Header is `if (x) `. `lastWholeWord` finds `if` at position 0 with boundaries OK. Returns 0. Then `identAfter` returns "" (space after `if`, no ident). So name = "". Returns "if", "". Good.

103. **braceparser/parse_core.go - `lastWholeWord` for multiple keywords**: For `function f if { ... }`, header is `function f if `. `lastWholeWord` iterates: first finds `function` at 0. Then searches for `if`, finds at position 11 (after `function f `). Updates bestIdx to 11. Then searches for `if` before 11, finds at 0 (but that's the `if` in `function`). Wait, `function` doesn't contain `if` as a whole word. `lastWholeWord` checks whole word boundaries. `function f if `: 
    - Search for `function`: finds at 0, boundaries OK. bestIdx=0, bestKw=function.
    - Search for `if`: starts from end, finds at position 11 (last `if`). Boundaries: before is ` `, OK; after is ` `, OK. bestIdx=11, bestKw=if.
    - Continue with from=11. Search for `if` in h[:11] = `function f `. No `if`. Returns -1.
    - Final: bestIdx=11, bestKw=if. Returns "if", "" (since named=false for if).
    
    Hmm, that's a problem. If you write `function f if something { ... }`, the parser thinks it's an if block. But realistically, this pattern doesn't occur in real code.

104. **braceparser/parse_core.go - `lastWholeWord` for nested keywords**: For `function f() { if (x) { ... } }`, the inner `{` triggers classifyHeader with header `function f() if (x) `. `lastWholeWord` finds `if` at later position. Returns "if". Good.

105. **braceparser/parse_core.go - TS `if (x) { ... }` after `function f()`**: Header is `function f() if (x) `. `lastWholeWord` finds `if`. Returns "if". Good.

106. **braceparser/parse_core.go - Issue with `elseif` for PHP keyword**: `elseif` is in the keyword list. `lastWholeWord` would match `elseif` if it's in the header. But what about `else if` (with space)? `else` is a keyword (returns "else"), `if` is a keyword (returns "if"). For `else if (x) { ... }`, header is `else if (x) `. `lastWholeWord` finds `else` at later position (index 5), bestIdx=5. Returns "else", "". Hmm, so `else if (x) { ... }` would be classified as `else`, not `if`. But that's not necessarily wrong - the body is the else-if block.

    Actually, looking more carefully: for `elseif (x) { ... }`, the keyword `elseif` is matched. Returns "if". For `else if (x) { ... }`, the last keyword is `else`. Returns "else". So the classification differs based on syntax. That's a minor inconsistency but acceptable.

107. **Potential issue with `lastWholeWord` and PHP `elseif` matching `if`**: `lastWholeWord("elseif", "if")` - searches for `if` in `elseif`. Found at position 4. Boundaries: before is `e` (ident byte), so not a boundary. Returns -1. So `elseif` doesn't match `if`. Good. Only matches `elseif` keyword directly.

108. **braceparser/configs_test.go - `TestPHPConfig_TraitIsClass`**: `trait T { function m() { $a = 1; } }`. Header for inner `{` is `trait T function m() `. `lastWholeWord` finds `function` at later position than `trait`. Returns "func", "m". Good - inner is func, not class.

109. **braceparser/configs_test.go - `TestBashConfig_ParamExpansionNotABlock`**: `${name}` and `${other:-def}`. The parser sees `${`, enters stParamExp. Inside, `name`, then `}`, depth becomes 0, exit. Same for `${other:-def}`. So no blocks. Good.

110. **braceparser/configs_test.go - `TestBashConfig_NestedParamExpansion`**: `${a:-${b:-c}}`. Parser sees `${`, enters stParamExp, depth=1. Sees `a:-`, normal. Sees `${`, in stParamExp, sees `{`, depth=2. Sees `b:-c`, normal. Sees `}`, depth=1. Sees `}`, depth=0, exit stParamExp. So no blocks. Good.

111. **braceparser/parse_core_test.go - `TestParseSource_BashHeredocBracesIgnored`**: Uses inline bash config without `paramExpand`. The heredoc body contains `{ not a block }`. In stHeredoc, only `\n` is handled. So braces inside are ignored. Good.

112. **braceparser/parse_core_test.go - `TestParseSource_RustCharLiteralBraceIgnored`**: Uses inline rust config. `'{` and `'}` are char literals. The function handles them correctly. Good.

113. **braceparser/parse_core_test.go - `TestParseSource_CStyleForKeepsForKind`**: `for (let i = 0; i < 3; i++)`. The `(` increments parenDepth. `;` inside parens doesn't reset header. Good.

114. **braceparser/parse_core_test.go - `TestBashConfig_DollarHashNotComment`**: `$# -gt 0`. The `#` is preceded by `$`, not at a word boundary. So `commentWordBoundary` prevents it from being a comment. Good.

115. **braceparser/parse_core_test.go - `TestBashConfig_HashCommentStillWorks`**: `# a comment with a } brace`. The `#` is at the start of the line (word boundary). So it's a comment. The `}` inside is ignored. Good.

116. **braceparser/parse_core_test.go - `TestParseSource_TemplateLiteralIgnored`**: Template literal with `${x}`. Backticks. Single-quoted string handling treats them as part of the string. So `${`, `{`, `}`, etc. are ignored. Good.

117. **Potential issue: braceparser/main.go has `func main()` empty**: `package main` with empty `func main() {}`. When building for wasip1, this is the entry point. The `parse` and `alloc`/`free` are wasm exports, not called via `main`. Good.

118. **braceparser/configs.go - Missing `funcParen` for TS but config has it true**: The TS config has `funcParen: true`. The parser handles methods like `name(args) { ... }`. Good.

119. **braceparser/configs.go - TS `interface` mapped to "class"**: For grouping purposes, interface is similar to class. Good.

120. **braceparser/configs.go - PHP `trait` mapped to "class"**: Good.

121. **braceparser/configs.go - Rust `mod`, `struct`, `enum` mapped to "class"**: For grouping, these are similar scopes. Good.

122. **braceparser/configs.go - Rust `match` mapped to "switch"**: Good.

123. **braceparser/configs.go - Rust `loop` mapped to "while"**: Good.

124. **braceparser/configs.go - PHP `foreach` mapped to "for"**: Good.

125. **braceparser/configs.go - PHP `elseif` mapped to "if"**: Reasonable.

126. **braceparser/configs.go - All configs have non-empty keywords**: Tested in `TestConfigs_AllNamed`. Good.

127. **braceparser/configs.go - All configs have name**: Tested. Good.

128. **braceparser/parse_core.go - `matchAnyPrefix` defined but unused**: Dead code.

Now let me look for actual bugs more carefully:

**Bug 1: `isHeredocStart` doesn't guard against bit-shift**

For `a << b` in bash:
- `matchAt(src, i, "<<")` matches at position of first `<`.
- `isHeredocStart(src, i+2)` checks position after `<<`. For `a << b`, position is `b`. `b` is an ident byte. Returns true.
- So we'd enter heredoc mode with tag `b`.

This is a real bug. The comment claims it guards against this but it doesn't.

**Fix**: Check that `<<` is at the start of a line (after optional whitespace) or after a command separator (`;`, `|`, `&`, `(`, `)`, `\n`).

Wait, but the heredoc state is only entered after `\n`. So even if we misidentify `<<` in the middle of a line, the state isn't entered until `\n`. So the actual heredoc body doesn't start until the next line. Let me re-read.

Looking at the code:
```go
case cfg.heredocs && matchAt(src, i, cfg.heredocOp) && isHeredocStart(src, i+len(cfg.heredocOp)):
    tag, strip, consumed := parseHeredoc(src, i+len(cfg.heredocOp))
    heredocTag = tag
    heredocStrip = strip
    heredocPending = true
    i += len(cfg.heredocOp) + consumed - 1
```

So when `<<` is matched with valid tag, we set `heredocPending = true`. The state transition to `stHeredoc` happens at `\n`:
```go
if c == '\n' {
    line++
    lineStart = i + 1
    if heredocPending {
        heredocPending = false
        state = stHeredoc
    }
}
```

So if we see `a << b\n`, the heredocPending becomes true at `<<`. Then at `\n`, state transitions to stHeredoc. The next lines would be in heredoc mode. The tag is `b`.

If the next line is `c\n`, we'd check `heredocLineMatches("c", "b", false)`. `c` != `b`, doesn't start with `b`. Returns false. So we stay in stHeredoc. Continue scanning.

If we never find a line that matches `b`, the heredoc swallows everything to end of source. That's a real problem.

But in practice, `a << b` is a bit-shift operation. The next line would be more code, not the tag `b`. So the heredoc would swallow everything until end of source or until a line that happens to be just `b`.

This is a real correctness issue. Let me flag it.

Actually wait, let me check the bash corpus cases. The corpus uses `cat <<EOF`, `cat <<-EOF`, etc. These are correct. The bug doesn't manifest in the corpus. But it's still a latent issue.

The fix: in `isHeredocStart`, also check that the position before `<<` is at a word boundary or line start.

Actually, a simpler fix: check that `<<` is at the start of a line (or after a command separator). For `cat <<EOF`, `<<` is after `cat ` which is at the start of the line. For `cmd | <<EOF`, the `|` separator. For `cmd <<<word`, the third `<` is the start of the here-string.

Wait, the start of a line check would work for typical cases. But `cmd | <<EOF` (rare but possible) would fail.

Better fix: check that the byte before `<<` is a word boundary (whitespace, semicolon, pipe, etc.) AND that we're not in the middle of an expression.

Or: check that the position before `<<` is at the start of a line or after a pipe/redirect.

For now, the simplest fix is to require `<<` to be preceded by whitespace, `|`, `>`, `<`, `(`, `)`, or `\n` (start of line). This matches typical shell heredoc usage.

Let me think about whether this is a real bug or just an edge case.

In real bash code, `a << b` (without newline) is a bit-shift. But the parser would set `heredocPending=true` and not transition to stHeredoc until `\n`. So the heredoc body doesn't actually start until after the newline. If `a << b` is the only content on a line (unusual but possible), then we'd start a heredoc on the next line.

Wait, `a << b\n` - the `<<` is in the middle of the line. After `\n`, we'd enter stHeredoc. The body would be everything from after the `\n`. If there's a line that equals `b` alone, it'd terminate the heredoc.

This is rare in practice but a real correctness issue.

Let me decide whether to flag this. The sprint plan says: "Track string/comment/heredoc state in the scanner (as the 13.1 Python plugin does for triple-quoted strings); on misparse the finding falls back to line proximity — grouping degrades, never breaks. Corpus cases cover string/heredoc edge cases per language."

So misparse is acceptable as a fallback. But the comment in the code claims to guard against this specific case, and it doesn't. That's a documentation/incorrect-comment issue.

Let me also check `parseHeredoc` - it doesn't validate the tag is non-empty. If tag is empty (just spaces), `heredocLineMatches` returns false (because `tag == ""` check at top). So an empty tag would cause infinite heredoc. Edge case but minor.

**Bug 2: `matchAnyPrefix` unused function**

Dead code. Should be removed.

**Bug 3: `parseHeredoc` tag can be empty**

If the source has `<<` followed by only whitespace or non-ident chars, the tag could be empty. The `heredocLineMatches` would never match, so the heredoc swallows everything to end of source. Minor edge case.

**Bug 4: Documentation mismatch with acceptance criteria**

The sprint plan says "Build FOUR separate .wasm binaries" but AC1 says "A single shared brace-block parser plugin parses all four languages, parameterized by a per-language keyword/naming table (no separate parser binary per language)."

The clarifications say: "AC1 says 'A single shared brace-block parser plugin parses all four languages' but the diff creates 4 separate .wasm binaries. This contradicts AC1 but the clarifications explicitly noted: 'Single shared binary / ~3–4 MB appears only in the Proposed Solution / NFR sections, not the binding Technical Constraints; it is consciously relaxed.'"

So this is acknowledged. The acceptance criteria should probably be updated but the sprint explicitly noted this relaxation. Not really a code bug.

**Bug 5: `braceparser/go.mod` uses `go 1.26`**

This is a future Go version. If the project's CI uses Go 1.23 or 1.24, this might fail. But this is the project's choice and likely matches their Go version.

Actually wait, looking at the existing code in the repo - I don't have access to see what Go version the project uses. But using `go 1.26` in go.mod is unusual and might cause issues if not all modules use the same version.

Hmm, but the braceparser is a separate nested module. It just needs to be buildable with `go 1.26`. If the project's CI doesn't have Go 1.26, builds would fail.

This is a potential issue but minor.

**Bug 6: Build script array expansion idiom**

`${tagflag[@]+"${tagflag[@]}"}` is a bash idiom for handling empty arrays. The intent is: if the array is set (even empty), expand to `"${tagflag[@]}"`. If the array is unset, expand to nothing.

In this case, `tagflag=()` is always set (initialized in `build()`). So `${tagflag[@]+...}` always expands to the array content (which may be empty).

If tags is empty, `tagflag=()`. The expansion produces nothing. So command is `go build -buildmode=c-shared -trimpath -o output .`. Fine.

If tags is "bash", `tagflag=(-tags bash)`. The expansion produces `-tags bash`. Command is `go build -buildmode=c-shared -trimpath -tags bash -o output .`. Fine.

The idiom works but is unusual. Could be simplified.

**Bug 7: `parseHeredoc` consumed bytes**

For `<<<EOT`, the consumed is `len("EOT") = 3`. Plus the operator `<<<` is 3. So `i += 3 + 3 - 1 = 5`. After loop `i++`, `i = 6`. So we skip past `<<<EOT`. The next byte (after `<<<EOT`) is processed. Good.

For `<<-EOT`, the operator `<<` is 2. consumed is `len("EOT") + 1 (for the "-") = 4`. So `i += 2 + 4 - 1 = 5`. After `i++`, `i = 6`. Good.

For `<<EOF` (no `-`), operator `<<` is 2. consumed is `len("EOF") = 3`. So `i += 2 + 3 - 1 = 4`. After `i++`, `i = 5`. Good.

But what about `<<<"EOT"` (quoted PHP heredoc)? Operator `<<<` is 3. consumed in `parseHeredoc` would be: skip spaces (none), check `-`/`~` (no), check `'`/`"` (yes, consume 1), then read ident `EOT`. So consumed = 1 (quote) + 3 (EOT) = 4. Then `i += 3 + 4 - 1 = 6`. After `i++`, `i = 7`. Good.

**Bug 8: `embed.go` - LanguageForExt might miss uppercase extensions**

The function comment says "lower-cased" but doesn't actually lowercase the input. If a file has `.TS`, it wouldn't match. But the comment says the input should already be lowercased. This is consistent with the original `LanguageForExt` behavior.

**Bug 9: Test for empty extension**

The test checks `LanguageForExt("")` returns `""`. Good.

**Bug 10: The diff changes existing tests in host_test.go**

The change to `LanguageForExt(".rb")` comment is fine. The new tests for brace languages are good.

**Bug 11: TestHost_BraceParsersLoadAndParse doesn't check error cases**

The test only checks happy path. But the test is verifying that the .wasm files load and parse. Good.

**Bug 12: corpus.json extension - need to check format**

The new entries look consistent with existing format. Good.

**Bug 13: Build script doesn't clean up**

`build.sh` doesn't have a clean step. If a build fails midway, partial binaries might remain. Minor.

**Bug 14: Build script doesn't validate tags**

If a user runs `build.sh` with an invalid tag, it would build but produce a binary with no language table. Actually, the `active` variable would default to zero value. So `parseSource` would receive an empty langConfig. This would likely produce wrong results but not crash.

Actually, the `active_*.go` files have build tags matching the tag. If a wrong tag is used, none of the active files would be built, and `active` would be undefined. This would cause a compile error. So invalid tags are caught at build time.

**Bug 15: Build script doesn't check Go version**

If Go doesn't support `go 1.26`, the build fails. Minor.

**Bug 16: Configuration mismatches between active files and configs**

Let me check `active_ts.go` references `tsConfig`, `active_php.go` references `phpConfig`, etc. All consistent. Good.

**Bug 17: braceparser/configs.go - `strChars` for PHP doesn't include heredoc-style interpolation**

PHP heredocs can interpolate. The body of heredoc is in stHeredoc which doesn't track braces. So `$var` and `{$var}` inside heredoc don't affect brace counting. For `$var`, no braces. For `{$var}`, the `{` and `}` are inside stHeredoc (ignored). Good.

**Bug 18: braceparser/parse_core.go - `cfg.lineComments` iteration**

If `cfg.lineComments` contains both `//` and `#` (PHP), we check each. For `//`, matchAt succeeds. For `#`, matchAt would check if `#` matches at position i. If we're at `//`, position 0 is `/`, position 1 is `/`, neither matches `#`. But the loop checks each prefix.

Wait, `matchAt(src, i, m)` for `m = "#"` at position 0 where `src[0] = '/'` returns false. So we don't match `#`. Then we check `//` next, which matches at position 0. Returns true.

But what about `// comment # inside`? At position 0, we match `//`. State becomes stLineComment. We skip to end of line. Good.

What about ` # comment` (space then #)? At position 0 (space), no match for `//` or `#`. So we don't enter comment state. At position 1, we check `#`, matchAt succeeds. State becomes stLineComment. Good.

**Bug 19: braceparser/parse_core.go - `matchAt` for empty `s`**

```go
func matchAt(src []byte, i int, s string) bool {
    if s == "" || i+len(s) > len(src) {
        return false
    }
    return string(src[i:i+len(s)]) == s
}
```

For `s = ""`, returns false immediately. So callers need to check `s != ""` before calling. The callers do check `cfg.blockOpen != ""` before `matchAt(src, i, cfg.blockOpen)`. Good.

**Bug 20: braceparser/main.go - alloc returns 0 for n=0**

```go
func alloc(n int32) int32 {
    if n <= 0 {
        n = 1
    }
    b := make([]byte, n)
    p := int32(uintptr(unsafe.Pointer(&b[0])))
    pins[p] = b
    return p
}
```

For n=0, we allocate 1 byte. So we never return 0 from alloc. The check `if n <= 0 { n = 1 }` ensures this. Good.

But wait, what if `make([]byte, 1)` returns a buffer at address 0? In wasm, address 0 is usually the null page, which is unmapped. So `&b[0]` should never be 0. But just in case, the check is good.

Actually, in Go, `&b[0]` for a slice `b` is the address of the first element. For `make([]byte, 1)`, the slice header has a pointer to the backing array. The pointer is non-zero (Go runtime guarantees this).

**Bug 21: braceparser/parse_core.go - `i++` inside switch vs. for loop**

The `for i := 0; i < len(src); i++` loop increments `i` at each iteration. Inside the switch, we sometimes do `i += N` to skip multiple bytes. The `i++` at the end of the loop body adds 1 more. So if we do `i += 2`, then `i++`, total skip is 3.

For example, in `case cfg.blockOpen != "" && matchAt(src, i, cfg.blockOpen): i += len(cfg.blockOpen) - 1`. Then loop increments `i++`. Total skip = `len(cfg.blockOpen)`. So we skip past the entire block open string. Good.

For `case cfg.rawStrings && c == 'r' && rawStringStart(src, i): i += 1 + rawHashes`. Then `i++`. Total skip = `2 + rawHashes`. So we skip past `r#...#` and the next byte is the `"`. Good.

For heredoc: `i += len(cfg.heredocOp) + consumed - 1`. Then `i++`. Total skip = `len(cfg.heredocOp) + consumed`. Good.

**Bug 22: braceparser/parse_core.go - state transitions on `\n`**

In stLineComment, `\n` transitions to normal. In stBlockComment, no transition on `\n` (just line counting). In stString, no transition on `\n`. In stRawString, no transition on `\n`. In stHeredoc, we check for terminator at `\n`. In stParamExp, no transition on `\n`. In stNormal, normal processing.

So `\n` always increments line counter, and state transitions depend on current state. Good.

**Bug 23: `stParamExp` with newline**

Inside `${...}`, newlines are possible (multiline strings). The state machine ignores `\n` in stParamExp. Line counting is handled by the outer `if c == '\n' { line++ }`. Good.

**Bug 24: `cfg.charLiterals` for non-Rust languages**

Currently only rust has `charLiterals: true`. If a future language enables it incorrectly, the parser would treat `'` specially. But this is intentional design.

**Bug 25: braceparser/parse_core.go - `len(stack) <= 1` check in closeBlock**

```go
if len(stack) <= 1 {
    resetHeader()
    return
}
```

If stack has just the file node, we don't close (because there's nothing to close). We just reset header. Good.

**Bug 26: braceparser/parse_core.go - `addHeader` for tabs**

```go
if b == '\n' || b == '\r' || b == '\t' {
    b = ' '
}
```

Tabs are replaced with spaces in header. So `function\tf()` becomes `function f()`. Good.

**Bug 27: braceparser/parse_core.go - `headerLine` for blocks that span newlines**

For:
```
function
f()
{
```

Header accumulates `function\nf()`. The `\n` becomes space. So header is `function f()`. `headerLine` is set to the line of the first non-space char in header, which is `function` on line 1. So the func block starts at line 1. Good.

For:
```
function f()
{
  body
}
```

Header is `function f()`. `headerLine = 1`. Func block starts at line 1. EndLine is line of closing brace (4). Good.

**Bug 28: braceparser/parse_core.go - `resetHeader` for keyword on previous line**

For:
```
let x = 1
function f() {
  body
}
```

Header accumulates `let x = 1 function f() `. The `;` after `let x = 1` doesn't reset header because... wait, there's no `;`. The `=` is not `;`. So the header accumulates everything. classifyHeader finds `function` as the last keyword. Good.

Actually, the `;` rule: `if parenDepth == 0 { resetHeader() }`. So `;` ends the statement. Without `;`, the header continues.

In real JS/TS, ASI (automatic semicolon insertion) means `let x = 1\nfunction f()` is two statements. But the parser doesn't model ASI. It just accumulates.

For `let x = 1; function f()`, the `;` resets header. Then `function f()` accumulates. Good.

For `let x = 1\nfunction f()` (no `;`), header accumulates `let x = 1 function f() `. classifyHeader finds `function`. Returns "func", "f". Good - even though logically these are two statements, the parser correctly identifies the function.

**Bug 29: braceparser/parse_core.go - `header` allocation per byte**

The header slice grows by appending each non-skipped byte. For a large file, this could be expensive. But the header is reset on `;`, `{`, `}`, etc. So it stays bounded.

**Bug 30: braceparser/parse_core.go - `node` struct size**

The `node` struct has Kind, Name, StartLine, EndLine, Children. Each child allocates a new slice. For deep trees, this could be expensive. But for a heuristic parser, it's acceptable.

OK I think I have enough analysis. Let me also check the embed.go change for the comment about `parsers/go.wasm parsers/python.wasm` being added. Actually the original line is `//go:embed parsers/go.wasm parsers/python.wasm` and the new line adds `//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm`. So both `//go:embed` directives are present, which is the correct way to embed multiple files. Good.

Wait actually, looking more carefully, the diff shows:
```
 //go:embed parsers/go.wasm parsers/python.wasm
+//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
 var parserFS embed.FS
```

So we have two separate `//go:embed` directives. Go's `embed` package allows multiple directives to contribute to the same FS variable. This is the correct way to do it. Good.

Let me now think about the most important issues:

1. **Unguarded heredoc detection (bit-shift false positive)**: This is a real correctness issue. In bash, `a << b` (bit-shift) would be misinterpreted as a heredoc with tag `b`. The comment in `isHeredocStart` claims to guard against this but doesn't.

2. **Dead code: `matchAnyPrefix`**: Unused function. Should be removed.

3. **`braceparser/go.mod` uses `go 1.26`**: Future Go version. Might cause CI issues.

Let me focus on these.

Actually, let me re-check issue 1 more carefully. The `isHeredocStart` function checks the chars after `<<`. The comment says "Guards against treating a bit-shift `a << b` as a heredoc." But the function only checks if a tag plausibly starts after `<<`. It doesn't check the chars BEFORE `<<`.

For `a << b`:
- Before `<<`: `a` (ident byte)
- After `<<`: ` b` (space then `b`)

The function only looks after `<<`. It finds `b` as ident byte. Returns true.

To fix, we'd need to check that the position before `<<` is at a word boundary (start of line, or after space/`;`/`|`/`&`/etc.).

This is a real bug, but it might not manifest in practice because:
1. Bit-shift in bash is unusual in shell scripts.
2. Even if triggered, the "misparse falls back to line proximity" - but actually no, the misparse would cause the heredoc to swallow content, which might not fall back gracefully.

Wait, let me think about the failure mode. If `a << b` is misinterpreted as heredoc with tag `b`, then:
1. The parser enters stHeredoc at the next newline.
2. The body is everything until a line matching `b`.
3. If no such line exists, the heredoc swallows to end of source.
4. All code after `a << b` is hidden inside the heredoc.
5. Block structure is lost.

This is a real bug. Let me flag it.

Hmm, but `isHeredocStart` is called for both bash and PHP. For PHP, the heredocOp is `<<<` (3 chars). For `a << b`, `matchAt(src, i, "<<<")` would fail because there's only `<<`. So PHP isn't affected. Only bash with `<<` is affected.

Actually wait, for `a <<< b`, that's here-string in bash. `matchAt(src, i, "<<")` matches `<<`. Then `isHeredocStart(src, i+2)` checks position of third `<`. `<` is not an ident byte. So returns false. So `<<<` (here-string) is correctly NOT treated as heredoc.

But for `a << b` (bit-shift), the same `<<` matches, and `isHeredocStart(src, i+2)` checks position after `<<`, which is ` b` (space then `b`). The function skips leading spaces, finds `b` as ident byte. Returns true.

So `a << b` would be misinterpreted as `<<b` heredoc. Yes, this is a bug.

Now, how likely is this in real bash code? `<<` is the bit-shift operator. `a << b` shifts `a` left by `b` bits. This is rare in shell scripts but possible in arithmetic contexts like `$(( a << b ))`. In that case, the `<<` is inside `$((...))`. The `$` before `((` would not be processed by `isHeredocStart` (which only checks after `<<`).

Actually, `$(( a << b ))` - the `<<` is inside `$((...))`. The parser doesn't track `$((...))` specially. So when it sees `<<` after `$(( a `, it would trigger the heredoc check. Hmm.

But `$(( ... ))` is arithmetic expansion. The parser doesn't have special handling for it. So `<<` inside `$((...))` would be misinterpreted as a heredoc.

This is a real bug.

Let me also check `$(( a << 1 ))`:
- `(` increments parenDepth. Header accumulates `$(( a `.
- ` ` then `<<` - heredoc check fires.
- `isHeredocStart` checks after `<<`. Position has ` 1 ))`. Skip spaces, find `1`. `1` is ident byte. Returns true.
- So we set heredocPending = true, tag = `1`, etc.
- On next `\n`, state = stHeredoc.
- All subsequent code is in heredoc mode until a line of just `1`.

This is a real bug that would cause incorrect parsing for any bash code using arithmetic with shifts.

Let me flag this.

Also, let me think about whether `matchAt` is correctly called. For `$(( a << 1 ))`, the chars are `$`, `(`, `(`, ` `, `a`, ` `, `<`, `<`, ` `, `1`, ` `, `)`, `)`. At position of first `<` (index 6), `matchAt(src, 6, "<<")` matches. Then `isHeredocStart(src, 8)` checks position 8 which is ` 1 ))`. Function skips spaces, finds `1`. Returns true.

So yes, this is a real bug.

Now let me also check: in `classifyHeader`, when arrowFunc is true and `=>` is found, returns "func". But what if `=>` is inside an arithmetic context? Doesn't matter for classification.

OK let me finalize my findings.

Let me also double-check the unused `matchAnyPrefix` function:

```go
func matchAnyPrefix(src []byte, i int, prefixes []string) bool {
    for _, p := range prefixes {
        if matchAt(src, i, p) {
            return true
        }
    }
    return false
}
```

Searching the file... I don't see it called anywhere. Dead code.

Let me check the `parse_core.go` file once more for `matchAnyPrefix` usage:

The function is defined. Searching the rest of the file: not used.

OK so it's dead code.

Let me also check if there are any issues with the test corpus:

The corpus has 27 entries. The previous corpus had ~22 entries (based on the line numbers). The new entries are for brace languages.

Let me check the format consistency: each entry has `name`, `file`, `source`, `a_line`, `b_line`, `same_block`. All new entries follow this format. Good.

Wait, I notice the existing corpus had a trailing entry without comma. Let me check:
```
    "a_line": 5,
    "b_line": 8,
    "same_block": false
+  },
```

So the original last entry ended with `}` (no comma). The diff adds `,` after the closing brace and adds new entries. Good.

OK let me also check the build script more carefully:

```bash
build() {
  local name="$1" srcdir="$2" tags="${3:-}"
  local tagflag=()
  [ -n "${tags}" ] && tagflag=(-tags "${tags}")
  echo "building ${name}.wasm from ${srcdir}${tags:+ (tags: ${tags})}"
  ( cd "${here}/src/${srcdir}" && \
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath \
      ${tagflag[@]+"${tagflag[@]}"} -o "${here}/${name}.wasm" . )
  echo "  -> $(wc -c < "${here}/${name}.wasm") bytes"
}
```

The `${tagflag[@]+"${tagflag[@]}"}` is a bash idiom. Let me verify it's correct.

`tagflag=()` when tags is empty. `${tagflag[@]+...}` with `+` means "if tagflag is set (even empty), expand to ...". The inner `"${tagflag[@]}"` expands to nothing (empty array). So the whole thing expands to nothing.

`tagflag=(-tags bash)` when tags is "bash". `${tagflag[@]+...}` expands to `"${tagflag[@]}"` which is `-tags bash` (as a single quoted string).

So the command becomes:
- No tags: `go build -buildmode=c-shared -trimpath -o output .`
- With tags: `go build -buildmode=c-shared -trimpath -tags bash -o output .`

Looks correct.

But wait, there's a potential issue with the `${tagflag[@]+"${tagflag[@]}"}` syntax. Let me think about this in the context of being inside `${...}`.

Actually, the entire expression is `${tagflag[@]+"${tagflag[@]}"}`. The outer `${...}` is parameter expansion. The `+` indicates "if set, use this". The `"${tagflag[@]}"` is the expansion value.

This is a valid bash idiom for handling possibly-empty arrays. It works.

OK I think my final findings are:

1. **HIGH/MEDIUM**: `isHeredocStart` doesn't guard against bit-shift false positive. Real bug.

2. **LOW**: `matchAnyPrefix` is dead code.

3. **LOW**: `braceparser/go.mod` uses `go 1.26` which might not be available.

Let me also think about whether there's an issue with how `parenDepth` is used.

For:
```
if (x) {
  body
}
```

Header is `if (x) `. parenDepth goes 0 -> 1 -> 0 as we process `(`, `x`, `)`. Then `{` opens block. Good.

For:
```
function f(x) {
  body
}
```

Header is `function f(x) `. parenDepth goes 0 -> 1 -> 0. Good.

For:
```
if ((x > 0)) {
  body
}
```

In bash, `((` is arithmetic. Header accumulates `if ((x > 0`. parenDepth goes 0 -> 1 -> 2 (for `((`) -> 1 -> 0 (for `))`). Then `{`. Good.

For:
```
a << b
```

Header accumulates `a ` then sees `<<`. Heredoc check fires. Bug.

But wait, for `if ((x > 0)) { body }`:
- See `i`, add to header.
- See `f`, add to header.
- See ` `, add to header.
- See `(`, parenDepth=1, add to header.
- See `(`, parenDepth=2, add to header.
- See `x`, add to header.
- See ` `, add to header.
- See `>`, add to header.
- See ` `, add to header.
- See `0`, add to header.
- See `)`, parenDepth=1, add to header.
- See `)`, parenDepth=0, add to header.
- See ` `, add to header.
- See `{`, openBlock. classifyHeader finds `if` keyword. Returns "if".

Good. The arithmetic context is handled correctly because `parenDepth` is tracked.

For `a << b` (bit-shift):
- See `a`, add to header.
- See ` `, add to header.
- See `<`, matches `<<` start.
- See `<`, part of `<<` match.
- heredocOp is `<<` (2 chars). Matched.
- isHeredocStart checks after `<<`. Position has ` b`. Skip spaces, find `b`. Returns true.
- Set heredocPending=true, tag=`b`.
- Continue scanning. See `b`, add to header.
- See `\n`, line++, state transitions to stHeredoc.

So `a << b` triggers heredoc mode. Bug.

Wait, but in bash, `a << b` is only valid inside `$((...))`. The parser doesn't track `$((...))`. So the bug manifests.

Actually, in bash, `a << b` outside of arithmetic would be parsed as a heredoc. The shell interprets `a << b` as: run `a` with stdin from heredoc with tag `b`. So in shell semantics, `a << b` IS a heredoc!

But in a script body (not as a command), `a << b` would be a syntax error. Unless inside `$((...))`.

So in practice, `a << b` outside `$((...)` is invalid shell, and the parser correctly interprets it as heredoc.

But inside `$((...))`, it's arithmetic. The parser doesn't track `$((...))` so it can't distinguish.

This is an edge case. The comment claims to guard against it but doesn't. Real bug but rare.

OK let me finalize:

Final findings:
1. `isHeredocStart` doesn't actually guard against bit-shift false positive (HIGH or MEDIUM correctness).
2. `matchAnyPrefix` is unused dead code (LOW maintainability).
3. `braceparser/go.mod` uses `go 1.26` (LOW correctness/portability).

Wait, let me also think about whether there are any test coverage issues.

The test `TestBashConfig_DollarHashNotComment` tests `$#` not being a comment. But there's no test for `<<` bit-shift vs heredoc. So this bug isn't caught by tests.

Also, no test for `$(( a << b ))` arithmetic with bit-shift. Would be good to have.

OK, also let me check if there are any issues with the corpus JSON. Looking at:

```json
{
  "name": "bash_heredoc_no_overmerge",
  "file": "a.sh",
  "source": "greet() {\n  cat <<EOF\n  { not a block }\nEOF\n  echo done\n}\npost() {\n  echo hi\n}\n",
  "a_line": 5,
  "b_line": 8,
  "same_block": false
}
```

This tests that heredoc braces don't create blocks. a_line=5 is `echo done`, b_line=8 is `echo hi`. They're in different functions, so should be different blocks. Good.

Looking at `rust_impl_method_grouping`:
```json
{
  "name": "rust_impl_method_grouping",
  "file": "a.rs",
  "source": "impl Foo {\n    fn bar(&self) {\n        let a = 1;\n        let b = 2;\n        a + b\n    }\n}\n",
  "a_line": 3,
  "b_line": 5,
  "same_block": true
}
```

a_line=3 is `let a = 1;` inside `bar`. b_line=5 is `a + b` also inside `bar`. Same block. Good.

OK let me also check the embed.go file for any issues:

```go
//go:embed parsers/go.wasm parsers/python.wasm
//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
var parserFS embed.FS
```

Two separate embed directives. Go's embed package allows this. The resulting FS contains all 6 .wasm files. Good.

```go
var builtinParsers = map[string]string{
    "go":     "parsers/go.wasm",
    "python": "parsers/python.wasm",
    "ts":     "parsers/ts.wasm",
    "php":    "parsers/php.wasm",
    "rust":   "parsers/rust.wasm",
    "bash":   "parsers/bash.wasm",
}
```

Maps language ids to .wasm paths. Good.

```go
func LanguageForExt(ext string) string {
    switch ext {
    case ".go":
        return "go"
    case ".py":
        return "python"
    case ".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".mjs":
        return "ts"
    case ".php":
        return "php"
    case ".rs":
        return "rust"
    case ".sh", ".bash":
        return "bash"
    default:
        return ""
    }
}
```

Maps extensions to language ids. Good.

OK I think I've covered the main issues. Let me also check the braceparser/go.mod one more time:

```go
module github.com/samestrin/atcr/internal/astgroup/parsers/src/braceparser

go 1.26
```

The module path matches the directory structure. The `go 1.26` directive sets the minimum Go version. If the project's CI doesn't have Go 1.26, builds would fail.

Hmm, but this is the project's choice. It might be intentional.

Actually, I realize this is a real concern. If the project's main go.mod uses `go 1.23` (or similar), having a nested module with `go 1.26` might cause issues. Go modules can have different `go` directives, but the actual Go toolchain needs to support both.

If Go 1.26 doesn't exist yet (which I'm not 100% sure about given my knowledge cutoff), this would be a build failure.

Let me flag this as a potential issue.

Actually, given the merge commit date is `7d73422ee21705066eb840665b3cc272a16ba0da`, and the sprint plan was finalized in 2026-06-28, it's plausible that Go 1.26 was released by Feb 2026 (Go typically releases in Feb and Aug). So Go 1.26 should be available.

But this is still a portability concern. If the project supports older Go versions for builds, this would be a regression.

Let me also check the existing goparser and pyparser go.mod files... they're not in the diff. But presumably they have similar `go` directives. If they use `go 1.23` or earlier, the new `go 1.26` would be inconsistent.

Without more info, I'll flag this as a LOW issue.

OK let me finalize my findings:

1. **MEDIUM**: `isHeredocStart` doesn't actually guard against bit-shift false positive.
2. **LOW**: `matchAnyPrefix` is unused dead code.
3. **LOW**: `braceparser/go.mod` uses `go 1.26` which may not be available in all build environments.

Actually, let me reconsider #1. Is it really a bug?

In bash:
- `cmd <<EOF\nbody\nEOF` - heredoc
- `a << b` - bit-shift (only inside `$((...))`)
- `cmd | <<EOF` - heredoc (after pipe)

The parser would treat `a << b` as a heredoc with tag `b`. If `b` happens to be a line in the source, the heredoc terminates there. Otherwise, it swallows everything to end of file.

This is a real issue. But how common is `$(( a << b ))` in shell scripts? Probably uncommon. And the failure mode (swallowing code) would be caught by tests or manual inspection.

The comment in the code claims to guard against this:
> Guards against treating a bit-shift `a << b` as a heredoc.

But it doesn't. This is misleading documentation plus a real bug.

Severity: MEDIUM (correctness issue, but mitigated by infrequency and graceful degradation).

OK final findings:

1. **MEDIUM**: `isHeredocStart` doesn't guard against bit-shift false positive. The comment is misleading.
2. **LOW**: `matchAnyPrefix` is unused dead code.
3. **LOW**: `braceparser/go.mod` uses `go 1.26`.

Wait, I want to double-check #2 by searching for `matchAnyPrefix` in the braceparser source.

The function is defined in parse_core.go. Let me search for its usage:
- `matchAnyPrefix(` - I need to find where it's called.

Looking at parse_core.go... I don't see any call to `matchAnyPrefix`. It's defined but not used.

This is dead code. LOW maintainability issue.

OK let me also consider: are there any other issues I might have missed?

Let me check the braceparser/main.go file once more:

```go
//go:wasmexport alloc
func alloc(n int32) int32 {
    if n <= 0 {
        n = 1
    }
    b := make([]byte, n)
    p := int32(uintptr(unsafe.Pointer(&b[0])))
    pins[p] = b
    return p
}
```

`unsafe.Pointer(&b[0])` - this assumes the pointer fits in int32. In wasm32, addresses are 32-bit, so int32 is sufficient. Good.

```go
//go:wasmexport free
func free(p int32) { delete(pins, p) }
```

Removes the pin. Good.

```go
//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
    buf, ok := pins[ptr]
    if !ok || int(n) > len(buf) {
        return emit(node{Kind: "error", Name: "bad pointer"})
    }
    src := buf[:n]

    if len(src) == 0 {
        return emit(node{Kind: "file", StartLine: 1, EndLine: 1})
    }
    return emit(parseSource(src, active))
}
```

Looks correct.

```go
func emit(n node) int64 {
    b, err := json.Marshal(n)
    if err != nil {
        b = []byte(`{"kind":"error","name":"marshal"}`)
    }
    p := alloc(int32(len(b)))
    copy(pins[p], b)
    return (int64(p) << 32) | int64(len(b))
}
```

If `len(b) == 0`, `alloc(0)` is called, which allocates 1 byte. Then `copy(pins[p], b)` copies 0 bytes (since `b` is empty). OK.

Wait, `int32(len(b))` for `len(b) == 0` is `0`. `alloc(0)` sets `n = 1` and allocates 1 byte. `copy(pins[p], b)` copies 0 bytes (since `b` is empty). Returns `(int64(p) << 32) | 0`. The length is 0.

Hmm, but the buffer at `p` is 1 byte, allocated. So we have a 1-byte buffer with no useful content, but the pointer-length says length 0. The host would read 0 bytes. OK.

But wait, the pin map has an entry for `p` with a 1-byte buffer. That's a small memory leak (1 byte per empty emit). But emit is called rarely, so it's fine.

Actually, the JSON marshal of an empty node would produce `{}` or similar, not empty. So `len(b)` is unlikely to be 0 in practice.

OK the main.go looks correct.

Let me also check parseSource for any issues with `cfg.paramExpand` and `stParamExp`:

```go
case cfg.paramExpand && c == '$' && i+1 < len(src) && src[i+1] == '{':
    i++ // consume the '{'; depth tracks nested ${...}
    paramDepth = 1
    state = stParamExp
```

Wait, `i++` consumes the `{`. But the loop also has `i++` at the end. So total skip is 2 bytes (`$` and `{`). Then we're in stParamExp, processing the byte after `{`.

But what if `i+1 < len(src)` and `src[i+1] == '{'`? We've already incremented `i` to point to `{`. The loop will `i++` to point past `{`. So we process the byte after `{`.

Hmm, let me re-read. The case is for `$` followed by `{`. We see `$` at position `i`. We `i++` to consume the `{` (so `i` now points to `{`). Then `paramDepth = 1`. Then state = stParamExp. Then the outer `if c == '\n'` block runs (we're at `{`, not `\n`). Then the loop `i++` increments to the next byte.

Wait, but `i++` is in the `for` loop header: `for i := 0; i < len(src); i++`. So after this iteration, `i` increments by 1 (total). Plus the manual `i++` inside the case. So total is 2.

So we skip `$` and `{`. The next iteration starts at the byte after `{`. Good.

Then in stParamExp, we count `{` and `}`. When depth reaches 0, exit.

But wait, what if the source is `${${x}}`?
- i=0: `$`, then `{` at i=1. `i++` (now i=1). state=stParamExp, paramDepth=1.
- Loop increments i (now i=2). c=`$`. In stParamExp, no special handling for `$`. Just check `{` and `}`.
- i=3: c=`{`. paramDepth=2.
- i=4: c=`x`. nothing.
- i=5: c=`}`. paramDepth=1.
- i=6: c=`}`. paramDepth=0. state=stNormal.

Good, nested handled.

What about `${{x}}`?
- i=0: `$`, then `{` at i=1. `i++` (now i=1). state=stParamExp, paramDepth=1.
- Loop increments i (now i=2). c=`{`. paramDepth=2.
- i=3: c=`x`. nothing.
- i=4: c=`}`. paramDepth=1.
- i=5: c=`}`. paramDepth=0. state=stNormal.

Good.

What about `${}` (empty)?
- i=0: `$`, then `{` at i=1. `i++` (now i=1). state=stParamExp, paramDepth=1.
- Loop increments i (now i=2). c=`}`. paramDepth=0. state=stNormal.

Good.

What about `${` at end of input (unclosed)?
- i=0: `$`, then `{` at i=1. `i++` (now i=1). state=stParamExp, paramDepth=1.
- Loop increments i (now i=2). c=anything. paramDepth stays 1.
- End of loop. State is stParamExp, never closed.

The end-of-loop cleanup closes blocks but doesn't reset state. The blocks are already closed (no `{` was processed as openBlock). So no harm. Good.

OK the parser logic seems correct (modulo the bit-shift issue).

Let me also check one more thing: the `parenDepth` for `f() {` in bash.
- See `f`, add to header.
- See `(`, parenDepth=1, add to header.
- See `)`, parenDepth=0, add to header.
- See ` `, add to header.
- See `{`, openBlock. classifyHeader finds no keyword, no arrow. funcParen check: trimmed header ends with `)`? Header is `f() `, trimmed is `f()`. Ends with `)`. Index of `(` is 1. name = "f". Returns "func", "f". Good.

For TS method `name(args) {`:
- Header accumulates `name(args) `. funcParen check returns "func", "name". Good.

For TS arrow `() => {`:
- Header accumulates `() => `. arrowFunc check finds `=>`. Returns "func", "". Good.

OK all looking good.

Let me also think about one more potential issue: the `headerLine` for blocks where the keyword is on a previous line:

```
function
f() {
  body
}
```

- Line 1: `function`, headerLine=1, header="function"
- Line 2: `f() {`, headerLine=2 (updated when `f` is added), header="function f() "
- See `{`, openBlock. start = line (2) because headerStarted. So func starts at line 2.

Hmm, but the function definition actually starts at line 1. The block starts where the `{` is, which is line 2. So StartLine=2 is technically correct.

But the user might expect StartLine=1 (where the keyword is). This is a minor design decision.

Looking at the code:
```go
openBlock := func() {
    kind, name := classifyHeader(string(header), cfg)
    start := line
    if headerStarted {
        start = headerLine
    }
    stack = append(stack, node{Kind: kind, Name: name, StartLine: start, EndLine: line})
    resetHeader()
}
```

`start = line` if header not started. `start = headerLine` if header started.

For `function\nf() {`, header is started (function keyword on line 1). headerLine=1. So start=1. Func starts at line 1. Good.

For `{` immediately (no header), start=line (current line). Block starts at current line. Good.

OK this is correct.

Wait, let me re-trace `function\nf() {`:
- Line 1, i=0: `f`, headerLine=1, header="function"
- Line 1, i=1-7: `unction`, header="function"
- Line 1, i=8: `\n`. line=2, lineStart=9.
- Line 2, i=9: `f`, headerLine=2 (updated because headerStarted is true), header="function f"
- ... continues
- Line 2, i=11: `(`, parenDepth=1, header="function f("
- Line 2, i=12: `)`, parenDepth=0, header="function f()"
- Line 2, i=13: ` `, header="function f() "
- Line 2, i=14: `{`, openBlock. headerStarted=true, start=headerLine=2. Func starts at line 2.

Hmm, headerLine is updated to 2 when we add `f`. So the block starts at line 2 (where the `{` is). This might not be intuitive but is consistent.

Actually wait, let me re-read:
```go
addHeader := func(b byte) {
    if b == '\n' || b == '\r' || b == '\t' {
        b = ' '
    }
    if b == ' ' && !headerStarted {
        return
    }
    if !headerStarted {
        headerStarted = true
        headerLine = line
    }
    header = append(header, b)
}
```

So `headerLine = line` is set the first time a non-space char is added to header. For `function\nf() {`, the first non-space is `f` on line 1, so headerLine=1. Then for `f` on line 2, headerStarted is already true, so headerLine is NOT updated.

So StartLine=1 for the func block. Good.

OK my analysis is correct. Let me finalize.

One more check: the braceparser source has `package main`. When `go test` is run, Go generates a test binary with a generated `main()` that runs the tests. But `main.go` has `//go:build wasip1`, so it's excluded for normal builds. So the test binary has no user-defined `main()` but Go provides one.

For `go build` (without wasip1), `main.go` is excluded, so there's no `main()`. The build fails. But we don't run `go build` on the braceparser module directly - it's only built via build.sh with `GOOS=wasip1`.

For `go test`, the test framework generates `main()`, so it works.

OK no issue here.

Alright, my final findings:

1. **MEDIUM**: `isHeredocStart` in `braceparser/parse_core.go` doesn't actually guard against bit-shift false positive as the comment claims. This is a correctness issue.

2. **LOW**: `matchAnyPrefix` in `braceparser/parse_core.go` is dead code (defined but unused).

3. **LOW**: `braceparser/go.mod` uses `go 1.26`, which may not be available in all build environments.

Let me write these up.

Actually, let me reconsider #3. Looking at the existing build setup, the project presumably already has Go 1.26 available (since the merge commit is from after Feb 2026). So this might not be an issue in practice. But it's still worth flagging as a portability concern.

Actually, since I don't have access to the existing go.mod files, I can't verify. Let me drop this finding since it's speculative.

Final findings:
1. **MEDIUM**: `isHeredocStart` doesn't guard against bit-shift false positive.
2. **LOW**: `matchAnyPrefix` is dead code.

Hmm wait, let me also reconsider whether to flag #1 as MEDIUM or HIGH.

The bug would cause:
- Heredoc state triggered incorrectly for `a << b` patterns.
- Subsequent code swallowed into heredoc body.
- Block structure lost for that section.

But:
- The bit-shift pattern is uncommon in bash scripts.
- The failure is contained - the parser just loses block info for some sections.
- Grouping degrades to line proximity for affected sections.

The comment in the code explicitly claims to guard against this. So the implementation is wrong relative to its stated intent.

I'd say MEDIUM. It's a real bug but with limited practical impact.

OK let me write up the findings.

Actually, I want to verify my finding about `isHeredocStart` by re-reading the function:

```go
// isHeredocStart reports whether a heredoc tag plausibly begins at src[j] (after
// the heredoc operator): an optional -/~ then optional quote then an identifier
// char. Guards against treating a bit-shift `a << b` as a heredoc.
func isHeredocStart(src []byte, j int) bool {
    for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
        j++
    }
    if j < len(src) && (src[j] == '-' || src[j] == '~') {
        j++
    }
    if j < len(src) && (src[j] == '\'' || src[j] == '"') {
        j++
    }
    return j < len(src) && (isIdentByte(src[j]) && !(src[j] >= '0' && src[j] <= '9'))
}
```

For `a << b`:
- `matchAt(src, i, "<<")` matches at i=2 (where the first `<` is).
- `isHeredocStart(src, i+len(cfg.heredocOp))` = `isHeredocStart(src, 4)`.
- At position 4: ` ` (space). Skip spaces: j=5.
- At position 5: `b`. Not `'` or `"`. Check isIdentByte and not digit. `b` is ident, not digit. Returns true.

So yes, the function returns true for `a << b`. The comment is wrong.

For `a << EOF` (real heredoc):
- `matchAt` matches `<<` at i=2.
- `isHeredocStart(src, 4)`. Position 4 is ` `. Skip spaces: j=5.
- Position 5: `E`. isIdentByte, not digit. Returns true.

So both `a << b` and `a << EOF` pass the check. The function doesn't distinguish.

The fix would be to also check the position before `<<`:
- Require that `src[i-1]` is at a word boundary (start of line, or whitespace, or `;`/`|`/`&`/etc.).

Or simpler: require that `<<` is at the start of a line (after optional whitespace).

For `cat <<EOF`, `<<` is at position 4 (after `cat `). The byte before is ` ` (space). Not start of line.

Hmm, so start-of-line check wouldn't catch `cat <<EOF`. We need a more general "word boundary" check.

OK the bug is real. Let me flag it.

Severity: MEDIUM. The bug causes incorrect block detection for `a << b` patterns (bit-shift, usually inside arithmetic). Misparse falls back to line proximity per the sprint plan, so no data corruption. But the comment claiming to guard against this is misleading.

OK final findings:

1. **MEDIUM**: `isHeredocStart` (braceparser/parse_core.go) doesn't guard against bit-shift false positive despite the comment's claim.

2. **LOW**: `matchAnyPrefix` (braceparser/parse_core.go) is unused dead code.

Let me write up these findings in the required format.

Actually wait, I realize I should look at line numbers more carefully for accurate references.

Looking at braceparser/parse_core.go:
- `isHeredocStart` is at around line 442 (estimating).
- `matchAnyPrefix` is at around line 396.

Without exact line numbers, I'll estimate. The function definitions are roughly:
- `matchAt` ~ line 390
- `matchAnyPrefix` ~ line 396
- `lineCommentStarts` ~ line 402
- `hashAtWordBoundary` ~ line 420
- `rawStringStart` ~ line 432
- `countHashes` ~ line 446
- `hasHashes` ~ line 453
- `charLiteralLen` ~ line 463
- `isHeredocStart` ~ line 484
- `parseHeredoc` ~ line 498
- `heredocLineMatches` ~ line 518

These are estimates based on the function definitions in the diff.

For my findings:
- `isHeredocStart` is at approximately line 484-499 (based on the diff content).
- `matchAnyPrefix` is at approximately line 396-402.

Let me be more precise. The function definition starts with `func isHeredocStart(...)`. Looking at the diff:

```
+// isHeredocStart reports whether a heredoc tag plausibly begins at src[j] (after
+// the heredoc operator): an optional -/~ then optional quote then an identifier
+// char. Guards against treating a bit-shift `a << b` as a heredoc.
+func isHeredocStart(src []byte, j int) bool {
+	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
+		j++
+	}
+	if j < len(src) && (src[j] == '-' || src[j] == '~') {
+		j++
+	}
+	if j < len(src) && (src[j] == '\'' || src[j] == '"') {
+		j++
+	}
+	return j < len(src) && (isIdentByte(src[j]) && !(src[j] >= '0' && src[j] <= '9'))
+}
```

So `isHeredocStart` is at some line in the file. Without the full file content, I can't give exact line number.

For the output format, I'll use approximate line numbers or just the function name.

Actually, looking at the diff more carefully, the parse_core.go file is 522 lines (based on the diff showing `+522` total lines added to the new file). So lines go from 1 to 522.

The function `isHeredocStart` appears around line 484-499 based on its position in the file.
The function `matchAnyPrefix` appears around line 396-402.

Let me estimate more carefully. The file has these functions in order:
1. Package doc comment
2. Imports
3. `node` struct (~line 25)
4. `blockKeyword` struct (~line 33)
5. `langConfig` struct (~line 40)
6. State constants (~line 60)
7. `parseSource` (~line 72)
8. `classifyHeader` (~line 220)
9. `isIdentByte` (~line 240)
10. `lastWholeWord` (~line 246)
11. `identAfter` (~line 265)
12. `funcParenName` (~line 278)
13. `matchAt` (~line 296)
14. `matchAnyPrefix` (~line 304)
15. `lineCommentStarts` (~line 313)
16. `hashAtWordBoundary` (~line 333)
17. `rawStringStart` (~line 346)
18. `countHashes` (~line 358)
19. `hasHashes` (~line 365)
20. `charLiteralLen` (~line 376)
21. `isHeredocStart` (~line 397)
22. `parseHeredoc` (~line 411)
23. `heredocLineMatches` (~line 432)

These are rough estimates.

Without exact line numbers, I'll use approximate line numbers. Or I can just refer to the function name.

For the output format, FILE:LINE is required. Let me use approximate line numbers based on my best estimate.

Actually, looking at the original diff content for parse_core.go, I can count the `+` lines to get more accurate positions. Let me try:

The file starts at the top with package comment. Let me count sections:

Lines 1-13: Package doc
Lines 15: imports
Lines 17-25: node struct
Lines 27-32: blockKeyword struct
Lines 34-52: langConfig struct
Lines 54-61: state constants
Lines 63-220: parseSource function
Lines 222-254: classifyHeader
Lines 256-258: isIdentByte
Lines 260-282: lastWholeWord
Lines 284-294: identAfter
Lines 296-317: funcParenName
Lines 319-326: matchAt
Lines 328-336: matchAnyPrefix
Lines 338-360: lineCommentStarts
Lines 362-375: hashAtWordBoundary
Lines 377-392: rawStringStart
Lines 394-401: countHashes
Lines 403-412: hasHashes
Lines 414-435: charLiteralLen
Lines 437-453: isHeredocStart
Lines 455-478: parseHeredoc
Lines 480-508: heredocLineMatches

OK rough estimates. Let me just use approximate line numbers.

Actually I realize I'm not going to get this exactly right without running the code. Let me just use line ranges or function names as evidence.

Looking at the diff, the parse_core.go file is 522 lines. The functions I care about are in the second half:
- `isHeredocStart` is around line 437-453 (based on my estimate).
- `matchAnyPrefix` is around line 328-336.

Let me use these in my output.

Actually, I realize for accuracy, I should probably just reference the function name in the evidence, since the line numbers are estimates.

Let me finalize my output:

1. MEDIUM|braceparser/parse_core.go:440|isHeredocStart doesn't actually guard against bit-shift false positive|Add check for word boundary before <<|correctness|15|Comment claims to guard against a << b but only checks after <<|reviewer

2. LOW|braceparser/parse_core.go:330|matchAnyPrefix is unused dead code|Remove the function|maintainability|5|Defined but never called in the package|reviewer

Hmm, let me reconsider the reviewer name. The system prompt says "Your agent name". Let me use a sensible name like "code-reviewer".

Actually, looking at the example: `HIGH|src/auth.go:42|Missing input validation|Add length check|security|5|user input passed directly to query|bruce`

So the reviewer name is just a name. Let me use "code-reviewer".

OK final output:

1. MEDIUM|braceparser/parse_core.go:437|isHeredocStart doesn't actually guard against bit-shift false positive|Add check for word boundary before <<|correctness|15|Comment claims guard but only checks chars after <<|code-reviewer

2. LOW|braceparser/parse_core.go:328|matchAnyPrefix is unused dead code|Remove the function|maintainability|5|Defined but never called anywhere in the package|code-reviewer

Wait, I want to double-check the bit-shift issue once more. Let me think about this more carefully.

For bash:
```bash
x=$(( 1 << 3 ))
echo $x
```

The `<<` is inside `$((...))`. The parser doesn't track `$((...))`. So:
- `$` is processed normally.
- `(` increments parenDepth.
- `(` increments parenDepth.
- ` 1 ` is added to header.
- `<<` matches heredocOp. `isHeredocStart` checks after `<<`. Position is ` 3 ))`. Skip spaces, find `3`. `3` is ident byte. Returns true.
- Set heredocPending=true, tag=`3`.
- Continue. See `3`, add to header. See ` `, add to header. See `)`, parenDepth=1, add to header. See `)`, parenDepth=0, add to header.
- See `\n` at end of line. line++, state=stHeredoc.
- Now in heredoc mode. The next line is `echo $x`. Not equal to `3`. Doesn't start with `3`. Continue in heredoc.
- End of input. Heredoc never closes.

So the entire file would be consumed by the heredoc. Block structure lost.

This is a real bug.

The fix would be to check the position before `<<`:
- It should be a word boundary (whitespace, `;`, `|`, `&`, `(`, `)`, newline, start of file).

For `$(( 1 << 3 ))`, position before `<<` is ` ` (space). That's a word boundary. So the bit-shift check would still pass.

Hmm, that's not quite right. The `<<` inside `$((...))` is preceded by space. But we want to exclude it because it's inside arithmetic.

So just checking word boundary before `<<` isn't enough. We need to also track `$((...))` context.

Or we could check: if `<<` is inside `$(...)` (command substitution) or `$((...))` (arithmetic), exclude it.

But this adds complexity. The simpler fix: require `<<` to be at the start of a line (after optional whitespace). This would catch most real heredocs (`cat <<EOF`, `cmd | cat <<EOF` would fail though).

Hmm. Actually, for typical bash heredoc usage:
- `cat <<EOF` - `<<` after `cat ` (space before, not start of line)
- `cat <<-EOF` - same
- `cmd | cat <<EOF` - `<<` after `cat ` (pipe then space then cat then space)
- `cat <<EOF\nbody\nEOF` - `<<` after `cat `

So requiring start-of-line would miss most cases. We need a more nuanced check.

One option: check that `<<` is preceded by a "command position" - i.e., the parser is at a position where a command could start. This is hard to determine heuristically.

Another option: accept the bug and document it. The sprint plan says "Heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs, producing spurious blocks" is a known risk with Low impact (misparse falls back to proximity).

Wait, the bit-shift issue isn't really about "heredoc body" - it's about misinterpreting `<<` as a heredoc opener. That's a different bug class.

But the impact is similar: misparse causes incorrect block detection, falls back to proximity. No data corruption.

Hmm, let me reconsider the severity. The bug is real but the impact is contained. I'd say MEDIUM.

OK final findings:

1. MEDIUM: bit-shift issue.
2. LOW: dead code.

Let me also think about whether there are any issues with the build script that I missed.

```bash
build() {
  local name="$1" srcdir="$2" tags="${3:-}"
  local tagflag=()
  [ -n "${tags}" ] && tagflag=(-tags "${tags}")
  echo "building ${name}.wasm from ${srcdir}${tags:+ (tags: ${tags})}"
  ( cd "${here}/src/${srcdir}" && \
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath \
      ${tagflag[@]+"${tagflag[@]}"} -o "${here}/${name}.wasm" . )
  echo "  -> $(wc -c < "${here}/${name}.wasm") bytes"
}
```

The `${tagflag[@]+"${tagflag[@]}"}` works in bash but is unusual. Let me check if there's a simpler way.

For an empty array, `${tagflag[@]}` expands to nothing. For a non-empty array, it expands to the elements.

The issue is that bash historically had problems with empty array expansion in some contexts. The `+` idiom is a workaround.

Actually, since Bash 4.4 (or so), empty arrays expand correctly in most contexts. So `${tagflag[@]}` should work directly.

But the existing code uses the `+` idiom. It works. Not a bug.

OK I'm done with my analysis. Let me write the output.

Actually, one more thing. Let me check if the `parseHeredoc` function could have issues.

```go
func parseHeredoc(src []byte, j int) (tag string, strip bool, consumed int) {
    start := j
    for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
        j++
    }
    if j < len(src) && (src[j] == '-' || src[j] == '~') {
        strip = true
        j++
    }
    if j < len(src) && (src[j] == '\'' || src[j] == '"') {
        j++
    }
    ts := j
    for j < len(src) && isIdentByte(src[j]) {
        j++
    }
    tag = string(src[ts:j])
    return tag, strip, j - start
}
```

For `<<<EOT`:
- start = position after `<<<`.
- Skip spaces (none).
- Check `-`/`~` (no).
- Check `'`/`"` (no).
- Read ident: `EOT`.
- tag = "EOT", consumed = 3.

For `<<<"EOT"`:
- start = position after `<<<`.
- Skip spaces (none).
- Check `-`/`~` (no).
- Check `'`/`"` (yes, consume 1).
- Read ident: `EOT`.
- tag = "EOT", consumed = 4.

For `<<<EOT123`:
- Read ident: `EOT123`.
- tag = "EOT123".

For `<<<`:
- No chars after.
- tag = "".
- consumed = 0.

For `<<<  EOT`:
- Skip spaces (2).
- Read ident: `EOT`.
- tag = "EOT", consumed = 5.

Looks fine.

But wait, what about `<<-   EOT` (with extra spaces between `-` and `EOT`)?
- Skip spaces (0, none).
- Check `-` (yes). strip=true, j++.
- Check `'`/`"` (no).
- Skip spaces (3).
- Read ident: `EOT`.
- tag = "EOT", consumed = 5 (1 for `-`, 3 for spaces, 3 for `EOT` - wait that's 7).

Let me recount. start = position after `<<`.
- skip spaces: 0 (none).
- check `-`: yes. j++.
- check `'`/`"`: no.
- skip spaces: 3 (the spaces between `-` and `EOT`).
- read ident: `EOT`.
- consumed = j - start = 1 + 3 + 3 = 7.

Hmm, but the spaces between `-` and `EOT` aren't standard heredoc syntax. The standard is `<<-EOT` or `<<- EOT` (with optional single space). Multiple spaces are unusual.

Anyway, the parser handles it gracefully.

OK final output:

1. MEDIUM|braceparser/parse_core.go:437|isHeredocStart doesn't guard against bit-shift false positive|Add word boundary check before <<|correctness|15|Comment claims guard but only checks chars after <<|code-reviewer

2. LOW|braceparser/parse_core.go:328|matchAnyPrefix is unused dead code|Remove the function|maintainability|5|Defined but never called in package|code-reviewer

Hmm, actually, I realize the line numbers I'm using might be off. Let me be more careful.

Looking at the diff for parse_core.go, the file is shown with `+` markers for new lines. Let me try to count more carefully.

The diff shows the entire parse_core.go file (it's a new file). So all lines have `+` prefix.

The structure:
- Lines 1-12: package doc comment (12 lines)
- Line 13: empty
- Line 14: `package main`
- Line 15: empty
- Line 16: `import "strings"`
- Line 17: empty
- Lines 18-24: `// node mirrors internal/astgroup.Node; the JSON tags are the wire contract.` comment + struct
- Lines 25-30: `node` struct
- Line 31: empty
- Lines 32-37: `blockKeyword` struct comment + struct
- Lines 38-43: `blockKeyword` struct
- ...

OK this is getting tedious. Let me just say "around line X" and move on.

Actually, looking at the diff carefully, I see that parse_core.go is shown in the diff with all `+` lines. The total is 522 lines. Let me look at the function order:

1. Package + imports (lines 1-16)
2. node struct (lines 18-30)
3. blockKeyword struct (lines 32-43)
4. langConfig struct (lines 45-65)
5. State constants (lines 67-75)
6. parseSource function (lines 77-220)
7. classifyHeader function (lines 222-256)
8. isIdentByte function (lines 258-260)
9. lastWholeWord function (lines 262-283)
10. identAfter function (lines 285-296)
11. funcParenName function (lines 298-319)
12. matchAt function (lines 321-328)
13. matchAnyPrefix function (lines 330-338)
14. lineCommentStarts function (lines 340-360)
15. hashAtWordBoundary function (lines 362-375)
16. rawStringStart function (lines 377-392)
17. countHashes function (lines 394-401)
18. hasHashes function (lines 403-411)
19. charLiteralLen function (lines 413-434)
20. isHeredocStart function (lines 436-452)
21. parseHeredoc function (lines 454-477)
22. heredocLineMatches function (lines 479-509)

These are rough estimates. For my output, I'll use:
- matchAnyPrefix: ~line 330
- isHeredocStart: ~line 437

OK let me write the final output.

Actually, you know what, let me just look at the diff more carefully to get accurate line numbers.

The parse_core.go diff content shows the full file. Let me count from the top:

```
+// Package main is the shared brace-block structural parser. One Go source is
+// compiled once per target language (build tag ts|php|rust|bash) into a wasip1
+// reactor .wasm loaded by the internal/astgroup wazero host. It recovers block
+// structure for brace-delimited languages by tracking { } depth (plus string,
+// comment, and heredoc state so braces inside literals do not fabricate blocks)
+// and names each block from the nearest preceding block-introducing keyword.
+//
+// It is a heuristic structural parser for GROUPING ONLY, not a grammar: a
+// misparse degrades to line-proximity grouping for that finding and can never
+// break a reconcile. It emits the same kind/name/start_line/end_line/children
+// JSON contract as the Go and Python plugins, so the host stays language-
+// agnostic. Structure — not physical line text — drives the host's Merkle hash,
+// so blank-line / reformat drift does not change a block's hash.
+//
+// The scanner (this file) and the per-language tables (configs.go) carry no
+// build tag, so they are unit-tested on the host; only `active` selection
+// (active_*.go) and the wasm ABI (main.go) are build-constrained.
+package main
+
+import "strings"
```

That's the package doc and import. Lines 1-16 approximately.

Then the node struct comment and definition:

```
+// node mirrors internal/astgroup.Node; the JSON tags are the wire contract.
+type node struct {
+	Kind      string `json:"kind"`
+	Name      string `json:"name,omitempty"`
+	StartLine int    `json:"start_line"`
+	EndLine   int    `json:"end_line"`
+	Children  []node `json:"children,omitempty"`
+}
```

That's lines 17-25 or so.

OK without doing a careful count, let me just use the function names in my output and approximate line numbers.

Let me just write the output with approximate line numbers.

Final output:

1. MEDIUM|braceparser/parse_core.go:437|isHeredocStart doesn't guard against bit-shift false positive|Add word boundary check for char before <<|correctness|15|Comment claims guard but only checks chars after <<|code-reviewer

2. LOW|braceparser/parse_core.go:330|matchAnyPrefix is unused dead code|Remove the unused function|maintainability|5|Defined but never called anywhere in package|code-reviewer

OK let me also check if there are any issues with the braceparser source that I haven't considered.

Looking at the diff once more for braceparser source:
- configs.go (4 configs) - looks correct
- parse_core.go (scanner) - has the bit-shift issue and dead code
- main.go (wasm ABI) - looks correct
- active_*.go (build tag selection) - looks correct
- go.mod - uses go 1.26

Looking at the braceparser tests:
- configs_test.go - tests for each config
- parse_core_test.go - tests for scanner

The tests are reasonable.

OK I think I'm done. Let me write the output.

Wait, I want to double-check one more thing. Let me look at the `isHeredocStart` function call site:

```go
case cfg.heredocs && matchAt(src, i, cfg.heredocOp) && isHeredocStart(src, i+len(cfg.heredocOp)):
```

For bash with heredocOp="<<":
- `matchAt(src, i, "<<")` checks if `<<` starts at position i.
- If yes, `isHeredocStart(src, i+2)` checks if a tag starts at position i+2.

For `cat <<EOF`:
- i=4 (position of first `<`). matchAt matches.
- i+2=6 (position of `E`). isHeredocStart finds `EOF`. Returns true.

For `$(( 1 << 3 ))`:
- The chars: `$`, `(`, `(`, ` `, `1`, ` `, `<`, `<`, ` `, `3`, ` `, `)`, `)`.
- i=6 (position of first `<`). matchAt matches `<<` at position 6.
- i+2=8 (position of ` `). isHeredocStart skips spaces, finds `3`. Returns true.

So the bug manifests. Confirmed.

For `a << b`:
- i=2 (position of first `<`). matchAt matches.
- i+2=4 (position of ` `). isHeredocStart skips spaces, finds `b`. Returns true.

So the bug also manifests for `a << b`. Confirmed.

OK the bug is real. Let me finalize.

One more thought: is the bug actually observable in practice? In shell scripts:
- `a << b` outside of arithmetic is a syntax error.
- `a << b` inside `$((...))` is bit-shift.

For the bit-shift case, the parser would misinterpret `<<` as a heredoc opener. The heredoc body would consume subsequent lines until a line matching the tag.

For `x=$(( 1 << 3 )); echo $x`:
- `$(( 1 << 3 ))` triggers the bug.
- `;` ends the statement (well, after parenDepth returns to 0).
- After `)`, parenDepth=1. Then `)`, parenDepth=0. Then `;`, resetHeader.
- But heredocPending=true, so when `\n` is hit, state=stHeredoc.

So the heredoc would start at the next line. The tag is `3`. Subsequent lines would be in heredoc mode until a line matching `3`.

If the script has multiple lines, some of which might be just `3`, this could cause unexpected grouping.

But this is an edge case. The sprint plan acknowledges that misparse falls back to proximity. So no data corruption.

Severity: MEDIUM. Real bug, contained impact.

OK final answer.

Actually wait. Let me reconsider. The issue I'm flagging - is it really an issue worth flagging?

The comment in the code says "Guards against treating a bit-shift `a << b` as a heredoc." This is documentation claiming a guard that doesn't exist. So at minimum, the comment is wrong.

Whether the actual behavior is a problem depends on usage. For most bash scripts, `<<` is followed by an identifier that becomes the tag. For `a << b`, the parser would set tag=`b` and enter heredoc mode on next newline. Subsequent code would be swallowed.

This is a real correctness issue. The comment misleads readers into thinking the guard exists.

OK I'll flag it.

Final answer:

1. MEDIUM: isHeredocStart bug.
2. LOW: matchAnyPrefix dead code.

Let me write it out.

Actually, one more thing. The sprint plan mentions:
> Risk: Heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs, producing spurious blocks

This is acknowledged as a known risk with Low impact. The bit-shift issue is a different bug class - it's about misidentifying the heredoc operator, not misreading content inside the heredoc.

So this is a separate bug from the acknowledged risks. Worth flagging.

OK truly final answer.

Hmm actually I want to look once
</think>
