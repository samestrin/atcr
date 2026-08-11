package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD: gopkg.in/yaml.v3 decodes a YAML float into an int field by
// TRUNCATING it, so `max_context_lines: 1200.5` loaded as 1200 and
// `context_window_tokens: 1.28e6` loaded as 1280000 — an operator typo accepted
// with the fraction silently discarded. The guard is registry-WIDE (one shared
// strict-integer decode check driven by the target struct's own field types),
// not a bespoke unmarshaler on whichever field the bug was noticed in.

func TestStrictInt_RejectsFractionalAgentField(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    max_context_lines: 1200.5
`))
	require.Error(t, err, "a fractional value in an int field must not load")
	assert.Contains(t, err.Error(), "max_context_lines",
		"the error must name the offending key — the whole point is that the typo was silent")
	assert.Contains(t, err.Error(), "must be an integer")
}

func TestStrictInt_RejectsExponentNotation(t *testing.T) {
	// 1.28e6 is exactly representable, but it still arrives as a YAML float and
	// is truncated on the way in; accepting it makes the load path's behavior
	// depend on whether the typo happens to be lossless.
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: 1.28e6
`))
	require.Error(t, err, "exponent notation in an int field must not load")
	assert.Contains(t, err.Error(), "context_window_tokens")
}

func TestStrictInt_RejectsFractionalSettingsField(t *testing.T) {
	// The guard is not agent-scoped: a registry-level (settings-tier) int field
	// truncates identically, so it must be rejected by the same pass.
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
payload_byte_budget: 400000.5
agents:
  bruce:
    provider: p
    model: m
`))
	require.Error(t, err, "a fractional value in a settings int field must not load")
	assert.Contains(t, err.Error(), "payload_byte_budget")
}

func TestStrictInt_AcceptsIntegersAndRealFloatFields(t *testing.T) {
	// The guard must key on the FIELD's declared type, not on the value's shape:
	// a genuine float field (temperature) keeps accepting a fractional value, and
	// ordinary integers keep loading.
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
payload_byte_budget: 400000
agents:
  bruce:
    provider: p
    model: m
    max_context_lines: 1200
    context_window_tokens: 128000
    temperature: 0.7
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Agents["bruce"].MaxContextLines)
	assert.Equal(t, 1200, *reg.Agents["bruce"].MaxContextLines)
	require.NotNil(t, reg.Agents["bruce"].Temperature)
	assert.InDelta(t, 0.7, *reg.Agents["bruce"].Temperature, 1e-9,
		"a genuinely float-typed field must be unaffected by the integer guard")
}
