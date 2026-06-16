The user wants me to review a diff based on a sprint plan. Let me analyze the diff carefully against the sprint plan scope.

Sprint plan scope:
- IN SCOPE:
  - The scorecard public read+emit API signatures (or an options-struct carrier)
  - CLI caller wiring (`cmd/atcr`)
  - MCP caller wiring (`internal/mcp/handlers.go`)
  - Converting the existing `fmt.Fprintf(os.Stderr, …)` diagnostics to the threaded writer
  - Tests asserting diagnostic text via an injected buffer

- OUT of scope:
  - The diagnostics' content/wording (unchanged)
  - The JSONL record schema and aggregation logic

Key decisions from clarifications:
- Read path gets `ReadOpts{Writer io.Writer}`
- Emit path gets `Diag io.Writer` field on existing `EmitOpts`
- `Append` left unchanged
- MCP `handleReconcile` supplies `os.Stderr` explicitly via `EmitOpts{Diag: os.Stderr}`
- Nil `Writer`/`Diag` resolves to `os.Stderr` via `diagWriter` helper
- Default `ReadOpts{}` keeps current callers behavior-equivalent

Let me go through the diff file by file:

1. `cmd/atcr/leaderboard.go`: Added `ReadOpts{Writer: cmd.ErrOrStderr()}` to `ReadAll`. In scope (CLI wiring). Looks correct.

2. `cmd/atcr/reconcile.go`: Added `Diag: cmd.ErrOrStderr()` to `EmitOpts`. In scope (CLI wiring). Looks correct.

3. `cmd/atcr/scorecard.go`: Added `ReadOpts{Writer: cmd.ErrOrStderr()}` to `FindByRunID`. In scope (CLI wiring). Looks correct.

4. `internal/mcp/handlers.go`: Added `Diag: os.Stderr` to `EmitOpts`. In scope (MCP wiring). Looks correct per clarifications.

5. `internal/scorecard/integration_test.go`: Added `ReadOpts{}` to `FindByRunID` and `ReadAll` calls. In scope (tests). Looks correct - using zero value to preserve behavior.

6. `internal/scorecard/reconcile_test.go`: Added `ReadOpts{}` to `ReadRecords`. In scope. Looks correct.

7. `internal/scorecard/scorecard.go`:
   - Added `io` import. OK.
   - Added `Diag io.Writer` to `EmitOpts`. OK.
   - `Emit` function: added `w := diagWriter(opts.Diag)`, replaced `fmt.Fprintf(os.Stderr, ...)` with `_, _ = fmt.Fprintf(w, ...)`. OK.
   - `verdictTallies` signature changed to take `w io.Writer`. OK.
   - All diagnostic calls now use `w`. OK.
   
   Wait, I need to check for issues:
   - `_, _ = fmt.Fprintf(w, ...)` - ignoring errors. This is the same pattern used before (original was `fmt.Fprintf(os.Stderr, ...)` which also returns n and error that were ignored). So this is behavior-preserving.
   - Is `diagWriter` defined? It's defined in `store.go` in this diff. Since both files are in the same package (`scorecard`), this is fine.

8. `internal/scorecard/scorecard_test.go`:
   - `readJSONL` updated with `ReadOpts{}`. OK.
   - Three new tests:
     - `TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter`: Tests that orphan verdict warning goes to `Diag`. Uses `bytes.Buffer`. Asserts text. This is AC1.
     - `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter`: Tests write failure diagnostic routes to `Diag`. Good.
     - `TestEmit_NilDiagDefaultsToStderr`: Tests nil `Diag` defaults to stderr without panic. AC5.

   Wait, looking at `TestEmit_NilDiagDefaultsToStderr` - it calls `Emit(in, EmitOpts{Dir: dir})` without `Diag`. This should default to `os.Stderr`. The test just asserts no error and no panic. Seems fine.

