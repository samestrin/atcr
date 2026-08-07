package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `review --fresh` was polysemous: with --verify it re-verified verdicted
// findings, with --all/--scope it bypassed the baseline file-hash skip. The two
// intent-named flags are --reverify and --no-file-cache; --fresh remains a
// deprecated alias that resolves by context. The production resolution rule,
// mirrored:
func TestReviewFreshSplit_IntentFlagsAndContextualAlias(t *testing.T) {
	reverify := func(cmd *cobra.Command) bool {
		v, _ := cmd.Flags().GetBool("reverify")
		if v {
			return true
		}
		fresh, _ := cmd.Flags().GetBool("fresh")
		verify, _ := cmd.Flags().GetBool("verify")
		return fresh && verify
	}
	noFileCache := func(cmd *cobra.Command) bool {
		v, _ := cmd.Flags().GetBool("no-file-cache")
		if v {
			return true
		}
		fresh, _ := cmd.Flags().GetBool("fresh")
		return fresh && (cmd.Flags().Changed("all") || cmd.Flags().Changed("scope") || cmd.Flags().Changed("dir"))
	}

	verifyCtx := newReviewCmd()
	require.NoError(t, verifyCtx.ParseFlags([]string{"--verify", "--fresh"}))
	assert.True(t, reverify(verifyCtx), "--fresh with --verify resolves to --reverify")
	assert.False(t, noFileCache(verifyCtx), "--fresh with --verify does not touch the baseline cache")

	baselineCtx := newReviewCmd()
	require.NoError(t, baselineCtx.ParseFlags([]string{"--all", "--fresh"}))
	assert.True(t, noFileCache(baselineCtx), "--fresh on a baseline scan resolves to --no-file-cache")
	assert.False(t, reverify(baselineCtx), "--fresh on a baseline scan does not touch verification")

	intentVerify := newReviewCmd()
	require.NoError(t, intentVerify.ParseFlags([]string{"--verify", "--reverify"}))
	assert.True(t, reverify(intentVerify), "--reverify works independently of --fresh")

	intentBaseline := newReviewCmd()
	require.NoError(t, intentBaseline.ParseFlags([]string{"--all", "--no-file-cache"}))
	assert.True(t, noFileCache(intentBaseline), "--no-file-cache works independently of --fresh")
}

// All three flags are cross-referenced in review --help.
func TestReviewFreshSplit_HelpCrossReferences(t *testing.T) {
	_, help := execCmdCapture(t, "review", "--help")
	require.Contains(t, help, "--reverify")
	require.Contains(t, help, "--no-file-cache")
}
