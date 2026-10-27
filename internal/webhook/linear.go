package webhook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/integrations/linear"
	"github.com/samestrin/atcr/reconcile"
)

// PRLookup finds the GitHub pull request linked to a Linear issue;
// *linear.Client is the real one. Its own timeout bounds the request.
type PRLookup interface {
	PullRequest(ctx context.Context, issueID string) (linear.PR, error)
}

// PRCommenter posts a conversation comment on a pull request.
type PRCommenter interface {
	PostIssueComment(ctx context.Context, owner, repo string, number int, body string) error
}

// LinearRunner turns a verified Linear issue update into a review job: an
// issue of an allowlisted team that moved to a trigger status has its linked
// pull request looked up, and, when that PR's repository is allowlisted,
// checked out (WithPRCheckout) and reviewed there.
type LinearRunner struct {
	// Repos maps an allowlisted `owner/repo` to its local checkout.
	Repos map[string]string
	// Teams are the Linear teams (key or id) whose issues may trigger a review.
	Teams []string
	// Statuses are the workflow state names that trigger a review.
	Statuses []string
	// Lookup finds the issue's linked pull request inside the job.
	Lookup PRLookup
	// Scratch holds the throwaway worktrees; empty is the system temp directory.
	Scratch string
	// Review runs the review in the worktree.
	Review Reviewer
	// Commenter posts the summary on the pull request. nil logs the finding
	// count and posts nothing.
	Commenter PRCommenter
	// Secrets are credential values scrubbed from every finding title.
	Secrets []string
	// Logf receives one line per ignored event and per finished review.
	Logf func(format string, args ...any)
}

// Dispatch is a Server's Options.LinearDispatch. It returns a job only for an
// issue update whose updatedFrom shows the state changed, in an allowlisted
// team, to a trigger status. The job is keyed by the issue until it has looked
// up the pull request, then by the pull request.
func (r *LinearRunner) Dispatch(ev linear.Event) (Job, bool) {
	issue := ev.Data.ID
	switch {
	case r.Review == nil || r.Lookup == nil:
		r.logf("webhook: linear issue %s ignored: no reviewer or PR lookup configured", issue)
		return Job{}, false
	case ev.Type != "Issue" || ev.Action != "update":
		return Job{}, false
	case issue == "" || ev.Data.State.ID == "":
		r.logf("webhook: linear event ignored: no issue or state id")
		return Job{}, false
	case !slices.Contains(ev.UpdatedFromKeys(), "stateId"):
		// An edit that left the state alone: never a trigger.
		return Job{}, false
	case !r.teamAllowed(ev.Data):
		r.logf("webhook: linear issue %s ignored: team not allowlisted", issue)
		return Job{}, false
	case !containsFold(r.Statuses, ev.Data.State.Name):
		return Job{}, false
	}

	var repo string
	var pr linear.PR
	return Job{
		Key: "linear:" + issue,
		Resolve: func(ctx context.Context) (string, error) {
			p, err := r.Lookup.PullRequest(ctx, issue)
			if err != nil {
				return "", fmt.Errorf("linear issue %s: %w", issue, err)
			}
			name, ok := r.repoName(p.Owner + "/" + p.Repo)
			if !ok {
				return "", fmt.Errorf("linear issue %s: %w: %s/%s", issue, ErrRepoNotAllowed, p.Owner, p.Repo)
			}
			repo, pr = name, p
			return name + "#" + strconv.Itoa(p.Number), nil
		},
		Run: func(ctx context.Context) error {
			return r.run(ctx, issue, repo, pr)
		},
	}, true
}

// run reviews the pull request and posts the summary. The comment is posted
// even when ctx is done, so a timed-out job still says so on the PR.
func (r *LinearRunner) run(ctx context.Context, issue, repo string, pr linear.PR) error {
	label := repo + "#" + strconv.Itoa(pr.Number)
	var findings []reconcile.Finding
	var dir string
	err := WithPRCheckout(ctx, r.Repos, repo, pr.Number, r.Scratch, func(ctx context.Context, co *Checkout) error {
		dir = co.Dir
		fs, err := r.Review(ctx, co.Dir)
		findings = fs
		return err
	})
	if r.Commenter == nil {
		if err == nil {
			r.logf("webhook: linear issue %s: review of %s finished with %d findings; no PR commenter configured", issue, label, len(findings))
		}
		return err
	}
	var text string
	if err != nil {
		text = failureText(label, err)
	} else {
		text = summarize(label, findings, newScrubber(dir, r.Secrets))
	}
	if perr := r.Commenter.PostIssueComment(context.WithoutCancel(ctx), pr.Owner, pr.Repo, pr.Number, text); perr != nil {
		err = errors.Join(err, fmt.Errorf("webhook: PR comment for %s: %w", label, perr))
	}
	return err
}

// teamAllowed reports whether the issue's team key or id is allowlisted.
func (r *LinearRunner) teamAllowed(d linear.EventData) bool {
	for _, id := range []string{d.Team.Key, d.Team.ID, d.TeamID} {
		if id != "" && containsFold(r.Teams, id) {
			return true
		}
	}
	return false
}

// repoName returns the configured spelling of name from Repos, matched
// case-insensitively as GitHub does.
func (r *LinearRunner) repoName(name string) (string, bool) {
	for k := range r.Repos {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

func (r *LinearRunner) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// containsFold reports whether list holds s, ignoring case and surrounding
// space.
func containsFold(list []string, s string) bool {
	s = strings.TrimSpace(s)
	return slices.ContainsFunc(list, func(v string) bool { return strings.EqualFold(strings.TrimSpace(v), s) })
}
