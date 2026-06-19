The user wants me to review a diff against a specific sprint plan. I need to:

1. Focus only on files/areas directly related to the sprint plan
2. The sprint plan is about Epic 4.1.2: Mark Serve-Mode Detached Reviews Interrupted on Shutdown
3. In scope: server-shutdown signal reaching detached reviews, detached reviews receiving interrupted marker on server shutdown
4. Files mentioned: internal/mcp/handlers.go, internal/fanout/review.go (reference), internal/mcp/server.go (likely), and a new test file internal/mcp/shutdown_test.go

Let me analyze the diff carefully.

Files changed:
- internal/mcp/handlers.go
- internal/mcp/server.go
- internal/mcp/shutdown_test.go (new)

Let me look for issues in each file.

**internal/mcp/handlers.go:**

1. `withShutdownCancel` function:
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
Issue: If `cancel()` is called before the `AfterFunc` fires, calling `stop()` is safe (it returns false and unregisters). But what about if `cancel()` is called, then later `shutdownCtx` cancels? The `AfterFunc` might still fire if not stopped. Actually, `stop()` stops the `AfterFunc` from firing. But wait: if the returned cancel is called, it calls `stop()` then `cancel()`. If `shutdownCtx` has already fired and triggered `cancel()` via AfterFunc, then calling the returned func will call `stop()` (returns false, no-op) and `cancel()` again (safe, no-op on already cancelled context). That seems fine.

Wait, looking closer at `handleReview`:
```go
rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
defer cancel()
if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
```
This is inside a goroutine:
```go
go func() {
    defer e.bg.Done()
    defer func() { ... }()
    ...
    rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
    defer cancel()
    if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
        ...
    }
}()
```

Wait, `rctx` is derived from `e.reviewContext(ctx, prep.ID)` which uses `context.WithoutCancel(ctx)`. The `ctx` here is the request context. `withShutdownCancel` wraps that with `context.WithCancel`. The returned cancel is deferred.

Is there a race? The `defer cancel()` will run when `ExecuteReview` returns (or panics). That stops the AfterFunc and cancels the context. But `ExecuteReview` returning means the review is done. The cancel is harmless.

But wait: what if `e.shutdownCtx` is nil? The function returns `ctx, func() {}`. So the deferred cancel is a no-op. That's fine.

One potential issue: in `withShutdownCancel`, when `e.shutdownCtx` is not nil, it creates `cctx, cancel := context.WithCancel(ctx)`. But `ctx` here is already a detached context from `reviewContext`. That's fine.

Wait, is there an issue with `handleReview`? Let me check the function signature. It's not fully shown in the diff. But the diff shows:
```go
func (e *engine) handleReview(ctx context.Context, _ *mcpsdk.CallToolRequest, in...
```
The `ctx` parameter is the request context.

Potential issue: `withShutdownCancel` uses `context.AfterFunc`. `context.AfterFunc` was added in Go 1.21. Is that okay? Probably, assuming the project uses a recent Go version. Not an issue to flag unless I know otherwise.

Potential issue: In `handleReview`, the goroutine does `defer cancel()`. But what if the engine is shutting down and `shutdownCancel` is called? The `AfterFunc` will call `cancel()`. Then when `ExecuteReview` returns (because it noticed context cancellation), the deferred `cancel()` runs again. `cancel()` on an already cancelled context is safe.

Actually, wait. Is there a bug in `withShutdownCancel`?
```go
return cctx, func() { stop(); cancel() }
```
If `shutdownCtx` fires, `AfterFunc` calls `cancel()`. Then the caller's deferred func calls `stop()` then `cancel()` again. This is fine.

But what if the caller's `cancel()` is called BEFORE `shutdownCtx` fires? Then `stop()` is called, which unregisters the AfterFunc. Then `cancel()` cancels the context. That's fine.

What if `e.shutdownCtx == nil`? Returns `func() {}`. Deferred `cancel()` does nothing.

