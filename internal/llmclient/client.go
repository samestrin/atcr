package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

// WithHTTPClient injects a custom *http.Client (tests point it at httptest).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithRetry overrides the retry budget and backoff (tests use a tiny base so
// they do not sleep for real).
func WithRetry(maxRetries int, initialBackoff time.Duration, factor float64) Option {
	return func(c *Client) {
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
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
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

// clampNonNegative truncates a usage count toward zero and clamps negatives to
// zero at the data boundary, so every consumer of UsageData — not just
// ComputeCostUSD — sees a non-negative count. A non-numeric value yields zero.
func clampNonNegative(n json.Number) int {
	v, err := n.Float64()
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 1e15 {
		return 0
	}
	return int(v)
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage UsageData `json:"usage"`
}

// Complete invokes the provider and returns the assistant message content.
// Retries on 429/5xx and transport-level errors with tuned backoff; other
// non-2xx statuses and parse failures fail immediately. The API key value
// never appears in any error. Existing callers that do not need token usage
// keep this two-value signature; CompleteWithUsage exposes the usage block.
func (c *Client) Complete(ctx context.Context, inv Invocation) (string, error) {
	content, _, err := c.CompleteWithUsage(ctx, inv)
	return content, err
}

// CompleteWithUsage is Complete plus the provider's token usage. Usage is the
// zero value when the provider omits the `usage` block. All error paths return
// an empty UsageData, never partial counts.
func (c *Client) CompleteWithUsage(ctx context.Context, inv Invocation) (string, UsageData, error) {
	key, err := resolveKey(inv)
	if err != nil {
		return "", UsageData{}, err
	}
	body, err := json.Marshal(chatRequest{
		Model:       inv.Model,
		Messages:    []message{{Role: "user", Content: inv.Prompt}},
		Temperature: inv.Temperature,
		MaxTokens:   inv.MaxTokens,
	})
	if err != nil {
		return "", UsageData{}, fmt.Errorf("encoding request: %w", err)
	}
	raw, err := c.send(ctx, resolveEndpoint(inv.BaseURL), key, body)
	if err != nil {
		return "", UsageData{}, err
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", UsageData{}, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", UsageData{}, fmt.Errorf("failed to parse response: no choices returned")
	}
	return parsed.Choices[0].Message.Content, parsed.Usage, nil
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

// send performs the request with the retry/backoff schedule and returns the raw
// 200 response body for the caller to parse. It is shared by Complete (single
// message) and Chat (multi-turn with tools): both feed it a pre-marshalled body
// and decode the bytes themselves, so the retry, redirect, key-redaction, and
// size-cap semantics stay identical across the two call shapes.
func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
	var lastErr error
	delay := c.initialBackoff
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, delay); err != nil {
				return nil, err
			}
			delay = time.Duration(float64(delay) * c.backoffFactor)
		}

		payload, status, err := c.attempt(ctx, endpoint, key, body)
		switch {
		case status == 0:
			// Context cancellation/deadline must return immediately so timeout
			// classification stays correct; other transport-level failures
			// (connection reset, EOF, DNS blip) are as transient as a 5xx and
			// get the same backoff schedule.
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			if attempt < c.maxRetries {
				continue
			}
			return nil, fmt.Errorf("exhausted retries: %w", lastErr)
		case status == http.StatusOK:
			// A 200 with an unreadable/oversized body is a hard failure, not a retry.
			if err != nil {
				return nil, err
			}
			return payload, nil
		case retryableStatus[status]:
			lastErr = httpStatusError(status, string(payload))
			if attempt < c.maxRetries {
				continue
			}
			// Last attempt still retryable: report the exhausted budget.
			return nil, fmt.Errorf("exhausted retries: %w", lastErr)
		default:
			if err != nil {
				return nil, err
			}
			return nil, httpStatusError(status, string(payload))
		}
	}
	return nil, fmt.Errorf("exhausted retries: %w", lastErr)
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

// readErrorSnippet reads a bounded prefix of a non-200 response body and
// collapses it to a single whitespace-normalized line. The remainder of the
// body is drained so the connection can be reused.
func readErrorSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, r)
	return strings.Join(strings.Fields(string(b)), " ")
}

// attempt performs a single request. On 200 it returns the raw (bounded)
// response body for the caller to decode; on non-200 it returns a sanitized
// error-body snippet (as bytes). status is 0 on a transport error. A 200 whose
// body exceeds the size cap returns the size error with status 200.
func (c *Client) attempt(ctx context.Context, endpoint, key string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Capture a bounded snippet for error reporting; the provider's JSON
		// error body carries the actionable root cause. Scrub the key in case
		// the provider echoes the Authorization header back.
		snippet := strings.ReplaceAll(readErrorSnippet(resp.Body), key, "[redacted]")
		return []byte(snippet), resp.StatusCode, nil
	}

	// N is cap+1 so crossing the cap is distinguishable from a body that is
	// exactly cap bytes. A misbehaving or hostile endpoint cannot stream
	// unbounded memory into a long-lived process.
	limited := &io.LimitedReader{R: resp.Body, N: maxResponseBodyBytes + 1}
	raw, rerr := io.ReadAll(limited)
	if rerr != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response: %w", rerr)
	}
	if limited.N <= 0 {
		return nil, resp.StatusCode, fmt.Errorf("response exceeds %d byte size limit", maxResponseBodyBytes)
	}
	return raw, resp.StatusCode, nil
}

// sleepCtx waits for d or until ctx is cancelled, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
