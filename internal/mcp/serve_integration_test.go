package mcp

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startServeWithInFlightReview wires the production serve path end-to-end: it
// builds a server+engine via buildServer (so shutdownCtx is exactly as in
// production), starts a detached review whose agents block until cancelled,
// waits until that fan-out is in flight, then runs serveOver in a goroutine over
// an in-memory transport. It returns the transport's client peer (Close it to
// simulate a clean stdio disconnect), a channel carrying serveOver's eventual
// return, the review dir/id for on-disk status reads, the engine, and the cancel
// for the server's root ctx (call it to simulate a SIGINT-cancelled context). A
// t.Cleanup unblocks the parked review so a test that interrupts neither path
// does not leak the fan-out goroutine.
//
// The client peer is a RAW transport connection — the MCP initialize handshake is
// never completed. These tests drive the server's lifecycle directly (root-ctx
// cancel = SIGINT, peer Close = disconnect) rather than through protocol traffic,
// which is exactly the post-transport seam serveOver isolates.
func startServeWithInFlightReview(t *testing.T) (peer mcpsdk.Connection, serveDone <-chan error, dir, id string, e *engine, cancel context.CancelFunc) {
	t.Helper()
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)

	bc := &blockingCompleter{entered: make(chan struct{})}
	s, eng, err := buildServer(root, bc, nil)
	require.NoError(t, err)

	_, res, err := eng.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head})
	require.NoError(t, err)
	require.Equal(t, runningStatus, res.Status)

	select {
	case <-bc.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("detached fan-out never started")
	}

	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	clientPeer, err := clientTransport.Connect(context.Background())
	require.NoError(t, err)

	ctx, ctxCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveOver(ctx, s, eng, serverTransport) }()

	t.Cleanup(func() {
		_ = clientPeer.Close()
		if eng.shutdownCancel != nil {
			eng.shutdownCancel()
		}
		eng.drain(time.Second)
		ctxCancel()
	})
	return clientPeer, done, res.ReviewPath, res.ReviewID, eng, ctxCancel
}

// TestServeOver_SIGINTInterruptsInFlightDetachedReview locks the crux epic-4.1.2
// wiring end-to-end through serveOver (what Serve runs in production): when the
// server's root ctx is cancelled (SIGINT/SIGTERM) while a detached review is in
// flight, the transport loop returns with ctx.Err()!=nil and the review is marked
// interrupted on disk (AC1/AC4). The existing shutdown_test.go suite only calls
// e.shutdownReviews(...) with a hand-picked boolean — none drives the ctx.Err() !=
// nil expression that computes that boolean. (See the constant-substitution
// mutation guarded by this test and its clean-disconnect sibling.)
func TestServeOver_SIGINTInterruptsInFlightDetachedReview(t *testing.T) {
	_, serveDone, dir, id, _, cancel := startServeWithInFlightReview(t)

	cancel() // SIGINT: root ctx cancelled → s.Run returns with ctx.Err() != nil

	select {
	case <-serveDone:
	case <-time.After(10 * time.Second):
		t.Fatal("serveOver did not return after root ctx cancellation")
	}

	st, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	assert.Equal(t, fanout.RunInterrupted, st.Status,
		"a detached review in flight when Serve's root ctx is cancelled (SIGINT) must be interrupted (epic 4.1.2 AC1); this exercises the ctx.Err() != nil argument end-to-end")
}

// TestServeOver_CleanDisconnectLeavesInFlightReviewRunning locks AC3 end-to-end:
// a clean transport disconnect (the peer closes while the root ctx stays alive)
// makes s.Run return with ctx.Err()==nil, so the in-flight detached review must
// NOT be force-interrupted. Together with the SIGINT sibling this pins the
// ctx.Err() != nil expression: replacing it with the constant true fails this
// test, replacing it with false fails the SIGINT test.
func TestServeOver_CleanDisconnectLeavesInFlightReviewRunning(t *testing.T) {
	peer, serveDone, dir, id, _, cancel := startServeWithInFlightReview(t)
	defer cancel()

	require.NoError(t, peer.Close()) // clean stdio disconnect, ctx still alive

	select {
	case <-serveDone:
	case <-time.After(10 * time.Second):
		t.Fatal("serveOver did not return after peer disconnect")
	}

	st, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	assert.NotEqual(t, fanout.RunInterrupted, st.Status,
		"a clean transport disconnect (root ctx alive) must not force-interrupt an in-flight review (epic 4.1.2 AC3); this exercises ctx.Err()==nil end-to-end")
}
