package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	atcrerrors "github.com/samestrin/atcr/internal/errors"
)

// Default retry/backoff tuning. The base delay and 1.5x factor are chosen so
// the full retry budget (≈500ms + 750ms) stays well inside a typical per-agent
// timeout rather than exhausting it.
const (
	defaultMaxRetries     = 2 // 3 attempts total
	defaultInitialBackoff = 500 * time.Millisecond
	defaultBackoffFactor  = 1.5
	defaultHTTPTimeout    = 120 * time.Second

	// maxErrorBodyBytes bounds how much of a non-200 response body is read for
	// error reporting; the remainder is drained so the connection can be reused.
	maxErrorBodyBytes = 4 << 10

	// maxResponseBodyBytes caps how much of a 200 response body is decoded so a
	// misbehaving or hostile endpoint cannot stream unbounded memory into a
	// long-lived process. Generous: real completions are far smaller.
	maxResponseBodyBytes = 32 << 20
)

// retryableStatus is the set of HTTP statuses worth retrying. Every other
// non-2xx (e.g. 400, 401, 403, 404) fails immediately.
var retryableStatus = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	http.StatusGatewayTimeout:      true, // 504
}

// Client is a reusable OpenAI-compatible chat-completions client. The HTTP
// transport timeout guards a single exchange; per-agent and global deadlines
// live in the context passed to Complete.
type Client struct {
	httpClient     *http.Client
	maxRetries     int
	initialBackoff time.Duration
	backoffFactor  float64
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient injects a custom *http.Client (tests point it at httptest). The
// no-redirect guard is re-applied onto the injected client so the
// Authorization-not-forwarded-on-redirect invariant cannot be bypassed by
// dependency injection.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			h.CheckRedirect = noRedirect
		}
		c.httpClient = h
	}
}

// noRedirect blocks redirect following so the Authorization: Bearer header is
// never forwarded to a redirect target: a 3xx is a hard failure (only 200
// succeeds). Shared by New() and WithHTTPClient so the invariant holds for both
// the default and any injected client.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// WithRetry overrides the retry budget and backoff (tests use a tiny base so
// they do not sleep for real).
func WithRetry(maxRetries int, initialBackoff time.Duration, factor float64) Option {
	return func(c *Client) {
		// A negative budget would make the attempt loop never execute and fall
		// through to an "exhausted retries" error wrapping a nil cause; clamp it
		// to zero so at least one attempt is always made.
		if maxRetries < 0 {
			maxRetries = 0
		}
		c.maxRetries = maxRetries
		c.initialBackoff = initialBackoff
		c.backoffFactor = factor
	}
}

// New builds a Client with sensible defaults.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
			// Do not follow redirects: a 3xx is a hard failure (only 200
			// succeeds), and auto-following would forward the Bearer header to
			// the redirect target.
			CheckRedirect: noRedirect,
		},
		maxRetries:     defaultMaxRetries,
		initialBackoff: defaultInitialBackoff,
		backoffFactor:  defaultBackoffFactor,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Invocation is one chat-completions call. APIKeyEnv names the environment
// variable holding the key; it is resolved at invoke time (never at load) and
// the value is never logged.
type Invocation struct {
	BaseURL     string
	APIKeyEnv   string
	Model       string
	Temperature *float64
	// MaxTokens caps the completion budget (OpenAI max_tokens). Nil omits the
	// field so the provider's own default applies. Reasoning/thinking models
	// spend this budget on internal reasoning, so callers that need visible
	// output (e.g. the doctor self-test) must set it generously.
	MaxTokens *int
	Prompt    string
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent carries a reasoning/thinking model's chain-of-thought
	// (litellm returns it on a separate channel from Content). It is the
	// fallback when a reasoning model exhausts its output-token budget before
	// emitting any Content: the chain-of-thought still holds the draft review,
	// which the severity-prefix extraction recovers downstream. omitempty keeps
	// it out of request bodies, where this struct is also used.
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
}

