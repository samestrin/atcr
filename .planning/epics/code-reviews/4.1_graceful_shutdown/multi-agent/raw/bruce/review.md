<think>
Let me analyze this diff carefully for the sprint scope of epic 4.1 (Graceful Shutdown and Signal Handling).

Files in scope per sprint plan:
- `cmd/atcr/main.go` — signal handling
- `cmd/atcr/review.go` — partial result preservation
- `cmd/atcr/main_test.go` — unit tests
- `cmd/atcr/review_test.go` — unit tests
- `internal/fanout/engine.go` — already cooperates (no changes)
- `internal/fanout/review.go` — set-on-cancel in `ExecuteReview`
- `internal/fanout/status.go` — `RunInterrupted` derive
- `internal/fanout/interrupt_test.go` — new test
- `internal/fanout/status_test.go` — tests
- `internal/payload/manifest.go` — `Interrupted` field

Let me trace through the code carefully:

1. **`cmd/atcr/main.go`**:
   - `handleSignals` starts a goroutine that waits for a signal
   - On signal, prints message, cancels context, waits for grace period, force-exits with code 1
   - Issue: If the process exits cleanly (no signal) but the goroutine is still running, it will leak forever. The goroutine is blocked on `<-sigCh` but the signal channel is never closed.
   - Wait, actually since `signal.Notify` is called with `sigCh`, even after `main()` returns, the goroutine will be blocked. This is a goroutine leak on normal exit.
   - Actually, the goroutine leaks on every normal invocation. The process exits so it doesn't matter, but tests using this code may leak.
   - Wait, actually on process exit, the process is done. But the test code does call `handleSignals` and doesn't wait for the goroutine to finish. Actually, the test sends a signal, so the goroutine fires and calls forceExit (mocked). The goroutine then returns.
   - But in normal `main()` execution, if no signal comes, the goroutine blocks forever on `<-sigCh`. This isn't a problem in production since the process exits. But it could be a problem in tests or if main is called from another program.
   - More importantly: `defer cancel()` is called when main returns, but the goroutine is still blocked. signal.Notify still has the channel registered. The program will exit so it doesn't matter.

2. **`cmd/atcr/main.go` - handleSignals**:
   - The goroutine handles only the FIRST signal. If a user presses Ctrl-C twice, the second signal is buffered (channel size 1) and will be picked up by... no wait, the goroutine already returned after forceExit. So the second signal is dropped.
   - But the design says "Only the first signal is handled; the grace timer is the hard backstop against a hang." So this is by design.

3. **`cmd/atcr/main.go` - handleSignals goroutine**:
   - After `cancel()` is called, there's `<-time.After(gracefulShutdownTimeout)`. This is hardcoded as 10s in production, 15ms in test.
   - Issue: There's no way to abort the grace timer if the cooperative shutdown completes early. The process still waits 10 seconds before exiting successfully. Wait, actually no - the process doesn't exit after `forceExit` unless `forceExit` is called. If cooperative shutdown completes (e.g., root.ExecuteContext returns), main() returns normally, but the goroutine is still waiting for the grace timer.
   - This is a goroutine leak in the normal (non-interrupt) case, and also a problem when cooperative shutdown completes within grace period but main() returns before the timer fires - the goroutine will keep running until the timer fires, then call forceExit(1). 
   - Wait! This is actually a critical bug. Let me re-read...
   
   ```go
   func handleSignals(sigCh <-chan os.Signal, cancel context.CancelFunc, out io.Writer) {
       go func() {
           <-sigCh
           _, _ = fmt.Fprintln(out, "\nReceived interrupt, shutting down gracefully...")
           cancel()
           <-time.After(gracefulShutdownTimeout)
           _, _ = fmt.Fprintln(out, "Graceful shutdown timed out, forcing exit")
           forceExit(1)
       }()
   }
   ```
   
   So if SIGINT is received:
   1. Print "shutting down gracefully..."
   2. Call cancel()
   3. Wait 10 seconds
   4. Print "forcing exit" 
   5. Call forceExit(1)
   
   This means even if the cooperative shutdown completes immediately, the process won't exit for 10 seconds! The cancel() makes the context done, which should make ExecuteContext return, which makes main() return. But the goroutine will still wait 10 seconds before calling forceExit(1).
   
   Wait, but main() will return, and the process will exit. The goroutine is killed by the process exit. So in production, it doesn't matter.
   
   But there's a different issue: when main() returns normally, the deferred cancel() runs. But the goroutine is still blocked. When the process exits, the goroutine is killed.
   
   In tests, however, the goroutine is not in the test goroutine - it's a background goroutine. The test sends a signal and waits for forceExit to be called. The test then completes. But the goroutine has already returned. So this should be fine.
   
   Actually, let me re-read: in the test, the timeout is 15ms. After 15ms, forceExit is called. Test passes.