Is there any issue with the engine fields?
```go
shutdownCtx    context.Context
shutdownCancel context.CancelFunc
```
No initialization in `engine` literal outside `buildServer`. Handled by nil check.

Wait, looking at `buildServer` in server.go:
```go
shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
e := &engine{root: root, completer: completer, log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}
```
The context is never cancelled if `Serve` exits via `runErr` without `serverShutdown` being true? Let's look at `Serve`:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```
If `ctx` is not cancelled (client disconnect), `serverShutdown` is false, so `shutdownCancel` is NOT called. Then `e.drain` runs. But `shutdownCtx` remains uncancelled. The engine might be discarded after Serve returns. The context and cancel func would be garbage collected eventually since nothing references them anymore? Actually, `context.WithCancel` returns a context that is referenced by child contexts. But if no child contexts exist anymore, and `e` goes out of scope, it can be GC'd. This seems fine.

But wait: what about `shutdownCancel` being called in tests? `startInFlightReview` calls `e.shutdownCancel()` in `t.Cleanup`. That's fine.

Let's look at `internal/mcp/server.go` more carefully.

```go
func Serve(ctx context.Context, root string, completer fanout.Completer, logger *slog.Logger) error {
	...
	runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
	e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
	if runErr != nil {
		return fmt.Errorf("serve stdio: %w", runErr)
	}
	return nil
}
```

Wait, `s.Run` returns an error and also the context might be cancelled. The code checks `ctx.Err() != nil` to determine if it was a server shutdown. But what if `s.Run` returns because the transport closed (client disconnect) but `ctx` is also cancelled simultaneously? Or what if `runErr` is non-nil because of a transport error, but `ctx` is still valid?

The logic says: `serverShutdown` is true only when `ctx.Err() != nil`. This means if `s.Run` returns due to a transport error but the context is still valid, `serverShutdown` is false. That seems correct - it's not a signal-induced shutdown.

But wait: what if `ctx` is cancelled AFTER `s.Run` returns but BEFORE `e.shutdownReviews` is called? `ctx.Err()` would be non-nil, and it would trigger shutdown logic even if `s.Run` returned for a clean disconnect. This is a race condition. If the signal handler cancels `ctx` right after `s.Run` returns but before `shutdownReviews`, it would be treated as a server shutdown.

Actually, looking at typical stdio transport: when the client disconnects, stdin reaches EOF, `s.Run` returns. At that point, the context passed to `Serve` is not cancelled. But if a SIGINT arrives concurrently, `ctx` gets cancelled. The race is:
1. `s.Run` returns due to EOF (clean disconnect).
2. Before `e.shutdownReviews` runs, SIGINT arrives and cancels `ctx`.
3. `ctx.Err() != nil` is true, so `serverShutdown` is true.
4. `e.shutdownCancel()` is called, marking in-flight reviews as interrupted.
5. But it was actually a clean disconnect that happened just before the signal.

Is this a realistic issue? In `Serve`, the context is the root context. If SIGINT arrives, `s.Run` would likely also return because the context is cancelled. But there could be a race where `s.Run` returns for EOF, then SIGINT arrives immediately after. This is probably a minor correctness issue, but given the scope and the fact that the comment explicitly distinguishes the two cases, it might be worth noting.

Actually, looking more carefully: `s.Run(ctx, ...)` - if `ctx` is cancelled, `s.Run` should return. So if SIGINT arrives, `s.Run` returns because `ctx` is done. Then `shutdownReviews(true, ...)` runs. If it's a clean disconnect, `s.Run` returns with `ctx` still valid. The race window between `s.Run` returning and `shutdownReviews` checking `ctx.Err()` is extremely small. But a signal could arrive in that window.

However, is there a better way? Perhaps check if `runErr` indicates cancellation? The diff doesn't show `mcpsdk.StdioTransport` behavior. I think this might be a valid concern but perhaps too subtle for a code review unless I'm confident.

