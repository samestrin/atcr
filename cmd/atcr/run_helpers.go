package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/spf13/cobra"
)

// correlateAndRedact tags ctx's logger with the review id and installs a
// sink-level redactor rooted at repo. From the returned context on, every
// downstream log line is review_id-correlated (AC9), has the configured secret
// values and secret-shaped tokens scrubbed (AC5; epic 4.9 makes the exact-value
// scrub live for non-sk-/non-Bearer keys), and has absolute paths under repo
// relativized (AC6). The repo root is resolved to an absolute path first because
// the CLI default repo is "." and path relativization no-ops on ".".
//
// secrets are the resolved registry API key values (see PreparedReview.SecretValues);
// they are passed by value into NewRedactor and never logged. Both the fresh-review
// (runReview) and resume (runResume) paths call this so the correlation + redaction
// contract cannot drift between them.
func correlateAndRedact(ctx context.Context, id, repo string, secrets ...string) context.Context {
	ctx = correlateReviewID(ctx, id)
	return log.NewContext(ctx, log.WithRedactor(log.FromContext(ctx), log.NewRedactor(resolveRedactRoot(ctx, repo), secrets...)))
}

// reportInterrupt records a SIGINT/SIGTERM that landed mid-fan-out: it emits the
// structured, review_id-correlated Warn (so monitoring/CI can grep interrupted
// runs), mirrors the human-facing notice to stderr, and returns the exit-1 coded
// error. The completed agents and interrupted manifest are already persisted by
// the engine, so the caller only has to report and stop.
//
// Shared by runReview and runResume so an interrupted fresh review and an
// interrupted resume produce identical Warn records and exit codes.
func reportInterrupt(cmd *cobra.Command, ctx context.Context, result *fanout.ReviewResult, prep *fanout.PreparedReview) error {
	log.FromContext(ctx).Warn("review interrupted by signal")
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
	return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}