// UsageData carries the provider-reported token counts for one call. A zero
// value means the provider omitted the `usage` block entirely (graceful
// degradation, not an error) and is always safe to pass to ComputeCostUSD.
type UsageData struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// UnmarshalJSON tolerates malformed or non-integer usage blocks. Token usage is
// non-load-bearing metadata, so a provider that emits counts as JSON floats
// (e.g. 14200.0, which some gateways do) or otherwise malforms the block must
// NOT fail the parent response decode and discard the assistant content — it
// degrades to zero counts instead. Each field is decoded independently so a
// single bad field does not discard the other. Counts are truncated toward zero.
func (u *UsageData) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Structurally malformed usage block: degrade to zero, never error.
		return nil
	}
	if pt, ok := raw["prompt_tokens"]; ok {
		var n json.Number
		if err := json.Unmarshal(pt, &n); err == nil {
			u.PromptTokens = clampNonNegative(n)
		}
	}
	if ct, ok := raw["completion_tokens"]; ok {
		var n json.Number
		if err := json.Unmarshal(ct, &n); err == nil {
			u.CompletionTokens = clampNonNegative(n)
		}
	}
	return nil
}

// clampNonNegative truncates a usage count toward zero and clamps it into the
// non-negative int range at the data boundary, so every consumer of UsageData —
// not just ComputeCostUSD — sees a valid count. The 0 return is reserved for
// values that are not a usable count: non-numeric, NaN, Inf, or negative. A
// genuinely large but valid count that exceeds math.MaxInt clamps to math.MaxInt
// rather than collapsing to 0, so an oversized count is reported as a ceiling
// (over-counting at worst) instead of masking a real request as free.
func clampNonNegative(n json.Number) int {
	v, err := n.Float64()
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	// float64(math.MaxInt) rounds up to 2^63, so compare with >= to also clamp the
	// boundary value (int(2^63) would overflow).
	if v >= float64(math.MaxInt) {
		return math.MaxInt
	}
	return int(v)
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage UsageData `json:"usage"`
}

// CallRecord is per-attempt telemetry for one c.attempt() invocation. dispatch
// accumulates one record per attempt — retries included — so callers can count
// real HTTP round-trips and observe per-attempt latency. ReachedWire reports
// whether the request was written to the wire (detected via httptrace): it is
// false when the call never left the process (a context cancel before the bytes
// were sent, or a request-construction failure) and true once any bytes were
// written, even if the attempt then failed mid-flight. Duration is the wall-clock
// time the attempt took, recorded for every attempt regardless of ReachedWire.
type CallRecord struct {
	ReachedWire bool
	Duration    time.Duration
}

// Complete invokes the provider and returns the assistant message content.
// Retries on 429/5xx and transport-level errors with tuned backoff; other
// non-2xx statuses and parse failures fail immediately. The API key value
// never appears in any error. Existing callers that do not need token usage
// keep this two-value signature; CompleteWithUsage exposes the usage block.
func (c *Client) Complete(ctx context.Context, inv Invocation) (string, error) {
	content, _, _, err := c.CompleteWithUsage(ctx, inv)
	return content, err
}

