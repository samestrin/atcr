package ghaction

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// bearerTokenPattern matches an Authorization "Bearer <token>" form so a token
// echoed back in a GitHub error body is scrubbed even when it is not the exact
// configured Token literal. Mirrors llmclient.bearerTokenPattern.
var bearerTokenPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// DefaultAPIURL is the public GitHub REST API base. It is overridable via the
// Client.APIURL field so the action works against GitHub Enterprise and so tests
// can point the client at an httptest server.
const DefaultAPIURL = "https://api.github.com"

// APIError is returned by postDo for non-2xx, non-retriable responses.
// Callers can inspect StatusCode to distinguish expected failures (e.g. 422
// Unprocessable for off-diff inline comments) from systemic errors (401/403).
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github API returned %d: %s", e.StatusCode, e.Message)
}

// Client is a minimal GitHub REST client. It posts check runs and PR review
// comments, and — for the --auto-fix flow — creates branches, commits, and pull
// requests via the Git Data and Pulls APIs (CreateBranch, CreateCommit,
// CreatePullRequest, UpdatePullRequest, FindOpenPullRequest). Those mutating
// methods require a token with `contents: write` (branch/commit) and
// `pull_requests: write` (PR create/update) scope. A zero HTTPClient falls back
// to a sane default; a zero APIURL falls back to the public GitHub API. Timeout
// overrides the default HTTP client timeout when HTTPClient is nil.
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

// CreateBranch creates a new Git ref refs/heads/<branch> pointing at sha via
// POST /repos/{owner}/{repo}/git/refs. The branch is normalized to a full
// refs/heads/ ref inside the method, so callers pass a bare branch name. A ref
// that already exists surfaces as GitHub's 422 *APIError (StatusCode 422,
// "Reference already exists") unchanged, so a caller can errors.As it and decide
// whether to retry with a suffixed name — the collision policy is the caller's,
// not this wrapper's.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, branch, sha string) error {
	ref := branch
	if !strings.HasPrefix(ref, "refs/heads/") {
		ref = "refs/heads/" + ref
	}
	path := fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo)
	return c.postDo(ctx, path, map[string]any{"ref": ref, "sha": sha}, nil)
}

// CommitFile is one path in a CommitRequest. Content is the new file bytes for a
// modify/create; Deleted marks the path for removal (expressed as a null-SHA tree
// entry, with no blob created).
type CommitFile struct {
	Path    string
	Content string
	Deleted bool
}

// CommitRequest describes a single atomic commit to create on a branch. ParentSHA
// is the PARENT COMMIT SHA (e.g. the base-branch HEAD), not a tree SHA —
// CreateCommit resolves the base tree from it.
type CommitRequest struct {
	Branch    string
	Message   string
	ParentSHA string
	Files     []CommitFile
}

