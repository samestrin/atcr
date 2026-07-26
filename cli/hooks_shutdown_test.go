package cli

import (
	"context"
	"io"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownSignal_AbsentIsNilAndBlocks pins the degradation property the API
// depends on: a consumer that selects on this channel must not change behavior
// when the signal is absent (an older atcr, or a context that never went
// through runMain). A nil channel blocks forever in select, so such a consumer
// falls back to waiting rather than to abandoning its work immediately.
func TestShutdownSignal_AbsentIsNilAndBlocks(t *testing.T) {
	ch := ShutdownSignal(context.Background())
	assert.Nil(t, ch, "a context that carries no signal must report nil")

	fired := false
	select {
	case <-ch:
		fired = true
	case <-time.After(20 * time.Millisecond):
	}
	assert.False(t, fired, "a nil channel must block, never fire")
}

// TestShutdownSignal_OpenUntilShutdown verifies the channel is present but NOT
// closed on a healthy run. If it were readable before a signal, every consumer
// bounding cleanup on it would abandon work on a run that is not shutting down.
func TestShutdownSignal_OpenUntilShutdown(t *testing.T) {
	ctx := withShutdownSignal(context.Background(), (<-chan struct{})(make(chan struct{})))
	ch := ShutdownSignal(ctx)
	require.NotNil(t, ch)

	select {
	case <-ch:
		t.Fatal("the shutdown signal must stay open until a signal actually arrives")
	case <-time.After(20 * time.Millisecond):
	}
}

// TestShutdownSignal_SurvivesDerivedContexts is the load-bearing case. The
// observer receives the fan-out engine's per-agent context, which is several
// WithCancel/WithTimeout derivations below the one runMain built. A value that
// did not survive that trip would leave every consumer reading nil.
func TestShutdownSignal_SurvivesDerivedContexts(t *testing.T) {
	raw := make(chan struct{})
	ctx := withShutdownSignal(context.Background(), (<-chan struct{})(raw))

	derived, cancel := context.WithCancel(ctx)
	defer cancel()
	derived, cancel2 := context.WithTimeout(derived, time.Minute)
	defer cancel2()

	ch := ShutdownSignal(derived)
	require.NotNil(t, ch, "the signal must survive derivation")

	close(raw)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("closing the source must be observable through the derived context")
	}
}

// TestHandleSignals_ClosesShutdownBeforeGrace verifies the wiring runMain uses:
// the channel closes when a signal arrives, at the same point the root context
// is cancelled, and before the grace period elapses.
//
// It uses the package's stubForceExit helper with a short grace so the handler
// goroutine finishes promptly, and then WAITS for it to finish. Both matter: the
// goroutine reads gracefulShutdownTimeout and forceExit after cancelling, so a
// test that returned while it was still parked would let t.Cleanup restore those
// package vars underneath a live reader — a data race, not a flake.
func TestHandleSignals_ClosesShutdownBeforeGrace(t *testing.T) {
	code := stubForceExit(t, 15*time.Millisecond)

	shutdownCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	handleSignals(sigCh, func() {
		close(shutdownCh)
		cancel()
	}, io.Discard)

	select {
	case <-shutdownCh:
		t.Fatal("the signal must not be closed before a signal arrives")
	case <-time.After(20 * time.Millisecond):
	}

	sigCh <- syscall.SIGINT

	select {
	case <-shutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("a signal must close the shutdown channel")
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a signal must also cancel the root context")
	}

	// Drain the handler goroutine before returning, so cleanup cannot restore
	// forceExit/gracefulShutdownTimeout while it is still reading them.
	require.Eventually(t, func() bool { return atomic.LoadInt32(code) == 1 },
		2*time.Second, 5*time.Millisecond,
		"the grace period should elapse and force exit once the stub timeout passes")
}
