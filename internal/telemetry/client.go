// Package telemetry provides a fire-and-forget, panic-safe HTTP client for
// anonymous usage pings. The ping is default-enabled and opt-out (ATCR_TELEMETRY
// or `atcr config set telemetry false` disables it) and fails open: a network
// failure, a hung endpoint, a non-2xx response, or an internal panic never
// blocks, crashes, or changes the exit code of the CLI command that emitted the
// ping. An empty (or non-HTTPS) endpoint makes every Send a no-op — the seam the
// opt-out gate (Story 2) reuses. (The community quality signal is the inverse:
// opt-in and off by default — see cli/telemetry.go.)
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/version"
)

// defaultRequestTimeout bounds the background telemetry request's own lifetime — never
// the caller's, which returns as soon as the goroutine is dispatched.
//
// It is deliberately equal to (never greater than) the drain bound the production
// caller enforces at exit — cli's telemetryDrainTimeout, currently 2s. The two are
// budgets for the same send viewed from opposite ends, so a per-request budget that
// outlives the drain bound creates a dead window: at 3s against a 2s drain, a send
// completing between 2s and 3s was guaranteed to be abandoned at exit even though
// its own budget had not expired. The extra second could not produce a delivery; it
// only widened the gap between what the request believed was still possible and what
// the process had already given up on.
//
// Raising this above the caller's bound reopens that window.
// TestDefaultRequestTimeout_FitsWithinCallerDrainBound is the tripwire.
const defaultRequestTimeout = 2 * time.Second

// maxInFlightSends caps the number of concurrent background send goroutines PER
// destination: each surface (usage ping, quality signal) gets its own semaphore, so
// a slow/hung destination holding slots for up to requestTimeout can never starve
// the sibling surface — the same independence the per-endpoint gate guarantees.
// Client is an exported reusable type; a future caller invoking Send in a tight loop
// against a slow/hung endpoint (each send lives up to requestTimeout) would otherwise
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
// one per process via NewWithQualitySignal (or NewSingleDestination in tests) and inject it (it is
// deliberately not a package-level singleton); a nil Client or an empty/non-HTTPS
// endpoint makes the corresponding Send a no-op.
//
// The two payload surfaces carry SEPARATE, independently configurable
// destinations, and each is a no-op when empty — which is what lets them be
// activated or deactivated on independent schedules.
//
// The reasons the destinations must differ live with the service that imposes
// them, in docs/telemetry.md. This package has no dependency on, test against, or
// visibility into that service, so a contract restated here could never be
// detected going stale — it would simply rot into a confident lie in a transport
// leaf. What this package can state, and enforce, is the shape above.
type Client struct {
	endpoint        string
	qualityEndpoint string
	httpClient      *http.Client
	wg              sync.WaitGroup
	sem             chan struct{} // bounds concurrent usage-ping sends (see maxInFlightSends)
	qualitySem      chan struct{} // bounds concurrent quality-signal sends (see maxInFlightSends)
	requestTimeout  time.Duration
}

// NewSingleDestination returns a Client that POSTs BOTH the usage ping and the
// quality signal to one endpoint.
//
// THE NAME IS THE POINT. This has no production callers — every real site uses
// NewWithQualitySignal — and it exists for tests, which discriminate the two
// surfaces by body shape at the transport rather than by URL. It was called
// `New`, which made the shortest-to-type, most-default-looking constructor in the
// package the one that collapses two destinations into one: precisely the
// misconfiguration the endpoint tests exist to catch, left sitting in the path of
// anyone reaching for an obvious constructor. Naming it for what it does removes
// that trap without costing the tests anything.
//
// An empty endpoint yields a no-op client (Send never spawns a goroutine or touches
// the network). A configured endpoint MUST be an https:// URL; plaintext http is
// refused (no-op).
func NewSingleDestination(endpoint string) *Client {
	return NewWithQualitySignal(endpoint, endpoint)
}

