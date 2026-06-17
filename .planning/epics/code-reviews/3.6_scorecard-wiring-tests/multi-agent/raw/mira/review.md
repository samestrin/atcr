<think>
Let me analyze the diff carefully against the sprint plan scope.

The sprint plan covers:
1. AC3 (CLI) - tests for `leaderboard`, `scorecard`, `reconcile` commands routing diagnostics to `cmd.ErrOrStderr()`
2. AC4 (MCP) - test for `handleReconcile` passing non-default `EmitOpts.Diag` via the new `engine.diag` field

Files in scope:
- `cmd/atcr/scorecard_wiring_test.go` (new)
- `internal/mcp/handlers.go` (modified - the ~3 line production change)
- `internal/mcp/handlers_test.go` (modified - new test)

Let me review the changes carefully:

### `cmd/atcr/scorecard_wiring_test.go`

1. **`execCmdSplit` helper**: Uses `root.SetErr(&errBuf)` and `root.SetOut(&outBuf)`. This sets them on the root command. However, cobra subcommands inherit the output writer from the root by default, but this depends on cobra version. In cobra v1.7+, `cmd.ErrOrStderr()` resolves to `cmd.Err()` (or root's `Err()` if unset). When `SetErr` is called on root, subcommands that don't have their own `Err` set will inherit it. This should work.

2. **`seedMalformedStore`**: Uses `runID[:7]` to derive the month-file stem. This assumes the runID format is "YYYY-MM-DDTHH:MM:SSZ-suffix" so the first 7 chars are "YYYY-MM". This mirrors the `monthFromRunID` comment. However, there's no validation that runID has at least 7 characters. If `runID` is shorter than 7, this will panic with an index out of range error. But in the test cases, runID is always long enough, so this is acceptable for tests.

3. **`runID[:7]`**: This assumes the `monthFromRunID` function in production uses the same slicing. If production uses `runID[:len("YYYY-MM")]` = `runID[:7]`, this matches. But what if production uses a different slicing approach? The test would be coupled to the exact production implementation. This is a maintainability concern but within the scope of a test.

4. **`TestLeaderboardCmd_ReadDiagnosticRoutesToErrOrStderr`**: Seeds a malformed store, runs the command, checks stderr contains the diagnostic. Looks correct.

5. **`TestScorecardCmd_ReadDiagnosticRoutesToErrOrStderr`**: Similar. Looks correct.

6. **`TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr`**: 
   - Creates a fixture review
   - Makes the scorecard store path a regular file (so MkdirAll fails)
   - Runs `reconcile`
   - Checks exit code 0 and stderr contains "scorecard: write failed"
   - One concern: this checks the FULL stderr string for the exact phrase. The cobra command also prints other things to stderr. The test uses `require.Contains` which is fine.

### `internal/mcp/handlers.go`

1. Added `diag io.Writer` field to `engine` struct
2. Added `diagWriter()` method that resolves nil to `os.Stderr`
3. Changed `EmitOpts{Diag: os.Stderr}` to `EmitOpts{Diag: e.diagWriter()}`

This is clean and minimal. The `diagWriter()` method is only called once (in `handleReconcile`). It could be inlined, but keeping it as a method makes it testable and clear. This is a design choice, not necessarily a problem.

### `internal/mcp/handlers_test.go`

1. **`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`**: 
   - Uses `isolateUserConfig(t)` and `t.TempDir()` for root
   - Creates a scorecard store path that is a regular file
   - Constructs engine with `diag: &buf`
   - Calls `handleReconcile` directly
   - Asserts the buffer contains "scorecard: write failed"
   
   One concern: `handleReconcile` is called with `nil` as the `CallToolRequest` and `ReconcileArgs{}` as args. Let me check if `handleReconcile` can handle `nil` CallToolRequest... The diff doesn't show the full `handleReconcile` function, but if the production code accesses the request object, this would panic. However, the existing test `TestReconcileHandler_FailOnGate` follows the same pattern (uses `nil` and `ReconcileArgs{}`), so this is an established pattern.

   Another concern: The test passes `nil` as the CallToolRequest. Let me check if `handleReconcile` dereferences it. In the diff, we can see that `handleReconcile` is at line 220 in the original and now at line 227. The test calls `e.handleReconcile(context.Background(), nil, ReconcileArgs{})`. If the production code doesn't dereference the request object when it's nil, this is fine. But if it does (e.g., for logging), this would panic.

   Actually, looking at the existing test patterns (`TestReconcileHandler_FailOnGate`), this seems to be the established approach. So it's fine.

2. **Engine construction**: `&engine{root: root, diag: &buf}` - minimal construction. Is this sufficient? The `handleReconcile` function likely needs `root` to resolve the review path. Let me check... The test also calls `reviewFixture(t, root)` which presumably sets up the review directory under `root`. So `e.root` is the base for the review path resolution. This seems correct.

3. **Error from `e.handleReconcile`**: The test uses `require.NoError(t, err, ...)`. The comment says "a best-effort scorecard failure must not fail the reconcile." The test correctly asserts that the scorecard write failure doesn't propagate as a reconcile error.

Let me look for potential issues:

1. **`runID[:7]` potential panic**: If `runID` is < 7 chars, this panics. In test usage, runID is always long, so this is a low-severity maintainability concern. The helper doesn't validate the runID length.

2. **`storeRecord` and `reviewerRec` are used but not imported/shown**: These are presumably from the same `main` package test file (in `cmd/atcr`). They're existing helpers, so this is fine.

3. **`fixtureReview` is used in the reconcile test but not shown**: Again, presumably an existing helper.

4. **`execCmdSplit` uses `root.SetOut` and `root.SetErr`**: In cobra, subcommands that don't call `SetOut`/`SetErr` will use the parent's writers via `OutOrStderr()`/`ErrOrStderr()`. This should work for routing diagnostics to `cmd.ErrOrStderr()`. But wait - does `cmd.ErrOrStderr()` on a subcommand return the subcommand's `Err()` or fall back to the parent's? Let me think... In cobra, `cmd.ErrOrStderr()` returns `cmd.Err()` if set, otherwise it falls back to `cmd.parent.ErrOrStderr()`. And `cmd.parent.ErrOrStderr()` returns `parent.Err()` or `os.Stderr`. So if we set `root.SetErr(&errBuf)`, all subcommands without their own `SetErr` will use `&errBuf`. This is correct.

5. **The `Isolated store` and `isolate(t)`**: The `isolate(t)` function presumably sets up the scorecard store in a temp directory. The `seedMalformedStore` then writes to `scorecard.DefaultDir()` which should resolve to the isolated store.

6. **Race condition / cleanup in tests**: Tests use `t.TempDir()` (for MCP test) and `isolate(t)` (for CLI tests). These should handle cleanup automatically.

7. **The `engine` struct now has a `diag` field, but the constructor function (if any) doesn't set it**: Let me check if there's a constructor for `engine`... The diff shows the struct and the `diagWriter()` method, but no constructor. The test uses `&engine{root: root, diag: &buf}` directly. If production code creates engines via a constructor that doesn't initialize `diag`, the field will be nil, and `diagWriter()` will return `os.Stderr` - which is the documented default. This is correct behavior.

8. **`_, _, err = e.handleReconcile(...)`**: The function returns 3 values. Let me check what they are. The diff shows `e.handleReconcile(context.Background(), nil, ReconcileArgs{})` returning something assigned to `_, _, err`. The return signature is likely `(out *ReconcileResult, _ something, err error)`. This matches the pattern of other tests in the file.

9. **Potential issue: `runID[:7]` coupling**: The test helper slices `runID[:7]` to get the month file name. This assumes a specific runID format and a specific `monthFromRunID` implementation. If the production code changes to use `runID[:10]` or a regex, this test breaks even if the wiring is correct. This is a maintainability concern but is a common pattern in Go test code.

10. **Potential issue: `filepath.Dir(dir)` for MkdirAll in `TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr`**: The test does `os.MkdirAll(filepath.Dir(dir), 0o755)` to create the parent of the scorecard store dir, then writes a file at `dir`. When `Append` tries `MkdirAll(dir)`, it would fail because there's a file at that path. This is the intended behavior to force a write failure. But wait - what if the reconcile command resolves the store path differently than `scorecard.DefaultDir()`? If the production code uses a different resolution mechanism, the test might not actually force a write failure.

   Actually, looking at the MCP test (`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`), it does the same thing: `os.MkdirAll(filepath.Dir(storeDir), 0o755)` then `os.WriteFile(storeDir, ...)`. This is the same pattern, so it's the established approach.

11. **Cobra's `OutOrStderr` and `ErrOrStderr` behavior**: Let me verify - when `root.SetErr(&errBuf)` is called, and a subcommand calls `cmd.ErrOrStderr()`, does it resolve to `&errBuf`? In cobra v1.7+, the answer is yes. But what if a subcommand has already called `SetErr` (e.g., in its `RunE` function before the diagnostic is written)? In the production code, the scorecard calls happen in `runLeaderboard`, `runScorecard`, `runReconcile` - these presumably access `cmd.ErrOrStderr()` which returns the writer. The test sets the writer on root, and as long as the subcommands don't override it, the diagnostics will go to the buffer.

   But here's a subtlety: the test does `root.SetErr(&errBuf)`. The scorecard code receives a writer from `cmd.ErrOrStderr()`. If the cobra version correctly inherits the parent's writer when the child hasn't set its own, this works. If not, the test would fail.

   This is a real concern. Let me think about it more carefully... In cobra, `cmd.ErrOrStderr()` is defined as:
   ```go
   func (c *Command) ErrOrStderr() io.Writer {
       return c.getErr(os.Stderr)
   }
   ```
   And `getErr` returns `c.Err` if set, otherwise walks up to parent. So setting `root.SetErr(&errBuf)` and then calling `subCmd.ErrOrStderr()` should return `&errBuf` (assuming subCmd doesn't have its own Err set).

   This should work. But it's worth verifying in the test that the subcommands don't override Err. The tests seem to assume this is the case.

12. **Test isolation**: `isolate(t)` presumably sets environment variables to redirect the scorecard store to a temp directory. The `seedMalformedStore` then writes to the isolated store. This is correct test isolation.

Let me now compile the actual findings:

### Real issues:

1. **`runID[:7]` in `seedMalformedStore` could panic**: If runID is < 7 chars, it panics. This is a LOW maintainability concern. The helper has no validation.

2. **`seedMalformedStore` ignores `f.WriteString` error**: The code does `_, _ = f.WriteString(...)`. This is intentional (best-effort in test setup), but if the write fails, the test might pass falsely because the malformed line was never written. This is a MEDIUM correctness concern - the test could pass without actually exercising the malformed-record code path. However, the test is run in a temp directory and the file is just opened, so the write should succeed. This is a low-risk concern.

3. **`seedMalformedStore` doesn't close the file on error path**: If `os.OpenFile` succeeds but `f.WriteString` fails (or `f.Close()` fails), the file is left open. But the error is ignored, so this is consistent with the best-effort pattern. Not really an issue.

4. **`execCmdSplit` doesn't check if the root command is properly initialized**: If `newRootCmd()` requires environment setup (like `isolate(t)`), and the helper is called without it, the test would fail in a confusing way. This is a LOW maintainability concern - the helper should document its prerequisites.

5. **`seedMalformedStore` assumes `storeRecord` uses the same month file as the append**: The function first calls `storeRecord(t, reviewerRec(...))` which presumably writes to the correct month file, then appends to the same file. If `storeRecord` uses a different month file than `runID[:7]`, the malformed line would be in a different file than the valid record. The comment says "mirrors monthFromRunID" which suggests the production code uses `runID[:7]`, so the valid record should be in the same file. This is correct if the production code is consistent.

6. **The `engine.diag` field has no constructor**: The `engine` struct is constructed directly in the test (`&engine{root: root, diag: &buf}`). If there's a production constructor that doesn't initialize `diag`, that's fine because `diagWriter()` handles nil. But if there's a constructor that always sets `diag` to `os.Stderr`, the test's injection would be overridden. The diff doesn't show a constructor, so this is likely fine.

7. **`TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr` doesn't assert stdout**: Unlike the read-path tests, this test only checks `stderr` and doesn't verify that the diagnostic doesn't appear on stdout. This is a minor inconsistency but not a real issue (the reconcile command might write other things to stdout that contain "scorecard" in some form).

8. **`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` passes `nil` as CallToolRequest**: The function signature is `handleReconcile(ctx, req *mcpsdk.CallToolRequest, args ReconcileArgs)`. If the production code dereferences `req` at any point, this will panic. But the existing test `TestReconcileHandler_FailOnGate` uses the same pattern, so it's established. This is fine.

9. **`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` doesn't set `e.log`**: The `engine` struct has a `log *slog.Logger` field. If `handleReconcile` uses `e.log` and it's nil, it would panic. But the test passes `nil` for the request and the existing tests don't set `log` either, so this is the established pattern. The deferred TD item about routing through `e.log` is acknowledged in the sprint plan.

10. **`execCmdSplit` comment says "process-global os.Stderr"**: The comment is slightly misleading. The default for `cmd.ErrOrStderr()` when nothing is set is `os.Stderr`, not a "process-global writer." But this is a comment nit, not a real issue.

Let me focus on the most impactful findings:

### High-priority findings:

1. **`runID[:7]` in `seedMalformedStore` - no length validation**: If a test ever passes a short runID, the helper panics. This is a LOW maintainability issue. The helper should validate or document the expected format.

2. **Ignored error in `f.WriteString`**: The malformed line might not actually be written if the write fails, making the test pass falsely. This is a MEDIUM correctness concern for test reliability. However, in a temp directory with a freshly opened file, this is extremely unlikely. Still, the test could be more robust.

3. **`seedMalformedStore` doesn't verify the record was written to the same month file**: The function calls `storeRecord` then appends to `runID[:7]` month file. If `storeRecord` uses a different file, the test exercises a different code path than intended. The test should verify the file consistency. This is a MEDIUM maintainability/correctness concern.

Actually, let me reconsider. The `storeRecord` function is presumably an existing helper that writes to the correct month file based on the runID. The comment in `seedMalformedStore` says "mirrors monthFromRunID" which is the production function. If `storeRecord` already uses `monthFromRunID(runID)`, then both writes go to the same file. The test assumes this consistency.

4. **The `engine` struct's `diag` field - what about concurrent access?**: The `engine` struct has a `bg sync.WaitGroup` and presumably handles concurrent requests. The `diag` field is set once at construction and never modified, so there's no race condition. This is fine.

5. **Missing test for the `diagWriter()` method directly**: The method is only tested indirectly through `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`. A unit test for `diagWriter()` with nil and non-nil inputs would be more direct. But this is a LOW maintainability concern, not a real bug.

6. **`execCmdSplit` doesn't set `root.SetIn`**: Some cobra commands read from stdin. If a command reads from stdin and there's no input, it might hang. But the scorecard commands (leaderboard, scorecard, reconcile) don't read from stdin, so this is fine.

7. **`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` - the `reviewFixture` helper**: The test calls `reviewFixture(t, root)` which presumably creates a review directory under `root`. The `handleReconcile` function uses `e.root` to resolve the review path. If the fixture isn't at the expected path, the reconcile would fail. This is a test correctness concern, but it's an existing pattern in the test file.

8. **The `isolateUserConfig(t)` helper**: This presumably redirects user config (like `~/.config/atcr`) to a temp directory. This is needed because the scorecard store path might be derived from user config. The test correctly uses it.

Let me now finalize my findings. I'll focus on real, actionable issues within scope:

1. **MEDIUM - `seedMalformedStore` panics on short runID**: `runID[:7]` without length check. This is a maintainability/correctness concern for the test helper. Fix: add a length check or document the expected format.

2. **LOW - `f.WriteString` error ignored**: The malformed line might not be written if the write fails. Fix: use `require.NoError` instead of ignoring the error.

3. **LOW - `seedMalformedStore` assumes `storeRecord` writes to the same month file**: The test could fail to exercise the intended code path if the helpers are inconsistent. Fix: explicitly verify the file path or use a more robust approach.

4. **LOW - `execCmdSplit` doesn't document prerequisites**: The helper assumes `isolate(t)` has been called. Fix: add a comment about prerequisites.

5. **LOW - `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` doesn't test `diagWriter()` directly**: The nil-handling logic in `diagWriter()` is only tested indirectly. Fix: add a unit test for `diagWriter()`.

Actually, let me reconsider. The sprint plan says "AC3/AC4 wiring tests" and the scope is clearly defined. Let me focus on issues that are real bugs or significant maintainability concerns, not nitpicks.

Real findings:

1. **`runID[:7]` panic risk**: If `runID` is ever < 7 chars (e.g., in a future test or refactor), the helper panics. This is a real concern for test reliability. MEDIUM severity.

2. **Ignored write error in `seedMalformedStore`**: `_, _ = f.WriteString(...)` - if the write fails, the test might pass falsely. This is a test correctness concern. MEDIUM severity.

3. **The `engine` struct's `diag` field - is it thread-safe?**: The field is set at construction and read in `handleReconcile`. If `engine` is shared across goroutines (which it is, given the `bg sync.WaitGroup`), the read of `e.diag` in `diagWriter()` is a data race with the construction. But construction happens before any goroutine is spawned, so this is fine in practice. This is a LOW concern.

4. **`TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` - `e.log` is nil**: If `handleReconcile` uses `e.log` for logging, it will panic. The deferred TD item acknowledges this. This is acknowledged in scope, so not a finding.

5. **`execCmdSplit` - the test doesn't verify that `cmd.ErrOrStderr()` actually returns the buffer**: The test assumes cobra's writer inheritance works correctly. If a future cobra version changes this behavior, the test would break in a confusing way. This is a LOW maintainability concern.

6. **The `engine.diag` field is exported as part of the struct, not a constructor parameter**: This means any code that creates an `engine` (if there are multiple construction sites) needs to remember to set `diag` (or leave it nil for the default). The test creates the engine directly, which is fine for tests, but in production, if there's a constructor that doesn't set `diag`, the default behavior (os.Stderr) is used. This is correct by design.

Let me also check: does the `engine` struct have a constructor function? The diff shows the struct and the `diagWriter()` method, but no constructor. The test uses `&engine{root: root, diag: &buf}`. If there's a production constructor (like `newEngine`), it would need to be updated to not override `diag` (or to default it to `os.Stderr`). But the diff doesn't show a constructor, so I'll assume there isn't one or it's been updated.

Actually, let me re-read the diff more carefully for `handlers.go`:

The diff shows:
- Added `io` import
- Added `diag io.Writer` field to `engine` struct
- Added `diagWriter()` method
- Changed `os.Stderr` to `e.diagWriter()` in `handleReconcile`

No constructor is shown. If there is a `newEngine` function that creates engines, it would need to not set `diag` (so it defaults to nil → `os.Stderr`). The diff doesn't show this, which could be a problem if a constructor exists and sets `diag` to something. But since the diff is focused on the minimal change, and the sprint plan says "the ~3-line production change is the only path that makes the MCP AC achievable," I'll assume there's no constructor or it's been handled.

Let me also check: the `engine` struct is defined in `handlers.go` at line ~34. The test creates it directly with `&engine{root: root, diag: &buf}`. This is fine for tests.

7. **`seedMalformedStore` - the malformed line is appended to a file that may or may not exist**: The function calls `os.OpenFile(..., os.O_APPEND|os.O_WRONLY, ...)`. If the file doesn't exist, `OpenFile` with `O_APPEND` and `O_WRONLY` (without `O_CREATE`) will fail. The test assumes `storeRecord` creates the file first. If `storeRecord` doesn't create the month file (e.g., it uses a different path), the `OpenFile` call would fail, and the test would error out. This is correct behavior - the test would fail with a clear error. Not really an issue.

8. **The `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` test uses `nil` for `CallToolRequest`**: This is the same pattern as other tests. If the production code dereferences the request, it would panic. The sprint plan acknowledges this is an established pattern. Not a new issue.

OK, let me compile my final findings. I'll focus on the most impactful real issues:

1. **MEDIUM - `runID[:7]` panic risk in `seedMalformedStore`**: No length validation. If runID is < 7 chars, the helper panics with an index out of range error.

2. **MEDIUM - Ignored error in `f.WriteString` in `seedMalformedStore`**: The malformed line might not be written if the write fails, making the test pass falsely.

3. **LOW - `seedMalformedStore` assumes `storeRecord` uses the same month file**: The test could exercise a different code path if the helpers are inconsistent. The comment says "mirrors monthFromRunID" but doesn't verify.

4. **LOW - `execCmdSplit` doesn't verify cobra writer inheritance**: The test assumes `root.SetErr` propagates to subcommands. If a future refactor changes this, the test breaks confusingly.

5. **LOW - No direct unit test for `diagWriter()` method**: The nil-handling logic is only tested indirectly.

Actually, I want to reconsider #1 and #2. These are in the test helper, not the test itself. The test helper is called with well-known runIDs, so the panic risk is minimal. And the write error in a temp directory is extremely unlikely. These are LOW severity at best.

Let me also consider: is there a race condition in the `engine` struct? The `diag` field is set at construction and read in `handleReconcile`. The `bg sync.WaitGroup` suggests concurrent access. But construction happens before any goroutine is spawned (the engine is created in the main goroutine), so the write to `diag` happens-before any read. This is fine.

What about the cobra command's `SetErr`? In `execCmdSplit`, the test sets `root.SetErr(&errBuf)`. If the cobra command tree is shared across tests (it's not, since `newRootCmd()` creates a new tree each time), there could be a race. But each test creates its own root command, so this is fine.

One more thing: the `execCmdSplit` function uses `context.Background()`. If the command spawns goroutines that outlive the test, they would continue running. But the test waits for `root.ExecuteContext()` to return, which should wait for all goroutines. This is fine.

Let me also check: does the `seedMalformedStore` function close the file properly? It does `require.NoError(t, f.Close())` at the end. If `f.WriteString` fails, the file is not closed. But the write error is ignored, so the test continues. The file would be closed when the test process exits (or when the temp directory is cleaned up). This is a minor concern but not a real issue.

OK, I think I've been thorough enough. Let me compile my final findings.

Actually, wait. Let me re-read the `seedMalformedStore` function more carefully:

```go
func seedMalformedStore(t *testing.T, runID, reviewer string) {
    t.Helper()
    storeRecord(t, reviewerRec(runID, reviewer, "claude-sonnet-4-6", 12, 7))
    dir, err := scorecard.DefaultDir()
    require.NoError(t, err)
    month := runID[:7] // YYYY-MM month-file stem (mirrors monthFromRunID)
    f, err := os.OpenFile(filepath.Join(dir, month+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
    require.NoError(t, err)
    _, _ = f.WriteString("{not valid json\n")
    require.NoError(t, f.Close())
}
```

Issues:
1. `runID[:7]` - if runID < 7 chars, panic. No length check.
2. `_, _ = f.WriteString(...)` - error ignored. If write fails, test might pass falsely.
3. No `defer f.Close()` - if any subsequent line panics, the file is left open.
4. `os.OpenFile` with `O_APPEND|O_WRONLY` without `O_CREATE` - if the file doesn't exist (e.g., `storeRecord` didn't create it), the test fails with a clear error. This is fine.

