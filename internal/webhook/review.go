package webhook

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/reconcile"
)

const (
	// maxReplyTitles is how many finding titles a reply lists; the rest are
	// counted, not shown.
	maxReplyTitles = 10
	// maxTitleRunes caps one finding title, ellipsis included.
	maxTitleRunes = 120
	// otherSeverity counts a finding whose severity is not a known level.
	otherSeverity = "OTHER"
)

// severityOrder is the order counts are shown in, most severe first.
var severityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", otherSeverity}

// tokenShapes matches Slack and GitHub token shapes the log redactor does not
// know, so a token a model quotes back cannot reach a thread.
var tokenShapes = regexp.MustCompile(`\b(?:xox[a-z]-[A-Za-z0-9-]+|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)

// Reviewer runs a review of the working tree at dir and returns its findings.
// Only each finding's severity and the first line of its problem reach Slack.
type Reviewer func(ctx context.Context, dir string) ([]reconcile.Finding, error)

// ReplyPoster posts a thread reply; *slack.Poster is the real one.
type ReplyPoster interface {
	PostThreadReply(ctx context.Context, channel, threadTS, text string) error
}

// ReviewRunner turns an authorized `review <pr-url>` mention into a job that
// checks the pull request out (WithPRCheckout), reviews it there and replies
// in the mention's thread with severity counts and finding titles only: no
// path, secret or raw model text.
type ReviewRunner struct {
	// Repos maps an allowlisted `owner/repo` to its local checkout.
	Repos map[string]string
	// Allow is who may trigger a review, and on which repositories.
	Allow slack.Allowlist
	// Scratch holds the throwaway worktrees; empty is the system temp directory.
	Scratch string
	// Review runs the review in the worktree.
	Review Reviewer
	// Poster posts the reply. Its own timeout bounds the post.
	Poster ReplyPoster
	// Secrets are credential values (bot token, signing secret, GitHub token)
	// scrubbed from every title even when a model echoes one back.
	Secrets []string
	// Logf receives one line per refused mention.
	Logf func(format string, args ...any)
}

// Dispatch is a Server's Options.Dispatch: it authorizes and parses m and
// returns the review job for it. A refused mention is logged and gets no
// reply, so an unlisted caller learns nothing.
func (r *ReviewRunner) Dispatch(m *slack.AppMention) (Job, bool) {
	if r.Review == nil || r.Poster == nil {
		r.logf("webhook: slack event %s ignored: no reviewer or poster configured", eventID(m))
		return Job{}, false
	}
	cmd, err := slack.ParseReviewCommand(m, r.Allow)
	if err != nil {
		r.logf("webhook: slack event %s refused: %v", eventID(m), err)
		return Job{}, false
	}
	label := cmd.Repo + "#" + strconv.Itoa(cmd.Number)
	thread := m.ThreadTS
	if thread == "" {
		thread = m.TS
	}
	channel := m.Channel
	return Job{Key: label, Run: func(ctx context.Context) error {
		return r.run(ctx, label, cmd, channel, thread)
	}}, true
}

// run reviews the pull request and posts the reply. The reply is posted even
// when ctx is done, so a timed-out job still says so in the thread. The review
// error and the post error are both returned for the job runner to log.
func (r *ReviewRunner) run(ctx context.Context, label string, cmd slack.ReviewCommand, channel, thread string) error {
	var findings []reconcile.Finding
	var dir string
	err := WithPRCheckout(ctx, r.Repos, cmd.Repo, cmd.Number, r.Scratch, func(ctx context.Context, co *Checkout) error {
		dir = co.Dir
		fs, err := r.Review(ctx, co.Dir)
		findings = fs
		return err
	})

	var text string
	if err != nil {
		text = failureText(label, err)
	} else {
		text = summarize(label, findings, newScrubber(dir, r.Secrets))
	}
	if perr := r.Poster.PostThreadReply(context.WithoutCancel(ctx), channel, thread, text); perr != nil {
		err = errors.Join(err, fmt.Errorf("webhook: slack reply for %s: %w", label, perr))
	}
	return err
}

// failureText says that the review of label did not finish, and never why in
// any detail: the cause may carry a path or the model's own words.
func failureText(label string, err error) string {
	head := "*ATCR review of " + slack.EscapeText(label) + "*"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return head + " timed out."
	case errors.Is(err, context.Canceled):
		return head + " was cancelled."
	}
	return head + " failed. The daemon log has the details."
}

// summarize builds the reply: a count per severity, then up to maxReplyTitles
// titles, most severe first.
func summarize(label string, findings []reconcile.Finding, s *scrubber) string {
	if s == nil {
		s = newScrubber("", nil)
	}
	head := "*ATCR review of " + slack.EscapeText(label) + "*"
	if len(findings) == 0 {
		return head + ": no findings."
	}

	type entry struct {
		sev   string
		title string
	}
	counts := map[string]int{}
	entries := make([]entry, 0, len(findings))
	for _, f := range findings {
		sev := reconcile.NormalizeSeverity(f.Severity)
		if _, known := reconcile.SeverityRank[sev]; !known {
			sev = otherSeverity
		}
		counts[sev]++
		entries = append(entries, entry{sev: sev, title: s.title(f.Problem)})
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		return cmp.Compare(reconcile.SeverityRank[b.sev], reconcile.SeverityRank[a.sev])
	})

	var parts []string
	for _, sev := range severityOrder {
		if n := counts[sev]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+sev)
		}
	}
	noun := "findings"
	if len(findings) == 1 {
		noun = "finding"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d %s (%s)", head, len(findings), noun, strings.Join(parts, ", "))
	for i, e := range entries {
		if i == maxReplyTitles {
			fmt.Fprintf(&b, "\n…and %d more not shown.", len(entries)-maxReplyTitles)
			break
		}
		fmt.Fprintf(&b, "\n• [%s] %s", e.sev, e.title)
	}
	return b.String()
}

// scrubber reduces a finding's problem text to a one-line title that carries
// no path and no secret.
type scrubber struct {
	root   string
	redact *log.Redactor
}

func newScrubber(root string, secrets []string) *scrubber {
	return &scrubber{root: root, redact: log.NewRedactor("", secrets...)}
}

// title returns the first line of problem with secrets and token shapes
// redacted, every word that holds a path separator (a path or a URL) replaced
// by [path], truncated to maxTitleRunes and escaped for Slack.
func (s *scrubber) title(problem string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(problem), "\n")
	if s.root != "" {
		line = strings.ReplaceAll(line, s.root, "[path]/")
	}
	line = s.redact.Redact(line)
	line = tokenShapes.ReplaceAllString(line, "[redacted]")
	words := strings.Fields(line)
	for i, w := range words {
		if strings.ContainsAny(w, `/\`) {
			words[i] = "[path]"
		}
	}
	line = strings.Join(words, " ")
	if line == "" {
		return "(untitled finding)"
	}
	if r := []rune(line); len(r) > maxTitleRunes {
		line = strings.TrimSpace(string(r[:maxTitleRunes-1])) + "…"
	}
	return slack.EscapeText(line)
}

func (r *ReviewRunner) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

func eventID(m *slack.AppMention) string {
	if m == nil {
		return ""
	}
	return m.EventID
}
