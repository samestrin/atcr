package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/spf13/cobra"
)

// newGithubCmd builds `atcr github`: render the reconciled findings of a review
// onto a GitHub pull request as a check run (honoring --fail-on for the merge
// gate). It is the thin CLI wrapper over internal/ghaction; the composite Action
// (action.yml) invokes it after `atcr review` + `atcr reconcile`.
func newGithubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github [id-or-path]",
		Short: "Post reconciled findings to a GitHub pull request as a check run",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runGithub,
	}
	cmd.Flags().String("repo", "", "owner/name target repository (default: $GITHUB_REPOSITORY)")
	cmd.Flags().String("sha", "", "head commit SHA the check is reported against (default: $GITHUB_SHA)")
	cmd.Flags().String("token", "", "GitHub token with checks:write (default: $GITHUB_TOKEN)")
	cmd.Flags().String("api-url", "", "GitHub REST API base (default: $GITHUB_API_URL or https://api.github.com)")
	cmd.Flags().String("fail-on", "", "exit 1 (and mark the check failed) if any finding at/above this severity survives")
	cmd.Flags().String("check-name", "atcr", "name of the GitHub check run")
	cmd.Flags().Bool("inline-comments", false, "also post inline PR review comments (default: check + artifacts only)")
	cmd.Flags().Int("pr", 0, "pull request number (required with --inline-comments)")
	return cmd
}

// envOr returns flag when non-empty, else the named environment variable.
func envOr(flag, env string) string {
	if strings.TrimSpace(flag) != "" {
		return flag
	}
	return strings.TrimSpace(os.Getenv(env))
}

// parseRepo splits an "owner/name" slug into its parts, erroring on any other
// shape so a misconfigured GITHUB_REPOSITORY fails fast (exit 2).
func parseRepo(slug string) (owner, repo string, err error) {
	parts := strings.Split(strings.TrimSpace(slug), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("--repo must be owner/name, got %q", slug)
	}
	return parts[0], parts[1], nil
}

func runGithub(cmd *cobra.Command, args []string) error {
	failOnFlag, _ := cmd.Flags().GetString("fail-on")
	failOn := strings.TrimSpace(failOnFlag)
	// Validate the gate threshold before any I/O so a bad value is a usage error
	// (exit 2), consistent with `atcr reconcile`.
	if failOn != "" {
		canon, err := reconcile.ParseSeverity(failOn)
		if err != nil {
			return usageError(err)
		}
		failOn = canon
	}

	repoFlag, _ := cmd.Flags().GetString("repo")
	owner, repo, err := parseRepo(envOr(repoFlag, "GITHUB_REPOSITORY"))
	if err != nil {
		return usageError(err)
	}

	tokenFlag, _ := cmd.Flags().GetString("token")
	token := envOr(tokenFlag, "GITHUB_TOKEN")
	if token == "" {
		return usageError(errors.New("a GitHub token is required (pass --token or set GITHUB_TOKEN)"))
	}

	shaFlag, _ := cmd.Flags().GetString("sha")
	sha := envOr(shaFlag, "GITHUB_SHA")
	if sha == "" {
		return usageError(errors.New("a head commit SHA is required (pass --sha or set GITHUB_SHA)"))
	}

	apiURLFlag, _ := cmd.Flags().GetString("api-url")
	apiURL := envOr(apiURLFlag, "GITHUB_API_URL")

	// Validate the inline-comments/pr pairing before any network call so a
	// misconfiguration is a fast usage error (exit 2), not masked by a later
	// check-post failure.
	inline, _ := cmd.Flags().GetBool("inline-comments")
	pr, _ := cmd.Flags().GetInt("pr")
	if inline && pr <= 0 {
		return usageError(errors.New("--inline-comments requires --pr <number>"))
	}

	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := anchorDir(arg)
	if err != nil {
		return usageError(err)
	}
	findings, err := readReconciledFindings(reviewDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return usageError(err) // absent data → exit 2 (usage: run reconcile first)
		}
		return &codedError{code: exitFailure, err: err} // present-but-malformed → exit 1
	}

	checkName, _ := cmd.Flags().GetString("check-name")
	conclusion, failCount := ghaction.Conclusion(findings, failOn)
	output := ghaction.BuildCheckOutput(findings, failOn)

	client := &ghaction.Client{APIURL: apiURL, Token: token}

	// Inline comments are opt-in (AC4): the check + artifacts are the baseline,
	// comments are the enhancement. Post them before the check run so the check
	// output can reflect the posted count.
	if inline {
		posted, deduped, err := postInlineComments(cmd, client, owner, repo, pr, sha, findings)
		if err != nil {
			return err
		}
		if posted > 0 || deduped > 0 {
			output.Text += fmt.Sprintf("\n\n_Inline comments: %d posted, %d already present._", posted, deduped)
		}
	}

	if err := client.CreateCheckRun(cmd.Context(), owner, repo, ghaction.CheckRunRequest{
		Name:       checkName,
		HeadSHA:    sha,
		Conclusion: conclusion,
		Output:     output,
	}); err != nil {
		// A failure to reach GitHub is an operational error, not a gate verdict:
		// exit 1 so CI surfaces it, distinct from a clean gate pass.
		return &codedError{code: exitFailure, err: err}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted check %q to %s/%s @ %s: %s (%d finding(s))\n",
		checkName, owner, repo, sha, conclusion, len(findings))

	// When running inside a GitHub Actions workflow, expose the machine-readable
	// result so downstream steps can branch on the gate verdict.
	if ghOutput := os.Getenv("GITHUB_OUTPUT"); ghOutput != "" {
		f, err := os.OpenFile(ghOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "conclusion=%s\nfindings=%d\n", conclusion, len(findings))
			_ = f.Close()
		}
	}

	// The merge gate also rides the process exit code, so a consumer can gate on
	// either the check conclusion or the step's exit status.
	if conclusion == ghaction.ConclusionFailure {
		return &codedError{code: exitFailure, err: fmt.Errorf("%d finding(s) at or above %s", failCount, failOn)}
	}
	return nil
}

