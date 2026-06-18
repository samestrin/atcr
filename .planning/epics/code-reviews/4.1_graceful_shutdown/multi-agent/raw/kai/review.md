The user wants me to review a diff against a specific sprint plan scope. I need to:

1. Only flag issues in files/areas directly related to the work items in the sprint plan.
2. Output findings in the exact TD_STREAM format: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER`
3. If no issues, output nothing.
4. No prose, no preamble, no summary, no markdown, no code fences, no headings.

Let me analyze the diff carefully.

Files modified:
- `cmd/atcr/main.go` - signal handling (in scope)
- `cmd/atcr/main_test.go` - tests for signal handling (in scope)
- `cmd/atcr/review.go` - partial result preservation, interrupt handling (in scope)
- `cmd/atcr/review_test.go` - tests for interrupt message (in scope)
- `internal/fanout/interrupt_test.go` - integration test for interrupt (in scope)
- `internal/fanout/review.go` - ExecuteReview changes for interrupted flag (in scope)
- `internal/fanout/status.go` - ReadReviewStatus changes for RunInterrupted (in scope)
- `internal/fanout/status_test.go` - tests for interrupted status (in scope)
- `internal/payload/manifest.go` - Interrupted field added (in scope)

Let me look for issues:

**cmd/atcr/main.go:**
- `handleSignals` starts a goroutine that reads from `sigCh`, prints, cancels, waits 10s, then force exits.
- Issue: The goroutine will leak if no signal is ever sent, but it will just be blocked on `<-sigCh` forever. That's a goroutine leak on normal exit. However, since the program exits immediately after `root.ExecuteContext` returns normally, the process exits, so it's not a leak in practice. But wait, if `root.ExecuteContext` returns nil, the process exits via `os.Exit(0)` or implicit return? Actually looking at the code, if err != nil it prints and os.Exit(code). If err == nil, it falls through and `main()` returns normally (exit 0). But the goroutine is still running. Since the process exits, it's fine.
- Issue: `forceExit(1)` is called after the timeout, but what if the program already exited cleanly before the timeout? The goroutine is still running and will call `forceExit(1)` after 10 seconds even if `ExecuteContext` returned normally before that. Wait, no - if `ExecuteContext` returns normally, `main` returns and the process exits. The goroutine dies with the process. But in tests, if we call `handleSignals` and don't actually exit the process, the goroutine stays alive. That's why `stubForceExit` is used in tests. But in production code, it's fine because process exit kills it.
- Actually, there is a real issue: if a signal arrives and cancels the context, and then `ExecuteContext` returns (either nil or err), `main()` will proceed to exit. But the goroutine is still waiting on `time.After`. If `main()` calls `os.Exit(code)` (line 34 in the diff? Let me check).
Looking at the diff:
```go
	if err := root.ExecuteContext(ctx); err != nil {
		code := exitCode(err)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "atcr:", err)
			os.Exit(code)
		}
	}
