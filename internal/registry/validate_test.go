package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