// CompleteWithUsage is Complete plus the provider's token usage and the per-call
// telemetry for the dispatch (one CallRecord per HTTP attempt, retries included).
// Usage is the zero value when the provider omits the `usage` block. All error
// paths return an empty UsageData, never partial counts. The CallRecord slice is
// surfaced on every path that reached dispatch — including error paths — so a
// mid-flight timeout still reports its wire attempt; it is nil only when no HTTP
// attempt was made (key/marshal failure, or a circuit-open fail-fast).
func (c *Client) CompleteWithUsage(ctx context.Context, inv Invocation) (string, UsageData, []CallRecord, error) {
	key, err := resolveKey(inv)
	if err != nil {
		return "", UsageData{}, nil, err
	}
	body, err := json.Marshal(chatRequest{
		Model:       inv.Model,
		Messages:    []message{{Role: "user", Content: inv.Prompt}},
		Temperature: inv.Temperature,
		MaxTokens:   inv.MaxTokens,
	})
	if err != nil {
		return "", UsageData{}, nil, fmt.Errorf("encoding request: %w", err)
	}
	raw, records, err := c.send(ctx, resolveEndpoint(inv.BaseURL), key, body)
	if err != nil {
		return "", UsageData{}, records, err
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", UsageData{}, records, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", UsageData{}, records, fmt.Errorf("failed to parse response: no choices returned")
	}
	msg := parsed.Choices[0].Message
	content := msg.Content
	if content == "" {
		// Reasoning model that ran out of output budget mid-thought: salvage the
		// chain-of-thought so the reviewer still contributes instead of returning
		// an empty review.
		content = msg.ReasoningContent
	}
	if content == "" {
		// Both content and reasoning_content are empty: the provider said nothing.
		// Fail loudly so callers cannot mistake silence for a clean/empty review.
		// Non-retryable — a re-request with the same budget would repeat the result.
		return "", UsageData{}, records, atcrerrors.NewSystemError(fmt.Errorf("provider returned an empty completion (no content or reasoning_content)"))
	}
	return content, parsed.Usage, records, nil
}

// resolveKey reads the invocation's API key env var; the value is never logged.
func resolveKey(inv Invocation) (string, error) {
	key := os.Getenv(inv.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("API key env var %s is not set", inv.APIKeyEnv)
	}
	return key, nil
}

// resolveEndpoint builds the chat-completions URL and defensively drops any
// userinfo embedded in the base URL so transport and request-creation errors
// (which echo the endpoint) cannot surface it.
func resolveEndpoint(baseURL string) string {
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	if u, err := url.Parse(endpoint); err == nil && u.User != nil {
		u.User = nil
		endpoint = u.String()
	}
	return endpoint
}

// send wraps the retry loop (dispatch) with the per-provider circuit breaker
// (Epic 4.5). The provider name travels on the context (set by the fan-out
// engine), not on the Invocation, so the breaker keys on the logical provider
// rather than a lossy BaseURL. When no provider is attached — e.g. the doctor
// self-test, which must probe every endpoint regardless of circuit state — the
// breaker is skipped entirely. An open circuit fails fast with no HTTP request
// (AC2). After the call, only a breaker-failure (5xx / timeout / transport)
// records a failure; a 4xx (incl. 429/401) or a caller cancellation records
// nothing (AC10).
func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, []CallRecord, error) {
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		return c.dispatch(ctx, endpoint, key, body)
	}
	breaker := circuitbreaker.DefaultRegistry.Get(provider)
	if !breaker.Allow() {
		// Fail fast with no HTTP attempt (AC2): nil records signals "no call made"
		// so the metrics layer counts zero rather than a phantom round-trip.
		return nil, nil, &CircuitOpenError{Provider: provider}
	}
	// resolved is set to true once the switch below records a verdict. The defer
	// fires on both normal return and panic; if resolved is still false when it
	// runs (panic path), it releases the probe so the slot is never leaked.
	resolved := false
	defer func() {
		if !resolved {
			breaker.ReleaseProbe()
		}
	}()
	raw, records, err := c.dispatch(ctx, endpoint, key, body)
	resolved = true
	// Every branch reports the outcome to the breaker exactly once: a half-open
	// probe MUST be resolved (RecordSuccess/RecordFailure/ReleaseProbe) or the
	// probe slot leaks and the circuit wedges half-open forever.
	switch {
	case err == nil:
		// A successful round-trip: closes a half-open probe, resets the run.
		breaker.RecordSuccess()
	case errors.Is(err, context.Canceled):
		// The caller cancelled mid-call — nothing was learned about the provider.
		// Release any half-open probe without a verdict (do not count it). Checked
		// before isBreakerFailure so the classification is explicit rather than
		// resting on isBreakerFailure happening to return false for cancellation.
		breaker.ReleaseProbe()
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == context.DeadlineExceeded:
		// The caller's OWN deadline expired (the per-agent TimeoutSecs or the
		// global fan-out deadline), not a provider stall: ctx is Done, so this
		// says nothing about provider health — release any half-open probe without
		// a verdict. A genuine provider stall trips the HTTP client's timeout while
		// the caller ctx is NOT done; that falls through to isBreakerFailure below
		// and counts as an outage (AC10). The ctx.Err() guard is the load-bearing
		// discriminator between the two.
		breaker.ReleaseProbe()
	case isBreakerFailure(err):
		// 5xx, provider-stall timeout, or transport failure: trips/advances the run.
		breaker.RecordFailure()
	default:
		// A non-tripping HTTP response (3xx redirect surfaced by noRedirect, or
		// 4xx incl. 429/401): the provider replied, so it is reachable. The
		// breaker tracks outages, not redirect or auth/rate-limit correctness,
		// so a provider reply counts as a healthy round-trip — which also closes
		// a half-open probe instead of wedging it.
		breaker.RecordSuccess()
	}
	return raw, records, err
}