```
Wait, if code == 0 (err is nil or exitCode returns 0?), it just falls through. Actually `exitCode(err)` - if err is nil, code is 0. Then it doesn't print and doesn't os.Exit. So main returns normally. But if err != nil and code is 1 or 2, it prints and os.Exit.
But if a signal arrives, context is canceled, and then `ExecuteContext` returns an error or nil. If it returns nil, `main()` falls through. The goroutine still has the timer running and will call `forceExit(1)` after 10s. That means even on graceful shutdown where `ExecuteContext` returns quickly after cancellation, the process will be killed with exit code 1 after 10 seconds? No, because `main()` returning causes the program to exit with 0 immediately (Go program exit). The goroutine is terminated when the process exits. So that's fine.

Wait, but what if `root.ExecuteContext(ctx)` returns before the grace period expires? Then `main` exits (0 or whatever), process ends. The timer goroutine is gone. So that's fine.

But what if `root.ExecuteContext(ctx)` hangs even after context cancellation? Then `main` doesn't return, and the timer eventually fires and kills the process with code 1. That's the intended behavior.

However, there is a subtle issue: `handleSignals` is called in `main()`, but there is no synchronization to wait for the goroutine to finish on normal exit. But since the process exits, it's fine.

Wait, I see another issue. In `cmd/atcr/review.go`, the `runReview` function checks `errors.Is(ctx.Err(), context.Canceled)` after `ExecuteReview` returns. But what if `ExecuteReview` returns an error AND the context is canceled? Looking at the diff:
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
This checks context canceled before checking `err`. But `ExecuteReview` might return an error (like a write pool error) and the context could be canceled. If context is canceled, it returns the interrupted error. But wait, if ExecuteReview returned an error and context is canceled, `result` might be nil. The `interruptMessage` handles nil result. But the comment says "completed agents are already persisted by WritePool and the manifest is marked interrupted". However, if `ExecuteReview` returned an error due to `WritePool` failure, the manifest might not be written (looking at the code in `review.go`, if `WritePool` fails, it writes a best-effort failure manifest but doesn't set `Interrupted`? Let me check `internal/fanout/review.go` diff.

In `internal/fanout/review.go`:
```go
	results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
	interrupted := errors.Is(ctx.Err(), context.Canceled)
	sum, err := WritePool(poolDir, results)
	if err != nil {
		// Persistence failed after the fan-out ran. Write a best-effort failure
		// manifest...
		if p.manifest != nil {
			p.manifest.CompletedAt = time.Now().UTC()
			p.manifest.Interrupted = interrupted  // <-- Wait, looking at the diff, it DOES set interrupted in the error path too!
			_ = WriteManifest(p.Dir, p.manifest) // best-effort
		}
		return nil, err
	}
```
Actually, looking at the diff carefully:
```go
 	results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
+
+	// Detect an external interrupt ...
+	interrupted := errors.Is(ctx.Err(), context.Canceled)
+
 	sum, err := WritePool(poolDir, results)
 	if err != nil {
 		// Persistence failed after the fan-out ran. Write a best-effort failure
 		// manifest so EnsureReviewComplete does not mistake this for an in-progress
 		// review and try to reconcile it. The original error (persistence) is
 		// preserved; callers (review.go) show the root cause.
 		//
 		// Nil guard: PreparedReview may be constructed directly in tests without a manifest.
 		if p.manifest != nil {
 			p.manifest.CompletedAt = time.Now().UTC()
+			p.manifest.Interrupted = interrupted
 			_ = WriteManifest(p.Dir, p.manifest) // best-effort; if this also fails, stale inference covers it
 		}
 		return nil, err
 	}
 	...
 	m := *p.manifest
 	m.Partial = sum.Partial
 	m.CompletedAt = time.Now().UTC()
+	m.Interrupted = interrupted
```
So in the error path (WritePool failed), it does set Interrupted. But in `cmd/atcr/review.go`, if `ExecuteReview` returns an error, `result` is nil. Then it checks `ctx.Err() == Canceled` and returns the interrupted error. But wait - if `ExecuteReview` returned an error because WritePool failed, AND the context was canceled, the code in `review.go` treats it as an interrupt and returns exit 1 with a message. But actually, a WritePool error during interrupt is still an interrupt. However, if WritePool failed but context was NOT canceled, it falls through to `if err != nil` check later? Looking at `cmd/atcr/review.go`:
```go
	result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)

	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
		return &codedError{code: exitFailure, err: errors.New("review interrupted")}
	}

	if result != nil {
		// print success summary
	}

	if err != nil {
		// log and return err
	}

	// then reconcile/verify/etc
