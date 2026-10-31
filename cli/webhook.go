package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/integrations/linear"
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

// linearAPIURL is the Linear GraphQL endpoint a Linear-triggered job looks the
// linked pull request up on. A var so a test can point it at a fake.
var linearAPIURL = linear.DefaultAPIURL

// githubAPIURL is the GitHub REST root the Linear summary comment is posted to;
// empty means the public API. A var so a test can point it at a fake server.
var githubAPIURL string

// autoFixBinary is the atcr executable an auto-fix job runs; empty is the
// running executable. A var so a test can point it at a stub.
var autoFixBinary = ""

// autoFixStatePath is where the daemon keeps its auto-fix claims and daily job
// count: under the operator's .atcr directory, so they survive a restart.
func autoFixStatePath(root string) string {
	return filepath.Join(root, ".atcr", "webhook", "auto-fix-state.json")
}

// webhookReviewerFn builds the reviewer a Slack- or Linear-triggered job runs. base
// carries the command's logger and hooks; root is the directory the daemon was
// started in, whose .atcr/ config the review uses. A var so a test can stub
// the model fan-out.
var webhookReviewerFn = newWebhookReviewer

// newWebhookCmd builds `atcr webhook`: the daemon that reviews a GitHub pull
// request when someone mentions the Slack app with `review <pr-url>`, or when
// a Linear issue linked to it moves to a trigger status.
func newWebhookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "webhook",
		Short: "Run the webhook daemon that reviews a pull request from a Slack mention or a Linear status change",
		Long: `Run the webhook daemon. A Slack user mentions the app with
"review https://github.com/<owner>/<repo>/pull/<n>"; atcr checks the request's
signature and the workspace, channel, user and repository allowlists, fetches
the pull request into a throwaway worktree of the configured local checkout,
reviews it and replies in the thread with severity counts and finding titles.

With a webhook.linear block, a signed Linear issue update whose state changed
to one of trigger_statuses, in one of teams, reviews the GitHub pull request
attached to the issue when its repository is in repos. A redelivery, or a
second delivery of the same status change, runs one review, and one review
runs per pull request at a time.

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
	if cfg.Slack == nil && cfg.Linear == nil {
		return usageError(errors.New("neither webhook.slack nor webhook.linear is configured, so there is nothing to serve; see docs/webhook.md"))
	}
	var signingSecret, botToken, linearSecret, linearKey string
	var secrets []string
	if cfg.Slack != nil {
		if signingSecret, err = requiredEnv("webhook.slack.signing_secret_env", cfg.Slack.SigningSecretEnv); err != nil {
			return err
		}
		if botToken, err = requiredEnv("webhook.slack.bot_token_env", cfg.Slack.BotTokenEnv); err != nil {
			return err
		}
		secrets = append(secrets, signingSecret, botToken)
	}
	if cfg.Linear != nil {
		if linearSecret, err = requiredEnv("webhook.linear.webhook_secret_env", cfg.Linear.WebhookSecretEnv); err != nil {
			return err
		}
		if linearKey, err = requiredEnv("webhook.linear.api_key_env", cfg.Linear.APIKeyEnv); err != nil {
			return err
		}
		secrets = append(secrets, linearSecret, linearKey)
	}
	var pushToken string
	if cfg.Linear != nil && cfg.Linear.AutoFix != nil {
		if pushToken, err = requiredEnv("webhook.linear.auto_fix.push_token_env", cfg.Linear.AutoFix.PushTokenEnv); err != nil {
			return err
		}
		secrets = append(secrets, pushToken)
	}
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
	review := webhookReviewerFn(ctx, root)
	opts := webhook.Options{
		MaxBodyBytes: cfg.BodyLimit(),
		JobTimeout:   cfg.JobTimeoutDuration(),
		Logf:         logf,
	}
	var routes []string
	if cfg.Slack != nil {
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
			Review:  review,
			Poster:  &slack.Poster{Token: botToken, BaseURL: slackAPIBaseURL},
			Secrets: secrets,
			Logf:    logf,
		}
		opts.SlackSigningSecret = signingSecret
		opts.Dispatch = runner.Dispatch
		routes = append(routes, "Slack events at "+webhook.SlackEventsPath)
	}
	if cfg.Linear != nil {
		runner := &webhook.LinearRunner{
			Repos:    cfg.Repos,
			Teams:    cfg.Linear.Teams,
			Statuses: cfg.Linear.TriggerStatuses,
			Lookup:   &linear.Client{APIKey: linearKey, URL: linearAPIURL},
			Review:   review,
			Secrets:  secrets,
			Logf:     logf,
		}
		// The summary goes on the pull request only when a GitHub token is
		// configured; without one the job logs the finding count instead.
		if cfg.GitHubTokenEnv != "" {
			if tok := os.Getenv(cfg.GitHubTokenEnv); tok != "" {
				runner.Commenter = &ghaction.Client{APIURL: githubAPIURL, Token: tok}
			}
		}
		if runner.Commenter == nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "atcr webhook: warning: webhook.linear is set but no GitHub token resolves from webhook.github_token_env, so Linear review findings will only be logged, not posted to the pull request")
		}
		if a := cfg.Linear.AutoFix; a != nil {
			state, err := webhook.OpenAutoFixState(autoFixStatePath(root), a.DailyJobCap)
			if err != nil {
				return usageError(err)
			}
			// The push token reads the PR head and posts the cap refusal: the
			// review token may be unset, and is never handed push rights.
			gh := &ghaction.Client{APIURL: githubAPIURL, Token: pushToken}
			runner.AutoFix = &webhook.LinearAutoFix{
				Statuses: a.Statuses,
				Repos:    a.Repos,
				State:    state,
				Heads:    gh,
				Fixer: &webhook.AutoFixRunner{
					Repos:        cfg.Repos,
					Binary:       autoFixBinary,
					PushTokenEnv: a.PushTokenEnv,
					Secrets:      secrets,
					Logf:         logf,
				},
				Commenter: gh,
			}
		}
		opts.LinearSigningSecret = linearSecret
		opts.LinearDispatch = runner.Dispatch
		routes = append(routes, "Linear webhooks at "+webhook.LinearWebhookPath)
	}
	srv := webhook.NewServer(opts)
	ln, err := webhook.Listen(cfg.ListenAddress())
	if err != nil {
		return usageError(err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "atcr webhook: listening on http://%s (%s)\n", ln.Addr(), strings.Join(routes, "; "))
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
// personas, ignore rules and the review directory come from root, the
// operator's project, so a pull request cannot change how it is reviewed.
func newWebhookReviewer(base context.Context, root string) webhook.Reviewer {
	completer := newCompleter(base)
	return func(jobCtx context.Context, dir string) ([]reclib.Finding, error) {
		var ctx context.Context = jobContext{Context: jobCtx, values: base}
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
		// Reviewer roles for report.md's `name (role)` labels (Epic 83.0 T5);
		// display metadata, so a failed write leaves bare names.
		_ = reconcile.WriteReviewerRoles(prep.Dir, registry.ReviewerRoles(cfg.Registry.Agents, cfg.PersonaDirs))
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
			IgnoreRoot:        root, // ignore.yaml follows the operator's config, so a pull request cannot suppress its own findings
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

// jobContext is the job's context with base's values (the logger and the
// hooks) layered over it. Done, Err and Deadline are the job's own, so a
// timed-out review reports context.DeadlineExceeded and a shutdown reports
// context.Canceled, while the command's cancellation never reaches a job.
type jobContext struct {
	context.Context
	values context.Context
}

func (c jobContext) Value(key any) any {
	if v := c.values.Value(key); v != nil {
		return v
	}
	return c.Context.Value(key)
}
