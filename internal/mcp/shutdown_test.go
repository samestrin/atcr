package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
