package doctor

import (
	"context"
	"net/http"
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
	assert.Contains(t, got.Hint, "endpoint is healthy, but",
		"the lead distinguishes this arm from the marker-absent one; without it pinned, the "+
			"two messages collapse and the branch that picks between them is untested")
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

// The case the row was actually filed for: marker absent AND no input budget. The
// marker-absent hint's remedy is "raise this agent's max_tokens declaration", and at a
// closed budget that advice makes the run worse — the cap comes out of the same window —
// while the probe passes at the higher cap because the nonce prompt is trivial. So the
// operator raises the cap, doctor goes green, and the review ships one file.
//
// Skipping already-warning rows would have preserved exactly that trap, so the budget
// verdict has to REPLACE the hint here, not defer to it.
func TestRun_ZeroBudgetHintOverridesTheRaiseTheCapAdvice(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	const declaredCap = 32000
	res := zeroBudgetTarget(t, declaredCap)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", nil // HTTP 200, marker absent — the ok_warning path
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	require.Equal(t, StatusOKWarning, got.Status)
	assert.NotContains(t, got.Hint, "raise this agent's max_tokens declaration",
		"the marker-absent remedy must not survive here — following it deepens the zero budget")
	assert.Contains(t, got.Hint, "Do NOT raise the cap",
		"the hint has to contradict that advice, not merely omit it")
	assert.Contains(t, got.Hint, zeroBudgetRemedy)
	assert.Contains(t, got.Hint, "the marker was absent AND",
		"this arm must SAY the marker was absent — asserting only the clause both arms share "+
			"leaves the branch that chooses between them unpinned")
}

// The verdict asserts a REVIEW-time outcome, so it may only be drawn from a cap review
// will actually resolve. Under an explicit --max-tokens the probed cap is DOCTOR'S flag
// value: cli/doctor.go passes it as the override, ResolveWithCap stamps it onto every
// target, and probe() uses it — while review resolves independently through
// resolveMaxTokens (its own --max-tokens -> the agent's declaration -> 8192).
//
// So `atcr doctor --max-tokens 30000` would, read off the PROBE, close the budget of
// every agent on the 32768 default window (32768-30000-4096 = -1328) and warn that
// review is about to ship one file. It is not: this agent DECLARES nothing, so review
// caps it at 8192 and the 32768 window leaves 20480 input tokens (payload/sizing.go —
// 32768 - 8192 output - 4096 overhead), a budget with room to spare.
//
// The row therefore stays ok because of what review will resolve, not because doctor's
// flag was typed. That distinction is the whole point: an agent whose OWN declaration
// closed the budget must still be reported under the same flag, which is what
// TestRun_ZeroBudgetVerdictFiresForADeclaredCapDespiteDoctorsOwnFlag pins. run.go states
// the rule this obeys for classify()'s hint: no branch may assert which knob governs the
// real run, because that is conditional on how review is later invoked.
func TestRun_NoZeroBudgetVerdictWhenTheCapCameFromDoctorsOwnFlag(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	// Undeclared agent: nothing here closes the budget except the flag below.
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m"}},
	)
	res, err := ResolveWithCap(reg, &registry.ProjectConfig{Agents: []string{"a"}}, 30000)
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 30000, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	require.Equal(t, MaxTokensSourceFlag, got.MaxTokensSource, "precondition: the cap came from the flag")

	assert.Equal(t, StatusOK, got.Status,
		"doctor's own --max-tokens does not reach `atcr review`, so a budget it closes says "+
			"nothing about the run and must not downgrade the row")
	assert.Empty(t, got.Hint)
}

// The declaration tier is the case the verdict IS entitled to speak for: `atcr review`
// resolves the same declaration when no review-side flag overrides it.
func TestRun_ZeroBudgetVerdictStillFiresForADeclaredCap(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := zeroBudgetTarget(t, 32000)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	require.Equal(t, MaxTokensSourceDeclaration, rep.Agents[0].MaxTokensSource, "precondition")
	assert.Equal(t, StatusOKWarning, rep.Agents[0].Status)
}

// The healthy() clause standing alone. The missing_key fixture below cannot pin it,
// because that probe also reports MaxTokens 0 and is caught by the other clause — each
// guard was covered only by the other's fixture. An auth failure resolves a cap first, so
// it isolates this one.
func TestRun_ZeroBudgetVerdictDoesNotOverwriteAFailureThatResolvedACap(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := zeroBudgetTarget(t, 32000)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", &llmclient.HTTPStatusError{Status: http.StatusUnauthorized, Snippet: "bad key"}
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	require.NotZero(t, rep.Agents[0].MaxTokens, "precondition: this probe DID resolve a cap before failing")
	assert.Equal(t, StatusAuthFailed, rep.Agents[0].Status,
		"a real failure outranks a budget warning — the operator has to fix the key first")
}

