package doctor

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The verdict must speak for the cap `atcr review` will resolve, which is the
// agent's own declaration whenever review is invoked without its own --max-tokens.
// Suppressing the verdict whenever DOCTOR's flag was typed threw that away with it:
// for an agent declaring max_tokens 32000 on the 32768 default window, `atcr review`
// ships the smallest single file (or refuses under on_overflow fail/fallback), plain
// `atcr doctor` says so — and `atcr doctor --max-tokens 4096` reported ok with an
// empty hint. The declaration is what closes the budget, and doctor's flag does not
// reach review, so the flag cannot be a reason to stay silent about it.
func TestRun_ZeroBudgetVerdictFiresForADeclaredCapDespiteDoctorsOwnFlag(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	const declared = 32000
	cap := declared
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", MaxTokens: &cap}},
	)
	// `atcr doctor --max-tokens 4096`: the probe runs at 4096, which leaves the
	// window plenty of room — so nothing about the PROBE is alarming, and only the
	// declaration review will resolve reveals the closed budget.
	res, err := ResolveWithCap(reg, &registry.ProjectConfig{Agents: []string{"a"}}, 4096)
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 4096, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	require.Equal(t, MaxTokensSourceFlag, got.MaxTokensSource, "precondition: the PROBE was capped by the flag")
	require.Equal(t, 4096, got.MaxTokens, "precondition: the probe really did run at the flag value")

	assert.Equal(t, StatusOKWarning, got.Status,
		"the agent's OWN declaration closes the budget review will run at; doctor's flag does not reach review, "+
			"so it cannot be a reason to suppress that")
	assert.Contains(t, got.Hint, "32000",
		"the hint must name the cap REVIEW will resolve, not the 4096 this probe happened to use")
	assert.Contains(t, got.Hint, zeroBudgetRemedy)
}