Actually, looking at the test `TestServeShutdown_ClientDisconnectDoesNotInterrupt`: it calls `e.shutdownReviews(false, 200*time.Millisecond)`. In production, `Serve` does `e.shutdownReviews(ctx.Err() != nil, shutdownDrain)`. The issue is that in production, `ctx.Err()` might become non-nil after `s.Run` returns but before the check. But this is likely acceptable.

Wait, I see another issue. In `buildServer`:
```go
shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
e := &engine{root: root, completer: completer, log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}
```
The comment says: "The NewServer path discards the engine and never fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so the unfired cancel is reclaimed with the engine."

Actually, `context.WithCancel` DOES create a goroutine internally in Go's context package? Wait, no. As of recent Go versions, `context.WithCancel` does not necessarily spawn a goroutine. The background context is a static global. `WithCancel` creates a `cancelCtx` struct. No goroutine is spawned. The comment is correct.

But there's a potential leak: if `shutdownCancel` is never called, and child contexts are created via `withShutdownCancel`, those child contexts register `AfterFunc`s on `shutdownCtx`. The `AfterFunc` registrations remain until `stop()` is called or `shutdownCtx` is cancelled. If `shutdownCtx` is never cancelled (e.g., `NewServer` path or tests), and the returned `cancel()` from `withShutdownCancel` is called, then `stop()` unregisters the `AfterFunc`. So no leak.

But what if a caller creates a child context with `withShutdownCancel` and never calls the returned cancel? Then the `AfterFunc` remains registered until the process exits. In `handleReview`, the `cancel()` is deferred, so it's always called when the goroutine exits. Good.

Is there any issue with `shutdown_test.go`?

Looking at `startInFlightReview`:
```go
t.Setenv("ATCR_TEST_KEY", "secret")
```
This is fine.

```go
_, e, err := buildServer(root, bc, nil)
```
`buildServer` can return an error.

```go
_, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
```
Wait, `handleReview` returns what? Looking at the signature: `func (e *engine) handleReview(ctx context.Context, _ *mcpsdk.CallToolRequest, in...)` - the third parameter isn't fully shown but from usage it's `ReviewArgs`. The return values seem to be `(*someToolResult, *ReviewResult, error)`? The test does `_, res, err := e.handleReview(...)`. Then `res.Status`, `res.ReviewPath`, `res.ReviewID`. That seems consistent.

```go
t.Cleanup(func() {
    if e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(time.Second)
})
```
This cleanup runs after the test body but before `t.TempDir` cleanup (because cleanup funcs are LIFO, and `t.TempDir` registers its cleanup at the start). The comment says: "before t.TempDir cleanup, which is registered earlier and so runs after this". Actually, `t.TempDir()` registers a cleanup function when called. If `startInFlightReview` is called after `t.TempDir()` (via `gitRepo(t)` perhaps?), then `t.TempDir()` cleanup was registered before `t.Cleanup` in `startInFlightReview`. Wait, `t.Helper()` doesn't matter. `gitRepo(t)` probably calls `t.TempDir()`. So `t.TempDir()` cleanup is registered first. Then `t.Cleanup` in `startInFlightReview` is registered. Since cleanup is LIFO, `startInFlightReview`'s cleanup runs FIRST, then `t.TempDir()` cleanup. So the comment is correct? Wait: "before t.TempDir cleanup, which is registered earlier and so runs after this". Yes, earlier registered cleanup runs later (LIFO). So `e.shutdownCancel()` and `e.drain` run before the temp dir is removed. That's correct.

But wait: `startInFlightReview` has a deferred `t.Cleanup`. The `t.Cleanup` func calls `e.shutdownCancel()` if not nil. But in `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`, after calling `e.shutdownReviews(true, shutdownDrain)`, the review should have finished. But the cleanup will call `e.shutdownCancel()` again and `e.drain(time.Second)`. That should be harmless since the review is already done.

