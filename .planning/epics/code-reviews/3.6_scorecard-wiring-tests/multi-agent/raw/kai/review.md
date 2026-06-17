The user wants me to review a diff against a sprint plan. Let me analyze what's in scope and what the diff contains.

Scope from sprint plan:
- AC3: CLI entry points (leaderboard, scorecard, reconcile) - add wiring tests in `cmd/atcr/*_test.go` (specifically `cmd/atcr/scorecard_wiring_test.go`)
- AC4: MCP handler - add `engine.diag io.Writer` seam in `internal/mcp/handlers.go` and test in `internal/mcp/handlers_test.go`

Files in diff:
1. `cmd/atcr/scorecard_wiring_test.go` - new file, CLI wiring tests
2. `internal/mcp/handlers.go` - production change adding `diag io.Writer` and `diagWriter()` method
3. `internal/mcp/handlers_test.go` - new test `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`

Let me review each file for issues.

**cmd/atcr/scorecard_wiring_test.go:**

Looking at `seedMalformedStore`:
- `month := runID[:7]` - assumes runID is at least 7 chars. The runID passed in tests is `"2026-06-14T10:00:00Z-corrupt"` which is longer than 7, so this works. But it's a bit fragile if someone passes a shorter runID. However, this is test helper code and the callers are controlled. Not a huge issue.

Looking at `execCmdSplit`:
- Returns `exitCode(err)`, but if `err` is not an `exec.ExitError`, `exitCode` likely returns 0 or some default. The function seems fine.

Looking at `TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr`:
- `fixtureReview(t, "2026-06-10_feat", map[string]string{...})` - sets up a review fixture
- Then gets `dir, err := scorecard.DefaultDir()` and creates the store path as a file to cause ENOTDIR
- Then calls `execCmdSplit(t, "reconcile", "2026-06-10_feat")`
- The test asserts exit code 0 and stderr contains "scorecard: write failed"

Wait, there's a potential issue. The test calls `fixtureReview(t, "2026-06-10_feat", ...)` BEFORE calling `scorecard.DefaultDir()` and creating the file. If `fixtureReview` triggers any scorecard write operations (which it might, since reconcile emits scorecards), and if that happens before the test creates the file at `scorecard.DefaultDir()`, then the store directory might be created as a directory first, and then the file creation fails, or vice versa. But actually, looking at the test flow:
1. `fixtureReview` creates source files
2. Then test gets `DefaultDir()` and creates parent dir, then writes a file at `dir`
3. Then runs reconcile

Actually, wait - `fixtureReview` just creates source files in `root` (the temp dir). It doesn't seem to touch scorecard. So this is probably fine.

But let me look more carefully at `seedMalformedStore` in the first test:
- `storeRecord(t, reviewerRec(runID, reviewer, "claude-sonnet-4-6", 12, 7))` - wait, is `storeRecord` available in this package? This file is in `package main`, same as other CLI test files presumably. It references `isolate(t)`, `storeRecord`, `reviewerRec`, `fixtureReview` - these must be helpers defined in other test files in the same package. That's fine.

Let me look for actual bugs:

In `seedMalformedStore`:
```go
f, err := os.OpenFile(filepath.Join(dir, month+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
require.NoError(t, err)
_, _ = f.WriteString("{not valid json\n")
require.NoError(t, f.Close())
```
Issue: If `f.WriteString` returns an error, it's ignored. But `require.NoError(t, f.Close())` is checking close error. Missing write error check. This could be considered error-handling. But it's test code and a helper. If WriteString fails, the test might pass unexpectedly (malformed record not there, diagnostic not emitted). This is a real issue but minor.

Wait, actually, looking at the diff again. I need to be careful about what's IN SCOPE. The sprint plan says the scope is test additions and the ~3-line production change in `internal/mcp/handlers.go`.

Is there a correctness issue with `month := runID[:7]`? The comment says "YYYY-MM month-file stem (mirrors monthFromRunID)". If runID doesn't have this format, it will panic. But the callers use fixed strings that are long enough.

