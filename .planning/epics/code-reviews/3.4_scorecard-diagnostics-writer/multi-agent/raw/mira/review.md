<think>
Let me review the diff carefully against the sprint plan scope.

The sprint plan is about:
1. Threading an injectable diagnostics writer through the scorecard read+emit API
2. Adding ReadOpts{Writer io.Writer} and EmitOpts.Diag io.Writer
3. CLI wiring (cmd.ErrOrStderr())
4. MCP wiring (os.Stderr)
5. Converting all fmt.Fprintf(os.Stderr,...) diagnostics
6. Tests asserting diagnostic text via injected buffer

Let me check each file in the diff for issues:

**cmd/atcr/leaderboard.go** - Adds ReadOpts{Writer: cmd.ErrOrStderr()} to ReadAll call. Looks correct.

**cmd/atcr/reconcile.go** - Adds Diag: cmd.ErrOrStderr() to EmitForReconcile. Looks correct.

**cmd/atcr/scorecard.go** - Adds ReadOpts{Writer: cmd.ErrOrStderr()} to FindByRunID. Looks correct.

**internal/mcp/handlers.go** - Adds Diag: os.Stderr. Looks correct.

**internal/scorecard/integration_test.go** - Updates test calls. Looks correct.

**internal/scorecard/reconcile_test.go** - Updates test calls. Looks correct.

**internal/scorecard/scorecard.go**:
- Adds `io` import
- Adds `Diag io.Writer` field to EmitOpts
- Adds `diagWriter` helper usage
- Changes `verdictTallies` signature to take `w io.Writer`
- Replaces `fmt.Fprintf(os.Stderr, ...)` with `_, _ = fmt.Fprintf(w, ...)`

Looking more carefully at scorecard.go:

```go
func Emit(in EmitInput, opts EmitOpts) error {
	if opts.NoScorecard {
		return nil
	}
	w := diagWriter(opts.Diag)

	dir, err := resolveDir(opts.Dir)
	if err != nil {
		_, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
		return err
	}
```

Wait, there's a subtle issue here. Looking at the original code:
```go
dir, err := resolveDir(opts.Dir)
if err != nil {
    fmt.Fprintf(os.Stderr, "scorecard: write failed: %v\n", err)
    return err
}
```

The diagnostic in the resolveDir error case goes to the writer `w`. But in the loop:
```go
for _, rec := range records {
    if err := Append(dir, rec); err != nil {
        _, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
```

These both use `w` correctly. But there's a potential issue: the test `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` uses a `storeDir` that's under a regular file, so `MkdirAll` in `Append` fails. But wait, `Append` is called inside the loop after `resolveDir` succeeds. The test sets `Dir: storeDir` where `storeDir` is `blocker/scorecard` and `blocker` is a regular file. So `Append` calls `MkdirAll` for the store dir. Let me re-read Append:

The diff doesn't show Append's implementation. But it would need to be able to create the directory. The test expects the error to come from `Append` inside the loop, not from `resolveDir`.

Actually, the test `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` expects:
```go
err := Emit(in, EmitOpts{Dir: storeDir, Diag: &buf})
require.Error(t, err, "a store path under a regular file must fail to write")
assert.Contains(t, buf.String(), "write failed",
    "write-failure diagnostic must route to the injected EmitOpts.Diag")
```

This relies on the Append path to fail and write to `w`. That looks correct given the code.

Let me check the test `TestEmit_NilDiagDefaultsToStderr` - it just calls Emit with no Diag. It doesn't capture stderr but ensures no panic. This is fine but doesn't actually verify that the diagnostic goes to stderr. It just verifies no panic on nil Diag. The test name is somewhat misleading but acceptable.

**internal/scorecard/store.go**:
- Adds ReadOpts struct
- Adds diagWriter helper
- Updates ReadRecords signature
- Updates decodeRecord signature
- Updates FindByRunID signature
- Updates ReadAll signature
- Replaces all fmt.Fprintf(os.Stderr,...) calls

Looking at the store.go changes:
- `decodeRecord` now takes `w io.Writer` as parameter
- `ReadRecords` resolves `w := diagWriter(opts.Writer)` and passes it to `decodeRecord`
- `FindByRunID` passes `opts` to `ReadRecords` and uses `diagWriter(opts.Writer)` for the adjacent-month diagnostic
- `ReadAll` passes `opts` to `ReadRecords`

