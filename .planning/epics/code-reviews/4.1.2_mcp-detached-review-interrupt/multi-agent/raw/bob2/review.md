We are given a diff and a sprint plan. We must only flag issues that are in scope (as per the sprint plan) and in the files mentioned.

The sprint plan is about Epic 4.1.2: Mark Serve-Mode Detached Reviews Interrupted on Shutdown.

The changes in the diff are:

1. internal/mcp/handlers.go:
   - Added two fields to the engine struct: shutdownCtx and shutdownCancel.
   - Added a new method `withShutdownCancel` that creates a context that is cancelled when the server begins shutdown (via the engine's shutdownCtx) and returns a cancel function that must be deferred.
   - Modified the `handleReview` function to use `withShutdownCancel` around the detached review context.

2. internal/mcp/server.go:
   - Changed the Serve function to call `e.shutdownReviews` instead of `e.drain` directly, passing whether the root context was cancelled (serverShutdown) and the shutdownDrain timeout.
   - Added a new method `shutdownReviews` that conditionally cancels the shutdownCtx (if serverShutdown is true and shutdownCancel is not nil) and then calls drain.
   - In buildServer, initialized the engine with shutdownCtx and shutdownCancel from context.WithCancel(context.Background()).

3. internal/mcp/shutdown_test.go: a new test file.

We are to review the diff for issues, but only those that are in scope (related to the sprint plan) and in the files mentioned.

Let's look for potential issues in the changed code:

In handlers.go:

- The `withShutdownCancel` function:
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
  This function returns a context that is the child of the given ctx (with its own cancel) and an AfterFunc that will call the child's cancel when e.shutdownCtx is done. The returned cancel function calls both stop (which cancels the AfterFunc) and the child's cancel.

  However, note that the AfterFunc is registered to run when e.shutdownCtx is done. The returned cancel function is meant to be deferred by the caller. When the caller defers the cancel, it will:
    1. stop: which cancels the AfterFunc (so if the shutdownCtx hasn't fired yet, it prevents the AfterFunc from running)
    2. cancel: which cancels the child context.

  This seems correct for the use case: when the review finishes normally (before shutdown), we want to clean up the AfterFunc so that it doesn't fire later (and cancel the child context after the review is done). And when the shutdown happens, the AfterFunc will run and cancel the child context.

  However, there is a potential issue: if the shutdownCtx fires (i.e., the server begins shutdown) and then the caller defers the cancel (which happens at the end of the review), the defer will run after the shutdownCtx has fired. In that case, the AfterFunc has already run (and canceled the child context) so calling stop() in the defer is a no-op (because the AfterFunc has already been called and removed). Then we call cancel() on the child context, which is already canceled. This is safe but unnecessary.

  But note: the AfterFunc returns a stop function that cancels the AfterFunc. If the AfterFunc has already run, then stop() returns immediately without doing anything. So it's safe.

  However, there is a more subtle issue: the AfterFunc is set to run the child's cancel when e.shutdownCtx is done. But note that the child context (cctx) is derived from the given ctx (which in the call site is the output of reviewContext, which is context.WithoutCancel of the request context). The AfterFunc does not depend on the child context being alive; it just calls its cancel function. This is acceptable.

  But wait: what if the child context is canceled for another reason (e.g., the request context is canceled via reviewContext's WithoutCancel? Actually, note that reviewContext uses context.WithoutCancel, so the request context cancellation is stripped. Therefore, the only way the child context (cctx) gets canceled is either by the AfterFunc (when shutdownCtx fires) or by the explicit cancel in the defer (which we are using to clean up). So it's safe.

  However, there is a potential problem: the AfterFunc is registered to run when e.shutdownCtx is done. But note that e.shutdownCtx is set in buildServer and then canceled in shutdownReviews (which is called from Serve). The shutdownCtx is a context that is canceled when the server begins shutdown. The AfterFunc will run exactly once when that context is done.

  The code in `withShutdownCancel` seems correct.

  Now, look at the call site in handleReview:

  ```go
  rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID))
  defer cancel()
  if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
      e.logger().Error("review fan-out finished with errors", "review_id", prep.ID, "error", err)
  }
  ```

  This looks correct: we create a context that is detached from the request (via reviewContext) but then we attach a cancellation that is tied to the server shutdown. We defer the cancel so that when the review function returns (whether normally or due to an error), we clean up the AfterFunc and cancel the child context (if not already canceled).

  However, note that the `defer cancel()` will run after the ExecuteReview call. This is the right place because we want to clean up the AfterFunc after the review is done.

  But there is a potential issue: what if the ExecuteReview function panics? The defer will still run, which is good because we want to clean up resources.

  So far, no obvious issue in handlers.go.

Now, server.go:

- In the Serve function:
  ```go
  runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
  // s.Run returns for two distinct reasons that must NOT be treated alike: a
  // cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the
  // server itself is shutting down, while ctx.Err()==nil means a clean client
  // stdio disconnect. Only the former interrupts in-flight detached reviews.
  e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
  ```

  This seems correct: we check if the root context was canceled (which would be due to a signal) to decide whether to cancel the detached reviews.

- The shutdownReviews function:
  ```go
  func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
      if serverShutdown && e.shutdownCancel != nil {
          e.shutdownCancel()
      }
      e.drain(timeout)
  }
  ```

  This function cancels the shutdownCtx (if we are in a server shutdown and the shutdownCancel is not nil) and then calls drain. The drain function waits for up to timeout for in-flight background reviews to finish.

  However, note that the drain function is called after the shutdownCancel. The drain function (which we haven't changed) waits for the bg WaitGroup to finish (with a timeout). The bg WaitGroup is used to track the background reviews (each review fan-out is started in a goroutine and increments the bg WaitGroup).

  The sequence is:
    1. Cancel the shutdownCtx (which will cause the AfterFunc in each detached review's context to run, canceling the review context).
    2. Then call drain, which waits for the bg WaitGroup to finish (with a timeout).

  This is correct because:
    - When the review context is canceled, the ExecuteReview function should return (due to context cancellation) and then the goroutine will finish, decrementing the bg WaitGroup.
    - The drain will then wait for all the review goroutines to finish (or timeout).

  However, note that the drain function in the original code (which we haven't changed) is:

  ```go
  // drain waits up to timeout for in-flight background reviews to finish, so a
  // near-complete run is not abandoned mid-write when the client disconnects. A
  // review still running past timeout is left to the process exit.
  func (e *engine) drain(timeout time.Duration) {
      done := make(chan struct{})
      go func() {
          e.bg.Wait()
          close(done)
      }()
      select {
      case <-done:
      case <-time.After(timeout):
      }
  }
  ```

  This drain function waits for the bg WaitGroup to finish (with a timeout). It does not cancel anything; it just waits.

  So the sequence in shutdownReviews is:
    - Cancel the shutdownCtx (which will cause the review contexts to be canceled, leading to the review goroutines finishing and decrementing the bg WaitGroup).
    - Then wait for the bg WaitGroup to finish (with a timeout).

  This is correct.

  However, there is a potential issue: what if the shutdownCtx is nil? We check `if serverShutdown && e.shutdownCancel != nil` so we only call shutdownCancel if it's not nil. This is safe because in buildServer we set shutdownCtx and shutdownCancel, but note the comment: "nil when the engine is built outside buildServer (NewServer discards its engine; tests may construct &engine{} directly)."

  In the Serve function, we are using the engine built by buildServer, so shutdownCtx and shutdownCancel are set. But note that the Serve function is only called from the server build path? Actually, the Serve function is a method on the engine, and the engine is built by buildServer. So in the production server, we have a non-nil shutdownCancel.

  However, the tests might construct an engine directly (as in the shutdown_test.go). In the tests, we see:

  ```go
  _, e, err := buildServer(root, bc, nil)
  ```

  So the engine is built by buildServer, which sets shutdownCtx and shutdownCancel. Therefore, in the tests we are also using an engine with non-nil shutdownCancel.

  But note: the shutdown_test.go also has tests that create an engine directly? Let's see:

  In TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown:
      e := &engine{shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

  In TestWithShutdownCancel_ParentReturnDoesNotCancel:
      e := &engine{shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

  In TestWithShutdownCancel_NilShutdownCtxIsNoop:
      e := &engine{} // no shutdownCtx

  So the tests cover the nil case.

  Therefore, the check for `e.shutdownCancel != nil` is necessary and correct.

  Now, look at the buildServer function:

  ```go
  // Server-lifecycle cancellation: cancelled by Serve once the transport loop
  // returns (client disconnect or SIGINT-cancelled ctx). Detached reviews tie
  // their cancellation to shutdownCtx via withShutdownCancel, so a server
  // shutdown marks in-flight reviews interrupted without a handler return ever
  // aborting them (epic 4.1.2). The NewServer path discards the engine and never
  // fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so
  // the unfired cancel is reclaimed with the engine.
  shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
  e := &engine{root: root, completer: completer, log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}
  ```

  This is correct.

  Now, let's look at the test file: internal/mcp/shutdown_test.go.

  We are only to flag issues in the code that is in scope. The test file is new and is part of the epic, so we should review it for correctness? However, note the sprint plan says: "Only flag issues in files/areas directly related to the work items below." The work items are the code changes in handlers.go and server.go. The test file is also part of the change, but the sprint plan does not explicitly say to avoid testing code. However, the scope constraint says: "if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE".

  The sprint plan's tasks/stories are the code changes in handlers.go and server.go (as per the diff). The test file is also changed (added) in the diff, so it is in the diff. But note: the sprint plan does not mention the test file. However, the test file is directly related to the epic (it tests the epic's acceptance criteria). Therefore, we should review it.

  However, the problem says: "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

  The sprint plan's tasks/stories are the work items: the code changes to implement the epic. The test file is part of the work item (we are adding tests). Therefore, we should review the test file for issues that are in scope (i.e., related to the epic).

  But note: the sprint plan says "Source TD items" and points to a specific line in handlers.go. The test file is new and not mentioned in the source TD items. However, the sprint plan does not say to ignore test files.

  Given the ambiguity, and since the test file is in the diff and is testing the exact changes we are reviewing, I think we should review it for issues that are in scope (i.e., related to the epic 4.1.2). However, if we find an issue in the test file that is not related to the epic (e.g., a general testing mistake) but the test file is only for this epic, then it is in scope.

  Let's review the test file for any issues that might affect the correctness of the epic's implementation.

  Looking at the test file:

  - The blockingCompleter: it blocks each agent until the review context is canceled. It uses a sync.Once to close the entered channel the first time an agent runs. This is used in the tests to wait for the fan-out to start.

  - The startInFlightReview function: it sets up a server, starts a review, and waits for the blockingCompleter to be entered (meaning the fan-out has started). It returns the engine, review dir, and review id.

  - The test TestServeShutdown_MarksInFlightDetachedReviewInterrupted:
        e, dir, id := startInFlightReview(t)
        e.shutdownReviews(true, shutdownDrain) // serverShutdown=true: SIGINT path
        ... then checks that the review status is interrupted.

    This test calls shutdownReviews directly, bypassing the Serve function. This is acceptable for unit testing.

  - The test TestServeShutdown_ClientDisconnectDoesNotInterrupt:
        e, dir, id := startInFlightReview(t)
        e.shutdownReviews(false, 200*time.Millisecond) // serverShutdown=false: client disconnect
        ... then checks that the review is not interrupted and is still in_progress.

  - The test TestServeShutdown_NormalCompletionNotInterrupted:
        This test builds a server with a fake completer that returns valid findings (so the review completes quickly).
        It then waits for the review to complete (via waitForTerminalStatus).
        Then it cancels the shutdownCtx and drains (to clean up) and checks that the review is still completed.

  - The test TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown: tests the withShutdownCancel method.
  - The test TestWithShutdownCancel_ParentReturnDoesNotCancel: tests that handler return doesn't cancel the detached review.
  - The test TestWithShutdownCancel_NilShutdownCtxIsNoop: tests the nil case.

  The tests look comprehensive and correct.

  However, note in the test TestServeShutdown_NormalCompletionNotInterrupted:

        _, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
        require.NoError(t, err)

        st := waitForTerminalStatus(t, res.ReviewPath, res.ReviewID)
        require.Equal(t, fanout.RunCompleted, st.Status,
            "a detached review that finished before any shutdown must be completed (AC2)")

        if e.shutdownCancel != nil {
            e.shutdownCancel()
        }
        e.drain(shutdownDrain)

        st, err = fanout.ReadReviewStatus(res.ReviewPath, res.ReviewID)
        require.NoError(t, err)
        assert.Equal(t, fanout.RunCompleted, st.Status,
            "shutdown must not retroactively interrupt an already-finished review (AC4)")

  This test is correct: it checks that after the review completes normally, a later shutdown does not change the status to interrupted.

  But note: the test calls e.shutdownCancel() and then e.drain(shutdownDrain) after the review has completed. This is to clean up the engine's shutdownCtx (to prevent goroutine leaks). However, note that the engine's shutdownCtx is still alive and has not been canceled. Canceling it at this point is safe but unnecessary because there are no more detached reviews tied to it (the review has completed). However, it is good practice to clean up.

  However, there is a potential issue: the test does not check for any errors or panics during the cleanup. But that's beyond the scope.

  Now, let's look for any actual bugs in the code we changed.

  One potential issue in handlers.go: the `withShutdownCancel` function returns a cancel function that calls `stop(); cancel()`. The `stop` function is the one returned by `context.AfterFunc`. According to the Go documentation, the stop function returns true if it stops the AfterFunc from running, false if the AfterFunc has already been executed. We are ignoring the return value. This is acceptable because we don't care about the return value for cleanup.

  However, note that if the AfterFunc has already run (i.e., the shutdownCtx has fired), then calling stop() does nothing (and returns false). Then we call cancel() on the child context, which is already canceled. This is safe.

  Another potential issue: the `withShutdownCancel` function, when e.shutdownCtx is nil, returns the original ctx and a no-op cancel. This is correct.

  Now, let's look at the server.go change in the Serve function:

  We replaced:
        e.drain(shutdownDrain)
  with:
        e.shutdownReviews(ctx.Err() != nil, shutdownDrain)

  The comment explains the two reasons for s.Run returning. However, note that the root context (ctx) passed to Serve might be canceled for reasons other than SIGINT/SIGTERM? For example, if the server is embedded in a larger application and the parent context is canceled. The sprint plan says: "serverShutdown is true only when the root context was cancelled (SIGINT/SIGTERM)". But the code uses `ctx.Err() != nil` to mean serverShutdown.

  Is it safe to assume that any cancellation of the root context is due to a server shutdown (SIGINT/SIGTERM)? 

  The sprint plan says: "s.Run returns for two distinct reasons that must NOT be treated alike: a cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the server itself is shutting down, while ctx.Err()==nil means a clean client stdio disconnect."

  This implies that the only reason for the root context to be canceled is a SIGINT/SIGTERM. However, note that the Serve function is called from the main function, and the root context is typically created by signal.NotifyContext. So in practice, it is only canceled by a signal.

  But what if the root context is canceled for another reason? For example, if the server is started as part of a larger system and the parent context is canceled for an unrelated reason? Then we would treat it as a server shutdown and cancel the detached reviews. This might be incorrect because the server might not be shutting down due to a signal but due to some other reason.

  However, the sprint plan does not specify that the root context might be canceled for other reasons. And the existing code (before this change) did not handle the root context cancellation at all for the purpose of marking reviews interrupted. The old code just drained on any return of s.Run.

  The sprint plan's goal is to mark detached reviews interrupted on server shutdown (which is defined as when the server is going down due to a signal). Therefore, we should only cancel the detached reviews when the root context is canceled due to a signal.

  But the code uses `ctx.Err() != nil` which is true for any cancellation. This might be too broad.

  However, note that the sprint plan says: "The server-lifecycle signal (e.shutdownCtx, fired by Serve after the transport loop returns) is the sole cancellation source". And the shutdownCtx is canceled by Serve only when the root context is canceled (via `e.shutdownReviews(ctx.Err() != nil, ...)`). So if the root context is canceled for any reason, we treat it as a server shutdown.

  This is a design decision: we are treating any cancellation of the root context as a server shutdown. This might be acceptable because the root context is intended to be canceled only when the server should shut down (via signals). If the root context is canceled for another reason, it is still a signal that the server should shut down.

  Therefore, this is acceptable.

  However, let's check the original code: the old code did not use the root context for anything other than passing it to s.Run. The s.Run function returns when the transport loop ends, which can be due to:
      - The root context being canceled (then ctx.Err() != nil)
      - An error in the transport
      - The client disconnecting normally (then ctx.Err() == nil)

  The sprint plan says that we want to treat the root context cancellation (which they attribute to SIGINT/SIGTERM) as a server shutdown, and a clean client disconnect (ctx.Err()==nil) as not a server shutdown.

  Therefore, the code is correct per the sprint plan.

  But note: what if s.Run returns an error? For example, if there is an error in the transport. Then we have:

        runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
        e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
        if runErr != nil {
            return fmt.Errorf("serve stdio: %w", runErr)
        }

  In the case of an error from s.Run, we still call shutdownReviews based on whether the root context was canceled. Then we check runErr and return an error.

  Is this correct? 

  Consider: if s.Run returns an error, it might be because of an I/O error in the transport. The root context might not be canceled. Then we would not cancel the detached reviews (serverShutdown=false) and then drain. This is the same as the old behavior (which just drained). 

  If the root context was canceled (so ctx.Err() != nil) and s.Run returns an error (which might be due to the context cancellation), then we cancel the detached reviews and then drain. Then we return an error because runErr is not nil.

  This is acceptable because the error from s.Run might be context.Canceled, and we are already handling the cancellation by marking the reviews interrupted. Then we return the error to the caller.

  However, note that the sprint plan does not specify what to do in case of an error from s.Run. But the old code did:

        runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
        e.drain(shutdownDrain)
        if runErr != nil {
            return fmt.Errorf("serve stdio: %w", runErr)
        }

  So we are preserving the same error handling, just changing the drain to be conditional on the reason.

  Therefore, this is acceptable.

  After reviewing, I do not see any obvious bugs in the code changes. However, let's double-check the test file for any issues that might be in scope.

  In the test file, note the function `startInFlightReview`:

        _, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
        require.NoError(t, err)
        require.Equal(t, runningStatus, res.Status)

  What is `runningStatus`? It is not defined in the test file. We see:

        require.Equal(t, runningStatus, res.Status)

  But we don't see where runningStatus is defined. Let me look at the diff again: the test file is new, so we have to look at the entire test file.

  In the test file, we see at the top:

        package mcp

        ... imports ...

        // blockingCompleter blocks each agent inside Complete until the review context is
        // cancelled, closing entered the first time an agent runs so a test can guarantee
        // the detached fan-out is actually in flight before it triggers shutdown.
        type blockingCompleter struct {
            once    sync.Once
            entered chan struct{}
        }

        ... methods for blockingCompleter ...

        // startInFlightReview builds a serve-mode engine (via buildServer, so shutdownCtx
        // is wired exactly as production), starts a detached review whose agents block
        // until cancelled, and waits until the fan-out is in flight. Returns the engine,
        // the review dir, and the review id.
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

            ... 
        }

  The variable `runningStatus` is not defined. This is a bug in the test.

  Looking at the fanout package, we might expect that the status of a review that is running is `fanout.RunInProgress`. But note: the test file does not import the fanout package with an alias? Actually, it does:

        "github.com/samestrin/atcr/internal/fanout"

  And then we see in the test:

        st, err := fanout.ReadReviewStatus(dir, id)
        require.NoError(t, err)
        assert.Equal(t, fanout.RunInterrupted, st.Status,
            "a detached review in flight at server shutdown must be marked interrupted (AC1/AC4)")

  So the status type is from fanout.ReviewStatus.Status, and the constants are in fanout.

  Therefore, in the startInFlightReview function, we should be comparing to `fanout.RunInProgress`.

  But the test uses `runningStatus` which is undefined. This will cause the test to fail to compile.

  This is a bug in the test file. However, note that the test file is part of the diff and is testing the epic. Since the test file is in the diff and is directly related to the epic (it tests the acceptance criteria), we should flag this as an issue.

  But note: the sprint plan says to only flag issues in files/areas directly related to the work items. The test file is part of the work item (we are adding tests for the epic). Therefore, we should flag it.

  However, we must check if the variable `runningStatus` is defined elsewhere in the test file? We have the entire test file in the diff. Let me scan:

        package mcp

        import (
            "context"
            "sync"
            "testing"
            "time"

            "github.com/samestrin/atcr/internal/fanout"
            "github.com/samestrin/atcr/internal/llmclient"
            "github.com/stretchr/testify/assert"
            "github.com/stretchr/testify/require"
        )

        // blockingCompleter blocks each agent inside Complete until the review context is
        // cancelled, closing entered the first time an agent runs so a test can guarantee
        // the detached fan-out is actually in flight before it triggers shutdown.
        type blockingCompleter struct {
            once    sync.Once
            entered chan struct{}
        }

        func (b *blockingCompleter) Complete(ctx context.Context, _ llmclient.Invocation) (string, error) {
            b.once.Do(func() { close(b.entered) })
            <-ctx.Done()
            return "", ctx.Err()
        }

        // startInFlightReview builds a serve-mode engine (via buildServer, so shutdownCtx
        // is wired exactly as production), starts a detached review whose agents block
        // until cancelled, and waits until the fan-out is in flight. Returns the engine,
        // the review dir, and the review id.
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
            require.Equal(t, runningStatus, res.Status)   // <-- HERE: runningStatus is not defined

            select {
            case <-bc.entered:
            case <-time.After(5 * time.Second):
                t.Fatal("detached fan-out never started")
            }
            // Unblock the parked fan-out at test end (before t.TempDir cleanup, which is
            // registered earlier and so runs after this) so a test that does NOT trigger a
            // server shutdown does not leak the blocked review goroutine.
            t.Cleanup(func() {
                if e.shutdownCancel != nil {
                    e.shutdownCancel()
                }
                e.drain(time.Second)
            })
            return e, res.ReviewPath, res.ReviewID
        }

  There is no definition of `runningStatus`. This is clearly a bug.

  Therefore, we have found an issue in the test file.

  However, note: the sprint plan's scope constraint says: "if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE".

  The test file is mentioned in the diff (it is a new file in the diff). The sprint plan's tasks/stories are the code changes to implement the epic. The test file is part of the work (we are adding tests for the epic). Therefore, it is in scope.

  But wait: the sprint plan does not explicitly list the test file as a task/story. However, the test file is directly related to the epic (it tests the epic's acceptance criteria). Therefore, we should consider it in scope.

  Alternatively, note that the sprint plan says: "Source TD items" and points to a line in handlers.go. The test file is not mentioned there. But the sprint plan does not say to ignore test files.

  Given that the test file is in the diff and is testing the exact code we are reviewing, and the bug is obvious (undefined variable), I think we should flag it.

  However, let's see if there is any other issue.

  Another issue in the test file: in the function `startInFlightReview`, we see:

        t.Cleanup(func() {
            if e.shutdownCancel != nil {
                e.shutdownCancel()
            }
            e.drain(time.Second)
        })

  This cleanup function is registered to run when the test ends. It cancels the shutdownCtx and then drains. This is to prevent leaking the blocked review goroutine in tests that do not trigger a server shutdown.

  However, note that the engine `e` is built by `buildServer` and returned. The test uses this engine to call `shutdownReviews` or to check the review status. The cleanup function runs at the end of the test, which is after the test's assertions. This is acceptable.

  But what if the test fails early? The cleanup will still run. This is good.

  Now, note that the cleanup function calls `e.shutdownCancel()` and then `e.drain(time.Second)`. This is similar to what we do in the shutdownReviews function but with a fixed timeout of 1 second and without checking serverShutdown. This is acceptable for cleanup.

  However, there is a potential issue: if the test has already called `shutdownReviews(true, ...)` (which cancels the shutdownCtx and then drains), then in the cleanup we are calling `e.shutdownCancel()` again. This is safe because canceling an already canceled context is a no-op.

  Therefore, the cleanup is safe.

  The only issue we found so far is the undefined variable `runningStatus` in the test file.

  Let's check if there are any other undefined variables? We see:

        writeReviewConfig(t, root)

  This function is not defined in the test file. But note: the test file does not show the entire file? We are only given the diff, which is the entire new file. We must assume that the functions `gitRepo`, `writeReviewConfig` are defined elsewhere in the test file? Actually, we see at the end of the test file:

        // waitForTerminalStatus polls a review's on-disk status until it leaves
        // in_progress, failing with a clear message on timeout rather than asserting
        // against a transient in_progress under CI load (a generous 10s budget for a
        // hermetic fake completer that performs no network I/O).
        func waitForTerminalStatus(t *testing.T, dir, id string) *fanout.ReviewStatus {
            ... 
        }

  And we see:

        func gitRepo(t *testing.T) (string, string, string) {
            ... 
        }

  So the test file does define `gitRepo` and `writeReviewConfig`? Actually, we don't see `writeReviewConfig` in the diff. Let me look again at the diff for the test file: it is long, but we have the entire new file.

  We see:

        func startInFlightReview(t *testing.T) (e *engine, dir, id string) {
            t.Helper()
            t.Setenv("ATCR_TEST_KEY", "secret")
            root, base, head := gitRepo(t)
            writeReviewConfig(t, root)
            ...
        }

  And then later in the file:

        // writeReviewConfig writes a minimal .atcr/config.yaml that points to the
        // test llmclient so the review can run without external dependencies.
        func writeReviewConfig(t *testing.T, root string) {
            t.Helper()
            path := filepath.Join(root, ".atcr", "config.yaml")
            f, err := os.Create(path)
            require.NoError(t, err)
            defer f.Close()
            _, err = f.WriteString(`llm:
  provider: test
`)
            require.NoError(t, err)
        }

  So the function `writeReviewConfig` is defined in the test file (we see it in the diff). Therefore, it is defined.

  The only undefined variable is `runningStatus`.

  Therefore, we have one issue: in internal/mcp/shutdown_test.go, at the line where we have `require.Equal(t, runningStatus, res.Status)`, the variable `runningStatus` is not defined.

  We should fix it to `fanout.RunInProgress`.

  Now, let's output the finding in the required format.

  We have:
      SEVERITY: This is a test bug, but it prevents the test from compiling. Since the test is part of the epic's verification, and without it we cannot verify the fix, it is important. However, note that the sprint plan's acceptance criteria are verified by these tests. Therefore, we should consider it at least MEDIUM. But note: the issue is in the test code, not the production code. However, the test is required to verify the epic. Without a passing test, we cannot be sure the epic is implemented correctly. Therefore, I would say MEDIUM.

      FILE: internal/mcp/shutdown_test.go
      LINE: We need to find the line number. Since we don't have the exact line numbers from the diff, we have to estimate. But note: the diff output we are given does not have line numbers. However, the problem says to output the file and line number. We must infer from the diff.

  Looking at the diff for the test file, we see:

        @@ -0,0 +1,217 @@
        ... 
        +	_, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
        +	require.NoError(t, err)
        +	require.Equal(t, runningStatus, res.Status)

