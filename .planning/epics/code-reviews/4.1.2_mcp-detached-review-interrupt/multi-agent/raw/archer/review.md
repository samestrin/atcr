Let me carefully analyze this diff against the sprint plan scope.

The sprint plan is about: Marking serve-mode detached reviews interrupted on shutdown. The key files are `internal/mcp/handlers.go`, `internal/mcp/server.go`, and the new test file `internal/mcp/shutdown_test.go`.

Let me review the changes:

### handlers.go changes

1. Added `shutdownCtx` and `shutdownCancel` fields to the engine struct.
2. Added `withShutdownCancel` method that derives a child context cancelled when server shuts down.
3. Modified `handleReview` to use `withShutdownCancel` wrapping `reviewContext`.

Let me look at `withShutdownCancel`:
```go
func (e *engine) withShutdownCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.shutdownCtx == nil {
		return ctx, func() {}
	}
	cctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.shutdownCtx, cancel)
	return cctx, func() { stop(); cancel() }
}
```

This looks correct. The `AfterFunc` registers a callback that cancels `cctx` when `shutdownCtx` is cancelled. The returned cancel function stops the AfterFunc and cancels the context.

Wait - there's a potential issue here. When `stop()` is called, if the AfterFunc has already fired (i.e., shutdown happened), `stop()` returns false but the cancel has already been called. Then `cancel()` is called again, which is a no-op. That's fine.

But what if `stop()` returns true (meaning it successfully stopped the AfterFunc before it fired)? Then `cancel()` cancels the context. That's the normal cleanup path. Fine.

What if the AfterFunc is concurrently executing when `stop()` is called? According to Go docs, `stop()` returns false if the function has already been started or stopped. But there could be a race where the AfterFunc's `cancel` is called concurrently with the deferred `cancel()`. Since `cancel()` is idempotent, this is safe.

Actually wait - there's a subtle issue. The Go docs say:
> If it [stop] returns false, then either the context is done (and f has been or will be called) or the function f was already stopped.

So if stop returns false, the AfterFunc might be running concurrently. But since cancel is safe to call concurrently, this is fine.

### server.go changes