// CircuitOpenError is returned when a provider's circuit breaker is open: the
// request fails fast with no HTTP call (AC2). It is a permanent failure — the
// fan-out engine classifies it as a non-timeout failure and moves straight to
// the fallback chain (AC6). Provider names the tripped provider for diagnostics.
type CircuitOpenError struct {
	Provider string
}

func (e *CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit breaker open for provider %q: failing fast without an API call", e.Provider)
}

// isBreakerFailure reports whether err should count as a circuit-breaker failure
// (Epic 4.5, AC10 as clarified by the rubber-duck gate): a 5xx response, a
// timeout, or a connection-level transport error (connection refused/reset, EOF,
// DNS) — all signal the provider is unavailable. A 4xx response (including 429
// rate-limits, owned by Epic 4.6's backoff, and 401 auth errors) does NOT count:
// the server replied, so the provider is reachable. A caller-initiated
// cancellation does not count either — the call was aborted, which says nothing
// about provider health.
//
// A context.DeadlineExceeded counts as a failure here because it is the
// provider-stall signal (the HTTP client's own timeout firing). The send switch
// filters out the OTHER source of DeadlineExceeded first — the caller's own ctx
// deadline (ctx.Err()==DeadlineExceeded) — and releases the probe for it, so by
// the time this function is consulted a DeadlineExceeded means the provider
// stalled, not that the caller's budget ran out.
func isBreakerFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var he *HTTPStatusError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	// No HTTP response reached us: a transport-level failure or a timeout. Both
	// mean the provider is effectively unavailable → count as a breaker failure.
	return true
}

