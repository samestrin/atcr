package ghaction

import (
	"context"

	"github.com/samestrin/atcr/internal/reconcile"
)

// CommentRequest is a single inline PR review comment anchored at Path:Line.
type CommentRequest struct {
	Path string
	Line int
	Side string
	Body string
}

// BuildInlineComments renders one inline comment per anchorable finding.
func BuildInlineComments(findings []reconcile.JSONFinding) []CommentRequest { return nil }

// CreateReviewComment posts a single inline review comment to a pull request.
func (c *Client) CreateReviewComment(ctx context.Context, owner, repo string, pr int, commitID string, comment CommentRequest) error {
	return nil
}