4. **`cmd/atcr/review.go`**:
   - The interrupt check is `errors.Is(ctx.Err(), context.Canceled)`. This is on the parent context.
   - Issue: If the context is cancelled due to a deadline (review timeout), this is DeadlineExceeded, not Canceled. The code correctly distinguishes.
   - Wait, but if the review timeout is reached, the parent context is NOT cancelled - only the child runCtx is. So `ctx.Err()` returns nil for the parent. The code is correct.
   
5. **`cmd/atcr/review.go` - interruptMessage**:
   - The function takes `result` and `prep`. If result is nil, uses prep.Dir. If result is not nil, uses result.Summary.Succeeded/Total and result.Dir.
   - Issue: `done, total, dir` are set from result if not nil, but `prep.ID` is always used for the status command. This seems intentional.
   - Wait, but what if `prep` is nil? Looking at the call sites: `interruptMessage(result, prep)` - prep is always set by the time `ExecuteReview` is called. But what if ExecuteReview returns `(nil, nil)` and the caller hasn't nil-guarded prep? Let me check...
   - In the call site: `result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)` - prep is always non-nil at this point because it's set in `runReview`. And `interruptedBeforeFanout` doesn't take a prep parameter. OK.

6. **`cmd/atcr/review.go` - runReview**:
   - The flow is: Resolve → PrepareReview → ExecuteReview
   - For Resolve and PrepareReview, if cancelled, return `interruptedBeforeFanout(cmd)`.
   - For ExecuteReview, check `ctx.Err() == context.Canceled` after.
   - Issue: What if `ExecuteReview` returns `(nil, err)` and err is non-nil but ctx is also cancelled? The new code returns early with the interrupt message and `exitFailure`. But if ExecuteReview failed for a different reason, the original error is lost.
   - Wait, let me re-read: 
     ```go
     result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)
     
     if errors.Is(ctx.Err(), context.Canceled) {
         _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
         return &codedError{code: exitFailure, err: errors.New("review interrupted")}
     }
     
     if result != nil {
         ...
     }
     ```
   - So if `err` is non-nil AND ctx is cancelled, the new code returns the interrupt message, ignoring the original error. The original `err` is discarded.
   - This might be intentional, but it loses information. The user doesn't see what the original error was.
   - However, the original error is likely "context canceled" or similar, so the interrupt message is more informative.

7. **`cmd/atcr/review.go` - exit code on interrupt**:
   - The interrupted case returns exit code 1. The non-interrupt case can return exit code 0 or 2. This matches the AC7 requirement.
   - The interrupt message is printed even when `result == nil` (using prep fallback). Good.

8. **`internal/fanout/review.go` - ExecuteReview**:
   - Sets `interrupted = errors.Is(ctx.Err(), context.Canceled)` on the parent context.
   - Then sets `m.Interrupted = interrupted`.
   - Issue: What if `p.manifest == nil`? The code has a nil guard: `if p.manifest != nil`. Good.
   - Issue: The interrupt check happens AFTER `results := NewEngine(...).Run(runCtx, p.Slots)`. This is fine - we want to know if the parent context was cancelled after the engine ran.