9. `internal/scorecard/store.go`:
   - Added `ReadOpts` struct. OK.
   - Added `diagWriter` helper. OK.
   - `ReadRecords` now takes `opts ReadOpts`. OK.
   - Uses `w := diagWriter(opts.Writer)`. OK.
   - Replaces `fmt.Fprintf(os.Stderr, ...)` with `_, _ = fmt.Fprintf(w, ...)`. OK.
   - `decodeRecord` now takes `w io.Writer`. OK.
   - `FindByRunID` takes `opts ReadOpts`. OK.
   - But wait, in `FindByRunID`:
     ```go
     if fromNeighbour {
         _, _ = fmt.Fprintf(diagWriter(opts.Writer), ...)
     }
     ```
     This calls `diagWriter(opts.Writer)` directly inline. But `opts.Writer` might be nil. `diagWriter` handles nil. However, elsewhere in the same function, `ReadRecords` is called with `opts`, and inside `ReadRecords` it calls `diagWriter(opts.Writer)`. Here in `FindByRunID`, it directly calls `diagWriter(opts.Writer)` again. This is fine but redundant? Actually, looking at the code, `FindByRunID` doesn't have a local `w` variable, it just calls `diagWriter(opts.Writer)` inline. That's fine.

   - `ReadAll` takes `opts ReadOpts`. OK.

   Wait, let me check for any issues in `FindByRunID`:
   ```go
   if fromNeighbour {
       _, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
   }
   ```
   This uses `diagWriter(opts.Writer)`. Since `opts.Writer` could be nil and `diagWriter` handles it, this is correct.

   But wait - in `ReadRecords`, we have:
   ```go
   w := diagWriter(opts.Writer)
   ```
   And then uses `w` throughout. Good.

   In `decodeRecord`, takes `w io.Writer`. Good.

10. `internal/scorecard/store_test.go`:
    - Many tests updated to pass `ReadOpts{}`. OK.
    - New tests:
      - `TestStore_ReadRecords_DiagnosticsRouteToInjectedWriter`: Tests over-long line warning routes to injected writer. Good.
      - `TestStore_ReadRecords_MalformedDiagnosticRoutesToInjectedWriter`: Tests malformed record warning routes to injected writer. Good.
      - `TestStore_ReadRecords_NilWriterDefaultsToStderr`: Tests nil Writer defaults to stderr without panic. Good.

Now let me think about potential issues:

**Issue 1: `diagWriter` is defined in `store.go` but used in `scorecard.go`**
Since they're in the same package (`scorecard`), this is fine. But wait, is there a naming collision or visibility issue? No, both files are `package scorecard`. This is correct.

**Issue 2: Unused import in `scorecard.go`?**
The diff shows `io` was added. It is used in `EmitOpts.Diag io.Writer` and `verdictTallies` signature. So it's used.

**Issue 3: Unused import in `store.go`?**
Wait, `store.go` already had `io` import? Let me check the diff. The diff doesn't show imports for `store.go` changing, which means `io` was already imported (for `io.EOF`, `io.Reader`, etc.). Yes, looking at the original code context: `if derr == io.EOF` and `bufio.Reader`. So `io` was already there.

**Issue 4: `FindByRunID` passes `opts` to `ReadRecords` but also uses `diagWriter(opts.Writer)` inline**
In `FindByRunID`, the call `ReadRecords(filepath.Join(dir, m+".jsonl"), opts)` passes `opts`. Inside `ReadRecords`, `diagWriter(opts.Writer)` is called. Then later in `FindByRunID`, it calls `diagWriter(opts.Writer)` again for the neighbour message. This is fine, just slightly inconsistent style (could have declared `w := diagWriter(opts.Writer)` at top like `ReadRecords` does). But this is a style/maintainability issue? Is it worth flagging?

Let me think - is there a correctness issue? If `opts.Writer` is nil, `diagWriter` returns `os.Stderr`. If it's non-nil, it returns the writer. Calling it twice is harmless. But it's a minor inconsistency.

Actually, looking more carefully at `FindByRunID`:
```go
func FindByRunID(dir, runID string, opts ReadOpts) ([]Record, error) {
    ...
    for i, m := range months {
        recs, err := ReadRecords(filepath.Join(dir, m+".jsonl"), opts)
        ...
    }
    if fromNeighbour {
        _, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
    }
    return matches, nil
}
```

