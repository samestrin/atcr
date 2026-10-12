package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

// --max-units (Epic 35.16.14 T5) is the lever the up-front unit-budget refusal
// names, so it must reach the settings resolver: a refusal that points at an
// inert flag would leave the operator no way past it.
func TestReviewCmd_MaxUnitsFlagReachesCLIOverrides(t *testing.T) {
	cmd := newReviewCmd()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }

	require.Nil(t, cliOverrides(cmd).MaxUnits, "unset must stay unset — an absent flag overrides nothing")
	assert.Equal(t, "512", cmd.Flags().Lookup("max-units").DefValue, "help shows the default budget")

	require.NoError(t, cmd.Flags().Set("max-units", "900"))
	got := cliOverrides(cmd).MaxUnits
	require.NotNil(t, got, "a set --max-units must reach CLIOverrides")
	assert.Equal(t, 900, *got)

	s, err := registry.ResolveSettings(cliOverrides(cmd), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 900, s.MaxUnits)
}