```
Wait, I need to see the full context of `cmd/atcr/review.go` diff. The diff shows the insertion point. Let me reconstruct:

After the insertion:
```go
	result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)

	// Graceful interrupt ...
	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
		return &codedError{code: exitFailure, err: errors.New("review interrupted")}
	}

	if result != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
			result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
	}

	if err != nil {
		logger.Error("review fan-out failed", log.Err(err), log.String("review_id", prep.ID))
		return fmt.Errorf("review %s failed: %w", prep.ID, err)
	}
```
Wait, this is problematic. If `ExecuteReview` returns `err != nil` and `result == nil` (which it does on WritePool error), and context is NOT canceled, then it falls through to `if err != nil` and returns the error. But if context IS canceled, it returns the interrupted error. That seems intentional.

But wait - what if `ExecuteReview` returns `result != nil` and `err != nil`? Looking at the signature, `ExecuteReview` returns `(res, err)`. Can it return both non-nil? The error path returns `nil, err`. The success path returns `&result, nil`. So no.

But what about the check order? The `if errors.Is(ctx.Err(), context.Canceled)` check happens BEFORE the `if result != nil` check. So if context is canceled but `ExecuteReview` somehow returned a non-nil result (it does return a result on interrupt, based on the test), the result gets handled correctly by `interruptMessage`.

Actually, looking at `internal/fanout/review.go` diff again, `ExecuteReview` returns `res, err` where in the success path `err` is nil. But what about when interrupted? The engine runs, returns results. `WritePool` succeeds. `sum` is calculated. Then `m.Interrupted = interrupted` is set. Then `return &result, nil`. So `ExecuteReview` returns a non-nil result and nil error on interrupt.

So in `cmd/atcr/review.go`, when interrupted, `result != nil`, `err == nil`, `ctx.Err() == Canceled`. It hits the first check, prints message, returns codedError. It skips the success print. Good.

Now let me look for actual issues.

**Issue 1: cmd/atcr/main.go - goroutine may call forceExit after normal return?**
No, process exits.

**Issue 2: cmd/atcr/main.go - `handleSignals` goroutine can be blocked by `out`?**
If `out` blocks (unlikely for os.Stderr), the goroutine hangs and won't cancel context. But it's writing to stderr. Not a real issue.

**Issue 3: cmd/atcr/main.go - `forceExit` called without draining?**
The design says it force-exits. That's intended.

**Issue 4: cmd/atcr/review.go - nil pointer dereference risk?**
Looking at `interruptMessage`:
```go
func interruptMessage(result *fanout.ReviewResult, prep *fanout.PreparedReview) string {
	done, total, dir := 0, 0, prep.Dir
	if result != nil {
		done, total, dir = result.Summary.Succeeded, result.Summary.Total, result.Dir
	}
	return fmt.Sprintf(..., done, total, dir, prep.ID)
}
```
`prep` is used directly. If `prep` is nil, this panics. But `prep` is checked earlier:
```go
	prep, err := fanout.PrepareReview(ctx, cfg, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedBeforeFanout(cmd)
		}
		return usageError(err)
	}