// postInlineComments posts anchorable findings as a single batched PR review.
// It first lists existing PR review comments to skip any that atcr already
// posted (dedup across re-runs). It returns the number of new comments posted
// and the number skipped because they were already present.
func postInlineComments(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, findings []reconcile.JSONFinding) (int, int, error) {
	comments := ghaction.BuildInlineComments(findings)
	if len(comments) == 0 {
		return 0, 0, nil
	}

	existing, err := client.ListReviewComments(cmd.Context(), owner, repo, pr)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not list existing comments (dedup skipped): %v\n", err)
	}
	comments, deduped := deduplicateComments(comments, existing)

	if len(comments) == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted 0 inline comment(s) to %s/%s#%d (%d already present)\n", owner, repo, pr, deduped)
		return 0, deduped, nil
	}

	if err := client.CreatePRReview(cmd.Context(), owner, repo, pr, ghaction.PRReviewRequest{
		CommitID: sha,
		Comments: comments,
	}); err != nil {
		var apiErr *ghaction.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) could not be posted (HTTP 422 — comments may be off-diff): %v\n", len(comments), apiErr)
			return 0, deduped, nil
		}
		return 0, deduped, &codedError{code: exitFailure, err: fmt.Errorf("%d inline comment(s) failed to post: %w", len(comments), err)}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) to %s/%s#%d (%d already present)\n", len(comments), owner, repo, pr, deduped)
	return len(comments), deduped, nil
}

// deduplicateComments removes comments whose path:line already has an ATCR
// comment in the existing list. Returns the filtered slice and the dedup count.
func deduplicateComments(comments []ghaction.CommentRequest, existing []ghaction.ReviewComment) ([]ghaction.CommentRequest, int) {
	if len(existing) == 0 {
		return comments, 0
	}
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		if strings.HasPrefix(e.Body, "ATCR found:") {
			seen[fmt.Sprintf("%s:%d", e.Path, e.Line)] = true
		}
	}
	var out []ghaction.CommentRequest
	for _, c := range comments {
		if !seen[fmt.Sprintf("%s:%d", c.Path, c.Line)] {
			out = append(out, c)
		}
	}
	return out, len(comments) - len(out)
}