Let me look at `internal/mcp/handlers.go`:

```go
func (e *engine) diagWriter() io.Writer {
	if e.diag == nil {
		return os.Stderr
	}
	return e.diag
}
```
This looks correct. The production change is minimal and correct.

In `handleReconcile`:
```go
scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: e.diagWriter()})
```
This correctly uses the diagWriter.

Wait, is there a race condition or lifecycle issue? The `engine` struct has a `bg sync.WaitGroup`. The `diag` field is added. Is `diag` accessed concurrently? In the handler, it's read synchronously. In tests, it's set before the call. No issue.

Let me look at `internal/mcp/handlers_test.go`:

```go
func TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root)

	storeDir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(storeDir), 0o755))
	require.NoError(t, os.WriteFile(storeDir, []byte("x"), 0o600))

	var buf bytes.Buffer
	e := &engine{root: root, diag: &buf}
	_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err, "a best-effort scorecard failure must not fail the reconcile")
	require.Contains(t, buf.String(), "scorecard: write failed",
		"handleReconcile must wire engine.diag into EmitOpts.Diag")
}
```

Wait, there's an issue here. The `engine` struct is initialized with `root` and `diag`, but not with `completer` or `log`. If `handleReconcile` uses `e.completer` or `e.log`, this might panic or fail. Let me think about what `handleReconcile` does.

Looking at the existing test `TestReconcileHandler_LatestMergesToHighConfidence` and `TestReconcileHandler_FailOnGate`, they construct the engine with:
```go
e := &engine{root: root, completer: fakeCompleter{}}
```

In the new test:
```go
e := &engine{root: root, diag: &buf}
```

If `handleReconcile` uses `e.completer`, then this test will panic with nil pointer dereference or behave unexpectedly. Looking at the existing tests, they all set `completer: fakeCompleter{}`. The new test doesn't. Is `completer` used in the reconcile path?

I need to check the `handleReconcile` function (though I only see a snippet in the diff). The snippet shows scorecard emission at the end. But earlier in `handleReconcile`, it likely calls the completer to do the actual review. If `e.completer` is nil, that would be a nil pointer dereference.

Wait, the diff doesn't show the full `handleReconcile` function, but looking at existing tests like `TestReconcileHandler_LatestMergesToHighConfidence`, they all include `completer: fakeCompleter{}`. The new test does NOT include a completer. This seems like a bug - the test will likely panic when `handleReconcile` tries to use `e.completer`.

Actually, let me reconsider. Is it possible that `handleReconcile` doesn't use `completer` directly? Let me think... The `engine` struct has a `completer` field. The reconcile handler probably does use it. The fact that every other test includes `fakeCompleter{}` strongly suggests the new test should too. This is a correctness issue in the test.

Also, the new test doesn't set `log`. If `handleReconcile` logs things, a nil logger might panic if methods are called on it. But `*slog.Logger` might be checked for nil? Actually, calling methods on a nil `*slog.Logger` might panic. But other tests might not set it either? Looking at the diff, existing tests show `e := &engine{root: root, completer: fakeCompleter{}}` without log. So maybe log is optional or checked.

But the missing `completer` is suspicious. Let me look at the diff again - the new test in handlers_test.go definitely does not set completer. This is almost certainly a bug.

Wait, looking more carefully at the diff:
```
+func TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag(t *testing.T) {
+	isolateUserConfig(t)
+	root := t.TempDir()
+	reviewFixture(t, root)
+
+	storeDir, err := scorecard.DefaultDir()
+	require.NoError(t, err)
+	require.NoError(t, os.MkdirAll(filepath.Dir(storeDir), 0o755))
+	require.NoError(t, os.WriteFile(storeDir, []byte("x"), 0o600))
+
+	var buf bytes.Buffer
+	e := &engine{root: root, diag: &buf}
+	_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
+	require.NoError(t, err, "a best-effort scorecard failure must not fail the reconcile")
+	require.Contains(t, buf.String(), "scorecard: write failed",
+		"handleReconcile must wire engine.diag into EmitOpts.Diag")
+}
```