Wait, there's a potential issue in `TestServeShutdown_ClientDisconnectDoesNotInterrupt`:
```go
e.shutdownReviews(false, 200*time.Millisecond)

st, err := fanout.ReadReviewStatus(dir, id)
require.NoError(t, err)
assert.NotEqual(t, fanout.RunInterrupted, st.Status, ...)
assert.Equal(t, fanout.RunInProgress, st.Status, ...)
```
But `startInFlightReview`'s `t.Cleanup` calls `e.shutdownCancel()` which WILL cancel the review context! But wait, `t.Cleanup` runs AFTER the test function returns. The test function asserts status is `RunInProgress` at the end. Then the test function returns. Then `t.Cleanup` runs, cancelling the context. But by then, the assertions have already passed. However, after cleanup, the temp dir is deleted. The status on disk is in the temp dir. So reading it is fine during the test. But wait, `t.Cleanup` runs `e.shutdownCancel()`, which cancels the in-flight review, and then `e.drain`. But since the temp dir gets deleted AFTER the cleanup, the fan-out might write an interrupted status to the now-deleted temp dir? Or does it? Actually, the files were opened in the temp dir. On Unix, if the directory is removed but files are open, writes might still succeed if the files are referenced by FD. But `fanout.ReadReviewStatus` reads from path. After `t.Cleanup` and temp dir cleanup, no one cares.

But there's a bigger issue: `TestServeShutdown_ClientDisconnectDoesNotInterrupt` asserts the status is `RunInProgress` AFTER the drain. But `startInFlightReview` returns an engine with an in-flight review. The review is blocked inside `blockingCompleter.Complete` waiting for `<-ctx.Done()`. If we don't cancel the context, the review stays blocked. Then `e.shutdownReviews(false, 200*time.Millisecond)` just drains for 200ms. The review is still blocked. Status is `RunInProgress`. Then the test asserts that.

After the test returns, `t.Cleanup` runs, calls `e.shutdownCancel()`, which cancels the review context via `AfterFunc`. The review unblocks, returns `ctx.Err()`, and `ExecuteReview` writes the status. But since the test is over, no one checks it. However, if `fanout.ExecuteReview` writes to disk AFTER the temp dir is removed (race between cleanup funcs?), there might be an error. The cleanup order is:
1. `startInFlightReview` cleanup: `e.shutdownCancel(); e.drain(time.Second)`
2. `gitRepo` or `t.TempDir` cleanup: remove temp dir

Actually, `t.Cleanup` functions are LIFO. `startInFlightReview` calls `t.Cleanup` after `gitRepo(t)` was called (assuming `gitRepo(t)` calls `t.TempDir`). So cleanup 1 is `startInFlightReview`'s cleanup. Cleanup 2 is temp dir removal. So first, the review is cancelled and drained. Then the temp dir is removed. The drain waits for the fan-out to finish. Since `blockingCompleter` returns error when ctx is done, `ExecuteReview` should finish quickly and write to disk. Then the temp dir is removed. This seems fine.

Wait, but in `TestServeShutdown_ClientDisconnectDoesNotInterrupt`, the test ends with the review still in progress. The cleanup will cancel it and drain it. But during the test body, nothing cancels it. So the `assert.Equal(t, fanout.RunInProgress, st.Status, ...)` should pass. Good.

What about `TestServeShutdown_NormalCompletionNotInterrupted`?
```go
st := waitForTerminalStatus(t, res.ReviewPath, res.ReviewID)
require.Equal(t, fanout.RunCompleted, st.Status, ...)
if e.shutdownCancel != nil {
    e.shutdownCancel()
}
e.drain(shutdownDrain)

st, err = fanout.ReadReviewStatus(res.ReviewPath, res.ReviewID)
require.NoError(t, err)
assert.Equal(t, fanout.RunCompleted, st.Status, ...)
```
This looks correct.

Now let me look for actual bugs.

In `internal/mcp/handlers.go`, `withShutdownCancel`:
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

