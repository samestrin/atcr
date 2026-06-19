package mcp

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
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
	return "", context.Canceled
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
	require.Equal(t, runningStatus, res.Status)

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

// TestServeShutdown_MarksInFlightDetachedReviewInterrupted is the core epic 4.1.2
// guarantee (AC1/AC4): a detached MCP review in flight when the server is shutting
// down — the exact sequence Serve runs after a SIGINT-cancelled transport loop
// returns — is marked interrupted on disk, never left in_progress and never
// reported as a clean completion.
func TestServeShutdown_MarksInFlightDetachedReviewInterrupted(t *testing.T) {
	e, dir, id := startInFlightReview(t)

	e.shutdownReviews(true, shutdownDrain) // serverShutdown=true: SIGINT path

	st, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	assert.Equal(t, fanout.RunInterrupted, st.Status,
		"a detached review in flight at server shutdown must be marked interrupted (AC1/AC4)")
}

// TestServeShutdown_ClientDisconnectDoesNotInterrupt locks AC3 and the
// independent-review regression: a clean client/stdio disconnect (ctx NOT
// cancelled) must NOT force-cancel an in-flight detached review. The drain keeps
// its original contract — the review is left running, so it is never flipped to
// interrupted by a mere disconnect.
func TestServeShutdown_ClientDisconnectDoesNotInterrupt(t *testing.T) {
	e, dir, id := startInFlightReview(t)

	// serverShutdown=false: client disconnect. Short drain — the blocked review
	// stays running, so it must remain in_progress, never interrupted.
	e.shutdownReviews(false, 200*time.Millisecond)

	st, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	assert.NotEqual(t, fanout.RunInterrupted, st.Status,
		"a clean client disconnect must not force-interrupt an in-flight review (AC3)")
}

// TestServeShutdown_NormalCompletionNotInterrupted locks AC2/AC4: a detached
// review that finishes on its own (handler already returned, no shutdown) is
// recorded completed, and a later server shutdown does not retroactively flip a
// finished review to interrupted.
func TestServeShutdown_NormalCompletionNotInterrupted(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)

	_, e, err := buildServer(root, fakeCompleter{resp: validFindings}, nil)
	require.NoError(t, err)

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
}

// waitForTerminalStatus polls a review's on-disk status until it leaves
// in_progress, failing with a clear message on timeout rather than asserting
// against a transient in_progress under CI load (a generous 10s budget for a
// hermetic fake completer that performs no network I/O).
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

// TestWithShutdownCancel_CancelsDetachedCtxOnServerShutdown verifies the engine
// derives a review context that is cancelled when the server-lifecycle context is
// cancelled (Serve fires e.shutdownCancel on shutdown). This is what lets
// ExecuteReview's existing ctx.Err()==Canceled marker record Interrupted=true for
// a detached MCP review (epic 4.1.2 AC1).
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

// TestWithShutdownCancel_ParentReturnDoesNotCancel verifies a normal handler
// return — the request context being cancelled — does NOT cancel the detached
// review context (epic 4.1.2 AC2: detachment preserved). The detached base is
// derived via context.WithoutCancel, so request cancellation must not propagate
// to the running fan-out.
func TestWithShutdownCancel_ParentReturnDoesNotCancel(t *testing.T) {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	e := &engine{shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

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
}

// TestWithShutdownCancel_NilShutdownCtxIsNoop verifies the guard for engines
// constructed without buildServer (direct &engine{} in tests, or the NewServer
// path that discards the engine): withShutdownCancel returns the context
// unchanged and a no-op cancel, never panicking on a nil shutdownCtx.
func TestWithShutdownCancel_NilShutdownCtxIsNoop(t *testing.T) {
	e := &engine{} // no shutdownCtx
	rctx, cancel := e.withShutdownCancel(context.Background())
	defer cancel()

	select {
	case <-rctx.Done():
		t.Fatal("ctx unexpectedly cancelled with no shutdown context")
	default:
	}
}

// TestServeShutdown_LogsWarnOnInterrupt verifies a detached review cancelled by
// server shutdown emits a structured Warn for greppability parity with the CLI
// "review interrupted by signal" log (handlers.go:231 TD item).
func TestServeShutdown_LogsWarnOnInterrupt(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)

	var logBuf bytes.Buffer
	logger, err := log.New("warn", "text", &logBuf)
	require.NoError(t, err)

	bc := &blockingCompleter{entered: make(chan struct{})}
	_, e, err := buildServer(root, bc, logger)
	require.NoError(t, err)

	_, res, err := e.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
	require.NoError(t, err)
	require.Equal(t, runningStatus, res.Status)

	select {
	case <-bc.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("detached fan-out never started")
	}
	t.Cleanup(func() {
		if e.shutdownCancel != nil {
			e.shutdownCancel()
		}
		e.drain(time.Second)
	})

	e.shutdownReviews(true, shutdownDrain)

	assert.Contains(t, logBuf.String(), "review interrupted by server shutdown",
		"shutdown-interrupted MCP review must emit structured Warn for log greppability")
}

// TestShutdownReviews_LogsWarnOnServerShutdown verifies shutdownReviews emits a
// structured Warn when serverShutdown is true, making serve-side cancellation
// diagnosable from logs (server.go:48 TD item).
func TestShutdownReviews_LogsWarnOnServerShutdown(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := log.New("warn", "text", &logBuf)
	require.NoError(t, err)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	e := &engine{log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

	e.shutdownReviews(true, 0)

	assert.Contains(t, logBuf.String(), "server shutdown: cancelling in-flight detached reviews",
		"shutdownReviews(serverShutdown=true) must emit Warn for diagnosability")
}
