package doctor

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredMaxTokensTarget resolves a single agent that declares its own output cap.
func declaredMaxTokensTarget(t *testing.T, declared int) *Resolution {
	t.Helper()
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", MaxTokens: &declared}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	return res
}

// The ok_warning hint tells the operator to "raise --max-tokens", but probe() already
// applies the agent's DECLARED cap whenever the flag was not typed. Raising doctor's own
// flag therefore changes only the probe budget — `atcr review` still resolves the same
// declaration (resolveMaxTokens), so the operator gets a green doctor on an agent that
// still truncates to zero findings on the real run. The two actions that change the real
// run — raising the agent's max_tokens declaration, or passing --max-tokens to review —
// are named nowhere in the hint.
func TestRun_OKWarningHintNamesTheActionThatChangesTheRealRun(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := declaredMaxTokensTarget(t, 32000)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", nil // HTTP 200, marker absent — the ok_warning path
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})

	require.Len(t, rep.Agents, 1)
	require.Equal(t, StatusOKWarning, rep.Agents[0].Status, "precondition: the marker-absent warning fired")
	hint := rep.Agents[0].Hint

	assert.Contains(t, hint, "max_tokens",
		"the agent's own declaration is what `atcr review` resolves; the hint must name it")
	assert.Contains(t, hint, "atcr review --max-tokens",
		"the other action that changes the real run must be named as the review-side flag, "+
			"not as doctor's own flag")
}

// The probe applies the declared cap silently, so the report cannot be read to find out
// which budget produced the result: an operator seeing ok_warning has no way to tell
// whether the probe ran at the declaration or at doctor's default. Report it on the row,
// the same pre-flight view ContextWindowTokens/WindowSource already provide for the
// window declaration.
func TestRun_ReportsTheCapTheProbeActuallyUsed(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	fake := func() *fakeCompleter {
		return newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })
	}

	t.Run("declared cap when the flag was not typed", func(t *testing.T) {
		rep := Run(context.Background(), fake(), declaredMaxTokensTarget(t, 32000),
			Options{Nonce: testNonce, MaxTokens: 2048})
		require.Len(t, rep.Agents, 1)
		assert.Equal(t, 32000, rep.Agents[0].MaxTokens,
			"the row must report the cap the probe used, which is the declaration here")
	})

	t.Run("flag value when the operator typed it", func(t *testing.T) {
		rep := Run(context.Background(), fake(), declaredMaxTokensTarget(t, 32000),
			Options{Nonce: testNonce, MaxTokens: 777, MaxTokensSet: true})
		require.Len(t, rep.Agents, 1)
		assert.Equal(t, 777, rep.Agents[0].MaxTokens,
			"an explicit --max-tokens wins, and the row must say so")
	})
}