Is there a goroutine leak? `context.AfterFunc` was added in Go 1.21. If `e.shutdownCtx` is never cancelled, and `stop` is never called, the `AfterFunc` remains in a list. But `stop` IS called by the returned cancel func, which is deferred in `handleReview`. So as long as `cancel` is called, `stop` unregisters it.

But what about the case where `e.shutdownCtx` gets cancelled? Then `AfterFunc` fires, calling `cancel()`. The deferred func in `handleReview` later calls `stop()` (which returns false, since it already fired) and `cancel()` again (no-op). No leak.

Wait, actually: if `AfterFunc` fires, does `stop()` still need to be called? The documentation for `context.AfterFunc` says: "The returned stop function stops the execution of f. If f has already started, stop returns false. After calling stop, the AfterFunc cannot be started again by a later cancellation of ctx." So calling `stop()` after `AfterFunc` fired is fine and unregisters nothing extra? Actually, once fired, it's done. `stop()` just returns false.

So the code is correct.

But wait! There's a subtle issue with `context.AfterFunc`. If `e.shutdownCtx` is already cancelled when `withShutdownCancel` is called, `AfterFunc` invokes `cancel` immediately in its own goroutine? Actually, the docs say: "If ctx is already done, f is called immediately." And: "The caller needs to be careful that f does not depend on any state that could have changed since the call to AfterFunc, especially if ctx is already done." It calls `f` immediately, but in what goroutine? The docs say it runs in its own goroutine if ctx is already done? No: "If ctx is already done, f is called immediately." It might call it synchronously. Let's check. Actually, the Go docs say: "AfterFunc arranges to call f in its own goroutine after ctx is done." Wait, is that true? Let me recall. In Go 1.21, `context.AfterFunc` docs: "AfterFunc arranges to call f in its own goroutine after ctx is done (cancelled or timed out). If ctx is already done, f is called immediately in its own goroutine." So yes, it runs in a separate goroutine.

If `shutdownCtx` is already cancelled, `cancel()` is called in a new goroutine. Then `withShutdownCancel` returns `cctx` which is already cancelled (or will be very soon). The caller (`handleReview`) passes it to `ExecuteReview`. `ExecuteReview` sees `ctx.Err() != nil` and should handle it. But wait, the review was just started. Is it okay to start a review with an already cancelled context? If the server is already shutting down when a new review request comes in, but `handleReview` spawns it anyway. But `handleReview` is called during request handling. If the server is shutting down, `s.Run` has already returned, so no new requests should be handled. Actually, `Serve` does `runErr := s.Run(...)` which blocks. When it returns, it calls `shutdownReviews`. So `handleReview` is only called during `s.Run`. During `s.Run`, if `shutdownCtx` is cancelled (which only happens after `s.Run` returns), `shutdownCtx` is not cancelled. So `shutdownCtx` is never cancelled during `s.Run`. Therefore `shutdownCtx` is never cancelled when `withShutdownCancel` is called in production. In tests, it might be, but that's test code.

Wait, but what about `TestServeShutdown_MarksInFlightDetachedReviewInterrupted`? It calls `e.shutdownReviews(true, ...)` which calls `e.shutdownCancel()`. But before that, the review is in flight and `shutdownCtx` is not cancelled. So `AfterFunc` is registered but not fired. Then `shutdownCancel` is called. The `AfterFunc` fires. Good.

So no issue there.

Let me look at `internal/mcp/server.go` again.

```go
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
	if serverShutdown && e.shutdownCancel != nil {
		e.shutdownCancel()
	}
	e.drain(timeout)
}
```

What if `serverShutdown` is true but `e.shutdownCancel` is nil? This can happen if the engine was built outside `buildServer`. Then `e.drain(timeout)` is called. That's fine.