9. **`internal/fanout/status.go` - ReadReviewStatus**:
   - After determining `RunCompleted` or `RunFailed`, checks `m.Interrupted` and overrides with `RunInterrupted`.
   - Issue: What about the `RunStale` case? Let me look... The new code is inside the `else` branch:
   ```go
   if m.CompletedAt.IsZero() {
       st.Status = RunStale
   } else {
       st.Status = RunFailed  // or RunCompleted
   }
   if m.Interrupted {
       st.Status = RunInterrupted
   }
   ```
   - So interrupted overrides both completed and failed. But it doesn't override stale. Is that intentional? An interrupted review should have CompletedAt set (from the manifest update in ExecuteReview), so it won't be stale. But what if a review was interrupted before CompletedAt was set? Let me trace...
   - In `ExecuteReview`:
     ```go
     p.manifest.CompletedAt = time.Now().UTC()
     p.manifest.Interrupted = interrupted
     _ = WriteManifest(p.Dir, p.manifest)
     ```
   - This is in the `if err != nil` branch of WritePool. If WritePool succeeds, the manifest is updated in the next block. If WritePool fails, the manifest is updated here.
   - So in normal flow, CompletedAt is set when interrupted is set. Good.
   - But what if the process is force-exited before WriteManifest completes? Then CompletedAt might not be set. The status would be reported as stale. But the manifest is not yet marked interrupted either. So the override wouldn't apply.
   - This is an edge case that's hard to test. Not a major issue.

10. **`internal/payload/manifest.go`**:
    - Adds `Interrupted bool` with `omitempty`. Good - backwards compatible.

11. **Test: `TestHandleSignals_CancelsContextOnSignal`**:
    - Uses 15ms timeout. Sends SIGINT. Verifies context is cancelled. Uses `require.Eventually` to wait for forceExit.
    - Issue: The test creates `defer cancel()` but the cancel is never actually called in the test. The context is cancelled by the signal handler.
    - Wait, that's fine. The test only needs to verify the handler works.
    - But the test has a `defer cancel()` for cleanup. This is good.

12. **Test: `TestHandleSignals_ForceExitsAfterGracePeriod`**:
    - Uses 10ms timeout. Sends SIGTERM. Verifies forceExit is called with code 1.
    - Issue: The test creates `_, cancel := context.WithCancel(...)` and `defer cancel()`. The cancel is never called by the test. The signal handler calls cancel after receiving the signal. But the cancel is for the test's context, not used by anything.
    - Actually, the cancel is passed to `handleSignals`, so it IS called by the handler. But it's a no-op because nothing depends on the test's context.
    - Wait, but the test is testing that the handler calls forceExit after the grace period. The test's context is irrelevant. The cancel is called by the handler but does nothing. OK.

13. **Test: `TestExecuteReview_InterruptPreservesPartialAndMarksInterrupted`**:
    - Drives 4 agents with MaxParallel=1.
    - The completer succeeds for 2 agents, then cancels on the 2nd.
    - Wait, let me re-read: `cancelAt: 2`. So on the 2nd call to Complete, the completer cancels. The 1st call succeeds normally. The 2nd call cancels, but also returns success.
    - After cancel, the engine should not start new agents (it checks ctx.Err() before each invocation). The remaining 2 agents (3rd, 4th) should short-circuit.
    - The test expects: Succeeded=2, Failed=2.
    - But wait - the completer increments `count` BEFORE checking `n > c.cancelAt`. So:
      - Call 1: count=1, 1 <= 2, return success
      - Call 2: count=2, 2 == 2, cancel(), return success
      - Call 3: would be count=3, 3 > 2, return ctx.Err()
      - Call 4: would be count=4, 4 > 2, return ctx.Err()
    - So 2 succeed, 2 fail (with context error).
    - Test assertion: Succeeded=2, Failed=2. Correct.
    
14. **Race condition in `cancelAfterCompleter`**:
    - With MaxParallel=1, calls are serialized. No race. Good.
    - But the test is correct only if MaxParallel=1 is respected. The test passes `MaxParallel: 1` in the prep.