```
So `prep` is non-nil after that. Then `ExecuteReview` is called. If `ExecuteReview` panics, the defer in `runReview` might handle it, but `prep` is still non-nil. So `interruptMessage(result, prep)` is safe.

But wait, what if `ExecuteReview` takes a long time to return after context cancellation, and in the meantime `prep` gets garbage collected? No, Go doesn't work that way.

**Issue 5: internal/fanout/review.go - race condition?**
`interrupted := errors.Is(ctx.Err(), context.Canceled)` is checked after `Run` returns. The parent ctx could have been canceled during the run. But `ctx.Err()` is read after `Run` finishes. That's fine. But is there a race between the engine checking `ctx.Err()` before each agent and this code? No.

**Issue 6: internal/fanout/review.go - manifest write error ignored**
`_ = WriteManifest(...)` - the error is ignored. But the comment says "best-effort; if this also fails, stale inference covers it". This is intentional and within scope, but should I flag it? It's existing pattern (the `CompletedAt` write also ignores error). Probably not.

**Issue 7: cmd/atcr/main_test.go - `TestHandleSignals_CancelsContextOnSignal`**
This test sends a signal, waits for context done, then asserts. Then it says "Once the grace timer fires forceExit, the goroutine has returned, so reading buf is race-free." But the test calls `require.Eventually` to wait for `code == 1`. However, `buf` is read after that. The issue is: between `ctx.Done()` firing and `forceExit` firing, the goroutine is sleeping. The test then waits for `code == 1` with a 1 second timeout. But the timeout is set to 15ms. So it should fire quickly. But the test might be flaky if the system is overloaded? 15ms is small but acceptable for a test. The `require.Eventually` has 1s timeout with 5ms tick. Should be fine.

But wait, there's a subtle issue in `TestHandleSignals_CancelsContextOnSignal`: after `sigCh <- syscall.SIGINT`, the test waits for `ctx.Done()`. Then it asserts `ctx.Err()`. Then it waits for `code == 1`. But the goroutine does:
1. `<-sigCh`
2. `fmt.Fprintln(out, ...)`
3. `cancel()`
4. `<-time.After(gracefulShutdownTimeout)`
5. `fmt.Fprintln(out, ...)`
6. `forceExit(1)`

The test writes to `sigCh`, then waits for `ctx.Done()`. The cancel happens immediately after reading sigCh. So ctx.Done() fires quickly. Then the test waits for code==1. But between step 3 and step 4, the goroutine is sleeping for 15ms. The `require.Eventually` polls every 5ms. So after ~15-20ms, code should be 1. Then it reads `buf.String()`. At that point, the goroutine has exited (forceExit was called, but since it's stubbed, it just stores the code and returns). So reading buf is safe. This seems correct.

**Issue 8: cmd/atcr/main.go - defer cancel() in main**
`defer cancel()` is called in `main()`. If a signal arrives, `handleSignals` calls `cancel()`. Then later `main()` returns and `defer cancel()` runs again. Calling cancel twice is safe. No issue.

**Issue 9: internal/fanout/interrupt_test.go - `defer cancel()`**
The test does `ctx, cancel := context.WithCancel(context.Background()); defer cancel()`. Then it passes `cancel` to `cancelAfterCompleter`. When `n == c.cancelAt`, `c.cancel()` is called. Later, the test defers `cancel()` again. Double cancel is safe.

**Issue 10: `cancelAfterCompleter` race**
`c.mu` protects `c.count`. But `c.cancel()` is called while holding the lock? No:
```go
	c.mu.Lock()
	c.count++
	n := c.count
	c.mu.Unlock()
	if n > c.cancelAt {
		return "", ctx.Err()
	}
	if n == c.cancelAt {
		c.cancel() // called without lock
	}
```
This is fine because `cancel` is called after releasing the lock, and only one goroutine calls it (since MaxParallel=1). So no race.

**Issue 11: `cmd/atcr/review.go` - `interruptedBeforeFanout` doesn't check `cmd` for nil**
`cmd.ErrOrStderr()` could panic if cmd is nil, but `interruptedBeforeFanout` is only called within `runReview` where `cmd` is non-nil. And in tests, it's passed a non-nil cmd. So no issue.

**Issue 12: `cmd/atcr/review.go` - context check after `gitrange.Resolve`**
```go
	res, err := gitrange.Resolve(ctx, ".", ...)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedBeforeFanout(cmd)
		}
		return usageError(...)
	}
