// Package telemetry provides a fire-and-forget, panic-safe HTTP client for
// anonymous usage pings. It is opt-in and fails open: a network failure, a hung
// endpoint, a non-2xx response, or an internal panic never blocks, crashes, or
// changes the exit code of the CLI command that emitted the ping. An empty (or
// non-HTTPS) endpoint makes every Send a no-op — the seam the opt-out gate
// (Story 2) reuses.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// requestTimeout bounds the background telemetry request's own lifetime — never
// the caller's, which returns as soon as the goroutine is dispatched. A package
// var only so tests can shrink it, mirroring the gracefulShutdownTimeout seam in
// cmd/atcr/main.go.
var requestTimeout = 3 * time.Second

// doRequest performs the outbound POST. A package var so tests can force a panic
// inside the goroutine body and assert the deferred recover swallows it
// (AC 01-03); production always uses the real client.Do.
var doRequest = func(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// Client sends anonymous usage events to a configured HTTPS endpoint. Construct
// one per process via New and inject it (it is deliberately not a package-level
// singleton); a nil Client or an empty/non-HTTPS endpoint makes Send a no-op.
type Client struct {
	endpoint   string
	httpClient *http.Client
	wg         sync.WaitGroup
}

// New returns a Client that POSTs events to endpoint. An empty endpoint yields a
// no-op client (Send never spawns a goroutine or touches the network) — the
// documented Phase-2 default until a real ingestion backend is configured. A
// configured endpoint MUST be an https:// URL; plaintext http is refused (no-op).
func New(endpoint string) *Client {
	return &Client{endpoint: endpoint, httpClient: http.DefaultClient}
}

// Send fires ev to the endpoint on a detached goroutine and returns immediately.
// It is a no-op when the client is nil, the endpoint is empty, or the endpoint is
// not HTTPS. Every failure mode — non-2xx, network error, marshal error, or an
// internal panic — is logged at debug level (never a level that alarms an end
// user about an opt-in background feature) and swallowed: Send has no error
// return and never affects the caller's outcome or exit code.
func (c *Client) Send(ctx context.Context, ev Event) {
	if c == nil || !strings.HasPrefix(c.endpoint, "https://") {
		return
	}
	c.wg.Add(1)
	go c.send(ctx, ev)
}

func (c *Client) send(ctx context.Context, ev Event) {
	defer c.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.FromContext(ctx).Debug("telemetry: recovered from panic", "value", r)
		}
	}()

	body, err := json.Marshal(ev)
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: marshal failed", "error", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: build request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := doRequest(c.httpClient, req)
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: send failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.FromContext(ctx).Debug("telemetry: non-2xx response", "status", resp.StatusCode)
	}
}

// Wait blocks until all in-flight sends complete. Intended for deterministic
// tests and graceful-shutdown drain; production callers fire-and-forget and
// never call it. Safe on a nil Client.
func (c *Client) Wait() {
	if c == nil {
		return
	}
	c.wg.Wait()
}

// ctxKey is the unexported context key under which the process telemetry client
// is carried, so runReview/runReconcile can retrieve it without a signature change.
type ctxKey struct{}

// NewContext returns ctx carrying c. newRootCmd injects the single process client
// here (in PersistentPreRunE) so every subcommand inherits it.
func NewContext(ctx context.Context, c *Client) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the Client stored in ctx, or nil if none was injected. A
// nil Client's Send is a safe no-op, so callers need not nil-check the result.
func FromContext(ctx context.Context) *Client {
	c, _ := ctx.Value(ctxKey{}).(*Client)
	return c
}