// dispatch performs the request with the retry/backoff schedule and returns the
// raw 200 response body for the caller to parse. It is the inner retry loop
// wrapped by send (which adds the circuit breaker), shared by Complete (single
// message) and Chat (multi-turn with tools): both feed it a pre-marshalled body
// and decode the bytes themselves, so the retry, redirect, key-redaction, and
// size-cap semantics stay identical across the two call shapes.
func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte) ([]byte, []CallRecord, error) {
	var lastErr error
	// The retry budget and base delay come from the client by default, but a
	// per-call override on the context (Epic 4.6: the fan-out's resolved
	// per-agent max_retries / initial_backoff_ms) takes precedence. The 1.5x
	// factor and the maxBackoff cap stay fixed implementation constants.
	maxRetries := c.maxRetries
	delay := c.initialBackoff
	if o, ok := retryOverrideFromContext(ctx); ok {
		maxRetries = o.maxRetries
		delay = o.initialBackoff
	}
	// One CallRecord per attempt (retries included), so a caller can count real
	// HTTP round-trips and observe per-attempt latency. Sized to the worst-case
	// attempt count (initial try + maxRetries) so the common path never re-grows.
	records := make([]CallRecord, 0, maxRetries+1)
	// Clamp the starting delay so even the FIRST retry sleep respects maxBackoff:
	// every subsequent delay is clamped after the ×factor step, but without this
	// an out-of-range base would sleep its full unclamped duration on attempt 1.
	// In practice a config-sourced base can never trigger this — the registry
	// caps initial_backoff_ms at exactly maxBackoff (30s) — so the only input it
	// actually clamps is a direct WithRetryOverride exceeding 30s.
	delay = clampBackoff(delay)
	// honorExact is set when the next sleep is a server-advertised Retry-After
	// cooldown, which must be slept verbatim (neither jittered down nor clamped).
	honorExact := false
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			sleepFor := delay
			if !honorExact {
				sleepFor = jitter(delay)
			}
			if err := sleepCtx(ctx, sleepFor); err != nil {
				return nil, records, err
			}
			honorExact = false
			delay = clampBackoff(time.Duration(float64(delay) * c.backoffFactor))
		}

		payload, status, retryAfter, rec, err := c.attempt(ctx, endpoint, key, body)
		records = append(records, rec)
		switch {
		case status == 0:
			// Context cancellation/deadline must return immediately so timeout
			// classification stays correct; other transport-level failures
			// (connection reset, EOF, DNS blip) are as transient as a 5xx and
			// get the same backoff schedule.
			if ctx.Err() != nil {
				return nil, records, err
			}
			lastErr = err
			if attempt < maxRetries {
				continue
			}
			// Transport-level exhaustion (connection reset, EOF, DNS) is transient:
			// the same class as a 5xx, so callers can retry at a higher layer (AC11).
			return nil, records, atcrerrors.NewTransient(fmt.Errorf("exhausted retries: %w", lastErr))
		case status == http.StatusOK:
			// A 200 with an unreadable/oversized body is a hard failure, not a retry.
			if err != nil {
				return nil, records, err
			}
			return payload, records, nil
		case retryableStatus[status]:
			lastErr = httpStatusError(status, string(payload))
			if attempt < maxRetries {
				// Honor a server-advertised cooldown (Retry-After) over the fixed
				// backoff when present; otherwise keep the exponential schedule.
				if retryAfter > 0 {
					delay = retryAfter
					honorExact = true
				}
				continue
			}
			// Last attempt still retryable (429/5xx): report the exhausted budget as
			// transient. The wrapped *HTTPStatusError stays errors.As-reachable
			// through ClassifiedError.Unwrap → the exhausted-retries wrapper (AC11).
			return nil, records, atcrerrors.NewTransient(fmt.Errorf("exhausted retries: %w", lastErr))
		default:
			if err != nil {
				return nil, records, err
			}
			// Non-retryable status (401/403/404/...): a permanent failure. Wrapping
			// preserves errors.As reachability to *HTTPStatusError (AC11, AC12).
			return nil, records, atcrerrors.NewPermanent(httpStatusError(status, string(payload)))
		}
	}
	// Defensive loop-exit fallback (the switch always returns or continues): the
	// budget is exhausted, so classify transient like the other exhaustion paths.
	return nil, records, atcrerrors.NewTransient(fmt.Errorf("exhausted retries: %w", lastErr))
}

// HTTPStatusError is a non-2xx provider response surfaced to callers so they
// can classify the failure by HTTP status (via errors.As) instead of parsing
// the message string. Snippet is the bounded, whitespace-collapsed,
// key-redacted prefix of the provider's error body (empty when none was sent).
// It survives the exhausted-retries wrapper, so errors.As reaches it for both
// retryable (429/5xx) and non-retryable (401/403/404) failures.
type HTTPStatusError struct {
	Status  int
	Snippet string
}

