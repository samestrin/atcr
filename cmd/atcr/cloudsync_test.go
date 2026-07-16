package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSyncCloud_DisabledWhenFlagOmitted(t *testing.T) {
	cmd := newReconcileCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	plan, err := resolveSyncCloud(cmd)
	require.NoError(t, err)
	assert.False(t, plan.enabled)
}

func TestResolveSyncCloud_MissingAPIKey_AuthError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "")
	cmd := newReconcileCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestResolveSyncCloud_WhitespaceAPIKey_AuthError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "   ")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud"}))
	_, err := resolveSyncCloud(cmd)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestResolveSyncCloud_InvalidEndpoint_UsageError(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "not-a-url"}))
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

func TestFinishCloudSync_AuthRejectedMapsToExitAuth(t *testing.T) {
	var buf bytes.Buffer
	err := finishCloudSync(&buf, scorecard.ErrCloudAuthRejected)
	require.Error(t, err)
	assert.Equal(t, exitAuth, exitCode(err))
}

func TestFinishCloudSync_GenericFailureIsNonFatalWarning(t *testing.T) {
	var buf bytes.Buffer
	err := finishCloudSync(&buf, errors.New("cloud sync failed: server returned 500"))
	assert.NoError(t, err, "a non-auth push failure must not change the exit code")
	assert.Contains(t, buf.String(), "cloud sync failed")
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
}