```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
// s.Run returns for two distinct reasons...
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

Wait, there's a subtle issue here. After `s.Run` returns, we check `ctx.Err() != nil`. But what if `s.Run` returns an error because of something else (like a transport error), but `ctx` is not yet cancelled? Then `serverShutdown` would be false, and we wouldn't cancel the detached reviews.

Hmm, but the comment says: "a cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the server itself is shutting down, while ctx.Err()==nil means a clean client stdio disconnect."

What if `s.Run` returns with an error but ctx isn't cancelled? In that case, detached reviews would NOT be cancelled. Is that correct? Looking at the sprint plan, it says "Only the former interrupts in-flight detached reviews." So a transport error that's not a context cancellation would be treated like a client disconnect. That seems like a design decision, but it could be a correctness issue if the server is actually shutting down due to an error.

Actually, let me re-read. The sprint plan says:
- AC1: A detached MCP review in flight when the server begins shutdown is marked interrupted.
- AC3: The existing serve-mode drain behavior is unchanged.

The distinction here is between "server shutdown" (SIGINT) and "client disconnect". If `s.Run` returns with an error but ctx isn't cancelled, it's ambiguous. But the sprint plan specifically mentions SIGINT/SIGTERM as the shutdown signal, so checking `ctx.Err() != nil` seems reasonable.

But wait - what about the case where `runErr != nil` but `ctx.Err() == nil`? In that case, `shutdownReviews` is called with `serverShutdown=false`, meaning detached reviews are NOT cancelled. Then `runErr` is returned. But the server is about to return from `Serve`, which means the process might exit. If there are in-flight detached reviews, they'd be left running... but the process is about to exit anyway. Actually, `e.drain(timeout)` is still called, so they get the drain window. This seems acceptable.

Let me look at the test file more carefully.

### shutdown_test.go

The test `TestServeShutdown_ClientDisconnectDoesNotInterrupt` calls:
```go
e.shutdownReviews(false, 200*time.Millisecond)
```
with a 200ms drain. The review is still blocked (blockingCompleter blocks until ctx is cancelled). Since `serverShutdown=false`, `shutdownCancel` is NOT called, so the review context is never cancelled. The drain waits 200ms and returns. The review is still in_progress. Then the test checks:
```go
assert.Equal(t, fanout.RunInProgress, st.Status, ...)
```

But wait - after the test returns, `t.Cleanup` fires `e.shutdownCancel()` and `e.drain(time.Second)`. This will cancel the review and it should then be marked interrupted. But the test already passed by then. This is fine for test cleanup.

Actually, there's a subtle issue. The `t.Cleanup` in `startInFlightReview` calls `e.shutdownCancel()` and `e.drain(time.Second)`. But in `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`, the test already calls `e.shutdownReviews(true, shutdownDrain)` which calls `e.shutdownCancel()` and `e.drain(shutdownDrain)`. So `shutdownCancel` is called twice. That's fine since it's idempotent.

Let me look at the `blockingCompleter`:
```go
func (b *blockingCompleter) Complete(ctx context.Context, _ llmclient.Invocation) (string, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return "", ctx.Err()
}
```

This blocks until ctx is done. When the review context is cancelled (via shutdown), this returns. Good.

Now let me think about potential issues:

1. **Race condition in `withShutdownCancel`**: The `stop()` and `cancel()` in the returned cancel function - is there a race? Let me think... `context.AfterFunc` registers a function to run when the context is done. `stop()` returns true if it successfully prevented the function from running. If `stop()` returns false, the function either already ran or was already stopped. In either case, calling `cancel()` afterward is safe (idempotent). No race issue.

2. **`shutdownReviews` ordering**: `shutdownCancel()` is called BEFORE `e.drain(timeout)`. This means the detached reviews are cancelled first, then the drain waits for them to finish writing their interrupted manifests. This matches the sprint plan: "Serve fires the server-lifecycle cancel after s.Run returns and before e.drain(...), so the cancelled fan-out flushes its partial + interrupted manifest within the existing 5s drain window." Correct.

3. **Nil check for `shutdownCancel`**: In `shutdownReviews`, there's a check `e.shutdownCancel != nil`. But what about `e.shutdownCtx`? If `shutdownCtx` is nil but `shutdownCancel` is not nil... that shouldn't happen since they're set together in `buildServer`. But if someone constructs `&engine{}` directly, both are nil. The `shutdownReviews` function checks `e.shutdownCancel != nil`, which handles this. OK.

4. **`NewServer` path**: The comment says "The NewServer path discards the engine and never fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so the unfired cancel is reclaimed with the engine." Let me check if `NewServer` is a different code path. Looking at the diff, `buildServer` creates the engine with shutdownCtx/shutdownCancel. If `NewServer` doesn't use `buildServer`, then the engine wouldn't have these fields set. But `NewServer` isn't shown in the diff, so I can't verify this. The comment claims it's handled. I'll trust the comment for now.

5. **Potential issue: `ctx.Err() != nil` check after `s.Run` returns**. What if `s.Run` blocks for a long time after ctx is cancelled? By the time it returns, `ctx.Err()` would still be non-nil. That's fine. But what if `s.Run` returns normally (no error) but ctx was cancelled in the meantime? Then `ctx.Err() != nil` would be true, and we'd cancel the reviews. That seems correct - if the context was cancelled, the server is shutting down.

Actually, I want to think about one more thing. The `context.WithCancel(context.Background())` in `buildServer` creates a context that's never automatically cancelled. It's only cancelled when `shutdownCancel()` is called. If `Serve` returns without calling `shutdownReviews` (e.g., due to an early return), the cancel function is never called, and the context leaks. But looking at the code, `Serve` always calls `e.shutdownReviews(...)` after `s.Run` returns. The only early return is before `buildServer` is called (line 45 `return err`). So this seems fine.

Wait, let me look more carefully at the `Serve` function. The diff shows:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
if runErr != nil {
    return fmt.Errorf("serve stdio: %w", runErr)
}
return nil
```