```
Wait, `ctx.Err()` might be `context.Canceled` but `gitrange.Resolve` might have returned a different error (e.g., a git error) that wraps context.Canceled? The check is on `ctx.Err()` directly, not `errors.Is(err, context.Canceled)`. But if the user pressed Ctrl-C during a slow git operation, the context is canceled, and `gitrange.Resolve` should return `ctx.Err()` or a wrapped error. But if it returns a different error while ctx is canceled (unlikely but possible if the error was already in flight), then this check might miss it. However, the check `errors.Is(ctx.Err(), context.Canceled)` is robust for detecting the signal. The intent is to route any error during cancel to the graceful path. But if git returns a "repository not found" error and concurrently the context is canceled, we'd return "interrupted" instead of the git error. Is that bad? Actually, if the context is canceled, the user pressed Ctrl-C, so they don't care about the git error. They want the interrupt path. This seems correct.

But wait, `ctx.Err()` might not be set immediately when the signal arrives. The signal handler calls `cancel()`, which sets `ctx.Err()` to `Canceled`. But `gitrange.Resolve` might be blocked on I/O and not checking ctx. When it finally returns (perhaps with a timeout or completion), `ctx.Err()` is Canceled. So it routes to interrupt. Good.

However, there is a subtle issue: `errors.Is(ctx.Err(), context.Canceled)` - if the context is canceled, `ctx.Err()` returns `context.Canceled`. `errors.Is` works. But what if the parent context is canceled and `gitrange.Resolve` uses a child context? `ctx.Err()` on the parent still returns Canceled. So this is fine.

**Issue 13: `cmd/atcr/review.go` - `fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(...))`**
`interruptMessage` returns a string that already starts with `\n`. Then `fmt.Fprint` is used. If `cmd.ErrOrStderr()` is buffered, it might not include a trailing newline? Actually `interruptMessage` ends with `\n`. So it's fine.

Wait, `interruptMessage` returns:
```go
	return fmt.Sprintf(
		"\n⚠️ Review interrupted. %d/%d agents completed; partial results saved to %s.\n"+
			"   Run 'atcr status %s' to inspect.\n",
		done, total, dir, prep.ID)
```
This has newlines. Good.

**Issue 14: `cmd/atcr/review.go` - after `ExecuteReview` context canceled check**
```go
	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
		return &codedError{code: exitFailure, err: errors.New("review interrupted")}
	}
```
Then later:
```go
	if err != nil {
		logger.Error("review fan-out failed", ...)
		return fmt.Errorf("review %s failed: %w", prep.ID, err)
	}
```
But if `ExecuteReview` returns `result != nil` and `err != nil`? As discussed, it doesn't. But what if it returns `result == nil` and `err == nil`? Is that possible? Looking at `ExecuteReview`, it always returns an error if result is nil. So not possible.

But wait - what if `ExecuteReview` returns `result != nil` and `err != nil` in some future change? The code would fall through if context is not canceled. But that's not in this diff.

**Issue 15: `cmd/atcr/review.go` - `result != nil` check after interrupt check**
If `ctx.Err()` is not Canceled, and `result != nil`, it prints the success line. Then if `err != nil`, it returns the error. But `ExecuteReview` returns `result != nil` only when err is nil. So the `err != nil` block after `result != nil` would only trigger if err is non-nil but result is non-nil, which doesn't happen. However, looking at the original code, the `result != nil` block was there before, and the `err != nil` block was there after. The diff inserts the interrupt check between `ExecuteReview` and `result != nil`.

Wait, looking at the original code structure... The diff shows:
```go
 	result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)
+
+	// Graceful interrupt ...
+	if errors.Is(ctx.Err(), context.Canceled) {
+		...
+	}
+
 	if result != nil {
 		...
 	}
 
 	if err != nil {
 		logger.Error(...)
 		return fmt.Errorf(...)
 	}
```
This seems fine. If `ExecuteReview` returns err, and ctx is not canceled, it falls through to `if err != nil`. If `ExecuteReview` returns result and no err, it prints success and continues.

**Issue 16: `internal/fanout/status.go` - precedence of `RunInterrupted`**
The diff adds:
```go
	if m.Interrupted {
		st.Status = RunInterrupted
	}
