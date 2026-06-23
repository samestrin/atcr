package ghaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// CommentRequest is a single inline PR review comment anchored at Path:Line on
// the RIGHT (post-change) side of the diff.
type CommentRequest struct {
	Path string
	Line int
	Side string
	Body string
}

// BuildInlineComments renders one inline comment per anchorable finding. A
// finding is anchorable only when it carries a file path and a 1-based line; a
// finding with no line (Line == 0, the findings-format sentinel for "unknown")
// or no file cannot be placed in the diff and is skipped — it still appears in
// the check-run summary and the uploaded artifacts.
//
// The body follows the epic's contract: "ATCR found: <problem>. Fix: <fix>.
// Suggested by: <executor>", with the Fix clause omitted when there is no fix
// and the attribution clause omitted when the EVIDENCE field carries no
// "fix by <name>" executor token.
func BuildInlineComments(findings []reconcile.JSONFinding) []CommentRequest {
	var out []CommentRequest
	for _, f := range findings {
		if f.File == "" || f.Line <= 0 || strings.TrimSpace(f.Problem) == "" {
			continue
		}
		out = append(out, CommentRequest{
			Path: f.File,
			Line: f.Line,
			Side: "RIGHT",
			Body: commentBody(f),
		})
	}
	return out
}

// commentBody assembles the inline-comment text for a single finding, following
// the epic's AC3 contract: "ATCR found: <problem>. Fix: <fix>. Suggested by:
// <executor>", with the Fix and attribution clauses omitted when their source
// data is absent.
func commentBody(f reconcile.JSONFinding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ATCR found: %s.", defang(strings.TrimSpace(f.Problem)))
	if fix := defang(strings.TrimSpace(f.Fix)); fix != "" {
		fmt.Fprintf(&b, " Fix: %s.", fix)
	}
	if who := FixAttribution(f.Evidence); who != "" {
		fmt.Fprintf(&b, " Suggested by: %s.", who)
	}
	return b.String()
}

// defang neutralizes GitHub Markdown injection vectors in untrusted model output.
// It backslash-escapes @ (mention) and # (issue-ref) characters, and removes the
// HTML-comment open sequence (<!--) so crafted content cannot inject notifications
// or hide text when posted to the GitHub API.
func defang(s string) string {
	s = strings.ReplaceAll(s, "<!--", "<!-")
	s = strings.ReplaceAll(s, "@", `\@`)
	s = strings.ReplaceAll(s, "#", `\#`)
	return s
}

// CreateReviewComment posts a single inline review comment to a pull request.
// commitID is the PR head SHA the comment is anchored to (GitHub requires it to
// match a commit in the PR).
func (c *Client) CreateReviewComment(ctx context.Context, owner, repo string, pr int, commitID string, comment CommentRequest) error {
	side := comment.Side
	if side == "" {
		side = "RIGHT"
	}
	body := map[string]any{
		"body":      comment.Body,
		"commit_id": commitID,
		"path":      comment.Path,
		"line":      comment.Line,
		"side":      side,
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", owner, repo, pr)
	return c.post(ctx, path, body)
}
