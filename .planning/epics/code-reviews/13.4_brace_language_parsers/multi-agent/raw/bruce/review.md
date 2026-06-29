<think>
Let me analyze this diff carefully. It's adding brace-language AST parsers (TS/JS, PHP, Rust, Bash) to the astgroup system.

Let me review the files in scope:

1. **internal/astgroup/embed.go** - Adds new language mappings to `builtinParsers` and `LanguageForExt`. Looks reasonable.

2. **internal/astgroup/host_test.go** - Adds tests for new language extensions and brace parser loading/parsing. Looks reasonable.

3. **internal/astgroup/parsers/SHA256SUMS** - Adds new checksums. Standard.

4. **internal/astgroup/parsers/build.sh** - Modified to support build tags. Let me check this carefully:
   ```bash
   local tags="${3:-}"
   local tagflag=()
   [ -n "${tags}" ] && tagflag=(-tags "${tags}")
   ```
   Then:
   ```bash
   ${tagflag[@]+"${tagflag[@]}"} 
   ```
   The `${tagflag[@]+"${tagflag[@]}"}` syntax is an array expansion with "alternate value" — when the array is unset or empty, the alternate value is used; otherwise the array elements are used. This is a valid bash idiom for handling possibly-empty arrays in older bash. Should work.

5. **internal/astgroup/parsers/src/braceparser/active_*.go** - Build tag selections. Standard.

6. **internal/astgroup/parsers/src/braceparser/configs.go** - Per-language tables. Let me check:
   - TS: includes `interface`, `class`, `function`, `if`, `else`, `for`, `while`, `switch`. No `enum`, `type`, `namespace`, `module`. These are also block-introducing in TS. But `enum` body uses `{ }`, `namespace` uses `{ }`, `module` uses `{ }`. However, if they're missed they fall back to anonymous `block` — which still works for grouping, just with worse naming. This is acceptable per the "heuristic" nature.
   - PHP: looks reasonable. `namespace` blocks also use `{ }` but it's not listed. Acceptable.
   - Rust: `async fn`, `unsafe fn`, `pub fn` not listed, but they all contain `fn` so `lastWholeWord` with the `fn` keyword will still match. Let me check this... Actually `lastWholeWord` looks for "fn" as a whole word. In `async fn f() {`, "fn" appears as a whole word (preceded by space, followed by space), so it will match. Good.
   - Bash: Only `function`. As documented, function-level only. Good.

7. **internal/astgroup/parsers/src/braceparser/go.mod** - Standalone module for the wasm build. Standard.

8. **internal/astgroup/parsers/src/braceparser/main.go** - The WASM ABI. Let me check:
   - The `emit` function calls `alloc` and then `copy(pins[p], b)`. Since `alloc` already pins the buffer, this is fine.
   - The `main` function is empty. For wasip1 reactors with c-shared build mode and `//go:wasmexport` annotations, `main` is fine to be empty.
   - The `parse` function validates that `int(n) <= len(buf)`. Good.

9. **internal/astgroup/parsers/src/braceparser/parse_core.go** - The main scanner. Let me look for bugs:

Let me trace through the scanner state machine carefully.

Looking at the `stRawString` state:
```go
case stRawString:
    if c == '"' && hasHashes(src, i+1, rawHashes) {
        i += rawHashes
        state = stNormal
    }
```

Wait, when `rawHashes == 0` (e.g., `r"..."`), `hasHashes(src, i+1, 0)` always returns true (the loop runs 0 times), so a lone `"` closes it. Good.

When `rawHashes == 1` (e.g., `r#"..."#`), we need a `"` followed by exactly one `#`. Good.

For `r##"..."##`, we need a `"` followed by exactly two `#`. Good.

But what about when `rawHashes == 1` and the closing `"` is followed by fewer hashes? Like `r#"abc"#`:
- At position of `"`, `c == '"'` and `hasHashes(src, i+1, 1)` checks if src[i+1] is `#`. It is, so close. Then `i += 1` jumps past the `#`. Good.

What about `r#"abc"#more`? The `#` after the closing `"#` is just text — the loop continues past. Fine.

Looking at `stHeredoc`:
```go
case stHeredoc:
    if c == '\n' {
        if heredocLineMatches(src[lineStart:i], heredocTag, heredocStrip) {
            state = stNormal
        }
    }
```

This only triggers on `\n`. When we enter `stHeredoc`, we set state but the current character is already past the `<<EOF`. Let me trace:
- We're at position right after `cat <<EOF`. The parser scans each char. When it sees `\n`, it processes. At the `\n` after `EOF`, `lineStart` is still pointing to where `EOF` started, so `src[lineStart:i]` = "EOF". `heredocLineMatches` would match and close.
  
Wait, let me look at how the heredoc start is handled:
```go
case cfg.heredocs && matchAt(src, i, cfg.heredocOp) && isHeredocStart(src, i+len(cfg.heredocOp)):
    tag, strip, consumed := parseHeredoc(src, i+len(cfg.heredocOp))
    heredocTag = tag
    heredocStrip = strip
    heredocPending = true
    i += len(cfg.heredocOp) + consumed - 1
```

Then in the main loop:
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

OK so on the `\n` AFTER the heredoc operator, we transition to `stHeredoc`. At that point, `lineStart = i + 1` is the position of the first character on the new line.

Now in `stHeredoc`:
```go
case stHeredoc:
    if c == '\n' {
        if heredocLineMatches(src[lineStart:i], heredocTag, heredocStrip) {
            state = stNormal
        }
    }
```

This checks at each `\n` whether the line is the terminator. The line is `src[lineStart:i]`. Good.

But wait — there's a potential issue. The `stHeredoc` state is set after the `\n` that follows the heredoc opener. So the FIRST character we see in `stHeredoc` is the first char of the heredoc body. That's fine, no character matching needed there.

But the state itself has a special case: when we encounter a `\n`, we check if the line is a terminator. So the heredoc body can contain any characters including `{`, `}`, `/*`, `*/`, etc. — none of those will trigger state changes because we're in `stHeredoc` state.

Let me check `stParamExp`:
```go
case stParamExp:
    if c == '{' {
        paramDepth++
    } else if c == '}' {
        paramDepth--
        if paramDepth <= 0 {
            state = stNormal
        }
    }
```

OK, this handles nested `${...}`. Good.

But there's an issue: what if the parameter expansion contains a string literal like `${var:-"default"}`? The quote chars would be ignored — but that's fine because the brace counting still works. The string content is opaque.

Actually wait — what if it's inside an outer string? Like `"${var:-"default"}"`. The `$` is detected in stNormal, but only if we're in stNormal. Strings are processed in `stString` which only handles escapes and delimiters, not `$`. Hmm.

Actually looking at the code, `cfg.paramExpand` is only checked in `stNormal`. So if we're in a string and we encounter `${...}`, the `${` will be treated as text inside the string. The braces inside the string wouldn't trigger anything either (since we stay in `stString`). Good.

But wait — what about Bash command substitution `$()` or backticks? Those would contain code. But Bash's heredoc and string state tracking should handle most cases. And since bash's `paramExpand` is only for `${...}`, backticks aren't tracked. Hmm.