// A 1-token window against the cap `atcr review` will resolve. The PROBE here runs
// uncapped (MaxTokensSet with a 0 value), but that is evidence about the probe, and the
// verdict is not: review caps this undeclared agent at its built-in default, and 1 token
// funds neither that nor the prompt overhead. So the budget review will run at really is
// closed and the row must say so.
//
// This is the case that changed when the operand moved from the probed cap to review's.
// It used to assert the opposite, on the reasoning that "no cap was applied, so no cap
// closed this budget" — true of the probe, and the reason the maxTokens guard exists,
// but the guard now defends the FUNCTION's contract rather than a state Run can reach
// (see reviewMaxTokens). Under review-time evaluation a cap always notionally applies,
// so an uncapped probe no longer implies an uncapped run. The hint still does not blame
// the cap: its remedy names the context_window_tokens declaration, which is what is
// actually at fault here.
func TestRun_ZeroBudgetVerdictDoesNotBlameACapThatWasNeverApplied(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	tiny := 1
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &tiny}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	// MaxTokensSet with a 0 value: probe() applies no cap and records MaxTokens 0.
	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 0, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	require.Zero(t, rep.Agents[0].MaxTokens, "precondition: the PROBE applied no cap")
	assert.Equal(t, StatusOKWarning, rep.Agents[0].Status,
		"a 1-token window cannot fund the cap review will apply, whatever this probe was capped at")
	assert.Contains(t, rep.Agents[0].Hint, "context_window_tokens",
		"the window is what is at fault here, and the remedy must name it rather than a cap")
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

// The hint's "(not this probe's cap)" disclaimer is FALSE on every run without
// --max-tokens, which is the default. Unflagged, probe() resolves the same cap
// reviewMaxTokens does — the agent's declaration, else the shared default — so the
// two are equal, render.go SUPPRESSES the "/ review cap N" suffix, and the hint
// disclaims the only cap on screen while being exactly that cap. The disclaimer was
// added for the flag path and applied unconditionally; it belongs only where the two
// caps genuinely differ.
func TestRun_ZeroBudgetHintDoesNotDisclaimTheProbeCapWhenTheyAreEqual(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	tiny := 1
	declaredCap := 4096
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &tiny, MaxTokens: &declaredCap}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	// No --max-tokens: the default path. probe() resolves the agent's own
	// declaration and reviewMaxTokens returns the same one, so the two coincide.
	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	require.Equal(t, rep.Agents[0].ReviewMaxTokens, rep.Agents[0].MaxTokens,
		"precondition: unflagged, the probe's cap and review's are the same value")
	require.Equal(t, StatusOKWarning, rep.Agents[0].Status, "precondition: the zero-budget verdict fires")

	assert.NotContains(t, rep.Agents[0].Hint, "not this probe's cap",
		"the two caps are the same number here, so disclaiming one against the other tells the operator the cap on screen is a different cap than it is")
	assert.Contains(t, rep.Agents[0].Hint, "output cap",
		"the cap is still named — only the false contrast is dropped")
}

// ...and the disclaimer must survive where it is true: under --max-tokens with an N
// that is not the cap review would resolve, the row's own max_tokens column and the
// hint's operand are genuinely different numbers, and the operator has to act on the
// hint's.
func TestRun_ZeroBudgetHintKeepsTheDisclaimerWhenTheCapsDiffer(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	tiny := 1
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &tiny}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 111, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	require.NotEqual(t, rep.Agents[0].ReviewMaxTokens, rep.Agents[0].MaxTokens,
		"precondition: the flag makes the probe's cap differ from review's")
	require.Equal(t, StatusOKWarning, rep.Agents[0].Status, "precondition: the zero-budget verdict fires")

	assert.Contains(t, rep.Agents[0].Hint, "not this probe's cap",
		"here the hint's number really does disagree with the row's max_tokens column, and the operator must act on the hint's")
}

// Both remedies apply on this row and only one survives. Under `atcr doctor
// --max-tokens N` against an agent whose OWN declaration closes review's budget,
// classify() has already produced the flag-specific remedy — "this probe was capped
// by your explicit --max-tokens; raise it to re-probe" — and Run then replaces
// status AND hint wholesale with the zero-budget text, which ends "Do NOT raise the
// cap here: it is reserved out of this same window".
//
// The two sentences are about DIFFERENT knobs. "the cap here" is the declaration
// reserved out of the window; the max_tokens column on that same row reads "N
// (flag)", the cap the operator genuinely should raise to re-probe the marker. The
// "(not this probe's cap)" disclaimer attaches to the operand clause, not to the "Do
// NOT raise" sentence, so nothing on screen tells the operator the probe cap is
// still theirs to raise. The probe-specific remedy is simply unreachable under the
// flag.
func TestRun_ZeroBudgetHintKeepsTheProbeRemedyWhenTheMarkerWasAlsoAbsent(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	tiny := 1
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &tiny}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	// No marker: classify() returns StatusOKWarning carrying the flag-specific remedy.
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return "no marker here", nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 111, MaxTokensSet: true})

	require.Len(t, rep.Agents, 1)
	require.NotEqual(t, rep.Agents[0].ReviewMaxTokens, rep.Agents[0].MaxTokens,
		"precondition: the flag makes the probe's cap differ from review's")
	require.Equal(t, StatusOKWarning, rep.Agents[0].Status, "precondition: the zero-budget verdict fires")

	hint := rep.Agents[0].Hint
	assert.Contains(t, hint, "marker was absent",
		"precondition: this is the row where BOTH the marker-absent and the zero-budget conditions hold")
	assert.Contains(t, hint, "--max-tokens",
		"the probe cap is the operator's own flag and re-probing the marker means raising it — that remedy must not be dropped just because a second one applies")
	assert.Contains(t, hint, "111",
		"and it must name the cap that actually capped this probe, which is the number in the row's max_tokens column")
}
