package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultMaxTokens was a hardcoded 8192 output cap applied to EVERY reviewer call,
// with no registry field, no config key and no flag. Its own comment named the risk
// it could not be tuned around: a thinking model spends output budget on
// chain-of-thought, finishes mid-reasoning, hits finish_reason=length with zero
// parsed findings, and is demoted by the truncation gate — so the lens contributes
// NOTHING while the run still reports success. Observed 2026-08-14 across glm-5.2,
// minimax-m2.7, kimi-k3 and minimax-m3 on one baseline scan. `atcr doctor` already
// printed "raise --max-tokens", naming a lever `atcr review` did not expose.
//
// Resolution mirrors context_window_tokens: agent declaration first, embedded
// default last — plus a run-wide CLI override on top, which the window declaration
// deliberately does not have (a window is machine truth about a deployment; an
// output cap is an operator's per-run choice).
func TestResolveMaxTokens_DeclarationThenDefault(t *testing.T) {
	t.Run("undeclared agent keeps the embedded default", func(t *testing.T) {
		assert.Equal(t, defaultMaxTokens, resolveMaxTokens(registry.AgentConfig{}, 0))
	})

	t.Run("agent declaration wins over the default", func(t *testing.T) {
		declared := 32000
		assert.Equal(t, declared, resolveMaxTokens(registry.AgentConfig{MaxTokens: &declared}, 0))
	})

	t.Run("the run-wide override wins over an agent declaration", func(t *testing.T) {
		declared := 32000
		assert.Equal(t, 16000, resolveMaxTokens(registry.AgentConfig{MaxTokens: &declared}, 16000),
			"--max-tokens is the operator's per-run escape hatch; it must beat a stale declaration")
	})

	t.Run("the run-wide override applies to an undeclared agent too", func(t *testing.T) {
		assert.Equal(t, 16000, resolveMaxTokens(registry.AgentConfig{}, 16000))
	})

	t.Run("a zero override means unset, not zero tokens", func(t *testing.T) {
		declared := 32000
		assert.Equal(t, declared, resolveMaxTokens(registry.AgentConfig{MaxTokens: &declared}, 0),
			"0 is the not-set sentinel for the override; a zero output cap would make every call return nothing")
	})
}

// benchSlotCfg is a single-agent bulk roster with a declared window large enough
// that sizing never degrades the slot, so these tests observe the output cap alone.
func benchSlotCfg(t *testing.T) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, 128000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	return cfg
}

// The resolved cap must reach the actual Invocation — a resolution helper nothing
// dispatches through is a number in a unit test, not a lever on a run. This is the
// assertion that would have failed for the whole life of the defect.
func TestBuildSlots_ResolvedMaxTokensReachesTheInvocation(t *testing.T) {
	declared := 32000
	cfg := benchSlotCfg(t)
	ac := cfg.Registry.Agents["greta"]
	ac.MaxTokens = &declared
	cfg.Registry.Agents["greta"] = ac

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	require.NotNil(t, slots[0].Primary.Invocation.MaxTokens)
	assert.Equal(t, declared, *slots[0].Primary.Invocation.MaxTokens,
		"the declared output cap must be the cap the provider is actually asked for")
}

// The cap is also the value reserved out of the context window when sizing the
// payload (EffectiveByteBudget's outputTokens argument). Left at the constant while
// the Invocation used a declared value, an agent would be sized for 8192 output
// tokens and then asked for 32000 — over-filling the very window the sizing exists
// to respect.
func TestBuildSlots_ResolvedMaxTokensIsAlsoWhatSizingReserves(t *testing.T) {
	declared := 32000
	cfg := benchSlotCfg(t)
	ac := cfg.Registry.Agents["greta"]
	ac.MaxTokens = &declared
	cfg.Registry.Agents["greta"] = ac

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	assert.Equal(t, declared, slots[0].Primary.ReservedOutputTokens,
		"sizing must reserve the cap the call will actually use, not the embedded constant")
}