Yes, missing `completer`. That's a correctness issue. HIGH or MEDIUM? It'll cause a panic (nil pointer dereference), so HIGH.

Another issue in `cmd/atcr/scorecard_wiring_test.go`:

In `seedMalformedStore`:
```go
_, _ = f.WriteString("{not valid json\n")
```
Ignoring write error. If the write fails, the malformed line isn't there, no diagnostic is emitted, test fails with a confusing message. Should check error. But this is test setup. Category: error-handling. Severity: LOW or MEDIUM? Since it's test code and the failure mode is just a test failure, probably LOW. But wait, if the write fails silently, the test will fail at `require.Contains(t, stderr, "skipping malformed record")` with a message that doesn't indicate the setup failed. So it's a maintainability/debuggability issue. I'll flag it as LOW.

Actually, looking at it again, the `require.NoError(t, f.Close())` at the end - if `WriteString` failed, `Close` might also fail or succeed, but the malformed data isn't written. The error from WriteString is swallowed.

Another potential issue: `seedMalformedStore` uses `runID[:7]` without checking length. But the test uses fixed runIDs that are long enough. I don't think this needs to be flagged as it's controlled test input.

What about the reconcile CLI test?
```go
fixtureReview(t, "2026-06-10_feat", map[string]string{
    "sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
})
```
Wait, in the comment at the top of `TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr`, it says "Pointing the resolved store path at a regular file makes Append's MkdirAll fail". But `fixtureReview` might create files under `root`. Then the test gets `scorecard.DefaultDir()` which might resolve based on environment/home dir, not under `root`. So `fixtureReview` and the scorecard store are in different locations. That seems fine - the review runs in `root`, and scorecard tries to write to the user's default store dir. That's the intended behavior.

But wait - `isolate(t)` is called. Does `isolate(t)` redirect `scorecard.DefaultDir()` to somewhere under the temp dir? Looking at other tests, probably yes. So `scorecard.DefaultDir()` returns a path in the temp dir. Then `fixtureReview` also works in that temp dir? Actually `fixtureReview` takes a name and a map, and probably creates a git repo structure. It might not interact with scorecard. Then the test replaces the store dir with a file. This seems fine.

Let me look at `execCmdSplit`. It calls `exitCode(err)`. If `root.ExecuteContext` returns an error that's not an exec.ExitError, `exitCode` presumably returns 1 or 0? The function is not shown in the diff, but it must be defined elsewhere in the package. That's fine.

One more thing in `cmd/atcr/scorecard_wiring_test.go`: The `seedMalformedStore` function opens a file with `os.O_APPEND|os.O_WRONLY`, writes to it, but ignores the write error. I think this is worth flagging.

Actually, looking at the `seedMalformedStore` more carefully:
```go
f, err := os.OpenFile(filepath.Join(dir, month+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
require.NoError(t, err)
_, _ = f.WriteString("{not valid json\n")
require.NoError(t, f.Close())
```
If `WriteString` returns an error, it's discarded. Then `Close` is checked. But if write failed, the test data is bad. This is a real issue, albeit minor.

Wait, is there an issue with `os.O_WRONLY` without `os.O_CREATE`? If the file doesn't exist because `storeRecord` didn't create it (e.g., if `storeRecord` writes to a different path), `OpenFile` will fail. But `storeRecord` presumably creates it. The test assumes this.

Another issue: In `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`, the test doesn't set `e.completer`. Looking at `TestReconcileHandler_LatestMergesToHighConfidence` in the same file (visible in diff):
```go
e := &engine{root: root, completer: fakeCompleter{}}
```

So the new test is missing `completer`. This is very likely a nil pointer dereference waiting to happen. The severity should be HIGH because it causes a panic (test crash).

