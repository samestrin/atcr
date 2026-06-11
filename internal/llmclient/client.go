package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Prompt      string
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

// Complete invokes the provider and returns the assistant message content.
// Retries on 429/5xx with tuned backoff; other non-2xx statuses and parse
// failures fail immediately. The API key value never appears in any error.
func (c *Client) Complete(ctx context.Context, inv Invocation) (string, error) {
	key := os.Getenv(inv.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("API key env var %s is not set", inv.APIKeyEnv)
	}

	body, err := json.Marshal(chatRequest{
		Model:       inv.Model,
		Messages:    []message{{Role: "user", Content: inv.Prompt}},
		Temperature: inv.Temperature,
	})
	if err != nil {
		return "", fmt.Errorf("encoding request: %w", err)
	}

	endpoint := strings.TrimRight(inv.BaseURL, "/") + "/chat/completions"
	// Defensively drop any userinfo embedded in the base URL so transport and
	// request-creation errors (which echo the endpoint) cannot surface it.
	if u, err := url.Parse(endpoint); err == nil && u.User != nil {
		u.User = nil
		endpoint = u.String()
	}

	var lastErr error
	delay := c.initialBackoff
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, delay); err != nil {
				return "", err
			}
			delay = time.Duration(float64(delay) * c.backoffFactor)
		}

		content, status, err := c.attempt(ctx, endpoint, key, body)
		switch {
		case status == 0:
			// Transport-level failure (context, connection). Not retried.
			return "", err
		case status == http.StatusOK:
			// A 200 with an unparseable body is a hard failure, not a retry.
			if err != nil {
				return "", err
			}
			return content, nil
		case retryableStatus[status]:
			lastErr = httpStatusError(status, content)
			if attempt < c.maxRetries {
				continue
			}
			// Last attempt still retryable: report the exhausted budget.
			return "", fmt.Errorf("exhausted retries: %w", lastErr)
		default:
			if err != nil {
				return "", err
			}
			return "", httpStatusError(status, content)
		}
	}
	return "", fmt.Errorf("exhausted retries: %w", lastErr)
}


// httpStatusError formats a non-200 failure, appending the sanitized
// error-body snippet when the provider sent one.
func httpStatusError(status int, snippet string) error {
	if snippet == "" {
		return fmt.Errorf("provider returned HTTP %d", status)
	}
	return fmt.Errorf("provider returned HTTP %d: %s", status, snippet)
}

// readErrorSnippet reads a bounded prefix of a non-200 response body and
// collapses it to a single whitespace-normalized line. The remainder of the
// body is drained so the connection can be reused.
func readErrorSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, r)
	return strings.Join(strings.Fields(string(b)), " ")
}

// attempt performs a single request. It returns the parsed content (on 200)
// or a sanitized error-body snippet (on non-200), the HTTP status (0 on a
// transport error), and an error. A 200 with an unparseable body returns the
// parse error with status 200.
func (c *Client) attempt(ctx context.Context, endpoint, key string, body []byte) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Capture a bounded snippet for error reporting; the provider's JSON
		// error body carries the actionable root cause. Scrub the key in case
		// the provider echoes the Authorization header back.
		snippet := strings.ReplaceAll(readErrorSnippet(resp.Body), key, "[redacted]")
		return snippet, resp.StatusCode, nil
	}

	// N is cap+1 so crossing the cap is distinguishable from a body that is
	// exactly cap bytes.
	limited := &io.LimitedReader{R: resp.Body, N: maxResponseBodyBytes + 1}
	var parsed chatResponse
	if err := json.NewDecoder(limited).Decode(&parsed); err != nil {
		if limited.N <= 0 {
			return "", resp.StatusCode, fmt.Errorf("response exceeds %d byte size limit", maxResponseBodyBytes)
		}
		return "", resp.StatusCode, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", resp.StatusCode, fmt.Errorf("failed to parse response: no choices returned")
	}
	return parsed.Choices[0].Message.Content, resp.StatusCode, nil
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
