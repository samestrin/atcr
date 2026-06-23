package ghaction

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPIURL is the public GitHub REST API base. It is overridable via the
// Client.APIURL field so the action works against GitHub Enterprise and so tests
// can point the client at an httptest server.
const DefaultAPIURL = "https://api.github.com"

// Client is a minimal GitHub REST client for posting check runs and PR review
// comments. A zero HTTPClient falls back to a sane default; a zero APIURL falls
// back to the public GitHub API. Timeout overrides the default HTTP client
// timeout when HTTPClient is nil.
type Client struct {
	APIURL     string
	Token      string
	HTTPClient *http.Client
	Timeout    time.Duration
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func (c *Client) baseURL() (string, error) {
	raw := strings.TrimSpace(c.APIURL)
	if raw == "" {
		return DefaultAPIURL, nil
	}
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return "", fmt.Errorf("invalid API URL %q", raw)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("insecure API URL %q: must use https", raw)
	}
	return raw, nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// CheckRunRequest is the payload for creating a GitHub check run.
type CheckRunRequest struct {
	// Name is the check-run name shown on the PR (e.g. "atcr").
	Name string
	// HeadSHA is the commit the check run is reported against. For a
	// pull_request event this must be the PR head SHA, not the merge SHA.
	HeadSHA string
	// Conclusion is one of GitHub's terminal conclusions: success, failure, or
	// neutral.
	Conclusion string
	// Output is the rendered title/summary/text.
	Output CheckOutput
}

// CreateCheckRunWithID creates a completed check run and returns the GitHub-assigned
// check-run id. Use this when the id is needed for update-in-place on subsequent
// runs. CreateCheckRun is a convenience wrapper that discards the id.
func (c *Client) CreateCheckRunWithID(ctx context.Context, owner, repo string, req CheckRunRequest) (int64, error) {
	body := map[string]any{
		"name":       req.Name,
		"head_sha":   req.HeadSHA,
		"status":     "completed",
		"conclusion": req.Conclusion,
		"output": map[string]string{
			"title":   req.Output.Title,
			"summary": req.Output.Summary,
			"text":    req.Output.Text,
		},
	}
	path := fmt.Sprintf("/repos/%s/%s/check-runs", owner, repo)
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := c.postDo(ctx, path, body, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

// CreateCheckRun creates a completed check run on owner/repo. The check run is
// always submitted with status "completed" and the given conclusion — atcr runs
// to completion before posting, so there is no in-progress phase to report.
// Each call creates a new check run; re-runs accumulate entries on the PR.
// Use CreateCheckRunWithID when the id is needed for update-in-place.
func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, req CheckRunRequest) error {
	_, err := c.CreateCheckRunWithID(ctx, owner, repo, req)
	return err
}

// post marshals body as JSON, POSTs it to path under the API base with the
// standard GitHub headers, and turns any non-2xx response into an error that
// carries the status code and the GitHub error message. The response body is
// discarded on success; use postDo to decode it.
func (c *Client) post(ctx context.Context, path string, body any) error {
	return c.postDo(ctx, path, body, nil)
}

// postDo is the inner implementation shared by post and CreateCheckRunWithID.
// If out is non-nil, the 2xx response body is JSON-decoded into it (best-effort;
// decode errors are silently ignored so callers need not handle partial responses).
func (c *Client) postDo(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	apiBase, err := c.baseURL()
	if err != nil {
		return err
	}
	url := apiBase + path
	makeReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}

	const maxRetries = 3
	backoff := 250 * time.Millisecond
	for attempt := 0; ; attempt++ {
		req, err := makeReq()
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			if attempt < maxRetries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return fmt.Errorf("posting %s: %w", path, err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil {
				_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(out)
			}
			_ = resp.Body.Close()
			return nil
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
		if attempt < maxRetries && (resp.StatusCode >= 500 || resp.StatusCode == 429) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		return fmt.Errorf("github API %s returned %d: %s", path, resp.StatusCode, githubMessage(respBody))
	}
}

// get performs an authenticated GET to path and JSON-decodes the response into
// out (when non-nil). Retries on transient 5xx / 429 with exponential back-off.
func (c *Client) get(ctx context.Context, path string, out any) error {
	apiBase, err := c.baseURL()
	if err != nil {
		return err
	}
	url := apiBase + path
	makeReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		return req, nil
	}

	const maxRetries = 3
	backoff := 250 * time.Millisecond
	for attempt := 0; ; attempt++ {
		req, err := makeReq()
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			if attempt < maxRetries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return fmt.Errorf("getting %s: %w", path, err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			_ = resp.Body.Close()
			if readErr != nil {
				return fmt.Errorf("reading response: %w", readErr)
			}
			if out != nil {
				return json.Unmarshal(body, out)
			}
			return nil
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
		if attempt < maxRetries && (resp.StatusCode >= 500 || resp.StatusCode == 429) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		return fmt.Errorf("github API %s returned %d: %s", path, resp.StatusCode, githubMessage(respBody))
	}
}

// githubMessage extracts the human-readable "message" from a GitHub error
// response body, falling back to the raw (truncated) body when it is not the
// expected shape.
func githubMessage(body []byte) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		return parsed.Message
	}
	return strings.TrimSpace(string(body))
}