One issue: in `FindByRunID`, the adjacent-month diagnostic is:
```go
_, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
```

This calls `diagWriter(opts.Writer)` even though `opts.Writer` was already resolved in `ReadRecords`. But that's fine - it's consistent. Actually wait, `FindByRunID` doesn't resolve `w` once and pass it down - it passes the raw `opts` to `ReadRecords` (which resolves internally) and also calls `diagWriter(opts.Writer)` for the adjacent-month message. This is correct but a tiny bit inconsistent in style - you could just resolve once at the top of FindByRunID. But not a real issue.

**internal/scorecard/scorecard_test.go**:
- Imports "bytes"
- Adds three new tests for diagnostic routing
- Updates readJSONL helper

**internal/scorecard/store_test.go**:
- Updates test calls
- Adds three new tests for diagnostic routing

Let me look for actual issues:

1. In `scorecard.go`:
```go
func verdictTallies(in EmitInput, w io.Writer) (verified, refuted map[string]int, present bool) {
```
This adds `w` as a parameter. The comment is updated. Looks correct.

2. In `scorecard.go` line ~118, the `Emit` function does:
```go
if opts.NoScorecard {
    return nil
}
w := diagWriter(opts.Diag)
```
But if `NoScorecard` is true, we never resolve `w`. This is fine since we don't need it.

3. Looking at `EmitForReconcile` - the diff doesn't show changes to this function. It's called from CLI and MCP with the new `EmitOpts{Diag: ...}`. The function presumably calls `Emit` which uses the Diag. Let me check if EmitForReconcile is in the diff... No, it's not shown being changed. So it must already pass through opts to Emit.

4. Let me look at the test `TestEmit_NilDiagDefaultsToStderr`:
```go
func TestEmit_NilDiagDefaultsToStderr(t *testing.T) {
	dir := t.TempDir()
	in := EmitInput{
		RunID:     testRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir})) // nil Diag → os.Stderr, must not panic
}
```
This test doesn't actually verify the default-to-stderr behavior - it only verifies no panic. It doesn't trigger any diagnostic that would go to stderr. So the test is weak - it just checks nil Diag doesn't panic. A stronger test would inject a malformed line that would trigger a diagnostic, with a nil Diag, and verify the diagnostic actually reaches stderr (e.g., by redirecting os.Stderr or checking a side effect). But this is more of a "nice to have" issue, not a bug.

Actually wait - in this test, there are no malformed records, no write failures, and no orphan verdicts. So no diagnostic is emitted. The test only verifies that `diagWriter(nil)` returning `os.Stderr` doesn't cause a panic when constructing the writer. That's a weak test for AC5.

5. The `TestStore_ReadRecords_NilWriterDefaultsToStderr` test:
```go
func TestStore_ReadRecords_NilWriterDefaultsToStderr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a", "bruce"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(string(good)+"\n{bad\n"), 0o600))

	recs, err := ReadRecords(path, ReadOpts{}) // nil Writer → os.Stderr, must not panic
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}
```
Similar issue - this test triggers a malformed record but uses nil Writer. It verifies no panic and that the record is read, but doesn't actually verify the diagnostic goes to os.Stderr. Same issue as above.

Both tests have weak assertions for the "default to os.Stderr" claim - they only check no panic, not that the diagnostic actually went to stderr.

6. Looking at the test file changes - imports for `bytes` and `assert` are needed but I should verify the existing imports include them. The diff shows:
- `scorecard_test.go`: adds `"bytes"` import
- `store_test.go`: I need to check if it already imports `bytes`...

Looking at store_test.go, it doesn't show new imports being added. The new tests use `bytes.Repeat`, `bytes.Buffer`, and `json.Marshal`. Let me check the existing imports... The diff doesn't show the full import block. But `json` is likely already imported. `bytes` - need to check. Actually, looking at the test code:

```go
huge := bytes.Repeat([]byte("x"), maxLineBytes+1024) // one line over the cap
var buf bytes.Buffer
```

