package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --max-tokens is the run-wide escape hatch for a reviewer that truncates mid-
// reasoning. `atcr doctor` printed "raise --max-tokens" for that symptom long before
// `atcr review` had the flag, so the hint named a lever that did not exist. This
// pins that the flag reaches the settings resolver — a flag registered but never
// read into CLIOverrides is exactly as inert as no flag at all.
func TestReviewCmd_MaxTokensFlagReachesCLIOverrides(t *testing.T) {
	cmd := newReviewCmd()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }

	require.Nil(t, cliOverrides(cmd).MaxTokens, "unset must stay unset — an absent flag overrides nothing")

	require.NoError(t, cmd.Flags().Set("max-tokens", "32000"))
	got := cliOverrides(cmd).MaxTokens
	require.NotNil(t, got, "a set --max-tokens must reach CLIOverrides")
	assert.Equal(t, 32000, *got)
}
