package webhook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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

// ErrForkPR is returned for an auto-fix of a pull request whose head branch
// lives in another repository: the fix pull request could not target it.
var ErrForkPR = errors.New("webhook: auto-fix refused: the pull request head is in another repository")

// PRHeadLookup finds a pull request's head branch and the `owner/repo` holding
// it; *ghaction.Client is the real one.
type PRHeadLookup interface {
	PullRequestHead(ctx context.Context, owner, repo string, number int) (ref, headRepo string, err error)
}

// AutoFixer runs one auto-fix job; *AutoFixRunner is the real one.
type AutoFixer interface {
	Run(ctx context.Context, t AutoFixTarget) error
}

// LinearAutoFix is the auto-fix half of a LinearRunner (Epic 49.2): an issue of
// an allowlisted team that moved to one of Statuses runs `atcr review
// --auto-fix` on its linked pull request, when that PR's repository is in
// Repos. Each issue and state starts at most one job, ever, and at most
// State's daily cap start per UTC day.
type LinearAutoFix struct {
	// Statuses are the workflow state names that start an auto-fix. A status
	// also listed in LinearRunner.Statuses starts the auto-fix, not a review.
	Statuses []string
	// Repos is the auto-fix allowlist; each must also be a key of
	// LinearRunner.Repos.
	Repos []string
	// State persists the issue and state claims and the day's job count.
	State *AutoFixState
	// Heads finds the linked pull request's head branch, which the fix pull
	// request targets.
	Heads PRHeadLookup
	// Fixer runs the job.
	Fixer AutoFixer
	// Commenter posts the daily job and token cap refusals on the pull
	// request. nil logs them.
	Commenter PRCommenter
	// Now is the clock for the daily cap; nil is time.Now.
	Now func() time.Time

	// serial, under a daily token cap, holds one job from its claim until its
	// fixer returns, so the next claim sees that job's charge.
	serialOnce sync.Once
	serial     chan struct{}
}

// lockSerial takes the token-cap lock, or returns ctx's error once ctx is
// done first. A job waiting behind another stays cancellable.
func (a *LinearAutoFix) lockSerial(ctx context.Context) error {
	a.serialOnce.Do(func() { a.serial = make(chan struct{}, 1) })
	select {
	case a.serial <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *LinearAutoFix) unlockSerial() { <-a.serial }

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
	// AutoFix, when set, turns a change to an auto-fix status into an
	// auto-fix job instead of a review.
	AutoFix *LinearAutoFix
}

// Dispatch is a Server's Options.LinearDispatch. It returns a job only for an
// issue update whose updatedFrom shows the state changed, in an allowlisted
// team, to a trigger status or an auto-fix status. The job is keyed by the
// issue for its whole run, so one issue runs one job at a time, and also by
// the pull request once it has looked that up.
func (r *LinearRunner) Dispatch(ev linear.Event) (Job, bool) {
	issue := ev.Data.ID
	switch {
	case r.AutoFix == nil && (r.Review == nil || r.Lookup == nil):
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
	case r.AutoFix != nil && containsFold(r.AutoFix.Statuses, ev.Data.State.Name):
		return r.autoFixJob(issue, ev.Data.State.ID)
	case !containsFold(r.Statuses, ev.Data.State.Name):
		return Job{}, false
	case r.Review == nil || r.Lookup == nil:
		r.logf("webhook: linear issue %s ignored: no reviewer or PR lookup configured", issue)
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
		text = failureText(label, err, githubFormat)
	} else {
		text = summarize(label, findings, newScrubber(dir, r.Secrets), githubFormat)
	}
	if perr := r.Commenter.PostIssueComment(context.WithoutCancel(ctx), pr.Owner, pr.Repo, pr.Number, text); perr != nil {
		err = errors.Join(err, fmt.Errorf("webhook: PR comment for %s: %w", label, perr))
	}
	return err
}