But wait: in `Serve`, after `s.Run` returns, if `ctx.Err() != nil` (server shutdown), `e.shutdownReviews(true, shutdownDrain)` is called. `e.shutdownCancel()` cancels `shutdownCtx`. This triggers all `AfterFunc`s for in-flight detached reviews. Those reviews will see `ctx.Err() != nil` in `ExecuteReview` and mark interrupted. Then `e.drain` waits for them to finish. This matches AC1 and AC3.

What if `e.shutdownCancel` is called multiple times? `context.CancelFunc` can be called multiple times; subsequent calls are no-ops. So if `shutdownReviews` is called multiple times, it's safe.

Wait, but `TestServeShutdown_NormalCompletionNotInterrupted` calls `e.shutdownCancel()` directly after the review is completed. That's fine.

Let me check for any issues with the test file.

`TestServeShutdown_MarksInFlightDetachedReviewInterrupted`:
```go
e, dir, id := startInFlightReview(t)
e.shutdownReviews(true, shutdownDrain)
```
`startInFlightReview` returns `e` which has `shutdownCtx` and `shutdownCancel` set. The test calls `e.shutdownReviews(true, ...)`. This cancels `shutdownCtx`. But wait, `startInFlightReview` also registered a `t.Cleanup` that calls `e.shutdownCancel()` if not nil. Since `e.shutdownCancel` is not nil, the cleanup will call it again. But as noted, calling a `CancelFunc` multiple times is safe.

However, there is a potential issue: `startInFlightReview` cleanup does `e.drain(time.Second)`. But `e.shutdownReviews` already drained it. Doing it again is harmless.

Wait, in `TestServeShutdown_ClientDisconnectDoesNotInterrupt`:
```go
e.shutdownReviews(false, 200*time.Millisecond)
```
This does NOT call `e.shutdownCancel()`. So the review remains in-flight. The test asserts `RunInProgress`. Then the test returns. Then `t.Cleanup` calls `e.shutdownCancel()` and `e.drain`. The review gets cancelled and finishes. That's fine.

But wait: `blockingCompleter` blocks until `ctx.Done()`. If `ctx` is never cancelled during the test body, the goroutine stays blocked. The test body asserts status is `RunInProgress`, which is correct. After the test body, cleanup cancels it. Fine.

Now let me look for issues that might be flagged.

One possible issue: In `Serve`, `runErr` is checked AFTER `e.shutdownReviews`. But if `runErr` is non-nil, `Serve` still returns the error. But `e.shutdownReviews` was already called. That's probably fine.

Another possible issue: `withShutdownCancel` comment says: "The returned cancel MUST be deferred by the caller for the lifetime of the review". The caller in `handleReview` does defer it. Good.

What about `e.bg`? `handleReview` increments `e.bg` (presumably before the diff snippet). The goroutine does `defer e.bg.Done()`. That's fine.

Wait, I see something in `startInFlightReview` in the test:
```go
// Unblock the parked fan-out at test end (before t.TempDir cleanup, which is
// registered earlier and so runs after this) so a test that does NOT trigger a
// server shutdown does not leak the blocked review goroutine.
t.Cleanup(func() {
    if e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(time.Second)
})
```
But `e.drain` waits for `e.bg` to reach zero. The goroutine does `defer e.bg.Done()`. If the context is cancelled, `ExecuteReview` returns, the goroutine finishes, `e.bg.Done()` is called. So `e.drain` returns. Good.

But what if `e.shutdownCancel` is nil? Then the cleanup does nothing and `e.drain(time.Second)` waits. But if the review is still blocked (because `blockingCompleter` waits for `ctx.Done()` and `ctx` is never cancelled), `e.drain` will time out after 1 second. The test will then proceed to temp dir cleanup. But the goroutine is still running, potentially holding file descriptors or writing to the temp dir while it's being cleaned up. However, `e.shutdownCancel` is set by `buildServer`, which is called in `startInFlightReview`. So it's not nil. The nil check is defensive.