// CreateCommit builds one atomic commit from req.Files and advances the branch
// ref to it, following GitHub's Git Data API sequence:
//
//	GET   /git/commits/{ParentSHA}   → resolve the parent's tree SHA (base_tree)
//	POST  /git/blobs                 → one blob per non-deleted file
//	POST  /git/trees                 → a tree over base_tree, one entry per file
//	POST  /git/commits               → a commit on the new tree, parented at ParentSHA
//	PATCH /git/refs/heads/{Branch}   → advance the branch ref to the new commit
//
// A deleted file creates no blob and becomes a null-SHA tree entry. Any step's
// failure short-circuits the rest — in particular the ref is never advanced after
// a failed commit — and the returned error names the failed step. The returned
// string is the new commit SHA (from the commit-creation response, not the ref
// update). An empty Files set is rejected before any HTTP call. All calls route
// through postDo / its PATCH sibling, inheriting the existing retry/back-off/
// redaction — no bespoke http.Client.Do anywhere.
func (c *Client) CreateCommit(ctx context.Context, owner, repo string, req CommitRequest) (string, error) {
	if len(req.Files) == 0 {
		return "", fmt.Errorf("creating commit: no files to commit")
	}
	// Reject trivially-malformed requests before any HTTP call: an empty ParentSHA
	// would produce a malformed parent-read, and an empty Branch would let the
	// commit be created and then orphaned by a bad ref PATCH.
	if strings.TrimSpace(req.ParentSHA) == "" {
		return "", fmt.Errorf("creating commit: empty parent SHA")
	}
	if strings.TrimSpace(req.Branch) == "" {
		return "", fmt.Errorf("creating commit: empty branch")
	}
	repoPath := func(suffix string) string {
		return fmt.Sprintf("/repos/%s/%s/%s", owner, repo, suffix)
	}

	// 1. Resolve the parent commit's tree SHA (the tree GitHub calls base_tree).
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.get(ctx, repoPath("git/commits/"+req.ParentSHA), &parent); err != nil {
		return "", fmt.Errorf("reading parent commit %s: %w", req.ParentSHA, err)
	}
	if parent.Tree.SHA == "" {
		return "", fmt.Errorf("reading parent commit %s: response carried no tree sha", req.ParentSHA)
	}

	// 2. Create a blob per non-deleted file and assemble the tree entries.
	treeEntries := make([]map[string]any, 0, len(req.Files))
	for _, f := range req.Files {
		entry := map[string]any{"path": f.Path, "mode": "100644", "type": "blob"}
		if f.Deleted {
			entry["sha"] = nil // an explicit null sha removes the path from the tree
			treeEntries = append(treeEntries, entry)
			continue
		}
		var blob struct {
			SHA string `json:"sha"`
		}
		blobBody := map[string]any{
			"content":  base64.StdEncoding.EncodeToString([]byte(f.Content)),
			"encoding": "base64",
		}
		if err := c.postDo(ctx, repoPath("git/blobs"), blobBody, &blob); err != nil {
			return "", fmt.Errorf("creating blob for path %q: %w", f.Path, err)
		}
		if blob.SHA == "" {
			return "", fmt.Errorf("creating blob for path %q: response carried no sha", f.Path)
		}
		entry["sha"] = blob.SHA
		treeEntries = append(treeEntries, entry)
	}

	// 3. Create the tree over the resolved base tree.
	var tree struct {
		SHA string `json:"sha"`
	}
	treeBody := map[string]any{"base_tree": parent.Tree.SHA, "tree": treeEntries}
	if err := c.postDo(ctx, repoPath("git/trees"), treeBody, &tree); err != nil {
		return "", fmt.Errorf("creating tree: %w", err)
	}
	if tree.SHA == "" {
		return "", fmt.Errorf("creating tree: response carried no sha")
	}

	// 4. Create the commit on the new tree, parented at ParentSHA.
	var commit struct {
		SHA string `json:"sha"`
	}
	commitBody := map[string]any{
		"message": c.redactSecrets(req.Message),
		"tree":    tree.SHA,
		"parents": []string{req.ParentSHA},
	}
	if err := c.postDo(ctx, repoPath("git/commits"), commitBody, &commit); err != nil {
		return "", fmt.Errorf("creating commit: %w", err)
	}
	if commit.SHA == "" {
		return "", fmt.Errorf("creating commit: response carried no sha")
	}

	// 5. Advance the branch ref to the new commit. The commit object exists on
	// GitHub even if this fails, so the error makes the branch/commit split clear.
	branch := strings.TrimPrefix(req.Branch, "refs/heads/")
	refBody := map[string]any{"sha": commit.SHA, "force": false}
	if err := c.sendDo(ctx, http.MethodPatch, repoPath("git/refs/heads/"+branch), refBody, nil); err != nil {
		return "", fmt.Errorf("updating ref refs/heads/%s (commit %s created but branch not advanced): %w", branch, commit.SHA, err)
	}
	return commit.SHA, nil
}

// PullRequestRequest is the caller-supplied content for opening or updating a PR.
// Title and Body are run through redactSecrets before being sent because — unlike
// the read-only check-run/comment endpoints — they can carry dynamic content
// sourced from local validation diagnostics that could echo a credential.
type PullRequestRequest struct {
	Head  string
	Base  string
	Title string
	Body  string
}

// CreatePullRequest opens a new PR via POST /repos/{owner}/{repo}/pulls and
// returns the GitHub-assigned PR number. Title/Body are redacted on the way out.
// A 422 (invalid base, or a duplicate-PR race) surfaces as a typed *APIError via
// postDo so the caller can treat it as a non-fatal condition rather than a crash.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, req PullRequestRequest) (int, error) {
	body := map[string]any{
		"head":  req.Head,
		"base":  req.Base,
		"title": c.redactSecrets(req.Title),
		"body":  c.redactSecrets(req.Body),
	}
	var resp struct {
		Number int `json:"number"`
	}
	if err := c.postDo(ctx, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), body, &resp); err != nil {
		return 0, err
	}
	// postDo ignores body-decode errors, so a 2xx whose body carried no number
	// would otherwise return (0, nil). A real PR number is always >= 1; surface 0
	// as an error so an orchestrator never misreads it as "not created" and opens
	// a duplicate PR.
	if resp.Number == 0 {
		return 0, fmt.Errorf("creating pull request: response carried no PR number")
	}
	return resp.Number, nil
}

