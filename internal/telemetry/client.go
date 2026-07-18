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
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// defaultRequestTimeout bounds the background telemetry request's own lifetime — never
// the caller's, which returns as soon as the goroutine is dispatched.
const defaultRequestTimeout = 3 * time.Second

// maxInFlightSends caps the number of concurrent background send goroutines. Client
// is an exported reusable type; a future caller invoking Send in a tight loop against
// a slow/hung endpoint (each send lives up to requestTimeout) would otherwise
// accumulate unbounded goroutines. The cap is well above any realistic legitimate
// burst (review + reconcile fire a handful), so it never drops in normal use; it only
// bounds a pathological caller. Excess sends are dropped — the ping is best-effort —
// never queued or blocked, so Send stays non-blocking.
const maxInFlightSends = 64

// doRequest performs the outbound POST. Stored in an atomic.Value so tests can
// force a panic inside the goroutine body and assert the deferred recover
// swallows it (AC 01-03) without racing the detached send goroutine.
// Production always uses the real client.Do.
type doRequestFunc func(*http.Client, *http.Request) (*http.Response, error)

var doRequest atomic.Value

func init() {
	doRequest.Store(doRequestFunc(func(client *http.Client, req *http.Request) (*http.Response, error) {
		return client.Do(req)
	}))
}

func currentDoRequest() doRequestFunc {
	return doRequest.Load().(doRequestFunc)
}

// SetDoRequestForTest overrides the outbound-request seam and returns a restore
// func. It exists so tests in OTHER packages (e.g. cmd/atcr's opt-out gate
// end-to-end tests) can count or intercept sends across the package boundary
// without real networking; in-package tests mutate doRequest directly.
// Production code never calls this.
func SetDoRequestForTest(fn func(*http.Client, *http.Request) (*http.Response, error)) func() {
	prev := currentDoRequest()
	doRequest.Store(doRequestFunc(fn))
	return func() { doRequest.Store(prev) }
}

// Client sends anonymous usage events to a configured HTTPS endpoint. Construct
// one per process via New and inject it (it is deliberately not a package-level
// singleton); a nil Client or an empty/non-HTTPS endpoint makes Send a no-op.
type Client struct {
	endpoint       string
	httpClient     *http.Client
	wg             sync.WaitGroup
	sem            chan struct{} // bounds concurrent send goroutines (see maxInFlightSends)
	requestTimeout time.Duration
}

// New returns a Client that POSTs events to endpoint. An empty endpoint yields a
// no-op client (Send never spawns a goroutine or touches the network) — the
// documented Phase-2 default until a real ingestion backend is configured. A
// configured endpoint MUST be an https:// URL; plaintext http is refused (no-op).
func New(endpoint string) *Client {
	// A dedicated client (not http.DefaultClient) so telemetry's connection pool
	// and Transport are isolated from the rest of the process.
	return &Client{
		endpoint:       endpoint,
		httpClient:     &http.Client{},
		sem:            make(chan struct{}, maxInFlightSends),
		requestTimeout: defaultRequestTimeout,
	}
}

// isHTTPS reports whether endpoint is a well-formed https URL (case-insensitive
// scheme). An empty, malformed, or plaintext-http endpoint is refused, so Send
// no-ops rather than ever sending in the clear.
func isHTTPS(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && strings.EqualFold(u.Scheme, "https") && u.Host != ""
}

// Send fires ev to the endpoint on a detached goroutine and returns immediately.
// It is a no-op when the client is nil, the endpoint is empty, or the endpoint is
// not HTTPS. Every failure mode — non-2xx, network error, marshal error, or an
// internal panic — is logged at debug level (never a level that alarms an end
// user about an opt-in background feature) and swallowed: Send has no error
// return and never affects the caller's outcome or exit code. The usage Event is
// marshaled compactly, preserving its existing wire format.
func (c *Client) Send(ctx context.Context, ev Event) {
	c.dispatch(ctx, func() ([]byte, error) { return json.Marshal(ev) })
}

// SendQualitySignal fires the community prompt quality-signal payload (Sprint 30.0)
// on the SAME detached, fail-open, HTTPS-only, nil/empty-endpoint-no-op, panic-safe
// path as Send — it is a sibling for the allowlisted []QualitySignal payload, never
// an extension of Event. The payload is marshaled with the SAME indentation the
// `atcr … --preview` surface renders (json.MarshalIndent with a two-space indent),
// so the transmitted bytes are byte-identical to the preview for the same data
// (AC 06-02); a byte-for-byte equivalence test locks the two paths together.
// A nil or empty payload is a no-op short-circuit BEFORE dispatch: the exported
// API is self-defending (no semaphore slot, goroutine, or contentless beacon)
// rather than depending on every caller pre-checking len(payload)==0.
func (c *Client) SendQualitySignal(ctx context.Context, payload []QualitySignal) {
	if len(payload) == 0 {
		return
	}
	c.dispatch(ctx, func() ([]byte, error) { return json.MarshalIndent(payload, "", "  ") })
}

// dispatch is the shared fail-open send core: it no-ops on a nil client or a
// non-HTTPS/empty endpoint, bounds concurrent goroutines via the in-flight
// semaphore, and hands the marshal closure to the detached send goroutine. Both
// Send and SendQualitySignal funnel through it so the goroutine/timeout/recover
// contract has a single implementation; only the marshaling differs per payload.
func (c *Client) dispatch(ctx context.Context, marshal func() ([]byte, error)) {
	if c == nil || !isHTTPS(c.endpoint) {
		return
	}
	// Non-blocking acquire: if maxInFlightSends are already running, drop this ping
	// (best-effort) rather than block the caller or spawn an unbounded goroutine.
	select {
	case c.sem <- struct{}{}:
	default:
		log.FromContext(ctx).Debug("telemetry: send dropped (in-flight cap reached)")
		return
	}
	c.wg.Add(1)
	go c.send(ctx, marshal)
}

func (c *Client) send(ctx context.Context, marshal func() ([]byte, error)) {
	defer c.wg.Done()
	defer func() { <-c.sem }() // release the in-flight slot acquired in dispatch
	defer func() {
		if r := recover(); r != nil {
			log.FromContext(ctx).Debug("telemetry: recovered from panic", "value", r)
		}
	}()

	body, err := marshal()
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: marshal failed", "error", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: build request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := currentDoRequest()(c.httpClient, req)
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: send failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.FromContext(ctx).Debug("telemetry: non-2xx response", "status", resp.StatusCode)
	}
	// Drain up to 64KB so the keep-alive connection is reused for the small
	// acks telemetry receives; a body larger than the cap is only partially
	// read and the connection is NOT reused — the cap intentionally trades
	// reuse on oversized bodies for a bounded read.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
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