Wait, in `TestWithShutdownCancel_NilShutdownCtxIsNoop`, `e := &engine{}`. Then `rctx, cancel := e.withShutdownCancel(context.Background())`. Then `defer cancel()`. That's fine.

Is there any issue with data races?

In `handleReview`, the goroutine uses `prep.ID`. `prep` is presumably a local variable or parameter. Captured by the closure. Fine.

Let me look at the diff for `server.go` again.
```go
-	e.drain(shutdownDrain)
+	e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```
This changes behavior: previously it always drained. Now it conditionally cancels first. The comment explains why.

But what if `ctx.Err() != nil` is true because the context was cancelled, but it was cancelled by something other than SIGINT? For example, a request handler cancelling the request context? No, `Serve` receives the root context. It should only be cancelled by the signal handler or by the caller. The comment says "cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler)". So it's fine.

Wait, I found something!

In `internal/mcp/server.go`, `Serve` function:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

But `ctx.Err()` is checked immediately after `s.Run` returns. However, `s.Run` might have returned because the transport closed (client disconnect), and then a signal arrives between `s.Run` returning and the check. Or conversely, `s.Run` returned because `ctx` was cancelled, but then `ctx.Err()` might be nil if the error was due to something else? No, if `ctx` is cancelled, `ctx.Err()` is non-nil and stays non-nil.

Actually, `ctx.Err()` is idempotent and once non-nil, stays non-nil. So if `s.Run` returned because `ctx` was cancelled, `ctx.Err()` will definitely be non-nil at the check. If `s.Run` returned for a clean disconnect, `ctx.Err()` should be nil. The only race is if a cancellation happens concurrently after `s.Run` returns but before the check. In that case, the server is shutting down anyway, so treating it as a shutdown is arguably correct, or at least acceptable. The client disconnected AND the server got a signal. The signal is the more important event.

But there's a more concrete issue: what if `runErr` is non-nil AND `ctx.Err()` is non-nil? The code handles both. Fine.

Actually, I notice a potential issue with `TestServeShutdown_ClientDisconnectDoesNotInterrupt`. It uses `e.shutdownReviews(false, 200*time.Millisecond)`. Then it reads status. But `startInFlightReview`'s cleanup will later call `e.shutdownCancel()`. However, `e.shutdownReviews(false, ...)` just calls `e.drain`. The drain waits up to 200ms. Since the review is blocked, `e.drain` times out. Then the test reads status: `RunInProgress`. Then test ends. Cleanup calls `e.shutdownCancel()`, then `e.drain(time.Second)`. But between `e.drain` timing out in the test and the cleanup, the review goroutine is still running. When cleanup calls `e.shutdownCancel()`, the context is cancelled. The review writes `Interrupted` status. Then `e.drain` returns. Then temp dir cleanup happens. This is fine.

Wait, but what if `e.drain` in the test body (200ms) doesn't wait for `e.bg` to reach zero (because the review is blocked), and then `ReadReviewStatus` is called? Since the review is in progress, the status file might not exist yet, or might be in some intermediate state. `fanout.ReadReviewStatus` must handle "in progress" correctly. The test assumes it does.

Actually, let me look for an issue in `withShutdownCancel` regarding nil `ctx`. The function signature accepts `ctx context.Context`. If `ctx` is nil, `context.WithCancel(ctx)` panics. But the caller passes `e.reviewContext(...)`, which returns a context. So `ctx` is not nil.

Let me re-read the problem statement and acceptance criteria.
- AC1: detached MCP review in flight when server begins shutdown is marked interrupted.
- AC2: handler returning normally does NOT mark detached review interrupted.
- AC3: existing serve-mode drain behavior unchanged.
- AC4: No false completed status for interrupted run.

Does the code correctly implement AC4? The comment says: "completed agents are preserved, in-flight agents are cut off, partial results are written, and the existing ExecuteReview marker logic records Interrupted=true". This is the same as CLI. The code looks correct.

Wait, I think I found a subtle bug!