This is fine. But I could flag it as LOW maintainability for style inconsistency. However, the sprint plan says "Only flag issues in files/areas directly related to the work items below." This is directly in the modified function. But is it a real issue? It's very minor. The instructions say "No false positives: Only report real issues". I think this is arguably not a real issue since it's just style.

Wait, let me look for something more concrete.

**Issue 5: Missing `t.Parallel()` in new tests?**
Not relevant to this sprint.

**Issue 6: `TestEmit_NilDiagDefaultsToStderr` doesn't actually verify stderr**
The test just ensures no panic and no error. It doesn't capture stderr to verify it actually went there. But AC5 says "Default behavior (no writer supplied) still writes to os.Stderr". The test name says "DefaultsToStderr" but doesn't verify it. However, verifying stderr in a unit test is hard/fragile. The test comment says "preserves prior behavior — emission succeeds and diagnostics fall back to os.Stderr without panicking." The test verifies no panic. But does it verify it goes to stderr? No.

However, the sprint acceptance criteria says "Default to os.Stderr when unset; cover with an existing-behavior test." The test exists and covers that nil doesn't panic and emission succeeds. Is the test sufficient? It might be considered a weak test, but is it an "issue"? Probably not worth flagging.

**Issue 7: `TestStore_ReadRecords_NilWriterDefaultsToStderr` similarly weak**
Same pattern - doesn't verify stderr output, just no panic. But again, probably acceptable.

**Issue 8: `diagWriter` could be exported or named differently?**
It's unexported, which is fine.

**Issue 9: Race condition or nil pointer?**
Looking at `verdictTallies`, it receives `w io.Writer`. What if `Emit` passes `w := diagWriter(opts.Diag)` and then passes `w` to `verdictTallies`. Since `diagWriter` never returns nil (it falls back to `os.Stderr`), `w` is always non-nil. So `fmt.Fprintf(w, ...)` won't panic. This is correct.

**Issue 10: Is `EmitOpts.Diag` properly documented?**
Yes, comment says it defaults to `os.Stderr`.

**Issue 11: Are there any direct `fmt.Fprintf(os.Stderr...)` left?**
The sprint says AC2: No `fmt.Fprintf(os.Stderr, …)` remains in `internal/scorecard/` for operational diagnostics. Let me scan the diff to see if any remain.

In `scorecard.go`:
- `fmt.Fprintf(os.Stderr...)` replaced with `fmt.Fprintf(w...)`
- One remaining `os` usage: `os.ReadFile`, `os.IsNotExist`, etc. Those are fine.

In `store.go`:
- All `fmt.Fprintf(os.Stderr...)` replaced with `fmt.Fprintf(w...)`
- One in `FindByRunID` uses `fmt.Fprintf(diagWriter(opts.Writer), ...)`

So AC2 seems satisfied.

**Issue 12: `cmd/atcr/reconcile.go` suppression flag**
The code says:
```go
scorecard.EmitForReconcile(reviewDir, res, scorecard.EmitOpts{NoScorecard: noScorecard, Diag: cmd.ErrOrStderr()})
```
This looks correct. `EmitForReconcile` presumably passes through to `Emit`.

Wait, I don't see `EmitForReconcile` in the diff. Is it in another file not shown? Let me check if `EmitForReconcile` definition is in the diff. Looking through the diff... I don't see `EmitForReconcile` implementation. It might be in `scorecard.go` but not modified (if it already passes `EmitOpts` through to `Emit`). Let me check... The diff shows `scorecard.go` but only changes to `Emit`, `verdictTallies`, and comments. `EmitForReconcile` is not shown, which means it wasn't changed, which implies it already forwards `EmitOpts` to `Emit`. That's fine.

