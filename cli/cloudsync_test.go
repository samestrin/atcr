package cli

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCloudSyncPushable_SkipsCodedInfraFailures covers the review.go:428 TD: the
// deferred --sync-cloud push must fire only for a finalized review outcome (success
// or a plain exit-1 findings-gate / all-agents-failed error), NOT for an already-coded
// exit-2 infra failure (usage/verify/debate I/O broke) — pushing then would make a
// needless network call and send the backend a misleading CloudSyncRecord.
func TestCloudSyncPushable_SkipsCodedInfraFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, true},
		{"findings-gate (plain exit-1)", fmt.Errorf("3 finding(s) survived"), true},
		{"all-agents-failed (plain exit-1)", errors.New("all agents failed"), true},
		{"usage/infra failure (exit-2 coded)", usageError(errors.New("reconcile I/O failed")), false},
		{"wrapped exit-2 coded", fmt.Errorf("outer: %w", usageError(errors.New("verify failed"))), false},
		{"auth failure (exit-3 coded)", authError(errors.New("bad key")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cloudSyncPushable(tc.err))
		})
	}
}

func TestResolveSyncCloud_DisabledWhenFlagOmitted(t *testing.T) {
	cmd := newReconcileCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	plan, err := resolveSyncCloud(cmd)
	require.NoError(t, err)
	assert.False(t, plan.enabled)
}

// The explicit --cloud-endpoint in both API-key tests is load-bearing (Epic
// 35.12): defaultCloudEndpoint is now empty, and resolveSyncCloud validates the
// endpoint BEFORE reading the key, so without an endpoint these would exit 2 on
// the destination and never reach the auth check they exist to cover.
func TestResolveSyncCloud_MissingAPIKey_AuthError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "")
	cmd := newReconcileCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "https://ingest.example.com"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestResolveSyncCloud_WhitespaceAPIKey_AuthError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "   ")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "https://ingest.example.com"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

// TestResolveSyncCloud_EmptyEndpoint_UsageError pins the defense-in-depth layer
// behind the PreRunE refusal: a caller that reaches resolveSyncCloud with no
// destination (bypassing the flag helper) still fails closed as a usage error
// rather than attempting a push at an empty URL.
func TestResolveSyncCloud_EmptyEndpoint_UsageError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestResolveSyncCloud_InvalidEndpoint_UsageError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "not-a-url"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestResolveSyncCloud_NonLoopbackHTTP_Rejected(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "http://example.com"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestResolveSyncCloud_ValidKeyAndEndpoint_Enabled(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "https://mock.test/ingest"}))
	plan, err := resolveSyncCloud(cmd)
	require.NoError(t, err)
	assert.True(t, plan.enabled)
	assert.Equal(t, "valid-key", plan.apiKey)
	assert.Equal(t, "https://mock.test/ingest", plan.endpoint)
}

func TestResolveSyncCloud_EndpointTrimmedOnce(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "  https://mock.test/ingest  "}))
	plan, err := resolveSyncCloud(cmd)
	require.NoError(t, err)
	assert.True(t, plan.enabled)
	assert.Equal(t, "https://mock.test/ingest", plan.endpoint)
}

func TestFinishCloudSync_AuthRejectedMapsToExitAuth(t *testing.T) {
	var buf bytes.Buffer
	err := finishCloudSync(&buf, scorecard.ErrCloudAuthRejected)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestFinishCloudSync_WrappedAuthRejectedMapsToExitAuth(t *testing.T) {
	var buf bytes.Buffer
	err := finishCloudSync(&buf, fmt.Errorf("wrap: %w", scorecard.ErrCloudAuthRejected))
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestFinishCloudSync_GenericFailureIsNonFatalWarning(t *testing.T) {
	var buf bytes.Buffer
	// The input error deliberately lacks the "warning: " label and the words the
	// assertion searches for, so the match can only come from finishCloudSync's
	// own formatting — the "clearly-labeled warning" contract the doc comment names.
	err := finishCloudSync(&buf, errors.New("server returned 500"))
	assert.NoError(t, err, "a non-auth push failure must not change the exit code")
	assert.Equal(t, "warning: server returned 500\n", buf.String(),
		"a non-fatal push failure must surface with the warning: label the contract depends on")
}

func TestFinishCloudSync_NilIsNoop(t *testing.T) {
	var buf bytes.Buffer
	assert.NoError(t, finishCloudSync(&buf, nil))
	assert.Empty(t, buf.String())
}

// TestResolveSyncCloudOutcome covers the 4.LAST gate fix: an auth rejection
// overrides a success or a plain findings-gate failure (exit 1), but must NOT mask
// an already-coded (exit 2) usage/infra failure.
func TestResolveSyncCloudOutcome(t *testing.T) {
	authErr := authError(errors.New("auth rejected"))
	usage := usageError(errors.New("reconcile I/O failed")) // exit 2
	gate := errors.New("findings survived")                 // plain → exit 1

	// No push error → the run's own outcome is returned unchanged.
	assert.Nil(t, resolveSyncCloudOutcome(nil, nil))
	assert.Equal(t, usage, resolveSyncCloudOutcome(usage, nil))
	assert.Equal(t, gate, resolveSyncCloudOutcome(gate, nil))

	// Auth rejection overrides a success and a plain findings-gate failure.
	assert.Equal(t, exitAuth, exitCode(resolveSyncCloudOutcome(nil, authErr)))
	assert.Equal(t, exitAuth, exitCode(resolveSyncCloudOutcome(gate, authErr)))

	// Auth rejection must NOT mask an already-coded (exit 2) infra/usage failure.
	assert.Equal(t, exitUsage, exitCode(resolveSyncCloudOutcome(usage, authErr)))
	assert.Equal(t, usage, resolveSyncCloudOutcome(usage, authErr), "the original coded error is preserved verbatim")

	// A non-auth sync error never overrides the run outcome.
	timeout := errors.New("sync timeout")
	assert.Equal(t, gate, resolveSyncCloudOutcome(gate, timeout), "non-auth sync error must preserve the run error")
	assert.Equal(t, exitFailure, exitCode(resolveSyncCloudOutcome(gate, timeout)))
}
