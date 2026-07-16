package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// syncCloudPlan carries the resolved --sync-cloud preconditions so the actual
// push (which runs after the command's primary outcome is finalized) needs no
// further env/flag reads. enabled is false when --sync-cloud was not passed.
type syncCloudPlan struct {
	enabled  bool
	endpoint string
	apiKey   string
}

// resolveSyncCloud validates the --sync-cloud preconditions ONCE, at the top of a
// review/reconcile run, so a missing key or bad endpoint fails fast — before the
// expensive run and any network call. When --sync-cloud is not set it returns a
// disabled plan and never reads ATCR_API_KEY (AC 04-03 EC2). A missing, empty, or
// whitespace-only ATCR_API_KEY is an authError (exit 3 — fail closed, never fall
// through to a push); an empty or non-https (non-loopback) --cloud-endpoint is a
// usageError (exit 2). It is deliberately NOT gated by telemetryGate: --sync-cloud
// is an explicit, user-invoked action with its own opt-in surface (the flag plus a
// valid ATCR_API_KEY), independent of the passive-ping opt-out.
func resolveSyncCloud(cmd *cobra.Command) (syncCloudPlan, error) {
	if !boolFlag(cmd, "sync-cloud") {
		return syncCloudPlan{}, nil
	}
	endpoint, _ := cmd.Flags().GetString("cloud-endpoint")
	endpoint = strings.TrimSpace(endpoint)
	if err := scorecard.ValidateCloudEndpoint(endpoint); err != nil {
		return syncCloudPlan{}, usageError(err)
	}
	// Read ATCR_API_KEY via os.Getenv + TrimSpace, matching the LOG_LEVEL-read
	// pattern; a whitespace-only key is treated identically to unset (AC 04-03 EC1).
	apiKey := strings.TrimSpace(os.Getenv("ATCR_API_KEY"))
	if apiKey == "" {
		return syncCloudPlan{}, authError(errors.New("ATCR_API_KEY is not set; --sync-cloud requires a valid API key"))
	}
	return syncCloudPlan{enabled: true, endpoint: endpoint, apiKey: apiKey}, nil
}

// runSyncCloud builds the CloudSyncRecord from the finalized run at reviewDir and
// pushes it, when the plan is enabled. It runs AFTER the command's primary outcome
// is determined, so a push failure never corrupts that outcome: an auth rejection
// (401/403) is returned as an authError (exit 3, overriding the run's own code per
// AC 04-04), while every other failure is a non-fatal warning to w and returns nil
// (the underlying exit code is preserved — AC 04-02).
func runSyncCloud(ctx context.Context, w io.Writer, plan syncCloudPlan, reviewDir, outcome string) error {
	if !plan.enabled {
		return nil
	}
	rec := scorecard.NewCloudSyncRecord(reviewDir, outcome)
	return finishCloudSync(w, scorecard.Push(ctx, plan.endpoint, plan.apiKey, rec))
}

// cloudSyncPushable reports whether a run reached a finalized review outcome that a
// --sync-cloud record should describe. STUB (RED): always true.
func cloudSyncPushable(err error) bool {
	return true
}

// resolveSyncCloudOutcome combines the run's own outcome (runErr) with the result
// of the cloud-sync push (syncErr, always nil or an authError from finishCloudSync)
// into the command's final return. An auth rejection (exit 3) overrides a SUCCESS
// (nil) or a plain findings-gate failure (a non-coded, exit-1 error) — per AC 04-04
// — but MUST NOT mask an already-coded failure (a usage/config/infra error, exit 2):
// misreporting a reconcile/verify I/O failure as an auth failure would hide the real
// cause and its message. This bounds the auth override to the same blast radius as
// reconcile.go, where the push can only ever supersede the exit-1 findings gate.
func resolveSyncCloudOutcome(runErr, syncErr error) error {
	if syncErr == nil {
		return runErr
	}
	// Only an auth rejection (exit 3) is allowed to override the run outcome;
	// any other sync error is treated as non-fatal and the run outcome is preserved.
	var syncCoded *codedError
	if !errors.As(syncErr, &syncCoded) || syncCoded.code != exitAuth {
		return runErr
	}
	var runCoded *codedError
	if runErr == nil || !errors.As(runErr, &runCoded) {
		return syncErr
	}
	return runErr // preserve an already-coded (usage/config/infra) failure
}

// finishCloudSync maps a scorecard.Push error to the command's return value: an
// auth rejection becomes an authError (exit 3); any other push failure is a
// non-fatal, clearly-labeled warning to w and returns nil (the already-finalized
// run outcome is preserved). A nil push error is a no-op.
func finishCloudSync(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, scorecard.ErrCloudAuthRejected) {
		return authError(errors.New("cloud sync rejected: ATCR_API_KEY was not accepted by the server"))
	}
	_, _ = fmt.Fprintf(w, "warning: %v\n", err)
	return nil
}