For the `TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr` test:

```go
func TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr(t *testing.T) {
    isolate(t)
    fixtureReview(t, "2026-06-10_feat", map[string]string{...})
    dir, err := scorecard.DefaultDir()
    require.NoError(t, err)
    require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o755))
    require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))
    
    code, _, stderr := execCmdSplit(t, "reconcile", "2026-06-10_feat")
    require.Equal(t, 0, code, ...)
    require.Contains(t, stderr, "scorecard: write failed", ...)
}
```

Issues:
1. Doesn't check stdout (unlike the read-path tests). Minor inconsistency.
2. Assumes `fixtureReview` creates a valid review that reconcile can process. If the fixture is malformed, the reconcile would fail for a different reason. This is a test correctness concern but is an existing pattern.

For `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag`:

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
    require.NoError(t, err, ...)
    require.Contains(t, buf.String(), "scorecard: write failed", ...)
}
```

Issues:
1. `e.log` is nil - if `handleReconcile` uses `e.log`, it panics. The deferred TD item acknowledges this.
2. `nil` for `CallToolRequest` - same pattern as other tests. Fine.
3. The test doesn't verify that the diagnostic doesn't go to `os.Stderr`. It only checks the buffer. If the wiring is broken (diags go to both the buffer AND `os.Stderr`), the test would still pass. But this is the established pattern for this kind of test.

Wait, actually, this is an important point. The test asserts that the diagnostic is in the buffer, but doesn't assert that it's NOT in `os.Stderr`. If the production code writes to both the buffer and `os.Stderr`, the test would pass even though the wiring is broken. But the sprint plan says the test should catch a "deliberate regression (swapping the wired writer back to a default)." A swap to `os.Stderr` would mean the diagnostic goes to `os.Stderr` instead of the buffer, and the test would fail. So the test is correct for the stated AC.

But what if the production code writes to both? The test wouldn't catch that. This is a minor concern, but the test is designed to catch a specific regression (swapping the writer), not a more subtle bug (writing to both). This is acceptable for the stated AC.

OK, let me finalize. I think the most impactful findings are:

1. **LOW - `runID[:7]` panic risk in `seedMalformedStore`**: No length validation. If runID is < 7 chars, the helper panics.

2. **LOW - Ignored error in `f.WriteString` in `seedMalformedStore`**: The malformed line might not be written if the write fails, making the test pass falsely.

3. **LOW - `seedMalformedStore` doesn't close the file on error path**: If `f.WriteString` fails, the file is left open (not closed via `require.NoError`).

4. **LOW - `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` doesn't assert the diagnostic is NOT in `os.Stderr`**: The test only checks the buffer. A wiring bug that writes to both the buffer and `os.Stderr` would not be caught.

5. **LOW - No direct unit test for `diagWriter()` method**: The nil-handling logic is only tested indirectly.

Actually, #4 is a design choice, not a bug. The test is designed to catch a specific regression (swapping the writer), and it does. A more comprehensive test would check both, but that's beyond the stated AC.

Let me also consider: is there a risk that the `engine` struct's `diag` field is not set in production (i.e., the constructor doesn't set it)? If the field is nil, `diagWriter()` returns `os.Stderr`, which is the documented default. This is correct behavior. The test sets the field directly, which is fine for testing.

But what if there's a production code path that creates an engine without going through the test? The field would be nil, and the default would be used. This is correct.

OK, I think I've been thorough enough. Let me write up the findings.

One more thing: the `execCmdSplit` function comment says "Unlike `execCmdCapture` (which merges the two)". This implies there's an existing `execCmdCapture` helper. If that helper exists, the new helper is a good addition. The comment is helpful for future maintainers.

Also, the `seedMalformedStore` comment says "isolate(t) must run first so DefaultDir() resolves into the per-test temp store." This is good documentation.

Let me also check: does the `runID` format in the tests match the production format? The tests use "2026-06-14T10:00:00Z-corrupt" which is a valid ISO 8601 timestamp with a suffix. The production `monthFromRunID` presumably handles this format. The test assumes the first 7 chars are "YYYY-MM", which is correct for this format.

Final findings:

1. **MEDIUM - `seedMalformedStore` panics on runID < 7 chars**: `runID[:7]` without bounds check. Fix: add `require.GreaterOrEqual(t, len(runID), 7, "runID must be at least 7 chars (YYYY-MM)")`.

2. **LOW - `seedMalformedStore` ignores `f.WriteString` error**: Fix: use `_, err := f.WriteString(...)` and `require.NoError(t, err)`.

3. **LOW - `seedMalformedStore` doesn't close file on error path**: Fix: use `defer require.NoError(t, f.Close())` or restructure.

4. **LOW - `TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag` relies on `e.log` being nil-safe**: The deferred TD item acknowledges this, but the test would panic if `handleReconcile` uses `e.log`. This is acknowledged in scope, so not a finding.

Actually, #4 is acknowledged in the sprint plan, so it's not a new finding. Let me remove it.

5. **LOW - No direct unit test for `diagWriter()` nil-handling**: The method's default behavior (nil → os.Stderr) is only tested indirectly. Fix: add `TestEngineDiagWriter_DefaultsToStderr`.

Hmm, but the sprint plan says the test should assert "a non-default writer" is passed. A test for `diagWriter()` with nil input would just verify it returns `os.Stderr`, which is the default. This is a unit test concern, not a wiring test concern. The wiring test already covers the injection path. I'll include this as a LOW finding.

6. **LOW - `seedMalformedStore` coupling to `storeRecord`'s month file**: The helper assumes `storeRecord` writes to `runID[:7]+".jsonl"`. If `storeRecord` uses a different path, the malformed line is in a different file than the valid record. Fix: verify the file path or use a more explicit approach.

Actually, this is a real concern. If `storeRecord` doesn't create the file at `runID[:7]+".jsonl"`, then `os.OpenFile` with `O_APPEND|O_WRONLY` (without `O_CREATE`) would fail, and the test would error out. So the test implicitly verifies the file exists. But if `storeRecord` creates a different file (e.g., with a different month calculation), the malformed
</think>