So `shutdownReviews` is always called. Good.

6. **Test: `TestServeShutdown_ClientDisconnectDoesNotInterrupt`** - This test calls `e.shutdownReviews(false, 200*time.Millisecond)`. The review is still blocked. After 200ms, the drain returns. The test then reads the status and asserts it's `RunInProgress`. But what if the fan-out hasn't written the in_progress status yet? Actually, `handleReview` already returned with `runningStatus`, so the status should already be on disk. And the review is blocked in the completer, so it hasn't changed. This should be fine.

7. **Test: `TestServeShutdown_NormalCompletionNotInterrupted`** - This test uses `fakeCompleter{resp: validFindings}` which presumably returns immediately. It waits for terminal status, then calls `e.shutdownCancel()` and `e.drain(shutdownDrain)`. Then it reads the status again and asserts it's still `RunCompleted`. This tests AC4. Looks correct.

8. **Potential issue: `withShutdownCancel` when `e.shutdownCtx` is nil**. The function returns `ctx, func() {}`. This means the returned cancel function does nothing. But the caller still `defer cancel()`s it. That's fine - it's a no-op.

9. **Is there a potential deadlock?** In `withShutdownCancel`, `context.AfterFunc(e.shutdownCtx, cancel)` registers `cancel` to be called when `e.shutdownCtx` is done. If `e.shutdownCtx` is already done when `AfterFunc` is called, `cancel` is called immediately (in a new goroutine). That's fine.

10. **Memory leak concern**: The comment mentions "it stops the AfterFunc registration so a completed review does not retain its cancelCtx on e.shutdownCtx until process exit." This is correct - if the AfterFunc isn't stopped, the `cctx` would be retained by `e.shutdownCtx`'s children until `e.shutdownCtx` is cancelled. The deferred `cancel()` calls `stop()` which removes the registration. Good.

Let me think about whether there are any actual bugs...

Actually, I want to look at one thing more carefully. In `shutdownReviews`:
```go
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
	if serverShutdown && e.shutdownCancel != nil {
		e.shutdownCancel()
	}
	e.drain(timeout)
}
```

The `serverShutdown` parameter is `ctx.Err() != nil`. But what if `ctx.Err()` returns `context.DeadlineExceeded` instead of `context.Canceled`? The sprint plan mentions SIGINT/SIGTERM, which would cause `context.Canceled`. But a deadline exceeded would also make `ctx.Err() != nil` true. In that case, `serverShutdown` would be true, and the reviews would be cancelled. Is that correct?

Looking at the sprint plan: "a cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the server itself is shutting down." A deadline exceeded would also mean the server is shutting down (the context expired). So treating it the same way seems correct.

But wait - in `review.go:361`, the interrupted marker keys off `errors.Is(ctx.Err(), context.Canceled)`. If the context is cancelled via `shutdownCancel()`, the review context's `ctx.Err()` would be `context.Canceled` (since `context.WithCancel` produces `context.Canceled`). So the interrupted marker would fire. Good.

But what if the root ctx was cancelled due to a deadline? Then `ctx.Err()` would be `context.DeadlineExceeded`, and `serverShutdown` would be true. `shutdownCancel()` would be called, which cancels the `shutdownCtx`. The `AfterFunc` would fire `cancel()` on the review context, making its `ctx.Err()` be `context.Canceled`. So the interrupted marker would still fire correctly. Good.

Hmm, actually I realize there might be an issue. Let me trace through more carefully:

1. Root ctx is cancelled (SIGINT).
2. `s.Run(ctx, ...)` returns.
3. `ctx.Err() != nil` → true → `serverShutdown = true`.
4. `e.shutdownCancel()` is called → cancels `e.shutdownCtx`.
5. `AfterFunc` fires `cancel()` on `cctx` (the review context).
6. `cctx.Err()` → `context.Canceled`.
7. `ExecuteReview` sees `errors.Is(ctx.Err(), context.Canceled)` → true → marks `Interrupted=true`.