```
This is after the succeeded/failed logic. So interrupted overrides everything. That's intended.

**Issue 17: `cmd/atcr/main.go` - `handleSignals` parameter `out io.Writer`**
The function signature takes `out io.Writer` but in `main()` it's passed `os.Stderr`. In tests it's passed `&buf`. Good.

**Issue 18: `cmd/atcr/main.go` - `os.Exit`**
Wait, looking at `main()`:
```go
	if err := root.ExecuteContext(ctx); err != nil {
		code := exitCode(err)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "atcr:", err)
			os.Exit(code)
		}
	}
```
If `err != nil` but `exitCode(err) == 0`, it doesn't print and doesn't exit. But `exitCode` returns 0 only for nil errors. Since `err != nil`, code won't be 0. So it always exits if err != nil. Fine.

But what about the case where `root.ExecuteContext` returns an error because context was canceled (e.g., from the signal handler)? The `review.go` returns `&codedError{code: exitFailure, ...}`. `exitCode` would return `exitFailure` (1). So `main` prints and `os.Exit(1)`. Good.

However, there's a subtle issue: if the signal handler's `forceExit(1)` fires AFTER `main()` has already started exiting (or even while `main()` is in `os.Exit`), there could be a race. But `os.Exit` exits immediately. The goroutine running `forceExit` is killed. But what if `main()` returns normally (no error) and the signal arrives just as `main()` is returning? Then `main()` doesn't hit the `os.Exit` path, it just returns. The process exits with 0. But the goroutine wakes up, calls `forceExit(1)` after 10s... Wait, no, if `main()` returns normally, the process exits with 0 immediately. The goroutine is terminated. Unless `main()` is blocked? No.

But what if `ExecuteContext` returns nil (success), and during the return path, a signal arrives? The goroutine prints the message and starts the timer. `main()` continues to return and process exits with 0. The timer goroutine is killed. That's actually fine - if the review finished successfully, we don't care about the signal? But the user pressed Ctrl-C after success. Should it still exit 1? The design says signal handling is uniform. But if the work is done, does it matter? The sprint plan says "SIGINT handling is applied uniformly to all subcommands including atcr serve". The signal could arrive at any time.

Actually, there's a more serious issue: if `ExecuteContext` returns an error with code 2 (usage error), `main()` does `os.Exit(2)`. The signal handler goroutine, if a signal arrived earlier and is sleeping, will call `forceExit(1)` after 10s. But since `os.Exit` was already called, the process is gone. So no issue.

Wait, what if the signal arrives, `cancel()` is called, and then `ExecuteContext` returns an error. `main()` calls `os.Exit(code)`. The goroutine is still sleeping. The process exits. Fine.

What if the signal arrives, `cancel()` is called, and `ExecuteContext` returns quickly (e.g., because it checks ctx and returns). `main()` calls `os.Exit(1)`. But the goroutine's timer is still running. It doesn't matter because the process is already gone. Unless `os.Exit` in Go does not immediately kill other goroutines? Actually, `os.Exit` terminates the program immediately; goroutines do not continue. So fine.

But in tests, they substitute `forceExit`. So tests don't actually exit.

**Issue 19: `cmd/atcr/main_test.go` - `TestHandleSignals_CancelsContextOnSignal` race**
After `ctx.Done()` fires, the test does:
```go
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	// Once the grace timer fires forceExit, the goroutine has returned, so reading
	// buf is race-free.
	require.Eventually(t, func() bool { return atomic.LoadInt32(code) == 1 }, time.Second, 5*time.Millisecond)
	assert.Contains(t, buf.String(), "shutting down gracefully", "AC1: graceful notice printed")
