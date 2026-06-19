<think>
Let me carefully review this diff against the sprint plan scope and check for issues.

The sprint plan is about Epic 4.1.2: Mark Serve-Mode Detached Reviews Interrupted on Shutdown. The in-scope files are:
- `internal/mcp/handlers.go` - the reviewContext and handler logic
- `internal/mcp/server.go` - the Serve function and buildServer
- `internal/fanout/review.go` - mentioned for the interrupted marker predicate (not modified)
- `internal/mcp/shutdown_test.go` - new test file

Let me analyze the diff carefully:

**handlers.go changes:**
1. Added `shutdownCtx` and `shutdownCancel` fields to engine struct
2. Added `withShutdownCancel` method that:
   - Returns ctx unchanged with no-op cancel if `e.shutdownCtx == nil`
   - Otherwise creates a cancellable child context with `AfterFunc(e.shutdownCtx, cancel)`
   - Returns a cancel func that stops the AfterFunc and cancels
3. Modified `handleReview` to wrap `e.reviewContext(ctx, prep.ID)` with `withShutdownCancel` and defer the cancel

**server.go changes:**
1. Changed `e.drain(shutdownDrain)` to `e.shutdownReviews(ctx.Err() != nil, shutdownDrain)`
2. Added `shutdownReviews` method that:
   - Cancels shutdown if `serverShutdown` is true and `shutdownCancel` is not nil
   - Then drains
3. Modified `buildServer` to create `shutdownCtx, shutdownCancel := context.WithCancel(context.Background())` and store them in engine

**Potential issues to check:**

1. **Race condition on `shutdownCancel` field:** The `shutdownCancel` is stored in the engine struct. Multiple goroutines could read/write to it. In the current code, `shutdownCancel` is set in `buildServer` and called in `shutdownReviews`. But `shutdownReviews` is called from `Serve` after `s.Run` returns, and `buildServer` happens before `Serve`. So the order is: buildServer sets it, Serve calls it. The `handleReview` reads `shutdownCtx` (not the cancel func) via `withShutdownCancel`. This seems safe in terms of happens-before, but let me think about it more.

Actually, wait. The `shutdownCancel` is called from `shutdownReviews` which is called from `Serve` after `s.Run` returns. The `buildServer` sets the cancel func. The Go memory model: `buildServer` is called sequentially before `Serve`, and within `buildServer` the engine is created. Then `Serve` uses the engine. So there's a happens-before relationship. The shutdownCancel is only called once from `shutdownReviews`, so no race there.

2. **The `e.shutdownCancel` is called only once but is it safe?:** Yes, `shutdownCancel` is from `context.WithCancel`, which is safe to call once.

3. **Potential issue: `shutdownCancel` not called on Serve error paths?** Looking at the code: if `s.Run` returns with an error, `runErr` is set, and then `e.shutdownReviews(ctx.Err() != nil, shutdownDrain)` is called. If `ctx` is cancelled (which is typical for SIGINT), the cancel is fired. If `ctx` is not cancelled but `s.Run` errored, then no cancel is fired. This seems correct.

