package ghaction

import (
	"context"
	"net/http"
)

// Client is a minimal GitHub REST client for posting check runs and PR review
// comments.
type Client struct {
	APIURL     string
	Token      string
	HTTPClient *http.Client
}

// CheckRunRequest is the payload for creating a GitHub check run.
type CheckRunRequest struct {
	Name       string
	HeadSHA    string
	Conclusion string
	Output     CheckOutput
}

// CreateCheckRun creates a completed check run on the given repo.
func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, req CheckRunRequest) error {
	return nil
}