**Issue 13: `leaderboard.go` `ReadAll` signature change**
```go
records, err := scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
This is correct.

**Issue 14: `scorecard.go` `runScorecard` `FindByRunID`**
```go
recs, err := scorecard.FindByRunID(dir, runID, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
Correct.

**Issue 15: `internal/mcp/handlers.go`**
```go
scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: os.Stderr})
```
Correct per clarifications.

**Issue 16: Potential bug in `FindByRunID` - calling `diagWriter(opts.Writer)` directly**
Actually, wait. There's a subtle issue here. In `FindByRunID`, `opts` is passed by value (not pointer), so `opts.Writer` is whatever the caller passed. If the caller passes `ReadOpts{Writer: &buf}`, then `diagWriter` returns `&buf`. This is fine.

But what about the `ReadRecords` call inside `FindByRunID`? It passes `opts` by value too. Inside `ReadRecords`, it does `w := diagWriter(opts.Writer)`. This is also fine.

Actually, I notice that `ReadRecords` takes `opts ReadOpts` by value. The original signature was `ReadRecords(path string) ([]Record, error)`. Now it's `ReadRecords(path string, opts ReadOpts)`. This matches the plan.

Wait, I should check if there are any compilation issues. `ReadOpts` contains `io.Writer`. Since it's an interface, passing `ReadOpts` by value copies the interface header (type + data pointer), which is fine and efficient.

**Issue 17: Tests in `scorecard_test.go` use `testRunID` which is presumably defined elsewhere.**
Not an issue.

**Issue 18: `TestEmit_WriteFailureDiagnosticRoutesToDiagWriter` sets `storeDir` under a regular file**
```go
blocker := filepath.Join(base, "blocker")
require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
storeDir := filepath.Join(blocker, "scorecard") // under a regular file → MkdirAll fails
```
Then calls `Emit(in, EmitOpts{Dir: storeDir, Diag: &buf})`.
Inside `Emit`:
```go
dir, err := resolveDir(opts.Dir)
if err != nil {
    _, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
    return err
}
```
Then later:
```go
for _, rec := range records {
    if err := Append(dir, rec); err != nil {
        _, _ = fmt.Fprintf(w, "scorecard: write failed: %v\n", err)
        if firstErr == nil {
            firstErr = err
        }
    }
}
```
Wait - if `resolveDir` fails, it returns `err` immediately. So the loop never runs. The test expects `buf.String()` to contain "write failed". It will get it from the `resolveDir` error path. Then `err` is returned. The test asserts `require.Error(t, err)`. Good.

But what if `resolveDir` succeeds but `Append` fails? The test uses `Dir: storeDir` where `storeDir` is under a regular file. If `resolveDir` uses `os.MkdirAll` or similar, it would fail. So the error should come from `resolveDir`. The test expects "write failed" which matches the diagnostic. This seems correct.

**Issue 19: The `diagWriter` function comment says "centralizes the default to os.Stderr rule shared by the read and emit paths"**
But in `scorecard.go`, it's used as `w := diagWriter(opts.Diag)`. In `store.go`, it's used as `w := diagWriter(opts.Writer)`. The function is in `store.go`. That's fine since they're in the same package.

**Issue 20: Unused parameter or variable?**
No.

**Issue 21: `ReadOpts` struct field is named `Writer` but `EmitOpts` field is named `Diag`**
This is intentional per the clarifications: "Read path gets `ReadOpts{Writer io.Writer}`" and "Emit path gets `Diag io.Writer`". So this is by design, not an issue.

**Issue 22: Missing updates to `Aggregate` or `export`?**
The sprint plan mentions `ReadAll, FindByRunID, ReadRecords` as in scope. It doesn't mention `Aggregate` or `export`. The clarifications say "OUT of scope: diagnostic wording, JSONL schema, aggregation logic". The diff shows `ReadAll` updated. If `Aggregate` calls `ReadAll`, it would need to be updated. But I don't see `Aggregate` in the diff. Is it in a file not shown? If `Aggregate` was not updated, the code wouldn't compile because `ReadAll` signature changed. Wait, the diff only shows changed files. If `Aggregate` calls `ReadAll`, it must have been updated or it would fail compilation. But maybe `Aggregate` is in another file that wasn't changed? No, if `ReadAll` signature changed, all callers must be updated. The diff shows all callers in the files provided. Let me check if there are other callers...

Looking at the diff, `ReadAll` is called in:
- `cmd/atcr/leaderboard.go` (updated)
- `internal/scorecard/integration_test.go` (updated)
- `internal/scorecard/store_test.go` (updated)

Is there an `Aggregate` function that calls `ReadAll`? I don't see it in the diff. It might be in a file not included in the diff, meaning it wasn't changed in this commit, which would be a compilation error if it exists and calls `ReadAll`. But the diff is what I'm reviewing. The user likely expects me to only review the diff provided. If `Aggregate` exists elsewhere and wasn't updated, the code wouldn't compile, but since I don't see it in the diff, I shouldn't flag it unless it's in the diff. The sprint plan mentions "ReadAll, FindByRunID, Aggregate, export" in the context of ReadRecords comment, but that's just a comment in the code about performance, not sprint scope.

Wait, actually looking at the `ReadRecords` comment in `store.go`:
```go
// The current signature is used across the read surface (ReadAll, FindByRunID,
// Aggregate, export) so expanding it to return the number of lines consumed is
// out of scope absent real data-volume pressure — it changes this read API and every
// caller (ReadAll, FindByRunID, Aggregate, export) and would need explicit
// sign-off.
```
This is a pre-existing comment. Not related to this change.

**Issue 23: `verdictTallies` signature change**
It now takes `w io.Writer`. This is an unexported function, so it's fine.

**Issue 24: `decodeRecord` signature change**
Takes `w io.Writer`. Unexported. Fine.

**Issue 25: Missing error check on `fmt.Fprintf`?**
The original code used `fmt.Fprintf(os.Stderr, ...)` and ignored the return value. The new code uses `_, _ = fmt.Fprintf(w, ...)` which explicitly ignores the return values. This is equivalent. Is it an issue? The sprint says "Not redirectable — the CLI threads cmd.ErrOrStderr() for its own output, but scorecard diagnostics escape that". The fix is to route through the writer. Ignoring the error on `fmt.Fprintf` is standard for diagnostics - if the diagnostics writer fails, what can you do? This is acceptable.

However, wait - if `w` is `os.Stderr` and `fmt.Fprintf` returns an error, previously it was silently ignored. Now it's explicitly ignored with `_, _ =`. This is fine.

**Issue 26: `TestEmit_NilDiagDefaultsToStderr` - potential data race?**
No, it's sequential.

**Issue 27: `bytes` import in `scorecard_test.go`**
Added `bytes` import. Used for `bytes.Buffer`. Good.

**Issue 28: `ReadRecords` defer**
```go
defer func() { _ = f.Close() }()
```
This already existed. Not an issue.

**Issue 29: `FindByRunID` calls `diagWriter(opts.Writer)` for the neighbour diagnostic, but doesn't create a local variable**
This is slightly inconsistent with `ReadRecords` which does `w := diagWriter(opts.Writer)`. But more importantly, what if `opts.Writer` is nil? `diagWriter` returns `os.Stderr`. This is correct per AC5.

But wait, I need to look at the actual line in `FindByRunID`:
```go
_, _ = fmt.Fprintf(diagWriter(opts.Writer), "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
```
This is fine. However, `diagWriter` allocates nothing, it's just a helper. Calling it inline is okay.

**Issue 30: `Emit` function - `w := diagWriter(opts.Diag)`**
Good.

**Issue 31: `Emit` - what about `NoScorecard` path?**
```go
if opts.NoScorecard {
    return nil
}
w := diagWriter(opts.Diag)
```
If `NoScorecard` is true, it returns early before creating `w`. That's fine since no diagnostics should be emitted when suppressed.

**Issue 32: `EmitForReconcile` - is it updated?**
Not shown in diff. But if it passes `opts` to `Emit`, then `opts.Diag` is forwarded. The test `TestEmitForReconcile_BridgesPoolSummaryAndFindings` calls `EmitForReconcile` but doesn't pass `EmitOpts` with `Diag`. Wait, looking at the diff for `reconcile_test.go`:
```go
recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
```
But I don't see `EmitForReconcile` calls in the diff. Are they unchanged? If `EmitForReconcile` takes `EmitOpts` and passes it through, and the tests call it with zero-value `EmitOpts{}`, then `Diag` is nil and defaults to `os.Stderr`. That's correct per AC5.

Wait, let me look at `reconcile_test.go` more carefully. The diff shows:
```go
@@ -44,7 +44,7 @@ func TestEmitForReconcile_BridgesPoolSummaryAndFindings(t *testing.T) {
 
 	cfg, err := os.UserConfigDir()
 	require.NoError(t, err)
-	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"))
+	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
 	require.NoError(t, err)
 	require.Len(t, recs, 3, "2 reviewer records + 1 aggregate")
```
The `EmitForReconcile` call itself is not in the diff, meaning it wasn't changed. But `EmitForReconcile` presumably already takes `EmitOpts` and this test calls it somewhere. If `EmitForReconcile` signature includes `EmitOpts`, and the test passes `scorecard.EmitOpts{}` (or similar), that's fine since nil `Diag` defaults to `os.Stderr`.

Actually, looking at the diff again for `reconcile_test.go`, only `ReadRecords` calls are changed. The `EmitForReconcile` call might be passing `EmitOpts{}` already, which is fine.

**Issue 33: Are there any remaining `fmt.Fprintf(os.Stderr` in the diff?**
Let me scan mentally:
- `internal/scorecard/scorecard.go`: All replaced with `w`. One import of `os` remains for `os.ReadFile`, `os.IsNotExist`.
- `internal/scorecard/store.go`: All replaced with `w` or `diagWriter(opts.Writer)`.

Wait, in `store.go`, the import block is not shown in the diff (presumably unchanged). `os` is still imported for `os.Open`, `os.ReadDir`, etc.

**Issue 34: `store.go` - `FindByRunID` passes `opts` to `ReadRecords` but `ReadRecords` now takes `ReadOpts`**
Yes, and the diff shows `recs, err := ReadRecords(filepath.Join(dir, m+".jsonl"), opts)`. This is correct.

**Issue 35: `FindByRunID` has `var fromNeighbour bool` and sets it to `true` when matches found in adjacent month**
The logic:
```go
for i, m := range months {
    recs, err := ReadRecords(filepath.Join(dir, m+".jsonl"), opts)
    ...
    for _, r := range recs {
        if r.RunID == runID {
            matches = append(matches, r)
            if i > 0 {
                fromNeighbour = true
            }
        }
    }
}
if fromNeighbour {
    _, _ = fmt.Fprintf(diagWriter(opts.Writer), ...)
}
```
Wait, `months` is returned by `monthsToScan(runID, month)`. It probably returns `[month, nextMonth]` or similar. `i > 0` means it's from a neighbouring month (not the first month in the list). This logic is unchanged. Good.

**Issue 36: `ReadAll` signature change**
```go
func ReadAll(dir string, opts ReadOpts) ([]Record, error) {
```
And passes `opts` to `ReadRecords`. Good.

**Issue 37: Potential nil panic in `fmt.Fprintf` if `w` is nil?**
`diagWriter` ensures `w` is never nil. But what about the direct call in `FindByRunID`? `diagWriter(opts.Writer)` is never nil. Good.

**Issue 38: What about the `Emit` function's `verdictTallies` call?**
```go
verified, refuted, hasVerification := verdictTallies(in, w)
```
And `verdictTallies` uses `w` for diagnostics. Good.

**Issue 39: Are the tests comprehensive enough?**
- Read path diagnostics: over-long line, malformed record, nil writer default.
- Emit path diagnostics: orphan verdict, write failure, nil diag default.
- AC1 covered by `TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter` and `TestStore_ReadRecords_DiagnosticsRouteToInjectedWriter` / `MalformedDiagnosticRoutesToInjectedWriter`.
- AC2 covered by code inspection (all `os.Stderr` diagnostics converted).
- AC3 covered by `cmd/atcr/reconcile.go` using `cmd.ErrOrStderr()`.
- AC4 covered by `internal/mcp/handlers.go` using `os.Stderr`.
- AC5 covered by nil-default tests.

Actually, wait. Let me look at `scorecard_test.go` line 341:
```go
// TestEmit_NilDiagDefaultsToStderr locks Epic 3.4 AC5: a zero EmitOpts (nil Diag)
// preserves prior behavior — emission succeeds and diagnostics fall back to
// os.Stderr without panicking.
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
This test doesn't actually verify that diagnostics went to `os.Stderr`. It only verifies no panic and emission succeeds. The name says "DefaultsToStderr" but the assertion doesn't verify the sink. However, this might be acceptable since capturing stderr in parallel tests is hard. But is it a correctness issue in the test? The comment says "preserves prior behavior — emission succeeds and diagnostics fall back to os.Stderr without panicking." The test only checks "without panicking" and "emission succeeds". It doesn't check "diagnostics fall back to os.Stderr".

Similarly for `TestStore_ReadRecords_NilWriterDefaultsToStderr`.

But these are tests. Is the test code itself buggy? Not really, just incomplete. But the sprint acceptance criteria specifically says "Default behavior (no writer supplied) still writes to os.Stderr; all existing scorecard tests pass." The test ensures it doesn't panic. Verifying actual stderr output would require more complex setup.

I think these tests are acceptable.

**Issue 40: `FindByRunID` - no `opts` validation?**
No need.

**Issue 41: `ReadOpts` vs `EmitOpts` naming inconsistency**
The read struct field is `Writer`, the emit struct field is `Diag`. This was an explicit design decision per clarifications: "Read path gets `ReadOpts{Writer io.Writer}`", "Emit path gets `Diag io.Writer` field on existing `EmitOpts`". So not an issue.

**Issue 42: Missing `Diag` in `EmitOpts` comment?**
No, the comment was updated:
```go
// EmitOpts controls emission side-effects. NoScorecard suppresses all I/O (the
// --no-scorecard gate; checked first, before any directory creation). Dir
// overrides the store root (tests pin a temp dir); empty means the default user
// config dir. Diag is the sink for operational diagnostics (write failures,
// verification read/parse failures, orphan verdicts); a nil Diag defaults to
// os.Stderr so existing callers keep their prior behavior (Epic 3.4).
```
This is good.

**Issue 43: `store.go` - `ReadOpts` comment**
```go
// ReadOpts carries read-path options for the scorecard store. Writer is the sink
// for operational diagnostics emitted while reading (malformed records, over-long
// lines, adjacent-month spans); a nil Writer defaults to os.Stderr so existing
// callers keep their prior behavior (Epic 3.4).
```
Good.

**Issue 44: `integration_test.go` - `ReadOpts{}` used**
Tests use zero value. Good.

**Issue 45: `scorecard_test.go` `readJSONL` helper uses `ReadOpts{}`**
Good.

**Issue 46: Are there any places where `ReadOpts` is passed but field names are wrong?**
No.

**Issue 47: `mcp/handlers.go` comment**
The comment was updated nicely to explain why `os.Stderr` is used.

**Issue 48: Does `EmitForReconcile` exist and is it properly handling the new `Diag` field?**
Since `EmitForReconcile` is not in the diff, but it must exist. If it takes `EmitOpts`, then passing `EmitOpts{Diag: os.Stderr}` works. If it doesn't take `EmitOpts`, then the diff in `mcp/handlers.go` wouldn't compile. Since the diff shows `EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: os.Stderr})`, and the code presumably compiles, `EmitForReconcile` must accept `EmitOpts`. The fact that it's not in the diff means it was already taking `EmitOpts` and just forwards it to `Emit`. This is fine.

Wait, but if `EmitForReconcile` was already defined and takes `EmitOpts`, and the diff doesn't show it, I can't review its implementation. But the sprint scope includes `EmitForReconcile`. The sprint plan says: "Threading through `ReadRecords`, `decodeRecord`, `FindByRunID`, `ReadAll`, `Emit`, `verdictTallies`, `EmitForReconcile`". The diff shows all of these except `EmitForReconcile`'s body. But `EmitForReconcile` is likely a thin wrapper around `Emit` and already took `EmitOpts`. So no change needed there.

**Issue 49: `cmd/atcr/leaderboard.go` `ReadAll` call**
```go
records, err := scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
Good.