// Error preserves the original message format so existing callers and tests
// that match on the text continue to work unchanged.
func (e *HTTPStatusError) Error() string {
	if e.Snippet == "" {
		return fmt.Sprintf("provider returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("provider returned HTTP %d: %s", e.Status, e.Snippet)
}

// httpStatusError builds an *HTTPStatusError for a non-200 failure.
func httpStatusError(status int, snippet string) error {
	return &HTTPStatusError{Status: status, Snippet: snippet}
}

// bearerTokenPattern and skKeyPattern match secret-shaped tokens that a provider
// might echo into an error body even when they are not the literal configured
// key (a foreign token, or the key in a transformed form). They back the
// defense-in-depth scrub in redactErrorSnippet. readErrorSnippet collapses
// whitespace first, so `Bearer <token>` is single-spaced when these run.
var (
	bearerTokenPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	skKeyPattern       = regexp.MustCompile(`(?i)sk-\S+`)
	// fleetKeyPattern matches the distinctive key prefixes of the other
	// OpenAI-compatible fleet providers (Google AIza…, Groq gsk_…, xAI xai-…) so a
	// foreign key echoed in an error body is scrubbed like an sk-/Bearer token.
	// This stays best-effort defense-in-depth: a generic JWT/hex/base64 secret
	// with no known prefix is not covered (only the configured key is scrubbed by
	// exact match for those).
	fleetKeyPattern = regexp.MustCompile(`(?i)(?:AIza|gsk_|xai-)\S+`)
)

// redactErrorSnippet scrubs secrets from a provider error snippet. It removes
// the configured key in both literal and URL-encoded form (an exact-match scrub
// alone misses a key the provider echoes re-encoded), then redacts any
// Bearer-, sk-, or known-fleet-prefixed (AIza/gsk_/xai-) token generically so a
// foreign or transformed secret cannot leak into HTTPStatusError.Snippet.
func redactErrorSnippet(snippet, key string) string {
	if key != "" {
		snippet = strings.ReplaceAll(snippet, key, "[redacted]")
		if enc := url.QueryEscape(key); enc != key {
			snippet = strings.ReplaceAll(snippet, enc, "[redacted]")
		}
	}
	snippet = bearerTokenPattern.ReplaceAllString(snippet, "Bearer [redacted]")
	snippet = skKeyPattern.ReplaceAllString(snippet, "[redacted]")
	snippet = fleetKeyPattern.ReplaceAllString(snippet, "[redacted]")
	return snippet
}

// readErrorSnippet reads a bounded prefix of a non-200 response body and
// collapses it to a single whitespace-normalized line. A bounded remainder is
// then drained so a normally-sized error body's connection can be reused.
//
// The drain is deliberately capped (total read ≤ 2×maxErrorBodyBytes, ~8KB), NOT
// drained in full: a hostile or malfunctioning endpoint streaming a huge error
// body on the error path must not be read to completion (a full io.Copy drain
// would spend time bounded only by the HTTP/context timeout on attacker-supplied
// bytes). The accepted trade-off is that an error body larger than ~8KB is left
// undrained, so its connection is closed rather than returned to the keep-alive
// pool — costing one extra TCP+TLS handshake on the next retry. Real provider
// error bodies are far smaller than 8KB, so bounding the read against abuse is
// worth more than saving that handshake on the rare oversized body. The bound is
// guarded by TestReadErrorSnippet_DrainIsBounded.
func readErrorSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	// Drain only a bounded remainder (see the function doc): enough to reuse the
	// connection for a normal error body, without reading an unbounded body from a
	// hostile/malfunctioning endpoint on the error path.
	_, _ = io.CopyN(io.Discard, r, maxErrorBodyBytes)
	return strings.Join(strings.Fields(string(b)), " ")
}

// attempt performs a single request. On 200 it returns the raw (bounded)
// response body for the caller to decode; on non-200 it returns a sanitized
// error-body snippet (as bytes). status is 0 on a transport error. The returned
// duration is the server-advertised Retry-After cooldown for a retryable
// status (0 when absent/malformed). A 200 whose body exceeds the size cap
// returns the size error with status 200.
func (c *Client) attempt(ctx context.Context, endpoint, key string, body []byte) ([]byte, int, time.Duration, CallRecord, error) {
	// Stamp the attempt's wall-clock start and detect whether the request bytes
	// were actually written to the wire (httptrace WroteRequest). The flag is the
	// load-bearing discriminator for ReachedWire on the transport-error path: a
	// mid-flight cancel/timeout fires WroteRequest (counts as a real round-trip),
	// while a cancel before the bytes are sent does not (must not count, AC2).
	// The flag is an atomic.Bool: net/http writes the request from a separate
	// transport goroutine, so on the Do-returns-error path there is no guaranteed
	// happens-before between the callback's store and this read — the atomic makes
	// the read well-defined rather than a data race.
	start := time.Now()
	var wroteRequest atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { wroteRequest.Store(true) },
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, CallRecord{ReachedWire: false, Duration: time.Since(start)}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, 0, CallRecord{ReachedWire: wroteRequest.Load(), Duration: time.Since(start)}, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A provider response was received, so the request reached the wire — every
	// status (200, 4xx, 5xx) is a completed HTTP attempt for telemetry.
	if resp.StatusCode != http.StatusOK {
		// Capture a bounded snippet for error reporting; the provider's JSON
		// error body carries the actionable root cause. Scrub the key in case
		// the provider echoes the Authorization header back.
		snippet := redactErrorSnippet(readErrorSnippet(resp.Body), key)
		return []byte(snippet), resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), CallRecord{ReachedWire: true, Duration: time.Since(start)}, nil
	}

	// N is cap+1 so crossing the cap is distinguishable from a body that is
	// exactly cap bytes. A misbehaving or hostile endpoint cannot stream
	// unbounded memory into a long-lived process.
	limited := &io.LimitedReader{R: resp.Body, N: maxResponseBodyBytes + 1}
	raw, rerr := io.ReadAll(limited)
	if rerr != nil {
		return nil, resp.StatusCode, 0, CallRecord{ReachedWire: true, Duration: time.Since(start)}, fmt.Errorf("reading response: %w", rerr)
	}
	if limited.N <= 0 {
		return nil, resp.StatusCode, 0, CallRecord{ReachedWire: true, Duration: time.Since(start)}, fmt.Errorf("response exceeds %d byte size limit", maxResponseBodyBytes)
	}
	return raw, resp.StatusCode, 0, CallRecord{ReachedWire: true, Duration: time.Since(start)}, nil
}

