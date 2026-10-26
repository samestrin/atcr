package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/webhook"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/spf13/cobra"
)

// slackAPIBaseURL is the Slack Web API root replies are posted to. A var so a
// test can point the daemon at a fake Slack server.
var slackAPIBaseURL = slack.DefaultAPIBaseURL

// webhookReviewerFn builds the reviewer a Slack-triggered job runs. base
// carries the command's logger and hooks; root is the directory the daemon was
// started in, whose .atcr/ config the review uses. A var so a test can stub
// the model fan-out.
var webhookReviewerFn = newWebhookReviewer

// newWebhookCmd builds `atcr webhook`: the daemon that reviews a GitHub pull
// request when someone mentions the Slack app with `review <pr-url>`.
func newWebhookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "webhook",
		Short: "Run the webhook daemon that reviews a pull request when the Slack app is mentioned",
		Long: `Run the webhook daemon. A Slack user mentions the app with
"review https://github.com/<owner>/<repo>/pull/<n>"; atcr checks the request's
signature and the workspace, channel, user and repository allowlists, fetches
the pull request into a throwaway worktree of the configured local checkout,
reviews it and replies in the thread with severity counts and finding titles.

The daemon reads the webhook: block of .atcr/config.yaml in the current
directory, and reviews run with that directory's review config. It binds
loopback only (default ` + registry.DefaultWebhookListen + `), so put a tunnel or
reverse proxy in front of it. Credentials are read from the environment
variables the config names, never from the config itself.

See docs/webhook.md.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: runWebhook,
	}
}

func runWebhook(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath(root))
	if err != nil {
		return usageError(err)
	}
	cfg := proj.Webhook
	if cfg == nil {
		return usageError(errors.New("no webhook: block in .atcr/config.yaml; see docs/webhook.md"))
	}
	if cfg.Slack == nil {
		return usageError(errors.New("webhook.slack is not configured: it is the only trigger, so there is nothing to serve; see docs/webhook.md"))
	}
	signingSecret, err := requiredEnv("webhook.slack.signing_secret_env", cfg.Slack.SigningSecretEnv)
	if err != nil {
		return err
	}
	botToken, err := requiredEnv("webhook.slack.bot_token_env", cfg.Slack.BotTokenEnv)
	if err != nil {
		return err
	}
	secrets := []string{signingSecret, botToken}
	if cfg.GitHubTokenEnv != "" {
		// Read only so the value is scrubbed from every reply: fetching uses
		// the checkout's own git credentials.
		if v := os.Getenv(cfg.GitHubTokenEnv); v != "" {
			secrets = append(secrets, v)
		}
	}

	// Every daemon and review log line scrubs the credential values too.
	ctx = log.NewContext(ctx, log.WithRedactor(log.FromContext(ctx), log.NewRedactor("", secrets...)))
	logger := log.FromContext(ctx)
	logf := func(format string, args ...any) { logger.Info(fmt.Sprintf(format, args...)) }
	repoNames := make([]string, 0, len(cfg.Repos))
	for name := range cfg.Repos {
		repoNames = append(repoNames, name)
	}
	runner := &webhook.ReviewRunner{
		Repos: cfg.Repos,
		Allow: slack.Allowlist{
			Workspaces: cfg.Slack.Workspaces,
			Channels:   cfg.Slack.Channels,
			Users:      cfg.Slack.Users,
			Repos:      repoNames,
		},
		Review:  webhookReviewerFn(ctx, root),
		Poster:  &slack.Poster{Token: botToken, BaseURL: slackAPIBaseURL},
		Secrets: secrets,
		Logf:    logf,
	}
	srv := webhook.NewServer(webhook.Options{
		MaxBodyBytes:       cfg.BodyLimit(),
		JobTimeout:         cfg.JobTimeoutDuration(),
		SlackSigningSecret: signingSecret,
		Dispatch:           runner.Dispatch,
		Logf:               logf,
	})
	ln, err := webhook.Listen(cfg.ListenAddress())
	if err != nil {
		return usageError(err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "atcr webhook: listening on http://%s (Slack events at %s)\n", ln.Addr(), webhook.SlackEventsPath)
	// Serve returns once ctx (cancelled on SIGINT/SIGTERM) is done and the
	// running jobs have been cancelled and cleaned up.
	if err := srv.Serve(ctx, ln); err != nil {
		return err
	}
	return nil
}

// requiredEnv returns the value of the environment variable name, which the
// config key field names. The error names the variable, never a value.
func requiredEnv(field, name string) (string, error) {
	v := os.Getenv(name)
	if strings.TrimSpace(v) == "" {
		return "", usageError(fmt.Errorf("%s names $%s, which is not set", field, name))
	}
	return v, nil
}

// newWebhookReviewer returns the reviewer `atcr webhook` runs: the same fan-out
// and reconcile as `atcr review --fail-on`, over the range from the default
// branch's merge base to the pull request head checked out in dir. Config,
// personas and the review directory come from root, the operator's project,
// so a pull request cannot change how it is reviewed.
func newWebhookReviewer(base context.Context, root string) webhook.Reviewer {
	completer := newCompleter(base)
	return func(jobCtx context.Context, dir string) ([]reclib.Finding, error) {
		ctx, cancel := jobContext(base, jobCtx)
		defer cancel()

		res, err := gitrange.Resolve(ctx, dir, gitrange.Options{})
		if err != nil {
			return nil, fmt.Errorf("resolve range: %w", err)
		}
		cfg, err := fanout.LoadReviewConfig(root, registry.CLIOverrides{})
		if err != nil {
			return nil, err
		}
		rawConsensus, err := registry.ResolveConsensus(root, "")
		if err != nil {
			return nil, err
		}
		consensus, ok := reclib.NormalizeConsensus(rawConsensus)
		if !ok {
			return nil, reclib.InvalidConsensusError(rawConsensus)
		}
		now := time.Now()
		prep, err := fanout.PrepareReview(ctx, cfg, fanout.ReviewRequest{
			Repo: dir,
			Root: root,
			Range: fanout.ReviewRange{
				Base:          res.Base,
				Head:          res.Head,
				DetectionMode: res.DetectionMode,
				DefaultBranch: res.DefaultBranch,
				CommitCount:   res.CommitCount,
			},
			Branch:     gitrange.CurrentBranch(ctx, dir),
			Date:       now.Format("2006-01-02"),
			TimeSuffix: now.Format("150405"),
			StartedAt:  now,
		})
		if err != nil {
			return nil, err
		}
		secrets, _ := prep.SecretValues()
		ctx = correlateAndRedact(ctx, prep.ID, dir, secrets...)
		ctx = hookobs.WithCall(ctx, hookobs.Call{RunID: prep.ID, Stage: "review"})
		if err := preflightAPIKeys(prep.Slots); err != nil {
			return nil, err
		}
		result, err := fanout.ExecuteReview(ctx, completer, prep)
		if err != nil {
			return nil, err
		}
		trustPriors, opportunityScoped, _ := scorecard.ResolveTrustPriorsForReview(result.Dir)
		rec, err := reconcile.RunReconcile(ctx, result.Dir, nil, reclib.Options{
			ReconciledAt:      time.Now(),
			Partial:           result.Summary.Partial,
			Root:              dir,
			TrustPriors:       trustPriors,
			OpportunityScoped: opportunityScoped,
			Consensus:         consensus,
		})
		if err != nil {
			return nil, err
		}
		log.FromContext(ctx).Info("webhook review finished", "review_dir", result.Dir, "findings", len(rec.Findings))
		findings := make([]reclib.Finding, 0, len(rec.Findings))
		for _, m := range rec.Findings {
			findings = append(findings, m.Finding)
		}
		return findings, nil
	}
}

// jobContext returns a context that carries base's values (the logger and the
// hooks) and ends when job does, with job's deadline, so a timed-out review
// reports context.DeadlineExceeded.
func jobContext(base, job context.Context) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(base)
	var cancel context.CancelFunc
	if dl, ok := job.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, dl)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	stop := context.AfterFunc(job, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}