Let me double-check: Is there any chance `handleReconcile` doesn't use `completer`? Looking at the function name and the engine struct, it almost certainly does. And even if it somehow didn't, every other test provides it, which is strong evidence. This is a correctness issue in the new test.

Are there any other issues?

In `internal/mcp/handlers.go`, the `diagWriter` method:
```go
func (e *engine) diagWriter() io.Writer {
	if e.diag == nil {
		return os.Stderr
	}
	return e.diag
}
```
This is fine. But wait, `e.diag` is an `io.Writer`. If it's a `bytes.Buffer` or something not safe for concurrent use, and if `handleReconcile` is called concurrently... But there's no indication of concurrent access to `e.diag` in the diff.

One more thing: In `cmd/atcr/scorecard_wiring_test.go`, the `execCmdSplit` function doesn't call `t.Helper()`? Wait, it does: `t.Helper()` at the top. Good.

In `seedMalformedStore`, the `month := runID[:7]` - what if `runID` is exactly the format used? The runIDs used are `"2026-06-14T10:00:00Z-corrupt"` and `"2026-06-10T..."` in reconcile test. Wait, reconcile test doesn't use `seedMalformedStore`.

Actually, looking at the first two tests, `seedMalformedStore` is called with `"2026-06-14T10:00:00Z-corrupt"`, so `month` is `"2026-06"`. That's correct.

I think the main issues are:
1. Missing `completer` in `engine` construction in new MCP test -> HIGH, correctness
2. Ignored `WriteString` error in `seedMalformedStore` -> LOW, error-handling

Wait, let me look once more at `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`. The test doesn't call `t.Parallel()`, but that's not an issue.

Is there any issue with `reviewFixture(t, root)` followed by creating a file at `scorecard.DefaultDir()`? If `reviewFixture` somehow triggers a scorecard write that creates the store directory, then `os.WriteFile(storeDir, ...)` would fail because the directory already exists. But `reviewFixture` probably just creates source files for review, and doesn't run the actual review logic. The review logic is only run inside `handleReconcile`. So this should be fine.

Actually, wait. `reviewFixture(t, root)` - does this run a review or just set up files? Looking at the name and context, it probably sets up files. Then `handleReconcile` runs the review. So before `handleReconcile`, the scorecard store doesn't exist yet. The test creates the parent dir and puts a file where the store dir should be. Then `handleReconcile` runs, tries to emit scorecard, `MkdirAll` fails because a file exists at that path. This is exactly what the test wants. Good.

One more potential issue: In `cmd/atcr/scorecard_wiring_test.go`, `execCmdSplit` captures stdout and stderr separately. It calls `root.SetOut(&outBuf)` and `root.SetErr(&errBuf)`. However, if `newRootCmd()` returns a command tree that doesn't properly propagate SetOut/SetErr to subcommands, the output might still go to os.Stdout/Stderr. But that's what the test is testing - the wiring. And the test passes according to the sprint (implied). So no issue there.

Actually, I should check if `newRootCmd()` and the command setup properly respect `SetErr`. The tests are asserting they do. If they didn't, the tests would fail. Since this is a code review of the diff, I should assume the underlying command structure exists and works.

Let me reconsider the `WriteString` error. In `seedMalformedStore`:
```go
_, _ = f.WriteString("{not valid json\n")
```
This is definitely swallowing an error. The fix is to check the error. It's test helper code. I'll flag it as LOW.

Are there any security issues? None obvious.

Performance issues? None.

Maintainability? The `runID[:7]` slicing without length check could panic if misused, but it's test helpers with controlled callers. I don't think I need to flag it.

Wait, one more thing. In `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`:
```go
_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
```
The return signature of `handleReconcile` - looking at the existing code, handlers probably return `(*mcpsdk.CallToolResult, error)`. Wait, the test ignores the first two return values. But the function signature from the context of MCP handlers is likely `func (e *engine) handleReconcile(ctx context.Context, req *mcpsdk.CallToolRequest, args ReconcileArgs) (*mcpsdk.CallToolResult, error)`. The test assigns `_, _, err = ...` which matches 3 return values? Wait:

```go
_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
```

If `handleReconcile` returns two values (`result, error`), then `_, _, err =` would be a compile error. If it returns three values, it's fine. Looking at the existing tests in the diff:

```go
func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
    // ...
    e := &engine{root: root, completer: fakeCompleter{}}
    res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
```

Wait, in `TestReconcileHandler_LatestMergesToHighConfidence`, it shows `res, err :=` - two return values. But the new test shows `_, _, err =` - three return values! That's a discrepancy. Let me look at the diff snippet again.

Actually, looking at the diff for `TestReconcileHandler_LatestMergesToHighConfidence`:
```go
func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
	// ...
	e := &engine{root: root, completer: fakeCompleter{}}
	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
	// ...
}
```

And the new test:
```go
_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
```

This is suspicious. Does `handleReconcile` return 2 or 3 values? It seems like it returns 2 values (`*mcpsdk.CallToolResult, error`). Then the new test would not compile. Or maybe the signature changed? Looking at `handleReconcile` signature in the diff:
```go
func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) (something) {
```

The diff snippet doesn't show the return values. But looking at existing usage: `res, err :=` suggests 2 values. The new test `_, _, err =` suggests 3 values. This is likely a bug! The new test won't compile, or the existing test was already using 3 values and the diff is misleading.

Wait, let me look at the diff more carefully. In the `handlers_test.go` diff, the existing test code shown is:
```go
func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
	// ...
	e := &engine{root: root, completer: fakeCompleter{}}
	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
	// ...
}
```

But is `res` used? The next line after the diff jump might tell us. The diff shows:
```go
	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
}
```

Actually, I can't see the full body. But if `handleReconcile` returns 2 values, then `_, _, err =` is wrong. This would be a HIGH correctness issue.

Wait, looking at the `handleReconcile` signature in the diff:
```go
func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) {
```

Wait no, the diff shows:
```go
-func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) {
+func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) {
```

Actually the diff doesn't show the return type clearly because it's part of the unchanged line. Let me look at the context. The diff shows:
```go
-func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) {
+func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) {
```

Wait, the diff format shows the function signature line unchanged? No, looking at the diff:
```
@@ -214,10 +227,11 @@ func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest,
```

So the function signature line is:
`func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, args ReconcileArgs) (???`

The return type is on the next line(s), but the diff starts at line 227 which is inside the function body. Actually, the diff hunk header is:
```
@@ -214,10 +227,11 @@ func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest,
```

Wait, that hunk header format is weird. Let me re-read the diff:

```
@@ -214,10 +227,11 @@ func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest,
```

Hmm, the `@@` line might be malformed in my parsing, but looking at the actual diff content:
```go
 	// Emit the per-run scorecard (Epic 3.3) via the same shared bridge the CLI
 	// reconcile uses, so MCP-driven and CLI-driven reconciles produce identical
 	// scorecard records (TD-005 — no entry-point divergence). The MCP path has no
 	// suppression flag, so it always emits. Scorecard diagnostics route to
-	// os.Stderr — the documented default for the MCP path, which has no cobra cmd
+	// the engine's diag sink — os.Stderr by default (the documented MCP default,
 	// to source a writer from (Epic 3.4 AC4). Best-effort.
-	scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: os.Stderr})
+	scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: e.diagWriter()})
```

So the return values of `handleReconcile` are not shown in the diff. I need to infer from the test.

In the new test:
```go
_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
```

If the function returns 2 values, this is a compile error. Let me check if maybe `handleReconcile` returns 3 values. Looking at the MCP SDK, tool handlers often return `(*mcp.CallToolResult, error)`. That's 2 values. So `_, _, err =` would be wrong.

But wait - maybe `handleReconcile` returns `(string, *mcpsdk.CallToolResult, error)` or something? Let me look at the call in `TestReconcileHandler_FailOnGate` which is partially visible at the bottom of the diff:
```go
 func TestReconcileHandler_FailOnGate(t *testing.T) {
 	isolateUserConfig(t) // a successful reconcile now emits a scorecard; keep it out of real ~/.config
 	root := t.TempDir()
```