4. **Issue: `shutdownCancel` is never called if Serve returns an error early:** If `buildServer` returns an error, `Serve` returns that error early. But at that point, no `shutdownCtx` was created (it's created inside `buildServer` after the check). Wait, looking at the code:

```go
func Serve(ctx context.Context, root string, completer fanout.Completer, logger *slog.Logger) error {
	...
	if _, err := buildServer(...); err != nil {
		return err
	}
	...
}
```

If `buildServer` fails, the `shutdownCtx` and `shutdownCancel` were never created. So no leak. Good.

But wait, looking at the code more carefully: the `shutdownCtx` and `shutdownCancel` are created inside `buildServer`:
```go
func buildServer(...) (*mcpsdk.Server, *engine, error) {
    ...
    shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
    e := &engine{...}
    ...
}
```

If `buildServer` returns an error AFTER creating these (e.g., during tool registration), then the cancel func is not stored anywhere accessible. This is a context leak! The `shutdownCtx` goroutine spawned by `context.WithCancel` won't be cleaned up properly. Actually, `context.WithCancel` doesn't spawn a goroutine - it only spawns one if you call `AfterFunc` on it. So calling `shutdownCancel` on it is optional. But to be safe, we should still call it.

Wait, looking at the diff more carefully:
```go
shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
e := &engine{root: root, completer: completer, log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}
```

If `buildServer` returns an error after this point, the local `shutdownCancel` goes out of scope and the engine isn't returned. So `shutdownCancel` is never called. This is a minor resource leak - not a goroutine leak (no goroutine was started), but the cancel func is lost. Actually, since `context.WithCancel` only spawns goroutines when `AfterFunc` is called on it (in our case, by `withShutdownCancel`), the leak is just a dangling cancel function that does nothing. This is very minor.

5. **CRITICAL: The `e.shutdownCancel()` is called even when `serverShutdown` is false, but the check guards it:** Looking at `shutdownReviews`:
```go
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
    if serverShutdown && e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(timeout)
}
```

Wait, what about when the client cleanly disconnects (serverShutdown=false)? The reviews are left running, and `drain` will wait for them or timeout. This matches the AC3 requirement. But the engine's `shutdownCancel` is never called, which means the `shutdownCtx` is never cancelled, and the `AfterFunc` registrations persist. This is a minor leak - not a goroutine leak because the AfterFunc is just a registration that gets cleaned up when the parent context (Background) is done. Actually, `context.Background()` is never "done" (it has no deadline or cancel), so these AfterFunc registrations persist for the process lifetime. This is a real leak!

Actually wait, let me re-read. The shutdownCtx is `context.WithCancel(context.Background())`. The `AfterFunc` registers a callback on shutdownCtx. If shutdownCtx is never cancelled, the AfterFunc registrations persist. But `context.AfterFunc` documentation says:

> AfterFunc registers the function f to be called within its own goroutine after ctx is done (cancelled or timed out).

So each call to `withShutdownCancel` that creates an AfterFunc registration will spawn a goroutine that waits for `shutdownCtx` to be done. If `shutdownCtx` is never cancelled, these goroutines... actually no, they just sit waiting. They don't prevent program exit because they're waiting on a channel that... hmm, actually they might prevent program exit because they're goroutines waiting on channels.

Wait, let me re-read the Go documentation for `context.AfterFunc`:
> If `ctx` is already done, AfterFunc calls `f` in its own goroutine immediately. Otherwise, it schedules `f` to be called in its own goroutine upon cancellation.

The `stop` function returned by `AfterFunc`:
> It returns a stop function that unregisters the f call, and reports whether the call has been stopped. If stop returns true, the AfterFunc is not called. If stop returns false, the AfterFunc is already running or has finished.

And the `defer cancel()` in the handler:
```go
rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
defer cancel()
```

The cancel function returned by `withShutdownCancel`:
```go
return cctx, func() { stop(); cancel() }
```

So the cancel function stops the AfterFunc registration. This is good - when the handler goroutine returns (the goroutine that called `handleReview`), the deferred cancel runs, stopping the AfterFunc registration. So if the review completes normally, the AfterFunc is unregistered.

But wait - the `handleReview` function returns immediately after spawning the goroutine:
```go
go func() {
    ...
    rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
    defer cancel()
    if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
        ...
    }
}()
```

The `defer cancel()` is inside the goroutine, not the handler. So it runs when the goroutine exits, which is when ExecuteReview returns. Good.

6. **Issue: `e.shutdownCancel` may be nil in some test paths:** The test `TestWithShutdownCancel_NilShutdownCtxIsNoop` creates `&engine{}` with no shutdownCtx. The `withShutdownCancel` method checks for this. But what about `shutdownReviews`?

```go
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
    if serverShutdown && e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(timeout)
}
```

This is fine - it checks `e.shutdownCancel != nil`.

7. **Issue: The `serverShutdown` parameter is based on `ctx.Err() != nil`:** But what if `ctx` is cancelled for reasons other than SIGINT? E.g., if the context has a deadline that expires. Looking at the original code:

```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

If `ctx.Err() == context.DeadlineExceeded`, then `ctx.Err() != nil` is true, and we'd cancel in-flight reviews as if it were a server shutdown. This might not match user intent, but the sprint plan says "SIGINT/SIGTERM via the CLI signal handler", so this seems acceptable for the scope.

8. **Potential race: `shutdownCancel` field is written in `buildServer` and read in `shutdownReviews`:** Since `buildServer` is called before `Serve`, and `Serve` calls `shutdownReviews` after `s.Run` returns, there's a happens-before relationship. However, the `engine` is also used by `handleReview` which reads `shutdownCtx` (in `withShutdownCancel`). The `handleReview` could be called concurrently from the MCP server's transport loop. The reading of `shutdownCtx` (a pointer to context.Context) and `shutdownCancel` (a function pointer) needs to be synchronized.

Looking at the diff:
```go
type engine struct {
    ...
    shutdownCtx    context.Context
    shutdownCancel context.CancelFunc
}
```

`shutdownCtx` is a `context.Context` interface (pointer under the hood), and `shutdownCancel` is a function pointer. These are set in `buildServer` and read in goroutines spawned by `handleReview` calls.

The question is: is there a happens-before edge from the write in `buildServer` to the read in `withShutdownCancel`?

The flow is:
1. `buildServer` creates the engine and sets the fields
2. `buildServer` returns the engine to `Serve`
3. `Serve` calls `s.Run(ctx, ...)` which starts the transport loop
4. The transport loop processes requests and calls `handleReview` for each
5. `handleReview` spawns a goroutine that calls `withShutdownCancel`

Step 1 happens-before step 2 (function return), step 2 happens-before step 3 (sequential code), step 3 happens-before step 4 (the transport loop starts), and step 4 happens-before step 5 (request handling).

So there is a happens-before chain. Good, no race.

But wait, what if `handleReview` is called from a goroutine that was spawned before `buildServer` returned? That can't happen because the engine isn't accessible until `buildServer` returns it.

Actually, looking at the code more carefully: the `engine` is passed to `Serve` via `buildServer`, and the engine is also returned to the caller of `Serve`. But the fields `shutdownCtx` and `shutdownCancel` are set in `buildServer` and the engine is created there. The engine is only used by the code path that called `buildServer`, so the fields are set before the engine is used.

9. **Issue: `shutdownCancel` could be called multiple times if `shutdownReviews` is called multiple times:** Looking at the code, `shutdownReviews` is called only once from `Serve` (after `s.Run` returns). The test calls it directly, but each test sets up its own engine. So in production, it's called once. Calling `context.CancelFunc` multiple times is safe (it's idempotent).

10. **Issue: Tests may have flaky behavior on `shutdownDrain` timing:** The test `TestServeShutdown_MarksInFlightDetachedReviewInterrupted` uses `shutdownDrain` (5s) for the drain timeout. The blockingCompleter blocks until cancelled, and `shutdownCancel` is called at the start of `shutdownReviews`. The `drain` then waits for the goroutine to finish. Since the goroutine will see the cancellation and return, this should complete quickly. But if there's any delay, the test could take up to 5s. This isn't a bug, just a potential slowness.

11. **Potential issue: The `_ = e` pattern in `startInFlightReview`:** The test function returns `e` but it's not clear if any test is using it correctly. Looking at the test:
```go
e, dir, id := startInFlightReview(t)
e.shutdownReviews(true, shutdownDrain)
```

The `e` is used to call `shutdownReviews`. But the `t.Cleanup` registered in `startInFlightReview` also calls `e.shutdownCancel()` and `e.drain(time.Second)`. So if `shutdownReviews` cancels, the drain in cleanup might see an already-drained state. This seems fine.

12. **Issue: The `t.Cleanup` in `startInFlightReview` calls `e.shutdownCancel()` and `e.drain(time.Second)` even after the test has called `shutdownReviews`.** This means:
- Test: `e.shutdownReviews(true, shutdownDrain)` cancels shutdown and drains
- Cleanup: `e.shutdownCancel()` (no-op since already cancelled) and `e.drain(time.Second)` (no-op since no in-flight reviews)

This is safe but redundant. Not a bug.

13. **Issue: Memory model in tests - `startInFlightReview` returns `e` from the goroutine-safe setup, but reads/writes after return are on different goroutines than `buildServer`.** As analyzed above, this should be safe due to the happens-before chain.

14. **Potential issue with the `Entered` channel:** In `blockingCompleter`:
```go
type blockingCompleter struct {
    once    sync.Once
    entered chan struct{}
}
```

This uses `sync.Once` to close the channel. But if the blockingCompleter is called multiple times (e.g., for multiple agents), only the first call closes the channel, and subsequent calls also block. This seems intentional - we want to know when at least one agent has started.

15. **Issue: What if `fanout.ExecuteReview` doesn't actually spawn multiple agents (and never calls `Complete`)?** Then `entered` would never be closed, and the test would time out after 5s with "detached fan-out never started". This is a test design concern, not a bug in the production code.

16. **Critical review of `withShutdownCancel`:** The method is called from `handleReview`'s goroutine. The returned `cancel` is deferred in that goroutine. When the goroutine exits (after ExecuteReview returns), the deferred cancel runs, which calls `stop()` and `cancel()`. 

Wait, there's a subtle issue: if `e.shutdownCtx` is cancelled while ExecuteReview is running, the AfterFunc will fire, calling `cancel()`. This cancels `cctx`. ExecuteReview sees this cancellation and returns. Then the deferred `cancel()` in the goroutine runs, calling `stop()` (which returns false because the AfterFunc already fired) and `cancel()` (which is a no-op since already cancelled). This is correct.

If ExecuteReview completes normally, the deferred `cancel()` runs, calling `stop()` (which returns true because the AfterFunc hasn't fired) and `cancel()`. This unregisters the AfterFunc and cancels the context. Correct.

17. **Issue: `e.shutdownCancel` is a `context.CancelFunc` which is a function type. Storing it in a struct and calling it concurrently with the goroutines that read `e.shutdownCtx` is safe because:** 
- The shutdownCancel is set in buildServer (happens-before everything else due to function return semantics)
- It's called in shutdownReviews (after s.Run returns, which happens after buildServer)
- The goroutines that read shutdownCtx via withShutdownCancel are spawned after buildServer returns (via s.Run's request handling)

So the memory model is satisfied.

18. **One thing I want to double-check: the test `TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown` checks that `rctx.Err() == context.Canceled` after `shutdownCancel()` is called.** Let me trace through:
- `shutdownCtx, shutdownCancel := context.WithCancel(context.Background())`
- `e := &engine{shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}`
- `rctx, cancel := e.withShutdownCancel(context.Background())` - creates a child of the passed context (Background), with AfterFunc on shutdownCtx
- `shutdownCancel()` - cancels shutdownCtx
- The AfterFunc fires, calling `cancel()` on the child
- `rctx` is now done with err = context.Canceled (propagated from the child)

This should work correctly.

19. **Looking at the `_` argument in `handleReview`:** The diff shows:
```go
func (e *engine) handleReview(ctx context.Context, _ *mcpsdk.CallToolRequest, in...
```

The second parameter is `_ *mcpsdk.CallToolRequest`. This was already in the original code, not changed in the diff.

20. **Let me look at the `e.logger()` call in handleReview.** It's called inside the goroutine, not changed in the diff.

21. **The diff says "The NewServer path discards the engine and never fires shutdownCancel".** Let me check if `NewServer` is still being used. Looking at the code:
```go
s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
```

Yes, `mcpsdk.NewServer` is still used. The comment says this path discards the engine. But looking at the code, the engine is stored in a variable and returned from `buildServer`. It's not discarded. Let me re-read the comment:

> "The NewServer path discards the engine and never fires shutdownCancel"

Hmm, I think this comment is misleading. Looking at the code, `buildServer` returns the engine, and `Serve` uses it. The `NewServer` (from mcpsdk) is the SDK's server constructor, and the engine is our internal struct. They're different things.

Actually, wait. Let me re-read the comment in the diff:
```go
// Server-lifecycle cancellation: cancelled by Serve once the transport loop
// returns (client disconnect or SIGINT-cancelled ctx). Detached reviews tie
// their cancellation to shutdownCtx via withShutdownCancel, so a server
// shutdown marks in-flight reviews interrupted without a handler return ever
// aborting them (epic 4.1.2). The NewServer path discards the engine and never
// fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so
// the unfired cancel is reclaimed with the engine.
```

The "NewServer path" might refer to a hypothetical path where someone calls `NewServer` (maybe the SDK's `NewServer`) directly without going through `buildServer`. But looking at the code, `buildServer` is the only path that creates the engine. The `NewServer` mentioned is `mcpsdk.NewServer`, which creates the SDK server, not our engine. So this comment is a bit confusing.

Actually, I think the comment is trying to say: if someone constructs an engine directly (e.g., in tests via `&engine{}`), the shutdownCtx is nil and the cancel is never fired. The `withShutdownCancel` method handles this case. This is correct.

22. **Let me check if there are any other files modified.** The diff shows:
- `internal/mcp/handlers.go` - modified
- `internal/mcp/server.go` - modified  
- `internal/mcp/shutdown_test.go` - new file

All within scope per the sprint plan.

23. **Let me check the test for any issues:**

`TestServeShutdown_MarksInFlightDetachedReviewInterrupted`:
- Sets up an in-flight review
- Calls `e.shutdownReviews(true, shutdownDrain)` - this cancels shutdown and drains
- Verifies the review is marked interrupted

This should work because:
1. `shutdownReviews(true, ...)` calls `e.shutdownCancel()` which cancels `e.shutdownCtx`
2. The AfterFunc fires, cancelling the review's context
3. The blocking agents see the cancellation and return
4. `ExecuteReview` sees `ctx.Err() == context.Canceled` and records Interrupted=true
5. The drain waits for the goroutine to finish
6. The test reads the on-disk status and verifies it's interrupted

`TestServeShutdown_ClientDisconnectDoesNotInterrupt`:
- Sets up an in-flight review
- Calls `e.shutdownReviews(false, 200*time.Millisecond)` - doesn't cancel, short drain
- Verifies the review is still in_progress (not interrupted)

This should work because:
1. `shutdownReviews(false, ...)` does NOT call `e.shutdownCancel()`
2. The review's context is NOT cancelled
3. The blocking agents are still blocking
4. The drain times out after 200ms
5. The review is still in_progress on disk

Wait, there's a potential issue here. The test uses `200*time.Millisecond` for the drain, but the cleanup also calls `e.drain(time.Second)`. The first drain times out, then the cleanup drain runs for 1 second. During the cleanup drain, `e.shutdownCancel()` is called (from the cleanup), which cancels the review. So the review will eventually be cancelled and marked interrupted. But the test assertion happens BEFORE the cleanup, so it should see in_progress.

Actually, looking more carefully:
```go
t.Cleanup(func() {
    if e.shutdownCancel != nil {
        e.shutdownCancel()
    }
    e.drain(time.Second)
})
```

The cleanup runs AFTER the test function returns. The test assertion `assert.Equal(t, fanout.RunInProgress, st.Status, ...)` runs before the cleanup. So the test should see in_progress at the time of assertion. Good.

But wait, there's still a subtle issue: the assertion checks that the status is `RunInProgress`, not `RunInterrupted`. The blocking agents are still blocking (no cancellation), so ExecuteReview is still running. The on-disk status is `in_progress`. The test should pass.

24. **Let me check if there's a race condition in the test between the assertion and the cleanup.** The assertion reads the on-disk status file. The cleanup eventually cancels the shutdown, which causes ExecuteReview to write the interrupted status. But the assertion happens first, so it should see in_progress.

25. **Issue: The test `TestServeShutdown_ClientDisconnectDoesNotInterrupt` might be flaky if the fan-out writes the in_progress status AFTER the test reads it.** Let me trace:
1. `handleReview` is called, returns `runningStatus` (which is `RunInProgress`?)
2. The goroutine starts ExecuteReview
3. The test waits for `bc.entered` (an agent has started)
4. The test calls `e.shutdownReviews(false, 200ms)`
5. The drain times out after 200ms
6. The test reads the on-disk status

Between steps 3 and 6, the fan-out might not have written `in_progress` to disk yet. If the first thing the fan-out does is spawn agents (which block), it might write `in_progress` before or after the agents start.

Actually, looking at the code flow: `ExecuteReview` likely writes the initial `in_progress` status before spawning agents. So by the time `bc.entered` is closed (an agent is running), the `in_progress` status should be on disk. This should be safe.

26. **One thing I notice: the test `TestServeShutdown_NormalCompletionNotInterrupted` uses `waitForTerminalStatus` to wait for the review to complete. Then it calls `e.shutdownCancel()` and `e.drain(shutdownDrain)`. Then it reads the status again and asserts it's still `RunCompleted`.**

The question is: can `e.shutdownCancel()` after the review has completed cause the status to change? Let's see:
- The review has completed, ExecuteReview has returned
- The goroutine has exited, so the deferred `cancel()` has run, which calls `stop()` (unregisters the AfterFunc) and `cancel()` (cancels the context)
- Now `e.shutdownCancel()` is called, which cancels `e.shutdownCtx`
- Since the AfterFunc was already unregistered, this is a no-op
- The status on disk is `RunCompleted`

This should work correctly.

27. **Looking at the test `TestWithShutdownCancel_ParentReturnDoesNotCancel`:**
```go
reqCtx, reqCancel := context.WithCancel(context.Background())
base := context.WithoutCancel(reqCtx) // mirrors reviewContext's detachment
rctx, cancel := e.withShutdownCancel(base)
defer cancel()

reqCancel() // the request handler returns

select {
case <-rctx.Done():
    t.Fatal("detached review ctx cancelled by handler return (AC2 violated)")
case <-time.After(50 * time.Millisecond):
    // still alive — correct
}
```

This tests that cancelling the request context (simulating handler return) does NOT cancel the review context. The `base` is `context.WithoutCancel(reqCtx)`, so cancelling `reqCtx` doesn't affect `base`. Then `rctx` is derived from `base` via `withShutdownCancel`. Since `base` is not cancelled, and `shutdownCtx` is not cancelled, `rctx` should not be cancelled. This should work.

But wait, `withShutdownCancel` uses `context.WithCancel(ctx)` where `ctx` is the parameter. So it creates a child of the passed context. The child's cancellation depends on:
- The parent (`base`) being cancelled
- `cancel()` being called
- `e.shutdownCtx` being cancelled (via AfterFunc)

In the test, `base` is `context.WithoutCancel(reqCtx)`, so cancelling `reqCtx` doesn't cancel `base`. The AfterFunc on `shutdownCtx` doesn't fire because `shutdownCtx` is not cancelled. So `rctx` should not be cancelled. Correct.

28. **Let me think about potential issues with the design itself (Design 1 from the sprint plan):**

The sprint plan says:
> `Serve` fires the server-lifecycle cancel **after `s.Run` returns and before `e.drain(...)`**, so the cancelled fan-out flushes its partial + interrupted manifest within the existing 5s drain window.

Looking at the code:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

`shutdownReviews` does:
```go
if serverShutdown && e.shutdownCancel != nil {
    e.shutdownCancel()
}
e.drain(timeout)
```

So `e.shutdownCancel()` is called BEFORE `e.drain(timeout)`. This matches the sprint plan. Good.

29. **But wait - the sprint plan says "after `s.Run` returns and before `e.drain(...)`".** This is exactly what the code does. Good.

30. **One concern: the `runErr` is captured but the code continues to call `shutdownReviews` even if `runErr != nil`.** Let me check:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
if runErr != nil {
    return fmt.Errorf("serve stdio: %w", runErr)
}
return nil
```

So `shutdownReviews` is always called after `s.Run` returns, regardless of `runErr`. Then if `runErr != nil`, the function returns the error. This is correct - we want to drain in-flight reviews even if `s.Run` errored.

31. **Another concern: what if `ctx` is cancelled but `s.Run` returns nil (no error)?** Then `ctx.Err() != nil` is true, so `serverShutdown` is true, and we cancel the shutdown. This is correct - if the context is cancelled, the server is shutting down.

32. **What if `ctx` is not cancelled but `s.Run` returns an error?** Then `ctx.Err() != nil` is false, so `serverShutdown` is false, and we don't cancel the shutdown. The reviews are left running, and the drain waits for them or times out. This might not be ideal if the error indicates a server failure, but it matches the AC3 requirement (drain behavior unchanged for non-shutdown cases).

33. **Let me check if there are any issues with the `e.drain` function:** The diff doesn't change `e.drain`, so I don't need to review it.

34. **Let me think about whether `shutdownCancel` being stored in the engine struct could cause issues with concurrent access.** As analyzed, the memory model is satisfied. But there's a subtle issue: if `shutdownCancel` is called concurrently from multiple goroutines, it's safe (context.CancelFunc is safe to call multiple times). But in this code, it's only called from `shutdownReviews`, which is called once from `Serve`. So no concurrent calls in production.

35. **Let me look at the comment on `shutdownCancel` more carefully:**
```go
// shutdownCtx is cancelled (via shutdownCancel) when the server begins
// shutdown — Serve fires it after the transport loop returns, before drain.
```

This says "after the transport loop returns, before drain". But the code does:
```go
runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
```

And `shutdownReviews` does:
```go
if serverShutdown && e.shutdownCancel != nil {
    e.shutdownCancel()
}
e.drain(timeout)
```

So the cancel is fired after `s.Run` returns and before `e.drain`. The comment is correct.

36. **Let me check if there's an issue with the `runErr` handling:** If `s.Run` returns an error and `ctx` is not cancelled, then `serverShutdown` is false, and we don't cancel the shutdown. The reviews continue running, and the drain waits. But `runErr` is returned, which means `Serve` returns. The reviews might still be running in the background when `Serve` returns. This is the existing behavior (before the diff), so it's not a regression.

37. **Let me check the test for the `startInFlightReview` helper more carefully:**

```go
func startInFlightReview(t *testing.T) (e *engine, dir, id string) {
    t.Helper()
    t.Setenv("ATCR_TEST_KEY", "secret")
    root, base, head := gitRepo(t)
    writeReviewConfig(t, root)

    bc := &blockingCompleter{entered: make(chan struct{})}
    _, e, err := buildServer(root, bc, nil)
    require.NoError(t, err)

    _, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
    require.NoError(t, err)
    require.Equal(t, runningStatus, res.Status)

    select {
    case <-bc.entered:
    case <-time.After(5 * time.Second):
        t.Fatal("detached fan-out never started")
    }
    ...
}
```

The `t.Cleanup` is registered AFTER the `select`. If the test times out (detached fan-out never started), the cleanup still runs. The cleanup calls `e.shutdownCancel()` and `e.drain(time.Second)`. This is correct.

But wait, there's a potential issue: the `t.Cleanup` is registered with `t.Cleanup`, which runs in LIFO order. The `t.TempDir()` cleanup is also registered. The comment says:
```go
// Unblock the parked fan-out at test end (before t.TempDir cleanup, which is
// registered earlier and so runs after this) so a test that does NOT trigger a
// server shutdown does not leak the blocked review goroutine.
```

Wait, `t.TempDir()` is called in `gitRepo(t)` which is called BEFORE `t.Cleanup(...)`. So `t.TempDir()` cleanup is registered FIRST, and `t.Cleanup(...)` is registered LATER. LIFO order means `t.Cleanup(...)` runs FIRST, then `t.TempDir()` cleanup runs. This matches the comment. Good.

38. **Let me check if there's a potential issue with the `handleReview` call in `startInFlightReview`:**

```go
_, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
```

The second argument is `nil` (a `*mcpsdk.CallToolRequest`). The original code probably has the same call, so this is not a new issue.

39. **Let me think about whether the diff correctly implements Design 1 from the sprint plan:**

Design 1: "Decoupled interrupt context — derive the detached context from `context.WithoutCancel(ctx)` but layer a separate server-lifecycle cancellation (cancelled only on server shutdown, never on handler return), so the existing `ctx.Err()`-based interrupted marker fires for the right reason."

The code:
- `e.reviewContext(ctx, prep.ID)` returns `context.WithoutCancel(ctx)` with values
- `e.withShutdownCancel(...)` wraps it with a cancellable context that has an AfterFunc on `e.shutdownCtx`
- `e.shutdownCtx` is cancelled only on server shutdown (not on handler return)

This matches Design 1. Good.

40. **One potential issue: the `e.shutdownCtx` is a `context.Context` stored in the engine. The `withShutdownCancel` method reads it. If the engine is used after `buildServer` returns and before `Serve` is called, there could be a race.** But looking at the code, the engine is created in `buildServer` and used in `Serve`. There's no other use between them. So no race.

41. **Let me check if there's an issue with the `mcpsdk.NewServer` call:**

```go
s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
```

This is the SDK's server constructor. The diff doesn't change this line. Good.

42. **Let me think about whether the test `TestServeShutdown_NormalCompletionNotInterrupted` could have a race condition:**

The test:
1. Sets up a review with `fakeCompleter` (completes immediately)
2. Calls `handleReview` which spawns a goroutine
3. Waits for the review to reach a terminal status (via `waitForTerminalStatus`)
4. Calls `e.shutdownCancel()` and `e.drain(shutdownDrain)`
5. Reads the status again and asserts it's `RunCompleted`

Between steps 3 and 4, the review has completed and the goroutine has exited. The deferred `cancel()` in the goroutine has run, unregistering the AfterFunc. So calling `e.shutdownCancel()` in step 4 doesn't affect anything (the AfterFunc is already unregistered).

But wait, there's a subtle issue: `waitForTerminalStatus` polls the on-disk status. The status is written by ExecuteReview. If ExecuteReview writes the status and then the goroutine exits, there's a happens-before relationship between the status write and the goroutine exit. The `waitForTerminalStatus` reads the status, so it sees the write. Good.

But the test then calls `e.shutdownCancel()`. This cancels `e.shutdownCtx`. Since the AfterFunc was already unregistered (by the deferred cancel), this is a no-op. The status remains `RunCompleted`. Good.

43. **Let me check if there's a potential issue with the `e.drain(shutdownDrain)` call in `TestServeShutdown_NormalCompletionNotInterrupted`:**

The `drain` function waits for background reviews to finish. If all reviews have finished, it returns immediately. So this is a no-op. Good.

44. **Overall assessment:** The diff looks correct and implements Design 1 as specified in the sprint plan. The tests cover all the acceptance criteria (AC1, AC2, AC3, AC4). I don't see any obvious bugs.

Let me look more carefully for subtle issues...

45. **Issue: The `e.shutdownCancel` field could be accessed concurrently if `shutdownReviews` is called from a different goroutine than the one that set it.** As analyzed, `buildServer` sets it, and `shutdownReviews` (called from `Serve`) reads and calls it. The happens-before chain is: `buildServer` returns → `Serve` calls `s.Run` → `s.Run` returns → `Serve` calls `shutdownReviews`. So the read in `shutdownReviews` happens after the write in `buildServer`. Good.

46. **Issue: The `e
</think>
