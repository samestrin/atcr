 The user wants me to review the provided diff according to the role "kai — architecture and design reviewer".

 First, let's understand the scope:
 - The sprint plan defines the active work item: Epic Plan 22.3: Quote/Escape-Aware Scanning in pyparser.
 - Components Touched: `internal/astgroup/parsers/src/pyparser`
 - Tasks:
   1. Restrict `scanTripleQuotes` to run only over the code portion of a line (skip scanning after an unquoted `#`).
   2. Skip triple-quote detection when it occurs inside a single-line string literal.
 - Acceptance Criteria:
   - A regression fixture with a `#` containing a triple-quote inside a comment is classified correctly (comment, not string).
   - A regression fixture with a string literal containing `#` is classified correctly (string, not comment).
   - Existing `pyparser` structural-hash tests pass unchanged for well-formed input.

 I need to review only the changed regions. The changed files in the diff are:
 1. `.planning/epics/active/24.0_false_positive_wontfix_dismissal.md` (NOT related to 22.3, but visible in diff)
 2. `.planning/epics/active/26.0_atcrignore_token_protection.md` (NOT related to 22.3)
 3. `.planning/epics/active/22.3_pyparser_quote_aware_scanning.md` -> `.planning/epics/completed/22.3_pyparser_quote_aware_scanning.md` (related, but just a rename)
 4. `.planning/epics/active/23.0_human_persona_renaming.md` -> `.planning/epics/superseded/23.0_human_persona_renaming.md` (NOT related)
 5. `CHANGELOG.md` (mentions 22.3, but is a changelog entry - not directly the code change unless there's an architectural issue in the changelog? Probably not relevant for architecture review unless it lies about contracts, but seems descriptive. I should focus on the actual code changes.)
 6. `internal/astgroup/host_test.go` (added tests for 22.3)
 7. `internal/astgroup/parsers/SHA256SUMS` (checksum update for python.wasm)
 8. `internal/astgroup/parsers/python.wasm` (binary file changed)
 9. `internal/astgroup/parsers/src/pyparser/main.go` (THE CORE CHANGE)

 The scope constraint says: "Constrain your findings to files and changes directly related to these work items. Suppress findings for unrelated changes that merely happen to appear in the diff". So I should IGNORE the epic plan files for 24.0, 26.0, 23.0, etc. Also suppress findings in `CHANGELOG.md` since it's just documentation of the change, not the change itself (unless there's a critical issue, but unlikely).

 So I need to focus on:
 - `internal/astgroup/parsers/src/pyparser/main.go`
 - `internal/astgroup/host_test.go`
 - Possibly `internal/astgroup/parsers/SHA256SUMS` if there's an issue with checksum updates (like not updating, but that's changed).

 Wait, the scope constraint is a hint, not a hard limit for genuinely critical issues. But the question is whether there are critical issues in the unrelated files. They are just epic markdown files being updated. No critical architectural issues there.

 Now let's analyze the actual code changes in `pyparser/main.go` and `host_test.go`.

 Main changes in `main.go`:
 1. `significantLines` docstring updated to mention quote/escape-awareness.
 2. `scanTripleQuotes` is refactored. A new function `scanLine` is introduced.
    - `scanLine` takes `(line, delim string)` and returns `(endDelim string, commentAt int)`.
    - It handles:
      - Active triple-quote delimiter (`delim != ""`): scans for closing delimiter.
      - Single-line string quotes (`q` tracks active quote char):
        - Handles backslash escapes: `i += 2` (potential bug here? If backslash is last char on line, `i+2` could exceed length. Let's check the loop: `for i := 0; i < len(line);`. If `i` is at `len(line)-1` and `line[i] == '\\'`, then `i += 2` makes `i = len(line)+1`, which is `>= len(line)`, so the loop stops. In Go, this is safe because the loop condition is checked before the body. But wait, there's no bounds check before `line[i]` accesses? Actually, the switch is on `line[i]`. If `i < len(line)`, `line[i]` is safe. After `i += 2`, if `i` becomes `len(line)+1`, the loop condition `i < len(line)` fails, so it exits. But what if `i == len(line)-1`? Then `line[i]` is the backslash. `i += 2` sets `i` to `len(line)+1`. Loop ends. So no out-of-bounds. However, what if the escape is exactly at `len(line)-1`? The escape consumes the backslash and the next byte... but there is no next byte. This is a line continuation escape in Python? Actually a backslash at end of line inside a string is weird but possible. Here it just skips past the end, which is fine.
        - Handles closing quote: `case q: q = 0`.
        - Continues loop.
      - Triple-quote detection: `"""` or `'''`.
      - Comment start `#` when `q == 0` and `delim == ""`: returns `delim, i`.
      - Single quote start: `q = line[i]`.
    - Returns `delim, len(line)` if no comment found.
 3. `scanTripleQuotes` now calls `scanLine` and discards the comment offset: `end, _ := scanLine(line, delim); return end`.
 4. `stripComment` now uses `scanLine(text, "")` and takes the comment offset. This means it respects triple-quoted strings on the line. Wait, `stripComment` is called in `significantLines`:
    ```go
    text := strings.TrimSpace(stripComment(raw))
    ```
    But `stripComment` uses `scanLine(text, "")`. If there is an active triple-quote delimiter on this line, should it be passed? The original `stripComment` just looked for `#`. Now it calls `scanLine(text, "")`, so `delim` is always empty string.
    However, in `significantLines`, the flow is:
    ```go
    text := strings.TrimSpace(stripComment(raw))
    ...
    if d := bracketDelta(text); ...
    if nxt, ok := build(lines, i, indent, text); ...
    ```
    And before that:
    ```go
    prevDelim = scanTripleQuotes(raw, prevDelim)
    ```
    So the triple-quote state is tracked by `prevDelim`. But `stripComment` doesn't know about `prevDelim`. If a line is inside a triple-quoted string, `significantLines` checks `if prevDelim != "" { continue }` BEFORE calling `stripComment(raw)`. So if we are inside a triple-quoted string, we skip the line entirely. If we are NOT inside a triple-quoted string, but a triple-quoted string starts on this line, `scanTripleQuotes` updates `prevDelim`.
    
    Wait, let's look at `significantLines` carefully:
    ```go
    prevDelim = scanTripleQuotes(raw, prevDelim)
    if prevDelim != "" {
      // line is inside a triple-quoted string
      continue
    }
    text := strings.TrimSpace(stripComment(raw))
    ```
    So `stripComment` is only called when `prevDelim == ""` at the START of the line scan (after `scanTripleQuotes` runs). But what if a triple-quote OPENS and CLOSES on the same line? Example: `x = """hello""" # comment`. Let's trace:
    - `scanTripleQuotes(raw, "")` calls `scanLine`.
    - `scanLine` sees `"""`, sets `delim = """`. Then later sees `"""` again, sets `delim = ""`. Then sees `#`, returns `("", i)`.
    - `scanTripleQuotes` returns `""`.
    - `significantLines` sees `prevDelim == ""`, so it does NOT skip the line.
    - Then `stripComment(raw)` calls `scanLine(raw, "")`. It sees `"""`, sets `delim = """`. Then sees `"""`, sets `delim = ""`. Then sees `#`, returns `("", i)`. So it correctly strips the comment.
    
    What about a line that starts a triple-quoted string but does not end it? Example: `x = """hello`. 
    - `scanTripleQuotes(raw, "")`: sees `"""`, sets `delim = """`. Reaches end of line. Returns `"""`.
    - `prevDelim` becomes `"""`.
    - Since `prevDelim != ""`, line is skipped.
    - Next line continues the string.
    So `stripComment` is not called on lines inside an open triple-quoted string. That seems correct.

    But wait: what if a line has a `#` inside a triple-quoted string that starts and ends on the same line? The `stripComment` calls `scanLine(text, "")`. It correctly tracks the triple quotes. So the `#` is ignored. Good.

    Now, what about a single-line string containing `#`? Example: `if tag == "x#y":`.
    - `scanTripleQuotes` returns `""` (no triple quote issues).
    - `stripComment` calls `scanLine(text, "")`. It sees `"`, sets `q = '"'`. Then sees `#`, but `q != 0`, so it's ignored. Then sees `"`, sets `q = 0`. Then sees `:`. Loop ends. Returns `("", len(line))`. So `stripComment` returns the whole line. Good.

    What about a `#` containing triple quotes in a comment? Example: `x = 1  # a fake """ opens here`.
    - `scanTripleQuotes(raw, "")`: `scanLine` goes through. Sees no triple quotes (there's `"""` but only after `#`). Wait! In `scanLine`, when `delim == ""` and `q == 0`, it first checks for `"""` before checking for `#`.
    - On `x = 1  # a fake """ opens here`:
      - It scans `x = 1  `, then hits `#`. BUT WAIT. In `scanLine`, the order is:
        ```go
        if strings.HasPrefix(line[i:], `"""`) { ... }
        if strings.HasPrefix(line[i:], `'''`) { ... }
        switch line[i] {
        case '#': return delim, i
        case '"', '\'': q = line[i]
        }
        ```
      - So when `i` is at the `#`, it checks `strings.HasPrefix(line[i:], `"""`)` -> false. `'''` -> false. Then switch `line[i]` is `#`, so it returns `delim, i`.
      - So `#` is found at index of `#`. It returns. The `"""` after `#` is never seen.
      - Thus `scanTripleQuotes` returns `""`. The line is not in string mode.
      - `stripComment` similarly returns text before `#`. Good.

    What about triple quotes inside a single-line string? Example: `s = "text with ''' inside"`.
    - `scanTripleQuotes`: `scanLine` sees `"`, sets `q = '"'`. Then scans inside string. Sees `'''`, but `q != 0`, so the triple quote checks at the top of the loop are only reached when `q == 0`? Let's look:
      ```go
      for i := 0; i < len(line); {
          if delim != "" { ... continue }
          if q != 0 {
             switch line[i] { case '\\': ... case q: ... }
             i++
             continue
          }
          if strings.HasPrefix(line[i:], `"""`) { ... }
          if strings.HasPrefix(line[i:], `'''`) { ... }
          switch line[i] { case '#': ... case '"', '\'': q = line[i] }
          i++
      }
      ```
    - Yes! When `q != 0`, it handles escapes and closing quote, then `continue`. It does NOT check for triple quotes. So `'''` inside a double-quoted string is treated as content. `scanTripleQuotes` returns `""`. Good.
    - `stripComment` also does the same. Good.

 Now let's look for architectural/design issues.

 **Potential Issue 1: `scanLine` backslash escape handling is crude.**
 `case '\\': i += 2; continue`
 This assumes the byte after backslash exists. As I reasoned, if backslash is the last byte, `i += 2` jumps past `len(line)`. The loop condition catches it, so it's safe from panic. But it means a backslash at end of line inside a single-line string will skip the loop end. Is this correct behavior? In Python, a backslash at end of line inside a string literal is an error if it's not escaping a quote? Actually, in Python, a backslash at the very end of a line inside a string continues the string to the next line. But the code's doc comment says: "single-line strings are not carried across physical lines (a backslash line-continuation inside one is out of scope)." So this is documented. But is the `i += 2` a problem? What if `i == len(line)-1`? Then `i += 2` -> `i = len(line)+1`, loop ends. The string remains open (`q != 0`). But the function returns at end of line. Since `q` is local to `scanLine` and not returned, the single-line string state is LOST at line boundary. That means on the next line, `q` starts at 0. This matches the documented limitation: "single-line strings are not carried across physical lines". So it's acceptable.

 **Potential Issue 2: `stripComment` now calls `scanLine(text, "")`, ignoring active triple-quote delimiter.**
 As noted, `stripComment` is only called in `significantLines` when `prevDelim == ""` AFTER running `scanTripleQuotes`. But what if a triple-quoted string starts and ends on the same line? Then `scanTripleQuotes` returns `""`, and `stripComment` runs. `stripComment` calls `scanLine(text, "")`. It correctly tracks the triple quotes on that line. So a `#` inside a same-line triple-quoted string is handled.
 But what if `stripComment` is called elsewhere? Let's check the file. `stripComment` is only called in `significantLines`. So it's fine.

 Wait, is there a contract issue? The old `stripComment` was a simple utility. The new one depends on `scanLine` which is more complex. This is fine.

 **Potential Issue 3: The name `scanLine` is generic but returns two things.**
 It returns `endDelim` and `commentAt`. The function is used by both `scanTripleQuotes` and `stripComment`. This consolidates logic. Is this good? Yes, it avoids duplication. But is it a boundary violation? No, it's within the same module.

 **Potential Issue 4: Duplication of responsibility / Two sources of truth.**
 `scanLine` is now the single source of truth for quote/comment parsing on a line. This is actually good — it centralizes the logic that was previously split.

 **Potential Issue 5: Extensibility traps.**
 The code adds `scanLine` which handles `"""`, `'''`, `"`, `'`, `#`, `\`. It explicitly does NOT handle raw/byte/f-string prefixes. The doc comment says: "raw/byte/f-string prefixes are not modelled". This is consistent with the sprint plan's "Out of Scope" section which says: "Full AST-accurate Python parsing — this remains a heuristic". However, is there an extensibility trap? The function `scanLine` hardcodes the quote characters and comment character. If Python were to add new string prefixes or different quote mechanics, this would need changes. But given this is a PoC-grade parser, that's acceptable.

 **Potential Issue 6: Behavior of `scanLine` with backslash escaping outside strings.**
 In the old `scanTripleQuotes`, backslashes were not modeled. In the new `scanLine`, backslash is only handled inside a single-line string (`q != 0`). What about backslash escaping outside strings? In Python, a backslash at end of line outside a string continues the logical line. The old code didn't handle it. The new code still doesn't. The sprint plan doesn't mention this. Not an issue.

 **Potential Issue 7: Test design in `host_test.go`.**
 The tests are regression tests. They instantiate a `Host`, get a `Parser("python")`, and parse source strings. They check that function names are collected.
 But wait: `collectFuncNames` and `firstOfKind` are helpers. Are they in the test file? Yes, they are defined in `host_test.go`. `firstOfKind` is added in this diff.
 Is there any architectural issue in the tests? The tests use `require.Contains(t, names, "b", "...")` where the last arg is the assertion message. That's fine.
 But wait: the test `TestHost_PyParseHashInsideString` uses `firstOfKind(root, "if")`. Is `firstOfKind` robust? It does pre-order walk. That's fine.
 However, look at `collectFuncNames`. Is it defined? It is called in the tests but not defined in the diff snippet. Wait, looking at the diff for `host_test.go`:
 ```go
 collectFuncNames(root, &names)
 ```
 This function must have been pre-existing in `host_test.go` (below the visible diff context? No, the diff starts at line 647 and adds lines after). Actually, looking at the diff header:
 ```
 @@ -647,3 +647,97 @@ func TestHost_OversizedResultRejectsWithoutTrapping(t *testing.T) {
 ```
 So the new code is AFTER line 647. `collectFuncNames` is likely already defined earlier in the file (before line 647). Okay.

 Are there any issues with the tests from an architecture perspective? The tests are in `host_test.go` but test `pyparser` behavior. `host_test.go` is in `internal/astgroup`. It tests the host's parser integration. That's the right place for black-box regression tests.

 **Potential Issue 8: SHA256SUMS and binary file.**
 The `SHA256SUMS` file is updated. The binary `python.wasm` is changed. Is there a design issue here? The binary is a build artifact checked into git. There is no build verification in the diff. But is this within my scope? The sprint plan says Components Touched: `internal/astgroup/parsers/src/pyparser`. The `.wasm` file is a compiled artifact. Updating it is necessary for the change to take effect. The SHA256SUMS ensures integrity. This seems like a mechanical necessity, not an architectural issue.

 **Potential Issue 9: `stripComment` contract change.**
 Old `stripComment`: returns text up to first `#`.
 New `stripComment`: returns text up to first unquoted `#`.
 This is a behavior change. Is there any caller that relied on the old behavior? Let's check `main.go`. Only called in `significantLines`. In `significantLines`, `stripComment` is used to remove comments before analyzing code structure. The new behavior is more correct. No other callers. So no contract breakage.

 **Potential Issue 10: `scanLine` and multi-byte characters.**
 The code operates on bytes, not runes. What if the Python source contains Unicode? Python 3 source is UTF-8. A `#` is ASCII. Quotes are ASCII. Triple quotes are ASCII. Backslash is ASCII. If a multi-byte UTF-8 character contains a byte that matches one of these, it could be misinterpreted. For example, if a Unicode character has a byte value of `0x23` (`#`), `0x22` (`"`), etc. In UTF-8, ASCII bytes only appear as themselves (bytes >= 0x80 are continuation bytes, bytes < 0x80 are single-byte ASCII characters). Wait, actually in UTF-8, bytes 0x00-0x7F represent ASCII characters directly. Bytes 0x80-0xBF are continuation bytes and cannot be confused with ASCII. So byte-matching for ASCII characters is safe in UTF-8. So no issue there.

 **Potential Issue 11: `scanLine` ordering bug?**
 Let's trace a pathological case: `x = '#' # comment`.
 - `scanLine` with `delim=""`, `q=0`.
 - `x = ` -> no match.
 - `'` -> `q = '\''`.
 - `#` -> inside `q != 0`, so handled by string case. It's not a closing quote. `i++`.
 - `'` -> closing quote. `q = 0`.
 - ` ` -> no match.
 - `#` -> `case '#'`: return `delim, i`.
 So it correctly finds the second `#` as comment start. Good.

 What about `x = "" # "comment"`?
 - Sees `"`, `q = '"'`.
 - Sees `"`, `q = 0`.
 - Sees ` `.
 - Sees `#`, returns. Good.

 What about `x = """ # not a comment`?
 Wait, `"""` opens a triple-quoted string. Then space, then `#`. In `scanLine`, `delim` becomes `"""`. Then inside `delim != ""` loop, it scans until closing `"""`. There is none. So it returns `"""`, len(line). So `#` is inside triple-quoted string. `scanTripleQuotes` would return `"""`. In `significantLines`, if this line didn't close the triple quote, the line would be skipped (because `prevDelim != ""` after the call). But wait: what if `stripComment` was called with a line that starts and ends a triple quote and has a comment? Already checked above.

 **Potential Issue 12: Backslash handling in `scanLine` when `q != 0` can skip past the end.**
 As noted, `i += 2` when `i == len(line)-1` sets `i = len(line)+1`. Then loop ends. The function returns. `endDelim` is whatever `delim` was (could be `""`). `commentAt` is `len(line)`. So it's safe. But what if `q != 0` at the end of line? Example: `s = "unterminated`. `scanLine` sets `q = '"'`, reaches end, returns `("", len(line))`. So `stripComment` would return whole line. `scanTripleQuotes` returns `""`. This means `significantLines` will NOT skip the next line as a continuation of the string. But the doc comment explicitly says: "single-line strings are not carried across physical lines... out of scope." So this is documented and acceptable.

 **Potential Issue 13: Is there a hidden dependency or coupling introduced?**
 `scanLine` is called by `scanTripleQuotes` and `stripComment`. This reduces coupling compared to having separate heuristics. Good.

 **Potential Issue 14: Contract design / APIs that lie.**
 The new `scanLine` doc comment is fairly detailed. It says: "A backslash escapes the next byte inside a single-line string, so an escaped quote does not prematurely close it." But the implementation does `i += 2`. This means it also escapes ANY next byte, not just quotes. For example, in `"hello\nworld"`, the `\n` is treated as escape + `n`. The `n` is not a quote, so it doesn't close. This is fine. But what about `"hello\\"`? Backslash escapes backslash. `i += 2`. Then next iteration sees `"`? No, if the string is `"hello\\"`, after `h`, `e`, `l`, `l`, `o`, `\`:
 - At `\`: `i += 2`. Now `i` points past the second `\` to `"`. Loop continues. Sees `"`. This is `q`, so string closes. Correct.
 What about `"hello\"`? At `\`: `i += 2`. Now `i` points past `"` to end of line. Loop ends. String remains open. But in Python, `\"` is an escaped quote inside the string. If it's at end of line without a closing quote, it's an unterminated string literal (syntax error). The parser is heuristic anyway.

 But wait: what if the backslash escapes the closing quote but there are more characters after? `"\"”` (not a real quote, ignore). Actually, `"he said \"hello\""`. Trace:
 - `"`: q = '"'
 - `h`, `e`, ` `, `s`, `a`, `i`, `d`, ` `: normal
 - `\`: i += 2. Now `i` points at `h` of `hello`.
 - `h`, `e`, `l`, `l`, `o`: normal.
 - `\`: i += 2. Now `i` points at `"` after `hello`.
 - `"`: q = 0 (closes string).
 - `"`: q = '"' (opens a new string? Wait, after the first string closes, the scan continues. Then it sees the next `"` which starts a new string literal, unless there's no more chars. But there is another `"` after the escaped one?) Let's trace carefully.
 Actually string: `"he said \"hello\""`
 Positions: 
 0: `"` -> q = '"'
 1: h
 ...
 10: `\` -> i becomes 12 (skipping `"` at 11)
 12: h
 ...
 17: `\` -> i becomes 19 (skipping `"` at 18)
 19: `"` -> q = 0 (closes string)
 20: `"` -> q = '"' (opens new string) ... wait, there is no character at 20 because the string is exactly length 20? Let's count: 
 `"` (0), `h` (1), `e` (2), ` `(3), `s`(4), `a`(5), `i`(6), `d`(7), ` `(8), `\`(9), `"`(10), `h`(11), `e`(12), `l`(13), `l`(14), `o`(15), `\`(16), `"`(17), `"`(18). Length 19. Wait, my mental string was wrong.
 Let's write it out: `"he said \"hello\""`
 It should be: quote, h, e, space, s, a, i, d, space, backslash, quote, h, e, l, l, o, backslash, quote, quote.
 After `hello`, there is `\` which escapes the quote. Then the final quote closes the string. Actually in Go string literal: `"he said \"hello\""`. Inside the Go source code for the test, the source being parsed is written as a Go string literal, so we have to be careful. But the actual bytes passed to Parse are what matters.
 The actual bytes are: `he said "hello"` enclosed in double quotes. So the raw bytes are: `"`, `h`, `e`, ..., ` `, `\`, `"`, `h`, ..., `o`, `\`, `"`, `"`. 
 Trace in `scanLine`:
 - First `"`: q = '"'
 - ... scan ...
 - `\` at position of backslash before first inner quote: i += 2. Now i points to `h` of `hello`.
 - scan `hello`.
 - `\` at position of backslash before second inner quote: i += 2. Now i points to the final `"`.
 - That final `"` matches q, so q = 0. String is closed. Returns end of line. Good.

 So the escape handling seems adequate for the purpose.

 **Potential Issue 15: Does the new `scanLine` introduce a performance concern?**
 It iterates per byte. The old one also iterated per byte (or used `strings.HasPrefix` which is also byte-wise). No change in complexity. Fine.

 **Potential Issue 16: Missing test coverage for `#` inside triple-quoted string on same line?**
 AC says:
 - A `#` containing triple-quote inside comment -> covered by `TestHost_PyParseTripleQuoteInComment`.
 - A string literal containing `#` -> covered by `TestHost_PyParseHashInsideString`.
 - Existing tests pass.
 But is there a test for `#` inside a triple-quoted string that starts and ends on the same line? E.g., `x = """ hello # world """ # comment`. The `stripComment` handles this via `scanLine`, but there is no regression test. However, the sprint plan acceptance criteria don't explicitly require this. Is this an extensibility trap? Not really, it's just test coverage. As an architecture reviewer, I care about contract and design, not test coverage per se, unless the missing coverage hides a contract violation. But the code structurally handles it.

 Wait, let me re-read the prompt. "Find problems the author would prefer you didn't." "Coupling: hidden dependencies, shared mutable state, config reach-through." "Contract design: APIs that lie, error types that lose information, ambiguous ownership." "Duplication of responsibility: two sources of truth, parallel code paths that will drift." "Extensibility traps: hardcoded assumptions the roadmap already contradicts."

 Is there a hidden dependency? `scanLine` is used by both `scanTripleQuotes` and `stripComment`. If `scanLine` is changed, both are affected. This is centralization, not hidden dependency.

 Is there ambiguous ownership? `scanLine` owns the line scanning state machine. It's well-defined.

 Is there a contract issue? The function `scanTripleQuotes` now delegates to `scanLine`. Its signature is unchanged. It still returns the delimiter. Good.

 But wait! Look at `stripComment`:
 ```go
 func stripComment(text string) string {
 	_, at := scanLine(text, "")
 	return text[:at]
 }
 ```
 What if `scanLine` returns a `commentAt` that is inside a single-line string? According to `scanLine`'s logic, it returns `i` only when `line[i] == '#'` AND `q == 0` AND `delim == ""`. So `commentAt` should never be inside a string. Good.

 However, what if `scanLine` receives `text` that is already partially processed? `stripComment` is called on `raw` line from source. `strings.TrimSpace(stripComment(raw))` is used. So it gets the raw line. Good.

 Let me think about whether the binary SHA256SUMS update is coupled correctly. The SHA256SUMS file is a manifest. It is updated. This is a build artifact coupling. Not really a codebase architecture issue.

 Are there any findings from the epic markdown files that relate to 22.3? The 22.3 epic was moved from active to completed. The CHANGELOG was updated. These are documentation/planning changes. The scope constraint says suppress unrelated changes. The epic move is related to 22.3, but is there an architectural issue there? No.

 Wait, I need to be careful. The payload shows changes to multiple epic files. The scope constraint says: "Constrain your findings to files and changes directly related to these work items. Suppress findings for unrelated changes that merely happen to appear in the diff". The work item is 22.3. Changes in 24.0, 26.0, 23.0 are unrelated. I should not flag issues there.

 But what about `CHANGELOG.md`? It documents 22.3. Is there an architectural issue in the changelog? Unlikely. The changelog says "triple-quote appearing inside a single-line string literal" and "triple-quote inside a # comment". It's just a description.

 Let me reconsider `main.go` for any subtle design flaws.

 **Subtle design flaw: `scanLine` uses byte indexing but `strings.HasPrefix` on slices.**
 When `q != 0`, the code uses `line[i]` (byte indexing). For ASCII, this is fine. For non-ASCII inside a Python string literal, if there is a byte that happens to be a backslash or quote, it will be interpreted. But Python source is typically UTF-8. A multi-byte UTF-8 character cannot contain bytes 0x22 (`"`) or 0x27 (`'`) or 0x5C (`\`) as continuation bytes. Wait, actually UTF-8 continuation bytes are 0x80-0xBF. ASCII bytes 0x00-0x7F are self-representing. So a byte value of 0x22 can ONLY occur as the ASCII character `"`. So byte-level scan is safe for UTF-8 text when looking for ASCII delimiters. No issue.

 **Subtle design flaw: `scanTripleQuotes` no longer strips comments itself.**
 The old `scanTripleQuotes` scanned the entire line for triple quotes. The new one calls `scanLine`, which stops at `#`. This means if there's a `#` on the line, `scanTripleQuotes` will not see any triple quotes after it. This is the intended behavior. But what about triple quotes that straddle the comment boundary? Impossible: a `#` starts a comment that goes to end of line. Triple quotes after `#` are in comment. So correct.

 What about triple quotes inside a string that itself is before a comment? `s = "'''" # comment`. `scanLine` handles this: inside string, `'''` is ignored. Then after string closes, `#` is found. Good.

 Is there any issue with `bracketDelta`? It wasn't changed. It still doesn't handle brackets inside strings. The diff didn't change it. The sprint plan doesn't require it.

 Let me reconsider the `host_test.go` file. The tests were added to `internal/astgroup/host_test.go`. This tests the Python parser via the Host. Is this the right layer? The epic says "Existing pyparser structural-hash tests pass unchanged". The regression tests are at the Host level. This is fine, but it introduces a dependency on the full Wasm runtime to test the parser. However, `host_test.go` already has tests for parsers. So it's consistent with existing practice.

 Are there any issues with the test functions themselves?
 `firstOfKind` does a recursive walk. If the node graph is deep/cyclic, it could stack overflow. But Python AST from this parser is a tree (or should be). The old code didn't have cycles. Not an issue.

 Wait, I need to look closely at `collectFuncNames`. It's called but not defined in the diff. It must exist earlier in the file. That's fine.

 Let me think about whether I should flag anything as MEDIUM or LOW.

 Possible LOW finding: The function `scanLine` is in `main.go` but has a very long comment. That's not really a finding.

 Possible MEDIUM finding: The binary artifact `python.wasm` is checked in without a visible build script change or CI verification step in the diff. But this is a pre-existing pattern. The diff only updates the binary. Is there a "duplication of responsibility" or "coupling" issue? The test `host_test.go` uses the compiled Wasm. If the source `main.go` is updated but the `.wasm` is not, tests would fail (or test the old parser). The SHA256SUMS guards against accidental corruption, but nothing in the diff ensures the `.wasm` was actually built from the corresponding `main.go`. However, this is out-of-scope for the diff (pre-existing workflow). And as per instructions, pre-existing issues should be categorized `out-of-scope` and flagged only if in unchanged code. But since this is about the relationship between changed source and changed binary, is it a finding?
 The diff includes both the source change and the binary change. If the binary doesn't match the source, that's a bug. But I have no way to verify the binary from the diff. I should probably not flag this.

 Another possible issue: The `scanLine` function mutates `q` and `delim` as local variables. Not shared mutable state.

 Let me think about "APIs that lie".
 `scanTripleQuotes` comment says: "advances the triple-quoted-string state across one physical line... It is comment-aware via scanLine: a triple-quote token appearing inside a # comment does not flip the state machine." This is accurate.
 `stripComment` comment says: "returns text with any trailing # comment removed. It is comment-aware via scanLine: a # inside a triple-quoted string span on this line is treated as string content". This is accurate.

 Is there an issue with `stripComment` calling `scanLine(text, "")` while ignoring the returned `endDelim`? If the line has an unclosed triple-quoted string, `endDelim` will be non-empty, but `stripComment` ignores it. However, as discussed, `stripComment` is only called when `prevDelim == ""` and after `scanTripleQuotes` returned `""`. But what if a triple quote opens and does NOT close on the same line? Then `scanTripleQuotes` would have returned the delimiter, and `significantLines` would skip the line entirely, never calling `stripComment`. So `stripComment` is always called on lines that are either:
 1. Outside any triple-quoted string (`scanTripleQuotes` returned `""` and didn't open one on this line).
 2. Inside a triple-quoted string that both opened and closed on this line (`scanTripleQuotes` returned `""` but `scanLine` would have returned non-empty `endDelim` during its execution,