I can't see the body.

Actually, I notice in `TestReconcileHandler_LatestMergesToHighConfidence`, there's a line `assert.Equal(t, 1, out.TotalFindings, ...)` but `out` is not defined in the visible snippet. Maybe `res` has an `Out` field or something. This doesn't help with the return value count.

Wait, I think I see it now. Let me re-read the test carefully:
```go
+func TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag(t *testing.T) {
+	isolateUserConfig(t)
+	root := t.TempDir()
+	reviewFixture(t, root)
+
+	storeDir, err := scorecard.DefaultDir()
+	require.NoError(t, err)
+	require.NoError(t, os.MkdirAll(filepath.Dir(storeDir), 0o755))
+	require.NoError(t, os.WriteFile(storeDir, []byte("x"), 0o600))
+
+	var buf bytes.Buffer
+	e := &engine{root: root, diag: &buf}
+	_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
+	require.NoError(t, err, "a best-effort scorecard failure must not fail the reconcile")
+	require.Contains(t, buf.String(), "scorecard: write failed",
+		"handleReconcile must wire engine.diag into EmitOpts.Diag")
+}
```

If `handleReconcile` returns only 2 values, then `_, _, err =` won't compile. This is almost certainly a bug. HIGH severity.

But actually, looking at Go syntax, if a function returns 2 values, you cannot write `_, _, err = f()`. The compiler would error with "assignment mismatch: 3 variables but f returns 2 values". So if this diff was actually committed and tests pass (the sprint plan mentions merge commit), then `handleReconcile` MUST return 3 values. Let me reconsider.

Wait, looking back at the existing test `TestReconcileHandler_LatestMergesToHighConfidence`:
```go
res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
```

Maybe `res` is actually two variables? No. Or maybe there's a helper that wraps it? No.

Could it be that the function signature is:
`func (e *engine) handleReconcile(...) (x, y, z)`

And the existing test uses `res, err :=`? No, that would also be a mismatch.

Unless... `res` is declared as `res, _ :=` but shown without the blank? No.

Actually, maybe the existing test uses `res, err` but `res` is actually a multi-value return using named return values and a bare return? No, that doesn't change the count.

I think the most likely explanation is that the existing test shown in the diff was truncated or I'm misreading it. Let me look again:

```go
 func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
 	isolateUserConfig(t)
 	root := t.TempDir()
 	reviewFixture(t, root)
 
 	e := &engine{root: root, completer: fakeCompleter{}}
 	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
 	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
 }
```

Wait! `res` is assigned, but then `out.TotalFindings` is checked. Maybe `res` is not used, and `out` is something else declared earlier in the test (not shown in the diff snippet because the diff only shows parts of the file). The diff shows:
```
@@ -209,6 +211,33 @@ func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
```

So there are lines before line 211 in the hunk that are unchanged. The test body might declare `out` earlier. This doesn't tell us about return values.