**Issue 50: `cmd/atcr/scorecard.go` `FindByRunID` call**
```go
recs, err := scorecard.FindByRunID(dir, runID, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})
```
Good.

**Issue 51: Potential bug - `cmd.ErrOrStderr()` can return nil?**
Looking at cobra documentation, `ErrOrStderr()` returns `os.Stderr` if no error writer is set, but it should never return nil. Even if it did, `diagWriter` would handle it (since nil writer defaults to `os.Stderr`). So safe.

**Issue 52: `FindByRunID` calls `diagWriter(opts.Writer)` directly, but `opts` could be...**
It's fine.

Let me think harder. Is there any logic error introduced?

**Issue 53: `ReadRecords` now takes `opts ReadOpts`. What if existing external callers exist?**
The sprint plan says this is a package API change. The diff updates all in-repo callers. External callers would need updating, but that's expected.

**Issue 54: `scorecard.go` `verdictTallies` - the `io.Writer` parameter is named `w` but is it shadowed?**
No.

**Issue 55: `store.go` `decodeRecord` takes `w io.Writer`. Is there any issue with passing `w` when it's `os.Stderr` and json unmarshal fails?**
No, it just prints the diagnostic and returns false. Good.

**Issue 56: `TestStore_ReadRecords_DiagnosticsRouteToInjectedWriter`**
```go
huge := bytes.Repeat([]byte("x"), maxLineBytes+1024) // one line over the cap
var content []byte
content = append(content, huge...)
content = append(content, '\n')
content = append(content, good...)
content = append(content, '\n')
```
This creates a line longer than `maxLineBytes`. The scanner uses `bufio.Reader.ReadBytes('\n')` with a limited buffer? Wait, looking at `ReadRecords`:
```go
const maxLineBytes = 1 << 20

// ReadRecords...
func ReadRecords(path string, opts ReadOpts) ([]Record, error) {
    ...
    br := bufio.NewReader(f)
    for {
        frag, err := br.ReadBytes('\n')
        if err == bufio.ErrBufferFull {
            // Line exceeds maxLineBytes...
```
Wait, `bufio.Reader.ReadBytes('\n')` can return `bufio.ErrBufferFull`? Let me check. Actually, `bufio.Reader.ReadBytes(delim)` reads until delim. If the buffer is full and delim not found, it returns the buffer and `bufio.ErrBufferFull`. The buffer size for `bufio.NewReader` is default (4096). But `maxLineBytes` is 1<<20 (1MB). 