// parseRetryAfter interprets a Retry-After header value per RFC 7231: either
// delta-seconds (a non-negative integer) or an HTTP-date. It returns the
// indicated delay, or 0 when the header is absent, malformed, non-positive, or
// in the past — in which case the caller falls back to its own backoff. The
// returned delay is capped at maxRetryAfter so an excessive or overflowing
// advertised cooldown cannot stall a worker.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		// Cap before multiplying: a huge delta-seconds would overflow
		// time.Duration (int64 nanoseconds) and wrap to a negative/garbage delay.
		// Anything at or above the ceiling is clamped without performing the
		// overflowing multiplication.
		if secs >= int(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return clampRetryAfter(d)
		}
	}
	return 0
}

// maxRetryAfter bounds how long a server-advertised Retry-After cooldown is
// honored. Without a ceiling a hostile or misconfigured endpoint could stall a
// worker for hours, or overflow time.Duration via a huge delta-seconds value.
// The cap is generous enough to respect realistic rate-limit cooldowns while
// keeping a single stuck call from blocking far longer than a human would wait.
const maxRetryAfter = 5 * time.Minute

// clampRetryAfter bounds a Retry-After delay at maxRetryAfter.
func clampRetryAfter(d time.Duration) time.Duration {
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}

// maxBackoff caps the per-retry exponential backoff so a large WithRetry budget
// cannot produce multi-minute sleeps. Server-advertised Retry-After cooldowns
// are honored up to maxRetryAfter (a separate, larger ceiling) and are not
// subject to this cap.
const maxBackoff = 30 * time.Second

// clampBackoff bounds an exponential backoff delay at maxBackoff.
func clampBackoff(d time.Duration) time.Duration {
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// jitter spreads a backoff delay across [d/2, d) so many agents that hit a 429
// at the same instant do not retry in lockstep (thundering herd). A delay too
// small to halve is returned unchanged.
func jitter(d time.Duration) time.Duration {
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(rand.Int63n(int64(half)))
}

// sleepCtx waits for d or until ctx is cancelled, whichever comes first. It is a
// package var rather than a plain func so tests can substitute a recorder and
// assert the backoff schedule (e.g. that a direct WithRetryOverride base above
// maxBackoff is clamped on the first sleep) without spending real wall-clock time.
var sleepCtx = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