Actually, I need to be careful. If the code was merged and the commit is 9626b718..., then it compiles and presumably passes. So `_, _, err =` must match the actual signature. The existing test `res, err :=` might be from a function that returns 2 values, but wait - if the function returns 3 values, then `res, err :=` would fail to compile. So either:
1. Both assignments are valid because the function returns 2 values and the new test is wrong, OR
2. Both assignments are valid because the function returns 3 values and the old test is shown out of context (maybe it's `res, _, err :=` but the blank was removed by me misreading).

Looking very carefully: `res, err := e.handleReconcile(...)` - in Go, if a function returns 3 values, you cannot assign to 2 variables. Period.

So either:
- The old test actually has `_, res, err :=` or `res, _, err :=` and the diff just happens to not show that line correctly? But I can see the exact line: `res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})`

Wait, could `handleReconcile` be a method that returns 2 values, and the new test has a bug that was fixed before merge? But the user is asking me to review the diff. The diff shows `_, _, err =`. If the diff is what's being reviewed, and if it compiles, then the function returns 3 values.

Let me reconsider the existing tests. Maybe `handleReconcile` is not the standard handler but a wrapper. Or maybe it returns `(result, something, error)`.

Given the sprint plan says the merge commit is 9626b718..., and the diff is presented for review, I should assume the code is as shown. If `_, _, err =` compiles, then the function returns 3 values. The old test's `res, err :=` would then also need to compile... unless the old test is in a different context.

Actually, wait. I just realized I might have missed something. In the diff for `internal/mcp/handlers_test.go`, the visible old test code says:
```go
 	e := &engine{root: root, completer: fakeCompleter{}}
 	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
 	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
```

But notice: the diff starts at line 209 with `func TestReconcileHandler_LatestMergesToHighConfidence`. Lines 214-216 are shown. Then it jumps to line 227? No, the hunk is:
```
@@ -209,6 +211,33 @@ func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
```

This is a diff hunk that replaces 6 lines starting at line 211 with 33 lines? No, the standard diff format is:
```
@@ -start,count +start,count @@
```

So `@@ -209,6 +211,33 @@` means:
- Old file: starting at line 209, 6 lines of context
- New file: starting at line 211, 33 lines of context

Wait, no. Unified diff hunk headers can have two pairs of numbers. The format is `@@ -L,S +L,S @@` where L is starting line and S is number of lines. But in git diff, it can also have the third `@@` with the function name.

So `-209,6` means in the old file, lines 209-214 are shown. `+211,33` means in the new file, lines 211-243 are shown.

Looking at the old lines 209-214:
```
 func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
 	isolateUserConfig(t)
 	root := t.TempDir()
 	reviewFixture(t, root)
 
 	e := &engine{root: root, completer: fakeCompleter{}}
```

Then in the new file, after `e := ...` there are 33 lines including the new test and then the old test continues? No, the new lines are inserted between existing code.

Actually, looking at the diff structure:
```
@@ -209,6 +211,33 @@ func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
 	isolateUserConfig(t)
 	root := t.TempDir()
 	reviewFixture(t, root)
 
 	e := &engine{root: root, completer: fakeCompleter{}}
+
+// TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag locks Epic 3.4
+...
+}
+
 	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
 	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
 }
```

Wait, the diff shows blank line then `res, err :=`. Let me look at the actual text again:
```
@@ -209,6 +211,33 @@ func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
 	isolateUserConfig(t)
 	root := t.TempDir()
 	reviewFixture(t, root)
 
 	e := &engine{root: root, completer: fakeCompleter{}}
+
+// TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag locks Epic 3.4
+... (33 lines of new test) ...
+}
+
 	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
 	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
 }
```

Oh! I see. The new test is inserted BEFORE `TestReconcileHandler_LatestMergesToHighConfidence`. The diff shows the context lines of the old test, and the new test is inserted between the `e := ...` line and the `res, err :=` line? No, that doesn't make sense - a test function can't be inserted in the middle of another test function.

Wait, the diff shows:
```
@@ -209,6 +211,33 @@ func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
 	isolateUserConfig(t)
 	root := t.TempDir()
 	reviewFixture(t, root)
 
 	e := &engine{root: root, completer: fakeCompleter{}}
+
+// TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag locks Epic 3.4
```

Then 33 lines later:
```
+}
+
 	res, err := e.handleReconcile(context.Background(), req, ReconcileArgs{})
```

Ah! The `+}` closes the new test function. Then there's a blank line (the `+` line with nothing), then the old `res, err :=` line.

Wait, but the context is `@@ -209,6` which means 6 lines from the old file are shown as context. Those 6 lines are:
1. `func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {`
2. `isolateUserConfig(t)`
3. `root := t.TempDir()`
4. `reviewFixture(t, root)`
5. (blank)
6. `e := &engine{root: root, comple