15. **Test: `TestReadReviewStatus_InterruptedWithPartialSuccess`**:
    - Writes a manifest with `interrupted: true, partial: true`. summary shows 1 succeeded, 1 failed.
    - Expects status to be `RunInterrupted`.
    - Good test.

16. **Test: `TestReadReviewStatus_InterruptedOverridesFailed`**:
    - Writes manifest with `interrupted: true, partial: false`. summary shows 0 succeeded, 1 failed.
    - Expects status to be `RunInterrupted`, not `RunFailed`.
    - Good test.

17. **Test: `TestReadReviewStatus_NotInterruptedByDefault`**:
    - Writes manifest WITHOUT interrupted field. summary shows 1 succeeded, 0 failed.
    - Expects status to be `RunCompleted`.
    - Good test.

Now let me look for actual bugs:

**Bug 1: `cmd/atcr/main.go` - Goroutine leak on non-interrupt exit**
- In production, this is fine because the process exits. But it's a real leak. The signal channel is buffered (size 1), so signal.Notify will write to it. The goroutine blocks on `<-sigCh`. On normal exit, the goroutine is killed. But signal.Notify is never unregistered.
- This is a known pattern issue. In tests, it could be a problem. But the tests in the diff all send a signal, so the goroutine fires and returns.

**Bug 2: `cmd/atcr/main.go` - Force-exit fires even on successful shutdown**
- After `cancel()`, the goroutine waits 10 seconds. If main() returns before 10s (e.g., cooperative shutdown completes), the goroutine is killed by process exit.
- But if main() returns AFTER 10s (e.g., cooperative shutdown hangs for exactly 10s), the goroutine calls forceExit(1), and then... main() also returns. Process exits with code 1 (from forceExit). OK, this is the intended behavior.
- If main() returns BEFORE 10s, process exits normally. Goroutine is killed. OK.
- Actually, there's a subtle issue: `defer cancel()` runs when main() returns. But the signal handler also calls cancel(). The signal handler's cancel is the same function. Calling it twice is fine (no-op the second time).

**Bug 3: `cmd/atcr/review.go` - Lost error on interrupt**
- If ExecuteReview returns (result, err) where err is non-nil AND ctx is cancelled, the new code ignores err and returns the interrupt message. The user doesn't see the original error.
- This might be intentional (the interrupt is more important), but it's worth noting.

**Bug 4: `cmd/atcr/review.go` - interrupt check happens BEFORE result is processed**
- The new code checks `ctx.Err() == context.Canceled` immediately after ExecuteReview. This is correct.

**Bug 5: `cmd/atcr/review.go` - The check uses `ctx.Err()` which is the parent context**
- This is correct per the clarifications.

**Bug 6: `internal/fanout/review.go` - WriteManifest is best-effort, may fail silently**
- The code has `_ = WriteManifest(p.manifest...)`. If this fails, the interrupt marker is not persisted. The status would not show as interrupted.
- This is acknowledged in the risk table: "Partial results are incomplete or corrupted" → "Write status.json atomically". But the mitigation says "write to temp file, rename" - is this implemented?
- Looking at WriteManifest, it's not shown in the diff. But the diff doesn't add atomic write either. This could be a finding.

**Bug 7: Goroutine leak in `handleSignals` on non-interrupt path**
- The goroutine waits for `<-sigCh`. If no signal arrives, the goroutine blocks until process exit. Not a real issue in production.

**Bug 8: Test: `TestHandleSignals_CancelsContextOnSignal` - 15ms timeout is fragile**
- The test uses 15ms grace period. On slow CI, this might flake. Should be more robust, e.g., 50ms.

Let me look at specific issues that are clearly in scope:

1. **`cmd/atcr/review.go` - `interruptedBeforeFanout` is called from `gitrange.Resolve` and `fanout.PrepareReview`**, but the error message says "before it started". This is misleading for the PrepareReview case where the review directory has been created. Wait, no - PrepareReview creates the directory. Let me check the original code... The comment in the diff says "no review directory exists yet" for PrepareReview. Let me check...

