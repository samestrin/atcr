package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
)

// TD cli/doctor.go:255 (TD-017 re-attempt, user decision 2026-09-27 scope a):
// AgentResult must carry the DECLARED thinking polarity — "off", "on", or the
// declared level — so the cli warning can split its remedy by polarity instead
// of telling every not-honored agent "a larger max_tokens or a different
// model". A level is logically "on", so it must serialize as the level, not a
// boolean. Undeclared agents and probes that placed no call leave it empty.
func TestRun_ThinkingDeclaredPolarity(t *testing.T) {
	thinks := withMarker(llmclient.Completion{Usage: llmclient.UsageData{ReasoningTokens: 32, ReasoningTokensReported: true}})
	silent := withMarker(llmclient.Completion{Usage: llmclient.UsageData{ReasoningTokensReported: true}})

	// off: a reasoning signal contradicts it → not honored, polarity "off".
	res := thinkingTarget(t, registry.ThinkingOff, "", registry.ThinkingStyleQwen)
	agent, _, _ := runThinking(t, res, thinks, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingNotHonored, agent.ThinkingStatus)
	assert.Equal(t, registry.ThinkingOff, agent.ThinkingDeclared)

	// level: implies on, serializes as the LEVEL, not a boolean.
	res = thinkingTarget(t, registry.ThinkingOn, registry.ThinkingLevelLow, registry.ThinkingStyleQwen)
	agent, _, _ = runThinking(t, res, silent, nil, thinks, nil)
	assert.Equal(t, ThinkingHonored, agent.ThinkingStatus)
	assert.Equal(t, registry.ThinkingLevelLow, agent.ThinkingDeclared)

	// on with no level: a signal on the declared call itself is what makes it
	// honored (a reported 0 short-circuits before any control call). Polarity "on".
	res = thinkingTarget(t, registry.ThinkingOn, "", registry.ThinkingStyleQwen)
	agent, _, _ = runThinking(t, res, thinks, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingHonored, agent.ThinkingStatus)
	assert.Equal(t, registry.ThinkingOn, agent.ThinkingDeclared)

	// Undeclared agent: no probe, no polarity.
	res, err := Resolve(regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m"}},
	), &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	agent, _, _ = runThinking(t, res, silent, nil, llmclient.Completion{}, nil)
	assert.Empty(t, agent.ThinkingStatus)
	assert.Empty(t, agent.ThinkingDeclared)
}
