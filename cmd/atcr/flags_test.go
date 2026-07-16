package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile covers AC 04-01: both
// subcommands expose --sync-cloud (bool, default false) and --cloud-endpoint
// (string, default = the documented dashboard endpoint).
func TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile(t *testing.T) {
	for _, mk := range []func() *cobra.Command{newReviewCmd, newReconcileCmd} {
		cmd := mk()

		sc := cmd.Flags().Lookup("sync-cloud")
		require.NotNil(t, sc, "sync-cloud must be registered on %q", cmd.Name())
		assert.Equal(t, "bool", sc.Value.Type())
		assert.Equal(t, "false", sc.DefValue)

		ce := cmd.Flags().Lookup("cloud-endpoint")
		require.NotNil(t, ce, "cloud-endpoint must be registered on %q", cmd.Name())
		assert.Equal(t, "string", ce.Value.Type())
		assert.Equal(t, defaultCloudEndpoint, ce.DefValue)
	}
}

// TestAddSyncCloudFlags_PreservesPriorPreRunE covers AC 04-01 EC2: review
// registers addRangeFlags THEN addSyncCloudFlags, and the range-validation
// PreRunE must still fire (chained, not overwritten) — --head without --base is
// still a usage error.
func TestAddSyncCloudFlags_PreservesPriorPreRunE(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--head", "x"}))
	require.NotNil(t, cmd.PreRunE)
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err, "addRangeFlags PreRunE must survive addSyncCloudFlags")
	assert.Equal(t, exitUsage, exitCode(err))
}