Actually, looking at the PrepareReview call: the review directory is created by PrepareReview. If PrepareReview is cancelled mid-way, the directory might be partially created. The error message says "no partial results to save" - this might be inaccurate.

2. **`cmd/atcr/review.go` line ~158 - `errors.Is(ctx.Err(), context.Canceled)` check for PrepareReview**:
- The comment says "An interrupt during payload build / scaffolding cancels the context; no review directory exists yet". This is an assumption that may not hold.

3. **`cmd/atcr/main.go` - handleSignals never returns**:
- Wait, the goroutine in handleSignals never returns unless a signal arrives. If no signal arrives, the goroutine blocks forever. In production this is fine. But the design is a bit fragile.

Let me focus on the clearest, most impactful issues:

**Issue 1: Lost error on interrupt path in `cmd/atcr/review.go`**
- When `ExecuteReview` returns (result, err) with err != nil AND ctx is cancelled, the original err is discarded. This may be intentional but could lose important error information.

**Issue 2: Test fragility in `cmd/atcr/main_test.go`**
- 15ms and 10ms timeouts are too short for CI environments. Could flake.

**Issue 3: Signal channel never stopped**
- `signal.Notify` is called in main() but never stopped. signal.Stop(sigCh) is not called. This means the signal channel is registered for the lifetime of the process, and the goroutine is blocked. This is fine in production (process exits) but could leak in tests if main() is called multiple times.

**Issue 4: handleSignals doesn't return after cooperative shutdown completes**
- After cancel(), the goroutine waits 10s. If main() returns before 10s, the goroutine is killed. If main() doesn't return until after 10s (or hangs), forceExit(1) is called. The process exits with code 1.
- But if main() returns within 10s, the process exits with code 0 (success) or whatever code main() returned. The goroutine is killed mid-wait. This is correct behavior.

**Issue 5: `interruptedBeforeFanout` is called from `PrepareReview`**
- The error message says "before it started" but PrepareReview is called AFTER the user has done a lot of work (config loading, etc.). The message might be misleading.
- Also, if PrepareReview creates the review directory, calling interruptedBeforeFanout is correct because no fan-out has happened.

**Issue 6: The new code in review.go checks `ctx.Err()` after `ExecuteReview`**
- This is on the parent context, which is correct. But what if the parent context was cancelled for a reason OTHER than SIGINT/SIGTERM (e.g., the parent context had a deadline that was reached)? The code treats it as an interrupt.
- Actually, the parent context is `ctx, cancel := context.WithCancel(context.Background())` in main(). The only way it gets cancelled is via the signal handler's call to cancel(). So this is safe.

Wait, but the `runReview` function uses `cmd.Context()` which is the root context. If the root context is cancelled (by signal), the parent context is cancelled. But what if `cmd.Context()` returns a different context? Let me check... 

In the runReview function, `ctx` is likely `cmd.Context()`. The ExecuteContext call passes the parent context. So `cmd.Context()` is the same as the parent context. OK.