So `bytes` is needed. The diff doesn't show the import being added to store_test.go. This could be a missing import, or it could already exist. Let me re-read the diff... The diff for store_test.go doesn't show import changes. If `bytes` wasn't already imported, this would be a compilation error. But since this is a review of the diff, and the diff doesn't show the import, either:
(a) `bytes` was already imported (e.g., the file might have it)
(b) The import is missing (would be a bug)

Looking at the existing store_test.go content in the diff, I don't see "bytes" in imports. But the test file also has `splitNonEmptyLines` which deals with bytes. Let me look at the full file structure...

The diff context shows:
```go
func TestStore_ReadAll_OverLongLineDoesNotAbortAllMonths(t *testing.T) {
```

And later:
```go
// splitNonEmptyLines splits raw JSONL bytes into trimmed non-empty lines so a
```

So there's a `splitNonEmptyLines` function that uses `bytes`. The imports aren't shown in the diff but the file likely already imports `bytes`. This is probably fine.

Actually, looking more carefully at the diff for store_test.go, I see the new tests use `json.Marshal` and `sampleRecord` which probably exist. The new tests also use `bytes.Repeat` and `bytes.Buffer`. If `bytes` wasn't already imported, this would be a compile error. Since this is a diff and the test compiles (presumably), `bytes` must already be imported. So this is not an issue.

Let me look for other issues:

7. The `TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter` test:
```go
verJSON := `{"findings":[{"file":"ghost.go","line":99,"problem":"none","verdict":"confirmed"}]}`
require.NoError(t, os.WriteFile(verPath, []byte(verJSON), 0o600))
```
The verification has a finding with `file:"ghost.go", line:99` but the EmitInput has a finding with `File: "a.go", Line: 1`. So the verification finding won't match - good, it's an orphan verdict. The test then asserts the diagnostic is in the buffer. This looks correct.

But wait - the `Reviewers` field in the finding needs to match. Let me check `verdictTallies`:
```go
for _, vfind := range vf.Findings {
    matched := false
    for _, f := range in.Findings {
        if f.File == vfind.File && f.Line == vfind.Line && f.Problem == vfind.Problem {
            matched = true
            break
        }
    }
    if !matched {
        // orphan diagnostic
    }
}
```
The test's EmitInput has `Findings: []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}}` and the verification has `{"findings":[{"file":"ghost.go","line":99,"problem":"none","verdict":"confirmed"}]}`. So the verification finding doesn't match (different file, line, problem). This will trigger the orphan diagnostic. Good.

But wait - the test also needs `Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}}` - this is needed for the Emit function to work properly. The test has this. Good.

8. The `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` test:
```go
blocker := filepath.Join(base, "blocker")
require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
storeDir := filepath.Join(blocker, "scorecard") // under a regular file → MkdirAll fails
```
Then it calls `Emit(in, EmitOpts{Dir: storeDir, Diag: &buf})`. 