This looks correct.

Now for the client disconnect case:
1. Client disconnects (stdio EOF).
2. `s.Run(ctx, ...)` returns with `runErr` (maybe nil or maybe an error).
3. `ctx.Err() != nil` → false → `serverShutdown = false`.
4. `e.shutdownCancel()` is NOT called.
5. `e.drain(timeout)` waits for in-flight reviews.
6. Reviews are NOT cancelled, so they continue running.
7. If a review finishes during the drain, it's marked completed.
8. If it doesn't finish, it's left in_progress (and the process might exit).

This matches AC3. Good.

Let me now think about edge cases and potential issues more carefully...

**Issue 1: `shutdownCancel` called but `shutdownCtx` is nil**

In `shutdownReviews`, the check is `e.shutdownCancel != nil`. But what if someone constructs an engine with `shutdownCancel` set but `shutdownCtx` nil? That shouldn't happen in practice since they're always set together. Not a real issue.

**Issue 2: Double cancel in tests**

In `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`, the test calls `e.shutdownReviews(true, shutdownDrain)` which calls `e.shutdownCancel()`. Then `t.Cleanup` calls `e.shutdownCancel()` again. `context.CancelFunc` is idempotent, so this is fine.

**Issue 3: The `stop()` return value is ignored**

In the returned cancel function `func() { stop(); cancel() }`, the return value of `stop()` is ignored. This is fine because:
- If `stop()` returns true, the AfterFunc was prevented, and `cancel()` cancels the context.
- If `stop()` returns false, the AfterFunc either already ran (context already cancelled) or was already stopped. `cancel()` is a no-op on an already-cancelled context.

No issue here.

**Issue 4: Potential goroutine leak if `AfterFunc` fires concurrently with `stop()`**

According to Go docs, if `stop()` returns false, the function may have already started or completed. If it's already started but not completed, it's running in a goroutine. `cancel()` might be called concurrently by both the AfterFunc and the deferred cancel. Since `cancel()` is safe for concurrent use, this is fine.

**Issue 5: What about the `NewServer` function?**

The comment says "The NewServer path discards the engine and never fires shutdownCancel." If `NewServer` creates an engine via `buildServer`, the engine has `shutdownCtx` and `shutdownCancel` set, but if the engine is discarded, `shutdownCancel` is never called. The `context.WithCancel(context.Background())` creates a context that's never cancelled, but since no one holds a reference to it (the engine is discarded), it'll be garbage collected. Actually wait - `context.WithCancel` doesn't spawn a goroutine for `Background()`, so there's no goroutine leak. The context and cancel func are just garbage collected. Fine.

But if `NewServer` doesn't use `buildServer`, then the engine doesn't have `shutdownCtx`/`shutdownCancel` set, and `withShutdownCancel` returns the context unchanged (nil check). Fine.

**Issue 6: Test reliability**

The test `TestServeShutdown_ClientDisconnectDoesNotInterrupt` uses a 200ms drain. The review is blocked, so after 200ms, the drain returns. The test then reads the status. But what if the fan-out is slow to write the initial in_progress status? Actually, `handleReview` already returned with `runningStatus`, which means the in_progress status was already written before the fan-out started (or at least before handleReview returned). So the status should be on disk. Fine.

**Issue 7: `waitForTerminalStatus` polling**

The test helper polls every 5ms with a 10s timeout. This is fine for a hermetic test.

Let me now think about whether there are any real issues to report...

Actually, I want to look at one more thing. The `Serve` function:

```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

What if `s.Run` panics? Then `shutdownReviews` is never called. But a panic would crash the process anyway, so this isn't a real concern.

What if `s.Run` returns very quickly (e.g., immediate error) before any detached review starts? Then `shutdownReviews` is called with no in-flight reviews, and `drain` returns immediately. Fine.

Let me look at the `handleReview` function more carefully:

```go
rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
defer cancel()
if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
    e.logger().Error("review fan-out finished with errors", "review_id", prep.ID, "error", err)
}
```

The `defer cancel()` is inside the goroutine (the background fan-out goroutine). So `cancel()` is called when `ExecuteReview` returns. This stops the AfterFunc registration. Good.

But wait - what if `ExecuteReview` is still running when the server shuts down? The `shutdownCancel()` fires, which triggers the `AfterFunc`, which calls `cancel()` on `cctx`. This cancels the review context. `ExecuteReview` sees the cancellation and returns (writing the interrupted manifest). Then `defer cancel()` runs, calling `stop()` (which returns false since the AfterFunc already fired) and `cancel()` (which is a no-op since the context is already cancelled). Fine.

I think the implementation is solid. Let me look for any subtle issues...

**Potential issue: `e.shutdownCtx` could be cancelled before a review starts**

If the server is shutting down and a new review request comes in (unlikely but possible), `withShutdownCancel` would register an `AfterFunc` on an already-cancelled `shutdownCtx`. According to Go docs, `AfterFunc` on an already-done context calls the function immediately in a new goroutine. So `cancel()` would be called immediately, cancelling the review context before `ExecuteReview` even starts. This would cause the review to be immediately interrupted. This seems like correct behavior - if the server is shutting down, new reviews should be interrupted.

Actually, this is a subtle race. The `AfterFunc` fires `cancel()` in a new goroutine. So there's a brief window where `ExecuteReview` starts with a not-yet-cancelled context, but then it gets cancelled almost immediately. This is fine - `ExecuteReview` should handle context cancellation gracefully.

**Potential issue: What if `shutdownCtx` is cancelled while `withShutdownCancel` is executing?**

The `context.AfterFunc` call is atomic with respect to the context's done channel. If the context is done, the function is called immediately. If not, it's registered. There's no race here. Fine.

Let me look at the test file one more time for any issues...

**Test: `startInFlightReview`**

```go
t.Cleanup(func() {
    if e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(time.Second)
})
```

This cleanup is registered after the review starts. It cancels the shutdown context and drains. This ensures the blocked review goroutine is unblocked at test end. Good.

But in `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`, the test calls `e.shutdownReviews(true, shutdownDrain)` which already cancels and drains. Then the cleanup does it again. The second cancel is a no-op, and the second drain should return quickly since the review already finished. Fine.

In `TestServeShutdown_ClientDisconnectDoesNotInterrupt`, the test calls `e.shutdownReviews(false, 200*time.Millisecond)` which does NOT cancel. The review is still blocked. Then the cleanup calls `e.shutdownCancel()` and `e.drain(time.Second)`. This cancels the review, which unblocks the completer, and the fan-out writes the interrupted manifest. The drain waits for it. Fine.

Hmm, but there's a subtle issue. In `TestServeShutdown_ClientDisconnectDoesNotInterrupt`, after `e.shutdownReviews(false, 200*time.Millisecond)`, the test reads the status and asserts it's `RunInProgress`. But the review is still running (blocked). Then the test function returns, and `t.Cleanup` fires. The cleanup cancels the review and drains. This is fine for test cleanup, but the test's assertion happens before the cleanup. So the assertion should be correct.

Wait, actually I want to check one more thing. The `blockingCompleter.Complete` blocks on `<-ctx.Done()`. When is `ctx` here? It's the context passed to `Complete` by `ExecuteReview`. This should be the review context (`rctx`) or a child of it. When `rctx` is cancelled (via shutdown), `ctx.Done()` fires, and `Complete` returns. Then `ExecuteReview` writes the interrupted manifest. This all happens within the drain window. Fine.

I think the implementation is correct and well-tested. Let me see if there are any maintainability or style issues...

**Maintainability: The `serverShutdown` parameter name**

In `shutdownReviews(serverShutdown bool, timeout time.Duration)`, the `serverShutdown` parameter is a boolean. This could be an enum or a typed value for clarity, but it's a minor style issue. Not worth flagging.

**Maintainability: Comment quality**

The comments are very detailed and explain the reasoning well. This is good for maintainability.

**Correctness: `ctx.Err() != nil` as the shutdown signal**

This is the key design decision. The sprint plan says "a cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the server itself is shutting down." Using `ctx.Err() != nil` captures this. But it also captures `context.DeadlineExceeded`. Is that correct? If the root context has a deadline and it expires, the server is shutting down, so cancelling reviews is correct. Fine.

Let me think about one more potential issue...

**Potential issue: `shutdownReviews` is called even when `buildServer` fails**

Looking at the `Serve` function, if `buildServer` fails, it returns early before `s.Run`. So `shutdownReviews` is never called. But if `buildServer` fails, there's no engine, so no reviews to shut down. Fine.

Actually wait, I need to look at the full `Serve` function. The diff only shows part of it. Let me look at what's visible:

```go
func Serve(ctx context.Context, root string, completer fanout.Completer, logger ...) {
    ...
    return err  // early return if buildServer fails
    ...
    runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
    e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
    if runErr != nil {
        return fmt.Errorf("serve stdio: %w", runErr)
    }
    return nil
}
```

So `shutdownReviews` is only called after `s.Run` returns. If `buildServer` fails, we return early. Fine.

OK, I've done a thorough review. Let me think about what issues to report...

Actually, I want to reconsider one thing. The `context.AfterFunc` function was introduced in Go 1.21. If the project uses an older Go version, this would be a compilation error. But since the code compiles (it's in the diff), this is fine.

Let me also check: is there a potential issue with the `stop()` function and the `cancel()` function being called in the wrong order?

```go
return cctx, func() { stop(); cancel() }
```

If `stop()` returns true (AfterFunc was prevented), then `cancel()` cancels the context. This is correct.
If `stop()` returns false (AfterFunc already fired or already stopped), then `cancel()` is a no-op. This is correct.

The order is important: `stop()` must be called before `cancel()`. If `cancel()` were called first, it would cancel the context, but the AfterFunc might still fire later (when `shutdownCtx` is cancelled), calling `cancel()` again (which is a no-op). But `stop()` would then return false (since the AfterFunc might have already been triggered). Actually, the order doesn't matter for correctness, but calling `stop()` first is cleaner because it prevents the AfterFunc from firing unnecessarily. The current order is correct.

I'm struggling to find real issues. Let me look one more time...

Actually, I want to think about the `NewServer` path more carefully. The comment says:

> nil when the engine is built outside buildServer (NewServer discards its engine; tests may construct &engine{} directly).

If `NewServer` creates an engine via `buildServer`, then `shutdownCtx` and `shutdownCancel` are set. But `NewServer` discards the engine, so `shutdownCancel` is never called. The `context.WithCancel(context.Background())` context is never cancelled. But since no one holds a reference to the engine (it's discarded), the context and cancel func are garbage collected. `context.WithCancel(context.Background())` doesn't spawn a goroutine (only `context.WithDeadline` does), so there's no goroutine leak. Fine.

But wait - if `NewServer` uses `buildServer`, then the engine has `shutdownCtx` set. And `handleReview` uses `withShutdownCancel`, which registers an `AfterFunc` on `shutdownCtx`. If the engine is discarded but the server is still running (via the SDK server returned by `NewServer`), then the `AfterFunc` registrations are never cleaned up (since `shutdownCancel` is never called, and the reviews' `cancel()` functions are deferred in the goroutines). Actually, the reviews' `cancel()` functions ARE called when `ExecuteReview` returns (via `defer cancel()`). So the `AfterFunc` registrations are cleaned up when each review finishes. Fine.

But if `NewServer` doesn't use `buildServer`, then the engine doesn't have `shutdownCtx` set, and `withShutdownCancel` returns the context unchanged. Fine.

I don't know which case applies since `NewServer` isn't in the diff. But either way, the code handles it.

Let me look at one more potential issue...

**Potential issue: The `shutdownCtx` is derived from `context.Background()`, not from the root ctx**

In `buildServer`:
```go
shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
```

This means `shutdownCtx` is NOT a child of the root ctx passed to `Serve`. So if the root ctx is cancelled, `shutdownCtx` is NOT automatically cancelled. `shutdownCancel` must be called explicitly. This is by design - the sprint plan says "Derive the interrupt signal from server lifecycle only, not the request context." The `shutdownCancel` is called in `shutdownReviews` when `serverShutdown` is true (i.e., when `ctx.Err() != nil`). So the root ctx cancellation triggers `shutdownCancel` via the `serverShutdown` check. Fine.

But what if `Serve` returns early (e.g., `buildServer` fails) and `shutdownCancel` is never called? Then `shutdownCtx` is never cancelled. But if `buildServer` fails, there's no engine, so no reviews. And the `shutdownCtx` is garbage collected with the engine. Fine.

OK, I'm now fairly confident the implementation is correct. Let me see if there are any minor issues worth flagging...

**Minor issue: `shutdownDrain` constant**

The test uses `shutdownDrain` (a constant) in one test and `200*time.Millisecond` in another. This is fine - the tests have different needs.

**Minor issue: Test `TestServeShutdown_NormalCompletionNotInterrupted` doesn't use `startInFlightReview`**

This test uses `fakeCompleter` instead of `blockingCompleter`, so it doesn't need the `startInFlightReview` helper. Fine.

**Minor issue: `t.Setenv("ATCR_TEST_KEY", "secret")` in tests**

This sets an environment variable for testing. It's automatically cleaned up by `t.Setenv`. Fine.

Let me think about whether there are any security issues...

**Security: No user input is passed to any dangerous operations.** The review context is derived from internal contexts. No SQL, no shell, no HTML. Fine.

**Security: The `ATCR_TEST_KEY` environment variable** is only set in tests. Fine.

OK, I think I've been very thorough. Let me consider whether there are any issues at all...

Actually, let me reconsider the `shutdownReviews` function one more time:

```go
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
	if serverShutdown && e.shutdownCancel != nil {
		e.shutdownCancel()
	}
	e.drain(timeout)
}
```

There's a subtle issue here. `e.shutdownCancel()` cancels `e.shutdownCtx`. This triggers all `AfterFunc` registrations, which call `cancel()` on each review's `cctx`. But the `cancel()` calls happen in separate goroutines (launched by `AfterFunc`). So there's a race between the `AfterFunc` goroutines calling `cancel()` and `e.drain(timeout)` waiting for the reviews to finish.

Is this a problem? Let's trace through:
1. `e.shutdownCancel()` is called.
2. `e.shutdownCtx` is cancelled.
3. `AfterFunc` launches goroutines that call `cancel()` on each review's `cctx`.
4. `e.drain(timeout)` starts waiting.
5. The review goroutines see `ctx.Done()` and return.
6. The `bg` WaitGroup is decremented.
7. `e.drain(timeout)` returns when all reviews are done or timeout expires.

The race between step 3 and step 4 is fine - the `AfterFunc` goroutines call `cancel()`, which is just setting a flag on the context. The review goroutines check `ctx.Done()` in their own time. No issue.

But what if the `AfterFunc` goroutine is slow to start? The `drain` might time out before the `cancel()` is called. This is unlikely (goroutine scheduling is fast), but theoretically possible. In practice, this is not an issue because the `AfterFunc` callback is called "immediately" (in Go runtime terms) when the context is cancelled.

Actually, I just realized there might be a more subtle issue. The `AfterFunc` documentation says:

> AfterFunc arranges to call f in its own goroutine after ctx is done (cancelled or timed out). If ctx is already done, AfterFunc calls f immediately.

So the `cancel()` call happens in a separate goroutine. There's no guarantee that `cancel()` is called before `e.drain(timeout)` starts. But that's fine - `cancel()` just sets the context's done channel, and the review goroutine will see it whenever it next checks `ctx.Done()`.

The only concern would be if `e.drain(timeout)` times out before the review goroutine sees the cancellation. But that's a timeout issue, not a race condition. The `shutdownDrain` is 5 seconds, which should be plenty of time for the `cancel()` to be called and the review to flush its manifest.

OK, I'm now very confident the implementation is correct. Let me see if there are any truly minor issues worth flagging...

Actually, I just thought of something. In the test `TestServeShutdown_ClientDisconnectDoesNotInterrupt`:

```go
e.shutdownReviews(false, 200*time.Millisecond)