// UpdatePullRequest refreshes an existing PR's title/body via
// PATCH /repos/{owner}/{repo}/pulls/{prNumber}, reusing the PATCH-capable sendDo.
// Both title and body are always sent (redacted) from the caller-supplied req —
// the client does no partial-field diffing. A 404 (PR closed/deleted between the
// existence check and this call) surfaces as a typed *APIError.
func (c *Client) UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, req PullRequestRequest) error {
	body := map[string]any{
		"title": c.redactSecrets(req.Title),
		"body":  c.redactSecrets(req.Body),
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	return c.sendDo(ctx, http.MethodPatch, path, body, nil)
}

// FindOpenPullRequest returns the number of the lowest-numbered OPEN pull request
// whose head is owner:branch, or found=false when none exist (an empty result is
// not an error). It uses GET /repos/{owner}/{repo}/pulls?head={owner}:{branch}&
// state=open via get, inheriting its retry/back-off; the branch is query-escaped.
// Multiple matches (e.g. a manually opened PR) resolve deterministically to the
// lowest number so a re-run refreshes a stable PR rather than opening a third.
//
// Exported so the Phase-5 internal/autofix orchestrator can run the existence
// check before choosing CreatePullRequest vs UpdatePullRequest (AC 05-02) — the
// create-vs-update decision itself lives in that orchestrator, not here.
func (c *Client) FindOpenPullRequest(ctx context.Context, owner, repo, branch string) (int, bool, error) {
	q := url.Values{}
	q.Set("head", owner+":"+branch)
	q.Set("state", "open")
	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, repo, q.Encode())
	var prs []struct {
		Number int `json:"number"`
	}
	if err := c.get(ctx, path, &prs); err != nil {
		return 0, false, err
	}
	if len(prs) == 0 {
		return 0, false, nil
	}
	lowest := prs[0].Number
	for _, pr := range prs[1:] {
		if pr.Number < lowest {
			lowest = pr.Number
		}
	}
	return lowest, true, nil
}

// sleepCtx waits for d to elapse or for ctx to be cancelled, whichever happens
// first. It returns ctx.Err() if the context is cancelled during the wait so
// that retry back-off is interruptible by cancellation or shutdown.
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

// post marshals body as JSON, POSTs it to path under the API base with the
// standard GitHub headers, and turns any non-2xx response into an error that
// carries the status code and the GitHub error message. The response body is
// discarded on success; use postDo to decode it.
func (c *Client) post(ctx context.Context, path string, body any) error {
	return c.postDo(ctx, path, body, nil)
}

// postDo issues a POST through sendDo. If out is non-nil, the 2xx response body
// is JSON-decoded into it (best-effort; decode errors are silently ignored so
// callers need not handle partial responses).
func (c *Client) postDo(ctx context.Context, path string, body any, out any) error {
	return c.sendDo(ctx, http.MethodPost, path, body, out)
}

// sendDo marshals body as JSON, issues an authenticated `method` request to path
// under the API base with the standard GitHub headers, retries transient 5xx/429
// responses with the existing exponential back-off, and JSON-decodes a 2xx
// response into out (when non-nil). Non-2xx, non-retriable responses become an
// *APIError carrying the status code and the redacted GitHub message. It is the
// shared implementation behind postDo (POST) and the PATCH ref-update / PR-update
// call sites, so every mutating endpoint inherits identical auth/retry/redaction.
func (c *Client) sendDo(ctx context.Context, method, path string, body any, out any) error {
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
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "atcr")
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
				if err := sleepCtx(ctx, backoff); err != nil {
					return err
				}
				backoff *= 2
				continue
			}
			return fmt.Errorf("posting %s: %w", path, err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil {
				// 8MB limit (matching get): a PR object or git/trees listing can
				// exceed 8KB, and the decoded number/sha is load-bearing for the
				// new mutating methods — truncation must not silently drop it.
				_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
			}
			_ = resp.Body.Close()
			return nil
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
		if attempt < maxRetries && (resp.StatusCode >= 500 || resp.StatusCode == 429) {
			if err := sleepCtx(ctx, backoff); err != nil {
				return err
			}
			backoff *= 2
			continue
		}
		return &APIError{StatusCode: resp.StatusCode, Message: c.redactSecrets(githubMessage(respBody))}
	}
}

// get performs an authenticated GET to path and JSON-decodes the response into
// out (when non-nil). Retries on transient 5xx / 429 with exponential back-off.
// A non-2xx, non-retriable response becomes an *APIError (matching sendDo) so a
// caller — e.g. CreateCommit reading a missing base SHA — can errors.As it and
// inspect StatusCode rather than string-matching the message.
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
		req.Header.Set("User-Agent", "atcr")
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
				if err := sleepCtx(ctx, backoff); err != nil {
					return err
				}
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
			if err := sleepCtx(ctx, backoff); err != nil {
				return err
			}
			backoff *= 2
			continue
		}
		return &APIError{StatusCode: resp.StatusCode, Message: c.redactSecrets(githubMessage(respBody))}
	}
}

// redactSecrets scrubs the client's credential from a string before it is embedded
// in an error: the configured Token literal (in case GitHub echoes the Authorization
// header back in an error body) plus any generic "Bearer <token>" form. Mirrors
// llmclient.redactErrorSnippet so ghaction errors never carry credentials.
func (c *Client) redactSecrets(s string) string {
	if c.Token != "" {
		s = strings.ReplaceAll(s, c.Token, "[redacted]")
	}
	return bearerTokenPattern.ReplaceAllString(s, "Bearer [redacted]")
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
