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
const shutdownDrain = 5 * time.Second

// NewServer constructs the atcr MCP server with all five tools registered
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
	runErr := s.Run(ctx, &mcpsdk.StdioTransport{})
	e.drain(shutdownDrain)
	if runErr != nil {
		return fmt.Errorf("serve stdio: %w", runErr)
	}
	return nil
}

// buildServer wires the engine and registers the five tools, returning both the
// SDK server and the engine (so Serve can drain its background reviews).
func buildServer(root string, completer fanout.Completer, logger *slog.Logger) (*mcpsdk.Server, *engine, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &engine{root: root, completer: completer, log: logger}

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
