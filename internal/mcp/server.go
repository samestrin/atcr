package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/fanout"
)

// Version is the server version advertised in the MCP initialize handshake.
const Version = "1.0.0"

// shutdownDrain bounds how long Serve waits for in-flight background reviews to
// finish their on-disk writes after the client disconnects, so a near-complete
// review is not orphaned mid-write. A review still running past this is
// abandoned (the process exits); clients re-run with a fresh id.
//
// On the SIGINT path (epic 4.1.2 AC1) this same bound also caps the
// interrupted-marker flush: after shutdownCancel unwinds blocked agents,
// ExecuteReview must complete WritePool + WriteManifest within this window for the
// on-disk interrupted status to persist before process exit. It must therefore
// comfortably exceed worst-case flush latency (slow disk, many agents, a completer
// slow to honor ctx.Done()); if it does not, a genuinely-interrupted review can be
// left in_progress — degraded-but-safe (never a false completed), but the AC1
// marker is lost. Raise this (or block the SIGINT path on e.bg.Wait() with a
// larger bound) before trusting the marker under heavier flush load.
const shutdownDrain = 5 * time.Second

// NewServer constructs the atcr MCP server with all seven tools registered
// against a shared engine rooted at root. completer drives the fan-out (the
// real *llmclient.Client in production, a fake in tests); logger receives all
// diagnostics and MUST write to stderr in serve mode (stdout is owned by the
// protocol). A nil logger discards output.
//
// Registration is fail-fast: a duplicate tool name or a schema-generation
// failure returns an error so `atcr serve` exits 1 at startup rather than
// panicking or serving an ambiguous tool set (AC 04-02 Edge Case 1, Error
// Scenario 1).
func NewServer(root string, completer fanout.Completer, logger *slog.Logger) (*mcpsdk.Server, error) {
	s, _, err := buildServer(root, completer, logger)
	return s, err
}

// Serve builds the server, runs it over stdio until the client disconnects or
// ctx is cancelled, then drains in-flight background reviews (bounded) so a
// disconnecting client does not orphan a review mid-write. It is the single
// entry the `atcr serve` command calls.
func Serve(ctx context.Context, root string, completer fanout.Completer, logger *slog.Logger) error {
	s, e, err := buildServer(root, completer, logger)
	if err != nil {
		return err
	}
	return serveOver(ctx, s, e, &mcpsdk.StdioTransport{})
}

// serveOver runs s over transport until the transport loop returns, then shuts
// down in-flight detached reviews according to WHY it returned. Split from Serve
// solely so a test can exercise this post-transport discrimination over an
// in-memory transport — the StdioTransport Serve passes in production binds
// os.Stdin/os.Stdout and is not drivable from a test, so the crux epic-4.1.2
// wiring (the ctx.Err() != nil argument below) would otherwise be untested
// end-to-end.
//
// s.Run returns for two distinct reasons that must NOT be treated alike: a
// cancelled root ctx (SIGINT/SIGTERM via the CLI signal handler) means the
// server itself is shutting down, while ctx.Err()==nil means a clean client
// stdio disconnect. Only the former interrupts in-flight detached reviews.
func serveOver(ctx context.Context, s *mcpsdk.Server, e *engine, transport mcpsdk.Transport) error {
	runErr := s.Run(ctx, transport)
	e.shutdownReviews(ctx.Err() != nil, shutdownDrain)
	if runErr != nil {
		return fmt.Errorf("serve stdio: %w", runErr)
	}
	return nil
}

// shutdownReviews ends in-flight detached reviews according to WHY the transport
// loop returned, then drains. serverShutdown is true only when the root context
// was cancelled (SIGINT/SIGTERM): the server is going down, so in-flight detached
// reviews are cancelled BEFORE the drain and record interrupted (epic 4.1.2 AC1),
// mirroring the CLI's SIGINT path; the drain then waits for those flush writes.
// On a clean client/stdio disconnect (serverShutdown false) the reviews are left
// running so the drain keeps its original contract — let a near-complete review
// FINISH its writes rather than orphan or force-interrupt it (AC3).
func (e *engine) shutdownReviews(serverShutdown bool, timeout time.Duration) {
	if serverShutdown && e.shutdownCancel != nil {
		e.logger().Warn("server shutdown: cancelling in-flight detached reviews")
		e.shutdownCancel()
	}
	e.drain(timeout)
}

// buildServer wires the engine and registers the seven tools, returning both the
// SDK server and the engine (so Serve can drain its background reviews).
func buildServer(root string, completer fanout.Completer, logger *slog.Logger) (*mcpsdk.Server, *engine, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	// Server-lifecycle cancellation: cancelled by Serve once the transport loop
	// returns (client disconnect or SIGINT-cancelled ctx). Detached reviews tie
	// their cancellation to shutdownCtx via withShutdownCancel, so a server
	// shutdown marks in-flight reviews interrupted without a handler return ever
	// aborting them (epic 4.1.2). The NewServer path discards the engine and never
	// fires shutdownCancel; context.WithCancel(Background) spawns no goroutine, so
	// the unfired cancel is reclaimed with the engine.
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	e := &engine{root: root, completer: completer, log: logger, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel}

	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
	r := &registrar{server: s, seen: map[string]bool{}}

	registerTool(r, &mcpsdk.Tool{Name: ToolReview, Description: descReview}, e.handleReview)
	registerTool(r, &mcpsdk.Tool{Name: ToolReconcile, Description: descReconcile}, e.handleReconcile)
	registerTool(r, &mcpsdk.Tool{Name: ToolVerify, Description: descVerify}, e.handleVerify)

	reportSchema, err := reportInputSchema()
	if err != nil {
		return nil, nil, fmt.Errorf("building %s schema: %w", ToolReport, err)
	}
	registerTool(r, &mcpsdk.Tool{Name: ToolReport, Description: descReport, InputSchema: reportSchema}, e.handleReport)

	registerTool(r, &mcpsdk.Tool{Name: ToolRange, Description: descRange}, e.handleRange)
	registerTool(r, &mcpsdk.Tool{Name: ToolStatus, Description: descStatus}, e.handleStatus)
	// atcr_metrics takes no arguments (MetricsArgs is an empty struct), so unlike
	// atcr_report it needs no explicit input schema — the SDK infers an empty one.
	// handleMetrics checks ctx once up front; revisit mid-render ctx handling only
	// if labeled-metric cardinality grows enough to make a render non-trivial.
	registerTool(r, &mcpsdk.Tool{Name: ToolMetrics, Description: descMetrics}, e.handleMetrics)

	if r.err != nil {
		return nil, nil, r.err
	}
	return s, e, nil
}

// registrar accumulates tool registrations, failing fast on the first error.
type registrar struct {
	server *mcpsdk.Server
	seen   map[string]bool
	err    error
}

// registerTool adds one generic typed tool to the server. It converts both our
// duplicate-name guard and the SDK's panic-on-bad-schema into a recorded error
// so NewServer can fail fast without a panic escaping to the caller. Once an
// error is recorded, subsequent calls are no-ops.
func registerTool[In, Out any](r *registrar, t *mcpsdk.Tool, h mcpsdk.ToolHandlerFor[In, Out]) {
	if r.err != nil {
		return
	}
	if r.seen[t.Name] {
		r.err = fmt.Errorf("failed to register tool %s: duplicate tool name", t.Name)
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.err = fmt.Errorf("failed to register tool %s: %v", t.Name, rec)
		}
	}()
	mcpsdk.AddTool(r.server, t, h)
	r.seen[t.Name] = true
}
