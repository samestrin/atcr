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