Wait, this is a potential issue in the ORIGINAL code, not introduced by this diff. `bufio.NewReader` default buffer size is 4KB. If a line is longer than 4KB, `ReadBytes('\n')` will return `bufio.ErrBufferFull` even if it's less than `maxLineBytes`. But the code comment says "A bufio.Reader (not bufio.Scanner) is used so a single over-long line can be detected without doubling memory."

Actually, looking at `bufio.Reader.ReadBytes`:
> ReadBytes reads until the first occurrence of delim in the input, returning a slice containing the data up to and including the delimiter. If ReadBytes encounters an error before finding a delimiter, it returns the data read before the error and the error itself (often io.EOF). ReadBytes returns err != nil if and only if the returned data does not end in delim. For simple uses, a Scanner may be more convenient.

Wait, does `ReadBytes` return `ErrBufferFull`? Let me check go doc:
> ReadBytes reads until the first occurrence of delim in the input... If ReadBytes encounters an error before finding a delimiter, it returns the data read before the error and the error itself (often io.EOF). 

Actually, looking at the Go source for `bufio.Reader.ReadBytes`:
```go
func (b *Reader) ReadBytes(delim byte) ([]byte, error) {
    full, err := b.ReadSlice(delim)
    ...
}
```

And `ReadSlice`:
```go
func (b *Reader) ReadSlice(delim byte) (line []byte, err error) {
    for {
        i := bytes.IndexByte(b.buf[b.r:b.w], delim)
       