```
Wait, the comment says "Once the grace timer fires forceExit, the goroutine has returned, so reading buf is race-free." But `buf.String()` is called after `require.Eventually` returns. At that point, `forceExit(1)` has been called. But `fmt.Fprintln(out, "Graceful shutdown timed out...")` happens BEFORE `forceExit(1)`. So when `code == 1`, the second Fprintln has already executed. Thus `buf` contains both strings. But does the goroutine necessarily write to `buf` before calling `forceExit`? Yes, sequentially. So `buf.String()` after `code == 1` is safe.

But what if `code == 1` is set, but the goroutine hasn't written to `buf` yet? No, the code is:
```go
		_, _ = fmt.Fprintln(out, "Graceful shutdown timed out, forcing exit")
		forceExit(1)
```
Sequential. So if `forceExit(1)` has executed (and in the stub, it just stores the code), the Fprintln has definitely executed. Good.

Wait, but in the first test, the assertion is `assert.Contains(t, buf.String(), "shutting down gracefully", ...)`. This is the FIRST message, written before `cancel()`. So it's definitely in `buf` by the time we read it, because it was written before the grace timer even started. So that's safe too.

**Issue 20: `cmd/atcr/main.go` - `sigCh` buffer size**
`sigCh := make(chan os.Signal, 1)`. The comment says "Buffer 1 so the signal is never dropped if it arrives before the goroutine blocks on the channel." But `signal.Notify` with a buffered channel: if two signals arrive before the goroutine reads, the second one might be dropped? Actually, `os/signal` package: "The channel should be buffered." If the channel is full, the signal package may drop the signal? Let's recall: package doc says "If the channel is full, the signal may be lost." With buffer 1, if two SIGINTs arrive before the goroutine reads, the second is lost. But the design says "Only the first signal is handled; the grace timer is the hard backstop against a hang." So losing the second signal is fine. No issue.

**Issue 21: `internal/fanout/interrupt_test.go` - `cancelAfterCompleter` comment says "succeeds for the first cancelAt completions"**
The comment says it succeeds for the first `cancelAt` completions and cancels on the `cancelAt`-th call. But the code says:
```go
	if n > c.cancelAt {
		return "", ctx.Err()
	}
	if n == c.cancelAt {
		c.cancel()
	}
	return "CRITICAL|...", nil
```
So for `n == c.cancelAt`, it cancels AND returns success. For `n > c.cancelAt`, it returns ctx.Err(). With `MaxParallel=1`, the engine checks `ctx.Err()` before each invocation. After cancel, the engine short-circuits remaining slots to StatusTimeout. So `Complete` is never called for `n > cancelAt`. Good.

But wait, the return value when `n == cancelAt` is a hardcoded TD_STREAM string. That's used as the agent result. Fine.

**Issue 22: `cmd/atcr/review.go` - `interruptMessage` uses `prep.ID` at the end**
```go
	return fmt.Sprintf(
		"\n⚠️ Review interrupted. %d/%d agents completed; partial results saved to %s.\n"+
			"   Run 'atcr status %s' to inspect.\n",
		done, total, dir, prep.ID)
```
This uses `prep.ID` in the format string. If `prep` is nil, panic. But `prep` is not nil here. Safe.

**Issue 23: `cmd/atcr/review.go` - `interruptedBeforeFanout` returns exitFailure**
`exitFailure` is 1. The sprint plan says "exit 1 (consistent with the 10s force-exit path)". Good.

**Issue 24: `cmd/atcr/review.go` - `runReview` has a check `if errors.Is(ctx.Err(), context.Canceled)` after `gitrange.Resolve`**
What if `ctx.Err()` is `context.DeadlineExceeded` instead? The sprint plan specifically says "Detect interruption via parent ctx.Err() == context.Canceled". So deadline exceeded is not treated as an interrupt. That's correct - it would fall through to usageError or other error handling. Good.

**Issue 25: `cmd/atcr/review.go` - `gitrange.Resolve` error check**
If `gitrange.Resolve` returns an error that wraps `context.Canceled`, but `ctx.Err()` is nil at the moment? That's impossible if the context was actually canceled. But if the function returned a stale context.Canceled error while ctx is not canceled? Unlikely.

**Issue 26: `internal/fanout/review.go` - `interrupted` detection uses parent `ctx`**
```go
	interrupted := errors.Is(ctx.Err(), context.Canceled)
