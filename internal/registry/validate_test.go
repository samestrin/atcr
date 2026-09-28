package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateAgentYAML_ExtraFieldsAllowed locks the non-strict unmarshal
// contract: community persona files carry persona-file metadata (version,
// description, fixture) that the registry's AgentConfig schema does not define,
// and those extra keys must be ignored rather than rejected.
func TestValidateAgentYAML_ExtraFieldsAllowed(t *testing.T) {
	yaml := []byte(`
version: "1.0"
description: "A community persona with extra metadata fields"
fixture: "testdata/sample.go"
provider: openai
model: gpt-4
role: reviewer
`)
	err := ValidateAgentYAML("extra-fields-persona", yaml)
	assert.NoError(t, err, "community persona metadata fields outside the AgentConfig schema must not cause validation errors")
}

func TestValidateAgentYAML_MissingRequiredFields(t *testing.T) {
	// Missing provider and model should fail validation.
	yaml := []byte(`
role: reviewer
`)
	err := ValidateAgentYAML("missing-required", yaml)
	assert.Error(t, err, "missing provider and model must fail validation")
	assert.Contains(t, err.Error(), "provider", "error must mention missing provider")
	assert.Contains(t, err.Error(), "model", "error must mention missing model")
}

// A community persona declaring every machine-local key is rejected once, in
// full: the joined error lists every rejection so one install attempt suffices
// to clean up the file (matching validateAgent's accumulate-never-
// short-circuit posture for the same fields).
func TestRejectMachineLocalFields_Accumulates(t *testing.T) {
	window := 128000
	cfg := AgentConfig{
		ContextWindowTokens: &window,
		ResponseFormat:      ResponseFormatJSONObject,
		Thinking:            ThinkingOn,
		ThinkingLevel:       ThinkingLevelHigh,
		ThinkingStyle:       ThinkingStyleQwen,
	}
	err := rejectMachineLocalFields("kai", cfg)
	require.Error(t, err)
	for _, want := range []string{
		"must not declare context_window_tokens",
		"must not declare response_format",
		"must not declare thinking:",
		"must not declare thinking_level",
		"must not declare thinking_style",
	} {
		assert.Contains(t, err.Error(), want, "the joined error must list every rejection, not stop at the first")
	}
}
