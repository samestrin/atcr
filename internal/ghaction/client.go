package ghaction

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *Client) baseURL() string {
	if strings.TrimSpace(c.APIURL) != "" {
		return strings.TrimRight(c.APIURL, "/")
	}
	return DefaultAPIURL
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

// CreateCheckRun creates a completed check run on owner/repo. The check run is
// always submitted with status "completed" and the given conclusion — atcr runs
// to completion before posting, so there is no in-progress phase to report.
func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, req CheckRunRequest) error {
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
	return c.post(ctx, path, body)
}

// post marshals body as JSON, POSTs it to path under the API base with the
// standard GitHub headers, and turns any non-2xx response into an error that
// carries the status code and the GitHub error message.
func (c *Client) post(ctx context.Context, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("posting %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("github API %s returned %d: %s", path, resp.StatusCode, githubMessage(respBody))
	}
	return nil
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