// NewWithQualitySignal returns a Client that POSTs the usage ping to usage and the
// quality signal to quality. The two are independent: either may be empty, and an
// empty one makes only its own surface a no-op — the property that allows the two
// compiled-in endpoint constants to be activated on separate schedules. Both must
// be https:// to send at all; plaintext http is refused on each path alike.
func NewWithQualitySignal(usage, quality string) *Client {
	// A dedicated client instance (not http.DefaultClient); its nil Transport
	// reuses the shared http.DefaultTransport connection pool (same as
	// internal/scorecard's cloudHTTPClient and llmclient's default client) —
	// only per-instance policy is isolated, never the connection pool.
	return &Client{
		endpoint:        usage,
		qualityEndpoint: quality,
		httpClient: &http.Client{
			// isHTTPS vets only the INITIAL endpoint; re-vet every redirect hop so a
			// 307/308 (which replays the POST body via GetBody) can never downgrade
			// the payload to plaintext http. Mirrors checkRegistryRedirect
			// (internal/registry) and noRedirect (internal/scorecard).
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				if !isHTTPS(req.URL.String()) {
					return fmt.Errorf("telemetry: refusing redirect to non-HTTPS %q", req.URL.String())
				}
				return nil
			},
		},
		sem:            make(chan struct{}, maxInFlightSends),
		qualitySem:     make(chan struct{}, maxInFlightSends),
		requestTimeout: defaultRequestTimeout,
	}
}

// userAgent is the User-Agent every telemetry request carries: the client name
// and its build version, e.g. "atcr/1.2.3" (or "atcr/0.0.0" for an unstamped dev
// build). internal/version is a zero-dependency leaf holding an ldflags-stamped
// var, so this stays a strictly downward dependency and a release build reports
// its real version with no plumbing through the constructors.
func userAgent() string { return "atcr/" + version.Version }

// isHTTPS reports whether endpoint is a well-formed https URL (case-insensitive
// scheme). An empty, malformed, or plaintext-http endpoint is refused, so Send
// no-ops rather than ever sending in the clear. The client's CheckRedirect
// re-runs this same check on every redirect hop, so a 307/308 cannot downgrade
// the payload to plaintext after the initial URL passed.
func isHTTPS(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && strings.EqualFold(u.Scheme, "https") && u.Host != ""
}

// Send fires ev to the endpoint on a detached goroutine and returns immediately.
// It is a no-op when the client is nil, the endpoint is empty, or the endpoint is
// not HTTPS. Every failure mode — non-2xx, network error, marshal error, or an
// internal panic — is logged at debug level (never a level that alarms an end
// user about a default-on, opt-out background feature) and swallowed: Send has
// no error return and never affects the caller's outcome or exit code. The usage Event is
// marshaled compactly, preserving its existing wire format.
func (c *Client) Send(ctx context.Context, ev Event) {
	if c == nil {
		return
	}
	c.dispatch(ctx, c.endpoint, func() ([]byte, error) { return json.Marshal(ev) })
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
	if c == nil || len(payload) == 0 {
		return
	}
	c.dispatch(ctx, c.qualityEndpoint, func() ([]byte, error) { return json.MarshalIndent(payload, "", "  ") })
}

// dispatch is the shared fail-open send core: it no-ops on a nil client or a
// non-HTTPS/empty endpoint, bounds concurrent goroutines via the in-flight
// semaphore, and hands the marshal closure to the detached send goroutine. Both
// Send and SendQualitySignal funnel through it so the goroutine/timeout/recover
// contract has a single implementation; only the destination and the marshaling
// differ per payload.
//
// endpoint is passed in rather than read off c so each surface is gated by ITS OWN
// destination: an empty or non-HTTPS endpoint silences only the payload aimed at
// it, never the sibling surface.
func (c *Client) dispatch(ctx context.Context, endpoint string, marshal func() ([]byte, error)) {
	if c == nil || !isHTTPS(endpoint) {
		return
	}
	// Bound per destination: a semaphore is keyed on the destination, not the
	// payload type, so a hung endpoint can only ever starve payloads aimed at IT.
	// When both surfaces share one destination (New), they share its semaphore —
	// a single destination is a single surface for bounding purposes.
	sem := c.sem
	if endpoint == c.qualityEndpoint {
		sem = c.qualitySem
	}
	// Non-blocking acquire: if maxInFlightSends are already running, drop this ping
	// (best-effort) rather than block the caller or spawn an unbounded goroutine.
	select {
	case sem <- struct{}{}:
	default:
		log.FromContext(ctx).Debug("telemetry: send dropped (in-flight cap reached)", "endpoint", endpoint)
		return
	}
	c.wg.Add(1)
	go c.send(ctx, endpoint, sem, marshal)
}

