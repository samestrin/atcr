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
	// 16000, not 32000: a 32000 cap against the 32768 default window closes the input
	// budget entirely, and zeroBudgetVerdict then (correctly) replaces this hint with one
	// that tells the operator NOT to raise the cap. That is a different state with its own
	// coverage in zero_budget_test.go. This test is about the marker-absent remedy, so its
	// fixture has to be an agent whose only problem IS the marker.
	res := declaredMaxTokensTarget(t, 16000)
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

// The cap alone cannot say whether a declaration took effect — the same argument
// WindowSource exists for, stated in render.go and docs/registry.md: "a declaration that
// was ignored and a static-table hit that happens to match look identical." Three origins
// are otherwise indistinguishable in the report: an operator flag, the agent's own
// declaration, and doctor's built-in default (an agent declaring exactly 2048 reads
// identically to an undeclared agent at the default).
func TestRun_ReportsWhereTheProbedCapCameFrom(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	fake := func() *fakeCompleter {
		return newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })
	}

	t.Run("declaration", func(t *testing.T) {
		rep := Run(context.Background(), fake(), declaredMaxTokensTarget(t, 32000),
			Options{Nonce: testNonce, MaxTokens: 2048})
		require.Len(t, rep.Agents, 1)
		assert.Equal(t, 32000, rep.Agents[0].MaxTokens)
		assert.Equal(t, MaxTokensSourceDeclaration, rep.Agents[0].MaxTokensSource,
			"the agent's own max_tokens supplied the cap")
	})

	t.Run("flag", func(t *testing.T) {
		rep := Run(context.Background(), fake(), declaredMaxTokensTarget(t, 32000),
			Options{Nonce: testNonce, MaxTokens: 777, MaxTokensSet: true})
		require.Len(t, rep.Agents, 1)
		assert.Equal(t, MaxTokensSourceFlag, rep.Agents[0].MaxTokensSource,
			"an explicit --max-tokens overrode the declaration, and the report must say which won")
	})

	t.Run("default", func(t *testing.T) {
		// twoAgentSharedTarget declares no max_tokens, so doctor's own default applies.
		rep := Run(context.Background(), fake(), twoAgentSharedTarget(t),
			Options{Nonce: testNonce, MaxTokens: 2048})
		require.NotEmpty(t, rep.Agents)
		assert.Equal(t, MaxTokensSourceDefault, rep.Agents[0].MaxTokensSource,
			"an undeclared agent at the flag default must not read as a declaration")
	})
}

// The ok_warning hint must not tell an operator to raise a declaration their OWN flag is
// overriding. With --max-tokens 100 against an agent declaring 32000, "raise this agent's
// max_tokens declaration" is advice that cannot work: probe() used 100 because the flag
// won, and raising 32000 changes nothing about this probe.
func TestRun_OKWarningHintDoesNotBlameTheDeclarationWhenTheFlagWon(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	// 2000, not 32000, for the same reason the sibling test above picks 16000: a
	// declaration at or above 28672 closes the input budget on the 32768 default
	// window, and zeroBudgetVerdict then (correctly) replaces this hint with one that
	// tells the operator NOT to raise any cap. This test is about which knob the
	// MARKER-ABSENT hint names, so its fixture must not also be a closed budget. The
	// flag below is still lower than the declaration, which is what the assertions need.
	res := declaredMaxTokensTarget(t, 2000)
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return "", nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 100, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	require.Equal(t, StatusOKWarning, rep.Agents[0].Status, "precondition: marker absent")
	hint := rep.Agents[0].Hint

	assert.NotContains(t, hint, "raise this agent's max_tokens declaration",
		"the flag is what capped this probe; the declaration is already higher and raising it is a no-op")
	assert.Contains(t, hint, "--max-tokens",
		"the hint must point at the knob that actually governed this probe")
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
