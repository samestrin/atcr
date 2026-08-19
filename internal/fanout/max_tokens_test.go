package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
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

// Only the BULK path was pinned: TestBuildSlots_ResolvedMaxTokensIsAlsoWhatSizingReserves
// uses a bulk roster, so the chunked site's use of the resolved cap survived reversion
// to defaultMaxTokens with the fanout suite green. That leaves maxTokensFor's stated
// guarantee — that the CLI tier cannot be applied at some sites and forgotten at
// others — unproven at precisely the site where forgetting it changes how the diff is
// cut, not just what the provider is asked for.
func TestBuildSlots_ChunkedLineBudgetDerivesFromTheResolvedMaxTokens(t *testing.T) {
	// A cap far above the embedded default reserves more of the window for output, so
	// ChunkMaxLines derives a SMALLER per-chunk line budget. Asserting the difference
	// (rather than an absolute) is what makes reverting to the constant observable.
	declared := 60000
	require.NotEqual(t, defaultMaxTokens, declared, "precondition: the declaration must differ from the constant")

	build := func(t *testing.T, maxTokens *int) Agent {
		t.Helper()
		cfg := declaredWindowRoster(t, 128000)
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.ReviewStrategy = "chunked"
		ac := cfg.Registry.Agents["greta"]
		ac.MaxTokens = maxTokens
		cfg.Registry.Agents["greta"] = ac

		slots, _, err := buildSlots(cfg, map[string]modePayload{
			"blocks": {Text: diffOfNFiles(12, 900), FileCount: 12},
		}, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
		require.Greater(t, len(slots), 1, "precondition: the diff must split into chunk slots")
		return slots[0].Primary
	}

	atDefault := build(t, nil)
	atDeclared := build(t, &declared)

	require.Greater(t, atDefault.chunkMaxLines, 0, "precondition: the chunked path derives a line budget")
	assert.Less(t, atDeclared.chunkMaxLines, atDefault.chunkMaxLines,
		"a larger output reservation must leave fewer input lines per chunk — the chunked site must read "+
			"the RESOLVED cap, not the embedded default")

	// And the same resolved cap still reaches the chunk slot's own invocation, so the
	// number the diff was cut for and the number the provider is asked for agree.
	require.NotNil(t, atDeclared.Invocation.MaxTokens)
	assert.Equal(t, declared, *atDeclared.Invocation.MaxTokens)
	assert.Equal(t, declared, atDeclared.ReservedOutputTokens)
}

// buildFallbackAgent resolves its own output cap at TWO sites — the
// EffectiveByteBudget call that sizes it, and the Invocation it dispatches with —
// and both survived reversion to defaultMaxTokens. A fallback could therefore be
// sized and dispatched at the embedded default while its own declaration said
// otherwise, with no test failing. Pin the whole fallback chain: declaration,
// run-wide override, and the two sites agreeing with each other.
func TestBuildFallbackAgent_ResolvedMaxTokensReachesSizingAndInvocation(t *testing.T) {
	t.Run("the fallback resolves its OWN declaration, not the primary's", func(t *testing.T) {
		cfg := declaredWindowRoster(t, 512000)
		primaryCap, fallbackCap := 40000, 24000
		greta := cfg.Registry.Agents["greta"]
		greta.MaxTokens = &primaryCap
		cfg.Registry.Agents["greta"] = greta
		kai := cfg.Registry.Agents["kai"]
		kai.MaxTokens = &fallbackCap
		cfg.Registry.Agents["kai"] = kai

		primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)
		require.Equal(t, primaryCap, primary.ReservedOutputTokens, "precondition: the primary carries its own cap")

		fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
		require.NoError(t, err)

		require.NotNil(t, fb.Invocation.MaxTokens)
		assert.Equal(t, fallbackCap, *fb.Invocation.MaxTokens,
			"the Invocation site must use the fallback's own resolved cap")
		assert.Equal(t, fallbackCap, fb.ReservedOutputTokens,
			"and the SIZING site must reserve the same number — sized for one budget and asked for "+
				"another is the defect maxTokensFor exists to prevent")
		assert.Equal(t, payload.EffectiveByteBudget(kai.Model, kai.ContextWindowTokens, fallbackCap), fb.EffectiveBudget,
			"the byte budget must be derived from that same cap, not the embedded default")
	})

	t.Run("the run-wide override beats the fallback's declaration at both sites", func(t *testing.T) {
		cfg := declaredWindowRoster(t, 512000)
		declared, override := 24000, 12000
		kai := cfg.Registry.Agents["kai"]
		kai.MaxTokens = &declared
		cfg.Registry.Agents["kai"] = kai
		cfg.Settings.MaxTokens = override

		primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)

		fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
		require.NoError(t, err)

		require.NotNil(t, fb.Invocation.MaxTokens)
		assert.Equal(t, override, *fb.Invocation.MaxTokens, "--max-tokens must reach the fallback's invocation")
		assert.Equal(t, override, fb.ReservedOutputTokens, "...and its sizing reservation")
	})
}