In `handleReview`:
```go
go func() {
    defer e.bg.Done()
    defer func() {
        if r := recover(); r != nil {
            e.logger().Error("review fan-out panicked", "review_id", prep.ID, "panic", r)
        }
    }()
    rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
    defer cancel()
    if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
        e.logger().Error("review fan-out finished with errors", "review_id", prep.ID, "error", err)
    }
}()
```

What if the goroutine panics before `defer cancel()` is executed? Actually, `defer` is registered at the line `defer cancel()`. If a panic happens before that line, `cancel()` won't be called. But the only code before that is `defer e.bg.Done()` and `defer func()...`. The `withShutdownCancel` call could panic? `context.WithCancel` panics if parent is nil, but parent is not nil. `context.AfterFunc` panics if ctx is nil, but `e.shutdownCtx` is not nil here (because if it were nil, we returned earlier). So no panic there.

But what if `e.shutdownCtx` is nil? Then `withShutdownCancel` returns early with no-op cancel. So no panic.

What if `e` is nil? Then `e.withShutdownCancel` panics. But `e` is the receiver, presumably not nil.

So that's fine.

Wait, let me look at the `buildServer` function comment:
```go
// The NewServer path discards the engine and never
// fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so
// the unfired cancel is reclaimed with the engine.
```
This is slightly misleading. The "unfired cancel" is a function value. It will be GC'd when the engine is GC'd. The context itself will also be GC'd if no one holds a reference. But if any goroutine is holding onto `shutdownCtx`, it won't be GC'd. However, no goroutine should hold it unless `AfterFunc` was registered. And `AfterFunc` is only registered by `withShutdownCancel`, which is only called for detached reviews. And those are cleaned up. So fine.

Actually, I think there's a real issue in `Serve`:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```
Consider the case where `s.Run` returns `runErr != nil` AND `ctx.Err() == nil`. The function then calls `e.shutdownReviews(false, shutdownDrain)`, which drains but does not cancel. Then it returns `fmt.Errorf("serve stdio: %w", runErr)`. This is fine.

But what if `runErr != nil` because the stdio transport encountered an error, but the server is still running? Then we drain. That's fine.

Actually, is there an issue with `e.shutdownCancel` being called in `shutdownReviews` but `e` being used after `Serve` returns? No, `Serve` is the only user.

Let me think about whether the test `TestServeShutdown_ClientDisconnectDoesNotInterrupt` is racy. It asserts `RunInProgress` after a 200ms drain. But `startInFlightReview` starts a review that is blocked. The status on disk at that point might still be "starting" or whatever the initial status is. The test asserts `RunInProgress`. This seems correct if the fan-out sets `RunInProgress` before calling the completer. Assuming `ExecuteReview` does that.

Wait, in `TestServeShutdown_NormalCompletionNotInterrupted`, it uses `fakeCompleter{resp: validFindings}`. Where is `fakeCompleter` defined? Not in the diff. It's presumably in another test file in the same package. Not an issue.

Let me look at `waitForTerminalStatus`:
```go
func waitForTerminalStatus(t *testing.T, dir, id string) *fanout.ReviewStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := fanout.ReadReviewStatus(dir, id)
		require.NoError(t, err)
		if st.Status != fanout.RunInProgress {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("review %s did not reach a terminal status within 10s (last: %s)", id, st.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
```
This is fine. `time.Sleep` in a tight-ish loop. Not a big deal for tests.

Hmm, let me look at `TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown`:
```go
func TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown(t *testing.T) {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	e := &engine{shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

	rctx, cancel := e.withShutdownCancel(context.Background())
	defer cancel()

	select {
	case <-rctx.Done():
		t.Fatal("review ctx cancelled before server shutdown")
	default:
	}

	shutdownCancel()

	select {
	case <-rctx.Done():
		assert.ErrorIs(t, rctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("review ctx not cancelled after server shutdown")
	}
}
```
Wait, `defer cancel()` is called. At the end of the