func (c *Client) send(ctx context.Context, endpoint string, sem chan struct{}, marshal func() ([]byte, error)) {
	defer c.wg.Done()
	defer func() { <-sem }() // release the in-flight slot acquired in dispatch
	defer func() {
		if r := recover(); r != nil {
			log.FromContext(ctx).Debug("telemetry: recovered from panic", "value", r, "endpoint", endpoint)
		}
	}()

	body, err := marshal()
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: marshal failed", "error", err, "endpoint", endpoint)
		return
	}

	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: build request failed", "error", err, "endpoint", endpoint)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// Identify the client and its build. Without this every ping arrives as the
	// generic Go default UA, leaving the aggregate data with no client-version
	// dimension: no way to tell a current build from a two-year-old one, to
	// correlate a status distribution with a release, or to deprecate a
	// misbehaving version server-side. The Event allowlist deliberately carries no
	// version field, so the header is the only place for it — and a build version
	// is not PII: it is already public in the shipped binary.
	req.Header.Set("User-Agent", userAgent())

	resp, err := currentDoRequest()(c.httpClient, req)
	if err != nil {
		log.FromContext(ctx).Debug("telemetry: send failed", "error", err, "endpoint", endpoint)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Name the destination: with two endpoints serving two payload shapes behind
		// closed key allowlists, a bare status is ambiguous about WHICH surface was
		// rejected — and a mis-routed payload's 400 is otherwise dropped silently.
		log.FromContext(ctx).Debug("telemetry: non-2xx response", "status", resp.StatusCode, "endpoint", endpoint)
	}
	// Drain up to 64KB so the keep-alive connection is reused for the small
	// acks telemetry receives; a body larger than the cap is only partially
	// read and the connection is NOT reused — the cap intentionally trades
	// reuse on oversized bodies for a bounded read.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
}

// Go runs fn on a detached goroutine that is registered with the client's
// WaitGroup BEFORE it starts, so a concurrent Wait cannot return while fn is
// still running.
//
// It exists because registering at DISPATCH is too late for any caller that does
// work before deciding to send. The quality-signal path is exactly that shape: it
// spawns a goroutine, performs an O(n) debt-store read and aggregation, and only
// then calls SendQualitySignal — so its wg.Add landed after the command had
// returned and after main's bounded drain had already observed an empty
// WaitGroup. In production that meant the drain systematically failed to cover
// the quality signal, and the send was stranded at process exit far more often
// than "best effort" implies. It is also a latent panic: sync.WaitGroup forbids
// an Add that races a Wait when the counter is at zero ("WaitGroup misuse: Add
// called concurrently with Wait").
//
// fn's panics are recovered here, matching the send path's fail-open contract, so
// a caller's detached work can never crash the process. Nil-safe: a nil Client
// runs fn on a plain goroutine with nothing to wait on.
//
// Callers must still obey Wait's precondition — no new Go/Send may start once a
// drain has begun.
func (c *Client) Go(ctx context.Context, fn func()) {
	if c == nil {
		go fn()
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.FromContext(ctx).Debug("telemetry: recovered from panic in detached work", "value", r)
			}
		}()
		fn()
	}()
}

// Wait blocks until all in-flight sends complete. Intended for deterministic
// tests and graceful-shutdown drain; production callers fire-and-forget and
// never call it. Safe on a nil Client.
//
// Wait is safe ONLY after the caller has quiesced dispatch — no concurrent
// Send/SendQualitySignal may be in flight. sync.WaitGroup forbids an Add that
// races a Wait when the counter is zero: Wait can return before the
// just-dispatched send is counted, dropping the very ping the drain was meant
// to flush. A graceful shutdown must stop new dispatches first, then Wait.
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