The `resolveDir` function is called first. If `storeDir` is a valid path string, `resolveDir` will just return it (it doesn't check if it exists). So `resolveDir` succeeds, and then `Append` is called. `Append` presumably calls `MkdirAll` on the directory, which would fail because `blocker` is a regular file. So the error comes from the loop, and the diagnostic is written to `w`. This should work.

But wait - there's a subtle issue. The `Append` function in the diff isn't shown. It might use `resolveDir` or do its own directory creation. The test assumes `Append` fails when the parent is a regular file. This is a reasonable assumption.

9. Let me check if there are any issues with the `decodeRecord` change. The function now takes `w io.Writer`:
```go
func decodeRecord(line []byte, path string, w io.Writer) (Record, bool) {
```
And `ReadRecords` passes `w` (the resolved writer) to it. This is correct.

10. Let me look for potential issues with the `FindByRunID` adjacent-month diagnostic:
```go
if fromNeighbour {
    _, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
}
```
This calls `diagWriter(opts.Writer)` which resolves to `os.Stderr` if nil. But `ReadRecords` (called in the loop) also resolves `opts.Writer` independently. There's a minor inconsistency: the function could resolve `w` once at the top and use it everywhere, similar to how `Emit` does. But this is a style preference, not a bug.

11. Let me check the `scorecard.go` change more carefully:

```go
verified, refuted, hasVerification := verdictTallies(in, w)
```

The `w` is passed to `verdictTallies`. Good.

12. In `verdictTallies`:
```go
func verdictTallies(in EmitInput, w io.Writer) (verified, refuted map[string]int, present bool) {
	if in.VerificationPath == "" {
		return nil, nil, false
	}
```
If `VerificationPath` is empty, `w` is unused. The Go compiler won't complain about unused parameters, so this is fine.

13. Let me check the `TestEmit_NilDiagDefaultsToStderr` test more carefully:
```go
require.NoError(t, Emit(in, EmitOpts{Dir: dir})) // nil Diag → os.Stderr, must not panic
```
This test doesn't actually trigger any diagnostic. It just verifies that Emit succeeds with nil Diag. The test name says "NilDiagDefaultsToStderr" but it doesn't verify the default behavior - it just verifies no panic. This is a weak test for the stated AC5 requirement.

14. Similarly, `TestStore_ReadRecords_NilWriterDefaultsToStderr`:
```go
recs, err := ReadRecords(path, ReadOpts{}) // nil Writer → os.Stderr, must not panic
```
This test does trigger a diagnostic (the malformed line), but it doesn't capture os.Stderr to verify the diagnostic went there. It just verifies no panic and correct record count. This is a weak test for AC5.

Both of these are weak test assertions for the "default to os.Stderr" behavior. They verify no panic but not that the diagnostic actually reaches os.Stderr. A stronger test would redirect os.Stderr to a buffer and check the content.

This is a legitimate concern - the tests claim to lock AC5 but only verify the no-panic aspect, not the actual default-to-stderr routing. However, this is more of a test quality issue than a bug.

15. Let me check if there are any other issues...

Actually, I notice that in the `scorecard.go` diff, the `Emit` function has:
```go
if opts.NoScorecard {
    return nil
}
w := diagWriter(opts.Diag)
```

But the `diagWriter` function is defined in `store.go`, not `scorecard.go`. Since they're in the same package, this works. But it's worth noting that the helper is in `store.go` while it's used in both files. Not a bug, just a placement observation.

16. Let me also check the `scorecard_test.go` test `TestEmit_NilDiagDefaultsToStderr`:
```go
require.NoError(t, Emit(in, EmitOpts{Dir: dir})) // nil Diag → os.Stderr, must not panic
```
This test doesn't actually trigger a diagnostic. With valid input and no verification, no diagnostic is emitted. So this test only verifies that `diagWriter(nil)` returning `os.Stderr` doesn't cause a panic when the writer is never actually written to. The test name is misleading - it doesn't actually test the "defaults to os.Stderr" behavior, just the "doesn't panic with nil" behavior.

17. One more thing to check - the `scorecard_test.go` adds `assert` import. Let me verify the test file already has `assert` imported... The diff shows new tests using `assert.Contains` and `require.NoError`. The existing test file likely already has these imports (the diff shows `readJSONL` helper which uses `require.NoError` and `assert.Equal`). So the imports are likely fine.

Let me summarize the issues I've found:

1. **Weak test for AC5 (nil Diag defaults to os.Stderr)**: The tests `TestEmit_NilDiagDefaultsToStderr` and `TestStore_ReadRecords_NilWriterDefaultsToStderr` only verify no panic, not that diagnostics actually reach os.Stderr. The test names claim to lock AC5 but the assertions are insufficient. A stronger test would redirect os.Stderr or trigger a diagnostic and verify it appears on the captured output.

2. **Inconsistent style in FindByRunID**: The function calls `diagWriter(opts.Writer)` inline for the adjacent-month diagnostic, while `ReadRecords` (called in the loop) resolves `w` once at the top. Minor style inconsistency, not a bug.

3. **Missing import check for store_test.go**: The new tests use `bytes.Repeat` and `bytes.Buffer`. If `bytes` wasn't already imported, this would fail to compile. But since the diff doesn't show the import being added, it was likely already there.

Actually, I need to be more careful about what constitutes a real issue vs. a style preference. Let me focus on real bugs and significant concerns.

Real issues:
- The weak test assertions for AC5 are a legitimate concern. The tests claim to verify the default-to-stderr behavior but only check for no-panic. This is in scope (the sprint plan explicitly mentions "buffer-injection tests" and AC5).

Let me also check if there's an issue with the `w` variable shadowing or scope in `Emit`:

```go
func Emit(in EmitInput, opts EmitOpts) error {
	if opts.NoScorecard {
		return nil
	}
	w := diagWriter(opts.Diag)

	dir, err := resolveDir(opts.Dir)
	if err != nil {
		_, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
		return err
	}

	verified, refuted, hasVerification := verdictTallies(in, w)
```

This looks correct. `w` is used in the error path and passed to `verdictTallies`.

Actually, I want to double-check one more thing. The `Emit` function has a loop:
```go
for _, rec := range records {
    if err := Append(dir, rec); err != nil {
        _, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
```

This uses `w` which was resolved at the top. Good.

Let me also check if there's an issue with the `Append` function being called with `dir` (the resolved directory) vs. the original `opts.Dir`. The `Append` function in the original code was:
```go
func Append(dir string, rec Record) error {
```

So it takes a dir. The `Emit` function resolves `dir` from `opts.Dir` and passes it to `Append`. This is unchanged. The test `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` uses `Dir: storeDir` where `storeDir` is a path under a regular file. `resolveDir` presumably just returns the path as-is if it's not empty, so `Append` is called with `storeDir`, and `Append`'s `MkdirAll` fails. The diagnostic goes to `w`. This should work.

But wait - the test comment says "pointing the store under a regular file makes Append's MkdirAll fail". This assumes `Append` calls `MkdirAll` on the store directory. Looking at the original `Append` signature: `func Append(dir string, rec Record) error`. The diff doesn't show `Append`'s implementation, but it presumably creates the directory if it doesn't exist. The test relies on this behavior. This is a reasonable assumption given the function name and the test comment.

Actually, I realize I should look at the original `Append` to understand. The diff says:
```
 // Append writes one record as a single JSONL line to the month file derived from
 // rec.RunID, under dir (created lazily with 0700 on first write).
```

So `Append` creates the directory lazily. If `dir` is under a regular file, `MkdirAll` will fail. Good.

Now, let me think about whether there are any other issues...

One thing I notice: in the `scorecard.go` diff, the `Emit` function has the `NoScorecard` check at the very top:
```go
if opts.NoScorecard {
    return nil
}
w := diagWriter(opts.Diag)
```

This means if `NoScorecard` is true, `w` is never resolved. This is fine since no diagnostics are emitted in that case.

But wait - the `TestIntegration_NoScorecardSuppresses` test calls `Emit` with `NoScorecard: true` and then `ReadAll`. The test is about suppression, not about the writer. So the nil Diag with NoScorecard is fine.

Let me also check if there's an issue with the `w` variable being used after `verdictTallies` returns. Looking at the code flow:
1. `w := diagWriter(opts.Diag)` - resolve writer
2. `dir, err := resolveDir(opts.Dir)` - resolve directory
3. If error, write to `w` and return
4. `verdictTallies(in, w)` - pass `w` for verification diagnostics
5. Loop over records, write errors to `w`

This all looks correct.

Let me also check if there's an issue with the `diagWriter` function being called on `opts.Writer` (which is an `io.Writer` interface) vs. `opts.Diag` (also `io.Writer`). Both are interfaces, so the function works correctly with both.

I think the main issues I've identified are:
1. Weak test assertions for AC5 (the "defaults to os.Stderr" tests only check no-panic, not actual routing)
2. Minor style inconsistency in `FindByRunID` (resolving `w` inline vs. once at the top)

The first is a real concern (the tests don't actually verify the stated AC). The second is a style preference, not a bug.

Let me also check if there are any security issues... The sprint plan is about threading a writer through the API. The main security concern would be if the writer is not properly validated. But `io.Writer` is an interface, and any implementation is acceptable. No security issues.

Performance: The `diagWriter` function is called once per diagnostic. No performance concerns.

Error handling: The `_, _ = fmt.Fprintf(w, ...)` pattern discards the error. This is acceptable for diagnostic output (the original code also didn't check the return value of `fmt.Fprintf(os.Stderr, ...)`). Not a regression.

Maintainability: The code is well-structured with the `ReadOpts` and `EmitOpts` structs. The `diagWriter` helper centralizes the default behavior. Good.

Let me also check if there's an issue with the `scorecard.go` import:
```go
import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
```

The `io` import is added. But wait - in `store.go`, `io` was already imported (the original code uses `io.EOF`, `io.ReadAll`, etc.). In `scorecard.go`, `io` is newly added. Let me check if `scorecard.go` uses `io` anywhere else... The diff shows `io.Writer` in the `EmitOpts` struct and the `verdictTallies` parameter. So the import is needed. Good.

Actually, I want to verify one more thing. The `scorecard.go` diff shows:
```go
import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
```

But the original imports might have been different. The diff adds `io` to the import block. This is correct since `io.Writer` is now used.

OK, I think I've covered the main issues. Let me formulate my findings:

1. The two "defaults to os.Stderr" tests are weak - they only verify no panic, not that diagnostics actually reach os.Stderr. This is a legitimate concern for AC5 verification.

2. Minor style inconsistency in `FindByRunID` - resolves `w` inline rather than once at the top.

Actually, let me reconsider #2. In `Emit`, the pattern is:
```go
w := diagWriter(opts.Diag)
```
resolved once at the top, then used throughout. In `FindByRunID`, the pattern is:
```go
_, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
```
resolved inline. And in `ReadRecords`, the pattern is:
```go
w := diagWriter(opts.Writer)
```
resolved once at the top, then passed to `decodeRecord`. So there's an inconsistency between `FindByRunID` and the other functions. This is a minor maintainability concern.

But actually, is this really a maintainability issue? The `FindByRunID` function only uses the writer once (for the adjacent-month diagnostic), so resolving it inline is fine. The `ReadRecords` function uses it multiple times (passed to `decodeRecord`), so resolving once makes sense. The `Emit` function uses it multiple times, so resolving once makes sense. So the pattern is actually appropriate for each function. Not a real issue.

Let me finalize my findings. I think the main legitimate issue is the weak test assertions for AC5. Let me also check if there are any other issues I missed...

Actually, I want to re-examine the `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` test more carefully:

```go
func TestEmit_WriteFailureDiagnosticRoutesToDiagWriter(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	storeDir := filepath.Join(blocker, "scorecard") // under a regular file → MkdirAll fails

	var buf bytes.Buffer
	in := EmitInput{
		RunID:     testRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	err := Emit(in, EmitOpts{Dir: storeDir, Diag: &buf})
	require.Error(t, err, "a store path under a regular file must fail to write")
	assert.Contains(t, buf.String(), "write failed",
		"write-failure diagnostic must route to the injected EmitOpts.Diag")
}
```

The test expects `Emit` to return an error and write "write failed" to the buffer. This relies on `Append` failing when trying to create a directory under a regular file. But what if `resolveDir` fails first? Let me check `resolveDir` - it's not shown in the diff, but it likely just returns the path if it's not empty, or computes the user config dir. Since `storeDir` is not empty, `resolveDir` should return it as-is. So the error comes from `Append`'s `MkdirAll`. Good.

But wait - there's a potential issue. The `Emit` function first calls `resolveDir(opts.Dir)`. If `opts.Dir` is `storeDir` (a non-empty string), `resolveDir` should return it. Then `Append(dir, rec)` is called, which tries to create the directory. Since `storeDir` is under a regular file, `MkdirAll` fails. The error is caught in the loop, written to `w`, and `firstErr` is set. After the loop, `Emit` returns `firstErr`. The test checks for the error and the diagnostic. This should work.

Actually, I realize there might be an issue with the test. The test expects the error to come from `Append` in the loop. But what if `Append` is called with a `dir` that doesn't exist yet? `MkdirAll` would try to create `storeDir` (which is `blocker/scorecard`). Since `blocker` is a regular file, `MkdirAll` would fail with something like "not a directory". This is the expected behavior. Good.

Let me also check if there's an issue with the `scorecard_test.go` test `TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter`:

```go
verJSON := `{"findings":[{"file":"ghost.go","line":99,"problem":"none","verdict":"confirmed"}]}`
```

The verification has a finding with `file:"ghost.go", line:99, problem:"none"`. The EmitInput has a finding with `File: "a.go", Line: 1, Problem: "x"`. These don't match, so the verification finding is an orphan. The diagnostic is written to `w`. Good.

But wait - the verification file is at `verPath` which is `filepath.Join(t.TempDir(), "verification.json")`. And `t.TempDir()` returns a new temp dir each time. So `verPath` is in a different temp dir than `dir` (the store dir). This is fine - `EmitInput.VerificationPath` is used by `verdictTallies` to read the verification file. Good.

OK, I think I've thoroughly reviewed the diff. Let me also check if there are any issues with the MCP handler:

```go
scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: os.Stderr})
```

This passes `os.Stderr` as the Diag. The `EmitForReconcile` function presumably calls `Emit` with these opts. The `Emit` function resolves `w := diagWriter(opts.Diag)` which returns `os.Stderr` (since `opts.Diag` is not nil). So diagnostics go to `os.Stderr`. This is the documented default for the MCP path (AC4). Good.

But wait - the sprint plan says "MCP handleReconcile supplies os.Stderr explicitly via EmitOpts{Diag: os.Stderr}". The diff shows exactly this. Good.

Now let me check the `os` import in `internal/mcp/handlers.go`. The diff adds `os.Stderr`. If `os` wasn't already imported, this would fail. But since the file likely already uses `os` (e.g., for `os.Stat`, `os.ReadFile`, etc.), the import is probably already there. Not a concern.

Let me also check the `cmd/atcr/leaderboard.go` change:
```go
records, err := scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
This passes `cmd.ErrOrStderr()` as the writer. `cmd.ErrOrStderr()` returns `io.Writer`. Good.

And `cmd/atcr/scorecard.go`:
```go
recs, err := scorecard.FindByRunID(dir, runID, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
Same pattern. Good.

And `cmd/atcr/reconcile.go`:
```go
scorecard.EmitForReconcile(reviewDir, res, scorecard.EmitOpts{NoScorecard: noScorecard, Diag: cmd.ErrOrStderr()})
```
Same pattern. Good.

OK, I think the main legitimate issue I've found is the weak test assertions for AC5. Let me also check if there are any other subtle issues...

Actually, I want to check one more thing. The `scorecard.go` diff shows the `Emit` function's error path:

```go
dir, err := resolveDir(opts.Dir)
if err != nil {
    _, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
    return err
}
```

This writes to `w` (the resolved writer) and returns the error. The original code wrote to `os.Stderr` and returned the error. The behavior is the same when `Diag` is nil (default to `os.Stderr`). Good.

But wait - there's a subtle issue. The `resolveDir` function is called with `opts.Dir`. If `opts.Dir` is empty, `resolveDir` might compute the user config dir. If that computation fails (e.g., `os.UserConfigDir()` fails), the error is written to `w` and returned. The test `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` uses `Dir: storeDir` (non-empty), so `resolveDir` should return it as-is. The error comes from `Append` in the loop. Good.

Actually, I realize I should also check if there's an issue with the `w` variable being used after a potential nil `opts.Diag`. The `diagWriter` function handles nil:
```go
func diagWriter(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}
```
So even if `opts.Diag` is nil, `w` is `os.Stderr`, and `fmt.Fprintf(w, ...)` works. Good.

OK, I'm confident the main issue is the weak test assertions for AC5. Let me formulate my findings.

Actually, wait. Let me re-read the test `TestEmit_NilDiagDefaultsToStderr`:

```go
func TestEmit_NilDiagDefaultsToStderr(t *testing.T) {
	dir := t.TempDir()
	in := EmitInput{
		RunID:     testRunID,
		Findings:  []Finding{{File: "a.go", Line: 1, Problem: "x", Reviewers: []string{"bruce"}}},
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "model-a"}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir})) // nil Diag → os.Stderr, must not panic
}
```

This test doesn't trigger any diagnostic. The input is valid, there's no verification file, and the store dir is a temp dir that can be created. So no diagnostic is emitted. The test only verifies that `diagWriter(nil)` returning `os.Stderr` doesn't cause a panic when the writer is never actually written to. The test name claims to verify "NilDiagDefaultsToStderr" but it only verifies "NilDiagDoesntPanic". This is a weak test for AC5.

Similarly, `TestStore_ReadRecords_NilWriterDefaultsToStderr` triggers a malformed record diagnostic, but with nil Writer, it goes to os.Stderr (which is not captured). The test verifies no panic and correct
</think>
