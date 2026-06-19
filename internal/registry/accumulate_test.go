package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 4.2 (AC6): configuration validation reports every error at once rather
// than short-circuiting on the first one, so a user fixes all config mistakes
// in a single edit instead of one-per-run. These tests pin the accumulate-all
// contract across the three validation surfaces: (*Registry).validate(),
// ValidateFallbacks(), and the merged-registry attribution path.

// TestValidate_AccumulatesAllErrors proves a single registry carrying multiple
// independent faults surfaces all of them in one error, not just the first.
func TestValidate_AccumulatesAllErrors(t *testing.T) {
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
		Agents: map[string]AgentConfig{
			"alpha": {Provider: "openai", Model: ""},                        // missing model
			"bravo": {Provider: "openai", Model: "m", MinSeverity: "BOGUS"}, // invalid enum
		},
		PayloadMode: "bogus",    // invalid enum
		TimeoutSecs: intPtr(-1), // out of range
	}

	err := reg.validate()
	require.Error(t, err)

	msg := err.Error()
	// Every distinct fault must appear — not just whichever the loop hit first.
	assert.Contains(t, msg, "timeout_secs", "settings range error must be reported")
	assert.Contains(t, msg, "payload_mode", "settings enum error must be reported")
	assert.Contains(t, msg, "alpha", "missing-model agent error must be reported")
	assert.Contains(t, msg, "model", "missing-model agent error must be reported")
	assert.Contains(t, msg, "bravo", "invalid-enum agent error must be reported")
	assert.Contains(t, msg, "min_severity", "invalid-enum agent error must be reported")
}

// TestValidate_DeterministicOrder proves accumulated output is stable across
// runs despite Go map iteration being randomized — required so error messages
// and any golden assertions don't flake.
func TestValidate_DeterministicOrder(t *testing.T) {
	build := func() *Registry {
		return &Registry{
			Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
			Agents: map[string]AgentConfig{
				"zulu":   {Provider: "openai", Model: ""},
				"alpha":  {Provider: "openai", Model: ""},
				"mike":   {Provider: "openai", Model: ""},
				"bravo":  {Provider: "openai", Model: ""},
				"yankee": {Provider: "openai", Model: ""},
			},
		}
	}
	first := build().validate().Error()
	for i := 0; i < 20; i++ {
		require.Equal(t, first, build().validate().Error(),
			"accumulated validation output must be deterministic across runs")
	}
	// Agents reported in sorted name order.
	idxAlpha := strings.Index(first, "alpha")
	idxBravo := strings.Index(first, "bravo")
	idxMike := strings.Index(first, "mike")
	idxYankee := strings.Index(first, "yankee")
	idxZulu := strings.Index(first, "zulu")
	assert.True(t, idxAlpha < idxBravo && idxBravo < idxMike && idxMike < idxYankee && idxYankee < idxZulu,
		"agent errors must be emitted in sorted order, got: %s", first)
}

// TestValidate_ValidRegistryReturnsNil guards the happy path: accumulation must
// not manufacture an error when there are none (errors.Join(nil...) == nil).
func TestValidate_ValidRegistryReturnsNil(t *testing.T) {
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
		Agents:    map[string]AgentConfig{"alpha": {Provider: "openai", Model: "m"}},
	}
	assert.NoError(t, reg.validate())
}

// TestValidateFallbacks_AccumulatesMultipleDangling proves two distinct dangling
// references are both reported in one pass.
func TestValidateFallbacks_AccumulatesMultipleDangling(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{
		"alpha": "ghost-a",
		"bravo": "ghost-b",
	})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDanglingFallback)
	assert.Contains(t, err.Error(), "agent 'alpha' fallback references unknown agent 'ghost-a'")
	assert.Contains(t, err.Error(), "agent 'bravo' fallback references unknown agent 'ghost-b'")
}

// TestValidateFallbacks_AccumulatesDanglingAndCycle proves an independent
// dangling reference and a cycle are both reported, not just the first found.
func TestValidateFallbacks_AccumulatesDanglingAndCycle(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{
		"dang":  "ghost",
		"cycle": "loop",
		"loop":  "cycle",
	})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDanglingFallback)
	assert.ErrorIs(t, err, ErrFallbackCycle)
	assert.Contains(t, err.Error(), "agent 'dang' fallback references unknown agent 'ghost'")
	assert.Contains(t, err.Error(), "fallback cycle detected")
}

// TestValidateFallbacks_LeadInIntoReportedCycleNoPanic guards the accumulation
// hazard: after a cycle is detected, continuing the DFS must not re-walk a
// lead-in node that points INTO the already-reported cycle (which would trip
// walkFallbacks's "gray node must be on current path" invariant and panic).
// {a:b, b:a} is a cycle; {c:a} is a lead-in that must NOT be reported as a
// second cycle and must NOT panic.
func TestValidateFallbacks_LeadInIntoReportedCycleNoPanic(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{
		"a": "b",
		"b": "a",
		"c": "a",
	})
	var err error
	require.NotPanics(t, func() { err = reg.ValidateFallbacks() })
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFallbackCycle)
	// Exactly one cycle reported — the lead-in "c" is not a separate cycle.
	assert.Equal(t, 1, strings.Count(err.Error(), "fallback cycle detected"),
		"lead-in into an existing cycle must not be reported as a second cycle, got: %s", err.Error())
}

// TestValidateFallbacks_TwoIndependentCycles proves two disjoint cycles are both
// reported.
func TestValidateFallbacks_TwoIndependentCycles(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{
		"a": "b", "b": "a",
		"c": "d", "d": "c",
	})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Equal(t, 2, strings.Count(err.Error(), "fallback cycle detected"),
		"two disjoint cycles must both be reported, got: %s", err.Error())
}

// TestValidateFallbacks_ValidReturnsNil guards the happy path.
func TestValidateFallbacks_ValidReturnsNil(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"a": "b", "b": ""})
	assert.NoError(t, reg.ValidateFallbacks())
}