```
This is correct per the design: runCtx might be DeadlineExceeded, but parent ctx is Canceled on signal.

**Issue 27: `cmd/atcr/review.go` - `errors.Is(ctx.Err(), context.Canceled)` after `ExecuteReview`**
What if the context was canceled by something other than the signal handler? For example, a parent timeout? But the root context in `main()` has no deadline. Only the signal handler cancels it. And `runReview` might create child contexts, but it passes `ctx` (from `cmd`) down. The `cmd.Context()` is the root context created in `main()`. So `ctx.Err() == Canceled` only happens on signal. Good.

Wait, but `runReview` might be called with a different context in tests? That's fine.

**Issue 28: `cmd/atcr/main.go` - `handleSignals` starts a goroutine but what if `main()` exits before the goroutine even starts?**
Not possible in practice.

**Issue 29: `cmd/atcr/main.go` - missing `signal.Stop` on normal exit?**
The sprint plan doesn't mention cleanup. But not stopping signal notify is a minor leak (the runtime keeps a reference to the channel). Since `main()` exits immediately after, it's fine.

**Issue 30: `cmd/atcr/main.go` - `fmt.Fprintln(out, "\nReceived interrupt...")`**
Writes to `out` (stderr). If stderr is closed or redirected to a full pipe, this blocks. But that's standard.

**Issue 31: `internal/fanout/status.go` - `ReadReviewStatus`**
If `m.Interrupted` is true, it sets `st.Status = RunInterrupted`. But what if the summary file is missing? The function might return early with an error before reaching this point? Let's look at the original code. The diff only shows the added lines. The original `ReadReviewStatus` probably reads summary and manifest. If summary is missing, it might return error or treat as in-progress. But the diff is minimal. Not enough info to flag.

**Issue 32: `cmd/atcr/review.go` - `interruptMessage` uses `result.Summary.Total`**
If `result` is not nil but `result.Summary` is zero value? `Total` would be 0. That's fine.

**Issue 33: `cmd/atcr/review.go` - `result.Dir` vs `prep.Dir`**
If `result` is nil, uses `prep.Dir`. If `result` is not nil, uses `result.Dir`. The comment says "the review id and directory fall back to the PreparedReview". But the format string uses `prep.ID` always, not `result.ID`. Wait:
```go
	return fmt.Sprintf(
		"\n⚠️ Review interrupted. %d/%d agents completed; partial results saved to %s.\n"+
			"   Run 'atcr status %s' to inspect.\n",
		done, total, dir, prep.ID)
```
It uses `dir` (which is `result.Dir` if result != nil, else `prep.Dir`), but for ID it always uses `prep.ID`. Is `result.ID` different from `prep.ID`? Looking at `ExecuteReview`, it creates `result.ID = p.ID` (from `prep.ID`). So they should be the same. But what if they diverge in the future? The comment says "fall back to the PreparedReview, which is always available before the fan-out starts." But it doesn't fallback ID. Actually, looking at the comment: "result may be nil when ExecuteReview was cancelled before producing one, so the review id and directory fall back to the PreparedReview, which is always available before the fan-out starts." But the code always uses `prep.ID`. That's fine because `result.ID` should equal `prep.ID` anyway. But if `result` is nil, we need `prep.ID`. So using `prep.ID` unconditionally is correct. The `dir` variable handles the fallback for the directory. This seems correct.

Wait, but the comment says "the review id and directory fall back to the PreparedReview". The code falls back directory but not ID. However, since `prep.ID` is always correct, it's not a bug.

**Issue 34: `cmd/atcr/main.go` - `context.Background()`**
The root context is `context.Background()`. The design says "create root context with cancellation". That's fine.

**Issue 35: `internal/fanout/review.go` - `WritePool` error path**
If `WritePool` fails, it sets `p