st, err := fanout.ReadReviewStatus(dir, id)
require.NoError(t, err)
assert.NotEqual(t, fanout.RunInterrupted, st.Status, ...)
assert.Equal(t, fanout.RunInProgress, st.Status, ...)
```

After `shutdownReviews(false, 200ms)`, the review is still blocked (not cancelled). The drain waits 200ms and returns. The review is still in_progress. The test reads the status and asserts it's in_progress. This is correct.

But then the test function returns, and `t.Cleanup` fires:
```go
t.Cleanup(func() {
    if e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(time.Second)
})
```

This cancels the review and drains for 1 second. The review should finish (write interrupted manifest) within 1 second. If it doesn't, the drain returns and the review goroutine is leaked. But since the completer blocks on `ctx.Done()`, and `shutdownCancel()` cancels the context, the completer should return immediately. Then `ExecuteReview` writes the manifest and returns. The drain should complete well within 1 second. Fine.

But wait - there's a subtlety. The `t.Cleanup` is registered inside `startInFlightReview`, which is called at the beginning of the test. Go's `t.Cleanup` functions are executed in LIFO order (last registered, first executed). So if the test registers additional cleanups after `startInFlightReview`, they would execute before the `startInFlightReview` cleanup. But in this test, there are no additional cleanups. Fine.

OK let me also check: is there a potential issue with the `t.TempDir()` cleanup? The `gitRepo(t)` and `writeReviewConfig(t, root)` helpers likely use `t.TempDir()`, which is cleaned up automatically. The `startInFlightReview` cleanup runs before `t.TempDir()` cleanup (since `t.Cleanup` is LIFO and `t.TempDir()` is registered earlier). Wait, actually `t.TempDir()` registers its cleanup when it's first called, which is inside `gitRepo(t)` or `writeReviewConfig(t, root)`, which is called before the `t.Cleanup` in `startInFlightReview`. So the `startInFlightReview` cleanup runs FIRST (LIFO), then `t.TempDir()` cleanup. This means the review goroutine is unblocked before the temp dir is deleted. Good - this prevents a race where the review tries to write to a deleted directory.

Actually wait, the comment in the code says:
```go
// Unblock the parked fan-out at test end (before t.TempDir cleanup, which is
// registered earlier and so runs after this) so a test that does NOT trigger a
// server shutdown does not leak the blocked review goroutine.
```

This confirms my analysis. The cleanup ordering is correct.

Alright, I've been extremely thorough. Let me now consider whether there are any issues worth reporting.

After this exhaustive review, I believe the implementation is correct and well-tested. The code handles edge cases properly, the comments are detailed and accurate, and the tests cover the acceptance criteria.

Let me look one final time for any subtle issues...

One thing I notice: in `withShutdownCancel`, when `e.shutdownCtx` is nil, the function returns `ctx, func() {}`. The returned cancel function is a no-op. But the caller still does `defer cancel()`. This is fine - it's a no-op.

Another thing: the `context.AfterFunc` function was added in Go 1.21. If the project's go.mod specifies an older version, this would fail to compile. But since the code is in the diff and presumably compiles, this is fine.

Let me think about one more thing: the `shutdownCtx` is created with `context.WithCancel(context.Background())`. This means it's a child of `Background`, not of the root ctx. So it's not automatically cancelled when the root ctx is cancelled. This is by design. But it also means that if `Serve` is called multiple times (unlikely but possible), each call creates a new `shutdownCtx`. The old one is never cancelled. But since the old engine is discarded, the old `shutdownCtx`