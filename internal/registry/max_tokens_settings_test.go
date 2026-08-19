package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The --max-tokens CLI tier had coverage on either SIDE of ResolveSettings and none
// crossing it: cli/review_max_tokens_test.go pins flag -> CLIOverrides, and
// internal/fanout/max_tokens_test.go pins resolveMaxTokens once a value is already in
// Settings. Nothing exercised the resolution itself, so its 1..MaxTokensCap rejection
// survived deletion with ./internal/registry ./cli ./internal/fanout green — meaning
// `--max-tokens 0` and `--max-tokens 99999999` were accepted by every test in the repo.
//
// The two rejected values are not symmetric and both matter: 0 is the not-set sentinel
// everywhere else in this file (payload_byte_budget, max_parallel both treat it as
// "unlimited"/"unbounded"), so an operator who typed it asked for something that cannot
// work and must be told rather than silently defaulted; the cap catches a typo'd order
// of magnitude before it becomes a provider-side error mid-run.
func TestResolveSettings_MaxTokens_CLITierAcceptsAndRejects(t *testing.T) {
	t.Run("an in-range override reaches Settings", func(t *testing.T) {
		v := 32000
		s, err := ResolveSettings(CLIOverrides{MaxTokens: &v}, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, v, s.MaxTokens,
			"the resolved value is what the fan-out's resolveMaxTokens reads as its override")
	})

	t.Run("the boundary values are accepted", func(t *testing.T) {
		for _, v := range []int{1, MaxTokensCap} {
			v := v
			s, err := ResolveSettings(CLIOverrides{MaxTokens: &v}, nil, nil)
			require.NoError(t, err, "the range is inclusive at both ends")
			assert.Equal(t, v, s.MaxTokens)
		}
	})

	t.Run("zero is rejected, not treated as unset", func(t *testing.T) {
		v := 0
		_, err := ResolveSettings(CLIOverrides{MaxTokens: &v}, nil, nil)
		require.Error(t, err,
			"a zero output cap would make every reviewer call return nothing; silently substituting the "+
				"default would hide the mistake behind a run that looks normal")
		assert.Contains(t, err.Error(), "max_tokens")
	})

	t.Run("a negative override is rejected", func(t *testing.T) {
		v := -1
		_, err := ResolveSettings(CLIOverrides{MaxTokens: &v}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max_tokens")
	})

	t.Run("an over-cap override is rejected", func(t *testing.T) {
		v := MaxTokensCap + 1
		_, err := ResolveSettings(CLIOverrides{MaxTokens: &v}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max_tokens")
	})

	t.Run("unset leaves the zero sentinel the fan-out reads as no override", func(t *testing.T) {
		s, err := ResolveSettings(CLIOverrides{}, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, s.MaxTokens,
			"0 here means 'no run-wide override', which is what lets an agent declaration win")
	})
}

// validateAgent's max_tokens bound was the new registry field's ONLY validation and
// was itself untested — it survived deletion, so a declaration of 0 (every reviewer
// call returns nothing) or a typo'd order of magnitude loaded clean and failed later
// at the provider, mid-run. The valid case runs alongside so this pins the bound
// rather than the field being rejected outright.
func TestLoadRegistry_AgentMaxTokensBound(t *testing.T) {
	load := func(t *testing.T, value string) error {
		t.Helper()
		_, err := LoadRegistry(writeRegistry(t, validRegistry+"    max_tokens: "+value+"\n"))
		return err
	}

	t.Run("an in-range declaration loads", func(t *testing.T) {
		require.NoError(t, load(t, "32000"))
	})

	t.Run("the boundaries load", func(t *testing.T) {
		require.NoError(t, load(t, "1"))
		require.NoError(t, load(t, "1000000"))
	})

	for _, tc := range []struct{ name, value string }{
		{"zero", "0"},
		{"negative", "-1"},
		{"over the cap", "1000001"},
	} {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			err := load(t, tc.value)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "max_tokens")
			assert.Contains(t, err.Error(), "greta",
				"the error must name the offending agent — a roster-wide message is not actionable")
		})
	}
}

// The registry tier's chunk_byte_budget check was the one tier left unguarded:
// TestChunkByteBudget_RejectsNegative covers the project tier (LoadProjectConfig) and
// the resolved backstop (ResolveSettings), so this check survived deletion. A negative
// value set in ~/.config/atcr/registry.yaml would then reach ResolveSettings — which
// does catch it, but reports it without naming the registry file it came from.
func TestLoadRegistry_ChunkByteBudgetRejectsNegative(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, validRegistry+"\nchunk_byte_budget: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_byte_budget")

	_, err = LoadRegistry(writeRegistry(t, validRegistry+"\nchunk_byte_budget: 0\n"))
	require.NoError(t, err, "0 is the documented unlimited escape hatch, not an invalid value")
}
