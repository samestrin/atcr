package doctor

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zeroBudgetTarget resolves one agent whose declared cap leaves no input budget once the
// prompt overhead is reserved out of its resolved window. The window is left UNDECLARED
// on purpose: an unknown model resolves to payload's 32768 default, which is the
// documented normal case for a litellm proxy alias, so this is an ordinary config rather
// than a contrived one.
func zeroBudgetTarget(t *testing.T, declaredCap int) *Resolution {
	t.Helper()
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", MaxTokens: &declaredCap}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	return res
}

// doctor is the only surface that holds BOTH operands deciding whether a review can run
// at all — the resolved window and the resolved output cap — and it printed them side by
// side without ever comparing them. payload.EffectiveByteBudget returns 0 once
// window - cap - overhead <= 0, and at the 32768 default that is any cap at or above
// 28672. `atcr review` then ships only the smallest single file (a false-clean review at
// exit 0) or refuses the whole run pre-dispatch under on_overflow fail/fallback.
//
// The probe itself succeeds, because the nonce prompt is trivial at any cap — so doctor
// reported `ok`, exit 0, on a configuration review cannot execute. Worse, doctor's own
// marker-absent hint recommends raising the declaration, which is the action that drives
// an operator into this state.
func TestRun_WarnsWhenTheCapLeavesNoInputBudget(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	const declaredCap = 32000
	res := zeroBudgetTarget(t, declaredCap)

	// Guard the premise rather than restating payload's arithmetic here: if the budget is
	// not actually zero, this test is asserting nothing.
	window := res.Agents[0].ContextWindowTokens
	require.Zero(t, payload.EffectiveByteBudget("m", &window, declaredCap),
		"premise broken: window %d with cap %d still funds an input budget", window, declaredCap)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil // a healthy endpoint: the marker comes back
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	assert.Equal(t, StatusOKWarning, got.Status,
		"an agent review cannot size a payload for is not `ok`, however healthy its endpoint")
	assert.Contains(t, got.Hint, "max_tokens",
		"the hint must name the cap — it is one of the two knobs that closed the budget")
	assert.Contains(t, got.Hint, "context_window_tokens",
		"and the window, which is the other one")
}

// The warning must not fire on an ordinary agent, or it is noise that trains operators to
// ignore the row. A default-capped agent on the same default window has budget to spare.
func TestRun_SilentWhenTheCapLeavesInputBudget(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	const declaredCap = 8192
	res := zeroBudgetTarget(t, declaredCap)

	window := res.Agents[0].ContextWindowTokens
	require.NotZero(t, payload.EffectiveByteBudget("m", &window, declaredCap),
		"premise broken: this cap was supposed to leave a budget")

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	assert.Equal(t, StatusOK, rep.Agents[0].Status)
	assert.Empty(t, rep.Agents[0].Hint)
}

// A probe that never placed a call reports MaxTokens 0, which means "no cap applied" and
// not "a cap of zero". Reading it as a cap would compute a full-window budget and, worse,
// would overwrite a real failure status with a budget warning.
func TestRun_ZeroBudgetWarningDoesNotOverwriteAFailedProbe(t *testing.T) {
	res := zeroBudgetTarget(t, 32000) // no ATCR_DOCTOR_KEY set: the probe short-circuits

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		t.Fatal("no call should be placed when the api key env is unset")
		return "", nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	assert.Equal(t, StatusMissingKey, rep.Agents[0].Status,
		"the real failure must survive; a budget warning must not mask it")
	assert.Zero(t, rep.Agents[0].MaxTokens, "no cap was applied because no call was made")
}