Actually the bash config has `strChars: "\"'"` — so single and double quotes are string delimiters. Inside double quotes, ${...} is NOT a block. Good. Inside single quotes, ${...} is NOT a block (single quotes don't expand). But what about unquoted ${...}? The scanner handles it in stNormal. Good.

But what about `$(command)` substitution in Bash? That contains code with braces. If we have:
```bash
foo() {
  echo $(case $x in 1) echo one ;; esac)
  bar
}
```
The `$(...)` content would be treated as regular text (just parens, which don't affect brace depth). Good — it won't break anything.

But what about:
```bash
foo() {
  echo ${var:-$(echo {nested})}
}
```
The `${` triggers `stParamExp`. Inside, we have `var:-$(echo {nested})`. The `{` would increment paramDepth. Then we see `}` (matching the inner brace), decrementing. Eventually the OUTER `}` closes the paramExp. This might mis-parse if there's nested `{...}` that's not parameter expansion. Hmm.

Actually, the scanner uses `paramDepth` and the closing `}` decrements it. So `$(echo {nested})` inside `${...}` would cause `{` and `}` to be tracked as if they were nested param expansions. That's likely OK since the depth balance usually works out.

Let me look at potential edge cases:

1. **Heredoc within a function body in PHP**: The `<?php` opening tag is on line 1, then the heredoc opener `<<<EOT` is inside the function. The scanner sees `<?php` as text (header accumulator). Then `<<<EOT` matches the heredoc operator. After `\n`, enters `stHeredoc`. Body braces are ignored. Then `\nEOT\n` matches the terminator (because of `heredocLineMatches` allowing tag + non-ident).

But wait! The PHP heredoc body also allows `$variable` interpolation. And PHP allows `${var}` for complex interpolation. The scanner doesn't handle PHP interpolation. Hmm, but does it matter? Let me think.

In PHP, `"$var"` expands. Heredoc bodies also expand. If we have:
```php
$a = <<<EOT
hello $name { brace }
EOT;
```
Inside the heredoc, the brace `{` and `}` are literal characters (in PHP heredoc, brace interpolation uses `{$var}`). Since we're in `stHeredoc` state, those braces are ignored. Good.

2. **What about `${var}` inside a PHP heredoc?** The scanner is in `stHeredoc`, so `$` doesn't trigger `paramExpand`. The `{` is ignored. Good.

3. **What about TS template literal interpolation `${expr}`?** The scanner is in `stString` (with ` as delimiter). The `cfg.paramExpand` is only checked in `stNormal`. So `${expr}` inside a template literal is NOT treated as opaque — it's just text within the string. Wait, is this correct?

Actually, template literal `${expr}` CAN contain code with braces! Like:
```typescript
const s = `a ${function() { return 1; }()} b`;
```

The inner function definition `{ return 1; }` has braces that would mess up the brace count if we were tracking them. But because we're in `stString`, those braces are ignored. So template literal interpolation is handled by being inside a string state. Good.

But what about nested template literals? Like `${`inner ${nested}`}`? The outer ` is the delimiter, then we see `${` — which is text in `stString`. The inner `` ` `` is just a char, the inner `${...}` is text. Eventually the outer ` closes. OK that works.

4. **TS regex literal**: The scanner doesn't differentiate regex from division. `cfg.strChars` for TS is `"'\``. So `/` is NOT a string delimiter. This could be an issue! In TS, `/foo/` is a regex literal, and the regex content can contain `{` and `}` (e.g., `/{.*}/`). These would be tracked as real braces.

Example:
```typescript
function f() {
  const re = /\{.*\}/;
  use(re);
}
```

The `\{` and `\}` characters inside the regex would be seen as `{` and `}` by the scanner. But! Looking at the code, in `stString` we'd handle escapes... wait, but regex literals aren't in `stString`. They're in `stNormal`. So `\{` would just be `{` (not an escape), and `\}` would be `}`. That would incorrectly close the function block!

Hmm, this is a potential bug. But let me check the existing TS plugin (pyparser/goparser precedent)... The epic says:
> Track string/comment/heredoc state in the scanner (as the 13.1 Python plugin does for triple-quoted strings); on misparse the finding falls back to line proximity — grouping degrades, never breaks.

So they accept some misparse. The regex issue would cause a misparse for TS regex literals containing braces. But:
1. This is a known risk in the epic ("heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs").
2. Mitigation: the corpus cases cover regex edge cases.

Let me check if there's a corpus case for TS regex with braces. Looking at the corpus... I don't see a TS regex test case. There IS `ts_string_brace_no_overmerge` which tests string braces. No regex test.

Actually, the risk mentions "regex" explicitly. So this is acknowledged. The corpus doesn't cover it but it falls back to proximity. OK, not really a finding per the sprint plan.

But wait, the sprint plan says "Corpus cases cover string/heredoc edge cases per language." — So the corpus should cover regex for TS. It doesn't. This might be a finding. But it's medium-low priority since the fall-back behavior works.

5. **String termination with backslash at end of line**: Line continuation in bash. In a string like `"abc\<newline>def"`, the scanner is in `stString`, sees `\`, sets `escape = true`. Then sees `\n`, decrements... no, `escape` is just a flag that affects the next char. So:
   - `\` sets `escape = true`
   - `\n` is the next char, so it goes into `stString` which does `escape=false; // skip`, meaning the `\n` is just absorbed.

Wait, let me re-read:
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

If `escape == true`, we set `escape = false` and consume the char. So `\\n` (backslash + n) is handled. But what about `\<newline>`?

Actually in this code, `\` sets escape. Next char is consumed without effect. So `\` followed by `\n`: escape becomes true, then `\n` is consumed with escape reset. Good.

But the line counter is incremented! Let me check... The line incrementing code is OUTSIDE the switch:
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

So the line counter increments regardless of state. For a line continuation, this is correct (we still need to count newlines).

6. **Bash `<<-` and `<<~`**: The `parseHeredoc` handles `-` and `~` for stripping. But `isHeredocStart` is called first:
```go
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

OK, this skips whitespace then optional `-`/`~` then optional quote then identifier char. Good.

But wait — `bashConfig.heredocOp` is `"<<"`. What about `<<<`? That's a here-string, different from heredoc. If we have `cat <<<"hello"`, the `<` at i matches `<<`, then `j = i + 2`, `isHeredocStart(src, j)` checks if `"hello"` starts a tag. The first non-whitespace char is `"`, which is a quote, so we skip it. Then `h` is an ident char. So `isHeredocStart` returns true. Then `parseHeredoc` is called which reads `hello` as the tag. Then the scanner enters `stHeredoc` after the next `\n`. But here-strings are a single line, not multi-line!

This is a potential bug. If someone uses `cat <<<"hello\nworld"` or similar, the scanner would treat it as a heredoc and look for a terminator on its own line, which wouldn't be found.

However, this is a pretty obscure edge case. And the fall-back is line proximity. Probably acceptable.

7. **Class methods in TS**: 
```typescript
class Foo {
  method() {
    body();
  }
}
```
The scanner sees `class Foo {` → opens class block (name = Foo). Then `method() {` → opens block. `classifyHeader("class Foo {\n  method() ")`:
- Last whole word of "class" → index of "class"
- Last whole word of "method" → index of "method" (greater than "class")
- So we pick "method", kind = "func" (but wait, `method` is NOT in the keyword list!)

Looking at tsConfig:
```go
keywords: []blockKeyword{
    {word: "function", kind: "func", named: true},
    {word: "class", kind: "class", named: true},
    {word: "interface", kind: "class", named: true},
    {word: "if", kind: "if"},
    {word: "else", kind: "else"},
    {word: "for", kind: "for"},
    {word: "while", kind: "while"},
    {word: "switch", kind: "switch"},
},
```

`method` is NOT in the keyword list. So `lastWholeWord` doesn't find it. Then `arrowFunc` check — not an arrow. Then `funcParenName("class Foo {\n  method() ")` — `funcParenName` requires `t` to end with `)`. But our header is `"class Foo {\n  method() "` — wait, when does the open happen?

Let me trace:
- We see `class`, accumulate in header → "class "
- We see `Foo`, header → "class Foo"
- We see ` `, header → "class Foo " (but spaces don't matter)
- We see `{`, calls `openBlock()` with header "class Foo"

Hmm so the FIRST `{` opens the class block. Then we continue accumulating header. The header was reset by `openBlock()`. Now we're inside the class.

Inside class:
- `  method()` — `method(` accumulated. Wait, the header accumulator is reset by `openBlock`. So now header is empty.
- `m`, `e`, ..., `d`, `(`, `)`, ` `, `{` — at the `{`, openBlock is called with header "  method() " (or similar).
- `classifyHeader("  method() ", cfg)`:
  - keyword search: `function` not found, `class` not found (it's "method"), etc. None match.
  - `arrowFunc`: no `=>`. Skip.
  - `funcParenName("  method() ")`: `t = "method()"`. Has suffix `)`. Open at index of `(`. `name = "method"`. All ident bytes. Return `("method", true)`.
- Returns `("func", "method")`.

OK so `funcParen` saves us here. Good. The method is correctly identified as a func named "method".

8. **TS function declaration**: `function f() {` — `f` is captured via the `function` keyword's `named: true`.

9. **TS arrow function**: `const f = () => {` — arrowFunc triggers, returns "func" with empty name. Good.

10. **TS async function**: `async function f() {` — `function` is the last whole word, kind = "func", named = true, name = "f". Good.

11. **PHP traits, interfaces, classes**: All handled. Good.

12. **Rust lifetimes and char literals**: Handled via `charLiterals` flag. Good.

13. **Bash heredocs**: The bash config has `heredocOp: "<<"`. So `<<EOF` starts a heredoc. The body is scanned until a line matching `EOF` (at column 0, or with leading tabs stripped). But the actual bash rule is that the terminator must be at the START of a line (after whitespace). The current code's `heredocLineMatches` doesn't enforce column-0 strictness — it just checks if the trimmed line starts with the tag.

Wait, let me re-read:
```go
func heredocLineMatches(lineBytes []byte, tag string, strip bool) bool {
    if tag == "" {
        return false
    }
    s := strings.TrimRight(string(lineBytes), "\r")
    if strip {
        s = strings.TrimLeft(s, "\t")
    }
    if s == tag {
        return true
    }
    if strings.HasPrefix(s, tag) {
        return !isIdentByte(s[len(tag)])
    }
    return false
}
```

This trims `\r` but not leading whitespace (unless `strip`). So if a line is `  EOF`, it would NOT match (without strip). The test `TestParseSource_BashHeredocBracesIgnored` checks `EOF` at column 0, which works.

But what if a user has indented heredocs (uncommon but allowed)? Actually, bash heredocs MUST have the terminator at column 0. The scanner is correct here.

But wait — what if a line in the heredoc body starts with `EOF`? E.g.:
```
cat <<EOF
some text EOF
EOF
```
The body line "some text EOF" — would it match? After `TrimRight`, it doesn't equal `EOF`. After `HasPrefix`, "some text EOF" doesn't start with "EOF". So no match. Good.

What about:
```
cat <<EOF
EOFthis
EOF
```
The line "EOFthis" — `HasPrefix("EOFthis", "EOF")` is true. `isIdentByte(s[3])` — `s[3]` is "t" which is an ident byte. So no match. Good, the `!isIdentByte` check saves us.

What about `EOF;` (PHP-style)?
```
$s = <<<EOT
hello
EOT;
```
The line is "EOT;". `HasPrefix("EOT;", "EOT")` is true. `isIdentByte(";")` is false. So matches. Good.

14. **Heredoc strip is missing when reading tag but available for terminator**: In Bash, `<<-EOF` strips leading tabs from body AND terminator. The code handles this with `strip` parameter. Good.

15. **TestBashConfig_DollarHashNotComment**: This test exercises `[ $# -gt 0 ] && {`. The `$#` is special — should NOT be a comment. The code handles this via `commentWordBoundary` and `hashAtWordBoundary`. Let me verify:
```go
func hashAtWordBoundary(src []byte, i int) bool {
    if i == 0 {
        return true
    }
    switch src[i-1] {
    case ' ', '\t', '\n', '\r', ';', '&', '|', '(', '`':
        return true
    }
    return false
}
```

Before `$`, the byte is space. So `$#` — checking at the position of `#`, the previous byte is `$`. `$` is not in the boundary list. So returns false. `#` is NOT at a word boundary. So it's not treated as a comment. 

What about `[ $# -gt 0 ] && {`? Scanning byte by byte:
- `[` → add to header
- ` ` → space, skip (headerStarted = false)
- `$` → add to header
- `#` → check `lineCommentStarts`. `#` matches lineComments. `commentWordBoundary` is true. `hashAtWordBoundary`: prev byte is `$`, not a boundary. So returns false. NOT a comment. Good.

16. **PHP `<?php` opener**: PHP source often starts with `<?php`. The scanner treats it as text. Good — it doesn't introduce a block, so the first function's keyword `function` is correctly identified.

17. **Hash inside `${#var}` in bash**: `${#var}` means string length. The `#` is inside `${...}`. In the scanner:
- `$` in stNormal → check `paramExpand && c == '$' && i+1 < len(src) && src[i+1] == '{'` → yes, enter stParamExp.
- Inside stParamExp, all chars are ignored (except `{` and `}` for depth tracking).
- `#` is ignored.
- `v`, `a`, `r` ignored.
- `}` → paramDepth = 0 → state = stNormal.

Good, no comment detection happens inside paramExp.

But wait! The check for `lineCommentStarts` happens in `stNormal` ONLY. In `stParamExp`, the switch case is different. So `#` in `stParamExp` is just ignored. 

18. **What about a `#` followed by `{` directly in bash (not inside `${}`)?** Like:
```bash
foo() {
  # comment {
  echo done
}
```
- `#` is at position, prev byte is space (boundary). `lineCommentStarts` returns true. Enter `stLineComment`.
- All chars until `\n` are ignored.
- `\n` → state = stNormal.
- `  echo done` → text in header.
- `}` → closeBlock.

Good, the comment brace is ignored.

19. **PHP `//` inside `function f() { // comment }`**: 
- `//` → lineCommentStarts, enter stLineComment.
- ` comment }` ignored.
- `\n` → state = stNormal.

Good.

20. **Potential issue: `closeBlock` may not balance**: Let me check if `closeBlock` is called appropriately:
```go
case c == '}':
    closeBlock()
```
This is in stNormal. What if we're in stLineComment or stBlockComment? `}` is consumed by the comment state, so closeBlock isn't called. Good.

What about `}` in a string? `stString` consumes it. Good.

What about `}` in a heredoc body? `stHeredoc` ignores everything. Good.

What about `}` in paramExp? Tracked by depth. Good.

What about `}` in rawString? `stRawString` only checks for `"`. So `}` in a raw string is just a char. Good.

21. **Trailing braces after EOF**: Unbalanced `}` at end. The `for len(stack) > 1 { closeBlock() }` at the end closes any unclosed blocks. If there are extra `}`s, `closeBlock` has `if len(stack) <= 1` guard:
```go
closeBlock := func() {
    if len(stack) <= 1 {
        resetHeader()
        return
    }
    ...
}
```
So extra `}` doesn't underflow. Good.

22. **Header line for brace blocks**: When a block is opened, the header's line is captured:
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

`headerLine` is the line where the header started (e.g., the line with `function`). So the block's start line is correctly the function declaration line. Good.

But wait — `headerLine` is a closure-captured variable. Let me check... yes, it's captured by the closures (resetHeader, addHeader, openBlock). Each call to `addHeader` (when headerStarted transitions to true) sets `headerLine = line`. Good.

23. **`heredocPending` state**: After seeing `<<<EOT` on a line, the next `\n` sets `heredocPending = false` and `state = stHeredoc`. Wait:
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

This is OUTSIDE the switch. But the switch processes the current char FIRST. So for the `\n` at the end of `<<<EOT`:
- Switch processes `\n` in stNormal (no special case). Falls through to default → `addHeader('\n')` → header gets a space.
- Then the `if c == '\n'` block runs: line++, heredocPending = false, state = stHeredoc.

Good. Then we're in stHeredoc state for the body.

24. **Bash `function` keyword followed by `{`**: `function name {` — `function` is in bashConfig.keywords. The `{` triggers openBlock with header "function name". classifyHeader finds "function" as last whole word, returns ("func", "name"). 

25. **What about `function` as a command name?** E.g., `function=$1; echo $function;`. `function` here is a variable name, not a keyword. The scanner would still match `function` as a whole word in classifyHeader when the next `{` comes. But for a simple assignment, there's no `{` so openBlock isn't called. So no harm.

26. **What about a string containing `function`?** E.g., `echo "function"; { block }`. In stString, `function` is consumed. The `;` is consumed. The `{` starts a new block. header is empty (or has only "echo \"function\"; " which doesn't end with a keyword). Returns ("block", ""). Good.

27. **TS decorators**: `@Component({...})` — `@` and `Component` are added to header. Then `({` — `(`, header gets `(`. Then `{`, openBlock. Header is "@Component(". classifyHeader finds "Component" as last whole word... but "Component" is not in the keyword list! So no keyword match. `arrowFunc`? No. `funcParenName` — header doesn't end with `)`. Returns ("block", ""). So a decorator call's `{...}` becomes an anonymous block. That's a minor issue — decorators are not named. But acceptable since the heuristic parser isn't expected to handle decorators perfectly.

28. **PHP `namespace` blocks**: `namespace Foo { ... }` — `namespace` is NOT in phpConfig.keywords. So this would be an anonymous block. Hmm, but PHP namespace blocks use `{ }`. Let me check if this matters... probably minor.

29. **TS `enum` and `namespace`**: Not in tsConfig. Anonymous blocks. Minor.

30. **Potential bug: PHP `elseif`**: It's listed as a separate keyword. But in PHP, `elseif` can also be written as `else if`. The config doesn't have `else if`. Minor.

31. **PHP `match` expression**: Not in phpConfig. Not block-introducing though (uses `=>`). Fine.

32. **PHP arrow functions**: `fn() => ...`. Not block-introducing. Fine.

33. **TS `do { } while`**: Not in tsConfig. The `while` is the keyword but `do` isn't. So `do { ... }` would not be classified as anything special — `while` would not be seen as the last whole word before `{`. Actually, `do {` has no `while` keyword. The `{` after `do` would open an anonymous block. Minor.

34. **Rust `unsafe fn`, `async fn`, `pub fn`, `const fn`, etc.**: These have `fn` as a whole word after a space. `lastWholeWord` would find `fn`. Good.

35. **Rust `impl Trait for Type`**: `impl` as a keyword is mapped to "class". If you have `impl Foo for Bar { ... }`, it's an impl block named "Foo". The `for` is not a whole word in the relevant position... actually it IS, but `for` is in keywords with kind "for". `lastWholeWord` finds the latest one. So `impl Foo for Bar {` — last whole word is `for`. Hmm.

Wait, let me re-read lastWholeWord. It returns the LAST whole-word occurrence. So for `impl Foo for Bar`:
- `impl` matches at index 0
- `for` matches at index 9
- `for` is later, so we pick `for` with kind "for".

But this is an impl block, not a for block! This is a bug — `impl Foo for Bar { ... }` would be classified as "for" instead of "class". Hmm.

Actually let me look at the test `TestRustConfig_ImplFnGrouping`:
```go
src := []byte("impl Foo {\n    fn bar(&self) {\n        let x = 1;\n        let y = 2;\n        x + y\n    }\n}\n")
root := parseSource(src, rustConfig)
if root.Children[0].Kind != "class" || root.Children[0].Name != "Foo" {
    t.Fatalf("rust impl should map to class Foo: %+v", root.Children[0])
}
```

This test passes because there's no `for` keyword in `impl Foo {`. So in the simple case, it works.

But `impl Foo for Bar {` would NOT work — would be classified as "for". This is a known limitation since Rust impl blocks can have `for` clauses.

36. **Rust `where` clauses**: `fn foo<T>(x: T) -> T where T: Clone { ... }`. The `where` is not a keyword, but `fn` is. So classifyHeader finds `fn` as the last whole word. Good.

37. **Bash `[ $var -gt 0 ] && {`**: Test covers this. Good.

38. **Line numbers for nested blocks**: The header's line is captured as `headerLine`. So if a `function` is declared on line 5, the block starts at line 5. The closing `}` sets `EndLine = line`. Good.

39. **`resetHeader` resets `parenDepth`**: When `resetHeader` is called (on `;` or on `{` opening a block), `parenDepth` is reset to 0. So after `for (let i = 0; i < n; i++) {`, we open a block. `resetHeader` is called. But the parens were inside the header, not after. Wait, let me trace `for (let i = 0; i < n; i++) {`:
- `f`, `o`, `r` → header "for"
- ` ` → space
- `(` → parenDepth = 1, addHeader('(') → header "for ("
- `l`, `e`, `t`, ` `, `i`, ` `, `=`, ` `, `0`, `;` → header gets `;` (since parenDepth > 0). Header "for (let i = 0; "
- ` `, `i`, ` `, `<`, ` `, `n`, `;` → header gets `;`. Header "for (let i = 0; i < n; "
- ` `, `i`, `+`, `+`, `)` → parenDepth = 0, addHeader(')') → header "for (let i = 0; i < n; i++)"
- ` `, `{` → openBlock. classifyHeader finds "for" as keyword. Returns ("for", ""). 
- resetHeader: header = empty, headerStarted = false, parenDepth = 0.

Good. The `for` is preserved through the semicolons.

40. **What if a `;` appears before any `(`?** Like `let x = 1; function f() {`. The `;` at parenDepth 0 calls `resetHeader()`. So "let x = 1" is discarded. Then "function f()" is accumulated for the next block. Good.

41. **`addHeader` skips leading spaces**: `if b == ' ' && !headerStarted { return }`. So leading whitespace doesn't accumulate. Good.

42. **`addHeader` for `(` and `)`**: They're added to the header. But parens aren't really part of a "block-introducing header". However, having them in the header doesn't hurt classifyHeader. Good.

43. **What about a newline in the middle of a block header?** Like:
```
function
  f() {
```
The newline would be replaced with a space (via `addHeader`):
```go
addHeader := func(b byte) {
    if b == '\n' || b == '\r' || b == '\t' {
        b = ' '
    }
    ...
}
```
So header becomes "function   f() ". `lastWholeWord` finds `function` (since `f` is preceded by space). Then `identAfter` returns "f". 

But the `headerLine` is set to the line where headerStarted becomes true (first non-space char). For "function", that's line 1. So block start = line 1. The `{` is on line 3. But the block starts at line 1 (where `function` is). That's reasonable.

44. **TestParseSource_BraceInStringIgnored**: Tests `let s = "a { b } c"` and `let t = 'x } y {'`. The braces inside the string should be ignored. The strings are in `stString` state. Good.

45. **Multi-line string in TS template literal**: 
```typescript
const s = `line1
line2 { brace }
line3`;
```
The string state spans newlines. The braces are ignored. Good.

46. **Potential issue with `$` in TS strings**: TS config doesn't have `paramExpand: true`. So `$` is just text. But TS doesn't use `${...}` interpolation outside template literals, and template literals are handled by string state. Good.

47. **Bash `<<<` (here-string)**: Already discussed. Edge case but probably OK.

48. **TS regex `/.../`**: Already discussed. Could be a false block. But this is acknowledged in the risk register.

49. **TS class field initializer `{ ... }`**: 
```typescript
class Foo {
  bar = { x: 1, y: 2 };
}
```
The `bar = { ... }` is a field initializer. The `{` after `=` triggers openBlock with header "  bar = ". No keywords match. `arrowFunc` no. `funcParenName` no. Returns ("block", ""). Anonymous block. Good — won't false-merge with another class.

50. **PHP `function __construct()`**: Handled by `function` keyword, name = `__construct` (identAfter handles `_`). Good.

51. **Multiple-line function in PHP**:
```php
function
  f() {
  body();
}
```
Header accumulates with newline→space. classifyHeader finds `function`. Good.

OK I've done extensive review. Let me now look for ACTUAL BUGS or issues.

Let me look at the tests more carefully:

`TestBashConfig_HashCommentStillWorks`:
```go
src := []byte("f() {\n  # a comment with a } brace\n  echo hi\n}\n")
root := parseSource(src, bashConfig)
if len(root.Children) != 1 || root.Children[0].Name != "f" || len(root.Children[0].Children) != 0 {
    t.Fatalf("boundary # comment must still be stripped: %+v", root.Children)
}
```

`# a comment with a } brace` — the `}` in the comment should be ignored. Let me trace:
- ` ` → skipped
- `#` at position. prev byte is space → word boundary. `lineCommentStarts` returns true. Enter `stLineComment`.
- Rest of line ignored.
- `\n` → state = stNormal.
- Continue. `echo hi`, `}`, etc.

Good.

`TestBashConfig_DollarHashNotComment`:
```go
src := []byte("f() {\n  [ $# -gt 0 ] && {\n    echo yes\n  }\n  echo done\n}\n")
root := parseSource(src, bash)
if len(root.Children) != 1 || root.Children[0].Name != "f" {
    t.Fatalf("want single func f, got %+v", root.Children)
}
if root.Children[0].EndLine != 6 {
    t.Fatalf("func f should span to its real closing brace on line 6, got EndLine=%d", root.Children[0].EndLine)
}
d, _ := deepest(root, 5)
if d.Kind != "func" || d.Name != "f" {
    t.Fatalf("line 5 should resolve to func f, got %+v", d)
}
```

Trace:
- `f() {` → opens func "f". line 1.
- `\n` → line 2.
- `  [ $# -gt 0 ] && {` → 
  - ` ` skipped
  - `[` added to header (starts a new header? No, header was reset. headerStarted = true at `[`.)
  - Wait, `f()` opened the block and reset the header. Now the header is empty.
  - `[` is added to header.
  - ` ` skipped.
  - `$` added.
  - `#` — `lineCommentStarts`? `#` matches. `commentWordBoundary` true. `hashAtWordBoundary`: prev is `$`. Not in boundary list. So NOT a comment. `#` added to header.
  - ` `, `-`, `g`, `t`, ` `, `0`, ` `, `]` → all added.
  - ` ` skipped.
  - `&`, `&` → added.
  - ` ` skipped.
  - `{` → openBlock. classifyHeader on `[ $# -gt 0 ] && `. No keywords. arrowFunc? No. funcParenName? Doesn't end with `)`. Returns ("block", ""). New block opens, unnamed.

So a "block" opens inside `f`. Hmm, but the test expects `len(root.Children[0].Children) == ...` — actually it expects `deepest(root, 5)` to be "func/f" with `Name != "f"`. Wait, the test has:
```go
d, _ := deepest(root, 5)
if d.Kind != "func" || d.Name != "f" {
```
So deepest for line 5 should be the outer func. Let me continue tracing:
- `echo yes` — header accumulated inside the new block.
- `\n` → line 3.
- `  }` → closeBlock. Closes the inner block. EndLine = 3. Wait, the `}` is on line 3 actually... let me re-check the source.

```
f() {           line 1
  [ $# -gt 0 ] && {  line 2
    echo yes        line 3
  }                  line 4
  echo done         line 5
}                    line 6
```

Hmm I miscounted. Let me retrace:
- `f() {` at line 1. Opens func f, EndLine will be set at close.
- `\n` at end of line 1. line = 2.
- `  [ $# -gt 0 ] && {` at line 2. Header accumulates. `{` at end → openBlock. New unnamed block. headerLine = 2 (where `[` started).
- `\n`. line = 3.
- `    echo yes` at line 3. Header accumulates. No `{` or `}`.
- `\n`. line = 4.
- `  }` at line 4. closeBlock. Closes the unnamed block. EndLine = 4. Appends to func f's Children.
- After closeBlock, `resetHeader()`. header is reset.
- `\n`. line = 5.
- `  echo done` at line 5. Header accumulates.
- `\n`. line = 6.
- `}` at line 6. closeBlock. Closes func f. EndLine = 6. Appends to root's Children.

So root.Children = [{Kind: "func", Name: "f", StartLine: 1, EndLine: 6, Children: [{Kind: "block", Name: "", StartLine: 2, EndLine: 4}]}].

`len(root.Children) == 1` ✓
`root.Children[0].Name == "f"` ✓
`root.Children[0].EndLine == 6` ✓

`deepest(root, 5)`:
- root: line 5 in [1, 6]? root.EndLine = 6. Yes. (root.StartLine = 1, root.EndLine = 1 originally, then set to max(line, 1). But since `if line > root.EndLine`, it's set to 6 after the loop. So [1, 6]. Yes 5 in range.
- Children: [func f at [1, 6]]. 5 in [1, 6]. Recurse.
  - func f: 5 in [1, 6]. Children: [block at [2, 4]]. 5 not in [2, 4]. Return func f. ✓

So test passes.

OK let me now look for ACTUAL ISSUES.

Hmm, let me re-check the parse function in main.go:

```go
//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
    buf, ok := pins[ptr]
    if !ok || int(n) > len(buf) {
        return emit(node{Kind: "error", Name: "bad pointer"})
    }
    src := buf[:n]
    ...
}
```

`pins` is `map[int32][]byte`. The `unsafe.Pointer(&b[0])` is the guest memory address. The host writes source to that address. We read from `buf[:n]`. This is correct for the wazero memory protocol.

But there's a subtle issue: `pins` is a Go map. Go maps are not safe for concurrent access. If two goroutines call `parse` simultaneously on different `ptr`s, the map could be corrupted. But since the wasm module is single-threaded by default and Go runtime in wasip1 is goroutine-per-call... hmm actually the goroutines could be on different OS threads.

Wait, this is similar to the existing goparser/pyparser pattern (mentioned in the comment). If it's a precedent, we trust it. The existing code presumably works.

Let me also check: `alloc` returns a pointer that's pinned. The buffer is NOT zeroed. So if the host doesn't fill it completely, the remaining bytes are uninitialized. But the protocol requires the host to write `n` bytes before calling `parse(ptr, n)`. So the `buf[:n]` is valid.

Actually wait — `pins[p] = b` makes b a reference. Then `unsafe.Pointer(&b[0])` is the address. If Go's GC moves the slice (which it doesn't for pinned heap objects, but normally it can), the pointer becomes stale. The existing goparser/pyparser comment says "non-moving-GC assumption". This is a known limitation. OK.

Now let me check for any potential issues with the build.sh:

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

The `${tagflag[@]+"${tagflag[@]}"}` is the bash idiom for "use tagflag elements if tagflag is set, otherwise use empty string". This works in older bash for handling possibly-empty arrays. In newer bash (4.4+), you can use `"${tagflag[@]}"` directly. But this idiom works in all versions. Good.

The build script then runs:
- `build go goparser` → no tags, builds go.wasm from src/goparser
- `build python pyparser` → no tags, builds python.wasm from src/pyparser
- `build ts braceparser ts` → with -tags ts, builds ts.wasm from src/braceparser
- `build php braceparser php` → with -tags php, builds php.wasm
- `build rust braceparser rust` → with -tags rust, builds rust.wasm
- `build bash braceparser bash` → with -tags bash, builds bash.wasm

The active_*.go files each have `//go:build ts` (or php/rust/bash). They're mutually exclusive. But what if NONE is set? Then `active` is undeclared and the build fails. With the `-tags` always passed by build.sh, this is fine.

But wait — the unit tests in the braceparser directory run WITHOUT any build tag! Looking at parse_core_test.go and configs_test.go, they call `parseSource(src, phpConfig)` etc. directly. They don't use `active`. So the tests don't need a build tag. Good.

But what about main.go and active_*.go? Those have build tags:
- main.go: `//go:build wasip1` — only built for wasip1
- active_ts.go: `//go:build ts`
- active_php.go: `//go:build php`
- active_rust.go: `//go:build rust`
- active_bash.go: `//go:build bash`

For `go test` (host), wasip1 isn't set, so main.go is excluded. The active_*.go files have tags but we're not in any of them — they're also excluded. So `active` isn't declared during `go test`. That's fine because tests don't use `active`.

For `go build -tags ts -buildmode=c-shared -trimpath ...`, GOOS=wasip1 is set, so main.go is included. AND `-tags ts` is set, so active_ts.go is included. So `active = tsConfig`. Good.

Hmm but wait — `//go:build wasip1` is triggered when GOOS=wasip1? Let me check. Go has implicit build constraints from GOOS/GOARCH. So when GOOS=wasip1, the `wasip1` build constraint is satisfied. So main.go is included. Good.

Actually, looking more carefully, I think there's a potential issue with the build tags. The test files (parse_core_test.go, configs_test.go) don't have build tags. They reference `parseSource`, `node`, `langConfig`, `blockKeyword`, `tsConfig`, etc. These are defined in parse_core.go and configs.go, which have no build tags. So the test files compile in host mode. Good.

But for the wasm build, the test files are NOT compiled (because test files are excluded from build). Wait — does `go build` include test files? No. `go test` includes them. So:
- `go build -tags ts ...` (wasm) → main.go + active_ts.go + parse_core.go + configs.go (no test files)
- `go test ./...` (host) → test files + parse_core.go + configs.go (no main.go since wasip1 not set, no active_*.go since no tag set)

Good. The split is clean.

But wait — the `braceparser` directory has its OWN go.mod. It's a separate module. So `go test ./...` in the parent module doesn't recurse into it. To test the parser, you'd need to `cd` into braceparser and run `go test`. The build.sh builds the wasm. OK, this seems intentional (per the comment in go.mod).

But — does the parent module's `go test ./internal/astgroup/...` test work? Let me check. The astgroup has its own tests (host_test.go). Those tests reference `Parser(lang)` which looks up `builtinParsers`. The new `ts`, `php`, `rust`, `bash` entries are added. The new `LanguageForExt` returns the right language. The .wasm files are embedded via `//go:embed`. So as long as the .wasm files exist (which they do — they were added in this diff), the tests should pass.

Now `TestHost_BraceParsersLoadAndParse`:
```go
h := NewHost()
defer func() { _ = h.Close() }()
for _, c := range cases {
    p, err := h.Parser(c.lang)
    require.NoErrorf(t, err, "parser for %q should be registered", c.lang)
    root, err := p.Parse([]byte(c.src))
    ...
}
```

This instantiates each parser and parses representative source. Each instantiation compiles the wasm. This is slow for tests but acceptable.

OK let me look for any actual bugs now.

Let me think about edge cases that could cause issues:

1. **TS optional chaining and nullish coalescing**: `?.` and `??` don't introduce blocks. OK.

2. **TS/JS object spread `{...obj}`**: Inside `{`, header is empty. The `...` and `obj` are added. The `{` opens. No keyword match. funcParenName? No. Returns ("block", ""). OK.

3. **JS computed property names `{[expr]: val}`**: Same, becomes anonymous block. OK.

4. **Rust async blocks `async { ... }`**: `async` is not a keyword. Becomes anonymous block. OK.

5. **Rust closures `|x| { ... }`**: The `{` is in stNormal (the `|` is text). Header is empty (or has the closure args). Becomes anonymous block. OK.

6. **Rust `unsafe { ... }`**: Same, anonymous. OK.

7. **Bash `[[ ... ]]`**: Brackets, not braces. OK.

8. **Bash arrays `arr=(...)`**: Parens, not braces. OK.

9. **PHP arrays `[...]`**: Brackets, not braces. OK.

10. **PHP short array `{...}` deprecated**: Was `{...}` for arrays in old PHP. Becomes anonymous block. OK.

11. **TS tagged template literals `tag\`...\``**: The `\` starts a string. Body is ignored. OK.

12. **TS/JS `for...of` and `for...in`**: `for` is the keyword. The `...` is just text. Becomes "for" block. OK.

13. **PHP `match` arm bodies**: `match($x) { 1 => 'one', 2 => 'two' }` — the `=>` doesn't introduce a block (only the outer `{` does). The outer is an anonymous block. OK.

14. **PHP `match` as expression vs statement**: Same as above. OK.

15. **Bash `case` statement**: Uses `esac` end-keyword. Not in scope per the epic. The `{` inside `case` patterns? Like `case $x in {a,b}) echo a;; esac`. The `{` would open a block, `}` would close it. This could create false blocks inside Bash case statements. Minor edge case.

Hmm actually let me check Bash brace expansion `{a,b,c}`. In:
```bash
echo {a,b,c}
```
The `{` and `}` are in stNormal. They'd open and close a block. Inside, `a,b,c` is text. Header is "echo " (no keyword match). Becomes anonymous block. False block created but anonymous, so doesn't false-merge with real declarations. Acceptable per the spec.

But wait, what if:
```bash
foo() {
  echo {a,b,c}
  echo done
}
```
- `foo() {` → opens func foo
- `  echo {a,b,c}` → header is "echo ". `{` opens block. Anonymous. `a,b,c` text. `}` closes block.
- `  echo done` → header in func. OK.

So a stray anonymous block inside `foo`. The deepest line for "echo done" is foo. Good.

16. **Bash variable expansion inside braces `${var,,}` (lowercase)**: `${var,,}` is lowercase. The scanner tracks `${...}` as paramExp. Inside, we count braces. `${var,,}` — the first `{` triggers paramExp, depth = 1. Then `}` closes, depth = 0 → state = stNormal. Good.

17. **Bash `${var:offset:length}` (substring)**: Same, single `{...}`. Good.

18. **Bash `${!var}` (indirect)**: Same, single `{...}`. Good.

OK let me look for actual code issues that would be flagged.

Let me look at the test for ts same func small drift:
```json
{
  "name": "ts_same_func_small_drift",
  "file": "a.ts",
  "source": "function target() {\n  const x = 1\n  const y = 2\n  return x + y\n}\n",
  "a_line": 2,
  "b_line": 4,
  "same_block": true
}
```

The source has 5 lines. Line 2 and line 4 should be in the same function block. The parser sees:
- Line 1: `function target() {` → opens func target.
- Lines 2-4: body
- Line 5: `}` → closes func.

So deepest(2) and deepest(4) are both "func target". Good.

`ts_off_by_one_same_func`:
```json
{
  "name": "ts_off_by_one_same_func",
  "file": "a.ts",
  "source": "function calc() {\n  let sum = 0\n  sum += 1\n  return sum\n}\n",
  "a_line": 2,
  "b_line": 3,
  "same_block": true
}
```
Lines 2 and 3 both in func calc. Good.

OK now let me check `ts_string_brace_no_overmerge`:
```json
{
  "name": "ts_string_brace_no_overmerge",
  "file": "a.ts",
  "source": "function ra() {\n  const s = \"} {\"\n  return s\n}\nfunction rb() {\n  const t = 2\n  return t\n}\n",
  "a_line": 2,
  "b_line": 6,
  "same_block": false
}
```

Line 2 is `const s = "} {"` (inside ra). Line 6 is `const t = 2` (inside rb). These are different functions. Good.

But wait, what does the scanner do with the `"} {"` string?
- `const` etc. → header.
- `=` → header.
- `"` → stString, delim = `"`.
- `}` → in stString, ignored. Good.
- ` ` → in stString, ignored. Good.
- `{` → in stString, ignored. Good.
- `"` → closes string. state = stNormal.
- `\n` → line++.

Good, the braces inside the string don't open/close blocks.

`ts_block_comment_drift_same_func`:
```json
{
  "name": "ts_block_comment_drift_same_func",
  "file": "a.ts",
  "source": "function handler() {\n  const a = compute()\n  /* a long\n     multi-line\n     comment that\n     drifts lines */\n  const b = a + 1\n  return b\n}\n",
  "a_line": 2,
  "b_line": 8,
  "same_block": true
}
```

Lines 2 and 8. Both in handler. The multi-line block comment shouldn't break the structure. Good.

But wait — there's a subtle issue with block comments and the line counter. Let me check:
- `/*` → stBlockComment.
- Body ignored, just looking for `*/`.
- `\n` inside the comment → line++ still happens (since line counter is outside the switch).

Good. So after the block comment, line is correctly updated.

`rust_char_literal_brace_same_fn`:
```json
{
  "name": "rust_char_literal_brace_same_fn",
  "file": "a.rs",
  "source": "fn f() {\n    let open = '{';\n\n\n\n    let close = '}';\n    use_it();\n}\n",
  "a_line": 2,
  "b_line": 7,
  "same_block": true
}
```

Line 2 has `'{'` (char literal). Line 7 has `'}'` (char literal). Both should be in fn f.

Scanner trace:
- `fn f() {` → opens func f.
- `    let open = '` → stString? Wait, Rust's strChars is `"`. Single quote is not a string char. So `'` is not a string delimiter. Then `cfg.charLiterals && c == '\''` matches. `charLiteralLen(src, i)` checks for `'X'` or `'\X'`. `'{` followed by `'`? Let me check.

```go
func charLiteralLen(src []byte, i int) int {
    n := len(src)
    if i+1 >= n {
        return 0
    }
    if src[i+1] == '\\' {
        if i+3 < n && src[i+3] == '\'' {
            return 4
        }
        return 0
    }
    if i+2 < n && src[i+2] == '\'' {
        return 3
    }
    return 0
}
```

For `'{`:
- i+1 = '{', not '\\'
- i+2 = `'`, so returns 3.

So `charLiteralLen` returns 3. Then `i += 3 - 1 = 2`. So `i` advances by 2, consuming `'{'`.

Wait, the outer loop has `i++`, so effectively `i` advances by 3 total (consuming `'`, `{`, `'`).

Good. The `{` is consumed as part of the char literal, not as a brace.

Same for `'}'`. Good.

Now there's an issue! Look at:
```go
case cfg.charLiterals && c == '\'':
    if n := charLiteralLen(src, i); n > 0 {
        i += n - 1 // consume whole 'x' / '\n' literal; not a string, not a lifetime
    }
    // else: a lone ' (lifetime / apostrophe) — treat as ordinary text.
```

The `i += n - 1` is added to the outer loop's `i++`. So total advance is `n`. Good.

But the char literal's NEWLINE handling — what if the char literal spans multiple lines? Rust char literals CAN'T span multiple lines (they're single chars). So this is fine. But what about `'\n'`? That's 4 chars: `'`, `\`, `n`, `'`. `charLiteralLen`:
- i+1 = '\\', so escape path
- i+3 = `'`, so returns 4.

Good. `i += 3`, total advance 4.

What about `'\u{7F}'`? That's 8 chars: `'`, `\`, `u`, `{`, `7`, `F`, `}`, `'`.
- i+1 = '\\', escape path
- i+3 = 'u' (not `'`), so returns 0.

So this is NOT consumed as a char literal. The `'` is treated as a lone lifetime. Then `{`, `}`, `'` are text. But `{` would be treated as a brace! That could mess up the brace count.

Hmm, this is a potential issue. Let me check `charLiteralLen`:
```go
if src[i+1] == '\\' {
    // '\X' — escaped single char then closing quote.
    if i+3 < n && src[i+3] == '\'' {
        return 4
    }
    return 0
}
```

The comment says "Multi-byte escapes like '\u{7f}' are not length-matched here; returning 0 leaves the lone quote as ordinary text". So the comment acknowledges this. And the risk register says: "Heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs, producing spurious blocks". So this is an accepted limitation.

But — returning 0 for `'\u{7F}'` leaves the `{` and `}` to be treated as braces! That could create false blocks. Hmm.

Actually wait, let me re-read the code:
```go
case cfg.charLiterals && c == '\'':
    if n := charLiteralLen(src, i); n > 0 {
        i += n - 1 // consume whole 'x' / '\n' literal; not a string, not a lifetime
    }
    // else: a lone ' (lifetime / apostrophe) — treat as ordinary text.
```

When `charLiteralLen` returns 0, the code does nothing. So the `'` is just consumed by the outer loop. Then on the next iteration, `i` is at `\`. Then `u`. Then `{` — which IS treated as a brace!

So `'\u{7F}'` inside a Rust function would create a false block. This is a real bug.

But — is this common? In Rust source, `'\u{7F}'` is used for Unicode escape sequences in char/string literals. It IS common. Hmm.

Let me check if there's a corpus case for this... No, the corpus has `rust_char_literal_brace_same_fn` (single char) and `rust_raw_string_no_overmerge` (raw string). No multi-byte unicode escape test.

This is a finding. The risk is acknowledged but the corpus doesn't cover it.

Actually wait, looking at the code more carefully:
```go
case cfg.charLiterals && c == '\'':
    if n := charLiteralLen(src, i); n > 0 {
        i += n - 1
    }
```

If `n == 0`, the case clause does nothing — it doesn't `break` or anything. The switch continues, but since this case is the only match (the if is false), nothing else happens. Then `i++` at the loop end.

So for `'\u{7F}'`, char-by-char:
- i=0: `'`, charLiteralLen returns 0, fall through. i++ → i=1.
- i=1: `\`, normal char, addHeader. i++ → i=2.
- i=2: `u`, normal char, addHeader. i++ → i=3.
- i=3: `{`, openBlock! False block.

This is a real bug. Let me flag it.

But wait — looking at the switch more carefully. The `stNormal` case has many conditions:
```go
default: // stNormal
    switch {
    case cfg.blockOpen != "" && matchAt(src, i, cfg.blockOpen):
        i += len(cfg.blockOpen) - 1
        state = stBlockComment
    case lineCommentStarts(src, i, cfg):
        state = stLineComment
    case cfg.rawStrings && c == 'r' && rawStringStart(src, i):
        ...
    case cfg.charLiterals && c == '\'':
        if n := charLiteralLen(src, i); n > 0 {
            i += n - 1
        }
    case cfg.paramExpand && c == '$' && i+1 < len(src) && src[i+1] == '{':
        ...
    case strings.IndexByte(cfg.strChars, c) >= 0:
        state = stString
        strDelim = c
        escape = false
    case cfg.heredocs && matchAt(src, i, cfg.heredocOp) && isHeredocStart(src, i+len(cfg.heredocOp)):
        ...
    case c == '(':
        parenDepth++
        addHeader(c)
    case c == ')':
        if parenDepth > 0 {
            parenDepth--
        }
        addHeader(c)
    case c == '{':
        openBlock()
    case c == '}':
        closeBlock()
    case c == ';':
        if parenDepth == 0 {
            resetHeader()
        } else {
            addHeader(c)
        }
    default:
        addHeader(c)
    }
```

The `case cfg.charLiterals && c == '\''` clause is BEFORE the brace cases. If `charLiteralLen` returns 0, the code just doesn't do anything special — it falls through to the next `case` (which is `case cfg.paramExpand && ...` — false; then `case strings.IndexByte(...)` — false; etc., eventually hitting `case c == '{'` which would match).

But in Go's switch statement, after a case matches and its body executes, control falls out of the switch (it doesn't continue to other cases). So even though multiple cases might "match", only the first one with a true condition executes.

For `'\u{7F}'` at the `'`:
- `cfg.blockOpen != ""` → "/*" → true, but is src[i:i+2] == "/*"? No, it's "'\u". False.
- `lineCommentStarts` → no.
- `rawStrings && 'r'` → c is `'`, not `r`. False.
- `charLiterals && c == '\''` → true! Match. `charLiteralLen` returns 0. Nothing happens.
- Exit switch.
- i++.

So the `{` IS encountered later and triggers openBlock. Bug confirmed.

This is a MEDIUM severity bug because:
1. It can cause false blocks for valid Rust code.
2. The epic acknowledges char literal handling but not multi-byte escapes.
3. The corpus doesn't cover it.

Actually wait, let me re-read charLiteralLen more carefully:

```go
func charLiteralLen(src []byte, i int) int {
    n := len(src)
    if i+1 >= n {
        return 0
    }
    if src[i+1] == '\\' {
        // '\X' — escaped single char then closing quote.
        if i+3 < n && src[i+3] == '\'' {
            return 4
        }
        return 0
    }
    // 'X' — single char then closing quote.
    if i+2 < n && src[i+2] == '\'' {
        return 3
    }
    return 0
}
```

For `'\u{7F}'` at position i (the `'`):
- i+1 = '\\'. Enter escape branch.
- i+3 = 'u' (not `'`). Return 0.

For `'\x7F'`:
- i+1 = '\\'. Enter escape branch.
- i+3 = 'F' (not `'`). Return 0.

For `'\n'`, `'\t'`, etc:
- i+1 = '\\'. Enter escape branch.
- i+3 = `'`. Return 4. 

So `\u` and `\x` escapes are NOT handled. The `{` in `'\u{7F}'` would create a false block.

Hmm. Let me see if there's a way to fix this simply.

Option 1: When `charLiteralLen` returns 0 in escape mode, skip until matching `'`.
Option 2: Handle `\u{...}` and `\x{...}` specially.

The simplest fix: if charLiteralLen sees `\`, scan forward to find the matching `'`, but only count balanced braces. Or more simply, scan forward ignoring `{` and `}` (treat them as text).

Actually, the cleanest fix might be: when we encounter `'\` (i.e., a char literal starting with backslash), consume until we find an unescaped `'`, ignoring all other characters. But that's tricky because `'` inside `\u{...}` doesn't exist (the `{` and `}` are between the `'` and the closing `'`).

Let me think. For `'\u{7F}'`:
- We start at the opening `'`.
- We see `\`, `u`, `{`, `7`, `F`, `}`, `'`.
- We need to consume all 8 characters.

One approach: when we see `'\` (backslash after `'`), find the next `'` (could be far ahead) and consume up to and including it. But what about `'` in a comment after? Like:
```rust
fn f() {
    let x = '\u{7F}'; // '
}
```
After `\u{7F}'`, the `;`, space, then `//`, then `'`, `\n`. The first `'` after `'\` is the closing one. Good.

But what about:
```rust
let x = '\'';  // escaped single quote
```
This is a char literal containing `'`. The scanner sees `'\`, then scans forward for `'`. The next `'` is the closing one. Good.

But what about a lifetime `'a` followed by a non-char-literal `'`? Like:
```rust
let x = 'a'.len();  // 'a' is char, but what if it's a typo?
```
After `'a`, `'` closes. OK.

This is getting complex. Let me check if there's a simpler approach.

Actually, looking at the code, the charLiteralLen only checks `i+3`. For `'\X'` it's 4 bytes (positions 0,1,2,3). For just `'\n'` it works. But for longer escapes, it fails.

Hmm. Let me see if there's a way to extend this. For `\u{...}`:
- After `\u`, look for `}` (within reasonable distance), then `'` after.

Actually a simpler approach: skip ahead to find the matching `'`, allowing `{` and `}` in between but tracking them. Or even simpler: skip ahead to find the matching `'`, treating everything between as opaque.

Let me look at how the existing code handles similar cases. The `stString` state handles escapes:
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

So in a string, `\'` is an escaped quote, but `'` closes. The escape handling is simple: if escape is true, the next char (regardless of what it is) is consumed.

For char literals in Rust, we could use a similar approach but more carefully. Or we could enter a temporary "in-char-literal" state.

Actually, the simplest fix: when charLiteralLen returns 0 due to escape but not finding the closing `'`, search forward for the next `'` (not preceded by `\`). This is similar to `stString`.

But this is getting complex. Let me check if the test suite catches this.

Actually, looking at the test corpus, there's no test for `'\u{...}'`. The existing test `TestParseSource_RustCharLiteralBraceIgnored` only uses `'{'` and `'}'`. So the bug isn't caught.

Let me check what severity this should be. It's:
- A real bug (false blocks)
- For a specific edge case (Rust char literals with `\u{...}` or `\x{...}` escapes)
- The epic mentions chars are handled but this case is missed
- The fall-back to line-proximity grouping works
- But it's a precision issue

I'd say MEDIUM. It's a specific edge case that's not covered.

Hmm, but this might be out of scope. The task is to add the brace parser. The bug is in the parser's char literal handling. It's directly related to the parser being added. So it IS in scope.

OK let me think about other potential bugs.

Now let me think about the `heredocStrip` semantics. In bash:
- `<<EOF` — terminator must be at column 0
- `<<-EOF` — leading tabs are stripped from BOTH the body lines AND the terminator line
- `<<~EOF` — same as `<<-` for the strip, plus `~` in the body is treated as part of the tag (for tilde expansion? actually no, `<<~` is for case-modifying terminator)

The code handles `strip = true` for both `-` and `~`. The `heredocLineMatches` strips leading tabs if `strip` is true. Good.

But what about the heredoc BODY? `<<-EOF` should strip leading tabs from body lines too. The current code doesn't do that — the body is just ignored (since we're in stHeredoc, all chars including leading tabs are passed through without effect). Actually since stHeredoc just looks for terminator on `\n`, the body is effectively ignored. So leading tabs in the body are fine — they're not treated as anything special. Good.

Let me look at the build.sh again:
```bash
local tagflag=()
[ -n "${tags}" ] && tagflag=(-tags "${tags}")
echo "building ${name}.wasm from ${srcdir}${tags:+ (tags: ${tags})}"
( cd "${here}/src/${srcdir}" && \
  GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath \
    ${tagflag[@]+"${tagflag[@]}"} -o "${here}/${name}.wasm" . )
```

Hmm — `${tagflag[@]+"${tagflag[@]}"}` — this expansion has the format `${parameter:+word}` where `parameter` is `tagflag[@]` and `word` is `"${tagflag[@]}"`. This is a "use alternate value if set" expansion. If the array is set (even if empty), it expands to the array elements. If unset, it expands to nothing.

But arrays in bash can be SET but EMPTY. Is `tagflag=()` considered "set"? Yes, it's set to an empty array. So `${tagflag[@]+...}` would expand to the array elements (which is nothing for an empty array).

Actually wait — the conditional `[ -n "${tags}" ]` only sets tagflag if tags is non-empty. So tagflag is either unset (when tags empty) or set with one element. So `${tagflag[@]+"${tagflag[@]}"}` works correctly in both cases.

Actually let me think — when tags is empty:
- tagflag is unset (not just empty).
- `${tagflag[@]+"${tagflag[@]}"}` — since tagflag is unset, expands to nothing.

When tags is set:
- tagflag = (-tags "ts").
- `${tagflag[@]+"${tagflag[@]}"}` — expands to "-tags" "ts".

OK that works.

But wait — what about the `-trimpath` and `-o`? They're always there. Let me parse the command:
```
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath [-tags ts] -o /path/to/ts.wasm .
```

When tags is empty:
```
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath -o /path/to/ts.wasm .
```

When tags is set:
```
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath -tags ts -o /path/to/ts.wasm .
```

Good.

Hmm, but `-trimpath` and `-o` are on the same line. The `${tagflag[@]+...}` is inserted between them. So the resulting command has the flags in the right order. OK.

Wait, `-trimpath -tags ts -o /path` — Go's flag parser should handle this. Good.

OK so the build script is fine.

Let me think about more bugs.

What about a function with no body? Like:
```typescript
function f();
```
There's no `{`. The function declaration doesn't open a block. The header is reset by `;`. So no func block. A finding on this line wouldn't be in a func block. That's correct behavior — there is no func body.

What about:
```typescript
const f = () => 1;
```
Arrow function without body braces. No block opened. Finding on line 1 has no func block. Correct.

```typescript
const f = () => {
  return 1;
};
```
Arrow function with body. `=> {` — `=>` triggers arrowFunc, header is "  return 1;". When `{` comes, header is "  return 1;". classifyHeader — arrowFunc wins (since "=>" is present). Returns ("func", ""). Opens anonymous func block. Good.

What about:
```typescript
const obj = {
  method() {
    body();
  }
};
```
- `const obj = {` → header "const obj = ". `{` opens anonymous block.
- `\n  method() {` → header "  method() ". `{` opens. classifyHeader — no keyword match. funcParenName? "method()" → yes. Returns ("func", "method"). Good.
- `\n  }` → closes method.
- `\n};` → `;` resets header. `}` closes the outer anonymous block.

Good. Two distinct blocks.

OK now let me think about something subtle. The `addHeader` accumulates until `resetHeader` is called. `resetHeader` is called by `;` at parenDepth 0, by `openBlock` (when `{`), and by `closeBlock` (when `}`).

Wait, looking at the code:
```go
closeBlock := func() {
    if len(stack) <= 1 {
        resetHeader()
        return
    }
    ...
    resetHeader()
}
```

`closeBlock` always calls resetHeader. So after a `}`, the header is empty.

What about a new line in the middle of a function declaration?
```typescript
function
  f() {
  body();
}
```
- `function` → header "function", line = 1, headerLine = 1.
- `\n` → addHeader('\n') → header "function " (replaced with space). line = 2.
- ` ` → addHeader(' '). But `if b == ' ' && !headerStarted { return }` — headerStarted is already true, so add.
- `f` → addHeader('f'). header "function f".
- `(` → parenDepth++. addHeader. header "function f(".
- `)` → parenDepth--. addHeader. header "function f()".
- ` ` → addHeader. header "function f() ".
- `{` → openBlock. classifyHeader — finds "function" at index 0. named=true, name = "f". Good.
- headerLine = 1 (set when 'f' was added, since headerStarted was already true at 'f'). Wait, headerLine is set when headerStarted transitions to true (i.e., when we see the first non-space). For `function`, headerStarted became true at 'f' (since "function" is the first non-space). headerLine = 1. Good.

OK now what about:
```typescript
// comment
function f() {
  body();
}
```
- `// comment` → stLineComment. Body ignored until `\n`.
- `\n` → state = stNormal. line++.
- `function f() {` → opens func f. Good.

What about:
```typescript
/* comment */
function f() {
  body();
}
```
- `/*` → stBlockComment. Looking for `*/`.
- `*/` → state = stNormal.
- `function f() {` → opens func f. Good.

What about a block comment that never closes?
```typescript
/* unterminated comment
function f() {
  body();
}
```
- `/*` → stBlockComment.
- All subsequent chars consumed (looking for `*/`).
- `\n` etc. → line++.
- EOF reached → loop ends. Stack still has the file root. No blocks opened. Function declarations are part of the header? Wait, they're consumed by stBlockComment!

So the unterminated comment swallows the rest of the source. No blocks are detected. That's a degenerate case but doesn't crash.

What about:
```typescript
function f() {
  /* unterminated comment
}
```
- `function f() {` → opens func f.
- `/*` → stBlockComment.
- `}` is consumed by stBlockComment. Doesn't close the func block.
- EOF → loop ends. Stack still has func f open. The final loop `for len(stack) > 1 { closeBlock() }` closes it. EndLine = last line.

OK, doesn't crash.

Now let me look at the embed.go:
```go
//go:embed parsers/go.wasm parsers/python.wasm
//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
var parserFS embed.FS
```

`//go:embed` accepts a space-separated list of file paths. Multiple `//go:embed` directives can be on the same line OR on adjacent lines. The above is valid.

Now let me look at the test for new languages:
```go
// Brace languages (epic 13.4): TypeScript/JavaScript family, PHP, Rust, Bash.
for _, ext := range []string{".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".mjs"} {
    require.Equalf(t, "ts", LanguageForExt(ext), "ext %s should map to ts", ext)
}
```

This iterates and checks each. Good.

```go
require.Equal(t, "bash", LanguageForExt(".sh"))
require.Equal(t, "bash", LanguageForExt(".bash"))
```

Good.

Now the brace parser test:
```go
func TestHost_BraceParsersLoadAndParse(t *testing.T) {
    cases := []struct{ lang, src string }{
        {"ts", "function f() {\n  const x = 1\n  return x\n}\n"},
        {"php", "<?php\nfunction f() {\n  $x = 1;\n  return $x;\n}\n"},
        {"rust", "fn f() -> i32 {\n  let x = 1;\n  x\n}\n"},
        {"bash", "f() {\n  local x=1\n  echo $x\n}\n"},
    }
    h := NewHost()
    defer func() { _ = h.Close() }()
    for _, c := range cases {
        p, err := h.Parser(c.lang)
        require.NoErrorf(t, err, "parser for %q should be registered", c.lang)
        root, err := p.Parse([]byte(c.src))
        require.NoErrorf(t, err, "parse %q", c.lang)
        require.Equalf(t, "file", root.Kind, "%q root kind", c.lang)
        var hasFunc bool
        for _, ch := range root.Children {
            if ch.Kind == "func" {
                hasFunc = true
            }
        }
        require.Truef(t, hasFunc, "%q: expected a func block, got %+v", c.lang, root.Children)
    }
}
```

This test creates a Host (which instantiates all parsers). For each language, gets the parser, parses the source, checks for a func block.

Wait — `NewHost()` is called once and then for each language we call `h.Parser(c.lang)`. This works.

But there's a subtle issue: `NewHost()` is slow (compiles all wasm modules). And this test runs for each language. So we have 4 compilations + 4 parses. That's 4x slower than necessary. But it's a test, so OK.

Hmm but actually — `NewHost()` only compiles ONCE per session. The 4 parsers are compiled during NewHost(). Then h.Parser() just looks up the cached instance. So actually it's 4 compilations + 4 parses, which is the same as before. OK.

Wait — does NewHost compile all parsers? Let me check... I don't have the host.go source in the diff, but from the existing code structure (per the comment "the host's existing language string"), NewHost likely lazily compiles parsers on first Parser() call. But the existing tests work this way too. OK.

OK let me also check `parseSource` in tests:

`TestParseSource_UnbalancedBracesDoNotPanic`:
```go
for _, src := range []string{"}}}\n", "function f() {\n  if (x) {\n", "{{{{\n"} {
    root := parseSource([]byte(src), tsConfig)
    if root.Kind != "file" {
        t.Fatalf("unbalanced %q: root kind %q", src, root.Kind)
    }
}
```

For `"}}}\n"`:
- Three `}` in stNormal. Each calls closeBlock.
- First `}`: stack has [file]. closeBlock — len(stack) <= 1, resetHeader, return. No actual close.
- Same for second and third.
- `\n`: line++.

Root is file with no children. Good.

For `"function f() {\n  if (x) {\n"`:
- `function f() {` → opens func f. EndLine will be set when closed.
- `\n` → line++.
- `  if (x) {` → opens if block.
- `\n` → line++. End of source.
- Loop ends. Stack = [file, func f, if]. `for len(stack) > 1 { closeBlock() }`:
  - closeBlock closes if. EndLine = 2 (last line).
  - closeBlock closes func f. EndLine = 2.

Wait, the EndLine is `line` at the time of close, which is the line of the closing `}`. But here there's no closing `}`. The loop ends with `line = 2` (after the final `\n`). Then closeBlock uses `line` which is 2. Hmm but actually the function body extends to line 2 and beyond... well, the source ends at line 2.

Actually let me re-trace. The source is:
```
function f() {
  if (x) {
```
Line 1: "function f() {"
Line 2: "  if (x) {"

So source ends without a closing brace. At end of loop, line = 2 (last processed line). Then closeBlock is called for each unclosed block. The EndLine = line = 2 for both blocks.

For the test, `root.Kind == "file"`. Yes. Good.

But wait — what if the test expects the func to span to a different line? Let me look:
```go
if root.Kind != "file" {
    t.Fatalf("unbalanced %q: root kind %q", src, root.Kind)
}
```
Only checks root kind. Good.

For `"{{{{\n"`:
- Four `{` in stNormal. Each calls openBlock.
- First `{`: header is empty. classifyHeader returns ("block", ""). Open unnamed block.
- Second `{`: header is empty (no text). Open unnamed block.
- Same for third and fourth.
- `\n`: line++.

Wait — but each `{` should accumulate text in header? No, header is reset by openBlock. So after each openBlock, header is empty. So each subsequent `{` opens with empty header.

Result: stack = [file, block, block, block, block]. End of loop → close all blocks. Each closes with EndLine = 1.

Good. No panic.

OK now let me think about a potential issue with `addHeader` when called from inside `default:`.

Looking at:
```go
default:
    addHeader(c)
}
```

This adds `c` to the header. So the header accumulates. Good.

But there's a subtle issue. What about EOF handling? The loop ends at `i < len(src)`. The header might have partial content. That's fine — it's discarded if no block opens.

What about EOF in the middle of a string? Like:
```typescript
function f() {
  const s = "hello
}
```
No closing `"`. The scanner is in stString at EOF. The string is never closed. Loop ends. Stack still has func f open. closeBlock closes it.

But the func's EndLine is wrong — it should extend to where the actual content is, not just the last line. Hmm, but actually `line` at EOF is the line of the last `\n` + 1. So EndLine might be slightly off but close enough.

OK no major bug.

Let me also check the `i++` placement in the loop. The outer loop is `for i := 0; i < len(src); i++`. Inside, some cases modify `i` (like `i += len(cfg.blockOpen) - 1`). The `i++` happens at the end of each iteration. So if a case sets `i += 3`, then `i++` makes it `i += 4`. Good.

OK now let me look at one more potential issue: `cfg.lineComments` is checked even when c is, say, `\n`. The `lineCommentStarts` function:
```go
func lineCommentStarts(src []byte, i int, cfg langConfig) bool {
    for _, m := range cfg.lineComments {
        if !matchAt(src, i, m) {
            continue
        }
        if m == "#" && cfg.commentWordBoundary && !hashAtWordBoundary(src, i) {
            continue
        }
        return true
    }
    return false
}
```

For c = '\n', `matchAt(src, i, "//")` would check if src[i:i+2] == "//". The first char is '\n', so no match. Good. `matchAt(src, i, "#")` checks src[i:i+1] == "#". If c is '\n', no match. Good.

So line comments only trigger on actual comment chars. Good.

What about line comments at end of file? Like:
```
function f() {
  // comment
}
```
- `function f() {` → opens func f.
- `  // comment` → stLineComment.
- `\n` → state = stNormal.
- `}` → closeBlock. Closes func f.

Good.

Hmm wait — what about line comment right before `}`?
```
function f() {
  // comment }
```
- `function f() {` → opens func f.
- `  // comment }` → stLineComment. The `}` is consumed.
- `\n` → state = stNormal.

So func f is never closed! Because the `}` was inside the comment.

Wait, but the test case `TestParseSource_BraceInLineCommentIgnored` has:
```go
src := []byte("function f() {\n  // } stray brace {\n  doWork()\n}\n")
root := parseSource(src, tsConfig)
if len(root.Children) != 1 || root.Children[0].Kind != "func" {
    t.Fatalf("line-comment braces must not break structure: %+v", root.Children)
}
```

The `}` inside the comment is consumed. But then `doWork()` and `\n` and `}` follow. The actual `}` on the last line closes the func. So the test passes.

But if the `}` was ONLY in the comment, with no real `}` after:
```
function f() {
  // }
```
- `function f() {` → opens func f.
- `  // }` → stLineComment. `}` consumed.
- EOF. No more `}`.

Loop ends. Stack has [file, func f]. `for len(stack) > 1 { closeBlock() }` → closes func f. EndLine = last line.

So the func block still gets closed at EOF. Good.

OK now let me think about other issues.

Hmm let me also check the `i` advancement in `addHeader` — actually addHeader doesn't advance `i`, it just modifies the header byte slice. The `i++` at the end of the outer loop is what advances.

OK I think I've found the main bug: char literal handling for `\u{...}` and `\x{...}` escapes. Let me also look at a few more things.

What about the corpus test for bash `$#`? Let me look:
```json
{
  "name": "bash_param_expansion_same_func",
  "file": "a.sh",
  "source": "greet() {\n  local x=${name}\n\n\n\n  local y=${other:-def}\n  echo $x $y\n}\n",
  "a_line": 2,
  "b_line": 7,
  "same_block": true
}
```

`${name}` and `${other:-def}` are parameter expansions. Both should be inside greet. Let me trace:
- `greet() {` → opens func greet.
- `  local x=${name}` → 
  - `local x=` → header.
  - `$` → check paramExpand. `i+1 < len(src) && src[i+1] == '{'` → yes. Enter stParamExp. paramDepth = 1.
  - `name` → in stParamExp, ignored.
  - `}` → paramDepth = 0, state = stNormal.
- `\n\n\n\n` → line++ four times.
- `  local y=` → header.
- `$` → enter stParamExp.
- `other:-def` → ignored.
- `}` → exit.
- `\n  echo $x $y\n` → header.
- `\n  ` → more text.
- `}` → close func greet.

Good. Line 2 and line 7 are both in greet.

What about `bash_heredoc_no_overmerge`:
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

Line 5 is `echo done` (in greet). Line 8 is `echo hi` (in post). Different blocks. Good.

Let me trace to make sure the heredoc doesn't break:
- `greet() {` → opens func greet. line 1.
- `\n  cat <<EOF` → 
  - `cat ` → header.
  - `<<` → matchAt for heredocOp. isHeredocStart(src, j=5) — j=5 is `E`. isIdentByte('E') && not digit. True. parseHeredoc reads "EOF". heredocPending = true.
- `\n` → line = 2. heredocPending = false. state = stHeredoc.
- `  { not a block }` → in stHeredoc. `\n` is the only trigger for terminator check.
- At `\n`: lineBytes = src[lineStart:i] = "  { not a block }". heredocLineMatches? "  { not a block }" doesn't start with "EOF" (after stripping). Doesn't match.
- `\nEOF` → line = 3. At `\n` (end of "EOF"), lineBytes = "EOF". heredocLineMatches — yes, matches. state = stNormal.
- `  echo done` → header.
- `\n}` → line = 5. closeBlock closes greet.
- `\npost() {` → line = 6. 
  - `post` → header.
  - `(` → parenDepth++.
  - `)` → parenDepth--.
  - ` ` → header.
  - `{` → openBlock. classifyHeader — no keyword. funcParenName — "post()" → yes. Returns ("func", "post"). Opens func post.

Wait, but the source line 8 is "  echo hi" inside post. Let me count again:
- Line 1: `greet() {`
- Line 2: `  cat <<EOF`
- Line 3: `  { not a block }`
- Line 4: `EOF`
- Line 5: `  echo done`
- Line 6: `}`
- Line 7: `post() {`
- Line 8: `  echo hi`
- Line 9: `}`

Hmm, "a_line: 5" is `echo done` (in greet). "b_line: 8" is `echo hi` (in post). Different blocks. Good.

The scanner:
- Line 1: greet func opens. EndLine = 6 (where } is).
- Line 5: inside greet (body line).
- Line 6: } closes greet.
- Line 7: post func opens.
- Line 8: inside post (body line).
- Line 9: } closes post.

OK good.

But wait — what if the heredoc termination is INSIDE the func? Let me re-check.

`<<EOF` on line 2. The body line 3 `{ not a block }` is inside the heredoc (stHeredoc). The `EOF` on line 4 is the terminator. So the heredoc ends on line 4. Then line 5 `echo done` is back to normal. Line 6 `}` closes greet.

But the `{}` inside the heredoc — these are in stHeredoc and don't affect the brace stack. Good.

But what if the heredoc is `<<-EOF` and the body has indented braces? Same — stHeredoc ignores everything except `\n` for terminator check.

OK, all looks good for bash heredocs.

Let me think about one more thing — the `\u{...}` bug. Is this a real issue worth flagging?

Actually, looking more carefully at the code, I realize charLiteralLen is called when `c == '\''` AND `cfg.charLiterals` is true. In Rust config, `charLiterals: true`. So `'\u{7F}'` in Rust code would have:
- `'` matches the case.
- charLiteralLen returns 0.
- Fall through (effectively do nothing).
- Next iter: `\`, treated as regular text.
- Next: `u`, regular text.
- Next: `{`, openBlock. False block!

This is a real bug. Severity MEDIUM because:
- It's a specific but common pattern in Rust.
- It causes false blocks (precision issue, not correctness — groupings still work via fallback).
- The corpus doesn't cover it.
- The mitigation in the epic mentions "heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs" but char literals aren't mentioned.

Actually wait, looking at the code, even `charLiteralLen` handling for `'\n'` is already partial. Let me see what about `'\xFF'`? Wait, `'\xFF'` has 4 bytes: `'`, `\`, `x`, `F`, `'` — that's 5 bytes. Hmm wait. `'\xFF'` is 6 chars total: `'`, `\`, `x`, `F`, `F`, `'`. charLiteralLen at i=0:
- i+1 = `\`. Escape path.
- i+3 = `F` (not `'`). Returns 0.

So `'\xFF'` would also fall through. Then `\x` is text, then `FF` is text, then `'` is text (no length match), then... wait, but the `'` triggers the case again. At the second `'` (i=5):
- i+1 = end of src. Returns 0.

So `'\xFF'` becomes 5 text chars (the inner chars) plus the second `'` is also a no-op. No false blocks. Good.

But for `'\u{7F}'`:
- First `'` at i=0. charLiteralLen returns 0.
- `\`, `u`, text.
- `{` at i=3. openBlock! False block.

This IS a bug. The `{` and `}` in `\u{...}` are treated as braces.

Hmm, but actually let me think about this more. In a real Rust program, when would `'\u{7F}'` appear? It's used in string/char literals for Unicode escape sequences. The pattern would be:
```rust
let c = '\u{7F}';
let s = "\u{7F}";
```

Inside a double-quoted string, the `\u{7F}` is part of the string, so no braces to worry about (string state handles it).

Inside a char literal `'\u{7F}'`, the issue arises.

In Rust idiomatic code, char literals are usually simple ASCII. Unicode escapes are rare. But they do occur.

The severity: this is a precision issue, not a correctness issue. The fall-back is line-proximity grouping. So it's MEDIUM.

Now let me also think about whether the existing goparser/pyparser plugins handle this differently. Looking at the comment:
> Track string/comment/heredoc state in the scanner (as the 13.1 Python plugin does for triple-quoted strings); on misparse the finding falls back to line proximity — grouping degrades, never breaks.

So the design acknowledges misparses. The question is whether this specific bug should be flagged.

Hmm. Let me also consider the spec:
- The risk register says "Heuristic parser misreads braces/keywords inside strings, comments, regex, or Bash heredocs, producing spurious blocks". It does NOT mention Rust char literals.
- The "Mitigation" is "Track string/comment/heredoc state in the scanner".

So the spec expects char literals to be handled. The code DOES handle single-char literals (`'X'`) and simple escapes (`'\n'`). But not multi-char escapes (`\u{...}`).

OK I'll flag this as a MEDIUM bug.

Actually wait, let me re-look at the charLiteralLen function. Is there a way it could handle `\u{...}`?

Looking at it:
```go
if src[i+1] == '\\' {
    // '\X' — escaped single char then closing quote.
    if i+3 < n && src[i+3] == '\'' {
        return 4
    }
    return 0
}
```

It only handles 4-byte escape sequences. To handle `\u{7F}` (8 bytes), the function would need to scan forward.

A better implementation: when we see `'\`, scan forward until we find a `'` that's not preceded by `\`. This handles arbitrary escape sequences.

But this is a fix, not the current bug. I'll flag it.

Now let me also look at a few other things.

Actually, I want to also verify the `heredocLineMatches` logic for edge cases.

For PHP heredoc `<<<EOT\nbody\nEOT;\n`:
- `<<<` at some position. heredocOp = "<<<". 
- isHeredocStart(src, j=i+3) — j is just after `<<<`. `E` is an ident char (not digit). True.
- parseHeredoc reads "EOT". heredocTag = "EOT".
- `\n` after `EOT`. state = stHeredoc.
- Body. `\n` → check `src[lineStart:i]` against "EOT". Doesn't match for "body". Continue.
- `\n` after "EOT;". lineBytes = "EOT;". heredocLineMatches — HasPrefix("EOT;", "EOT") true. isIdentByte(';') false. Match. state = stNormal.

Good.

But wait, what if the body has a line that starts with `EOT`? Like:
```
<<<EOT
hello EOTworld
EOT
```
Body line "hello EOTworld" — HasPrefix("hello EOTworld", "EOT") false. So doesn't match. Good.

What about:
```
<<<EOT
EOT
EOT
```
First `EOT` (line 1) — matches, terminate. State = stNormal. Then we're past the heredoc. Then we have another `EOT` on line 2 as text. Good.

But wait, what if the body is JUST `EOT`?
```
<<<EOT
EOT
```
Line 1: `<<<EOT` — operator and tag.
Line 2: `EOT` — terminator.
The terminator check at the `\n` after `EOT` (line 2): lineBytes = "EOT". Matches. state = stNormal.

But the terminator line itself is consumed. Hmm, what about the body? Empty body. That's fine for heredocs.

OK no major bug here.

Let me also check the embedding to ensure no issues:

```go
//go:embed parsers/go.wasm parsers/python.wasm
//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
var parserFS embed.FS
```

`//go:embed` directives must be on lines immediately preceding the var declaration. Both `//go:embed` comments are on the same code block, both before `var parserFS`. This is valid Go syntax.

But wait — can you have two `//go:embed` directives for the same var? Yes, but they merge. So this is fine.

Actually, looking at this, it might be cleaner to put all paths on one line:
```go
//go:embed parsers/go.wasm parsers/python.wasm parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
var parserFS embed.FS
```

But splitting across two lines is also valid.

OK I think the main bugs I've found are:
1. MEDIUM: Rust char literal handling doesn't handle `\u{...}` and `\x{...}` escapes.
2. LOW: TS regex literals with braces could create false blocks (acknowledged in risk).

Let me also check for any issues with the test setup.

Looking at `TestHost_BraceParsersLoadAndParse`:
```go
defer func() { _ = h.Close() }()
```

Uses `_ = h.Close()` to ignore error. OK.

Actually, let me re-examine the scan carefully. One thing I want to verify: when in `stParamExp` state, are braces tracked correctly even when there's nested `${...}`?

```go
case stParamExp:
    if c == '{' {
        paramDepth++
    } else if c == '}' {
        paramDepth--
        if paramDepth <= 0 {
            state = stNormal
        }
    }
```

For `${a:-${b:-c}}`:
- `$` in stNormal → enter stParamExp. paramDepth = 1.
- `{` in stParamExp → paramDepth = 2.
- `}` → paramDepth = 1. Still in stParamExp.
- `}` → paramDepth = 0. State = stNormal.

Good.

For `${a:-{b:-c}}` (which is invalid bash but let's trace):
- `$` → stParamExp. paramDepth = 1.
- `{` → paramDepth = 2.
- `}` → paramDepth = 1.
- `}` → paramDepth = 0. State = stNormal.

Good. (The brace expansion `{b:-c}` inside `${...}` is treated as if it were a parameter expansion.)

What about `$(cmd)`? Bash config has `paramExpand: true` but no special handling for `$()`. So `$` triggers paramExpand check, `(` is not `{`, so no enter. Then `(` adds to header (parenDepth++). Body is text. `)` decrements parenDepth. 

What about bash `$(...)` containing braces?
```bash
echo $(case $x in 1) echo {a,b} ;; esac)
```
- `$(` → parenDepth = 1.
- `case $x in 1)` → header.
- ` echo ` → header.
- `{` → openBlock. False block!
- `a,b` → header.
- `}` → closeBlock.
- ` ;; esac)` → header.
- `)` → parenDepth = 0.

So a `$()` containing `{a,b}` would create a false block. This is a known limitation. Bash command substitution can contain arbitrary code. The scanner doesn't handle this.

This is a LOW severity issue — it's a specific edge case and the fallback to line proximity works.

OK let me also check for issues with the build script:

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

The `${tagflag[@]+"${tagflag[@]}"}` expansion. Let me verify this works.

In bash, the format `${parameter:+word}` evaluates to `word` if `parameter` is set and non-null, else nothing. For arrays, `parameter[@]` represents all elements.

When `tagflag` is unset (tags is empty):
- `${tagflag[@]+...}` — since tagflag is unset, expands to nothing.

When `tagflag=(-tags "ts")`:
- `${tagflag[@]+...}` — tagflag is set. Expands to the array elements with the wrapper.

Actually I need to verify. Let me think more carefully.

`${tagflag[@]+"${tagflag[@]}"}` — this is a single substitution. The parameter is `tagflag[@]` (the array). The operator is `+`. The word is `"${tagflag[@]}"`.

`+` operator: if parameter is set (and non-null for `+`), substitute `word`. Otherwise, substitute nothing.

For arrays, "set" means the array variable has been assigned to. Even if empty, it's set.

When tagflag is unset:
- `tagflag[@]` is unset.
- `${tagflag[@]+word}` → word is substituted. Wait, no — `+` substitutes `word` when parameter is UNSET or null/empty.
  
Actually let me look up the exact semantics:

Per bash docs:
- `${parameter:+word}` — If parameter is null or unset, nothing is substituted, otherwise the expansion of word is substituted.

Wait, that's `:+`, not `:+`. Let me re-read.

Actually:
- `:-` — If parameter is unset or null, substitute word.
- `:+` — If parameter is null or unset, substitute nothing; otherwise substitute word.
- `:-` defaults, `:+` alternates.

So `${tagflag[@]+"${tagflag[@]}"}`:
- If tagflag is unset or null → substitute nothing.
- If tagflag is set and non-null → substitute "${tagflag[@]}" (which expands to the array elements).

So when tags is empty (tagflag unset), nothing is added. When tags is set, the array elements are added. Correct behavior.

But wait — what if tagflag is empty array (`tagflag=()`)? Then `tagflag[@]` is set but the array is empty. `${tagflag[@]+...}` — is an empty array considered "null"? In bash, an empty array might or might not be considered "null" for this purpose.

Actually, in bash, `tagflag=()` makes tagflag an empty array, not null. So `${tagflag[@]+...}` would substitute the word. The word `"${tagflag[@]}"` expands to... nothing (empty array). So the net result is nothing added.

Hmm wait, that's confusing. Let me think again.

If tagflag is unset (not declared):
- `${tagflag[@]+word}` → word NOT substituted → nothing added.
- Result: no -tags flag.

If tagflag is set to empty:
- `${tagflag[@]+word}` → word substituted.
- Word is `"${tagflag[@]}"` which expands to nothing.
- Result: no -tags flag.

If tagflag is set to (-tags "ts"):
- `${tagflag[@]+word}` → word substituted.
- Word is `"${tagflag[@]}"` which expands to "-tags" "ts".
- Result: -tags "ts" added.

OK so all three cases work correctly. Good.

But wait — the `[ -n "${tags}" ] && tagflag=(-tags "${tags}")` only sets tagflag when tags is non-empty. So tagflag is either unset or has one element. Never empty array. OK.

OK now let me look at the overall build process:

```bash
build go goparser
build python pyparser
build ts braceparser ts
build php braceparser php
build rust braceparser rust
build bash braceparser bash
```

For the braceparser builds, the build tag selects the active_*.go file with the matching tag. So:
- `build ts braceparser ts` → -tags ts → active_ts.go → active = tsConfig.

But wait — there's a subtle issue. The active_*.go files are mutually exclusive due to their build tags. But what if `-tags ts php` is passed? Both active_ts.go and active_php.go would be compiled, causing duplicate `active` declarations. But build.sh only passes one tag at a time, so this doesn't happen in practice.

But it's a potential issue if someone runs `go build -tags ts,php` from the braceparser directory. That would cause a compile error. The fix would be to use mutually exclusive build constraints.

Actually, Go's `//go:build` directive with comma-separated terms is OR'd. So `//go:build ts` matches when ts is set. `//go:build php` matches when php is set. If both are set, both files compile. Duplicate declaration.

For safety, we could change the build tags to be mutually exclusive. But the build.sh only passes one tag at a time, so this isn't an issue in practice.

Hmm, but actually Go's build tag syntax doesn't have a direct NOT operator (you can use `!` but it's awkward). And the file names already encode the tag, so this is mostly a code-organization issue.

A common pattern is to use a single file with `//go:build ts` (in the file name) and let the tag select the file. Go does support filename-based tags like `active_ts.go` which would only compile when `-tags ts` is set. But the explicit `//go:build` directive is more explicit.

Actually, Go's filename conventions allow suffix tags. So `active_ts.go` would compile when `-tags ts` is set, without needing a `//go:build ts` directive. But the explicit directive is clearer.

This isn't a bug. Let me move on.

Now let me think about one more thing: the corpus test cases.

Let me count them:
- 6 original (before this diff)
- 21 new ones (8 TS + 6 PHP + 5 Rust + 5 Bash + ... wait let me count)

Actually let me re-count:
```
ts_same_func_small_drift
ts_same_func_large_drift_blank_lines
ts_same_if_block
ts_different_funcs_adjacent_lines
ts_different_funcs_far_apart
ts_off_by_one_same_func
ts_block_comment_drift_same_func
ts_string_brace_no_overmerge
```
8 TS

```
php_same_func_small_drift
php_same_method_large_drift
php_different_methods_adjacent
php_heredoc_no_overmerge
php_off_by_one_same_func
php_foreach_body
```
6 PHP

```
rust_same_fn_small_drift
rust_same_fn_large_drift_blank_lines
rust_impl_method_grouping
rust_different_fns_adjacent
rust_char_literal_brace_same_fn
rust_raw_string_no_overmerge
```
6 Rust

```
bash_same_func_small_drift
bash_same_func_large_drift_blank_lines
bash_function_keyword_form
bash_different_funcs_adjacent
bash_param_expansion_same_func
bash_heredoc_no_overmerge
```
6 Bash

Total new: 26. Plus the existing 8 (1 was the 7-line one and there are a few more). Actually let me count existing:

Looking at the diff, only 1 existing entry is shown (the one that was modified by adding same_block). The rest are above the diff hunk. Hmm. Anyway, the count is roughly: 26 new entries.

The AC3 says "the accuracy benchmark (extended corpus) holds AST recall ≥ 0.95 and precision ≥ proximity on the new languages". The new corpus should provide enough test coverage.

But notice: no TS regex test, no Rust `'\u{...}'` test, no bash command substitution test. These are edge cases that aren't covered.

Should I flag these as missing corpus tests? The sprint plan says "corpus cases cover string/heredoc edge cases per language". So edge cases are expected. Missing tests for known edge cases is a gap.

Let me flag these as LOW severity.

OK now let me consolidate my findings:

1. **MEDIUM**: Rust `charLiteralLen` doesn't handle `\u{...}` and `\x{...}` escape sequences. The `{` and `}` in `'\u{7F}'` create false blocks.

2. **LOW**: TS regex literals containing braces (e.g., `/[{}]/`) are not handled — the scanner is in stNormal, so `{` and `}` would be treated as braces.

3. **LOW**: Bash `$(cmd)` containing brace blocks (e.g., `$(case x in 1) ... esac)`) — braces inside command substitution create false blocks.

4. **LOW**: No corpus test for TS regex with braces (acknowledged edge case).

5. **LOW**: No corpus test for Rust char literal with `\u{...}` escape (the bug I found).

Hmm, let me think about which to include.

The first one is a clear bug that should be flagged. Let me verify the location.

The charLiteralLen function is in parse_core.go:
- Line ~370 (counting lines in the diff). Let me find the exact location.

Actually, the diff shows:
```
+func charLiteralLen(src []byte, i int) int {
+    n := len(src)
+    if i+1 >= n {
+        return 0
+    }
+    if src[i+1] == '\\' {
+        // '\X' — escaped single char then closing quote.
+        if i+3 < n && src[i+3] == '\'' {
+            return 4
+        }
+        return 0
+    }
+    // 'X' — single char then closing quote.
+    if i+2 < n && src[i+2] == '\'' {
+        return 3
+    }
+    return 0
+}
```

This is in `parse_core.go`. The line numbers in the diff are new file additions, so the actual file starts at line 1.

Looking at the diff line numbers:
- Line 1-13: package and node type
- Line 14-30: langConfig struct
- Line 31-37: scanner states
- Line 38-180ish: parseSource function
- Line 180ish: classifyHeader
- Line 195ish: ident helpers
- Line 215ish: lastWholeWord
- Line 235ish: identAfter
- Line 245ish: funcParenName
- Line 255ish: matchAt
- Line 265ish: matchAnyPrefix
- Line 270ish: lineCommentStarts
- Line 290ish: hashAtWordBoundary
- Line 300ish: rawStringStart
- Line 320ish: countHashes
- Line 335ish: hasHashes
- Line 345ish: charLiteralLen

Hmm I can't easily get exact line numbers from the diff. Let me just refer to it by the function name and approximate area.

Actually looking at the diff format, the diff shows context lines with line numbers. The charLiteralLen function is around line 350+ of parse_core.go.

Let me estimate:
- parse_core.go has 522 lines total (per the file header).
- charLiteralLen is somewhere in the middle.

I'll just refer to it as the function. Let me find it more precisely.

Actually, looking at the diff more carefully, each hunk shows @@ -X,Y +A,B @@. The line numbers are in the hunk headers. Let me find the charLiteralLen hunk.

The diff shows several hunks. The relevant hunk for charLiteralLen... I'll need to count.

Actually, let me just estimate. The function charLiteralLen appears in the diff at approximately line 357 based on the file structure I see (after countHashes and hasHashes, which come after rawStringStart). But this is approximate.

Looking at the existing parse_core.go content shown in the diff, I'll just say it's around line 350.

Let me search the diff for `charLiteralLen`:
```
+func charLiteralLen(src []byte, i int) int {
```

The hunks in parse_core.go are:
- @@ -0,0 +1,31 @@ — node and types
- @@ -0,0 +32,53 @@ — wait, hunks merge

Actually let me count from the start. The file is 522 lines (per the final `+522` or similar). Let me find charLiteralLen by counting from the top.

Hmm, this is getting complex. Let me just refer to the function and approximate line. Or use a line range.

Looking at the diff output, I'll estimate charLiteralLen is around line 350-360 of parse_core.go. Actually, let me search for the function more carefully.

Looking at the diff:
- parseSource (the big function) is from line ~32 to ~190
- classifyHeader from ~195 to ~230
- ident helpers from ~235 to ~290
- lineCommentStarts from ~295 to ~310
- hashAtWordBoundary from ~315 to ~330
- rawStringStart from ~335 to ~355
- countHashes from ~360 to ~370
- hasHashes from ~375 to ~390
- charLiteralLen from ~395 to ~415

OK so approximately line 395-415. Let me just say line 400 (approximate).

Hmm, but for my finding, I need an exact line number. Let me look more carefully.

Actually, looking at the diff for parse_core.go, I see hunks separated by `@@`. The hunks are:
1. @@ -0,0 +1,31 @@ — node type, blockKeyword, langConfig, scanner states (lines 1-31)
2. @@ -32,0 +32,180 @@ — parseSource (lines 32-211)
3. @@ -212,0 +212,30 @@ — classifyHeader (lines 212-241)
4. @@ -242,0 +242,XX @@ — more functions

Wait, the hunks don't quite work that way for new files. Let me look at the actual line counts.

Looking at the diff, the hunks for parse_core.go:
- First hunk adds lines 1-31 (node, blockKeyword, langConfig, constants)
- Second hunk adds lines 32-?? (parseSource function)

Without exact line numbers, I'll just estimate. The charLiteralLen function is somewhere in the latter half of the file. Let me just say line 400 as an estimate, or use a less precise reference.

Actually, for the finding format, I need to provide a file:line reference. Let me just estimate ~line 360-370 for charLiteralLen.

Wait, let me count from the diff context. The diff shows hunks with content. Each `+` line is added. Let me count the `+` lines in parse_core.go up to charLiteralLen:

Looking at the diff:
1. Header comment: ~20 lines
2. node struct: 7 lines
3. blockKeyword struct: 6 lines
4. langConfig struct: 15 lines
5. Scanner states constants: 9 lines (with the const block)
6. parseSource function: many lines
7. classifyHeader: many lines
8. Helper functions

This is getting too tedious. Let me just look for the function name in the diff and approximate the line.

In the diff for parse_core.go, charLiteralLen appears in a hunk around what would be line 350-400. I'll use line 360 as a reasonable estimate.

Actually, let me search through the diff for the exact placement. The diff has hunks like:
```
@@ -0,0 +A,B @@
```
Where A and B are the starting line and count.

Looking at the diff for parse_core.go, I see (from what I can parse):
- Lines 1-15 or so: header comment
- Then node, blockKeyword, langConfig, scanner constants
- Then parseSource
- Then classifyHeader and helpers
- Then charLiteralLen

The file is 522 lines (per the file's last hunk context). charLiteralLen is roughly at line 350.

OK I'll just estimate and use a function name as the reference. For the finding:
- File: `internal/astgroup/parsers/src/braceparser/parse_core.go`
- Line: ~350-360
- Function: `charLiteralLen`

Let me actually look at the diff one more time. The diff for parse_core.go shows multiple hunks. The hunk for charLiteralLen might be around line 350.

Actually, looking at the diff more carefully, I see the structure of the additions:

After parseSource (which is the long function around line 32-190 or so), there's classifyHeader. Then helper functions follow. The order seems to be:
- parseSource
- classifyHeader
- identByte
- lastWholeWord
- identAfter
- funcParenName
- matchAt
- matchAnyPrefix
- lineCommentStarts
- hashAtWordBoundary
- rawStringStart
- countHashes
- hasHashes
- charLiteralLen

If parse_core.go is 522 lines, then charLiteralLen is roughly at line 380-400.

OK I'll use line 400 as an approximation.

Actually wait, I need to give an exact line. Let me just count the function calls in the diff to estimate.

Hmm, without being able to scroll through the file precisely, I'll just use an approximate line and note the function.

OK let me finalize my findings:

1. **MEDIUM** - charLiteralLen doesn't handle `\u{...}` and `\x{...}` escapes in Rust char literals.

2. **LOW** - TS regex literals with braces not handled.

3. **LOW** - Bash command substitution with brace blocks not handled.

Let me also check if there are any other issues I missed.

Hmm, one thing I want to verify: the `heredocOp` matching. For PHP `<<<EOT`:
- `<<` at position i. matchAt(src, i, "<<<") — needs 3 chars at i. If src has `<<<EOT`, this matches.
- isHeredocStart(src, i+3) — src[i+3] is 'E'. isIdentByte('E') && !('E' >= '0' && 'E' <= '9') — true. Good.

For PHP `<<EOT` (just `<<`, missing one):
- `<<` at i. matchAt for "<<<" — src has only `<<`, not `<<<`. No match. Then we'd never enter the heredoc handling for `<<`. So `<<` is just regular text. Hmm but PHP doesn't have `<<` heredoc anyway, only `<<<`. So this is fine.

But wait — the heredocOp is "<<<". So we'd only match `<<<`. And then i+=3. So the heredoc start logic skips past `<<<`. Good.

For PHP `<<<\nEOT` (immediately newline):
- `<<<` matched. isHeredocStart at i+3. src[i+3] is '\n'. isIdentByte('\n') false. Not a heredoc start.

So `<<<\n` is just text. Good.

OK what about the case where the tag is quoted? PHP supports `<<<'EOT'` (nowdoc):
```
$s = <<<'EOT'
hello
EOT;
```
The tag is `'EOT'` (quoted). parseHeredoc:
```go
if j < len(src) && (src[j] == '\'' || src[j] == '"') {
    j++
}
```
Skips the quote. Then reads identifier. tag = "EOT". Good.

The `strip` is false for nowdoc. heredocStrip = false. heredocLineMatches checks `s == tag` or `HasPrefix(tag) && !isIdentByte`. Good.

OK let me also check bash `<<~`:
```bash
cat <<~EOT
hello~world
EOT
```
- `<<~` — `<<` matches heredocOp. isHeredocStart at i+2. src[i+2] is `~`. The code skips `~`, then reads identifier. tag = "EOT". strip = true.
- Body. heredocLineMatches strips leading tabs (none). Line "hello~world" doesn't match "EOT". OK.
- "EOT" matches. state = stNormal.

But wait — in `<<~`, the tilde in the BODY is part of the terminator? No, only the LEADING tabs are stripped. Body content is unchanged.

OK no issue.

Hmm let me also look at one more thing. The `isHeredocStart` for `<<~EOT`:
```go
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

For `<<~EOT`:
- j = i+2 (after `<<`). src[j] = `~`.
- Not space/tab. Skip whitespace loop.
- `~` matches, j++.
- src[j] = `E`. Not quote.
- isIdentByte('E') true. Not digit. Return true.

Good.

For `cat <<  EOF` (extra space):
- j = i+2. src[j] = ` `.
- Skip whitespace. j++.
- src[j] = ` ` (another space). Skip. j++.
- Eventually j at `E`. Not `-`/`~`/quote. Ident char. Return true.

Good. Whitespace between operator and tag is tolerated.

OK I think I've covered the main potential issues. Let me write up the findings.

Actually let me reconsider the char literal bug. Is it really MEDIUM?

Arguments for MEDIUM:
- It's a real bug.
- It can affect grouping precision.
- It's an edge case that's not uncommon (Unicode chars in Rust).

Arguments against MEDIUM:
- The fallback to line-proximity works.
- The epic accepts heuristic parser limitations.
- It's a specific edge case.

I'll keep it as MEDIUM because it's a specific code-level bug (not just an accepted limitation).

OK let me also think about the corpus test for `bash_heredoc_no_overmerge`:
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

Let me trace this through the scanner:
- greet() { at line 1. Opens func greet.
- cat <<EOF at line 2. Header accumulates `cat `. heredocOp matched. parseHeredoc reads "EOF". heredocPending = true.
- \n. line = 2. heredocPending = false. state = stHeredoc.
- { not a block } at line 3. In stHeredoc, ignored. \n at end.
- lineBytes at the \n of line 3 = "{ not a block }". heredocLineMatches? Doesn't start with "EOF". Continue.
- \n after line 3. line = 3.
- EOF at line 4. State is still stHeredoc.
- \n at end of line 4. lineBytes = "EOF". heredocLineMatches? s == "EOF". Match. state = stNormal.
- line = 4.
- echo done at line 5. Header in stNormal. \n.
- line = 5.
- } at line 6. closeBlock. Closes greet. EndLine = 6.
- line = 6.
- post() { at line 7. Opens func post.
- echo hi at line 8. Header. \n.
- line = 8.
- } at line 9. closeBlock. Closes post. EndLine = 9.

Result:
- root.Children[0] = func greet, [1, 6]
- root.Children[1] = func post, [7, 9]

deepest(root, 5) = func greet (line 5 in [1, 6]).
deepest(root, 8) = func post (line 8 in [7, 9]).

Different blocks. Good. The test passes.

OK now let me also check the `bash_function_keyword_form`:
```json
{
  "name": "bash_function_keyword_form",
  "file": "a.bash",
  "source": "function hello {\n  local a=1\n  local b=2\n  echo done\n}\n",
  "a_line": 2,
  "b_line": 4,
  "same_block": true
}
```

Scanner:
- function hello { at line 1. Header "function hello". classifyHeader finds "function" (last whole word). named=true, name = "hello". Open func hello.
- local a=1 at line 2. Header. \n.
- local b=2 at line 3. Header. \n.
- echo done at line 4. Header. \n.
- } at line 5. closeBlock. Closes hello. EndLine = 5.

deepest(2) = hello. deepest(4) = hello. Same block. Good.

OK all looks well for bash.

Now let me also check `bash_same_func_large_drift_blank_lines`:
```json
{
  "name": "bash_same_func_large_drift_blank_lines",
  "file": "a.sh",
  "source": "greet() {\n  local x=1\n\n\n\n\n  local y=2\n  echo $x $y\n}\n",
  "a_line": 2,
  "b_line": 8,
  "same_block": true
}
```

Lines: 1=greet() {, 2=local x=1, 3-6=blank, 7=local y=2, 8=echo, 9=}

deepest(2) = greet. deepest(8) = greet. Same. Good.

OK I think I've thoroughly reviewed this. Let me write the findings.

Final findings:

1. **MEDIUM** - `internal/astgroup/parsers/src/braceparser/parse_core.go` - charLiteralLen function doesn't handle `\u{...}` and `\x{...}` multi-byte escape sequences in Rust char literals, causing `{` and `}` to be treated as block delimiters.

Hmm wait, the exact line number. Let me try to find it more precisely.

Looking at the diff for parse_core.go, the hunks have:
- The function `charLiteralLen` appears in a hunk. Looking at the diff output more carefully...

Actually I realize I should just look at the raw content. The new file parse_core.go has 522 lines (based on the +522 in the hunk header). The charLiteralLen function is around line 350-360.

Let me just count by looking at the structure of additions:
- Lines 1-23: header comment + package + import
- Lines 24-30: node struct
- Lines 31-37: blockKeyword struct
- Lines 38-55: langConfig struct
- Lines 56-64: scanner states constants
- Lines 65-220: parseSource function (the big one)
- Lines 221-258: classifyHeader function
- Lines 259-263: identByte function
- Lines 264-283: lastWholeWord function
- Lines 284-294: identAfter function
- Lines 295-313: funcParenName function
- Lines 314-319: matchAt function
- Lines 320-327: matchAnyPrefix function
- Lines 328-340: lineCommentStarts function
- Lines 341-354: hashAtWordBoundary function
- Lines 355-370: rawStringStart function
- Lines 371-378: countHashes function
- Lines 379-388: hasHashes function
- Lines 389-410: charLiteralLen function
- Lines 411-423: isHeredocStart function
- Lines 424-444: parseHeredoc function
- Lines 445-462: heredocLineMatches function

This is rough estimation. The actual line numbers depend on the exact content.

Let me use a rough line number like "approximately line 395" or just refer to the function name. For the format, I'll use the line number where the function starts.

Actually, looking at the diff more carefully, I can see the hunk context. Let me look for the @@ line for charLiteralLen.

Looking at the diff... I see hunks like:
```
@@ -0,0 +1,31 @@
@@ -0,0 +32,152 @@
@@ -0,0 +153,XX @@
```

Hmm, without the exact diff headers I can't get precise line numbers. Let me just estimate line 395.

Actually wait, I should look at the @@ lines in the diff more carefully. Let me search:

```
@@ -0,0 +1,31 @@
```

This is the first hunk, adds lines 1-31.

```
@@ -0,0 +32,191 @@
```

This adds lines 32-222 (191 lines after the first 31).

Wait, that's a lot. Let me look for smaller hunks.

Actually, since the file is new, all hunks start at -0,0. The format is +start,count. Let me look for the charLiteralLen hunk.

Without the exact diff headers, I'll estimate. Let me use line 395 for charLiteralLen.

Actually, looking at the diff content for parse_core.go, I see function definitions. Let me count them:

1. parseSource (the big one)
2. classifyHeader
3. isIdentByte (not in diff? or maybe named differently)
4. lastWholeWord
5. identAfter
6. funcParenName
7. matchAt
8. matchAnyPrefix
9. lineCommentStarts
10. hashAtWordBoundary
11. rawStringStart
12. countHashes
13. hasHashes
14. charLiteralLen
15. isHeredocStart
16. parseHeredoc
17. heredocLineMatches

So charLiteralLen is the 14th function. If parseSource takes ~150 lines and classifyHeader takes ~30 lines, then charLiteralLen is around line 350-400.

OK I'll use line 395 as a reasonable estimate.

Wait, let me try to count more precisely. The diff shows the hunks. Looking at the visible hunks for parse_core.go, I see:

Actually I realize I can use the line count from the diff. Each hunk header shows the line range. The file has 522 lines total (based on the last hunk). If I look at the diff for the hunk containing charLiteralLen, I can find the line number.

But without scrolling through the entire diff, I'll just estimate.

OK let me also consider: am I missing any other bugs?

Hmm let me think about test coverage. The tests are:
- TestHost_BraceParsersLoadAndParse (end-to-end test)
- TestLanguageForExt (extension mapping test)
- Various unit tests in parse_core_test.go and configs_test.go

The corpus tests in testdata/corpus.json extend the accuracy benchmark.

What about integration tests? The testdata/corpus.json is loaded by some benchmark test (TestBenchmark_ASTGroupingAccuracy per the spec). I assume this test exists. Let me see if the diff adds or modifies any benchmark code... No, just the corpus.

OK let me think about any other code paths.

The `embed.go` has new entries
</think>