// autoFixJob returns the auto-fix job for issue, which moved to state. The
// issue and state are claimed only once the job is about to run, so a lookup
// failure or a busy pull request uses neither the claim nor the daily cap.
func (r *LinearRunner) autoFixJob(issue, state string) (Job, bool) {
	a := r.AutoFix
	if r.Lookup == nil || a.State == nil || a.Heads == nil || a.Fixer == nil {
		r.logf("webhook: linear issue %s ignored: auto-fix is not fully configured", issue)
		return Job{}, false
	}
	var target AutoFixTarget
	var pr linear.PR
	return Job{
		Key: "linear:" + issue,
		Resolve: func(ctx context.Context) (string, error) {
			p, err := r.Lookup.PullRequest(ctx, issue)
			if err != nil {
				return "", fmt.Errorf("linear issue %s: %w", issue, err)
			}
			name, ok := r.repoName(p.Owner + "/" + p.Repo)
			if !ok || !containsFold(a.Repos, name) {
				return "", fmt.Errorf("linear issue %s: auto-fix: %w: %s/%s", issue, ErrRepoNotAllowed, p.Owner, p.Repo)
			}
			pr, target = p, AutoFixTarget{Repo: name, Number: p.Number}
			return name + "#" + strconv.Itoa(p.Number), nil
		},
		Run: func(ctx context.Context) error {
			return r.runAutoFix(ctx, issue, state, pr, target)
		},
	}, true
}

// runAutoFix looks up the pull request's head branch, claims the issue and
// state, and runs the fix. Once the day's job cap or token cap is used it posts
// a refusal on the pull request instead, carrying the cap and nothing else. A
// job that went over its per-job token cap is reported once it has run.
//
// Under a daily token cap one lock is held from the claim until the fixer
// returns: a job is charged only when it finishes, so a claim made while
// another job runs would check the cap against a total missing that job's
// spend. Without a token cap jobs run at once.
func (r *LinearRunner) runAutoFix(ctx context.Context, issue, state string, pr linear.PR, t AutoFixTarget) error {
	a := r.AutoFix
	label := t.Repo + "#" + strconv.Itoa(t.Number)
	ref, headRepo, err := a.Heads.PullRequestHead(ctx, pr.Owner, pr.Repo, pr.Number)
	if err != nil {
		return fmt.Errorf("webhook: auto-fix of %s: head branch: %w", label, err)
	}
	if !strings.EqualFold(headRepo, t.Repo) {
		return fmt.Errorf("%w: %s", ErrForkPR, label)
	}
	t.Base = ref
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	if a.State.DailyTokenCap() > 0 {
		if err := a.lockSerial(ctx); err != nil {
			return fmt.Errorf("webhook: auto-fix of %s: waiting for the running job: %w", label, err)
		}
		defer a.unlockSerial()
	}
	err = a.State.Claim(issue, state, now())
	switch {
	case errors.Is(err, ErrAutoFixDailyCap):
		text := fmt.Sprintf("ATCR auto-fix did not run for %s: daily cap reached (%d auto-fix jobs per UTC day). "+
			"Move the Linear issue to the auto-fix status again tomorrow to retry.", label, a.State.dailyCap)
		return r.refuseAutoFix(ctx, issue, label, pr, "daily cap reached", text, err)
	case errors.Is(err, ErrAutoFixDailyTokenCap):
		text := fmt.Sprintf("ATCR auto-fix did not run for %s: daily token cap reached (%d tokens per UTC day). "+
			"Move the Linear issue to the auto-fix status again tomorrow to retry.", label, a.State.DailyTokenCap())
		return r.refuseAutoFix(ctx, issue, label, pr, "daily token cap reached", text, err)
	case err != nil:
		return fmt.Errorf("linear issue %s: %w", issue, err)
	}
	r.logf("webhook: linear issue %s: auto-fix of %s started against %s", issue, label, t.Base)
	err = a.Fixer.Run(ctx, t)
	if errors.Is(err, ErrAutoFixJobTokenCap) {
		r.logf("webhook: linear issue %s: auto-fix of %s went over its per-job token cap; its spend is charged to the day", issue, label)
	}
	return err
}

// refuseAutoFix posts text, a cap refusal, on the pull request and returns err
// joined with any failure to post it. Without a PR commenter it logs reason.
func (r *LinearRunner) refuseAutoFix(ctx context.Context, issue, label string, pr linear.PR, reason, text string, err error) error {
	a := r.AutoFix
	if a.Commenter == nil {
		r.logf("webhook: linear issue %s: auto-fix of %s refused: %s; no PR commenter configured", issue, label, reason)
		return err
	}
	if perr := a.Commenter.PostIssueComment(context.WithoutCancel(ctx), pr.Owner, pr.Repo, pr.Number, text); perr != nil {
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