**Issue 7: Best-effort WriteManifest in `internal/fanout/review.go`**
- The `_ = WriteManifest(...)` is best-effort. If it fails, the interrupt marker is not persisted. The next status read would not show as interrupted.
- The risk table mentions "Write status.json atomically (write to temp file, rename)" as a mitigation. But this is not implemented in the diff.
- This is a potential issue but may be out of scope for the diff (the diff doesn't modify WriteManifest).

Let me also check the test `TestExecuteReview_InterruptPreservesPartialAndMarksInterrupted`:

The test creates a prep with `manifest: m` but the manifest is not yet written. The test then writes the manifest via `WriteManifest(dir, m)`. The prep's `manifest` field is set to the same `m`. So when ExecuteReview runs, it has access to the manifest.

Wait, but the test uses `WriteManifest(dir, m)` to write the initial manifest, and then `prep.manifest = m`. So the prep's manifest is the in-memory `m`. When ExecuteReview modifies `m.Interrupted`, the on-disk version is updated via `WriteManifest(p.Dir, p.manifest)`.

Let me check the test more carefully:
```go
m := &payload.Manifest{...}
require.NoError(t, WriteManifest(dir, m))
// ...
prep := &PreparedReview{
    ID: "2026-06-17_interrupt", Dir: dir,
    Slots: slots, MaxParallel: 1, manifest: m,
}
res, err := ExecuteReview(ctx, fake, prep)
// ...
mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
var got payload.Manifest
require.NoError(t, json.Unmarshal(mdata, &got))
assert.True(t, got.Interrupted, "AC4: manifest records the interrupt")
```

So after ExecuteReview, the manifest on disk should have `interrupted: true`. The test reads it back and checks. Good.

Now let me think about the overall structure:

The diff is well-organized and matches the sprint plan. The main areas of concern are:

1. **Signal handler goroutine lifetime**: The goroutine in handleSignals blocks on `<-sigCh` and is never explicitly stopped. In production, the process exits so it doesn't matter. In tests, the goroutine fires and returns.

2. **Lost error on interrupt path**: If ExecuteReview returns an error AND ctx is cancelled, the error is discarded.

3. **Test fragility**: 10-15ms timeouts may flake on slow CI.

4. **Best-effort WriteManifest**: If WriteManifest fails, the interrupt marker is lost.

5. **`interruptedBeforeFanout` message accuracy**: The message says "before it started" but PrepareReview is called after config loading, etc.

Let me also look at the `cmd/atcr/main.go` more carefully:

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    handleSignals(sigCh, cancel, os.Stderr)

    root := newRootCmd()
    if err := root.ExecuteContext(ctx); err != nil {
        code := exitCode(err)
        if code != 0 {
            fmt.Fprintln(os.Stderr, "atcr:", err)
        }
        os.Exit(code)
    }
}
```

The `signal.Notify` is called and the channel is registered. There's no `signal.Stop(sigCh)`. This means:
- On normal exit, the goroutine is still blocked on `<-sigCh`.
- The process exits, so the goroutine is killed.
- But if the process is embedded in another program (e.g., a test that calls main()), the goroutine leaks.

This is a minor concern for production but a real concern for testability.

**Issue 8: `signal.Stop` is never called**
- The signal channel is registered but never stopped. The goroutine blocks forever on `<-sigCh`. This is fine in production (process exits) but could cause issues in test environments where main() is called multiple times.

**Issue 9: `handleSignals` goroutine blocks forever if no signal arrives**
- Same as above.

Let me think about the test for `TestHandleSignals_CancelsContextOnSignal`:

```go
code := stubForceExit(t, 15*time.Millisecond)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
sigCh := make(chan os.Signal, 1)
var buf bytes.Buffer
handleSignals(sigCh, cancel, &buf)
sigCh <- syscall.SIGINT
```

The test sends SIGINT via the channel (not the actual signal). The handler receives it, cancels the context, waits 15ms, calls forceExit. The test then verifies the context is cancelled and the buffer contains the message.

Issue: The test doesn't call `signal.Stop(sigCh)`. But it uses the channel directly, not signal.Notify. So this is fine.

But wait, the goroutine in handleSignals is still running after the test. When the test ends, the goroutine is still blocked... no wait, the goroutine called forceExit and returned. The test waits for forceExit to be called via `require.Eventually`. So the goroutine has returned by the time the test ends.

OK, so the test is fine.

**Issue 10: The interrupt message in `cmd/atcr/review.go` uses `prep.ID` even when `result` is provided**
- Looking at `interruptMessage`:
  ```go
  return fmt.Sprintf(
      "\n⚠️ Review interrupted. %d/%d agents completed; partial results saved to %s.\n"+
          "   Run 'atcr status %s' to inspect.\n",
      done, total, dir, prep.ID)
  ```
- It always uses `prep.ID` for the status command, not `result.ID`. This is consistent because `prep.ID` is the review ID. But what if `result.ID != prep.ID`? Let me check... In normal flow, `result.ID` should equal `prep.ID`. The test confirms this:
  ```go
  result := &fanout.ReviewResult{ID: "rev", Dir: "/real/dir", ...}
  // ...
  assert.Contains(t, msg, "atcr status 2026-06-17_feat", "AC6: points at a command that exists")
  ```
  Wait, in `TestInterruptMessage_NilResultFallsBackToPrep`, prep.ID is "2026-06-17_feat" and the assertion checks for "atcr status 2026-06-17_feat". Good.
  In `TestInterruptMessage_UsesResultCounts`, prep.ID is "rev" and result.ID is "rev". The test doesn't check the status command, so we can't tell from the test.

OK, I think I've analyzed enough. Let me also check for any issues with the test setup:

**Issue 11: `TestExecuteReview_InterruptPreservesPartialAndMarksInterrupted` - the completer cancels on the 2nd call, but the 2nd call also returns success**
- The completer increments count first, then checks. On the 2nd call, count=2, which equals cancelAt. It calls cancel() and returns success.
- The 1st call also returns success.
- So 2 agents succeed, 2 agents fail (the 3rd and 4th are short-circuited by the engine's ctx.Err() check).
- The test expects Succeeded=2, Failed=2. Correct.

**Issue 12: The test `TestHandleSignals_CancelsContextOnSignal` uses `assert.ErrorIs(t, ctx.Err(), context.Canceled)`**
- This verifies that the context error is context.Canceled. Good.

**Issue 13: The test `TestHandleSignals_ForceExitsAfterGracePeriod` uses `assert.Equal(t, int32(1), atomic.LoadInt32(code), ...)`**
- This verifies that forceExit is called with code 1. Good.

Let me also check the `interruptedBeforeFanout` function:

```go
func interruptedBeforeFanout(cmd *cobra.Command) error {
    _, _ = fmt.Fprintln(cmd.ErrOrStderr(), "\n⚠️ Review interrupted before it started; no partial results to save.")
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}
```

Issue: The message says "no partial results to save" but if PrepareReview created the review directory, there might be partial files. The message is misleading.

Actually, looking at the flow:
1. `gitrange.Resolve` - no review directory created
2. `fanout.PrepareReview` - creates the review directory and writes initial files

If PrepareReview is cancelled mid-way, the directory might be partially created. The message "no partial results to save" is inaccurate.

But this is a minor issue. The user can still inspect the directory.

**Issue 14: `interruptedBeforeFanout` is called from both `gitrange.Resolve` and `fanout.PrepareReview`**
- The message is the same for both, but the situations are different. For `gitrange.Resolve`, no review directory exists. For `PrepareReview`, the directory might be partially created.
- A more accurate message would differentiate.

**Issue 15: The interrupt message doesn't mention the review ID when result is nil and prep.ID is used**
- Actually, it does: "Run 'atcr status %s' to inspect." with prep.ID. Good.

Let me also check the `cmd/atcr/main.go` for the `os.Stderr` usage:
- `handleSignals(sigCh, cancel, os.Stderr)` - uses os.Stderr. Good.
- But the test uses `var buf bytes.Buffer` and passes `&buf`. So the function is testable. Good.

**Issue 16: The `handleSignals` function uses `io.Writer` for the output parameter**
- This makes it testable. Good design.

Now let me think about what's most important to flag:

1. **Goroutine leak / signal channel not stopped**: Medium severity, affects testability and could cause issues if main() is called multiple times.

2. **Lost error on interrupt path**: Low severity, the interrupt message is more informative.

3. **Test fragility with 10-15ms timeouts**: Low severity, may flake on slow CI.

4. **Best-effort WriteManifest**: Low severity, acknowledged risk.

5. **`interruptedBeforeFanout` message accuracy**: Low severity, minor inaccuracy.

6. **The `interruptedBeforeFanout` is called for `PrepareReview` cancellation, but the message says "before it started"**: This is somewhat misleading because PrepareReview is called after config loading and git range resolution.

Let me also check the `cmd/atcr/review.go` for the `interruptedBeforeFanout` call site:

```go
prep, err := fanout.PrepareReview(ctx, cfg, req)
if err != nil {
    if errors.Is(ctx.Err(), context.Canceled) {
        return interruptedBeforeFanout(cmd)
    }
    return usageError(err)
}
```

If PrepareReview returns an error due to context cancellation, the code returns `interruptedBeforeFanout(cmd)`. But PrepareReview might have created the review directory before being cancelled. The message "no partial results to save" is inaccurate.

Actually, looking at PrepareReview more carefully (not in the diff), it likely creates the directory and writes initial files. If it's cancelled mid-way, the directory exists with partial files. The message should be more nuanced.

But this is a minor issue. The user can still inspect the directory.

Let me also check the `cmd/atcr/review.go` for the `gitrange.Resolve` call site:

```go
res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
if err != nil {
    if errors.Is(ctx.Err(), context.Canceled) {
        return interruptedBeforeFanout(cmd)
    }
    return usageError(fmt.Errorf("review failed: %w", err))
}
```

If Resolve returns an error due to context cancellation, the code returns `interruptedBeforeFanout(cmd)`. But Resolve doesn't create a review directory. The message is accurate here.

**Issue 17: The `interruptedBeforeFanout` function doesn't differentiate between Resolve and PrepareReview cancellations**
- For Resolve, no review directory exists.
- For PrepareReview, the directory might be partially created.
- The message "no partial results to save" is inaccurate for PrepareReview.

Let me also check the test for `TestInterruptedBeforeFanout_ExitOneWithNotice`:

```go
func TestInterruptedBeforeFanout_ExitOneWithNotice(t *testing.T) {
    cmd := newReviewCmd()
    var buf bytes.Buffer
    cmd.SetErr(&buf)

    err := interruptedBeforeFanout(cmd)
    require.Error(t, err)
    assert.Equal(t, exitFailure, exitCode(err), "interrupt before fan-out exits 1, not the exit-2 usage error")
    assert.Contains(t, buf.String(), "Review interrupted before it started")
    assert.NotContains(t, err.Error(), "review failed", "must not surface as a range/usage failure")
}
```

The test verifies:
- exit code is 1 (not 2)
- message contains "Review interrupted before it started"
- error doesn't contain "review failed"

Good test.

**Issue 18: The `interruptedBeforeFanout` error message says "review interrupted" but the test checks for "Review interrupted" (capital R)**
- The test checks `assert.Contains(t, buf.String(), "Review interrupted before it started")` - this is the stderr message.
- The error returned is `errors.New("review interrupted")` - lowercase.
- The test checks `assert.NotContains(t, err.Error(), "review failed")` - this is the error message.
- The test doesn't check the error message content, just that it doesn't contain "review failed". Good.

OK, I think I have a good understanding of the diff. Let me finalize my findings.

**Findings:**

1. **MEDIUM: `cmd/atcr/main.go` - Signal channel never stopped, goroutine blocks forever on normal exit**
   - The `handleSignals` goroutine blocks on `<-sigCh` indefinitely. `signal.Stop` is never called. In production this is fine (process exits), but it could cause issues if `main()` is called from another program or test harness.
   - Fix: Add `signal.Stop(sigCh)` after the goroutine returns, or use a context to cancel the goroutine.

2. **MEDIUM: `cmd/atcr/review.go` - Original error from ExecuteReview is discarded on interrupt path**
   - When `ExecuteReview` returns `(result, err)` with `err != nil` and `ctx.Err() == context.Canceled`, the original error is discarded. The user only sees the interrupt message.
   - Fix: Include the original error in the interrupt message or log it for debugging.

3. **LOW: `cmd/atcr/main_test.go` - Test timeouts of 10-15ms are fragile on slow CI**
   - The tests use very short timeouts (10ms, 15ms) which may flake on slow CI environments.
   - Fix: Use longer timeouts (e.g., 50-100ms) with appropriate `require.Eventually` polling.

4. **LOW: `cmd/atcr/review.go` - `interruptedBeforeFanout` message is inaccurate for PrepareReview cancellation**
   - The message says "no partial results to save" but PrepareReview may have created the review directory with initial files before being cancelled.
   - Fix: Differentiate the message based on
</think>
